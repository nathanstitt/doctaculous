package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// --- helpers for the positioning layout tests ---
//
// These build cssbox.Box trees directly (no HTML/cascade), so they must honor the
// zero-value Length trap: a zero-value Width/Height/MaxWidth/MaxHeight reads as "0",
// and a zero-value Top/Right/Bottom/Left reads as a real "0" offset (NOT auto). The
// helpers default every dimension AND every offset to auto, then the caller sets only
// the offsets it wants.

// autoLen is the auto keyword as a Length. (px, eps, and approx are shared helpers
// declared in positioning_test.go / floats_test.go.)
func autoLen() gcss.Length { return gcss.Length{Unit: gcss.UnitAuto} }

// posStyle returns a ComputedStyle with all four offsets defaulted to auto and the
// width/height/min/max defaults the engine expects, ready for a positioned box.
func posStyle() gcss.ComputedStyle {
	return gcss.ComputedStyle{
		Display:   "block",
		Width:     autoLen(),
		Height:    autoLen(),
		MaxWidth:  autoLen(),
		MaxHeight: autoLen(),
		Top:       autoLen(),
		Right:     autoLen(),
		Bottom:    autoLen(),
		Left:      autoLen(),
	}
}

// posBox builds a block box with the given style and position kind, defaulting any
// unset dimension/offset to auto (so an omitted field does not read as 0).
func posBox(style gcss.ComputedStyle, pos cssbox.PositionKind, kids ...*cssbox.Box) *cssbox.Box {
	if style.Width == (gcss.Length{}) {
		style.Width = autoLen()
	}
	if style.Height == (gcss.Length{}) {
		style.Height = autoLen()
	}
	if style.MaxWidth == (gcss.Length{}) {
		style.MaxWidth = autoLen()
	}
	if style.MaxHeight == (gcss.Length{}) {
		style.MaxHeight = autoLen()
	}
	if style.Top == (gcss.Length{}) {
		style.Top = autoLen()
	}
	if style.Right == (gcss.Length{}) {
		style.Right = autoLen()
	}
	if style.Bottom == (gcss.Length{}) {
		style.Bottom = autoLen()
	}
	if style.Left == (gcss.Length{}) {
		style.Left = autoLen()
	}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: style, Children: kids}
	b.Position = pos
	return b
}

// TestRelativeBoxInFlowUnchangedButFlagged: a relative box keeps its in-flow
// fragment position (the offset is paint-time), is flagged IsPositioned with the
// resolved RelOffsetX/Y, and a following sibling's Y is unchanged (no reflow).
func TestRelativeBoxInFlowUnchangedButFlagged(t *testing.T) {
	eng := New(nil, nil, nil)

	relStyle := posStyle()
	relStyle.Height = px(30)
	relStyle.Top = px(10)
	relStyle.Left = px(20)
	rel := posBox(relStyle, cssbox.PosRelative)

	follow := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(40); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, rel, follow)
	frag := eng.layoutTree(context.Background(), root, 200)
	if frag == nil {
		t.Fatal("nil root fragment")
	}

	// The relative box stays an in-flow child (its space is reserved).
	if len(frag.Children) != 2 {
		t.Fatalf("root has %d in-flow children, want 2 (relative stays in flow)", len(frag.Children))
	}
	rf := frag.Children[0]
	// Its fragment is at the UN-offset in-flow position: first child border top at y=0.
	if !approx(rf.Y, 0) {
		t.Errorf("relative fragment Y=%v, want 0 (in-flow, offset is paint-time)", rf.Y)
	}
	if !rf.IsPositioned {
		t.Errorf("relative fragment not flagged IsPositioned")
	}
	if !rf.IsStackingContext {
		t.Errorf("relative fragment not flagged IsStackingContext")
	}
	if !approx(rf.RelOffsetX, 20) || !approx(rf.RelOffsetY, 10) {
		t.Errorf("RelOffset = (%v,%v), want (20,10)", rf.RelOffsetX, rf.RelOffsetY)
	}
	// The relative box reserved 30pt of space; the following sibling sits below it
	// unchanged (no reflow from the offset).
	ff := frag.Children[1]
	if !approx(ff.Y, 30) {
		t.Errorf("following sibling Y=%v, want 30 (below the relative box's reserved space)", ff.Y)
	}
	// The relative box is lifted into the root's positioned layer (painted once there).
	if len(frag.Positioned) != 1 || frag.Positioned[0] != rf {
		t.Errorf("relative box not in root.Positioned (got %d entries)", len(frag.Positioned))
	}
}

