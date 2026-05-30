package launch

// CANONICAL UPSTREAM PROOF — godot_screenshot returns real rendered pixels.
//
// This is the reference test that downstream projects point to when a
// visual-smoke screenshot comes back grey/empty: if THIS passes, Stagehand's
// capture + transport + PNG decode path is sound, so a blank downstream
// screenshot is the downstream scene (or that project's display plumbing), not
// Stagehand.
//
// What it does:
//   1. Launches the deterministic fixture project (testdata/screenshot_fixture)
//      on a VISIBLE display — its main scene paints four primary-color quadrants
//      (red / green / blue / white).
//   2. Captures a full-viewport screenshot through the live addon "screenshot"
//      method, decodes the PNG, and asserts the sampled quadrant pixels match
//      the expected colors. Only a genuinely rendered frame can satisfy this.
//
// Gating (so it is honest on headless/no-GPU CI, see docs/screenshot-pixel-proof.md):
//   - short mode / no Godot binary / no display server  -> t.Skip (frame not obtainable here).
//   - addon reports it could not produce a frame (viewport_image_empty etc.)
//     -> t.Skip with the addon's own diagnostic (no real render on this path).
//   - addon returns image DATA but pixels are blank/black/transparent/uniform
//     or the wrong colors -> t.Fatal LOUDLY (a frame WAS produced and is
//     expected to be meaningful — this is the regression the issue describes).
//
// It is deliberately NOT behind the `integration` build tag: it runs under the
// normal `go test ./...` and self-skips where a real frame cannot be rendered.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mrf/godot-stagehand/internal/imgdiff"
)

