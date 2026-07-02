package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// A float's outer top may not be higher than the margin-box bottom of an earlier
// block in the same containing block (CSS 2.1 §9.5). When a collapsed margin (or
// clearance) opens a gap between the float's provisional band origin and its
// containing block's resolved border-box top, the float — which attaches to the BFC
// root's float list rather than to the shifted in-flow subtree — used to be left too
// high by that gap. These tests exercise the fix (block.go: shiftFloatsFrom after the
// in-flow shift) at the fragment-tree level, before any pagination.

// firstFloatFrag returns the first floated fragment found anywhere in the tree.
func firstFloatFrag(root *Fragment) *Fragment {
	var found *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil || found != nil {
			return
		}
		if f.IsFloat {
			found = f
			return
		}
		for _, c := range f.Children {
			walk(c)
		}
		for _, fl := range f.Floats {
			walk(fl)
		}
	}
	walk(root)
	return found
}

// fragWithBottomBorderColor returns the first non-float fragment whose bottom border
// has color (r,g,b) — used to locate a specific block (e.g. the lede) structurally.
func fragWithBottomBorderColor(root *Fragment, r, g, b uint8) *Fragment {
	var found *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil || found != nil {
			return
		}
		e := f.Border[layout.EdgeBottom]
		if !f.IsFloat && e.Width > 0 && e.Color.R == r && e.Color.G == g && e.Color.B == b {
			found = f
			return
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

func layoutTreeSrc(t *testing.T, src string, w float64) *Fragment {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	frag := New(layoutfont.NewFaceCacheWithFonts(nil, nil, nil, nil), nil, nil).layoutTree(context.Background(), root, w)
	if frag == nil {
		t.Fatal("layoutTree returned nil")
	}
	return frag
}

// TestFloatNotHigherThanPrevSiblingMarginBottom is the core regression: a float whose
// immediately-preceding in-flow sibling carries a bottom margin must sit at (or below)
// that sibling's MARGIN-box bottom — not at its border-box bottom. The preceding block
// has a border-bottom (to mark its border-box bottom) and a 20px bottom margin.
func TestFloatNotHigherThanPrevSiblingMarginBottom(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .prev { height: 40px; border-bottom: 2px solid #c9c2b0; margin-bottom: 20px }
	  .fig  { float: left; width: 50px; height: 30px; margin: 0 }
	</style></head><body>
	  <div class="prev"></div>
	  <div class="fig"></div>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	prev := fragWithBottomBorderColor(root, 0xc9, 0xc2, 0xb0)
	fig := firstFloatFrag(root)
	if prev == nil {
		t.Fatal("previous block (with bottom border) not found")
	}
	if fig == nil {
		t.Fatal("float fragment not found")
	}
	prevMarginBottom := prev.Y + prev.H + 20 // border-box bottom + margin-bottom
	// The float has margin:0, so its margin-box top == its border-box top (fig.Y).
	if fig.Y < prevMarginBottom-eps {
		t.Errorf("float top %.2f is above prev margin-box bottom %.2f (by %.2f); float ignored the preceding sibling's bottom margin",
			fig.Y, prevMarginBottom, prevMarginBottom-fig.Y)
	}
	// And it should sit EXACTLY at the margin-box bottom (nothing pushes it lower here).
	if !approx(fig.Y, prevMarginBottom) {
		t.Errorf("float top %.2f, want exactly %.2f (prev border-box bottom + margin-bottom)", fig.Y, prevMarginBottom)
	}
}

// TestFloatAfterCollapsedSiblingMargins reproduces the showcase mechanism: the float is
// inside the SECOND section; the first section's margin-bottom (26) collapses into the
// gap before the second section. The float (after a lede inside the second section) must
// still land at the lede's margin-box bottom — the collapsed inter-section margin must
// not lift it. This is the exact case the handoff described.
func TestFloatAfterCollapsedSiblingMargins(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .section { margin-bottom: 26px }
	  .lede { height: 20px; border-bottom: 1px solid #c9c2b0; padding-bottom: 10px; margin-bottom: 14px }
	  .fig  { float: left; width: 60px; height: 40px; margin: 4px 0 0 0 }
	</style></head><body>
	  <section class="section"><div style="height:30px">first</div></section>
	  <section class="section">
	    <p class="lede"></p>
	    <div class="fig"></div>
	    <p style="height:20px">after</p>
	  </section>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	lede := fragWithBottomBorderColor(root, 0xc9, 0xc2, 0xb0)
	fig := firstFloatFrag(root)
	if lede == nil {
		t.Fatal("lede (bottom-bordered block) not found")
	}
	if fig == nil {
		t.Fatal("float fragment not found")
	}
	ledeMarginBottom := lede.Y + lede.H + 14
	figMarginTop := fig.Y - 4 // fig has margin-top 4
	if figMarginTop < ledeMarginBottom-eps {
		t.Errorf("float margin-top %.2f is above lede margin-box bottom %.2f (by %.2f); the collapsed inter-section margin lifted the float",
			figMarginTop, ledeMarginBottom, ledeMarginBottom-figMarginTop)
	}
	if !approx(figMarginTop, ledeMarginBottom) {
		t.Errorf("float margin-top %.2f, want exactly lede margin-box bottom %.2f", figMarginTop, ledeMarginBottom)
	}
}

// TestFloatByteIdenticalWhenNoCollapse guards the no-op path: when the preceding sibling
// has NO bottom margin, the fix must not move the float at all — the float sits exactly at
// the previous block's border-box bottom, as before. (This is the common case that must
// stay byte-identical.)
func TestFloatByteIdenticalWhenNoCollapse(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .prev { height: 40px; border-bottom: 2px solid #c9c2b0; margin-bottom: 0 }
	  .fig  { float: left; width: 50px; height: 30px; margin: 0 }
	</style></head><body>
	  <div class="prev"></div>
	  <div class="fig"></div>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	prev := fragWithBottomBorderColor(root, 0xc9, 0xc2, 0xb0)
	fig := firstFloatFrag(root)
	if prev == nil || fig == nil {
		t.Fatal("prev or float fragment not found")
	}
	if !approx(fig.Y, prev.Y+prev.H) {
		t.Errorf("float top %.2f, want %.2f (prev border-box bottom, no margin); the no-collapse case must be unchanged", fig.Y, prev.Y+prev.H)
	}
}

// TestShiftFloatsFromGeometryAndFragment unit-tests the primitive the fix uses: it must
// move BOTH the avoidance geometry (floatBox.y, so later floats/content avoid the moved
// box) and the placed fragment's Y, only for floats at index >= from, and be a no-op for
// dy==0.
func TestShiftFloatsFromGeometryAndFragment(t *testing.T) {
	c := newCtx(0, 200)
	f0 := &Fragment{Y: 10}
	f1 := &Fragment{Y: 50}
	c.floats = []floatBox{
		{side: 0, x: 0, y: 10, w: 40, h: 30, frag: f0},
		{side: 0, x: 0, y: 50, w: 40, h: 30, frag: f1},
	}
	// Shift only from index 1 by +15.
	c.shiftFloatsFrom(1, 15)
	if !approx(c.floats[0].y, 10) || !approx(f0.Y, 10) {
		t.Errorf("float[0] moved (y=%.2f frag.Y=%.2f); only index>=1 should move", c.floats[0].y, f0.Y)
	}
	if !approx(c.floats[1].y, 65) {
		t.Errorf("float[1].y = %.2f, want 65 (geometry moved)", c.floats[1].y)
	}
	if !approx(f1.Y, 65) {
		t.Errorf("float[1].frag.Y = %.2f, want 65 (fragment moved)", f1.Y)
	}
	// dy==0 is a no-op.
	c.shiftFloatsFrom(0, 0)
	if !approx(c.floats[1].y, 65) || !approx(f1.Y, 65) {
		t.Errorf("dy==0 must be a no-op; got y=%.2f frag.Y=%.2f", c.floats[1].y, f1.Y)
	}
}

// TestFloatInNestedBFCNotDoubleShifted guards the interaction with nested BFCs: a float
// declared inside a child that establishes its OWN BFC (here overflow:hidden) is placed in
// the child's own float context and rides the child's in-flow shift via res.frag — it must
// NOT also be corrected by the parent's shiftFloatsFrom (which only touches floats in the
// PARENT's context). The child BFC is gapped from its predecessor by a collapsed margin, so
// a double-shift would push the float DOWN past its correct spot. The float should sit at
// the top of the BFC child's content box.
func TestFloatInNestedBFCNotDoubleShifted(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .prev { height: 30px; margin-bottom: 25px }
	  .bfc  { overflow: hidden; border: 1px solid #000 }
	  .fig  { float: left; width: 40px; height: 20px; margin: 0 }
	</style></head><body>
	  <div class="prev"></div>
	  <div class="bfc"><div class="fig"></div><p style="height:20px">beside</p></div>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	fig := firstFloatFrag(root)
	if fig == nil {
		t.Fatal("float fragment not found")
	}
	// The BFC child's border-box top = prev border-box bottom(30) + collapsed margin(25) = 55.
	// Its content box starts at 55 + border(1) = 56. The float (margin 0) sits at 56 (the BFC's
	// content-box top), NOT double-shifted a further 25px (to ~81) by the parent's correction —
	// the parent's shiftFloatsFrom only touches floats in the PARENT's context, and this float is
	// in the nested BFC's own context (it rides res.frag's shift instead).
	if !approx(fig.Y, 56) {
		t.Errorf("float in nested BFC at Y=%.2f, want 56 (BFC content-box top); a double-shift would move it further", fig.Y)
	}
}

// TestFloatInBorderedBFCInsetByOwnPadding pins the related fix (block.go: shift a nested
// BFC's floats by contentTopY): a float that is a direct child of a BFC box with its OWN top
// border + padding must sit at the BFC's CONTENT-box top-left — inset by that border+padding
// on BOTH axes — not at its border-box top (Y was previously short by the top border+padding).
// The BFC also grows to enclose the float below it.
func TestFloatInBorderedBFCInsetByOwnPadding(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .bfc { overflow: hidden; border: 4px solid #000; padding: 6px }
	  .fig { float: left; width: 40px; height: 80px; margin: 0 }
	</style></head><body>
	  <div class="bfc"><div class="fig"></div></div>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	var bfc *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil || bfc != nil {
			return
		}
		if f.Border[layout.EdgeTop].Width == 4 {
			bfc = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	fig := firstFloatFrag(root)
	if bfc == nil || fig == nil {
		t.Fatal("bfc or float fragment not found")
	}
	// Content-box top-left = border(4) + padding(6) = 10 on each axis (BFC at origin).
	if !approx(fig.Y, 10) {
		t.Errorf("float Y=%.2f, want 10 (BFC content-box top = border 4 + padding 6); Y was not inset by the BFC's own top border+padding", fig.Y)
	}
	if !approx(fig.X, 10) {
		t.Errorf("float X=%.2f, want 10 (BFC content-box left)", fig.X)
	}
	// The BFC encloses the float: H = float content(80) + padding(12) + border(8) = 100.
	if !approx(bfc.H, 100) {
		t.Errorf("BFC H=%.2f, want 100 (encloses the float: 80 + padding 12 + border 8)", bfc.H)
	}
}

// TestRightFloatAfterCollapsedMargin verifies the correction is side-agnostic (a pure
// vertical shift): a right float in a section preceded by a margin-bottom sibling lands at
// the lede's margin-box bottom just like a left float, and stays pinned to the right edge.
func TestRightFloatAfterCollapsedMargin(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  .section { margin-bottom: 22px }
	  .lede { height: 18px; border-bottom: 1px solid #c9c2b0; margin-bottom: 12px }
	  .fig  { float: right; width: 60px; height: 40px; margin: 0 }
	</style></head><body>
	  <section class="section"><div style="height:25px">first</div></section>
	  <section class="section">
	    <p class="lede"></p>
	    <div class="fig"></div>
	  </section>
	</body></html>`
	root := layoutTreeSrc(t, src, 300)
	lede := fragWithBottomBorderColor(root, 0xc9, 0xc2, 0xb0)
	fig := firstFloatFrag(root)
	if lede == nil || fig == nil {
		t.Fatal("lede or float fragment not found")
	}
	ledeMarginBottom := lede.Y + lede.H + 12
	if !approx(fig.Y, ledeMarginBottom) {
		t.Errorf("right float top %.2f, want lede margin-box bottom %.2f", fig.Y, ledeMarginBottom)
	}
	// Right float pinned to the right content edge (viewport 300, float width 60 → X=240).
	if !approx(fig.X, 240) {
		t.Errorf("right float X=%.2f, want 240 (pinned to right edge, unaffected by the vertical correction)", fig.X)
	}
}
