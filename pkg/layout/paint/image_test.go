package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// imageRecordDevice records the DrawImage ctm and whether a clip was pushed, so
// object-fit tests can assert the mapping without pixel sampling. Other methods are
// no-ops except the clip counter.
type imageRecordDevice struct {
	ctms     []render.Matrix
	pushClip int
}

func (d *imageRecordDevice) Size() (int, int)                        { return 0, 0 }
func (d *imageRecordDevice) Fill(*render.Path, render.FillPaint)     {}
func (d *imageRecordDevice) Stroke(*render.Path, render.StrokePaint) {}
func (d *imageRecordDevice) DrawImage(_ image.Image, ctm render.Matrix, _ float64, _ string) {
	d.ctms = append(d.ctms, ctm)
}
func (d *imageRecordDevice) FillGlyph(*render.Path, render.FillColor, string) {}
func (d *imageRecordDevice) DrawGlyph(render.GlyphRef)                        {}
func (d *imageRecordDevice) FillShading(render.Shader, render.Matrix, string) {}
func (d *imageRecordDevice) PushClip(*render.Path, render.FillRule)           { d.pushClip++ }
func (d *imageRecordDevice) Save()                                            {}
func (d *imageRecordDevice) Restore()                                         {}

// fourCorner returns a 2x2 image with distinct corner colors so orientation is
// observable: TL=red TR=green BL=blue BR=white.
func fourCorner() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})     // top-left
	img.Set(1, 0, color.RGBA{0, 255, 0, 255})     // top-right
	img.Set(0, 1, color.RGBA{0, 0, 255, 255})     // bottom-left
	img.Set(1, 1, color.RGBA{255, 255, 255, 255}) // bottom-right
	return img
}

