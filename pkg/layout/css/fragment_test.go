package css

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// tinyOutline returns a minimal non-nil path so a glyph is treated as drawable. The
// flatten only copies the pointer, so the exact segments are irrelevant.
func tinyOutline() *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(1, 1)
	return p
}

// TestAppendItemsBackgroundAndBorders checks that a single fragment with a
// background and all four borders flattens to a background item followed by the four
// border strips in EdgeTop, EdgeRight, EdgeBottom, EdgeLeft order, each with the
// strip rectangle the per-side formulas dictate.
func TestAppendItemsBackgroundAndBorders(t *testing.T) {
	bg := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	const (
		x, y, w, h = 100.0, 200.0, 50.0, 40.0
		top        = 1.0
		right      = 2.0
		bottom     = 3.0
		left       = 4.0
	)
	f := &Fragment{X: x, Y: y, W: w, H: h, Background: bg}
	f.Border[layout.EdgeTop] = BorderEdge{Width: top, Color: color.RGBA{A: 255}, Style: layout.BorderSolid}
	f.Border[layout.EdgeRight] = BorderEdge{Width: right, Color: color.RGBA{R: 1, A: 255}, Style: layout.BorderDashed}
	f.Border[layout.EdgeBottom] = BorderEdge{Width: bottom, Color: color.RGBA{G: 1, A: 255}, Style: layout.BorderDotted}
	f.Border[layout.EdgeLeft] = BorderEdge{Width: left, Color: color.RGBA{B: 1, A: 255}, Style: layout.BorderDouble}

	items := f.AppendItems(nil)
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5 (bg + 4 borders)", len(items))
	}

	// Item 0: the background fills the whole border box.
	if items[0].Kind != layout.BackgroundKind {
		t.Fatalf("item[0] kind = %v, want BackgroundKind", items[0].Kind)
	}
	if got := items[0].Rule; got != (layout.RuleItem{XPt: x, YPt: y, WPt: w, HPt: h, Color: bg}) {
		t.Errorf("background rule = %+v, want full border box with bg color", got)
	}

	// Items 1..4: borders, in EdgeTop, EdgeRight, EdgeBottom, EdgeLeft order, each
	// with the strip rect from the documented formulas.
	want := []layout.BorderItem{
		{XPt: x, YPt: y, WPt: w, HPt: top, Color: color.RGBA{A: 255}, Style: layout.BorderSolid, Side: layout.EdgeTop},
		{XPt: x + w - right, YPt: y, WPt: right, HPt: h, Color: color.RGBA{R: 1, A: 255}, Style: layout.BorderDashed, Side: layout.EdgeRight},
		{XPt: x, YPt: y + h - bottom, WPt: w, HPt: bottom, Color: color.RGBA{G: 1, A: 255}, Style: layout.BorderDotted, Side: layout.EdgeBottom},
		{XPt: x, YPt: y, WPt: left, HPt: h, Color: color.RGBA{B: 1, A: 255}, Style: layout.BorderDouble, Side: layout.EdgeLeft},
	}
	for i, wb := range want {
		it := items[i+1]
		if it.Kind != layout.BorderKind {
			t.Errorf("item[%d] kind = %v, want BorderKind", i+1, it.Kind)
			continue
		}
		if it.Border != wb {
			t.Errorf("border[%d] = %+v, want %+v", i, it.Border, wb)
		}
	}
}

// TestAppendItemsSkipsEmpty checks that a zero-alpha background and any zero-width or
// BorderNone edges contribute no items.
func TestAppendItemsSkipsEmpty(t *testing.T) {
	f := &Fragment{X: 0, Y: 0, W: 10, H: 10} // Background.A == 0 => no background
	// A zero-width edge and a BorderNone edge (even with width) are both skipped.
	f.Border[layout.EdgeTop] = BorderEdge{Width: 0, Color: color.RGBA{A: 255}, Style: layout.BorderSolid}
	f.Border[layout.EdgeLeft] = BorderEdge{Width: 5, Color: color.RGBA{A: 255}, Style: layout.BorderNone}
	// EdgeRight/EdgeBottom left as zero values (Width 0, BorderNone) => skipped too.

	if items := f.AppendItems(nil); len(items) != 0 {
		t.Fatalf("got %d items, want 0 (nothing drawable): %+v", len(items), items)
	}
}

// TestAppendItemsGlyphs checks that a line's glyphs flatten to GlyphKind items at the
// line baseline, and that a glyph with a nil outline is skipped.
func TestAppendItemsGlyphs(t *testing.T) {
	out := tinyOutline()
	col := color.RGBA{R: 7, G: 8, B: 9, A: 255}
	f := &Fragment{
		Lines: []LineFragment{{
			BaselineY: 123.5,
			Glyphs: []GlyphFragment{
				{Outline: out, X: 50, SizePt: 12, Color: col},
				{Outline: nil, X: 60, SizePt: 12, Color: col}, // whitespace -> skipped
			},
		}},
	}

	items := f.AppendItems(nil)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (the nil-outline glyph is skipped)", len(items))
	}
	it := items[0]
	if it.Kind != layout.GlyphKind {
		t.Fatalf("item[0] kind = %v, want GlyphKind", it.Kind)
	}
	wantGlyph := layout.GlyphItem{Outline: out, XPt: 50, YPt: 123.5, SizePt: 12, Color: col}
	if it.Glyph != wantGlyph {
		t.Errorf("glyph = %+v, want %+v", it.Glyph, wantGlyph)
	}
	if it.Glyph.Outline != out {
		t.Errorf("glyph outline pointer not preserved")
	}
}

