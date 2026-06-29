package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// findByBox walks a fragment tree (Children + Positioned + Floats) and returns
// the first fragment whose Box == target, or nil.
func findByBox(f *Fragment, target *cssbox.Box) *Fragment {
	if f == nil {
		return nil
	}
	if f.Box == target {
		return f
	}
	for _, c := range f.Children {
		if g := findByBox(c, target); g != nil {
			return g
		}
	}
	for _, p := range f.Positioned {
		if g := findByBox(p, target); g != nil {
			return g
		}
	}
	for _, fl := range f.Floats {
		if g := findByBox(fl, target); g != nil {
			return g
		}
	}
	return nil
}

// TestPositionedFragmentCarriesBox: a relatively-positioned box's fragment retains a
// pointer to its source cssbox.Box (so the flatten z-sort can read Box.Style.ZIndex).
func TestPositionedFragmentCarriesBox(t *testing.T) {
	eng := New(nil, nil, nil)
	relStyle := posStyle()
	relStyle.Height = gcss.Length{Value: 20, Unit: gcss.UnitPx}
	relStyle.BackgroundColor = color.RGBA{1, 2, 3, 255}
	rel := posBox(relStyle, cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, rel)

	frag := eng.layoutTree(context.Background(), root, 200)
	got := findByBox(frag, rel)
	if got == nil {
		t.Fatal("relative fragment not found in tree")
	}
	if got.Box != rel {
		t.Errorf("frag.Box = %p, want the source box %p", got.Box, rel)
	}
}

// zfill returns a posStyle with a px height, a background color, and a z-index. Using
// px offsets (top/left) places the boxes so their backgrounds overlap in page space,
// making paint order observable via bgIndex.
func zfill(h float64, bg color.RGBA, top, left float64, z int, auto bool) gcss.ComputedStyle {
	s := posStyle()
	s.Height = gcss.Length{Value: h, Unit: gcss.UnitPx}
	s.Width = gcss.Length{Value: h, Unit: gcss.UnitPx}
	s.BackgroundColor = bg
	s.Top = gcss.Length{Value: top, Unit: gcss.UnitPx}
	s.Left = gcss.Length{Value: left, Unit: gcss.UnitPx}
	s.ZIndex = z
	s.ZIndexAuto = auto
	return s
}

// TestZNegativeBehindInFlowContent: a position:relative; z-index:-1 box paints BEFORE
// (behind) an in-flow sibling block's background. (The load-bearing assertion —
// negatives emit before decorations.)
func TestZNegativeBehindInFlowContent(t *testing.T) {
	eng := New(nil, nil, nil)
	negBG := color.RGBA{10, 0, 0, 255}
	flowBG := color.RGBA{0, 20, 0, 255}

	neg := posBox(zfill(40, negBG, 0, 0, -1, false), cssbox.PosRelative)
	flow := posBox(func() gcss.ComputedStyle {
		s := posStyle()
		s.Height = gcss.Length{Value: 40, Unit: gcss.UnitPx}
		s.BackgroundColor = flowBG
		return s
	}(), cssbox.PosStatic)
	root := posBox(posStyle(), cssbox.PosStatic, neg, flow)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ni, fi := bgIndex(items, negBG), bgIndex(items, flowBG)
	if ni < 0 || fi < 0 {
		t.Fatalf("missing backgrounds: neg=%d flow=%d", ni, fi)
	}
	if ni >= fi {
		t.Errorf("negative-z background at %d must paint BEFORE in-flow content at %d", ni, fi)
	}
}

// TestZNegativeBehindHostOwnBackground pins F-C (adversarial-review finding): a
// stacking context with its OWN background containing a position:relative; z-index:-1
// descendant must paint its own background BEFORE the negative-z descendant (CSS 2.1
// Appendix E: own background = step 1, negative-z = step 2). The bug was that the
// non-clipping AppendItems branch emitted negatives before the context's own
// decorations, so the host background painted OVER (hid) the negative-z child. Mutation-
// verify: restore `appendBand(negatives) ; appendDecorations` order in AppendItems and
// the host background lands AFTER the negative child, so this FAILS.
func TestZNegativeBehindHostOwnBackground(t *testing.T) {
	eng := New(nil, nil, nil)
	hostBG := color.RGBA{30, 30, 30, 255}
	negBG := color.RGBA{90, 10, 10, 255}

	// A relative host WITH a background, containing a z-index:-1 relative child that
	// overlaps it. Appendix E: host bg first, then the negative child (which thus peeks
	// out where it extends beyond / on top of the host's own background region).
	neg := posBox(zfill(60, negBG, 10, 10, -1, false), cssbox.PosRelative)
	hostStyle := posStyle()
	hostStyle.Height = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	hostStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	hostStyle.BackgroundColor = hostBG
	host := posBox(hostStyle, cssbox.PosRelative, neg)
	root := posBox(posStyle(), cssbox.PosStatic, host)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	hi, ni := bgIndex(items, hostBG), bgIndex(items, negBG)
	if hi < 0 || ni < 0 {
		t.Fatalf("missing backgrounds: host=%d neg=%d", hi, ni)
	}
	if hi >= ni {
		t.Errorf("host's own background at %d must paint BEFORE its negative-z child at %d (Appendix E steps 1 then 2)", hi, ni)
	}
}

