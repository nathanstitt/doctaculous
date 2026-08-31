package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/internal/css"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout/cssbox"
)

// autoMarginBox builds a static block of the given width with the requested
// horizontal margins, so each CSS 10.3.3 case can be spelled out.
func autoMarginBox(w gcss.Length, mLeft, mRight gcss.Length) *cssbox.Box {
	s := posStyle()
	s.Width = w
	s.Height = px(50)
	s.MarginLeft, s.MarginRight = mLeft, mRight
	return posBox(s, cssbox.PosStatic)
}

// layoutChildX lays a single child out in a cbWidth-wide root and returns its
// border-box X and W.
func layoutChildX(t *testing.T, child *cssbox.Box, cbWidth float64) (x, w float64) {
	t.Helper()
	root := blockBox(gcss.ComputedStyle{Display: "block"}, child)
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, cbWidth)
	if frag == nil || len(frag.Children) != 1 {
		t.Fatalf("want 1 child fragment, got %v", frag)
	}
	return frag.Children[0].X, frag.Children[0].W
}

// TestMarginAutoCenters is the headline case: `margin: 0 auto` on a fixed-width block
// centres it. Before this, usedEdges resolved both auto margins to 0 and the box sat
// hard against the left edge — the most common centering idiom in CSS, silently wrong
// on every document that used it.
func TestMarginAutoCenters(t *testing.T) {
	x, w := layoutChildX(t, autoMarginBox(px(200), autoLen(), autoLen()), 600)
	if !approx(w, 200) {
		t.Fatalf("width = %v, want 200 (auto margins must not change the width)", w)
	}
	if !approx(x, 200) {
		t.Errorf("X = %v, want 200 — (600-200)/2. X=0 means the auto margins were dropped.", x)
	}
}

// TestMarginAutoLeftOnlyPushesRight: a single auto margin absorbs ALL the leftover,
// pushing the box to the opposite edge (CSS 10.3.3), rather than half of it.
func TestMarginAutoLeftOnlyPushesRight(t *testing.T) {
	x, _ := layoutChildX(t, autoMarginBox(px(200), autoLen(), px(0)), 600)
	if !approx(x, 400) {
		t.Errorf("X = %v, want 400 (margin-left:auto pushes the box flush right)", x)
	}
}

// TestMarginAutoRightOnlyStaysLeft: the mirror case. margin-right:auto absorbs the
// leftover, so the box stays at the left edge.
func TestMarginAutoRightOnlyStaysLeft(t *testing.T) {
	x, _ := layoutChildX(t, autoMarginBox(px(200), px(0), autoLen()), 600)
	if !approx(x, 0) {
		t.Errorf("X = %v, want 0 (margin-right:auto leaves the box at the left edge)", x)
	}
}

// TestMarginAutoWithSpecifiedOpposite: `margin-left: 50px; margin-right: auto` keeps
// the specified margin and gives the rest to the auto one — the box lands at 50, not
// centred and not at 0.
func TestMarginAutoWithSpecifiedOpposite(t *testing.T) {
	x, _ := layoutChildX(t, autoMarginBox(px(200), px(50), autoLen()), 600)
	if !approx(x, 50) {
		t.Errorf("X = %v, want 50 (the specified margin is honoured, auto takes the rest)", x)
	}
}

// TestMarginAutoCentersWithinSpecifiedMargins: both margins auto, but the box is
// centred in what remains after... nothing else. Here a border and padding are added to
// prove the leftover is computed from the BORDER box, not the content box: a 100pt
// content box with 20pt padding and 10pt borders occupies 160pt, so it centres at 220.
func TestMarginAutoCentersWithinSpecifiedMargins(t *testing.T) {
	s := posStyle()
	s.Width = px(100)
	s.Height = px(50)
	s.MarginLeft, s.MarginRight = autoLen(), autoLen()
	s.PaddingLeft, s.PaddingRight = px(20), px(20)
	s.BorderLeftWidth, s.BorderRightWidth = px(10), px(10)
	s.BorderLeftStyle, s.BorderRightStyle = "solid", "solid"
	child := posBox(s, cssbox.PosStatic)

	x, w := layoutChildX(t, child, 600)
	if !approx(w, 160) {
		t.Fatalf("border-box width = %v, want 160 (100 + 2*20 padding + 2*10 border)", w)
	}
	if !approx(x, 220) {
		t.Errorf("X = %v, want 220 — (600-160)/2. A content-box leftover would give 250.", x)
	}
}

