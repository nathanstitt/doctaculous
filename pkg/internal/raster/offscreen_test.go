package raster

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// TestRenderOffscreenReturnsPaintedPixels is the core contract test for the
// filter seam: paint runs against a device wrapping a fresh surface, and the
// caller gets those pixels back.
func TestRenderOffscreenReturnsPaintedPixels(t *testing.T) {
	dev := New(image.NewRGBA(image.Rect(0, 0, 20, 20)))

	got := dev.RenderOffscreen(image.Point{X: 20, Y: 20}, func(scratch render.Device) {
		p := &render.Path{}
		p.MoveTo(5, 5)
		p.LineTo(15, 5)
		p.LineTo(15, 15)
		p.LineTo(5, 15)
		p.Close()
		scratch.Fill(p, render.FillPaint{
			Color: color.RGBA{R: 255, A: 255},
			Rule:  render.NonZero,
		})
	})
	if got == nil {
		t.Fatal("RenderOffscreen returned nil; the raster backend can rasterize")
	}
	if c := got.RGBAAt(10, 10); c.R != 255 || c.A != 255 {
		t.Errorf("painted pixel = %+v, want opaque red", c)
	}
	// Outside the painted shape the surface must still be the transparent
	// black BeginGroup's isolated backdrop guarantees — NOT opaque black,
	// which would composite as a visible box.
	if c := got.RGBAAt(1, 1); c.A != 0 {
		t.Errorf("unpainted pixel = %+v, want fully transparent (alpha 0)", c)
	}
}

// TestRenderOffscreenDoesNotDisturbTheReceiver confirms painting into the
// offscreen surface leaves the calling device's own pixels and state alone,
// which is what lets a filter render its source without touching the page.
func TestRenderOffscreenDoesNotDisturbTheReceiver(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 10, 10))
	dev := New(base)

	dev.RenderOffscreen(image.Point{X: 10, Y: 10}, func(scratch render.Device) {
		p := &render.Path{}
		p.MoveTo(0, 0)
		p.LineTo(10, 0)
		p.LineTo(10, 10)
		p.LineTo(0, 10)
		p.Close()
		scratch.Fill(p, render.FillPaint{Color: color.RGBA{B: 255, A: 255}, Rule: render.NonZero})
		// Leave unbalanced state behind: it must not leak to the receiver.
		scratch.Save()
		scratch.PushClip(p, render.NonZero)
		scratch.BeginGroup()
	})

	if c := base.RGBAAt(5, 5); c.A != 0 {
		t.Errorf("the receiver's own surface was painted: %+v", c)
	}
	// The receiver must still paint normally afterward.
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	p.LineTo(10, 10)
	p.LineTo(0, 10)
	p.Close()
	dev.Fill(p, render.FillPaint{Color: color.RGBA{G: 255, A: 255}, Rule: render.NonZero})
	if c := base.RGBAAt(5, 5); c.G != 255 {
		t.Errorf("the receiver could not paint after RenderOffscreen: %+v", c)
	}
}

// TestRenderOffscreenDegenerateInput upholds the never-panic rule for the
// inputs a malformed document can produce.
func TestRenderOffscreenDegenerateInput(t *testing.T) {
	dev := New(image.NewRGBA(image.Rect(0, 0, 10, 10)))

	if got := dev.RenderOffscreen(image.Point{X: 10, Y: 10}, nil); got != nil {
		t.Error("a nil paint func should yield nil, not a surface")
	}
	for _, size := range []image.Point{{}, {X: 0, Y: 5}, {X: 5, Y: 0}, {X: -3, Y: -3}} {
		called := false
		got := dev.RenderOffscreen(size, func(render.Device) { called = true })
		if got != nil {
			t.Errorf("size %v returned a surface, want nil", size)
		}
		if called {
			t.Errorf("size %v invoked paint; a non-positive size must not", size)
		}
	}
}

// TestRenderOffscreenResultIsCallerOwned confirms the returned image is not
// aliased by the device afterward, so a filter may transform it in place —
// which every primitive does.
func TestRenderOffscreenResultIsCallerOwned(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 8, 8))
	dev := New(base)

	got := dev.RenderOffscreen(image.Point{X: 8, Y: 8}, func(scratch render.Device) {})
	if got == nil {
		t.Fatal("nil surface")
	}
	got.SetRGBA(4, 4, color.RGBA{R: 255, A: 255})
	if c := base.RGBAAt(4, 4); c.R != 0 || c.A != 0 {
		t.Errorf("mutating the returned image changed the device's surface: %+v", c)
	}
}
