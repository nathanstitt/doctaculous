package pdfwrite

import (
	"image"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// TestRenderOffscreenDeclinesAndDoesNotPaint pins this writer's documented
// degradation for SVG filters: it has no pixel buffer, so it reports that by
// returning nil rather than inventing an approximation.
//
// The second half is the part that matters and is easy to get wrong: paint
// must NOT be invoked. Calling it would emit the filtered subtree's operators
// straight into this device's content stream, painting the element a second
// time on top of itself — a corrupted page rather than an honest degradation.
// The caller (pkg/svg/draw) paints the source unfiltered itself once it sees
// the nil.
func TestRenderOffscreenDeclinesAndDoesNotPaint(t *testing.T) {
	d := newPageDevice(100, 100)

	called := false
	got := d.RenderOffscreen(image.Point{X: 100, Y: 100}, func(render.Device) { called = true })

	if got != nil {
		t.Errorf("RenderOffscreen returned %v; a vector writer has no raster surface and must return nil", got)
	}
	if called {
		t.Error("paint was invoked; it would emit the subtree into this device's content stream, double-painting the element")
	}
	if s := d.contentStream(); len(s) != 0 {
		t.Errorf("declining to rasterize still wrote %d bytes of content: %q", len(s), s)
	}
}

// TestRenderOffscreenNilPaintIsSafe upholds the never-panic rule.
func TestRenderOffscreenNilPaintIsSafe(t *testing.T) {
	d := newPageDevice(10, 10)
	if got := d.RenderOffscreen(image.Point{X: 10, Y: 10}, nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
