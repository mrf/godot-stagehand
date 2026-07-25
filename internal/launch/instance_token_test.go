package launch

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyInstanceToken covers the spawned==connected proof: the token the
// addon echoes in its ping response must match the token we passed via env.
func TestVerifyInstanceToken(t *testing.T) {
	const want = "abc123"

	if err := verifyInstanceToken(want, want, "127.0.0.1", 26700); err != nil {
		t.Fatalf("matching tokens should pass, got error: %v", err)
	}

	// Mismatch: connected to a different instance that holds the port.
	err := verifyInstanceToken("someone-elses-token", want, "127.0.0.1", 26700)
	if err == nil {
		t.Fatal("expected error on token mismatch")
	}
	if !contains(err.Error(), "different") {
		t.Errorf("mismatch error should mention a different instance, got: %v", err)
	}
	if !contains(err.Error(), "26700") {
		t.Errorf("mismatch error should mention the port, got: %v", err)
	}
	// A token mismatch means we lost the port-assignment race to another
	// process; callers that auto-assigned the port need to detect this to
	// retry with a freshly picked one instead of failing the whole launch.
	if !errors.Is(err, ErrPortUnavailable) {
		t.Errorf("token mismatch should be identifiable as ErrPortUnavailable, got: %v", err)
	}

	// Empty echoed token (addon too old or different process) is also a failure:
	// we cannot prove we reached the process we spawned.
	if err := verifyInstanceToken("", want, "127.0.0.1", 26700); err == nil {
		t.Fatal("expected error when echoed token is empty")
	}
}

// TestLaunchRejectsOccupiedPort verifies that Launch fails fast with a clear,
// actionable error when something already holds the target port, instead of
// silently attaching to that pre-existing instance.
func TestLaunchRejectsOccupiedPort(t *testing.T) {
	// Occupy a port with a plain TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	tmpDir := t.TempDir()
	// A fake godot binary that should never even be started, because the
	// pre-dial occupancy check happens before cmd.Start().
	scriptPath := filepath.Join(tmpDir, "fake-godot")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.godot"), []byte("config_version=5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    scriptPath,
		Host:        "127.0.0.1",
		Port:        port,
		TimeoutMs:   2000,
	}
	_, err = Launch(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when launching against an occupied port")
	}
	if !contains(err.Error(), "already in use") {
		t.Errorf("expected an 'already in use' error, got: %v", err)
	}
	// This is the other half of the TOCTOU race: something already held the
	// port before we even spawned Godot. Callers that auto-assigned the port
	// need to retry with a freshly picked one instead of failing outright.
	if !errors.Is(err, ErrPortUnavailable) {
		t.Errorf("occupied-port error should be identifiable as ErrPortUnavailable, got: %v", err)
	}
}