// TestAbsoluteRemovedFromFlow: an absolute box between two normal blocks does not
// occupy in-flow space — the second normal block stacks directly under the first.
func TestAbsoluteRemovedFromFlow(t *testing.T) {
	eng := New(nil, nil, nil)

	first := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(20); return s }(), cssbox.PosStatic)

	absStyle := posStyle()
	absStyle.Height = px(50)
	absStyle.Top = px(5)
	absStyle.Left = px(5)
	abs := posBox(absStyle, cssbox.PosAbsolute)

	second := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(20); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, first, abs, second)
	frag := eng.layoutTree(context.Background(), root, 200)

	// The abs box is NOT an in-flow child.
	if len(frag.Children) != 2 {
		t.Fatalf("root has %d in-flow children, want 2 (abs box out of flow)", len(frag.Children))
	}
	// first at y=0 (h=20); second stacks directly under it at y=20 (as if abs absent).
	if !approx(frag.Children[0].Y, 0) {
		t.Errorf("first block Y=%v, want 0", frag.Children[0].Y)
	}
	if !approx(frag.Children[1].Y, 20) {
		t.Errorf("second block Y=%v, want 20 (abs box did not consume space)", frag.Children[1].Y)
	}
	// The abs box was positioned (against the page, no positioned ancestor) and lifted
	// to the root's positioned layer.
	if len(frag.Positioned) != 1 {
		t.Fatalf("root.Positioned = %d, want 1 (the abs box)", len(frag.Positioned))
	}
	af := frag.Positioned[0]
	if !af.IsPositioned || !af.IsStackingContext {
		t.Errorf("abs fragment flags: IsPositioned=%v IsStackingContext=%v, want both true", af.IsPositioned, af.IsStackingContext)
	}
}

// TestAbsoluteAgainstRelativeAncestor: an absolute child top:0;left:0 inside a
// relative container at some page Y lands at the container's content-box origin
// (NOT the page origin).
func TestAbsoluteAgainstRelativeAncestor(t *testing.T) {
	eng := New(nil, nil, nil)

	absStyle := posStyle()
	absStyle.Width = px(30)
	absStyle.Height = px(30)
	absStyle.Top = px(0)
	absStyle.Left = px(0)
	abs := posBox(absStyle, cssbox.PosAbsolute)

	// A relative container with padding so its content box is inset from its border box.
	contStyle := posStyle()
	contStyle.Height = px(100)
	contStyle.PaddingTop = px(7)
	contStyle.PaddingLeft = px(11)
	cont := posBox(contStyle, cssbox.PosRelative, abs)

	// A spacer above the container so the container sits at a nonzero page Y.
	spacer := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(40); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, spacer, cont)
	frag := eng.layoutTree(context.Background(), root, 200)

	// The container is the 2nd in-flow child; it sits at page Y = 40 (after the spacer).
	if len(frag.Children) != 2 {
		t.Fatalf("root has %d in-flow children, want 2", len(frag.Children))
	}
	cf := frag.Children[1]
	if !approx(cf.Y, 40) {
		t.Fatalf("container Y=%v, want 40", cf.Y)
	}
	// The abs child is in the container's Positioned layer.
	if len(cf.Positioned) != 1 {
		t.Fatalf("container.Positioned = %d, want 1 (the abs child)", len(cf.Positioned))
	}
	af := cf.Positioned[0]
	// top:0;left:0 against the container CONTENT box origin = border origin + padding.
	wantX := cf.X + 11
	wantY := cf.Y + 7
	if !approx(af.X, wantX) || !approx(af.Y, wantY) {
		t.Errorf("abs child at (%v,%v), want (%v,%v) = container content-box origin", af.X, af.Y, wantX, wantY)
	}
}