// TestPaintImageOrientation: object-fit:fill renders the image upright into the
// content box — the source's top row stays at the top, left column at the left.
// This pins the unit-square→content-box matrix against render.Device.DrawImage's
// v-flip sampling.
func TestPaintImageOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	dev := raster.New(img)
	it := &layout.ImageItem{Img: fourCorner(), XPt: 0, YPt: 0, WPt: 100, HPt: 100, Fit: layout.FitFill}
	paintImage(dev, it, render.Scale(1, 1))

	cases := []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"top-left", 25, 25, color.RGBA{255, 0, 0, 255}},
		{"top-right", 75, 25, color.RGBA{0, 255, 0, 255}},
		{"bottom-left", 25, 75, color.RGBA{0, 0, 255, 255}},
		{"bottom-right", 75, 75, color.RGBA{255, 255, 255, 255}},
	}
	for _, c := range cases {
		if got := img.RGBAAt(c.x, c.y); got != c.want {
			t.Errorf("%s pixel (%d,%d) = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

// TestPaintImageFillMapsToContentBox: with object-fit:fill the destination is the
// whole content box, so the recorded ctm maps the unit square's corners to the box's
// corners (top-left of the image at the box top-left).
func TestPaintImageFillMapsToContentBox(t *testing.T) {
	dev := &imageRecordDevice{}
	it := &layout.ImageItem{Img: solid(4, 4), XPt: 10, YPt: 20, WPt: 80, HPt: 40, Fit: layout.FitFill}
	paintImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 1 {
		t.Fatalf("DrawImage calls = %d, want 1", len(dev.ctms))
	}
	m := dev.ctms[0]
	// Image top-left is unit (0,1); it must map to the content-box top-left (10,20).
	x, y := m.Apply(0, 1)
	if !approx(x, 10) || !approx(y, 20) {
		t.Errorf("image top-left -> (%v,%v), want (10,20)", x, y)
	}
	// Image bottom-right is unit (1,0); maps to content-box bottom-right (90,60).
	x, y = m.Apply(1, 0)
	if !approx(x, 90) || !approx(y, 60) {
		t.Errorf("image bottom-right -> (%v,%v), want (90,60)", x, y)
	}
	if dev.pushClip != 0 {
		t.Errorf("fill pushed %d clips, want 0 (fill never overflows)", dev.pushClip)
	}
}

// TestPaintImageContainLetterboxes: object-fit:contain fits the image inside the box
// preserving aspect ratio and centers it; a 2:1 image in a 100x100 box becomes
// 100x50 centered vertically (y from 25 to 75). No clip (contain never overflows).
func TestPaintImageContainLetterboxes(t *testing.T) {
	dev := &imageRecordDevice{}
	it := &layout.ImageItem{Img: solid(20, 10), XPt: 0, YPt: 0, WPt: 100, HPt: 100, Fit: layout.FitContain, PosX: 0.5, PosY: 0.5}
	paintImage(dev, it, render.Scale(1, 1))
	m := dev.ctms[0]
	// Top-left of the fitted image: x=0 (full width), y=25 (centered: (100-50)/2).
	x, y := m.Apply(0, 1)
	if !approx(x, 0) || !approx(y, 25) {
		t.Errorf("contain top-left -> (%v,%v), want (0,25)", x, y)
	}
	// Bottom-right: x=100, y=75.
	x, y = m.Apply(1, 0)
	if !approx(x, 100) || !approx(y, 75) {
		t.Errorf("contain bottom-right -> (%v,%v), want (100,75)", x, y)
	}
	if dev.pushClip != 0 {
		t.Errorf("contain pushed %d clips, want 0", dev.pushClip)
	}
}

// TestPaintImageCoverClips: object-fit:cover scales to cover the box (overflowing on
// one axis) and clips to the content box. A 2:1 image in a 100x100 box becomes
// 200x100 centered horizontally (x from -50 to 150), so a clip is pushed.
func TestPaintImageCoverClips(t *testing.T) {
	dev := &imageRecordDevice{}
	it := &layout.ImageItem{Img: solid(20, 10), XPt: 0, YPt: 0, WPt: 100, HPt: 100, Fit: layout.FitCover, PosX: 0.5, PosY: 0.5}
	paintImage(dev, it, render.Scale(1, 1))
	m := dev.ctms[0]
	x, y := m.Apply(0, 1) // top-left of the oversized image
	if !approx(x, -50) || !approx(y, 0) {
		t.Errorf("cover top-left -> (%v,%v), want (-50,0)", x, y)
	}
	if dev.pushClip != 1 {
		t.Errorf("cover pushed %d clips, want 1 (overflow clipped to box)", dev.pushClip)
	}
}

// TestPaintImageNoneCentersIntrinsic: object-fit:none uses the intrinsic size,
// centered; a 40x20 image in a 100x100 box sits at (30,40)-(70,60), within the box,
// so no clip.
func TestPaintImageNoneCentersIntrinsic(t *testing.T) {
	dev := &imageRecordDevice{}
	it := &layout.ImageItem{Img: solid(40, 20), XPt: 0, YPt: 0, WPt: 100, HPt: 100, Fit: layout.FitNone, PosX: 0.5, PosY: 0.5}
	paintImage(dev, it, render.Scale(1, 1))
	m := dev.ctms[0]
	x, y := m.Apply(0, 1)
	if !approx(x, 30) || !approx(y, 40) {
		t.Errorf("none top-left -> (%v,%v), want (30,40)", x, y)
	}
	if dev.pushClip != 0 {
		t.Errorf("none (fitting) pushed %d clips, want 0", dev.pushClip)
	}
}

// TestPaintImageNilImageNoDraw: a nil image (failed decode placeholder) draws
// nothing and never panics.
func TestPaintImageNilImageNoDraw(t *testing.T) {
	dev := &imageRecordDevice{}
	it := &layout.ImageItem{Img: nil, XPt: 0, YPt: 0, WPt: 50, HPt: 50, Fit: layout.FitFill}
	paintImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 0 {
		t.Errorf("nil image drew %d times, want 0", len(dev.ctms))
	}
}

func solid(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{1, 2, 3, 255})
		}
	}
	return img
}

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}

// TestPaintImageObjectPosition pins D1: object-position shifts the fitted image within
// the content box. A 40x20 image with object-fit:none in a 100x100 box: at PosX=0,PosY=0
// (left top) it sits at (0,0); at PosX=1,PosY=1 (right bottom) at (60,80).
func TestPaintImageObjectPosition(t *testing.T) {
	check := func(posX, posY, wantX, wantY float64) {
		dev := &imageRecordDevice{}
		it := &layout.ImageItem{Img: solid(40, 20), XPt: 0, YPt: 0, WPt: 100, HPt: 100,
			Fit: layout.FitNone, PosX: posX, PosY: posY}
		paintImage(dev, it, render.Scale(1, 1))
		x, y := dev.ctms[0].Apply(0, 1) // top-left corner
		if !approx(x, wantX) || !approx(y, wantY) {
			t.Errorf("object-position (%.1f,%.1f) top-left -> (%v,%v), want (%v,%v)", posX, posY, x, y, wantX, wantY)
		}
	}
	check(0, 0, 0, 0)       // left top
	check(1, 1, 60, 80)     // right bottom: free space 60x80
	check(0.5, 0.5, 30, 40) // center (default)
}
