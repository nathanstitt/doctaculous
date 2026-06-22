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

func TestEvenOddLogged(t *testing.T) {
	dev, _ := newTestDevice(50, 50)
	logged := false
	dev.SetLogf(func(string, ...any) { logged = true })
	dev.Fill(rectPath(5, 5, 20, 20), render.FillPaint{Color: color.RGBA{0, 0, 0, 255}, Rule: render.EvenOdd})
	if !logged {
		t.Error("even-odd fill should emit a debug log (v1 approximation)")
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
