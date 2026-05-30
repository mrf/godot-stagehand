package imgdiff

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// quadrantImage paints the four-color fixture pattern (red/green/blue/white)
// used by the screenshot pixel fixture, so the analyzer can be exercised
// without launching Godot.
func quadrantImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.RGBA
			left := x < w/2
			top := y < h/2
			switch {
			case top && left:
				c = color.RGBA{255, 0, 0, 255}
			case top && !left:
				c = color.RGBA{0, 255, 0, 255}
			case !top && left:
				c = color.RGBA{0, 0, 255, 255}
			default:
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestSampleAtAndMatches(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{10, 20, 30, 255})
	got := SampleAt(img, 1, 1)
	want := Pixel{10, 20, 30, 255}
	if got != want {
		t.Fatalf("SampleAt = %+v, want %+v", got, want)
	}
	if !got.Matches(Pixel{12, 18, 33, 255}, 5) {
		t.Errorf("expected match within tolerance 5")
	}
	if got.Matches(Pixel{20, 20, 30, 255}, 5) {
		t.Errorf("expected no match: red channel differs by 10 > tol 5")
	}
}

func TestSummarizeMeaningfulRender(t *testing.T) {
	c := Summarize(quadrantImage(320, 240))
	if c.Width != 320 || c.Height != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", c.Width, c.Height)
	}
	if c.AllTransparent || c.AllOpaqueBlack || c.SingleColor {
		t.Fatalf("meaningful render flagged blank: %+v", c)
	}
	if c.DistinctColors < 4 {
		t.Errorf("DistinctColors = %d, want >= 4", c.DistinctColors)
	}
	if diag := BlankDiagnosis(c); diag != "" {
		t.Errorf("BlankDiagnosis on meaningful render = %q, want empty", diag)
	}
}

func TestSummarizeAllTransparent(t *testing.T) {
	c := Summarize(solidImage(64, 64, color.RGBA{0, 0, 0, 0}))
	if !c.AllTransparent {
		t.Fatalf("expected AllTransparent, got %+v", c)
	}
	diag := BlankDiagnosis(c)
	if diag == "" {
		t.Fatal("expected non-empty diagnosis for all-transparent image")
	}
	if !strings.Contains(diag, "transparent") {
		t.Errorf("diagnosis %q should mention transparency", diag)
	}
	if !strings.Contains(strings.ToLower(diag), "no-frame") && !strings.Contains(strings.ToLower(diag), "headless") {
		t.Errorf("diagnosis %q should distinguish no-frame/headless cause", diag)
	}
}

func TestSummarizeAllOpaqueBlack(t *testing.T) {
	c := Summarize(solidImage(64, 64, color.RGBA{0, 0, 0, 255}))
	if !c.AllOpaqueBlack {
		t.Fatalf("expected AllOpaqueBlack, got %+v", c)
	}
	diag := BlankDiagnosis(c)
	if !strings.Contains(strings.ToLower(diag), "black") {
		t.Errorf("diagnosis %q should mention black", diag)
	}
	if !strings.Contains(strings.ToLower(diag), "gpu") {
		t.Errorf("diagnosis %q should distinguish no-GPU/driver cause", diag)
	}
}

func TestSummarizeSingleColor(t *testing.T) {
	c := Summarize(solidImage(64, 64, color.RGBA{40, 50, 60, 255}))
	if !c.SingleColor {
		t.Fatalf("expected SingleColor, got %+v", c)
	}
	if c.AllOpaqueBlack {
		t.Fatalf("non-black uniform image must not be flagged AllOpaqueBlack: %+v", c)
	}
	diag := BlankDiagnosis(c)
	if diag == "" {
		t.Fatal("expected non-empty diagnosis for single-color image")
	}
	if !strings.Contains(strings.ToLower(diag), "color") {
		t.Errorf("diagnosis %q should mention the uniform color", diag)
	}
}

func TestSummarizeEmptyBounds(t *testing.T) {
	c := Summarize(image.NewRGBA(image.Rect(0, 0, 0, 0)))
	if c.SampleCount != 0 {
		t.Errorf("SampleCount = %d, want 0 for empty image", c.SampleCount)
	}
	// An image with no pixels is not a valid render, but must not panic and
	// must not be reported as a meaningful (non-blank) frame either.
	if c.AllTransparent || c.AllOpaqueBlack || c.SingleColor {
		t.Errorf("empty image should not assert blank-content flags: %+v", c)
	}
}
