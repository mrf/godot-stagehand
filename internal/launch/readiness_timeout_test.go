package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// launchNeverReady launches a fake Godot named binName that records the auth
// token it was handed and then runs without ever binding its port, so the
// readiness wait must time out. It returns the recorded token, the port, and
// the launch error.
func launchNeverReady(t *testing.T, binName string) (string, int, error) {
	t.Helper()
	requirePOSIXFakeBinary(t)
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "auth-token")
	binPath := filepath.Join(tmpDir, binName)
	writeFakeGodot(t, binPath, `case "$*" in *--import*) exit 0;; esac
printf '%s' "$STAGEHAND_AUTH_TOKEN" > '`+tokenPath+`'
exec sleep 30
`)
	projectDir := filepath.Join(tmpDir, "project")
	writeMinimalProject(t, projectDir)
	port := freeTCPPort(t)

	_, err := Launch(context.Background(), Config{
		ProjectPath: projectDir,
		GodotBin:    binPath,
		Host:        "127.0.0.1",
		Port:        port,
		TimeoutMs:   1000,
	})
	if err == nil {
		t.Fatal("expected a readiness timeout")
	}
	token, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		t.Fatalf("fake Godot never recorded its auth token: %v", readErr)
	}
	return string(token), port, err
}

// TestLaunchReadinessTimeoutKeepsAuthToken: a game that binds after the
// readiness deadline must stay connectable, so the timeout error carries the
// token the game was started with instead of discarding it.
func TestLaunchReadinessTimeoutKeepsAuthToken(t *testing.T) {
	token, port, err := launchNeverReady(t, "godot.exe")

	var timeoutErr *ReadinessTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error %v should be a *ReadinessTimeoutError", err)
	}
	if token == "" || timeoutErr.AuthToken != token {
		t.Errorf("AuthToken = %q, want the token the game was started with (%q)", timeoutErr.AuthToken, token)
	}
	if timeoutErr.Port != port || timeoutErr.Host != "127.0.0.1" {
		t.Errorf("address = %s:%d, want 127.0.0.1:%d", timeoutErr.Host, timeoutErr.Port, port)
	}
	if timeoutErr.PID <= 0 {
		t.Errorf("PID = %d, want the spawned process id", timeoutErr.PID)
	}
	// Killing a Windows godot.exe's WSL interop process does not prove the
	// game itself is gone.
	if !timeoutErr.GameMayStillRun {
		t.Error("a Windows binary driven from a non-Windows host must report GameMayStillRun")
	}
	if !strings.Contains(err.Error(), "Godot failed to become ready") {
		t.Errorf("error %q lost its readiness-failure wording", err)
	}
	// Error() reaches logs and scenario reports; the token must not.
	if strings.Contains(err.Error(), token) {
		t.Errorf("error text %q leaks the auth token", err)
	}
}

// TestLaunchReadinessTimeoutConfirmedKill: a native binary whose kill was
// confirmed by its exit cannot come up late.
func TestLaunchReadinessTimeoutConfirmedKill(t *testing.T) {
	_, _, err := launchNeverReady(t, "fake-godot")

	var timeoutErr *ReadinessTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error %v should be a *ReadinessTimeoutError", err)
	}
	if timeoutErr.GameMayStillRun {
		t.Error("a native process confirmed exited after the kill must not report GameMayStillRun")
	}
}
