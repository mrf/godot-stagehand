package imgdiff

import (
	"image"
	"image/color"
	"testing"
)

// solidImage returns a 1-colour RGBA image of the given size.
func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// checkerImage returns an image where odd pixels are c1 and even pixels are c2.
func checkerImage(w, h int, c1, c2 color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if (x+y)%2 == 0 {
				img.SetRGBA(x, y, c1)
			} else {
				img.SetRGBA(x, y, c2)
			}
		}
	}
	return img
}

func TestCompare_IdenticalImages(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	a := solidImage(4, 4, red)
	b := solidImage(4, 4, red)

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 0 {
		t.Errorf("DiffPixels = %d, want 0", result.DiffPixels)
	}
	if result.DiffRatio != 0.0 {
		t.Errorf("DiffRatio = %f, want 0.0", result.DiffRatio)
	}
	if result.MaxDelta != 0.0 {
		t.Errorf("MaxDelta = %f, want 0.0", result.MaxDelta)
	}
	if result.TotalPixels != 16 {
		t.Errorf("TotalPixels = %d, want 16", result.TotalPixels)
	}
	if result.DiffImage != nil {
		t.Error("DiffImage should be nil for identical images")
	}
}

func TestCompare_CompletelyDifferent(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	a := solidImage(4, 4, red)
	b := solidImage(4, 4, blue)

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 16 {
		t.Errorf("DiffPixels = %d, want 16", result.DiffPixels)
	}
	if result.DiffRatio != 1.0 {
		t.Errorf("DiffRatio = %f, want 1.0", result.DiffRatio)
	}
	if result.DiffImage == nil {
		t.Error("DiffImage should not be nil when images differ")
	}
}

func TestCompare_PartialDiff(t *testing.T) {
	// 2x2 image: top half red, bottom half blue vs all red
	a := image.NewRGBA(image.Rect(0, 0, 2, 2))
	a.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	a.SetRGBA(1, 0, color.RGBA{R: 255, A: 255})
	a.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	a.SetRGBA(1, 1, color.RGBA{B: 255, A: 255})

	b := solidImage(2, 2, color.RGBA{R: 255, A: 255})

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 2 {
		t.Errorf("DiffPixels = %d, want 2", result.DiffPixels)
	}
	if result.DiffRatio != 0.5 {
		t.Errorf("DiffRatio = %f, want 0.5", result.DiffRatio)
	}
}

func TestCompare_PixelSensitivityFiltersSmallDiffs(t *testing.T) {
	// Nearly identical: one pixel differs by 1/255 on R channel.
	a := solidImage(2, 2, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	b := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			b.SetRGBA(x, y, color.RGBA{R: 100, G: 100, B: 100, A: 255})
		}
	}
	// Nudge one pixel by a tiny amount.
	b.SetRGBA(0, 0, color.RGBA{R: 101, G: 100, B: 100, A: 255})

	// The delta is ~0.004; pixelSensitivity of 0.01 should absorb it.
	result, err := Compare(a, b, 0.01)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 0 {
		t.Errorf("DiffPixels = %d, want 0 (within pixel sensitivity)", result.DiffPixels)
	}

	// But with pixelSensitivity=0 it should detect the difference.
	result2, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare strict: %v", err)
	}
	if result2.DiffPixels != 1 {
		t.Errorf("DiffPixels = %d, want 1 (strict comparison)", result2.DiffPixels)
	}
}

// TestCompare_PixelSensitivityIndependentOfDiffRatio verifies that
// pixelSensitivity only controls per-pixel color tolerance and has no effect
// on how DiffRatio is interpreted by callers.
func TestCompare_PixelSensitivityIndependentOfDiffRatio(t *testing.T) {
	// 4 pixels: 2 differ by a large amount, 2 are identical.
	a := image.NewRGBA(image.Rect(0, 0, 2, 2))
	b := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			a.SetRGBA(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
			b.SetRGBA(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	// Make 2 out of 4 pixels differ significantly.
	b.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	b.SetRGBA(1, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})

	// With tight pixelSensitivity (0.0), 2 pixels are flagged → DiffRatio = 0.5.
	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 2 {
		t.Errorf("DiffPixels = %d, want 2", result.DiffPixels)
	}
	if result.DiffRatio != 0.5 {
		t.Errorf("DiffRatio = %f, want 0.5", result.DiffRatio)
	}

	// pixelSensitivity=1.0 means every per-pixel delta is tolerated, so DiffPixels → 0
	// even though the pixels differ visually. The diff-ratio gate (threshold) is a
	// caller concern and not affected by pixelSensitivity.
	resultLoose, err := Compare(a, b, 1.0)
	if err != nil {
		t.Fatalf("Compare loose: %v", err)
	}
	if resultLoose.DiffPixels != 0 {
		t.Errorf("DiffPixels = %d, want 0 with pixelSensitivity=1.0", resultLoose.DiffPixels)
	}
	if resultLoose.DiffRatio != 0.0 {
		t.Errorf("DiffRatio = %f, want 0.0 with pixelSensitivity=1.0", resultLoose.DiffRatio)
	}
}

func TestCompare_SizeMismatch(t *testing.T) {
	a := solidImage(4, 4, color.RGBA{R: 255, A: 255})
	b := solidImage(8, 8, color.RGBA{R: 255, A: 255})

	_, err := Compare(a, b, 0.0)
	if err == nil {
		t.Fatal("expected error for size mismatch, got nil")
	}
}

func TestCompare_MaxDelta(t *testing.T) {
	// White vs black: max delta should be 1.0.
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	a := solidImage(2, 2, white)
	b := solidImage(2, 2, black)

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.MaxDelta < 0.99 {
		t.Errorf("MaxDelta = %f, want ~1.0", result.MaxDelta)
	}
}

func TestCompare_DiffImageHighlightsDiffs(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	a := solidImage(2, 2, red)
	b := solidImage(2, 2, blue)

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffImage == nil {
		t.Fatal("expected DiffImage, got nil")
	}
	// Diff pixels should be red (R > 0, G == 0, B == 0).
	r, g, b2, _ := result.DiffImage.At(0, 0).RGBA()
	if r == 0 {
		t.Error("diff pixel R channel should be non-zero")
	}
	if g != 0 || b2 != 0 {
		t.Errorf("diff pixel G,B channels should be 0, got G=%d B=%d", g, b2)
	}
}

func TestCompare_CheckerPattern(t *testing.T) {
	c1 := color.RGBA{R: 255, A: 255}
	c2 := color.RGBA{B: 255, A: 255}
	a := checkerImage(4, 4, c1, c2)
	// Swapped checker — every pixel differs.
	b := checkerImage(4, 4, c2, c1)

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.DiffPixels != 16 {
		t.Errorf("DiffPixels = %d, want 16", result.DiffPixels)
	}
}

func TestCompare_ZeroSizeImage(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 0, 0))
	b := image.NewRGBA(image.Rect(0, 0, 0, 0))

	result, err := Compare(a, b, 0.0)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.TotalPixels != 0 {
		t.Errorf("TotalPixels = %d, want 0", result.TotalPixels)
	}
	if result.DiffRatio != 0.0 {
		t.Errorf("DiffRatio = %f, want 0.0", result.DiffRatio)
	}
}
