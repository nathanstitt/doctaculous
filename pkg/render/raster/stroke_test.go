package raster

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// linePath returns a single open subpath from (x0,y0) to (x1,y1).
func linePath(x0, y0, x1, y1 float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(x0, y0)
	p.LineTo(x1, y1)
	return p
}

// covered reports whether the pixel at (x,y) is meaningfully darker than the
// white background — i.e. the stroke painted there.
func covered(img interface {
	RGBAAt(x, y int) color.RGBA
}, x, y int) bool {
	c := img.RGBAAt(x, y)
	// Background is white (255,255,255). Any black-ish stroke lowers the channels.
	return c.R < 200 || c.G < 200 || c.B < 200
}

// TestStrokeRoundCap: a round cap paints a semicircle past the endpoint. For a
// horizontal line ending at x=end, a pixel just past the end and off-axis
// (within the half-width radius) is covered for RoundCap but NOT for ButtCap.
func TestStrokeRoundCap(t *testing.T) {
	const y = 50
	const x0, x1 = 30.0, 60.0
	const width = 12.0 // half-width 6

	black := color.RGBA{0, 0, 0, 255}

	// Off-axis point just past the end, inside the cap semicircle.
	px, py := 62, 47 // ~2px past end, 3px off-axis -> within radius 6 for round

	devButt, imgButt := newTestDevice(100, 100)
	devButt.Stroke(linePath(x0, y, x1, y), render.StrokePaint{
		Color: black, Width: width, Cap: render.ButtCap, Join: render.MiterJoin, MiterLimit: 10,
	})
	if covered(imgButt, px, py) {
		t.Errorf("butt cap: pixel (%d,%d) should be uncovered past the line end", px, py)
	}

	devRound, imgRound := newTestDevice(100, 100)
	devRound.Stroke(linePath(x0, y, x1, y), render.StrokePaint{
		Color: black, Width: width, Cap: render.RoundCap, Join: render.MiterJoin, MiterLimit: 10,
	})
	if !covered(imgRound, px, py) {
		t.Errorf("round cap: pixel (%d,%d) should be covered by the cap semicircle", px, py)
	}
}

// TestStrokeSquareCap: a square cap extends the stroke by half-width past the
// end, so a pixel directly on-axis past the end is covered for SquareCap but not
// for ButtCap.
func TestStrokeSquareCap(t *testing.T) {
	const y = 50
	const x0, x1 = 30.0, 60.0
	const width = 12.0 // half-width 6
	black := color.RGBA{0, 0, 0, 255}

	px, py := 63, 50 // 3px past end, on-axis -> within square cap (extends 6px), outside butt

	devButt, imgButt := newTestDevice(100, 100)
	devButt.Stroke(linePath(x0, y, x1, y), render.StrokePaint{
		Color: black, Width: width, Cap: render.ButtCap,
	})
	if covered(imgButt, px, py) {
		t.Errorf("butt cap: pixel (%d,%d) should be uncovered past the line end", px, py)
	}

	devSq, imgSq := newTestDevice(100, 100)
	devSq.Stroke(linePath(x0, y, x1, y), render.StrokePaint{
		Color: black, Width: width, Cap: render.SquareCap,
	})
	if !covered(imgSq, px, py) {
		t.Errorf("square cap: pixel (%d,%d) should be covered by the square cap", px, py)
	}
}

// cornerPolyline draws a right-angle corner: down then right, with the vertex at
// (vx,vy). The outer corner of the join is up-and-left of the vertex.
func cornerPolyline(vx, vy float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(vx, vy-40) // come down toward the vertex
	p.LineTo(vx, vy)    // vertex
	p.LineTo(vx+40, vy) // go right
	return p
}

// TestStrokeJoinMiterVsBevel: at a 90° corner, a miter join fills the outer
// corner to a sharp point; bevel cuts it off. The exact outer-corner pixel is
// covered for miter but not for bevel.
func TestStrokeJoinMiterVsBevel(t *testing.T) {
	const vx, vy = 50.0, 50.0
	const width = 16.0 // half-width 8
	black := color.RGBA{0, 0, 0, 255}

	// For this down-then-right path the convex (outer) corner is at the
	// bottom-left, around (vx-half, vy+half) = (42,58). The miter fills a triangle
	// out to that corner; bevel cuts it with a flat chord. A pixel inside that
	// triangle (near the outer corner) is covered by miter but not by bevel.
	px, py := 44, 55

	devMiter, imgMiter := newTestDevice(100, 100)
	devMiter.Stroke(cornerPolyline(vx, vy), render.StrokePaint{
		Color: black, Width: width, Join: render.MiterJoin, MiterLimit: 10, Cap: render.ButtCap,
	})
	if !covered(imgMiter, px, py) {
		t.Errorf("miter join: outer-corner pixel (%d,%d) should be covered", px, py)
	}

	devBevel, imgBevel := newTestDevice(100, 100)
	devBevel.Stroke(cornerPolyline(vx, vy), render.StrokePaint{
		Color: black, Width: width, Join: render.BevelJoin, MiterLimit: 10, Cap: render.ButtCap,
	})
	if covered(imgBevel, px, py) {
		t.Errorf("bevel join: outer-corner pixel (%d,%d) should be cut off (uncovered)", px, py)
	}
}

