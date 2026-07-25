//go:build godot

package launch

import (
	"context"
	"errors"
	"testing"
)

// TestVerifyInstanceTokenAgainstTwoRealLaunches is the end-to-end version of
// TestVerifyInstanceToken: instead of hand-picked strings, it launches two
// real Godot processes and checks their actual per-launch tokens (as echoed
// by the real addon's ping handshake) against each other. Fills M6's
// "token-mismatch end-to-end" gap by proving the real addon echoes a distinct
// token per instance and that verifyInstanceToken correctly rejects a token
// that belongs to a different live instance.
func TestVerifyInstanceTokenAgainstTwoRealLaunches(t *testing.T) {
	godotBin := requireGodotBinary(t)
	root := findProjectRoot(t)

	launchOne := func() *LaunchResult {
		t.Helper()
		projectDir := prepareGodotTestProject(t, root)
		ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
		defer cancel()
		result, err := Launch(ctx, Config{
			ProjectPath: projectDir,
			GodotBin:    godotBin,
			Host:        "127.0.0.1",
			Port:        freeTCPPort(t),
			Headless:    true,
			TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
		})
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		t.Cleanup(func() { _ = result.Kill() })
		return result
	}

	a := launchOne()
	b := launchOne()

	if a.Handshake == nil || b.Handshake == nil {
		t.Fatal("expected both real launches to negotiate a handshake")
	}
	tokenA := a.Handshake.InstanceToken
	tokenB := b.Handshake.InstanceToken
	if tokenA == "" || tokenB == "" {
		t.Fatal("expected non-empty instance tokens from both real launches")
	}
	if tokenA == tokenB {
		t.Fatal("two independent real launches produced the same instance token")
	}

	// Each instance's own real token against its own host:port must verify
	// cleanly -- a successful Launch() already proved this internally; this
	// re-asserts it with the real values in hand.
	if err := verifyInstanceToken(tokenA, tokenA, a.Host, a.Port); err != nil {
		t.Errorf("instance A's own real token failed to verify against itself: %v", err)
	}
	if err := verifyInstanceToken(tokenB, tokenB, b.Host, b.Port); err != nil {
		t.Errorf("instance B's own real token failed to verify against itself: %v", err)
	}

	// Cross-wiring: pretend we launched expecting A's token but the real ping
	// response we got back carries B's token (as if we'd reached B's process
	// instead of the one we spawned). Must be rejected.
	err := verifyInstanceToken(tokenB, tokenA, a.Host, a.Port)
	if err == nil {
		t.Fatal("expected verifyInstanceToken to reject B's real token against A's expected token")
	}
	if !errors.Is(err, ErrPortUnavailable) {
		t.Errorf("cross-instance mismatch should be identifiable as ErrPortUnavailable, got: %v", err)
	}
}
