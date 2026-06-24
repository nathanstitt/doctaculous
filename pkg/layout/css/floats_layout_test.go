package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
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
	if idxOuterFloat > idxInnerBg {
		t.Errorf("outer float (%d) painted after inner BFC bg (%d); want before", idxOuterFloat, idxInnerBg)
	}
	// Inner BFC paints atomically and in its own Appendix-E order: inner bg, then its
	// float, then its glyph — contiguous, with nothing else between.
	if idxInnerBg >= idxInnerFloat || idxInnerFloat >= idxInnerGlyph {
		t.Errorf("inner BFC internal order wrong: bg=%d float=%d glyph=%d; want bg<float<glyph",
			idxInnerBg, idxInnerFloat, idxInnerGlyph)
	}
}
