//go:build integration
// +build integration

package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/godotconn"
)

const godotStartupTimeout = 30 * time.Second

func TestIntegrationLaunchHeadless(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Godot integration test in short mode")
	}

	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	projectRoot := findProjectRoot(t)
	projectDir := prepareGodotTestProject(t, projectRoot)
	port := freeTCPPort(t)

	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    true,
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	}

	ctx := context.Background()
	result, err := Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	defer result.Kill()
	defer result.Conn.Close()

	// Verify connection via ping.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	pingResp, err := result.Conn.Call(pingCtx, "ping", nil)
	if err != nil {
		t.Fatalf("ping call failed: %v", err)
	}
	if pingResp.Error != nil {
		t.Fatalf("ping returned error: %v", pingResp.Error)
	}
	var ping struct {
		Status           string `json:"status"`
		Engine           string `json:"engine"`
		EngineVersion    string `json:"engine_version"`
		StagehandVersion string `json:"stagehand_version"`
	}
	if err := json.Unmarshal(pingResp.Result, &ping); err != nil {
		t.Fatalf("unmarshal ping result: %v; raw=%s", err, pingResp.Result)
	}
	if ping.Status != "ok" || ping.Engine != "godot" {
		t.Fatalf("unexpected ping response: %+v", ping)
	}
	if ping.EngineVersion == "" || ping.StagehandVersion == "" {
		t.Fatalf("ping response missing version fields: %+v", ping)
	}
	// Ensure launch result fields match.
	if result.EngineVersion != ping.EngineVersion {
		t.Errorf("EngineVersion mismatch: got %q, want %q", result.EngineVersion, ping.EngineVersion)
	}
	if result.StagehandVersion != ping.StagehandVersion {
		t.Errorf("StagehandVersion mismatch: got %q, want %q", result.StagehandVersion, ping.StagehandVersion)
	}
	if result.Port != port {
		t.Errorf("Port mismatch: got %d, want %d", result.Port, port)
	}
	if result.Host != "127.0.0.1" {
		t.Errorf("Host mismatch: got %q, want %q", result.Host, "127.0.0.1")
	}
	// Process should still be running.
	if result.Process.Process == nil {
		t.Fatal("Process missing")
	}
}

// Helper functions copied from internal/godotconn/godot_integration_test.go

func findProjectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", file)
		}
		dir = parent
	}
}

func prepareGodotTestProject(t *testing.T, projectRoot string) string {
	t.Helper()
	dst := t.TempDir()
	copyDir(t, filepath.Join(projectRoot, "testdata", "test_project"), dst)
	copyDir(t, filepath.Join(projectRoot, "addons", "stagehand"), filepath.Join(dst, "addons", "stagehand"))
	return dst
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read dir %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyDir(t, srcPath, dstPath)
			continue
		}
		copyFile(t, srcPath, dstPath)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}