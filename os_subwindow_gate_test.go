//go:build gdscript
// +build gdscript

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A project with display/window/subwindows/embed_subwindows = false gets real
// operating-system windows for its dialogs, and Stagehand cannot drive them:
// Godot never establishes such a window's GUI mouse-over state from a
// synthesized event, so a pushed click reaches Control._gui_input but never
// makes a BaseButton fire. StagehandInputSimulator refuses that target with a
// typed error naming the project setting instead of reporting a phantom
// success (godot-stagehand-inpw).
//
// testdata/test_project/test/test_input_simulator_os_subwindow.gd covers the
// code path with a hidden non-embedded Window, because popping a visible one
// permanently breaks Input.parse_input_event for the rest of the process and
// would take the shared GdUnit runner down with it. This test is the other
// half: it pops a real, visible OS window in a throwaway process, so the
// behaviour is checked against an actual window rather than an approximation.
//
// Build-tagged `gdscript` alongside TestGdUnitSuite, and needs GODOT_BIN:
//
//	GODOT_BIN=/path/to/godot go test -tags=gdscript -run TestOsSubwindow .
const osSubwindowProbe = "os_subwindow_probe.gd"

// osProbeTimeout bounds a run that has no display server to fail against — a
// hang here is a broken environment, not a slow test.
const osProbeTimeout = 90 * time.Second

// TestOsSubwindowInputIsUnreachableHeadless runs the probe under --headless,
// which every CI machine can do. Headless still produces a genuinely
// non-embedded Window (Window.is_embedded() is false), so the refusal and the
// window-geometry guarantees are both exercised.
func TestOsSubwindowInputIsUnreachableHeadless(t *testing.T) {
	runOsSubwindowProbe(t, true)
}

// TestOsSubwindowInputIsUnreachableWindowed runs the same probe against a real
// display server, which is the only way to observe an actual OS-level window.
// Skipped where there is no display — deliberately, because a headless-only
// result must not be reported as a windowed one.
func TestOsSubwindowInputIsUnreachableWindowed(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no DISPLAY or WAYLAND_DISPLAY; cannot open a real OS window")
	}
	runOsSubwindowProbe(t, false)
}

func runOsSubwindowProbe(t *testing.T, headless bool) {
	t.Helper()

	godotBin := os.Getenv("GODOT_BIN")
	if godotBin == "" {
		t.Skip("GODOT_BIN not set; skipping OS-subwindow probe")
	}

	root := repoRoot(t)
	project := filepath.Join(root, "testdata", "test_project")
	if _, err := os.Stat(filepath.Join(project, osSubwindowProbe)); err != nil {
		t.Fatalf("probe script not found: %v", err)
	}

	args := []string{}
	if headless {
		args = append(args, "--headless")
	}
	args = append(args, "--path", project, "-s", osSubwindowProbe)

	cmd := exec.Command(godotBin, args...)
	cmd.Dir = project
	output, runErr := combinedOutputWithin(cmd, osProbeTimeout)

	observations := parseOsProbeOutput(output)
	if len(observations) == 0 {
		t.Fatalf("probe emitted no OSWIN lines (err: %v)\n%s", runErr, tail(output))
	}

	// The premise: a display server that embedded the dialog anyway would make
	// every assertion below a statement about the embedded path.
	if got := observations["embedded"]; got != "false" {
		t.Fatalf("probe did not produce a non-embedded window: embedded=%q (display_server=%q)",
			got, observations["display_server"])
	}

	// Raw engine facts. If a future Godot starts delivering here, these fail and
	// the refusal below should be reconsidered rather than kept out of habit.
	assertOsProbe(t, observations, "hovered_control", "<Object#null>")
	assertOsProbe(t, observations, "button_presses", "0")

	// Stagehand's answer: a typed refusal that names the window and the setting,
	// and does not blame a non-existent overlay.
	assertOsProbe(t, observations, "click_success", "false")
	assertOsProbe(t, observations, "click_error_code", "not_supported")
	assertOsProbe(t, observations, "click_names_window", "true")
	assertOsProbe(t, observations, "click_blames_overlay", "false")
	assertOsProbe(t, observations, "click_next_action_names_setting", "true")
	assertOsProbe(t, observations, "move_success", "false")
	assertOsProbe(t, observations, "move_error_code", "not_supported")

	// The headless root-window size correction must never reach a subwindow: it
	// used to resize the application's own dialog to the project resolution.
	assertOsProbe(t, observations, "window_size_unchanged", "true")

	assertOsProbe(t, observations, "result", "ok")

	if runErr != nil {
		t.Fatalf("probe exited non-zero: %v\n%s", runErr, tail(output))
	}
	t.Logf("OS-subwindow probe clean on display_server=%s (window %s)",
		observations["display_server"], observations["window_size"])
}

func assertOsProbe(t *testing.T, observations map[string]string, key, want string) {
	t.Helper()

	got, ok := observations[key]
	if !ok {
		t.Errorf("probe did not report %q", key)
		return
	}
	if got != want {
		t.Errorf("probe %s = %q, want %q", key, got, want)
	}
}

// parseOsProbeOutput collects the `OSWIN <key>=<value>` lines the probe emits.
// A repeated key keeps the last value, matching the probe's own ordering.
func parseOsProbeOutput(output string) map[string]string {
	observations := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(stripANSI(scanner.Text()))
		rest, found := strings.CutPrefix(line, "OSWIN ")
		if !found {
			continue
		}
		key, value, found := strings.Cut(rest, "=")
		if !found {
			continue
		}
		observations[key] = value
	}
	return observations
}

// combinedOutputWithin runs cmd and kills it if it outlives limit, returning
// whatever it printed either way. A Godot that cannot reach its display server
// can sit forever, and an empty timeout failure is much harder to read than the
// partial output.
func combinedOutputWithin(cmd *exec.Cmd, limit time.Duration) (string, error) {
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return buf.String(), err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return buf.String(), err
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-done
		return buf.String(), os.ErrDeadlineExceeded
	}
}