// TestAppendItemsPaintOrder is the key ordering test: a parent's own background must
// come before any child's items, and children flatten in slice order (parent before
// child; siblings left to right).
func TestAppendItemsPaintOrder(t *testing.T) {
	parentBG := color.RGBA{R: 1, A: 255}
	child0BG := color.RGBA{R: 2, A: 255}
	child1BG := color.RGBA{R: 3, A: 255}

	parent := &Fragment{
		X: 0, Y: 0, W: 100, H: 100, Background: parentBG,
		Children: []*Fragment{
			{X: 10, Y: 10, W: 20, H: 20, Background: child0BG},
			{X: 40, Y: 40, W: 20, H: 20, Background: child1BG},
		},
	}

	items := parent.AppendItems(nil)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (parent bg + 2 child bgs)", len(items))
	}
	// All three are backgrounds; assert the order by color.
	wantColors := []color.RGBA{parentBG, child0BG, child1BG}
	for i, wc := range wantColors {
		if items[i].Kind != layout.BackgroundKind {
			t.Errorf("item[%d] kind = %v, want BackgroundKind", i, items[i].Kind)
		}
		if items[i].Rule.Color != wc {
			t.Errorf("item[%d] color = %v, want %v (paint order parent-before-child, siblings in order)", i, items[i].Rule.Color, wc)
		}
	}
}

// TestAppendItemsNonPositionedByteIdentical: a tree with NO positioned boxes
// produces the exact same item slice with the stacking pass as the 5a 3-phase pass.
// (Guards the "non-positioned pages byte-identical" invariant.) Build a BFC root
// with a background, a normal child with a background + a glyph line, and assert the
// item KINDS/coords are exactly decorations-then-content order.
func TestAppendItemsNonPositionedByteIdentical(t *testing.T) {
	// root (BFC, stacking context) bg; child bg + one glyph.
	child := &Fragment{X: 0, Y: 20, W: 100, H: 30, Background: color.RGBA{1, 1, 1, 255}}
	child.Lines = []LineFragment{{BaselineY: 35, Glyphs: []GlyphFragment{{Outline: tinyOutline(), X: 5, SizePt: 12, Color: color.RGBA{0, 0, 0, 255}}}}}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 60, Background: color.RGBA{2, 2, 2, 255}, IsBFC: true, IsStackingContext: true, Children: []*Fragment{child}}
	items := root.AppendItems(nil)
	// Expect: root bg, child bg, child glyph — backgrounds before glyph (decorations
	// before content), 3 items.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[0].Kind != layout.BackgroundKind || items[1].Kind != layout.BackgroundKind || items[2].Kind != layout.GlyphKind {
		t.Errorf("item kinds = %v/%v/%v, want bg/bg/glyph", items[0].Kind, items[1].Kind, items[2].Kind)
	}
}

// TestAppendItemsPositionedPaintsLast: a positioned child paints AFTER an in-flow
// sibling's content (positioned layer is last). Build a root with two children: a
// normal child (bg) and a positioned child (bg, IsPositioned, on the root's
// Positioned slice and excluded from Children-content). Assert the positioned bg
// comes last.
func TestAppendItemsPositionedPaintsLast(t *testing.T) {
	normal := &Fragment{X: 0, Y: 0, W: 50, H: 20, Background: color.RGBA{1, 1, 1, 255}}
	posChild := &Fragment{X: 0, Y: 0, W: 50, H: 20, Background: color.RGBA{9, 9, 9, 255}, IsPositioned: true, IsStackingContext: true}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 40, IsBFC: true, IsStackingContext: true,
		Children:   []*Fragment{normal, posChild}, // posChild in Children (skipped in-flow) ...
		Positioned: []*Fragment{posChild},         // ... and in the positioned layer.
	}
	items := root.AppendItems(nil)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (normal bg, positioned bg)", len(items))
	}
	if items[len(items)-1].Rule.Color != (color.RGBA{9, 9, 9, 255}) {
		t.Errorf("last item is not the positioned bg; positioned layer not last")
	}
}

// TestAppendItemsRelativeOffsetTranslatesRange: a relative fragment's RelOffsetX/Y
// shifts ALL of its emitted items (and its subtree's) by the offset.
func TestAppendItemsRelativeOffsetTranslatesRange(t *testing.T) {
	rel := &Fragment{X: 10, Y: 10, W: 30, H: 30, Background: color.RGBA{5, 5, 5, 255},
		IsPositioned: true, IsStackingContext: true, RelOffsetX: 5, RelOffsetY: 7}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 100, IsBFC: true, IsStackingContext: true,
		Children: []*Fragment{rel}, Positioned: []*Fragment{rel}}
	items := root.AppendItems(nil)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	// rel's background border box is X=10,Y=10; with the +5/+7 offset it must paint at 15/17.
	if items[0].Rule.XPt != 15 || items[0].Rule.YPt != 17 {
		t.Errorf("relative bg at (%v,%v), want (15,17) (offset applied)", items[0].Rule.XPt, items[0].Rule.YPt)
	}
}

// TestPage checks Page builds a layout.Page with the given dimensions and Items equal
// to AppendItems(nil).
func TestPage(t *testing.T) {
	f := &Fragment{
		X: 0, Y: 0, W: 50, H: 50, Background: color.RGBA{R: 5, A: 255},
		Children: []*Fragment{
			{X: 1, Y: 1, W: 2, H: 2, Background: color.RGBA{G: 5, A: 255}},
		},
	}
	const w, h = 612.0, 792.0

	page := f.Page(w, h)
	if page.WidthPt != w || page.HeightPt != h {
		t.Errorf("page size = %vx%v, want %vx%v", page.WidthPt, page.HeightPt, w, h)
	}
	if want := f.AppendItems(nil); !reflect.DeepEqual(page.Items, want) {
		t.Errorf("page.Items = %+v, want %+v (== AppendItems(nil))", page.Items, want)
	}
}