// TestMarginAutoOverWideBoxDoesNotShiftLeft pins the clamp. A box WIDER than its
// containing block has negative leftover; CSS 10.3.3 treats the auto margin as 0 rather
// than pulling the box backwards. Without the floor this would render at a negative X,
// off the left edge of the page — which no browser does.
func TestMarginAutoOverWideBoxDoesNotShiftLeft(t *testing.T) {
	x, w := layoutChildX(t, autoMarginBox(px(800), autoLen(), autoLen()), 600)
	if !approx(w, 800) {
		t.Fatalf("width = %v, want 800 (the box keeps its specified width and overflows)", w)
	}
	if x < 0 {
		t.Errorf("X = %v, want 0 — a negative auto margin would pull the box off-page", x)
	}
	if !approx(x, 0) {
		t.Errorf("X = %v, want 0 (over-constrained: auto margins resolve to 0)", x)
	}
}

// TestMarginAutoWidthAutoFillsContainer: with `width: auto`, the width absorbs all the
// available space, so there is no leftover and the auto margins stay 0. The box fills
// the container rather than being centred inside a shrunken one — this is why
// `margin: 0 auto` alone does nothing without a width, a behaviour authors rely on.
func TestMarginAutoWidthAutoFillsContainer(t *testing.T) {
	x, w := layoutChildX(t, autoMarginBox(autoLen(), autoLen(), autoLen()), 600)
	if !approx(w, 600) {
		t.Errorf("width = %v, want 600 (width:auto fills; auto margins get no leftover)", w)
	}
	if !approx(x, 0) {
		t.Errorf("X = %v, want 0", x)
	}
}

// TestSpecifiedMarginsUnchanged is the regression guard for every existing document:
// a box with two specified margins must be placed exactly as before, so this change is
// byte-identical for anything not using the idiom.
func TestSpecifiedMarginsUnchanged(t *testing.T) {
	x, w := layoutChildX(t, autoMarginBox(px(200), px(30), px(70)), 600)
	if !approx(x, 30) || !approx(w, 200) {
		t.Errorf("X,W = %v,%v; want 30,200 (specified margins must be untouched)", x, w)
	}
}

// TestMarginAutoPercentWidthCenters: the width may come from a percentage; centering
// works off the USED width either way.
func TestMarginAutoPercentWidthCenters(t *testing.T) {
	x, w := layoutChildX(t, autoMarginBox(gcss.Length{Value: 50, Unit: gcss.UnitPercent}, autoLen(), autoLen()), 600)
	if !approx(w, 300) {
		t.Fatalf("width = %v, want 300 (50%% of 600)", w)
	}
	if !approx(x, 150) {
		t.Errorf("X = %v, want 150 — (600-300)/2", x)
	}
}

// TestMarginAutoBorderBoxSizing: under `box-sizing: border-box` the specified width IS
// the border-box width, so the leftover — and the centre — are computed from that.
func TestMarginAutoBorderBoxSizing(t *testing.T) {
	s := posStyle()
	s.Width = px(200)
	s.Height = px(50)
	s.BoxSizing = "border-box"
	s.MarginLeft, s.MarginRight = autoLen(), autoLen()
	s.PaddingLeft, s.PaddingRight = px(20), px(20)
	child := posBox(s, cssbox.PosStatic)

	x, w := layoutChildX(t, child, 600)
	if !approx(w, 200) {
		t.Fatalf("border-box width = %v, want 200 (border-box sizing)", w)
	}
	if !approx(x, 200) {
		t.Errorf("X = %v, want 200 — (600-200)/2", x)
	}
}
