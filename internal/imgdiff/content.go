package imgdiff

import (
	"fmt"
	"image"
)

// Pixel is an 8-bit-per-channel RGBA sample.
type Pixel struct {
	R, G, B, A uint8
}

// SampleAt returns the pixel at (x, y) downsampled to 8 bits per channel.
func SampleAt(img image.Image, x, y int) Pixel {
	r, g, b, a := img.At(x, y).RGBA() // each channel is 16-bit (0..0xffff)
	return Pixel{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: uint8(a >> 8),
	}
}

// Matches reports whether p is within tol (inclusive, per channel, 0-255) of want.
func (p Pixel) Matches(want Pixel, tol uint8) bool {
	return channelClose(p.R, want.R, tol) &&
		channelClose(p.G, want.G, tol) &&
		channelClose(p.B, want.B, tol) &&
		channelClose(p.A, want.A, tol)
}

func channelClose(a, b, tol uint8) bool {
	if a > b {
		return a-b <= tol
	}
	return b-a <= tol
}

// Content summarizes whether an image looks like a real render or a blank/empty
// capture. It samples a bounded grid of points rather than every pixel, so it
// stays cheap on large viewports while still detecting uniform/blank frames.
type Content struct {
	Width, Height  int
	SampleCount    int
	DistinctColors int  // number of distinct sampled colors, capped at distinctCap
	AllTransparent bool // every sampled pixel has alpha 0
	AllOpaqueBlack bool // every sampled pixel is opaque (0,0,0,255)
	SingleColor    bool // every sampled pixel is identical
	Dominant       Pixel
}

const (
	// contentSampleGrid bounds the sampling to a grid of at most this many
	// points per axis (so at most contentSampleGrid^2 samples regardless of
	// viewport size).
	contentSampleGrid = 32
	// distinctCap bounds the distinct-color count so a busy image does not
	// allocate an unbounded map.
	distinctCap = 256
)

// Summarize inspects img on a sampling grid and reports content statistics.
func Summarize(img image.Image) Content {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	c := Content{Width: w, Height: h}
	if w <= 0 || h <= 0 {
		return c
	}

	stepX := w / contentSampleGrid
	if stepX < 1 {
		stepX = 1
	}
	stepY := h / contentSampleGrid
	if stepY < 1 {
		stepY = 1
	}

	seen := make(map[Pixel]struct{})
	allTransparent := true
	allOpaqueBlack := true
	singleColor := true
	var first Pixel
	n := 0

	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			p := SampleAt(img, x, y)
			if n == 0 {
				first = p
				c.Dominant = p
			} else if p != first {
				singleColor = false
			}
			if p.A != 0 {
				allTransparent = false
			}
			if !(p.A == 255 && p.R == 0 && p.G == 0 && p.B == 0) {
				allOpaqueBlack = false
			}
			if len(seen) < distinctCap {
				seen[p] = struct{}{}
			}
			n++
		}
	}

	c.SampleCount = n
	c.DistinctColors = len(seen)
	c.AllTransparent = n > 0 && allTransparent
	c.AllOpaqueBlack = n > 0 && allOpaqueBlack
	c.SingleColor = n > 0 && singleColor
	return c
}

// BlankDiagnosis returns an actionable message when the content looks empty or
// uniform (i.e. NOT a real render), or "" when the image carries meaningful
// pixels. The message distinguishes the likely failure mode so callers can tell
// a no-frame / headless / no-GPU / blank-scene problem apart from a transport
// or buffer issue (which surface earlier as decode errors, not blank pixels).
func BlankDiagnosis(c Content) string {
	if c.SampleCount == 0 {
		return "screenshot image has no pixels to sample (zero-size capture): " +
			"the viewport reported empty dimensions — likely a no-frame/headless capture or a truncated transport buffer."
	}
	switch {
	case c.AllTransparent:
		return "all sampled pixels are fully transparent (alpha=0): the viewport texture was read before any frame was drawn (no-frame), " +
			"or the renderer produced no output (headless/no-GPU). Run Godot on a visible display with a working rasterizer before capturing."
	case c.AllOpaqueBlack:
		return "all sampled pixels are opaque black (0,0,0): a frame was produced but nothing was drawn into it — " +
			"typically a no-GPU/software-driver failure or a cleared-but-unrendered scene. Verify a real GPU or software rasterizer is available and the scene draws content."
	case c.SingleColor:
		return fmt.Sprintf("all sampled pixels share one color (R=%d G=%d B=%d A=%d): only the clear/background color rendered; "+
			"scene geometry is missing, off-screen, or not yet visible.", c.Dominant.R, c.Dominant.G, c.Dominant.B, c.Dominant.A)
	default:
		return ""
	}
}
