package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLaunchToolExposesQuitOnDisconnect pins the opt-out godot_launch was missing:
// the addon's own quit message names an environment switch, which an agent
// driving this tool could not otherwise reach.
func TestLaunchToolExposesQuitOnDisconnect(t *testing.T) {
	property, ok := launchTool.InputSchema.Properties["quit_on_disconnect"]
	if !ok {
		t.Fatal("godot_launch schema must expose quit_on_disconnect")
	}
	propertySchema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("godot_launch quit_on_disconnect schema has type %T, want map[string]any", property)
	}
	if got := propertySchema["default"]; got != true {
		t.Fatalf("godot_launch quit_on_disconnect default = %v, want true", got)
	}
}

// TestLaunchToolForwardsQuitOnDisconnectOptOut proves the parameter reaches the
// launched process, through a fake binary that records what it was given.
func TestLaunchToolForwardsQuitOnDisconnectOptOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Godot binary is a POSIX shell script")
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "default", args: map[string]any{}, want: "quit=[]"},
		{name: "opt-out", args: map[string]any{"quit_on_disconnect": false}, want: "quit=[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STAGEHAND_QUIT_ON_DISCONNECT", "")
			root := t.TempDir()
			projectPath := filepath.Join(root, "project")
			if err := os.Mkdir(projectPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(projectPath, "project.godot"), []byte("config_version=5\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			binPath := filepath.Join(root, "fake-godot")
			outPath := binPath + ".output"
			script := "#!/bin/sh\necho \"quit=[$STAGEHAND_QUIT_ON_DISCONNECT]\" >> \"" + outPath + "\"\n"
			if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

			args := map[string]any{
				"project_path": projectPath,
				"godot_bin":    binPath,
				"timeout_ms":   float64(1000),
			}
			for name, value := range tc.args {
				args[name] = value
			}
			if _, err := New().handleLaunch(context.Background(), toolReq(args)); err != nil {
				t.Fatalf("handleLaunch: %v", err)
			}

			output, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("fake Godot never ran: %v", err)
			}
			if !strings.Contains(string(output), tc.want) {
				t.Errorf("expected %q in %s", tc.want, output)
			}
		})
	}
}