// TestZPositiveOverAuto: a z-index:2 abs box paints AFTER (over) a z:auto abs box.
func TestZPositiveOverAuto(t *testing.T) {
	eng := New(nil, nil, nil)
	autoBG := color.RGBA{0, 0, 30, 255}
	posBG := color.RGBA{40, 0, 0, 255}

	a := posBox(zfill(40, autoBG, 0, 0, 0, true), cssbox.PosAbsolute)
	p := posBox(zfill(40, posBG, 10, 10, 2, false), cssbox.PosAbsolute)
	cont := posBox(func() gcss.ComputedStyle {
		s := posStyle()
		s.Height = gcss.Length{Value: 80, Unit: gcss.UnitPx}
		return s
	}(), cssbox.PosRelative, a, p)
	root := posBox(posStyle(), cssbox.PosStatic, cont)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ai, pi := bgIndex(items, autoBG), bgIndex(items, posBG)
	if ai < 0 || pi < 0 {
		t.Fatalf("missing backgrounds: auto=%d pos=%d", ai, pi)
	}
	if pi <= ai {
		t.Errorf("z:2 background at %d must paint AFTER z:auto at %d", pi, ai)
	}
}

// TestZNegAutoPositiveOrder: three positioned boxes z=-1, auto, z=1 paint in strictly
// increasing index order, straddling the decoration/content phases.
func TestZNegAutoPositiveOrder(t *testing.T) {
	eng := New(nil, nil, nil)
	negBG := color.RGBA{11, 0, 0, 255}
	midBG := color.RGBA{0, 11, 0, 255}
	posBG := color.RGBA{0, 0, 11, 255}

	neg := posBox(zfill(40, negBG, 0, 0, -1, false), cssbox.PosRelative)
	mid := posBox(zfill(40, midBG, 5, 5, 0, true), cssbox.PosRelative)
	pos := posBox(zfill(40, posBG, 10, 10, 1, false), cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, neg, mid, pos)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	ni, mi, pi := bgIndex(items, negBG), bgIndex(items, midBG), bgIndex(items, posBG)
	if ni < 0 || mi < 0 || pi < 0 {
		t.Fatalf("missing backgrounds: neg=%d mid=%d pos=%d", ni, mi, pi)
	}
	if ni >= mi || mi >= pi {
		t.Errorf("want neg(%d) < mid(%d) < pos(%d)", ni, mi, pi)
	}
}

// TestZStableWithinBand: two z:5 boxes keep document order (stable sort), and two
// z:auto boxes keep document order, even interleaved in source.
func TestZStableWithinBand(t *testing.T) {
	eng := New(nil, nil, nil)
	a5 := color.RGBA{50, 1, 0, 255}
	b5 := color.RGBA{50, 2, 0, 255}
	aA := color.RGBA{0, 1, 50, 255}
	bA := color.RGBA{0, 2, 50, 255}

	// Source order: a5, aAuto, b5, bAuto.
	x1 := posBox(zfill(30, a5, 0, 0, 5, false), cssbox.PosRelative)
	x2 := posBox(zfill(30, aA, 4, 4, 0, true), cssbox.PosRelative)
	x3 := posBox(zfill(30, b5, 8, 8, 5, false), cssbox.PosRelative)
	x4 := posBox(zfill(30, bA, 12, 12, 0, true), cssbox.PosRelative)
	root := posBox(posStyle(), cssbox.PosStatic, x1, x2, x3, x4)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	i1, i2, i3, i4 := bgIndex(items, a5), bgIndex(items, aA), bgIndex(items, b5), bgIndex(items, bA)
	// Within z:5: a5 before b5. Within z:auto: aAuto before bAuto.
	if i1 >= i3 {
		t.Errorf("z:5 stable order broken: a5(%d) should precede b5(%d)", i1, i3)
	}
	if i2 >= i4 {
		t.Errorf("z:auto stable order broken: aAuto(%d) should precede bAuto(%d)", i2, i4)
	}
	// And the auto band (middle) paints after the z:5 band? No — z:5 is positive, auto
	// is middle, so auto paints BEFORE positive. Assert that too.
	if i2 >= i1 || i4 >= i1 {
		t.Errorf("z:auto (middle) must paint before z:5 (positive): auto=%d,%d pos=%d,%d", i2, i4, i1, i3)
	}
}

