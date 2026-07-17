//go:build godot

package launch

import (
	"context"
	"errors"
	"testing"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

func TestLaunchEstablishesAuthenticatedSafeConnection(t *testing.T) {
	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatalf("find Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; the godot build tag requires a Godot-equipped environment")
	}

	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	port := freeTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()
	result, err := Launch(ctx, Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    true,
		AllowUnsafe: false,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer result.Kill()

	if _, err := result.Conn.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("authenticated launch connection ping: %v", err)
	}
	_, err = result.Conn.Call(ctx, "evaluate", map[string]any{"expression": "1 + 1"})
	requireLaunchRPCCode(t, err, godotconn.CodeUnsafeCapability)

	unauthenticated, err := godotconn.Dial(ctx, "127.0.0.1", port)
	if err != nil {
		t.Fatalf("dial second connection: %v", err)
	}
	defer unauthenticated.Close()
	_, err = unauthenticated.Call(ctx, "set_property", map[string]any{
		"selector": "/root/TestScene/PropertyTarget",
		"property": "flag_prop",
		"value":    false,
	})
	requireLaunchRPCCode(t, err, godotconn.CodeAuthenticationRequired)
}

func TestLaunchCanExplicitlyEnableUnsafeMethods(t *testing.T) {
	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatalf("find Godot: %v", err)
	}
	if godotBin == "" {
		t.Fatal("Godot binary not found; the godot build tag requires a Godot-equipped environment")
	}

	projectDir := prepareGodotTestProject(t, findProjectRoot(t))
	ctx, cancel := context.WithTimeout(context.Background(), godotStartupTimeout)
	defer cancel()
	result, err := Launch(ctx, Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        freeTCPPort(t),
		Headless:    true,
		AllowUnsafe: true,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer result.Kill()

	if _, err := result.Conn.Call(ctx, "evaluate", map[string]any{"expression": "1 + 1"}); err != nil {
		t.Fatalf("evaluate with explicit unsafe opt-in: %v", err)
	}
}

func requireLaunchRPCCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected RPC error code %d, got nil", want)
	}
	var rpcError *godotconn.RPCError
	if !errors.As(err, &rpcError) {
		t.Fatalf("expected RPCError code %d, got %T: %v", want, err, err)
	}
	if rpcError.Code != want {
		t.Fatalf("RPC error code = %d, want %d", rpcError.Code, want)
	}
}
