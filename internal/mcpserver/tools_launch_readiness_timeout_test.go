package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// launchNeverReady runs godot_launch against a fake Godot named binName that
// records its auth token and never binds its port. It returns the error text
// and the recorded token.
func launchNeverReady(t *testing.T, binName string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Godot binary is a POSIX shell script")
	}
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	if err := os.Mkdir(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "project.godot"), []byte("config_version=5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "auth-token")
	binPath := filepath.Join(root, binName)
	script := "#!/bin/sh\ncase \"$*\" in *--import*) exit 0;; esac\n" +
		"printf '%s' \"$STAGEHAND_AUTH_TOKEN\" > '" + tokenPath + "'\nexec sleep 30\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := New().handleLaunch(context.Background(), toolReq(map[string]any{
		"project_path": projectPath,
		"godot_bin":    binPath,
		"timeout_ms":   float64(1000),
	}))
	if err != nil {
		t.Fatalf("handleLaunch: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected the readiness wait to time out")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("fake Godot never recorded its auth token: %v", err)
	}
	return result.Content[0].(mcp.TextContent).Text, string(token)
}

// TestLaunchTimeoutSurfacesTokenForLateGame: a Windows godot.exe launched from
// WSL can outlive the kill and bind after the deadline; the caller must get the
// token to attach to it instead of being left with an unconnectable game.
func TestLaunchTimeoutSurfacesTokenForLateGame(t *testing.T) {
	text, token := launchNeverReady(t, "godot.exe")
	for _, want := range []string{"godot_connect", "auth_token", token, "timeout_ms"} {
		if !strings.Contains(text, want) {
			t.Errorf("timeout error %q should mention %q", text, want)
		}
	}
}

// TestLaunchTimeoutWithConfirmedKillOmitsToken: a game known dead cannot be
// attached to, so its token is neither useful nor shown.
func TestLaunchTimeoutWithConfirmedKillOmitsToken(t *testing.T) {
	text, token := launchNeverReady(t, "fake-godot")
	if strings.Contains(text, token) {
		t.Errorf("timeout error %q exposes the token of a game that was confirmed killed", text)
	}
	if !strings.Contains(text, "timeout_ms") {
		t.Errorf("timeout error %q should suggest raising timeout_ms", text)
	}
}
