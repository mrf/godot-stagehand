// Package imgdiff provides pixel-by-pixel image comparison using only the
// standard library. No external dependencies.
package imgdiff

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// Result holds the outcome of comparing two images.
type Result struct {
	TotalPixels int
	DiffPixels  int
	// MaxDelta is the largest per-channel absolute difference seen, in [0,1].
	MaxDelta float64
	// DiffRatio is DiffPixels / TotalPixels.
	DiffRatio float64
	// DiffImage visualises differences: differing pixels are red, matching
	// pixels are shown at quarter brightness. Nil when images are identical.
	DiffImage image.Image
}

// Compare compares images a and b pixel-by-pixel. A pixel is considered
// different when any RGBA channel differs by more than threshold (range [0,1]).
// Returns an error if the images have different bounds.
func Compare(a, b image.Image, threshold float64) (Result, error) {
	bounds := a.Bounds()
	if bounds != b.Bounds() {
		return Result{}, fmt.Errorf("image size mismatch: %v vs %v", bounds, b.Bounds())
	}

	w, h := bounds.Dx(), bounds.Dy()
	total := w * h

	diff := image.NewRGBA(bounds)
	diffCount := 0
	maxDelta := 0.0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()

			dr := math.Abs(float64(r1)-float64(r2)) / 0xffff
			dg := math.Abs(float64(g1)-float64(g2)) / 0xffff
			db := math.Abs(float64(b1)-float64(b2)) / 0xffff
			da := math.Abs(float64(a1)-float64(a2)) / 0xffff
			maxChan := math.Max(dr, math.Max(dg, math.Max(db, da)))

			if maxChan > maxDelta {
				maxDelta = maxChan
			}

			if maxChan > threshold {
				diffCount++
				intensity := clampUint8(maxChan * 4 * 255)
				diff.SetRGBA(x, y, color.RGBA{R: intensity, G: 0, B: 0, A: 255})
			} else {
				// Show matching pixels at quarter brightness so differences stand out.
				r8 := uint8(r1 >> 8)
				g8 := uint8(g1 >> 8)
				b8 := uint8(b1 >> 8)
				diff.SetRGBA(x, y, color.RGBA{R: r8 / 4, G: g8 / 4, B: b8 / 4, A: 255})
			}
		}
	}

	ratio := 0.0
	if total > 0 {
		ratio = float64(diffCount) / float64(total)
	}

	var diffImg image.Image
	if diffCount > 0 {
		diffImg = diff
	}

	return Result{
		TotalPixels: total,
		DiffPixels:  diffCount,
		MaxDelta:    maxDelta,
		DiffRatio:   ratio,
		DiffImage:   diffImg,
	}, nil
}

func clampUint8(v float64) uint8 {
	if v >= 255 {
		return 255
	}
	if v <= 0 {
		return 0
	}
	return uint8(v)
}
