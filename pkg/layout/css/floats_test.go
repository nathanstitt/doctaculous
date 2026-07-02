package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

const eps = 1e-6

func approx(a, b float64) bool { d := a - b; return d < eps && d > -eps }

// newCtx makes a context spanning [left,right].
func newCtx(left, right float64) *floatContext {
	return &floatContext{cbLeft: left, cbRight: right}
}

// TestEdgesNoFloats: with no floats, the edges are the containing block edges.
func TestEdgesNoFloats(t *testing.T) {
	c := newCtx(0, 200)
	if l := c.leftEdge(0, 20); !approx(l, 0) {
		t.Errorf("leftEdge no floats = %v, want 0", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 200) {
		t.Errorf("rightEdge no floats = %v, want 200", r)
	}
}

// TestLeftFloatNarrowsLeftEdge: a left float pushes the left edge to its right
// margin edge within its vertical band, and only within it.
func TestLeftFloatNarrowsLeftEdge(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{{side: cssbox.FloatLeft, x: 0, y: 0, w: 60, h: 50}}
	if l := c.leftEdge(0, 20); !approx(l, 60) {
		t.Errorf("leftEdge inside band = %v, want 60", l)
	}
	if l := c.leftEdge(60, 20); !approx(l, 0) { // below the float's bottom (y=50)
		t.Errorf("leftEdge below band = %v, want 0", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 200) { // a left float doesn't move the right edge
		t.Errorf("rightEdge with left float = %v, want 200", r)
	}
}

// TestRightFloatNarrowsRightEdge mirrors the left case.
func TestRightFloatNarrowsRightEdge(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{{side: cssbox.FloatRight, x: 150, y: 0, w: 50, h: 50}}
	if r := c.rightEdge(0, 20); !approx(r, 150) {
		t.Errorf("rightEdge inside band = %v, want 150", r)
	}
	if r := c.rightEdge(60, 20); !approx(r, 200) {
		t.Errorf("rightEdge below band = %v, want 200", r)
	}
}

// TestOppositeFloatsNarrowBoth: a left and a right float narrow from both edges in
// the overlapping band.
func TestOppositeFloatsNarrowBoth(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, x: 0, y: 0, w: 40, h: 30},
		{side: cssbox.FloatRight, x: 160, y: 0, w: 40, h: 30},
	}
	if l := c.leftEdge(0, 20); !approx(l, 40) {
		t.Errorf("leftEdge = %v, want 40", l)
	}
	if r := c.rightEdge(0, 20); !approx(r, 160) {
		t.Errorf("rightEdge = %v, want 160", r)
	}
}

// TestPlaceHonorsInsetContainingBlock: a float whose containing block is inset from
// the BFC context (e.g. a padded <body>/<section> between the float and the BFC root)
// is floored/ceiled to that box's content edges, not the wider context edges. This
// pins the fix for a float:left in a padded body being placed at the border-box left
// (ignoring the body's left padding) instead of the content-box left.
func TestPlaceHonorsInsetContainingBlock(t *testing.T) {
	// BFC context spans [0,200]; the float's own containing block is inset to [40,160]
	// (a 40px-padded box). A left float must start at 40, not 0.
	c := newCtx(0, 200)
	fl := c.place(cssbox.FloatLeft, 50, 40, 0, 40, 160)
	if !approx(fl.x, 40) {
		t.Errorf("left float x = %v, want 40 (containing block's left padding honored)", fl.x)
	}
	// A right float in the same inset box must end at the box's right edge (160), so
	// its left edge is 160-50 = 110.
	c2 := newCtx(0, 200)
	fr := c2.place(cssbox.FloatRight, 50, 40, 0, 40, 160)
	if !approx(fr.x, 110) {
		t.Errorf("right float x = %v, want 110 (containing block's right padding honored)", fr.x)
	}
}

// TestPlaceStacksThenWraps: two left floats fit side by side; a third that doesn't
// fit wraps to a new row below the shallower of the two.
func TestPlaceStacksThenWraps(t *testing.T) {
	c := newCtx(0, 200)
	f1 := c.place(cssbox.FloatLeft, 80, 40, 0, c.cbLeft, c.cbRight) // left edge: x=0
	if !approx(f1.x, 0) || !approx(f1.y, 0) {
		t.Fatalf("f1 = %+v, want x=0 y=0", f1)
	}
	f2 := c.place(cssbox.FloatLeft, 80, 60, 0, c.cbLeft, c.cbRight) // next to f1: x=80
	if !approx(f2.x, 80) || !approx(f2.y, 0) {
		t.Fatalf("f2 = %+v, want x=80 y=0", f2)
	}
	// f3 width 80 cannot fit at y=0 (remaining 200-160=40 < 80): nextDropY finds
	// f1 bottom=40 (shallowest overlapping float). At y=40, f2 still occupies [0,60)
	// pushing leftEdge to 160, so still only 40px available — not enough. nextDropY
	// then finds f2 bottom=60. At y=60 no floats overlap: leftEdge=0, 200px available.
	// So f3 ends up at x=0, y=60.
	f3 := c.place(cssbox.FloatLeft, 80, 30, 0, c.cbLeft, c.cbRight)
	if !approx(f3.x, 0) || !approx(f3.y, 60) {
		t.Fatalf("f3 = %+v, want x=0 y=60", f3)
	}
}

