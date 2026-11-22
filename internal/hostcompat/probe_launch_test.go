package hostcompat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// probeArgv runs LaunchAndProbe against a fake Godot that records the game's
// argv and never binds, so the launch fails fast with a LaunchError.
func probeArgv(t *testing.T, envOnly bool) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Godot binary is a POSIX shell script")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "project.godot"), []byte("config_version=5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	argvPath := filepath.Join(t.TempDir(), "argv")
	binPath := filepath.Join(t.TempDir(), "fake-godot")
	script := "#!/bin/sh\ncase \"$*\" in *--import*) exit 0;; esac\n" +
		"printf '%s\\n' \"$@\" > '" + argvPath + "'\nexec sleep 30\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := LaunchAndProbe(context.Background(), LaunchConfig{
		ProjectPath:       root,
		GodotBin:          binPath,
		TimeoutMs:         1000,
		EnvOnlyActivation: envOnly,
	})
	var launchErr *LaunchError
	if !errors.As(err, &launchErr) {
		t.Fatalf("err = %v, want a LaunchError from the never-ready fake", err)
	}
	data, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("fake Godot never recorded its argv: %v", err)
	}
	return strings.Fields(string(data))
}

func TestLaunchAndProbePassesStagehandFlagByDefault(t *testing.T) {
	if argv := probeArgv(t, false); !slices.Contains(argv, "--stagehand") {
		t.Errorf("argv %q should carry --stagehand by default", argv)
	}
}

func TestLaunchAndProbeEnvOnlyActivationOmitsStagehandFlag(t *testing.T) {
	if argv := probeArgv(t, true); slices.Contains(argv, "--stagehand") {
		t.Errorf("argv %q must not carry --stagehand with EnvOnlyActivation", argv)
	}
}
