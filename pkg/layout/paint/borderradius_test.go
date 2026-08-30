package paint

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/layout"
	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/render/raster"
)

// black is the fill every shape test below paints with, so a sampled pixel is
// either near-black (inside the shape) or untouched white (outside it).
var black = color.RGBA{0, 0, 0, 255}

// rasterizeItems paints items onto a fresh w×h white raster surface and returns it,
// so a test can assert on ACTUAL PIXELS rather than on the item stream. Sampling
// real pixels is the only way to prove a corner is round: an item stream carrying
// the right radii still renders square if the painter ignores them.
func rasterizeItems(t *testing.T, w, h int, items []layout.Item) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)
	PaintPage(raster.New(img), &layout.Page{WidthPt: float64(w), HeightPt: float64(h), Items: items}, render.Identity)
	return img
}

// inked reports whether the pixel at (x,y) carries substantial ink. The 128
// threshold sits mid-way between the white backdrop and the black fill, so an
// antialiased edge pixel falls on whichever side its coverage actually lands —
// which is what makes a sample just inside/outside a corner arc meaningful.
func inked(t *testing.T, img *image.RGBA, x, y int) bool {
	t.Helper()
	r, _, _, _ := img.At(x, y).RGBA()
	return r>>8 < 128
}

// TestPaintRoundedBackgroundShape samples the four corners and the centre of a
// rounded background. This is the shape proof the item-stream tests cannot give:
// with a 40px radius on an 80×80 box the shape is a circle, so every corner pixel
// must be background and the centre must be filled.
func TestPaintRoundedBackgroundShape(t *testing.T) {
	items := []layout.Item{{
		Kind: layout.BackgroundKind,
		Rule: layout.RuleItem{
			XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black,
			Radii: uniformRadii(40),
		},
	}}
	dev := rasterizeItems(t, 80, 80, items)

	// The four extreme corner pixels lie outside a circle inscribed in the box.
	for _, c := range []struct{ x, y int }{{0, 0}, {79, 0}, {0, 79}, {79, 79}} {
		if inked(t, dev, c.x, c.y) {
			t.Errorf("corner (%d,%d) is inked; a 40px radius should round it away", c.x, c.y)
		}
	}
	// The centre and the four edge midpoints are inside the circle.
	for _, c := range []struct {
		x, y int
		what string
	}{
		{40, 40, "centre"},
		{40, 2, "top edge midpoint"},
		{40, 77, "bottom edge midpoint"},
		{2, 40, "left edge midpoint"},
		{77, 40, "right edge midpoint"},
	} {
		if !inked(t, dev, c.x, c.y) {
			t.Errorf("%s (%d,%d) is not inked; the circle should cover it", c.what, c.x, c.y)
		}
	}
}

// TestPaintSquareBackgroundKeepsCorners is the control for the test above: with no
// radii the SAME box must ink its corners. Without this, a painter that dropped the
// background entirely would pass the rounded-corner assertions.
func TestPaintSquareBackgroundKeepsCorners(t *testing.T) {
	items := []layout.Item{{
		Kind: layout.BackgroundKind,
		Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black},
	}}
	dev := rasterizeItems(t, 80, 80, items)
	for _, c := range []struct{ x, y int }{{0, 0}, {79, 0}, {0, 79}, {79, 79}} {
		if !inked(t, dev, c.x, c.y) {
			t.Errorf("corner (%d,%d) should be inked on a square box", c.x, c.y)
		}
	}
}

// TestPaintOverlargeRadiusIsCircle proves the §5.1 overlap correction reaches the
// PIXELS: border-radius:100px on an 80×80 box must paint exactly the same circle as
// border-radius:40px, not overlapping arcs. The two renders are compared pixel for
// pixel.
func TestPaintOverlargeRadiusIsCircle(t *testing.T) {
	renderRadius := func(r float64) *image.RGBA {
		corrected := uniformRadii(r).Correct(80, 80)
		return rasterizeItems(t, 80, 80, []layout.Item{{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black, Radii: corrected},
		}})
	}
	circle, corrected := renderRadius(40), renderRadius(100)
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			if inked(t, circle, x, y) != inked(t, corrected, x, y) {
				t.Fatalf("pixel (%d,%d) differs: radius 100 should correct to the same circle as radius 40", x, y)
			}
		}
	}
}

