package css

import (
	"context"
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// solidGlyph returns a fragment line with one fillable glyph (so the inline phase
// emits a GlyphKind item). The outline is a tiny non-empty path.
func glyphLine(y float64) LineFragment {
	return LineFragment{BaselineY: y, Glyphs: []GlyphFragment{{Outline: unitGlyph(), X: 0, SizePt: 10, Color: color.RGBA{0, 0, 0, 255}}}}
}

// unitGlyph returns a minimal non-empty render.Path so a GlyphFragment emits a
// GlyphKind item (AppendItems skips a nil outline).
func unitGlyph() *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(1, 0)
	p.LineTo(1, 1)
	p.Close()
	return p
}

// TestAppendItemsFloatPaintOrder: a float overlapping a later in-flow sibling paints
// AFTER the sibling's background/border but BEFORE its glyphs (CSS Appendix E).
func TestAppendItemsFloatPaintOrder(t *testing.T) {
	// BFC root with: [0] a float (background only), [1] an in-flow sibling with a
	// background AND a glyph. Expected item order:
	//   sibling background, (sibling has no border), float background, sibling glyph.
	floatFrag := &Fragment{X: 0, Y: 0, W: 50, H: 50, Background: color.RGBA{255, 0, 0, 255}, IsFloat: true}
	sibling := &Fragment{X: 0, Y: 0, W: 200, H: 80, Background: color.RGBA{0, 255, 0, 255}, Lines: []LineFragment{glyphLine(20)}}

	root := &Fragment{
		X: 0, Y: 0, W: 200, H: 80,
		Children: []*Fragment{sibling}, // the float is NOT an in-flow child
		Floats:   []*Fragment{floatFrag},
		IsBFC:    true,
	}

	items := root.AppendItems(nil)

	// Find the index of each kind/color of interest.
	idxSiblingBg, idxFloatBg, idxGlyph := -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			if items[i].Rule.Color == (color.RGBA{0, 255, 0, 255}) {
				idxSiblingBg = i
			}
			if items[i].Rule.Color == (color.RGBA{255, 0, 0, 255}) {
				idxFloatBg = i
			}
		case layout.GlyphKind:
			idxGlyph = i
		}
	}
	if idxSiblingBg < 0 || idxFloatBg < 0 || idxGlyph < 0 {
		t.Fatalf("missing items: siblingBg=%d floatBg=%d glyph=%d (items=%d)", idxSiblingBg, idxFloatBg, idxGlyph, len(items))
	}
	if idxSiblingBg >= idxFloatBg || idxFloatBg >= idxGlyph {
		t.Errorf("paint order wrong: siblingBg=%d floatBg=%d glyph=%d; want siblingBg < floatBg < glyph",
			idxSiblingBg, idxFloatBg, idxGlyph)
	}
}

// TestAppendItemsNoFloatsUnchanged: a BFC root with no floats still emits its own
// background before a child's (tree order within the background layer).
func TestAppendItemsNoFloatsUnchanged(t *testing.T) {
	child := &Fragment{X: 0, Y: 0, W: 100, H: 20, Background: color.RGBA{0, 0, 255, 255}}
	root := &Fragment{X: 0, Y: 0, W: 100, H: 40, Background: color.RGBA{200, 200, 200, 255}, Children: []*Fragment{child}, IsBFC: true}

	items := root.AppendItems(nil)
	if len(items) != 2 || items[0].Kind != layout.BackgroundKind || items[1].Kind != layout.BackgroundKind {
		t.Fatalf("unexpected items %+v", items)
	}
	// Root background paints before the child's.
	if items[0].Rule.Color != (color.RGBA{200, 200, 200, 255}) {
		t.Errorf("root background not first")
	}
}

