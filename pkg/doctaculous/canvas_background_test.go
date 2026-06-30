package doctaculous

import (
	"context"
	"image"
	"image/color"
	"testing"
)

// TestCanvasBackgroundPropagation is a regression test for CSS background
// propagation: a background on <body> (or <html>) must fill the WHOLE page canvas,
// not just that box. This is the behavior that made e.g. Hacker News (a beige body
// background) render with white margins/empty area before the fix.
func TestCanvasBackgroundPropagation(t *testing.T) {
	// HN-style beige body background; the page is taller than the content (a single
	// tall page at a fixed viewport), so there is empty area below the content that
	// must still be beige.
	const src = `<!DOCTYPE html><html><body style="background:#f6f6ef">` +
		`<p>Hacker News</p></body></html>`
	doc, err := OpenHTMLBytes([]byte(src), WithViewportWidth(200))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba := img.(*image.RGBA)
	b := rgba.Bounds()

	// Every corner of the canvas must be the propagated beige — including the
	// top-left (inside the body's default margin) and the bottom corners (empty area
	// below the content). Before propagation these were white.
	beige := func(x, y int) bool {
		c := rgba.RGBAAt(x, y)
		return c.R > 0xf0 && c.G > 0xf0 && c.B > 0xe0 && c.B < 0xf5 && c.A == 0xff
	}
	corners := [][2]int{
		{b.Min.X, b.Min.Y},         // top-left
		{b.Max.X - 1, b.Min.Y},     // top-right
		{b.Min.X, b.Max.Y - 1},     // bottom-left
		{b.Max.X - 1, b.Max.Y - 1}, // bottom-right
	}
	for _, c := range corners {
		if !beige(c[0], c[1]) {
			t.Errorf("canvas corner (%d,%d) = %v, want propagated beige #f6f6ef",
				c[0], c[1], rgba.RGBAAt(c[0], c[1]))
		}
	}
}

// TestNoBackgroundStaysWhite confirms the default is unchanged: a document with no
// root/body background still renders on the default white canvas (no propagation,
// byte-for-byte the prior behavior).
func TestNoBackgroundStaysWhite(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte(`<!DOCTYPE html><html><body><p>x</p></body></html>`),
		WithViewportWidth(120))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba := img.(*image.RGBA)
	if c := rgba.RGBAAt(0, 0); c != (color.RGBA{0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("canvas corner with no root background = %v, want opaque white", c)
	}
}
