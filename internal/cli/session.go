package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
	"github.com/mrf/godot-stagehand/internal/gwp"
	"github.com/mrf/godot-stagehand/internal/gwpop"
	"github.com/mrf/godot-stagehand/internal/launch"
)

// The CLI requires an explicit --port even though launch.DefaultPort exists:
// the shared default routinely belongs to a game somebody else launched, and
// silently driving another agent's SceneTree is worse than a usage error.

// connectionFlags are the flags every game-touching command accepts.
type connectionFlags struct {
	host    string
	port    int
	token   string
	timeout time.Duration
}

func (c *connectionFlags) bind(fset *flag.FlagSet) {
	fset.StringVar(&c.host, "host", launch.DefaultHost, "Godot host")
	fset.IntVar(&c.port, "port", 0, "Godot WebSocket port (required)")
	fset.StringVar(&c.token, "token", "", "session secret (default $STAGEHAND_AUTH_TOKEN)")
	fset.DurationVar(&c.timeout, "timeout", 30*time.Second, "bound the whole command")
}

// session is a connected Godot game plus its negotiated handshake.
type session struct {
	conn      *godotconn.Connection
	handshake *gwp.Info
	host      string
	port      int
}

func (s *session) Close() {
	if s != nil && s.conn != nil {
		_ = s.conn.Close()
	}
}

// Caller exposes the connection as a gwpop.Caller.
func (s *session) Caller() gwpop.Caller { return s.conn }

// open dials, authenticates and negotiates. Every failure here is a
// connection failure, so callers get ExitConnection rather than a generic one.
func (c *connectionFlags) open(ctx context.Context) (*session, error) {
	if c.port == 0 {
		return nil, usagef(fmt.Errorf(
			"--port is required: the addon's default %d is shared and may belong to another agent's game; pass the port your instance printed at startup",
			launch.DefaultPort))
	}
	if c.timeout <= 0 {
		return nil, usagef(fmt.Errorf("--timeout must be positive"))
	}
	token := c.token
	if token == "" {
		token = os.Getenv("STAGEHAND_AUTH_TOKEN")
	}
	if token == "" {
		return nil, usagef(fmt.Errorf(
			"no session secret: pass --token or set STAGEHAND_AUTH_TOKEN to the token this Godot session printed at startup"))
	}
	host := c.host
	if strings.TrimSpace(host) == "" {
		host = launch.DefaultHost
	}

	conn, handshake, err := gwpop.Connect(ctx, host, c.port, token)
	if err != nil {
		return nil, &connectionError{err: err}
	}
	return &session{conn: conn, handshake: handshake, host: host, port: c.port}, nil
}

// emit writes a JSON document, which is the CLI's only output format for
// results: a one-shot command is as likely to be consumed by jq in a shell
// script as read by a human.
func emit(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// emitRaw writes an addon result verbatim, re-indented when it is valid JSON.
func emitRaw(w io.Writer, raw json.RawMessage) error {
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		_, werr := fmt.Fprintf(w, "%s\n", raw)
		return werr
	}
	return emit(w, pretty)
}

// execute runs an operation and prints its raw result.
func execute(ctx context.Context, e *env, s *session, action string, params map[string]any) error {
	raw, err := gwpop.Execute(ctx, s.Caller(), gwpop.Op{Action: action, Params: params})
	if err != nil {
		return err
	}
	return emitRaw(e.stdout, raw)
}

// parseValue interprets a command-line value as JSON, falling back to a plain
// string. `--value 3` is the number 3; `--value hello` is the string "hello";
// `--value '"3"'` is the string "3".
func parseValue(raw string) any {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return parsed
	}
	return raw
}

// parseJSONObject decodes a flag holding a JSON object, e.g. --position '{"x":10,"y":20}'.
func parseJSONObject(name, raw string) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, usagef(fmt.Errorf("--%s must be a JSON object like {\"x\":10,\"y\":20}: %w", name, err))
	}
	return obj, nil
}

// parseJSONArray decodes a flag holding a JSON array, e.g. --args '[1,"two"]'.
func parseJSONArray(name, raw string) ([]any, error) {
	var arr []any
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, usagef(fmt.Errorf("--%s must be a JSON array like [1,\"two\"]: %w", name, err))
	}
	return arr, nil
}

// setIf adds a parameter only when the flag was given, so the addon's own
// defaults stay authoritative for anything the caller did not ask about.
func setIf(params map[string]any, name string, value any, present bool) {
	if present {
		params[name] = value
	}
}

// wasSet reports whether a flag was explicitly provided.
func wasSet(fset *flag.FlagSet, name string) bool {
	found := false
	fset.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