// TestAppendItemsBlockBgBeforeInlineContent: per CSS 2.1 Appendix E, ALL in-flow
// block-level backgrounds/borders paint (step 4) before ANY in-flow inline content
// (step 7), even across nesting. So a nested block's background paints before its
// parent's text. This is the intended layered order, NOT a regression — this test
// pins it so the phase-split is validated beyond the trivial two-background case.
func TestAppendItemsBlockBgBeforeInlineContent(t *testing.T) {
	// Parent block: a background + a text glyph; contains a nested block with its own
	// background. Expected order: parent bg, nested bg (both in the bg layer, tree
	// order), THEN parent glyph (content layer).
	nested := &Fragment{X: 0, Y: 0, W: 50, H: 10, Background: color.RGBA{0, 0, 255, 255}}
	parent := &Fragment{
		X: 0, Y: 0, W: 100, H: 40,
		Background: color.RGBA{200, 200, 200, 255},
		Lines:      []LineFragment{glyphLine(8)},
		Children:   []*Fragment{nested},
		IsBFC:      true,
	}
	items := parent.AppendItems(nil)

	idxParentBg, idxNestedBg, idxGlyph := -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			if items[i].Rule.Color == (color.RGBA{200, 200, 200, 255}) {
				idxParentBg = i
			}
			if items[i].Rule.Color == (color.RGBA{0, 0, 255, 255}) {
				idxNestedBg = i
			}
		case layout.GlyphKind:
			idxGlyph = i
		}
	}
	if idxParentBg < 0 || idxNestedBg < 0 || idxGlyph < 0 {
		t.Fatalf("missing items: parentBg=%d nestedBg=%d glyph=%d", idxParentBg, idxNestedBg, idxGlyph)
	}
	if idxParentBg >= idxNestedBg || idxNestedBg >= idxGlyph {
		t.Errorf("Appendix E order violated: parentBg=%d nestedBg=%d glyph=%d; want parentBg < nestedBg < glyph",
			idxParentBg, idxNestedBg, idxGlyph)
	}
}

// TestAppendItemsNestedBFCAtomic: a nested BFC child (e.g. an inline-block) paints
// as a single atom in the OUTER BFC's content phase — its own background and its
// own float paint together (the inner float between the inner bg and inner content),
// NOT split into the outer BFC's decoration/float/content phases. Concretely: the
// outer BFC has a float; the inner BFC (a child) has its own background and its own
// float. The outer float must NOT be interleaved with the inner BFC's internals.
func TestAppendItemsNestedBFCAtomic(t *testing.T) {
	outerFloat := &Fragment{X: 0, Y: 0, W: 20, H: 20, Background: color.RGBA{255, 0, 0, 255}, IsFloat: true}

	innerFloat := &Fragment{X: 100, Y: 0, W: 10, H: 10, Background: color.RGBA{0, 255, 0, 255}, IsFloat: true}
	innerBFC := &Fragment{
		X: 90, Y: 0, W: 60, H: 40,
		Background: color.RGBA{0, 0, 255, 255}, // inner bg
		Lines:      []LineFragment{glyphLine(10)},
		Floats:     []*Fragment{innerFloat},
		IsBFC:      true,
	}

	root := &Fragment{
		X: 0, Y: 0, W: 200, H: 60,
		Children: []*Fragment{innerBFC},
		Floats:   []*Fragment{outerFloat},
		IsBFC:    true,
	}

	items := root.AppendItems(nil)

	// Locate the outer float (red bg) and the inner BFC's items (blue bg, green float
	// bg, glyph). The inner BFC's three items must be contiguous-in-order AFTER the
	// outer float (the outer content phase), proving the inner BFC painted atomically.
	idxOuterFloat, idxInnerBg, idxInnerFloat, idxInnerGlyph := -1, -1, -1, -1
	for i := range items {
		switch items[i].Kind {
		case layout.BackgroundKind:
			switch items[i].Rule.Color {
			case color.RGBA{255, 0, 0, 255}:
				idxOuterFloat = i
			case color.RGBA{0, 0, 255, 255}:
				idxInnerBg = i
			case color.RGBA{0, 255, 0, 255}:
				idxInnerFloat = i
			}
		case layout.GlyphKind:
			idxInnerGlyph = i
		}
	}
	if idxOuterFloat < 0 || idxInnerBg < 0 || idxInnerFloat < 0 || idxInnerGlyph < 0 {
		t.Fatalf("missing items: outerFloat=%d innerBg=%d innerFloat=%d innerGlyph=%d",
			idxOuterFloat, idxInnerBg, idxInnerFloat, idxInnerGlyph)
	}
	// Outer float paints before the inner BFC atom (outer float layer precedes outer
	// content phase, which is where the inner BFC paints).
	if idxOuterFloat >= idxInnerBg {
		t.Errorf("outer float (%d) painted after inner BFC bg (%d); want before", idxOuterFloat, idxInnerBg)
	}
	// Inner BFC paints atomically and in its own Appendix-E order: inner bg, then its
	// float, then its glyph — contiguous, with nothing else between.
	if idxInnerBg >= idxInnerFloat || idxInnerFloat >= idxInnerGlyph {
		t.Errorf("inner BFC internal order wrong: bg=%d float=%d glyph=%d; want bg<float<glyph",
			idxInnerBg, idxInnerFloat, idxInnerGlyph)
	}
}

