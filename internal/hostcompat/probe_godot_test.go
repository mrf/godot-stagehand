//go:build godot

package hostcompat

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mrf/godot-stagehand/internal/launch"
)

// These are the real-engine proof for the capability probe: it is launched
// against testdata/test_project, plain and in two variants that change the
// host at runtime only (the test scene reads user args; project.godot is
// untouched). That is the point — a declarative read of project.godot cannot
// see either variant, which is how content-scale was missed on Pixelorama.
//
//	GODOT_BIN=$HOME/.local/bin/godot-4.6.2-linux go test -tags=godot ./internal/hostcompat/

func probeTestProject(t *testing.T, extraArgs ...string) *Probe {
	t.Helper()
	godotBin, err := launch.FindGodotBinary()
	if err != nil || godotBin == "" {
		t.Fatalf("no Godot binary: set GODOT_BIN before running -tags=godot (err=%v)", err)
	}
	project, err := filepath.Abs(filepath.Join("..", "..", "testdata", "test_project"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	got, err := LaunchAndProbe(ctx, LaunchConfig{
		ProjectPath: project,
		GodotBin:    godotBin,
		ExtraArgs:   extraArgs,
		TimeoutMs:   180_000,
	})
	if err != nil {
		t.Fatalf("LaunchAndProbe: %v", err)
	}
	t.Logf("probe: %+v", *got)
	return got
}

func TestProbeTestProjectPlain(t *testing.T) {
	got := probeTestProject(t)

	if got.Engine == "" || got.Addon == "" {
		t.Errorf("engine/addon not reported: %q/%q", got.Engine, got.Addon)
	}
	// test_project's project.godot sets no stretch mode and nothing scales it
	// at runtime: the identity configuration.
	if got.ContentScale.Factor != 1 || got.ContentScale.Mode != "disabled" {
		t.Errorf("content scale = %+v, want factor 1, mode disabled", got.ContentScale)
	}
	// The only autoload is StagehandServer, which the probe must not count.
	if got.AutoloadCount != 0 || len(got.Autoloads) != 0 {
		t.Errorf("autoloads = %v (%d), want none besides Stagehand", got.Autoloads, got.AutoloadCount)
	}
	if len(got.Windows) != 0 || got.EmbeddedSubwindows {
		t.Errorf("windows = %+v embedded=%v, want none", got.Windows, got.EmbeddedSubwindows)
	}
	if got.PausedAtBoot == nil || *got.PausedAtBoot {
		t.Errorf("paused_at_boot = %v, want false", got.PausedAtBoot)
	}
	if got.NodeCount < 3 {
		t.Errorf("node_count = %d, want at least root, StagehandServer and the scene", got.NodeCount)
	}
	if got.TimeToReadyMs <= 0 {
		t.Errorf("time_to_ready_ms = %d, want > 0", got.TimeToReadyMs)
	}
	if len(got.Axes) != 0 {
		t.Errorf("axes = %v, want none for the plain fixture", got.Axes)
	}
}

func TestProbeTestProjectPausedAtBoot(t *testing.T) {
	got := probeTestProject(t, "--pause-at-boot")
	if got.PausedAtBoot == nil || !*got.PausedAtBoot {
		t.Fatalf("paused_at_boot = %v, want true", got.PausedAtBoot)
	}
	if !slices.Equal(got.Axes, []string{AxisPausedTree}) {
		t.Errorf("axes = %v, want [paused-tree]", got.Axes)
	}
}

func TestProbeTestProjectContentScaleSetAtRuntime(t *testing.T) {
	got := probeTestProject(t, "--content-scale-factor=1.5")
	if got.ContentScale.Factor != 1.5 {
		t.Fatalf("content scale factor = %v, want 1.5 (set by script; project.godot shows nothing)", got.ContentScale.Factor)
	}
	if !slices.Equal(got.Axes, []string{AxisContentScale}) {
		t.Errorf("axes = %v, want [content-scale]", got.Axes)
	}
}
