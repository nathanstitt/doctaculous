package css

import (
	"image/color"
	"testing"
)

// TestEndToEndCascade exercises parse -> resolver -> compute on a small realistic
// sheet and DOM, the way sub-project 2 (box generation) will call this package.
func TestEndToEndCascade(t *testing.T) {
	src := `
		body { font-family: Arial; font-size: 16px; color: #222222; }
		h1 { font-size: 32px; font-weight: bold; }
		.note { color: gray; background-color: #eeeeee; padding-left: 8px; }
		p { margin-top: 1em; line-height: 1.5; }
	`
	sheet := Parse(src)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

	body := &fakeNode{tag: "body"}
	bodyCS := r.ComputeRoot(body)

	h1 := &fakeNode{tag: "h1", parent: body}
	h1CS := r.Compute(h1, bodyCS)
	if h1CS.FontSizePt != 32 || !h1CS.Bold {
		t.Errorf("h1 = {size %v bold %v}, want {32 true}", h1CS.FontSizePt, h1CS.Bold)
	}
	// font-family inherits from body:
	if h1CS.FontFamily != "Arial" {
		t.Errorf("h1 font-family = %q, want inherited Arial", h1CS.FontFamily)
	}

	p := &fakeNode{tag: "p", classes: []string{"note"}, parent: body}
	pCS := r.Compute(p, bodyCS)
	if pCS.Color != (color.RGBA{128, 128, 128, 255}) {
		t.Errorf("p.note color = %v, want gray", pCS.Color)
	}
	if pCS.BackgroundColor != (color.RGBA{0xee, 0xee, 0xee, 255}) {
		t.Errorf("p.note background = %v", pCS.BackgroundColor)
	}
	if pCS.MarginTop != (Length{1, UnitEm}) {
		t.Errorf("p margin-top = %v, want 1em", pCS.MarginTop)
	}
}

// TestUnitlessLineHeight covers the unitless multiplier form — the commonest spelling
// of the property. It used to be rejected: parseLength refuses a non-zero unitless
// number (correctly, for a length), so the declaration was dropped and line-height
// stayed at its inherited value, which made the property appear to do nothing at all.
//
// The three forms compute differently, and the difference is the point:
//   - a NUMBER stays a number, so it re-multiplies against each descendant's own font
//     size when it inherits;
//   - an EM or % is computed here against this element's font size, so descendants
//     inherit a fixed length (CSS 2.1 §10.8.1).
func TestUnitlessLineHeight(t *testing.T) {
	sheet := Parse(`p { line-height: 1.5; }`)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
	if cs.LineHeight != (Length{1.5, UnitNumber}) {
		t.Errorf("line-height = %v, want 1.5 as a unitless number", cs.LineHeight)
	}
	// An explicit unit IS applied:
	sheet2 := Parse(`p { line-height: 20px; }`)
	r2 := NewResolver([]OriginSheet{{Sheet: sheet2, Origin: OriginAuthor}}, nil)
	cs2 := r2.ComputeRoot(&fakeNode{tag: "p"})
	if cs2.LineHeight != (Length{20, UnitPx}) {
		t.Errorf("line-height = %v, want 20px (explicit unit applied)", cs2.LineHeight)
	}
	// An em value is COMPUTED against the element's own font size, so what a
	// descendant inherits is a fixed length rather than a multiplier.
	sheet3 := Parse(`p { font-size: 10px; line-height: 2em; }`)
	r3 := NewResolver([]OriginSheet{{Sheet: sheet3, Origin: OriginAuthor}}, nil)
	cs3 := r3.ComputeRoot(&fakeNode{tag: "p"})
	if cs3.LineHeight != (Length{20, UnitPt}) {
		t.Errorf("line-height = %v, want 2em computed to 20pt", cs3.LineHeight)
	}
	// A unitless number must NOT be a valid length elsewhere.
	if _, ok := parseLength(newTokenizer("1.5").next()); ok {
		t.Error("parseLength accepted a non-zero unitless number; width:1.5 must stay invalid")
	}
}
