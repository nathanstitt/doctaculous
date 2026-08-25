package draw

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

func TestDrawVector(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40">
	  <rect x="10" y="10" width="20" height="20" fill="#ff0000"/>
	  <rect x="0" y="0" width="40" height="5" fill="none" stroke="blue" stroke-width="2"/>
	</svg>`
	doc, err := svg.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	stddraw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	dev := raster.New(img)
	New(doc).DrawVector(dev, render.Identity)

	if got := img.RGBAAt(20, 20); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("center = %+v, want red", got)
	}
	if got := img.RGBAAt(20, 35); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("outside = %+v, want white", got)
	}
	// Stroke of width 2 on the y=5 edge: (20,5) is on the stroked line.
	if got := img.RGBAAt(20, 5); got.B < 200 || got.R > 100 {
		t.Errorf("stroke = %+v, want blue-ish", got)
	}
	// Scaled CTM: stroke width scales with it.
	img2 := image.NewRGBA(image.Rect(0, 0, 80, 80))
	stddraw.Draw(img2, img2.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	New(doc).DrawVector(raster.New(img2), render.Scale(2, 2))
	if got := img2.RGBAAt(40, 40); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("scaled center = %+v", got)
	}
}