// TestFixedAgainstPage: a fixed child top:0;left:0 lands at page (0,0) regardless of
// a positioned ancestor (the containing block is the viewport == the page).
func TestFixedAgainstPage(t *testing.T) {
	eng := New(nil, nil, nil)

	fixedStyle := posStyle()
	fixedStyle.Width = px(30)
	fixedStyle.Height = px(30)
	fixedStyle.Top = px(0)
	fixedStyle.Left = px(0)
	fx := posBox(fixedStyle, cssbox.PosFixed)

	// A relative container at a nonzero page Y, with padding — the fixed child must
	// IGNORE it and resolve against the page.
	contStyle := posStyle()
	contStyle.Height = px(100)
	contStyle.PaddingTop = px(7)
	contStyle.PaddingLeft = px(11)
	cont := posBox(contStyle, cssbox.PosRelative, fx)

	spacer := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(40); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, spacer, cont)
	frag := eng.layoutTree(context.Background(), root, 200)

	// The relative container is the 2nd in-flow child; it (being positioned) also
	// bubbles to the root's positioned layer. The KEY facts: the fixed box attaches to
	// the ROOT (page CB), NOT to the relative container, and lands at the page origin.
	cf := frag.Children[1]
	// The relative container must NOT own the fixed box (its CB is the page).
	if len(cf.Positioned) != 0 {
		t.Errorf("relative container.Positioned = %d, want 0 (fixed goes to the page)", len(cf.Positioned))
	}
	// Find the fixed box among the root's positioned layer (the entry that is not the
	// relative container fragment cf — the container is there too, since it is relative).
	var ff *Fragment
	for _, p := range frag.Positioned {
		if p != cf {
			ff = p
		}
	}
	if ff == nil {
		t.Fatalf("fixed box not found in root.Positioned (got %d entries)", len(frag.Positioned))
	}
	if !approx(ff.X, 0) || !approx(ff.Y, 0) {
		t.Errorf("fixed box at (%v,%v), want (0,0) (page top-left)", ff.X, ff.Y)
	}
}

// TestAbsoluteNoAncestorIsPage: an abs box with no positioned ancestor lands against
// the page (the ICB), at top:left offsets from page origin.
func TestAbsoluteNoAncestorIsPage(t *testing.T) {
	eng := New(nil, nil, nil)

	absStyle := posStyle()
	absStyle.Width = px(30)
	absStyle.Height = px(30)
	absStyle.Top = px(15)
	absStyle.Left = px(25)
	abs := posBox(absStyle, cssbox.PosAbsolute)

	// A static container (NOT a positioned ancestor) at a nonzero page Y.
	cont := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(100); return s }(), cssbox.PosStatic, abs)
	spacer := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(40); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, spacer, cont)
	frag := eng.layoutTree(context.Background(), root, 200)

	// No positioned ancestor => CB is the page => attaches to the root.
	if len(frag.Positioned) != 1 {
		t.Fatalf("root.Positioned = %d, want 1", len(frag.Positioned))
	}
	af := frag.Positioned[0]
	// Against the page: left=25, top=15 from page origin (0,0).
	if !approx(af.X, 25) || !approx(af.Y, 15) {
		t.Errorf("abs box at (%v,%v), want (25,15) (page-relative)", af.X, af.Y)
	}
}

// TestAbsoluteLeftRightAutoWidthLaidOut: an abs box with both left+right specified and
// width:auto is sized by the constraint solve (width = cb.w - left - right) — the used
// width must reach the laid-out fragment, not just absRect. Regression for the abs-pos
// pass laying the interior out at the full containing-block width and ignoring the
// offset-derived width.
func TestAbsoluteLeftRightAutoWidthLaidOut(t *testing.T) {
	eng := New(nil, nil, nil)

	// width:auto (left at default-auto in posStyle), both offsets set, in a 200pt page.
	absStyle := posStyle()
	absStyle.Left = px(30)
	absStyle.Right = px(50)
	absStyle.Height = px(20)
	abs := posBox(absStyle, cssbox.PosAbsolute)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, abs)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Positioned) != 1 {
		t.Fatalf("root.Positioned = %d, want 1", len(frag.Positioned))
	}
	af := frag.Positioned[0]
	// border-box left = cb.x + left = 30; width = cb.w - left - right = 200-30-50 = 120.
	if !approx(af.X, 30) {
		t.Errorf("abs X = %v, want 30 (cb left + left offset)", af.X)
	}
	if !approx(af.W, 120) {
		t.Errorf("abs W = %v, want 120 (cb.w - left - right); the offset-derived width must reach the fragment", af.W)
	}
}

