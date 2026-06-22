package raster

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

func newTestDevice(w, h int) (*Device, *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillBackground(img, color.White)
	return New(img), img
}

func rectPath(x0, y0, x1, y1 float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(x0, y0)
	p.LineTo(x1, y0)
	p.LineTo(x1, y1)
	p.LineTo(x0, y1)
	p.Close()
	return p
}

func TestDeviceFillBounded(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.Fill(rectPath(10, 10, 30, 30), render.FillPaint{Color: color.RGBA{255, 0, 0, 255}})
	// Inside the rect is red; outside is white.
	if c := img.RGBAAt(20, 20); c.R < 200 || c.G > 50 {
		t.Errorf("inside fill = %v, want red", c)
	}
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside fill = %v, want white", c)
	}
}

func TestDeviceClipRestrictsFill(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.Save()
	dev.PushClip(rectPath(10, 10, 30, 30), render.NonZero)
	// Fill the whole canvas red; only the clipped 20x20 region should change.
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: color.RGBA{255, 0, 0, 255}})
	dev.Restore()

	if c := img.RGBAAt(20, 20); c.R < 200 {
		t.Errorf("inside clip = %v, want red", c)
	}
	if c := img.RGBAAt(60, 60); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside clip = %v, want white (clipped out)", c)
	}
}

func TestDeviceClipPoppedOnRestore(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.Save()
	dev.PushClip(rectPath(10, 10, 30, 30), render.NonZero)
	dev.Restore() // clip gone
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: color.RGBA{0, 0, 255, 255}})
	// With the clip removed, a far pixel is painted blue.
	if c := img.RGBAAt(60, 60); c.B < 200 {
		t.Errorf("after restore, fill = %v, want blue everywhere", c)
	}
}

// donutPath returns an outer rectangle with an inner rectangle hole. Both
// subpaths wind the same direction, so the nonzero rule fills the hole solid
// while the even-odd rule leaves it empty — the case that distinguishes them.
func donutPath(outer, inner float64) *render.Path {
	p := &render.Path{}
	// Outer ring.
	p.MoveTo(outer, outer)
	p.LineTo(100-outer, outer)
	p.LineTo(100-outer, 100-outer)
	p.LineTo(outer, 100-outer)
	p.Close()
	// Inner ring (same winding direction).
	p.MoveTo(inner, inner)
	p.LineTo(100-inner, inner)
	p.LineTo(100-inner, 100-inner)
	p.LineTo(inner, 100-inner)
	p.Close()
	return p
}

func TestEvenOddLeavesHole(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	black := color.RGBA{0, 0, 0, 255}
	// Outer ring at 20px inset, inner hole at 40px inset: hole spans 40..60.
	dev.Fill(donutPath(20, 40), render.FillPaint{Color: black, Rule: render.EvenOdd})

	// The ring (e.g. 30,30) is painted.
	if c := img.RGBAAt(30, 30); c.R > 50 {
		t.Errorf("ring pixel = %v, want black", c)
	}
	// The hole center (50,50) must stay white under even-odd.
	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("hole pixel = %v, want white (even-odd leaves it empty)", c)
	}
}

func TestNonZeroFillsHole(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	black := color.RGBA{0, 0, 0, 255}
	// Same donut, but nonzero winding fills the hole solid (both rings same dir).
	dev.Fill(donutPath(20, 40), render.FillPaint{Color: black, Rule: render.NonZero})

	if c := img.RGBAAt(50, 50); c.R > 50 {
		t.Errorf("hole pixel = %v, want black (nonzero fills the hole)", c)
	}
}

func TestFillAlphaBlends(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	// Black at 50% alpha over white should yield ~mid-gray (128).
	half := color.RGBA{0, 0, 0, 128}
	dev.Fill(rectPath(10, 10, 40, 40), render.FillPaint{Color: half})
	c := img.RGBAAt(25, 25)
	if c.R < 110 || c.R > 145 {
		t.Errorf("50%% black over white = %v, want ~mid-gray (R≈128)", c)
	}
}

func TestDrawImageAlphaBlends(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	// A solid blue source image drawn at 50% alpha over white → light blue.
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	fillBackground(src, color.RGBA{0, 0, 255, 255})
	// Map the unit square to a 40x40 device region at (20,20).
	ctm := render.Matrix{A: 40, B: 0, C: 0, D: 40, E: 20, F: 20}
	dev.DrawImage(src, ctm, 0.5)
	c := img.RGBAAt(40, 40)
	// 50% blue over white ≈ (128,128,255).
	if c.B < 200 || c.R < 100 || c.R > 160 {
		t.Errorf("50%% blue image over white = %v, want ~light blue (R≈128, B≈255)", c)
	}
}

func TestFillOffCanvasNoOp(t *testing.T) {
	dev, img := newTestDevice(50, 50)
	// Entirely off-canvas; must not panic and must leave the canvas white.
	dev.Fill(rectPath(100, 100, 200, 200), render.FillPaint{Color: color.RGBA{255, 0, 0, 255}})
	if !isAllWhite(img.Pix) {
		t.Error("off-canvas fill changed the canvas")
	}
}