// TestPaintRoundedBackgroundArea measures INK COVERAGE — the sum of per-pixel
// coverage, which converges on the shape's true geometric area — and compares it to
// the analytic area. Counting any-ink pixels instead would overcount every
// antialiased edge pixel as fully covered and could not distinguish a correct
// circle from a slightly wrong one.
func TestPaintRoundedBackgroundArea(t *testing.T) {
	tests := []struct {
		name    string
		radii   layout.CornerRadii
		want    float64
		comment string
	}{
		{
			name: "circle", radii: uniformRadii(40), want: math.Pi * 40 * 40,
			comment: "a 40px radius on an 80x80 box is a circle",
		},
		{
			// Each corner removes the square minus its quarter-ellipse.
			name: "20px corners", radii: uniformRadii(20),
			want:    6400 - 4*(20*20-math.Pi/4*20*20),
			comment: "box area minus the four corner offcuts",
		},
		{
			// Elliptical corners: rx=40, ry=20 on every corner.
			name: "elliptical 40x20", radii: ellipticalRadii(40, 20),
			want:    6400 - 4*(40*20-math.Pi/4*40*20),
			comment: "the two semi-axes are independent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := rasterizeItems(t, 80, 80, []layout.Item{{
				Kind: layout.BackgroundKind,
				Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black, Radii: tt.radii},
			}})
			got := inkCoverage(dev, 80, 80)
			// 1.5% accommodates the cubic-Bézier quarter-arc approximation plus the
			// rasterizer's antialiasing; the measured error is well under 0.5%.
			if rel := math.Abs(got-tt.want) / tt.want; rel > 0.015 {
				t.Errorf("ink coverage = %.1f, want %.1f (%.2f%% off) — %s",
					got, tt.want, rel*100, tt.comment)
			}
		})
	}
}

// TestPaintBorderRingLeavesHole proves a rounded border paints a RING: ink on the
// edge, background in the middle. A ring drawn with the wrong fill rule would fill
// solid and swallow the hole, which this catches directly.
func TestPaintBorderRingLeavesHole(t *testing.T) {
	outer := uniformRadii(20)
	items := []layout.Item{{
		Kind: layout.BorderKind,
		Border: layout.BorderItem{
			XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black,
			Style: layout.BorderSolid, Side: layout.EdgeTop,
			Ring: &layout.BorderRing{
				Outer: outer,
				Inner: outer.Inset(10, 10, 10, 10).Correct(60, 60),
				Top:   10, Right: 10, Bottom: 10, Left: 10,
			},
		},
	}}
	dev := rasterizeItems(t, 80, 80, items)

	if !inked(t, dev, 40, 2) {
		t.Error("the ring's top edge should be inked")
	}
	if !inked(t, dev, 2, 40) {
		t.Error("the ring's left edge should be inked")
	}
	if inked(t, dev, 40, 40) {
		t.Error("the ring's interior should be a hole, not filled — check the even-odd fill rule")
	}
	// Just inside the inner edge is hole; just outside it is ink.
	if inked(t, dev, 40, 12) {
		t.Error("pixel just inside the inner curve should be hole")
	}
	if !inked(t, dev, 40, 8) {
		t.Error("pixel within the border band should be inked")
	}
	// The rounded outer corner must still be background.
	if inked(t, dev, 0, 0) {
		t.Error("the ring's outer corner should be rounded away")
	}
}

// TestPaintRoundedClipConfinesContent proves ClipPushKind honours radii: content
// painted inside the bracket is cut to the rounded shape, so the box's corners stay
// background even though the inner fill is a full square.
func TestPaintRoundedClipConfinesContent(t *testing.T) {
	items := []layout.Item{
		{
			Kind: layout.ClipPushKind,
			Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 80, HPt: 80, Radii: uniformRadii(40)},
		},
		{
			// A full-bleed square fill; only the rounded clip can shape it.
			Kind: layout.RuleKind,
			Rule: layout.RuleItem{XPt: 0, YPt: 0, WPt: 80, HPt: 80, Color: black},
		},
		{Kind: layout.ClipPopKind},
	}
	dev := rasterizeItems(t, 80, 80, items)
	for _, c := range []struct{ x, y int }{{0, 0}, {79, 0}, {0, 79}, {79, 79}} {
		if inked(t, dev, c.x, c.y) {
			t.Errorf("corner (%d,%d) escaped the rounded clip", c.x, c.y)
		}
	}
	if !inked(t, dev, 40, 40) {
		t.Error("the clip should not have removed the interior")
	}
}

// inkCoverage sums per-pixel ink coverage over the surface: a fully black pixel
// contributes 1, a half-covered antialiased edge pixel 0.5. The total converges on
// the painted shape's true geometric area.
func inkCoverage(img *image.RGBA, w, h int) float64 {
	sum := 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			sum += 1 - float64(r>>8)/255
		}
	}
	return sum
}

func uniformRadii(r float64) layout.CornerRadii {
	c := [2]float64{r, r}
	return layout.CornerRadii{TL: c, TR: c, BR: c, BL: c}
}

func ellipticalRadii(rx, ry float64) layout.CornerRadii {
	c := [2]float64{rx, ry}
	return layout.CornerRadii{TL: c, TR: c, BR: c, BL: c}
}