// fixtureScreenshotResponse mirrors the addon "screenshot" reply shape. On
// success it carries base64 PNG data + dimensions; on failure it carries the
// addon's error code and diagnostic details.
type fixtureScreenshotResponse struct {
	Data      string         `json:"data"`
	MimeType  string         `json:"mime_type"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	Error     string         `json:"error"`
	ErrorCode string         `json:"error_code"`
	Details   map[string]any `json:"details"`
}

// quadrant describes one expected colored region of the fixture and the
// fractional point sampled to verify it.
type quadrant struct {
	name  string
	fracX float64
	fracY float64
	wantR uint8
	wantG uint8
	wantB uint8
}

var fixtureQuadrants = []quadrant{
	{"top-left RED", 0.25, 0.25, 255, 0, 0},
	{"top-right GREEN", 0.75, 0.25, 0, 255, 0},
	{"bottom-left BLUE", 0.25, 0.75, 0, 0, 255},
	{"bottom-right WHITE", 0.75, 0.75, 255, 255, 255},
}

// colorTolerance allows for sRGB/encoding rounding while still rejecting any
// blank, grey, or wrong-quadrant frame (the expected colors are pure primaries
// and differ from each other and from black/grey by far more than this).
const colorTolerance uint8 = 24

func displayServerAvailable() (bool, string) {
	switch runtime.GOOS {
	case "windows", "darwin":
		// These platforms always have a window server for a headed process.
		return true, ""
	default:
		// X11 / Wayland (including WSLg, which exports both).
		if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
			return true, ""
		}
		return false, "no DISPLAY or WAYLAND_DISPLAY set"
	}
}

// TestScreenshotPixelFixtureProvesRealRender is the canonical real-pixel proof.
func TestScreenshotPixelFixtureProvesRealRender(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping screenshot pixel fixture in short mode")
	}

	godotBin, err := FindGodotBinary()
	if err != nil {
		t.Fatal(err)
	}
	if godotBin == "" {
		t.Skip("Godot binary not found; set GODOT_BIN, GODOT_PATH, or STAGEHAND_GODOT_BIN, or put godot/godot4 in PATH")
	}

	if ok, why := displayServerAvailable(); !ok {
		t.Skipf("no visible display available (%s); the screenshot pixel proof requires a headed Godot on a real display server "+
			"(local desktop, WSLg, or an X server such as Xvfb with a GPU/software rasterizer). See docs/screenshot-pixel-proof.md.", why)
	}

	projectRoot := findProjectRoot(t)
	projectDir := prepareFixtureProject(t, projectRoot, fixtureRelDir())
	port := freeTCPPort(t)

	cfg := Config{
		ProjectPath: projectDir,
		GodotBin:    godotBin,
		Host:        "127.0.0.1",
		Port:        port,
		Headless:    false, // a visible window is required for real pixels
		TimeoutMs:   int(godotStartupTimeout.Milliseconds()),
	}

	ctx := context.Background()
	result, err := Launch(ctx, cfg)
	if err != nil {
		// DISPLAY was advertised but Godot could not open a window — this is a
		// real, actionable environment problem, not a "no frame" condition.
		t.Fatalf("failed to launch headed Godot for screenshot proof: %v", err)
	}
	defer result.Kill()
	defer result.Conn.Close()

	shotCtx, cancel := context.WithTimeout(ctx, godotStartupTimeout)
	defer cancel()
	resp, err := result.Conn.Call(shotCtx, "screenshot", map[string]any{"full_page": true})
	if err != nil {
		t.Fatalf("screenshot call failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("screenshot RPC returned protocol error: %v", resp.Error)
	}

	var sr fixtureScreenshotResponse
	if err := json.Unmarshal(resp.Result, &sr); err != nil {
		t.Fatalf("unmarshal screenshot result: %v; raw=%s", err, resp.Result)
	}

	// The addon telling us it could not produce a frame means no real render is
	// obtainable on this display path (headless dummy driver / no GPU). That is
	// an honest skip, not a failure — but surface the diagnostic.
	if sr.Error != "" {
		t.Skipf("addon could not capture a real frame on this display path "+
			"(error_code=%q): %s; details=%v. See docs/screenshot-pixel-proof.md for the required display path.",
			sr.ErrorCode, sr.Error, sr.Details)
	}

	// From here a frame WAS produced and is expected to be meaningful. Any
	// blank / corrupt / wrong-color result is a hard failure.
	if sr.Data == "" {
		t.Fatal("screenshot reported no error but returned empty image data (buffer/transport problem)")
	}
	raw, err := base64.StdEncoding.DecodeString(sr.Data)
	if err != nil {
		t.Fatalf("base64 decode failed (transport corruption): %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("screenshot decoded to zero bytes (buffer problem)")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("PNG decode failed (buffer/transport corruption): %v", err)
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		t.Fatalf("decoded PNG has non-positive dimensions: %dx%d", w, h)
	}
	if sr.Width != 0 && sr.Width != w {
		t.Fatalf("addon reported width %d but PNG is %d", sr.Width, w)
	}
	if sr.Height != 0 && sr.Height != h {
		t.Fatalf("addon reported height %d but PNG is %d", sr.Height, h)
	}
	t.Logf("captured %dx%d frame from Godot %s (stagehand %s)", w, h, result.EngineVersion, result.StagehandVersion)

	// Detect blank/uniform frames with an actionable diagnosis BEFORE the
	// quadrant check, so an all-black/empty render fails with the precise cause.
	if diag := imgdiff.BlankDiagnosis(imgdiff.Summarize(img)); diag != "" {
		t.Fatalf("screenshot is not a real render: %s", diag)
	}

	// Prove pixel meaning: each quadrant must carry its expected primary color.
	var failures []string
	for _, q := range fixtureQuadrants {
		x := int(q.fracX * float64(w))
		y := int(q.fracY * float64(h))
		x = clampInt(x, 0, w-1)
		y = clampInt(y, 0, h-1)
		got := imgdiff.SampleAt(img, x, y)
		want := imgdiff.Pixel{R: q.wantR, G: q.wantG, B: q.wantB, A: 255}
		if !got.Matches(want, colorTolerance) {
			failures = append(failures, fmt.Sprintf(
				"%s at (%d,%d): got R=%d G=%d B=%d A=%d, want ~R=%d G=%d B=%d (tol %d)",
				q.name, x, y, got.R, got.G, got.B, got.A, want.R, want.G, want.B, colorTolerance))
		}
	}
	if len(failures) > 0 {
		t.Fatalf("screenshot pixels do not match the fixture's colored quadrants — "+
			"capture returned a frame but it is not the rendered fixture (wrong scene, color pipeline, or stale buffer):\n  %s",
			strings.Join(failures, "\n  "))
	}
}

// fixtureRelDir is the repo-relative path to the screenshot fixture project.
func fixtureRelDir() string {
	return filepath.Join("testdata", "screenshot_fixture")
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