// TestPositionedFloatPlacedAndOffset: float:left; position:relative; top:5; left:5 —
// the float fragment is at the float edge, IsFloat, carrying RelOffsetX/Y == (5,5),
// and is NOT additionally placed on a Positioned slice (it paints via the Floats
// layer).
func TestPositionedFloatPlacedAndOffset(t *testing.T) {
	eng := New(nil, nil, nil)

	fStyle := posStyle()
	fStyle.Float = "left"
	fStyle.Width = px(60)
	fStyle.Height = px(40)
	fStyle.Top = px(5)
	fStyle.Left = px(5)
	floated := posBox(fStyle, cssbox.PosRelative)
	floated.Float = cssbox.FloatLeft

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Floats) != 1 {
		t.Fatalf("root.Floats = %d, want 1", len(frag.Floats))
	}
	ff := frag.Floats[0]
	if !ff.IsFloat {
		t.Errorf("float fragment not marked IsFloat")
	}
	// Placed at the float edge (content-box left/top of the BFC root = 0,0).
	if !approx(ff.X, 0) || !approx(ff.Y, 0) {
		t.Errorf("float at (%v,%v), want (0,0) (float edge)", ff.X, ff.Y)
	}
	if !ff.IsPositioned {
		t.Errorf("positioned float not flagged IsPositioned")
	}
	if !approx(ff.RelOffsetX, 5) || !approx(ff.RelOffsetY, 5) {
		t.Errorf("float RelOffset = (%v,%v), want (5,5)", ff.RelOffsetX, ff.RelOffsetY)
	}
	// It paints via the Floats layer, NOT via a Positioned slice.
	if len(frag.Positioned) != 0 {
		t.Errorf("root.Positioned = %d, want 0 (positioned float paints via Floats)", len(frag.Positioned))
	}
}

// TestFloatWithRelativeChild: a relatively-positioned box inside a float is in flow,
// so it is moved into place by the float's own placement shift exactly ONCE — it must
// not be double-translated by the placement delta. Regression test for the placeFloat
// double-translate bug (a relative box is an in-flow Child already moved by
// translateFragment, so re-translating its Positioned/pending entries shifts it twice).
func TestFloatWithRelativeChild(t *testing.T) {
	eng := New(nil, nil, nil)

	// A relative child inside the float, with NO offset (top/left auto) so its painted
	// position equals its in-flow position inside the float — making a double-shift
	// observable as a wrong X.
	relChild := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(20); return s }(), cssbox.PosRelative)

	// A RIGHT float (large placement delta), width 60 in a 200 CB → content-box left 140.
	fStyle := posStyle()
	fStyle.Float = "right"
	fStyle.Width = px(60)
	fStyle.Height = px(40)
	floated := posBox(fStyle, cssbox.PosStatic, relChild)
	floated.Float = cssbox.FloatRight

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Floats) != 1 {
		t.Fatalf("root.Floats = %d, want 1", len(frag.Floats))
	}
	ff := frag.Floats[0]
	if !approx(ff.X, 140) {
		t.Fatalf("float X = %v, want 140 (right float of width 60 in 200)", ff.X)
	}
	// The relative child bubbled onto the float's Positioned layer and sits at the
	// float's content origin (X=140), NOT double-shifted to 280.
	if len(ff.Positioned) != 1 {
		t.Fatalf("float.Positioned = %d, want 1 (the relative child)", len(ff.Positioned))
	}
	rc := ff.Positioned[0]
	if !approx(rc.X, 140) {
		t.Errorf("relative child X = %v, want 140 (280 would be a double-translate)", rc.X)
	}
	// It is the same fragment as the float's in-flow child (painted once, via the
	// float's positioned layer; skipped in the in-flow passes).
	if len(ff.Children) != 1 || ff.Children[0] != rc {
		t.Errorf("relative child is not the float's in-flow Child (aliasing broken)")
	}
}

