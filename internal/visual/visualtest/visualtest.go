// Package visualtest provides the synthetic frames every test that stands in
// for a Godot screenshot needs. Keeping one generator means the CLI, scenario
// and visual suites all compare the same bytes, so a diff that passes in one
// package cannot fail in another for want of a differently-encoded PNG.
package visualtest

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
)

// SolidPNG encodes a w×h image filled with c.
func SolidPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// image/png cannot fail encoding an in-memory RGBA image; a panic here
		// means the standard library changed under us, and a test helper that
		// returned a silently-empty frame would be far harder to diagnose.
		panic(err)
	}
	return buf.Bytes()
}

// SolidPNGBase64 is SolidPNG in the base64 form the addon puts on the wire.
func SolidPNGBase64(w, h int, c color.RGBA) string {
	return base64.StdEncoding.EncodeToString(SolidPNG(w, h, c))
}

// Opaque and Red are the two frames a "did the picture change" test needs.
var (
	Opaque = color.RGBA{A: 255}
	Red    = color.RGBA{R: 255, A: 255}
	Green  = color.RGBA{G: 255, A: 255}
)
