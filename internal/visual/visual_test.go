package visual

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func screenshotJSON(t *testing.T, payload map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestParamsSelectorDisablesFullPage(t *testing.T) {
	full := Params("")
	if full["full_page"] != true {
		t.Errorf("Params(\"\")[full_page] = %v, want true", full["full_page"])
	}
	if _, ok := full["selector"]; ok {
		t.Error("Params(\"\") should not set a selector")
	}

	cropped := Params("name:Panel")
	if cropped["full_page"] != false {
		t.Errorf("Params(selector)[full_page] = %v, want false", cropped["full_page"])
	}
	if cropped["selector"] != "name:Panel" {
		t.Errorf("Params(selector)[selector] = %v, want name:Panel", cropped["selector"])
	}
}

func TestDecodeRejectsAddonReportedFailure(t *testing.T) {
	raw := screenshotJSON(t, map[string]any{
		"error":      "Failed to capture viewport image",
		"error_code": "capture_failed",
		"details":    map[string]any{"headless": true},
	})
	_, err := Decode(raw)
	if err == nil {
		t.Fatal("Decode accepted an addon-reported capture failure")
	}
	if !strings.Contains(err.Error(), "Failed to capture viewport image") {
		t.Errorf("error %q does not carry the addon reason", err)
	}
	if !strings.Contains(err.Error(), "capture_failed") {
		t.Errorf("error %q does not carry the addon error code", err)
	}
}

func TestDecodeRejectsDimensionMismatch(t *testing.T) {
	raw := screenshotJSON(t, map[string]any{
		"data":   base64.StdEncoding.EncodeToString(solidPNG(t, 4, 4, color.RGBA{A: 255})),
		"width":  8,
		"height": 4,
	})
	if _, err := Decode(raw); err == nil {
		t.Fatal("Decode accepted a width mismatch between the addon report and the PNG")
	}
}

func TestDecodeReturnsShot(t *testing.T) {
	pngBytes := solidPNG(t, 4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	raw := screenshotJSON(t, map[string]any{
		"data": base64.StdEncoding.EncodeToString(pngBytes),
	})
	shot, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if shot.Width != 4 || shot.Height != 4 {
		t.Errorf("shot dims = %dx%d, want 4x4", shot.Width, shot.Height)
	}
	if shot.MimeType != "image/png" {
		t.Errorf("shot mime = %q, want image/png", shot.MimeType)
	}
	if !bytes.Equal(shot.PNG, pngBytes) {
		t.Error("shot PNG bytes do not round-trip")
	}
}

func TestSaveBaselineWritesFile(t *testing.T) {
	dir := t.TempDir()
	shot := Shot{PNG: solidPNG(t, 2, 3, color.RGBA{A: 255}), Width: 2, Height: 3}
	outcome, err := SaveBaseline(dir, "main_menu", "name:Root", shot)
	if err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}
	want := filepath.Join(dir, "main_menu.png")
	if outcome.Path != want {
		t.Errorf("outcome.Path = %q, want %q", outcome.Path, want)
	}
	if outcome.Width != 2 || outcome.Height != 3 {
		t.Errorf("outcome dims = %dx%d, want 2x3", outcome.Width, outcome.Height)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
}

func TestSaveBaselineRejectsUnsafeName(t *testing.T) {
	dir := t.TempDir()
	shot := Shot{PNG: solidPNG(t, 1, 1, color.RGBA{A: 255}), Width: 1, Height: 1}
	if _, err := SaveBaseline(dir, "../escape", "", shot); err == nil {
		t.Fatal("SaveBaseline accepted a path-traversing baseline name")
	}
}

func TestCompareBaselineDetectsRegressionAndWritesArtifacts(t *testing.T) {
	baselineDir := t.TempDir()
	artifactDir := t.TempDir()

	baseline := Shot{PNG: solidPNG(t, 4, 4, color.RGBA{A: 255}), Width: 4, Height: 4}
	if _, err := SaveBaseline(baselineDir, "main_menu", "", baseline); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	current := Shot{PNG: solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255}), Width: 4, Height: 4}
	outcome, err := CompareBaseline(DiffConfig{
		BaselineDir: baselineDir,
		ArtifactDir: artifactDir,
		Name:        "main_menu",
	}, current)
	if err != nil {
		t.Fatalf("CompareBaseline: %v", err)
	}
	if outcome.Pass {
		t.Error("outcome.Pass = true, want false for a fully changed frame")
	}
	if outcome.DiffPixels != 16 || outcome.TotalPixels != 16 {
		t.Errorf("diff/total = %d/%d, want 16/16", outcome.DiffPixels, outcome.TotalPixels)
	}
	for _, path := range []string{outcome.ActualImagePath, outcome.DiffImagePath} {
		if path == "" {
			t.Fatalf("outcome missing artifact path: %+v", outcome)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("artifact %q not written: %v", path, err)
		}
	}
	if !strings.Contains(outcome.Report(), "Diff ratio") {
		t.Errorf("report %q missing diff ratio line", outcome.Report())
	}
}

func TestCompareBaselinePassesWithinThreshold(t *testing.T) {
	baselineDir := t.TempDir()
	baseline := Shot{PNG: solidPNG(t, 4, 4, color.RGBA{A: 255}), Width: 4, Height: 4}
	if _, err := SaveBaseline(baselineDir, "hud", "", baseline); err != nil {
		t.Fatalf("SaveBaseline: %v", err)
	}

	outcome, err := CompareBaseline(DiffConfig{
		BaselineDir: baselineDir,
		ArtifactDir: t.TempDir(),
		Name:        "hud",
		Threshold:   1.0,
	}, Shot{PNG: solidPNG(t, 4, 4, color.RGBA{G: 255, A: 255}), Width: 4, Height: 4})
	if err != nil {
		t.Fatalf("CompareBaseline: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("outcome.Pass = false with threshold 1.0: %+v", outcome)
	}
	if outcome.ActualImagePath != "" {
		t.Error("a passing diff must not write failure artifacts")
	}
}

func TestCompareBaselineMissingBaselineIsActionable(t *testing.T) {
	_, err := CompareBaseline(DiffConfig{
		BaselineDir: t.TempDir(),
		ArtifactDir: t.TempDir(),
		Name:        "absent",
	}, Shot{PNG: solidPNG(t, 1, 1, color.RGBA{A: 255}), Width: 1, Height: 1})
	if err == nil {
		t.Fatal("CompareBaseline succeeded without a baseline on disk")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("error %q does not name the missing baseline", err)
	}
}