// TestRelativeChildOfNonPositionedClipIsClipped: a position:relative child of a
// NON-positioned overflow:hidden box, offset so it would spill past the clip edge,
// must still be clipped to the clip box's padding box — its background paints BETWEEN a
// ClipPush(clipRect) and a ClipPop even though the child bubbles to an ancestor's
// positioned layer (it is not the clip box's CB). The adversarial part: the offset
// pushes it past the edge, so an UNclipped render would paint outside the box.
func TestRelativeChildOfNonPositionedClipIsClipped(t *testing.T) {
	eng := New(nil, nil, nil)
	childBG := color.RGBA{77, 0, 0, 255}

	// Relative child, offset down+right past the clip box edge.
	childStyle := zfill(60, childBG, 40, 40, 0, true) // z:auto; big offset
	child := posBox(childStyle, cssbox.PosRelative)

	// Non-positioned overflow:hidden clip box, small, containing the child.
	clipStyle := posStyle() // static (NON-positioned) → BFC but not a stacking context
	clipStyle.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosStatic, child)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ci := bgIndex(items, childBG)
	if push < 0 || pop < 0 {
		t.Fatalf("expected a clip bracket; push=%d pop=%d", push, pop)
	}
	if ci < 0 {
		t.Fatal("child background not painted")
	}
	if ci <= push || ci >= pop {
		t.Errorf("relative child background at %d must be INSIDE the clip bracket (push=%d, pop=%d)", ci, push, pop)
	}
}

// TestZIndexInsideClipOrdersWithinBracket: two absolutely-positioned boxes whose
// containing block IS an overflow:hidden box paint INSIDE the clip bracket, ordered by
// z-index (the z:2 box after the z:1 box, both between ClipPush and ClipPop).
func TestZIndexInsideClipOrdersWithinBracket(t *testing.T) {
	eng := New(nil, nil, nil)
	underBG := color.RGBA{0, 0, 60, 255}
	overBG := color.RGBA{60, 0, 0, 255}

	under := posBox(zfill(60, underBG, 10, 10, 1, false), cssbox.PosAbsolute)
	over := posBox(zfill(60, overBG, 30, 30, 2, false), cssbox.PosAbsolute)
	clipStyle := posStyle() // position:relative + overflow:hidden => the abs boxes' CB and a clip
	clipStyle.Width = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	clipStyle.Height = gcss.Length{Value: 100, Unit: gcss.UnitPx}
	clipStyle.Overflow = "hidden"
	clip := posBox(clipStyle, cssbox.PosRelative, under, over)
	root := posBox(posStyle(), cssbox.PosStatic, clip)

	items := eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	push, pop := clipBoundsReal(items)
	ui, oi := bgIndex(items, underBG), bgIndex(items, overBG)
	if push < 0 || pop < 0 || ui < 0 || oi < 0 {
		t.Fatalf("missing items: push=%d pop=%d under=%d over=%d", push, pop, ui, oi)
	}
	// Both inside the bracket, and z:2 (over) after z:1 (under). De-Morgan'd condition
	// (golangci-lint QF1001 forbids if !(a && b)).
	if ui <= push || ui >= pop || oi <= push || oi >= pop {
		t.Errorf("both abs boxes must paint inside the clip bracket: under=%d over=%d (push=%d pop=%d)", ui, oi, push, pop)
	}
	if oi <= ui {
		t.Errorf("z:2 box at %d must paint after z:1 box at %d", oi, ui)
	}
}

// TestZIndexAllAutoByteIdentical: a page whose positioned boxes are ALL z:auto produces
// the SAME item stream regardless of the sort (the stable sort is the identity on equal
// keys). Asserted by comparing the all-auto stream to the stream with the boxes given
// EXPLICIT z-index:0 (also the middle band, same document order) — they must be equal,
// proving auto and explicit-0 both land in document order with no reordering.
func TestZIndexAllAutoByteIdentical(t *testing.T) {
	eng := New(nil, nil, nil)
	build := func(z int, auto bool) []layout.Item {
		a := posBox(zfill(40, color.RGBA{1, 0, 0, 255}, 0, 0, z, auto), cssbox.PosRelative)
		b := posBox(zfill(40, color.RGBA{0, 1, 0, 255}, 5, 5, z, auto), cssbox.PosRelative)
		c := posBox(zfill(40, color.RGBA{0, 0, 1, 255}, 10, 10, z, auto), cssbox.PosRelative)
		root := posBox(posStyle(), cssbox.PosStatic, a, b, c)
		return eng.layoutTree(context.Background(), root, 200).AppendItems(nil)
	}
	autoItems := build(0, true)
	zeroItems := build(0, false)
	if len(autoItems) != len(zeroItems) {
		t.Fatalf("item count differs: auto=%d zero=%d", len(autoItems), len(zeroItems))
	}
	for i := range autoItems {
		if autoItems[i].Kind != zeroItems[i].Kind {
			t.Errorf("item %d kind differs: auto=%v zero=%v", i, autoItems[i].Kind, zeroItems[i].Kind)
			continue
		}
		// The three boxes carry distinct background colors; comparing the per-index color
		// proves the stable sort kept document order (a reorder would swap colors at an
		// index), so auto and explicit-0 both land in the middle band in document order.
		if autoItems[i].Kind == layout.BackgroundKind && autoItems[i].Rule.Color != zeroItems[i].Rule.Color {
			t.Errorf("item %d background color differs (reorder): auto=%v zero=%v", i, autoItems[i].Rule.Color, zeroItems[i].Rule.Color)
		}
	}
}
