package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestLaunchToolExposesImportTimeoutMs pins the knob godot_launch was missing:
// the import pre-pass has its own 120s default that timeout_ms never reached,
// so a slow first import (a UNC checkout under WSL) was killed every time.
func TestLaunchToolExposesImportTimeoutMs(t *testing.T) {
	property, ok := launchTool.InputSchema.Properties["import_timeout_ms"]
	if !ok {
		t.Fatal("godot_launch schema must expose import_timeout_ms")
	}
	propertySchema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("import_timeout_ms schema has type %T, want map[string]any", property)
	}
	if got := propertySchema["type"]; got != "number" {
		t.Fatalf("import_timeout_ms type = %v, want number", got)
	}
}

// TestLaunchToolForwardsImportTimeoutMs proves the parameter bounds the import
// pre-pass: a fake binary whose --import outlasts a short import_timeout_ms must
// be killed at that bound, long before the 120s default would fire.
func TestLaunchToolForwardsImportTimeoutMs(t *testing.T) {
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
	binPath := filepath.Join(root, "fake-godot")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	result, err := New().handleLaunch(context.Background(), toolReq(map[string]any{
		"project_path":      projectPath,
		"godot_bin":         binPath,
		"import_timeout_ms": float64(1000),
	}))
	if err != nil {
		t.Fatalf("handleLaunch: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("launch took %s; import_timeout_ms=1000 did not bound the import", elapsed)
	}
	if !result.IsError {
		t.Fatal("expected the import to be killed at import_timeout_ms")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "headless project import failed") {
		t.Errorf("error %q should report the failed import", text)
	}
}

// TestLaunchToolRejectsBadImportTimeoutMs keeps the new knob under the same
// range check as timeout_ms.
func TestLaunchToolRejectsBadImportTimeoutMs(t *testing.T) {
	for _, v := range []float64{0, -1, 999, 1.5} {
		result, err := New().handleLaunch(context.Background(), toolReq(map[string]any{
			"project_path":      t.TempDir(),
			"import_timeout_ms": v,
		}))
		if err != nil {
			t.Fatalf("handleLaunch(%v): %v", v, err)
		}
		if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "import_timeout_ms") {
			t.Errorf("import_timeout_ms=%v: want a parameter error naming import_timeout_ms, got %+v", v, result)
		}
	}
}
