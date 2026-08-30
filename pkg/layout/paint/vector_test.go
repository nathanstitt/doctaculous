package paint

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/render/raster"
)

// fakeScene records the ctm it was drawn with and paints one Fill op.
type fakeScene struct {
	got   render.Matrix
	drawn bool
}

func (f *fakeScene) DrawVector(dev render.Device, ctm render.Matrix) {
	f.got = ctm
	f.drawn = true
	p := &render.Path{}
	x, y := ctm.Apply(0, 0)
	x2, y2 := ctm.Apply(10, 10)
	p.MoveTo(x, y)
	p.LineTo(x2, y)
	p.LineTo(x2, y2)
	p.Close()
	dev.Fill(p, render.FillPaint{Color: color.RGBA{0, 255, 0, 255}})
}

func TestPaintVector(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := raster.New(img)
	fs := &fakeScene{}
	pg := &layout.Page{WidthPt: 50, HeightPt: 50, Items: []layout.Item{{
		Kind:   layout.VectorKind,
		Vector: layout.VectorItem{Scene: fs, XPt: 5, YPt: 5, WPt: 10, HPt: 10},
	}}}
	PaintPage(dev, pg, render.Scale(2, 2)) // 50pt page at 2x = 100px

	if !fs.drawn {
		t.Fatal("scene not drawn")
	}
	// ctm = Translate(5,5) then Scale(2): (0,0) -> (10,10).
	if x, y := fs.got.Apply(0, 0); x != 10 || y != 10 {
		t.Errorf("ctm origin = (%g,%g), want (10,10)", x, y)
	}
	// Painted inside the box (away from the diagonal hypotenuse, which
	// antialiases at exactly x==y in device space)...
	if got := img.RGBAAt(20, 12); got != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("inside = %+v", got)
	}
	// ...but clipped at the box edge: the triangle extends to (10,10) user =
	// (25,25) device? No: box is 10pt wide -> clip at x=(5+10)*2=30. The fill
	// reaches x2=(10 user)->30 device exactly, so check just past the clip.
	if got := img.RGBAAt(35, 12); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("outside clip = %+v, want white", got)
	}
	// Nil scene: no panic.
	pg.Items[0].Vector.Scene = nil
	PaintPage(dev, pg, render.Identity)
}
