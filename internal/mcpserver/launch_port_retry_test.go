package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// TestLaunchWithAutoPortRetriesOnPortCollision reproduces the TOCTOU race
// described in the port-picker: findFreePort's probe listener closes before
// Godot actually binds, so another process can grab the same port in that
// window. launch.Launch surfaces that as launch.ErrPortUnavailable; a caller
// that auto-assigned the port should retry with a freshly picked one instead
// of failing the whole launch on a one-shot loss of the race.
func TestLaunchWithAutoPortRetriesOnPortCollision(t *testing.T) {
	var pickedPorts []int
	pickPort := func() (int, error) {
		port := 30000 + len(pickedPorts)
		pickedPorts = append(pickedPorts, port)
		return port, nil
	}

	var attemptedPorts []int
	doLaunch := func(_ context.Context, cfg launch.Config) (*launch.LaunchResult, error) {
		attemptedPorts = append(attemptedPorts, cfg.Port)
		if len(attemptedPorts) < 3 {
			return nil, fmt.Errorf("bind collision: %w", launch.ErrPortUnavailable)
		}
		return &launch.LaunchResult{Port: cfg.Port}, nil
	}

	result, err := launchWithAutoPort(context.Background(), launch.Config{}, pickPort, doLaunch)
	if err != nil {
		t.Fatalf("expected success on the 3rd attempt, got error: %v", err)
	}
	if result.Port != 30002 {
		t.Errorf("expected the successful launch to use the 3rd picked port 30002, got %d", result.Port)
	}
	if len(attemptedPorts) != 3 {
		t.Errorf("expected 3 launch attempts, got %d: %v", len(attemptedPorts), attemptedPorts)
	}
	if len(pickedPorts) != 3 {
		t.Errorf("expected 3 distinct ports to be picked (never reusing the collided port), got %d: %v", len(pickedPorts), pickedPorts)
	}
}

// TestLaunchWithAutoPortGivesUpAfterMaxAttempts pins the "bounded" half of the
// fix: a port that keeps colliding must not loop forever, it must fail with a
// clean, actionable error after a fixed number of attempts.
func TestLaunchWithAutoPortGivesUpAfterMaxAttempts(t *testing.T) {
	attempts := 0
	pickPort := func() (int, error) { return 40000 + attempts, nil }
	doLaunch := func(_ context.Context, _ launch.Config) (*launch.LaunchResult, error) {
		attempts++
		return nil, fmt.Errorf("bind collision: %w", launch.ErrPortUnavailable)
	}

	_, err := launchWithAutoPort(context.Background(), launch.Config{}, pickPort, doLaunch)
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if attempts != maxAutoPortAttempts {
		t.Errorf("expected exactly %d attempts, got %d", maxAutoPortAttempts, attempts)
	}
	if !errors.Is(err, launch.ErrPortUnavailable) {
		t.Errorf("expected the final error to still wrap launch.ErrPortUnavailable, got: %v", err)
	}
}

// TestLaunchWithAutoPortDoesNotRetryUnrelatedErrors ensures a genuinely broken
// launch (bad project path, missing binary, etc.) fails clean on the first
// attempt instead of being masked by the port-collision retry loop.
func TestLaunchWithAutoPortDoesNotRetryUnrelatedErrors(t *testing.T) {
	calls := 0
	pickPort := func() (int, error) { return 50000, nil }
	wantErr := errors.New("project_path is required")
	doLaunch := func(_ context.Context, _ launch.Config) (*launch.LaunchResult, error) {
		calls++
		return nil, wantErr
	}

	_, err := launchWithAutoPort(context.Background(), launch.Config{}, pickPort, doLaunch)
	if !errors.Is(err, wantErr) {
		t.Errorf("expected the unrelated error to propagate unchanged, got: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected no retry for a non-port error, got %d calls", calls)
	}
}
