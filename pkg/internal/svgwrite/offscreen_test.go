package svgwrite

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// TestBuildClipMaskNeverReturnsNil pins a contract rule that is easy to break
// and silent when broken: nil means "no restriction" to EndGroup, so returning
// it for a clip that covers NOTHING would invert the result — the clipped
// content would paint in full instead of disappearing. Every degenerate input
// must still yield a real (possibly empty) mask.
func TestBuildClipMaskNeverReturnsNil(t *testing.T) {
	square := squarePath(0, 0, 5, 5)
	for _, tc := range []struct {
		name  string
		w, h  int
		paths []render.MaskPath
	}{
		{"empty-paths", 10, 10, nil},
		{"zero-size-device", 0, 0, nil},
		{"nil-path-entry", 10, 10, []render.MaskPath{{Path: nil}}},
		{"empty-path-entry", 10, 10, []render.MaskPath{{Path: &render.Path{}}}},
		{"real-path", 10, 10, []render.MaskPath{{Path: square, Rule: render.NonZero}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := New(tc.w, tc.h).BuildClipMask(tc.paths); got == nil {
				t.Error("returned nil; nil means \"no restriction\", the opposite of an empty clip")
			}
		})
	}
}

// TestRenderOffscreenReturnsRealPixels is the behavior that distinguishes this
// backend from pdfwrite. render.Device permits a vector backend to return nil
// here (pdfwrite does, having no raster surface), and callers treat nil as
// "filtering unavailable" and render unfiltered. An SVG document can embed a
// bitmap, so returning real pixels is what makes filters work on SVG output.
func TestRenderOffscreenReturnsRealPixels(t *testing.T) {
	d := New(20, 20)
	got := d.RenderOffscreen(image.Point{X: 20, Y: 20}, func(dev render.Device) {
		dev.Fill(squarePath(0, 0, 20, 20), render.FillPaint{Color: color.RGBA{R: 255, A: 255}})
	})
	if got == nil {
		t.Fatal("returned nil: this backend CAN rasterize, so filters must not degrade")
	}
	if got.Bounds().Dx() != 20 || got.Bounds().Dy() != 20 {
		t.Fatalf("bounds = %v, want 20x20", got.Bounds())
	}
	// The paint must actually have landed, not just been allocated.
	if c := got.RGBAAt(10, 10); c.R == 0 && c.A == 0 {
		t.Errorf("offscreen surface is blank at (10,10): %+v", c)
	}
}

// A degenerate request must not allocate or panic.
func TestRenderOffscreenRejectsDegenerateSize(t *testing.T) {
	d := New(20, 20)
	if got := d.RenderOffscreen(image.Point{}, func(render.Device) {}); got != nil {
		t.Errorf("zero size returned a surface: %v", got.Bounds())
	}
	if got := d.RenderOffscreen(image.Point{X: 10, Y: 10}, nil); got != nil {
		t.Error("nil paint returned a surface")
	}
}

// BuildLuminanceMask must convert painted content to coverage. A mask that
// comes back empty for content that was painted would silently erase the
// group it masks.
func TestBuildLuminanceMaskReflectsPaintedContent(t *testing.T) {
	d := New(16, 16)
	m := d.BuildLuminanceMask(image.Point{X: 16, Y: 16}, false, func(dev render.Device) {
		dev.Fill(squarePath(0, 0, 16, 16), render.FillPaint{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	})
	if m == nil {
		t.Fatal("returned nil for paintable content")
	}
	// White at full alpha is full luminance, so coverage should be near 255.
	if a := m.AlphaAt(8, 8).A; a < 250 {
		t.Errorf("white content gave coverage %d, want ~255", a)
	}
}
