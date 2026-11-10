//go:build !windows

package main

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestSignalContextCancelsOnSIGTERM pins godot-stagehand-ext-review-2026-11-327t.11:
// the CLI path turns SIGINT/SIGTERM into context cancellation so the runner can
// run teardown and write its artifacts instead of the process dying outright.
func TestSignalContextCancelsOnSIGTERM(t *testing.T) {
	ctx, stop := signalContext(context.Background())
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill self: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context was not cancelled by SIGTERM")
	}
}