// blockBox builds a minimal block box with the given style and children.
//
// It emulates initialStyle()'s auto/none conventions. A zero-value Length is
// {0, UnitPx}, which the resolver reads as an EXPLICIT 0 ("width:0"/"max-width:0"),
// NOT the CSS initial "auto"/"none". Real box-gen gets Width/Height=auto and
// Max*=none ("none") from the cascade, so a test style that omits any of them must
// set them, or e.g. a width-less block resolves to content width 0 (and cascades 0
// down to its children), or a 60×40 float clamps to 0×0. Only fields left at the
// zero value are defaulted, so an explicit Width/Height/Max* in the literal wins.
func blockBox(style gcss.ComputedStyle, kids ...*cssbox.Box) *cssbox.Box {
	if style.Width == (gcss.Length{}) {
		style.Width = gcss.Length{Unit: gcss.UnitAuto}
	}
	if style.Height == (gcss.Length{}) {
		style.Height = gcss.Length{Unit: gcss.UnitAuto}
	}
	if style.MaxWidth == (gcss.Length{}) {
		style.MaxWidth = gcss.Length{Unit: gcss.UnitAuto}
	}
	if style.MaxHeight == (gcss.Length{}) {
		style.MaxHeight = gcss.Length{Unit: gcss.UnitAuto}
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: style, Children: kids}
}

// TestFloatPlacedOutOfFlow: a left-floated child div is placed at the content-box
// left, marked IsFloat, collected on the root's Floats, and does NOT advance the
// in-flow cursor (a following in-flow block starts at y=0, beside the float).
func TestFloatPlacedOutOfFlow(t *testing.T) {
	eng := New(nil, nil, nil)

	floatStyle := gcss.ComputedStyle{Display: "block", Float: "left",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}}
	floated := blockBox(floatStyle)
	floated.Float = cssbox.FloatLeft

	following := blockBox(gcss.ComputedStyle{Display: "block",
		Height: gcss.Length{Value: 30, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, following)

	frag := eng.layoutTree(context.Background(), root, 200)
	if frag == nil {
		t.Fatal("nil root fragment")
	}
	if !frag.IsBFC {
		t.Errorf("root fragment not marked IsBFC")
	}
	if len(frag.Floats) != 1 {
		t.Fatalf("root has %d floats, want 1", len(frag.Floats))
	}
	ff := frag.Floats[0]
	if !ff.IsFloat {
		t.Errorf("float fragment not marked IsFloat")
	}
	if ff.X != 0 || ff.Y != 0 {
		t.Errorf("float at (%v,%v), want (0,0)", ff.X, ff.Y)
	}
	// The following in-flow block is a normal child; it should start at y=0 (the
	// float did not consume vertical space).
	if len(frag.Children) != 1 {
		t.Fatalf("root has %d in-flow children, want 1 (the float is not a child)", len(frag.Children))
	}
	if frag.Children[0].Y != 0 {
		t.Errorf("following block Y=%v, want 0 (float out of flow)", frag.Children[0].Y)
	}
}

// TestClearDropsBelowFloat: a clear:left block starts below a preceding left float.
func TestClearDropsBelowFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	cleared := blockBox(gcss.ComputedStyle{Display: "block", Clear: "left",
		Height: gcss.Length{Value: 20, Unit: gcss.UnitPx}})

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, cleared)
	frag := eng.layoutTree(context.Background(), root, 200)

	if len(frag.Children) != 1 {
		t.Fatalf("want 1 in-flow child, got %d", len(frag.Children))
	}
	if frag.Children[0].Y < 40-1e-6 {
		t.Errorf("cleared block Y=%v, want >= 40 (below the float)", frag.Children[0].Y)
	}
}