// TestPlaceRightStacks: right floats stack from the right edge leftward.
func TestPlaceRightStacks(t *testing.T) {
	c := newCtx(0, 200)
	f1 := c.place(cssbox.FloatRight, 50, 40, 0, c.cbLeft, c.cbRight) // right margin edge at 200 -> x=150
	if !approx(f1.x, 150) || !approx(f1.y, 0) {
		t.Fatalf("f1 = %+v, want x=150 y=0", f1)
	}
	f2 := c.place(cssbox.FloatRight, 50, 40, 0, c.cbLeft, c.cbRight) // next left of f1 -> x=100
	if !approx(f2.x, 100) || !approx(f2.y, 0) {
		t.Fatalf("f2 = %+v, want x=100 y=0", f2)
	}
}

// TestPlaceOverflowWide: a float wider than the band is placed at the edge at the
// requested y (allowed to overflow) rather than looping forever.
func TestPlaceOverflowWide(t *testing.T) {
	c := newCtx(0, 100)
	f := c.place(cssbox.FloatLeft, 250, 30, 10, c.cbLeft, c.cbRight)
	if !approx(f.x, 0) || !approx(f.y, 10) {
		t.Fatalf("overflow-wide float = %+v, want x=0 y=10", f)
	}
}

// TestPlaceNotAboveY: a float never rises above the requested y (content order).
func TestPlaceNotAboveY(t *testing.T) {
	c := newCtx(0, 200)
	f := c.place(cssbox.FloatLeft, 50, 20, 100, c.cbLeft, c.cbRight)
	if f.y < 100-eps {
		t.Errorf("float placed at y=%v, want >= 100", f.y)
	}
}

// TestClearY drops to the bottom of the matching floats.
func TestClearY(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, x: 0, y: 0, w: 40, h: 30},    // bottom 30
		{side: cssbox.FloatRight, x: 160, y: 0, w: 40, h: 70}, // bottom 70
	}
	if y := c.clearY("left", 0); !approx(y, 30) {
		t.Errorf("clearY(left) = %v, want 30", y)
	}
	if y := c.clearY("right", 0); !approx(y, 70) {
		t.Errorf("clearY(right) = %v, want 70", y)
	}
	if y := c.clearY("both", 0); !approx(y, 70) {
		t.Errorf("clearY(both) = %v, want 70", y)
	}
	if y := c.clearY("none", 0); !approx(y, 0) {
		t.Errorf("clearY(none) = %v, want 0", y)
	}
	if y := c.clearY("both", 90); !approx(y, 90) { // already below all floats
		t.Errorf("clearY(both, 90) = %v, want 90", y)
	}
}

// TestFloats2Frags returns the placed floats' fragments in order, skipping nil.
func TestFloats2Frags(t *testing.T) {
	c := newCtx(0, 200)
	if got := c.floats2frags(); got != nil {
		t.Errorf("empty context floats2frags = %v, want nil", got)
	}
	fa, fb := &Fragment{X: 1}, &Fragment{X: 2}
	c.floats = []floatBox{
		{side: cssbox.FloatLeft, frag: fa},
		{side: cssbox.FloatRight, frag: nil}, // skipped
		{side: cssbox.FloatLeft, frag: fb},
	}
	got := c.floats2frags()
	if len(got) != 2 || got[0] != fa || got[1] != fb {
		t.Errorf("floats2frags = %v, want [fa fb] (nil skipped, order preserved)", got)
	}
}

// TestOverlapBoundaryHalfOpen: a query band starting exactly at a float's bottom
// edge does not overlap it (the interval is half-open [y, y+h)), so the edge is
// not narrowed there.
func TestOverlapBoundaryHalfOpen(t *testing.T) {
	c := newCtx(0, 200)
	c.floats = []floatBox{{side: cssbox.FloatLeft, x: 0, y: 0, w: 60, h: 50}}
	// Band [50, 70): float occupies [0, 50); they do not overlap at the boundary.
	if l := c.leftEdge(50, 20); !approx(l, 0) {
		t.Errorf("leftEdge at float bottom boundary = %v, want 0 (half-open, no overlap)", l)
	}
	// Just inside (band [49,69) overlaps [0,50)) the edge IS narrowed.
	if l := c.leftEdge(49, 20); !approx(l, 60) {
		t.Errorf("leftEdge just inside float = %v, want 60", l)
	}
}

// TestMaxBottom: maxBottom returns the largest float bottom (y+h), or 0 for no floats.
func TestMaxBottom(t *testing.T) {
	c := newCtx(0, 200)
	if got := c.maxBottom(); got != 0 {
		t.Errorf("maxBottom (no floats) = %v, want 0", got)
	}
	c.place(cssbox.FloatLeft, 50, 40, 0, c.cbLeft, c.cbRight) // bottom 40
	c.place(cssbox.FloatLeft, 50, 70, 0, c.cbLeft, c.cbRight) // stacks beside; bottom 70
	if got := c.maxBottom(); got != 70 {
		t.Errorf("maxBottom = %v, want 70", got)
	}
}