// TestAbsFloatCollectedNotFloated: position:absolute; float:left — box-gen forces
// Float to none, so the box is collected as abs-pos (Positioned layer), NOT in the
// float layer. (Here we set both on the box to mimic the pre-blockify state and
// confirm the layout engine treats abs/fixed as out-of-flow BEFORE the float branch;
// box-gen normally zeroes Float, but the engine's ordering guarantees correctness.)
func TestAbsFloatCollectedNotFloated(t *testing.T) {
	eng := New(nil, nil, nil)

	// Per the box-gen contract (build.go forces Float=none on an abs/fixed box), the
	// box arrives with Float == FloatNone. We assert the engine collects it as abs-pos.
	absStyle := posStyle()
	absStyle.Width = px(40)
	absStyle.Height = px(40)
	absStyle.Top = px(0)
	absStyle.Left = px(0)
	abs := posBox(absStyle, cssbox.PosAbsolute)
	if abs.Float != cssbox.FloatNone {
		t.Fatalf("test setup: abs box Float=%v, want FloatNone (box-gen forces this)", abs.Float)
	}

	root := blockBox(gcss.ComputedStyle{Display: "block"}, abs)
	frag := eng.layoutTree(context.Background(), root, 200)

	// Not in the float layer.
	if len(frag.Floats) != 0 {
		t.Errorf("root.Floats = %d, want 0 (abs box is not a float)", len(frag.Floats))
	}
	// Collected as abs-pos and resolved onto the root's positioned layer.
	if len(frag.Positioned) != 1 {
		t.Fatalf("root.Positioned = %d, want 1 (abs box)", len(frag.Positioned))
	}
	if frag.Positioned[0].IsFloat {
		t.Errorf("abs fragment marked IsFloat, want not a float")
	}
}

// TestRelativeParentAbsChildPaintCoords: the spec's load-bearing test — a relative
// parent offset (top:10,left:20) containing an absolute child at (top:0,left:0): the
// child's PAINTED item coordinates land at the parent's in-flow content origin +
// (20,10). This exercises the flattened-range translate end-to-end (the abs child
// rides the parent's paint-time relative shift via AppendItems, NOT translateFragment).
func TestRelativeParentAbsChildPaintCoords(t *testing.T) {
	eng := New(nil, nil, nil)

	// The abs child has a background so it emits a BackgroundKind item we can find.
	absStyle := posStyle()
	absStyle.Width = px(20)
	absStyle.Height = px(20)
	absStyle.Top = px(0)
	absStyle.Left = px(0)
	abs := posBox(absStyle, cssbox.PosAbsolute)
	abs.Style.BackgroundColor = color.RGBA{1, 2, 3, 255}

	relStyle := posStyle()
	relStyle.Height = px(50)
	relStyle.Top = px(10)
	relStyle.Left = px(20)
	rel := posBox(relStyle, cssbox.PosRelative, abs)

	// A spacer so the relative parent's in-flow content origin is at a nonzero page Y.
	spacer := posBox(func() gcss.ComputedStyle { s := posStyle(); s.Height = px(30); return s }(), cssbox.PosStatic)

	root := blockBox(gcss.ComputedStyle{Display: "block"}, spacer, rel)
	frag := eng.layoutTree(context.Background(), root, 200)

	// The relative parent is the 2nd in-flow child; its in-flow content origin (no
	// border/padding) is its border origin (X=0, Y=30).
	rf := frag.Children[1]
	if !approx(rf.X, 0) || !approx(rf.Y, 30) {
		t.Fatalf("relative parent in-flow at (%v,%v), want (0,30)", rf.X, rf.Y)
	}

	// Flatten the whole tree and find the abs child's background item (color 1,2,3).
	items := frag.AppendItems(nil)
	var bg *layout.Item
	for i := range items {
		if items[i].Kind == layout.BackgroundKind && items[i].Rule.Color == (color.RGBA{1, 2, 3, 255}) {
			bg = &items[i]
			break
		}
	}
	if bg == nil {
		t.Fatalf("abs child background item not found among %d items", len(items))
	}
	// The abs child was positioned at the parent's in-flow content origin (0,30) +
	// (top:0,left:0) = (0,30) in fragment coords; then the parent's relative offset
	// (left:20,top:10) is applied at paint time over the parent's flattened range,
	// shifting the child's painted item to (20,40).
	wantX := rf.X + 20 // parent content origin X (0) + rel left (20)
	wantY := rf.Y + 10 // parent content origin Y (30) + rel top (10)
	if !approx(bg.Rule.XPt, wantX) || !approx(bg.Rule.YPt, wantY) {
		t.Errorf("abs child painted at (%v,%v), want (%v,%v) = parent content origin + relative offset",
			bg.Rule.XPt, bg.Rule.YPt, wantX, wantY)
	}
}