// TestTextWrapsBesideFloat: with a left float occupying the top-left, the first
// line of following text starts at the float's right edge; a line below the float's
// bottom starts back at the content-box left.
func TestTextWrapsBesideFloat(t *testing.T) {
	eng := New(nil, nil, nil)

	// A 60pt-wide, 40pt-tall left float.
	floated := blockBox(gcss.ComputedStyle{Display: "block",
		Width:  gcss.Length{Value: 60, Unit: gcss.UnitPx},
		Height: gcss.Length{Value: 40, Unit: gcss.UnitPx}})
	floated.Float = cssbox.FloatLeft

	// A sibling block of text with enough words to wrap several lines at width 200.
	// Width:auto so it fills the 200px container (a zero-value Width would resolve to
	// "width:0" — see blockBox). It is built as a raw literal (not blockBox) because it
	// needs Formatting: InlineFC for its text child.
	textStyle := gcss.ComputedStyle{Display: "block", FontFamily: "serif", FontSizePt: 12,
		LineHeight: gcss.Length{Value: 16, Unit: gcss.UnitPx}, Color: color.RGBA{0, 0, 0, 255},
		Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Unit: gcss.UnitAuto},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto}}
	para := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC, Style: textStyle,
		Children: []*cssbox.Box{{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Style: textStyle,
			Text: "Many words here that should wrap across several lines beside and below the floated box on the left side."}}}

	root := blockBox(gcss.ComputedStyle{Display: "block"}, floated, para)
	frag := eng.layoutTree(context.Background(), root, 200)

	// Find the paragraph fragment (the only in-flow child) and inspect its first line.
	if len(frag.Children) != 1 {
		t.Fatalf("want 1 in-flow child, got %d", len(frag.Children))
	}
	pf := frag.Children[0]
	if len(pf.Lines) < 2 {
		t.Fatalf("paragraph has %d lines, want >= 2 to test wrap", len(pf.Lines))
	}
	// First line's first glyph X should be at/after the float's right edge (60),
	// since the float occupies the top-left band.
	firstX := pf.Lines[0].Glyphs[0].X
	if firstX < 60-1e-6 {
		t.Errorf("first line starts at X=%v, want >= 60 (right of the float)", firstX)
	}
	// The last line should be below the float (baseline > 40), and start back near
	// the content-box left (X < 60). Find a line whose baseline is clearly below 40.
	var belowX float64 = -1
	for i := range pf.Lines {
		if pf.Lines[i].BaselineY > 40+8 { // a line whose top is past the float bottom
			belowX = pf.Lines[i].Glyphs[0].X
			break
		}
	}
	if belowX < 0 {
		t.Fatalf("no line below the float bottom; lines: %d", len(pf.Lines))
	}
	if belowX > 60 {
		t.Errorf("line below the float starts at X=%v, want < 60 (back at the left)", belowX)
	}
}
