package raster

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// circlePath returns a closed circular path approximated by cubic Beziers
// (kappa construction), matching how pkg/svg builds circle/ellipse geometry.
func circlePath(cx, cy, r float64) *render.Path {
	const kappa = 0.5522847498
	k := r * kappa
	p := &render.Path{}
	p.MoveTo(cx+r, cy)
	p.CubeTo(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
	p.CubeTo(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
	p.CubeTo(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
	p.CubeTo(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	p.Close()
	return p
}

// TestBuildClipMaskUnionsNonOverlappingChildren is THE discriminating test
// for this primitive: a <clipPath> with two non-overlapping children forms a
// UNION (both must show through), which is the opposite of what repeated
// PushClip would produce (INTERSECTION -> empty, since the two circles share
// no pixels). BuildClipMask must combine per-child rasterized coverage with
// max(), not push each child as its own clip.
func TestBuildClipMaskUnionsNonOverlappingChildren(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	// Two well-separated, non-overlapping circles.
	leftCenter := image.Pt(20, 50)
	rightCenter := image.Pt(80, 50)
	mask := dev.BuildClipMask([]render.MaskPath{
		{Path: circlePath(float64(leftCenter.X), float64(leftCenter.Y), 15), Rule: render.NonZero},
		{Path: circlePath(float64(rightCenter.X), float64(rightCenter.Y), 15), Rule: render.NonZero},
	})

	dev.BeginGroup()
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", mask, nil)

	left := img.RGBAAt(leftCenter.X, leftCenter.Y)
	right := img.RGBAAt(rightCenter.X, rightCenter.Y)
	outside := img.RGBAAt(50, 50) // between the two circles: neither covers it

	t.Logf("left circle center (%d,%d) = %+v", leftCenter.X, leftCenter.Y, left)
	t.Logf("right circle center (%d,%d) = %+v", rightCenter.X, rightCenter.Y, right)
	t.Logf("between circles (50,50) = %+v", outside)

	if left.R < 200 || left.G > 50 {
		t.Errorf("left circle center = %+v, want red (union must show BOTH children, not intersect to empty)", left)
	}
	if right.R < 200 || right.G > 50 {
		t.Errorf("right circle center = %+v, want red (union must show BOTH children, not intersect to empty)", right)
	}
	if outside.R != 255 || outside.G != 255 || outside.B != 255 {
		t.Errorf("between circles = %+v, want unpainted white (outside both children's union)", outside)
	}
}

// TestBuildClipMaskMixedRule verifies each child rasterizes under ITS OWN
// rule: a nonzero child beside an evenodd child must not have one rule
// silently applied to both (which naive path concatenation would do).
func TestBuildClipMaskMixedRule(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	// An evenodd "donut": two concentric squares wound the SAME direction,
	// which evenodd punches a hole through but nonzero would fill solid.
	donut := &render.Path{}
	donut.MoveTo(10, 10)
	donut.LineTo(50, 10)
	donut.LineTo(50, 50)
	donut.LineTo(10, 50)
	donut.Close()
	donut.MoveTo(20, 20)
	donut.LineTo(40, 20)
	donut.LineTo(40, 40)
	donut.LineTo(20, 40)
	donut.Close()

	mask := dev.BuildClipMask([]render.MaskPath{
		{Path: donut, Rule: render.EvenOdd},
		{Path: rectPath(60, 10, 90, 40), Rule: render.NonZero},
	})

	dev.BeginGroup()
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", mask, nil)

	if c := img.RGBAAt(30, 30); c.R != 255 || c.G != 255 {
		t.Errorf("donut hole (evenodd) at (30,30) = %+v, want unpainted white", c)
	}
	if c := img.RGBAAt(15, 15); c.R < 200 {
		t.Errorf("donut ring (evenodd) at (15,15) = %+v, want red", c)
	}
	if c := img.RGBAAt(75, 25); c.R < 200 {
		t.Errorf("nonzero rect at (75,25) = %+v, want red", c)
	}
}

// TestBuildClipMaskOverlappingEvenOdd verifies two overlapping children under
// evenodd union correctly: BuildClipMask rasterizes EACH child separately
// (so overlap is a union of two independently-evaluated evenodd regions),
// rather than concatenating both into one path and evaluating evenodd
// jointly (which would punch a hole where they overlap — the exact
// "overlapped-shapes-with-evenodd" corpus trap).
func TestBuildClipMaskOverlappingEvenOdd(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	a := rectPath(10, 10, 60, 60)
	b := rectPath(40, 40, 90, 90)

	mask := dev.BuildClipMask([]render.MaskPath{
		{Path: a, Rule: render.EvenOdd},
		{Path: b, Rule: render.EvenOdd},
	})

	dev.BeginGroup()
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", mask, nil)

	// The overlap region (40-60,40-60) must still be covered (union), not
	// hollowed out the way joint evenodd over the concatenated outline
	// would produce.
	if c := img.RGBAAt(50, 50); c.R < 200 {
		t.Errorf("overlap of two evenodd rects = %+v, want red (union of independently-evaluated regions)", c)
	}
	if c := img.RGBAAt(20, 20); c.R < 200 {
		t.Errorf("rect A only = %+v, want red", c)
	}
	if c := img.RGBAAt(80, 80); c.R < 200 {
		t.Errorf("rect B only = %+v, want red", c)
	}
}

// TestBuildClipMaskEmptyClipsToNothing verifies an empty (or all-empty-path)
// paths slice returns a mask that covers NOTHING, matching SVG's "an empty
// clipPath clips its target to nothing" rule — distinct from a nil
// GroupMask, which EndGroup treats as "no restriction at all."
func TestBuildClipMaskEmptyClipsToNothing(t *testing.T) {
	dev, img := newTestDevice(100, 100)
	red := color.RGBA{255, 0, 0, 255}

	mask := dev.BuildClipMask(nil)
	if mask == nil {
		t.Fatal("BuildClipMask(nil) returned nil; want a non-nil zero-coverage mask (nil means \"no restriction\" to EndGroup)")
	}

	dev.BeginGroup()
	dev.Fill(rectPath(0, 0, 100, 100), render.FillPaint{Color: red})
	dev.EndGroup(1, "", mask, nil)

	if c := img.RGBAAt(50, 50); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("empty clip mask = %+v, want unpainted white (empty clipPath must clip to nothing)", c)
	}
}

// TestBuildClipMaskCircleAntialiasedEdge is a smoke test that the circle
// helper used above produces real antialiased coverage (not just a
// bounding-box rectangle), guarding against a regression in the test helper
// itself silently making the union test above meaningless.
func TestBuildClipMaskCircleAntialiasedEdge(t *testing.T) {
	dev, _ := newTestDevice(100, 100)
	mask := dev.BuildClipMask([]render.MaskPath{{Path: circlePath(50, 50, 20), Rule: render.NonZero}})
	inscribed := 20.0 / math.Sqrt2
	off := int(inscribed) + 3
	corner := mask.AlphaAt(50-off, 50-off).A
	if corner != 0 {
		t.Errorf("corner outside circle bbox-inscribed area has coverage %d, want 0", corner)
	}
	center := mask.AlphaAt(50, 50).A
	if center != 255 {
		t.Errorf("circle center coverage = %d, want 255 (fully inside)", center)
	}
}
