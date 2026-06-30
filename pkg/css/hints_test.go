package css

import (
	"image/color"
	"testing"
)

// hintStyle computes a node's style under a (possibly empty) author sheet plus the
// presentational-hint tier, for precedence tests.
func hintStyle(node *fakeNode, authorSrc string) ComputedStyle {
	sheets := []OriginSheet{}
	if authorSrc != "" {
		sheets = append(sheets, OriginSheet{Sheet: Parse(authorSrc), Origin: OriginAuthor})
	}
	return NewResolver(sheets, nil).ComputeRoot(node)
}

func TestHintBgcolor(t *testing.T) {
	td := &fakeNode{tag: "td", attrs: map[string]string{"bgcolor": "#cc0000"}}
	cs := hintStyle(td, "")
	if cs.BackgroundColor != (color.RGBA{0xcc, 0, 0, 255}) {
		t.Errorf("bgcolor hint = %+v, want #cc0000", cs.BackgroundColor)
	}
	// Bare hex (no #) is tolerated.
	td2 := &fakeNode{tag: "td", attrs: map[string]string{"bgcolor": "00ff00"}}
	if c := hintStyle(td2, "").BackgroundColor; c != (color.RGBA{0, 0xff, 0, 255}) {
		t.Errorf("bare-hex bgcolor = %+v, want green", c)
	}
}

// An author rule beats a presentational hint; inline style beats both; a hint beats UA.
func TestHintCascadePrecedence(t *testing.T) {
	td := &fakeNode{tag: "td", attrs: map[string]string{"bgcolor": "#cc0000"}}
	// Author rule wins over the hint.
	if c := hintStyle(td, "td { background-color: #0000ff; }").BackgroundColor; c != (color.RGBA{0, 0, 0xff, 255}) {
		t.Errorf("author rule should beat bgcolor hint, got %+v", c)
	}
	// A hint beats a UA-origin rule.
	r := NewResolver([]OriginSheet{{Sheet: Parse("td { background-color: #00ff00; }"), Origin: OriginUA}}, nil)
	if c := r.ComputeRoot(td).BackgroundColor; c != (color.RGBA{0xcc, 0, 0, 255}) {
		t.Errorf("hint should beat UA rule, got %+v want #cc0000", c)
	}
}

func TestHintInlineStyleBeatsHint(t *testing.T) {
	td := &fakeNode{tag: "td", attrs: map[string]string{
		"bgcolor": "#cc0000",
		"style":   "background-color: #0000ff",
	}}
	if c := hintStyle(td, "").BackgroundColor; c != (color.RGBA{0, 0, 0xff, 255}) {
		t.Errorf("inline style should beat bgcolor hint, got %+v", c)
	}
}

func TestHintWidthHeight(t *testing.T) {
	td := &fakeNode{tag: "td", attrs: map[string]string{"width": "120", "height": "40"}}
	cs := hintStyle(td, "")
	if cs.Width != (Length{120, UnitPx}) {
		t.Errorf("width hint = %+v, want 120px", cs.Width)
	}
	if cs.Height != (Length{40, UnitPx}) {
		t.Errorf("height hint = %+v, want 40px", cs.Height)
	}
	// Percentage width.
	tbl := &fakeNode{tag: "table", attrs: map[string]string{"width": "50%"}}
	if w := hintStyle(tbl, "").Width; w != (Length{50, UnitPercent}) {
		t.Errorf("percent width = %+v, want 50%%", w)
	}
}

func TestHintAlign(t *testing.T) {
	// align on a cell → text-align.
	td := &fakeNode{tag: "td", attrs: map[string]string{"align": "center"}}
	if a := hintStyle(td, "").TextAlign; a != "center" {
		t.Errorf("td align=center → text-align %q, want center", a)
	}
	// align=right on an image → float (not text-align).
	img := &fakeNode{tag: "img", attrs: map[string]string{"align": "right"}}
	cs := hintStyle(img, "")
	if cs.Float != "right" {
		t.Errorf("img align=right → float %q, want right", cs.Float)
	}
	if cs.TextAlign == "right" {
		t.Error("img align=right should NOT set text-align")
	}
}

