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

// TestUnitlessLineHeightDeferred documents that a unitless line-height multiplier
// (e.g. "1.5") is not yet supported: parseLength rejects non-zero unitless
// numbers, so line-height stays at its inherited/initial value. Supporting the
// unitless multiplier form is deferred to a later sub-project (it needs a
// UnitNumber/multiplier concept the layout engine resolves against font size).
func TestUnitlessLineHeightDeferred(t *testing.T) {
	sheet := Parse(`p { line-height: 1.5; }`)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
	if cs.LineHeight.Unit != UnitAuto {
		t.Errorf("line-height unit = %v, want UnitAuto (unitless 1.5 not yet applied)", cs.LineHeight.Unit)
	}
	// But an explicit unit IS applied:
	sheet2 := Parse(`p { line-height: 20px; }`)
	r2 := NewResolver([]OriginSheet{{Sheet: sheet2, Origin: OriginAuthor}}, nil)
	cs2 := r2.ComputeRoot(&fakeNode{tag: "p"})
	if cs2.LineHeight != (Length{20, UnitPx}) {
		t.Errorf("line-height = %v, want 20px (explicit unit applied)", cs2.LineHeight)
	}
}