// TestStrokeMiterLimitFallback: a very sharp angle whose miter length exceeds
// MiterLimit*width must fall back to bevel — the far outer miter tip is NOT
// covered.
func TestStrokeMiterLimitFallback(t *testing.T) {
	const width = 10.0
	black := color.RGBA{0, 0, 0, 255}

	// A very thin wedge: two nearly-parallel segments meeting at a sharp point.
	// The miter tip would extend far past the vertex; a low miter limit forces
	// a bevel, so the far tip stays uncovered.
	sharp := &render.Path{}
	sharp.MoveTo(20, 48)
	sharp.LineTo(60, 50) // vertex at (60,50)
	sharp.LineTo(20, 52)

	// The miter tip for this sharp angle extends well past x=60 (to the right).
	px, py := 75, 50

	devLim, imgLim := newTestDevice(120, 100)
	devLim.Stroke(sharp, render.StrokePaint{
		Color: black, Width: width, Join: render.MiterJoin, MiterLimit: 1.5, Cap: render.ButtCap,
	})
	if covered(imgLim, px, py) {
		t.Errorf("miter limit exceeded: tip pixel (%d,%d) should fall back to bevel (uncovered)", px, py)
	}

	// Sanity: with a generous miter limit the tip IS reached.
	devBig, imgBig := newTestDevice(120, 100)
	devBig.Stroke(sharp, render.StrokePaint{
		Color: black, Width: width, Join: render.MiterJoin, MiterLimit: 100, Cap: render.ButtCap,
	})
	if !covered(imgBig, px, py) {
		t.Errorf("generous miter limit: tip pixel (%d,%d) should be covered by the miter", px, py)
	}
}

// TestStrokeAlphaHonored: a 50%-alpha black stroke over white must blend to a
// mid-tone, proving the stroke composites through the same alpha path as Fill.
func TestStrokeAlphaHonored(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.Stroke(linePath(10, 50, 90, 50), render.StrokePaint{
		Color: color.RGBA{0, 0, 0, 128}, // 50% black
		Width: 20,
		Cap:   render.ButtCap,
	})
	// Center of the stroke should be ~50% gray (~127), not full black or white.
	c := img.RGBAAt(50, 50)
	if c.R < 100 || c.R > 160 {
		t.Errorf("50%% stroke center = %v, want mid-tone (~127)", c)
	}
}

// TestStrokeClipRespected: a stroke must be clipped like a fill.
func TestStrokeClipRespected(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	dev.Save()
	dev.PushClip(rectPath(0, 0, 40, 100), render.NonZero)
	dev.Stroke(linePath(10, 50, 90, 50), render.StrokePaint{
		Color: color.RGBA{0, 0, 0, 255}, Width: 10, Cap: render.ButtCap,
	})
	dev.Restore()

	if !covered(img, 20, 50) {
		t.Errorf("inside clip: stroke pixel (20,50) should be covered")
	}
	if covered(img, 70, 50) {
		t.Errorf("outside clip: stroke pixel (70,50) should be clipped out")
	}
}

// TestStrokeEmptyPathNoPanic: degenerate inputs must not panic.
func TestStrokeEmptyPathNoPanic(t *testing.T) {
	dev, _ := newTestDevice(50, 50)
	dev.Stroke(nil, render.StrokePaint{Width: 4})
	dev.Stroke(&render.Path{}, render.StrokePaint{Width: 4})
	// A lone MoveTo (no actual segment) and a zero-length line.
	p := &render.Path{}
	p.MoveTo(10, 10)
	dev.Stroke(p, render.StrokePaint{Width: 4})
	p2 := linePath(10, 10, 10, 10)
	dev.Stroke(p2, render.StrokePaint{Width: 4})
}

// TestStrokeDashed: a dashed stroke leaves gaps. With a dash pattern, a pixel in
// the middle of a gap is uncovered while a pixel in a dash is covered.
func TestStrokeDashed(t *testing.T) {
	dev, img := newTestDevice(120, 60)
	dev.Stroke(linePath(10, 30, 110, 30), render.StrokePaint{
		Color:     color.RGBA{0, 0, 0, 255},
		Width:     8,
		Cap:       render.ButtCap,
		DashArray: []float64{10, 10}, // 10 on, 10 off
	})
	// Count covered vs uncovered along the line to confirm gaps exist.
	var on, off int
	for x := 12; x < 108; x++ {
		if covered(img, x, 30) {
			on++
		} else {
			off++
		}
	}
	if off == 0 {
		t.Errorf("dashed stroke produced no gaps (off=%d, on=%d)", off, on)
	}
	if on == 0 {
		t.Errorf("dashed stroke produced no dashes (off=%d, on=%d)", off, on)
	}
}
