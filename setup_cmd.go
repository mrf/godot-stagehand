package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/mrf/godot-stagehand/internal/cli"
	"github.com/mrf/godot-stagehand/internal/setup"
)

// runSetup implements the "godot-stagehand setup <project_path>" subcommand.
// Flag/usage output goes to stderr rather than the hardcoded os.Stderr so
// callers (and tests) can capture it; a flag.ErrHelp is returned to the
// caller unmodified rather than swallowed, matching how every other
// subcommand's --help is handled in internal/cli.
func runSetup(args []string, stderr io.Writer) error {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	fset.SetOutput(stderr)
	force := fset.Bool("force", false, "overwrite an existing addon installation")
	fset.Usage = func() {
		fmt.Fprintln(stderr, "Usage: godot-stagehand setup [--force] <project_path>")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Installs the Stagehand addon into a Godot project: copies the addon,")
		fmt.Fprintln(stderr, "enables the plugin, registers the StagehandServer autoload, and prints")
		fmt.Fprintln(stderr, "the MCP client configuration snippet.")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "  --force")
		fmt.Fprintln(stderr, "    \toverwrite an existing addon installation")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Flags may appear before or after <project_path>.")
	}
	// Go's flag package stops parsing at the first non-flag argument, so
	// "setup proj --force" would otherwise leave --force in the positional
	// args. cli.Permute reorders flags ahead of positionals, matching how
	// every other subcommand (via internal/cli's runWithFlags) already
	// accepts trailing flags.
	if err := fset.Parse(cli.Permute(fset, args)); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return fmt.Errorf("expected exactly one project path argument")
	}

	addonFS, err := subAddonFS()
	if err != nil {
		return err
	}

	binaryPath := detectBinaryPath()

	return setup.Run(os.Stdout, setup.Options{
		ProjectPath: fset.Arg(0),
		Force:       *force,
		BinaryPath:  binaryPath,
		AddonFS:     addonFS,
		IsWSL:       setup.DetectWSL(),
	})
}

// subAddonFS returns the embedded addon tree rooted so that plugin.cfg is at the
// top level (stripping the addons/stagehand prefix).
func subAddonFS() (fs.FS, error) {
	sub, err := fs.Sub(addonAssets, "addons/stagehand")
	if err != nil {
		return nil, fmt.Errorf("locate embedded addon: %w", err)
	}
	return sub, nil
}

// detectBinaryPath returns the absolute path to this executable for use in the
// MCP config snippet, falling back to os.Args[0] when detection fails.
func detectBinaryPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return os.Args[0]
}
