// Package cli is the executable Stagehand frontend: one-shot commands for
// human debugging and a declarative scenario runner for CI. It sits beside the
// MCP server rather than under it — both talk to internal/godotconn through
// the shared operation registry in internal/gwpop, and neither depends on the
// other.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// command is one CLI verb.
type command struct {
	name string
	// usage is the argument summary shown after the command name.
	usage string
	// summary is the one-line description in the command list.
	summary string
	// run executes the command; env carries the shared streams and context.
	run func(ctx context.Context, env *env, cmd *command, args []string) error
	// connects reports whether the command opens a Godot connection, which
	// determines whether the shared connection flags are registered.
	connects bool
}

// env bundles what every command needs, so tests can capture output.
type env struct {
	stdout io.Writer
	stderr io.Writer
	conn   connectionFlags
}

var commands = buildCommands()

func buildCommands() map[string]*command {
	list := []*command{
		cmdConnect, cmdStatus, cmdTree, cmdFind, cmdProperty, cmdCall,
		cmdEval, cmdInput, cmdWait, cmdScreenshot, cmdPerformance, cmdScene,
		cmdRun, cmdActions,
	}
	byName := make(map[string]*command, len(list))
	for _, c := range list {
		byName[c.name] = c
	}
	return byName
}

// Run dispatches a CLI invocation and returns the process exit code. It never
// panics on bad input: everything the caller can get wrong maps to ExitUsage
// with a printed usage block.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelp(args[0]) {
		printUsage(stdout)
		return ExitOK
	}

	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "error: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return ExitUsage
	}

	environment := &env{stdout: stdout, stderr: stderr}
	err := cmd.run(ctx, environment, cmd, args[1:])
	if err == nil {
		return ExitOK
	}

	code := exitCodeFor(err)
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	if code == ExitUsage {
		fmt.Fprintf(stderr, "\nusage: godot-stagehand %s %s\n", cmd.name, cmd.usage)
	}
	return code
}

func isHelp(arg string) bool {
	switch arg {
	case "help", "-h", "--help":
		return true
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `godot-stagehand — external automation for running Godot games

Usage:
  godot-stagehand                      Serve the MCP stdio protocol (default)
  godot-stagehand serve                Serve the MCP stdio protocol, explicitly
  godot-stagehand setup <project>      Install the addon into a Godot project
  godot-stagehand <command> [flags]    Run a one-shot command against a game
  godot-stagehand run <scenario.json>  Run a scenario end to end

Commands:
`)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-12s %s\n", name, commands[name].summary)
	}

	fmt.Fprint(w, `
Connection flags (every command except run and actions):
  --host string      Godot host (default 127.0.0.1)
  --port int         Godot WebSocket port (required; the shared default 26700
                     may belong to another agent's game)
  --token string     Session secret (default $STAGEHAND_AUTH_TOKEN)
  --timeout duration Bound the whole command (default 30s)

Exit codes:
  0 success   1 internal   2 usage   3 connection   4 godot error
  5 assertion failed   6 timeout

Run 'godot-stagehand <command> --help' for command flags.
`)
}

// runWithFlags parses flags, opens a connection when the command needs one,
// and invokes body. It is the single place connection setup and teardown live,
// so no command can leak a socket or skip the handshake.
func runWithFlags(
	ctx context.Context,
	cmd *command,
	e *env,
	args []string,
	bind func(*flag.FlagSet),
	body func(context.Context, *session, *flag.FlagSet) error,
) error {
	fset := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fset.SetOutput(e.stderr)
	fset.Usage = func() {
		fmt.Fprintf(e.stderr, "usage: godot-stagehand %s %s\n\n%s\n\nFlags:\n", cmd.name, cmd.usage, cmd.summary)
		fset.PrintDefaults()
	}
	if bind != nil {
		bind(fset)
	}
	if cmd.connects {
		e.conn.bind(fset)
	}
	if err := fset.Parse(Permute(fset, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usagef(err)
	}

	if !cmd.connects {
		return body(ctx, nil, fset)
	}

	ctx, cancel := context.WithTimeout(ctx, e.conn.timeout)
	defer cancel()

	sess, err := e.conn.open()
	if err != nil {
		return err
	}
	defer sess.Close()

	return body(ctx, sess, fset)
}

// Permute moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `find class:Button --limit 5` would silently ignore --limit. A CLI that
// demands flags-first is a trap, so the ordering is normalised here instead.
// A positional that genuinely begins with "-" must be preceded by the
// conventional "--" terminator. Exported so other flag.FlagSet-based
// subcommands outside this package (e.g. the root "setup" command) get the
// same trailing-flag handling instead of inventing a second convention.
func Permute(fset *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)

		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue // value is attached
		}
		defined := fset.Lookup(name)
		if defined == nil {
			continue // unknown: let Parse produce the error
		}
		if boolFlag, ok := defined.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue // booleans never consume the next token
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

// requireArgs enforces a positional argument count.
func requireArgs(cmd *command, args []string, want int, names string) error {
	if len(args) != want {
		return usagef(fmt.Errorf("%s expects %d argument(s): %s, got %d",
			cmd.name, want, names, len(args)))
	}
	return nil
}

// splitList parses a comma-separated flag value into a slice, dropping empties.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