func TestHintValign(t *testing.T) {
	td := &fakeNode{tag: "td", attrs: map[string]string{"valign": "top"}}
	if v := hintStyle(td, "").VerticalAlign; v != "top" {
		t.Errorf("valign hint = %q, want top", v)
	}
}

// cellpadding and table border propagate from the table to each cell.
func TestHintTablePropagation(t *testing.T) {
	table := &fakeNode{tag: "table", attrs: map[string]string{"cellpadding": "8", "border": "1"}}
	td := &fakeNode{tag: "td", parent: table}
	cs := hintStyle(td, "")
	if cs.PaddingTop != (Length{8, UnitPx}) || cs.PaddingLeft != (Length{8, UnitPx}) {
		t.Errorf("cellpadding → cell padding = T%v L%v, want 8px", cs.PaddingTop, cs.PaddingLeft)
	}
	if cs.BorderTopWidth != (Length{1, UnitPx}) || cs.BorderTopStyle != "inset" {
		t.Errorf("table border=1 → cell border = %v %q, want 1px inset", cs.BorderTopWidth, cs.BorderTopStyle)
	}
	// The table itself gets an outset border.
	tcs := hintStyle(table, "")
	if tcs.BorderTopWidth != (Length{1, UnitPx}) || tcs.BorderTopStyle != "outset" {
		t.Errorf("table border=1 → table border = %v %q, want 1px outset", tcs.BorderTopWidth, tcs.BorderTopStyle)
	}
}

func TestHintCellspacing(t *testing.T) {
	table := &fakeNode{tag: "table", attrs: map[string]string{"cellspacing": "5"}}
	if cs := hintStyle(table, ""); cs.BorderSpacingH != 5 || cs.BorderSpacingV != 5 {
		t.Errorf("cellspacing=5 → border-spacing %v/%v, want 5/5", cs.BorderSpacingH, cs.BorderSpacingV)
	}
}

func TestHintBorderZeroSuppresses(t *testing.T) {
	// border="0" → no border on the table or its cells (the common suppress case).
	table := &fakeNode{tag: "table", attrs: map[string]string{"border": "0"}}
	if cs := hintStyle(table, ""); cs.BorderTopStyle == "outset" {
		t.Error("border=0 should not add a table border")
	}
	td := &fakeNode{tag: "td", parent: table}
	if cs := hintStyle(td, ""); cs.BorderTopStyle == "inset" {
		t.Error("border=0 should not add a cell border")
	}
}

func TestHintListType(t *testing.T) {
	ol := &fakeNode{tag: "ol", attrs: map[string]string{"type": "I", "start": "3"}}
	cs := hintStyle(ol, "")
	if cs.ListStyleType != "upper-roman" {
		t.Errorf("ol type=I → %q, want upper-roman", cs.ListStyleType)
	}
	// start=3 resets list-item to 2 (so the first <li>'s +1 yields 3).
	if len(cs.CounterReset) != 1 || cs.CounterReset[0].Name != "list-item" || cs.CounterReset[0].Value != 2 {
		t.Errorf("ol start=3 → counter-reset %+v, want list-item=2", cs.CounterReset)
	}
}

func TestHintFontColor(t *testing.T) {
	f := &fakeNode{tag: "font", attrs: map[string]string{"color": "red"}}
	if c := hintStyle(f, "").Color; c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("font color=red → %+v, want red", c)
	}
}

func TestNoHintsForPlainElement(t *testing.T) {
	// An element with no presentational attributes yields no hint declarations.
	if ds := presentationalHints(&fakeNode{tag: "div"}); len(ds) != 0 {
		t.Errorf("plain div hints = %v, want none", ds)
	}
}
