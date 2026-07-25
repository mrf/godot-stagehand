package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/cli"
)

// buildBinary compiles the CLI once per test run so the compatibility checks
// exercise the real entry point rather than an in-process approximation.
func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "godot-stagehand")
	if os.Getenv("GOOS") == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build binary: %v", err)
	}
	return binary
}

// TestNoArgumentInvocationStaysAnMCPStdioServer is the compatibility gate for
// the CLI work: every configured MCP client launches this binary with no
// arguments and immediately speaks JSON-RPC over stdio. If that ever becomes a
// CLI usage dump, every existing Claude/Cursor/Windsurf configuration breaks.
func TestNoArgumentInvocationStaysAnMCPStdioServer(t *testing.T) {
	binary := buildBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"compat-test","version":"1"}}}` + "\n"

	for _, args := range [][]string{{}, {"serve"}} {
		name := "no arguments"
		if len(args) > 0 {
			name = strings.Join(args, " ")
		}
		t.Run(name, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Stdin = strings.NewReader(initialize)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			// The server exits when stdin closes, so Run returns on its own.
			if err := cmd.Run(); err != nil {
				t.Fatalf("server exited with error: %v\nstderr: %s", err, stderr.String())
			}

			line := strings.TrimSpace(strings.SplitN(stdout.String(), "\n", 2)[0])
			if line == "" {
				t.Fatalf("no MCP response on stdout; got stderr: %s", stderr.String())
			}
			var response struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  struct {
					ServerInfo struct {
						Name string `json:"name"`
					} `json:"serverInfo"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &response); err != nil {
				t.Fatalf("stdout is not an MCP JSON-RPC response: %v\ngot: %s", err, line)
			}
			if response.JSONRPC != "2.0" {
				t.Errorf("jsonrpc = %q, want 2.0", response.JSONRPC)
			}
			if response.Result.ServerInfo.Name != "godot-stagehand" {
				t.Errorf("serverInfo.name = %q, want godot-stagehand", response.Result.ServerInfo.Name)
			}
		})
	}
}

// TestSetupAndVersionSubcommandsStillDispatch guards the pre-existing
// subcommands against the new CLI dispatcher swallowing them.
func TestSetupAndVersionSubcommandsStillDispatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := dispatch(context.Background(), []string{"--version"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("--version exit = %d, want %d (stderr: %s)", code, cli.ExitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "godot-stagehand ") {
		t.Errorf("--version output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := dispatch(context.Background(), []string{"setup"}, &stdout, &stderr); code != cli.ExitUsage {
		t.Errorf("setup with no project exit = %d, want %d", code, cli.ExitUsage)
	}
}

// TestCLICommandsDispatchThroughMain checks the CLI is actually reachable from
// the real entry point, not merely importable.
func TestCLICommandsDispatchThroughMain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := dispatch(context.Background(), []string{"help"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("help exit = %d, want %d", code, cli.ExitOK)
	}
	if !strings.Contains(stdout.String(), "godot-stagehand run") {
		t.Errorf("help output does not document the scenario runner:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := dispatch(context.Background(), []string{"serve", "extra"}, &stdout, &stderr); code != cli.ExitUsage {
		t.Errorf("serve with extra arguments exit = %d, want %d", code, cli.ExitUsage)
	}
}
