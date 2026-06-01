package launch

// Regression proof for godot-stagehand-phase3-vrj.19:
// godot_click with a selector must land on its target even when the window size
// differs from the content-scale (render/viewport) size — i.e. under
// content-scale stretch.
//
// Node rects are reported in viewport/content-scale coordinates, but synthetic
// input is delivered in window/display coordinates. When window.size differs
// from content_scale_size, a click computed in canvas space lands off-target by
// the stretch ratio — and the addon previously still reported success:true.
//
// Headless Godot inherently runs with window.size != content_scale_size, so
// loading the content-scale-stretch fixture exercises the mismatch directly;
// no visible display is required. The fixture's toggle Button starts un-pressed;
// we click it by selector and assert button_pressed flips to true. With the bug
// present the click misses, the toggle never fires, and the assertion fails.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestContentScaleSelectorClickLandsOnTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping content-scale click regression test in short mode")
	}

	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	projectRoot := findProjectRoot(t)
	projectDir := prepareFixtureProject(t, projectRoot, filepath.Join("testdata", "contentscale_fixture"))
	port := freeTCPPort(t)

	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    true, // headless gives window.size != content_scale_size for free
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	}

	ctx := context.Background()
	result, err := Launch(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to launch headless Godot fixture: %v", err)
	}
	defer result.Kill()
	defer result.Conn.Close()

	callCtx, cancel := context.WithTimeout(ctx, godotStartupTimeout)
	defer cancel()

	const selector = "class:Button"

	// Precondition: the toggle button starts un-pressed.
	if pressed := getButtonPressed(callCtx, t, result, selector); pressed {
		t.Fatalf("precondition failed: button_pressed should start false, got true")
	}

	// Click the button by selector. A short hold keeps press+release close
	// together; BaseButton fires its toggle on the release.
	clickResp, err := result.Conn.Call(callCtx, "input_mouse", map[string]any{
		"selector": selector,
		"button":   "left",
		"hold_ms":  50,
	})
	if err != nil {
		t.Fatalf("input_mouse call failed: %v", err)
	}
	var clickResult struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(clickResp.Result, &clickResult); err != nil {
		t.Fatalf("unmarshal click result: %v; raw=%s", err, clickResp.Result)
	}
	if !clickResult.Success {
		t.Fatalf("input_mouse reported failure: %s (raw=%s)", clickResult.Error, clickResp.Result)
	}

	// The release (and thus the toggle) is scheduled via a SceneTreeTimer after
	// hold_ms, so the effect is not instantaneous — poll until it lands.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if getButtonPressed(callCtx, t, result, selector) {
			return // selector click landed on the button — fix verified
		}
		if time.Now().After(deadline) {
			t.Fatalf("button never toggled: selector click missed its target under " +
				"content-scale stretch (regression godot-stagehand-phase3-vrj.19)")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// getButtonPressed reads the bool "button_pressed" property of the node matched
// by selector through the live stagehand addon.
func getButtonPressed(ctx context.Context, t *testing.T, result *LaunchResult, selector string) bool {
	t.Helper()
	resp, err := result.Conn.Call(ctx, "get_property", map[string]any{
		"selector": selector,
		"property": "button_pressed",
	})
	if err != nil {
		t.Fatalf("get_property(button_pressed) failed: %v", err)
	}
	var pr struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("unmarshal get_property result: %v; raw=%s", err, resp.Result)
	}
	return pr.Value
}
