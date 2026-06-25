package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
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
