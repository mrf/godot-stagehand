package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// launchedArgv runs godot_launch against a fake Godot that records the argv of
// the game process (not the import pass) and never binds its port.
func launchedArgv(t *testing.T, extra map[string]any) []string {
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
	argvPath := filepath.Join(root, "argv")
	binPath := filepath.Join(root, "fake-godot")
	script := "#!/bin/sh\ncase \"$*\" in *--import*) exit 0;; esac\n" +
		"printf '%s\\n' \"$@\" > '" + argvPath + "'\nexec sleep 30\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"project_path": projectPath,
		"godot_bin":    binPath,
		"timeout_ms":   float64(1000),
	}
	for k, v := range extra {
		args[k] = v
	}
	if _, err := New().handleLaunch(context.Background(), toolReq(args)); err != nil {
		t.Fatalf("handleLaunch: %v", err)
	}
	data, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("fake Godot never recorded its argv: %v", err)
	}
	return strings.Fields(string(data))
}

func TestLaunchPassesStagehandFlagByDefault(t *testing.T) {
	if argv := launchedArgv(t, nil); !slices.Contains(argv, "--stagehand") {
		t.Errorf("argv %q should carry --stagehand by default", argv)
	}
}

// An own-cli-parser host rejects --stagehand, so env_only_activation must drop it.
func TestLaunchEnvOnlyActivationOmitsStagehandFlag(t *testing.T) {
	argv := launchedArgv(t, map[string]any{"env_only_activation": true})
	if slices.Contains(argv, "--stagehand") {
		t.Errorf("argv %q must not carry --stagehand with env_only_activation=true", argv)
	}
}

func TestLaunchSchemaExposesEnvOnlyActivation(t *testing.T) {
	property, ok := launchTool.InputSchema.Properties["env_only_activation"].(map[string]any)
	if !ok {
		t.Fatal("godot_launch schema must expose env_only_activation")
	}
	if got := property["default"]; got != false {
		t.Errorf("env_only_activation default = %v, want false", got)
	}
}
