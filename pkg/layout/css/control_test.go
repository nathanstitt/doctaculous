package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestClassifyControl(t *testing.T) {
	cases := []struct {
		tag  string
		typ  string // "" = attribute absent
		want cssbox.ControlKind
		skip bool
	}{
		{"input", "", cssbox.CtrlText, false}, // bare input → text
		{"input", "text", cssbox.CtrlText, false},
		{"input", "email", cssbox.CtrlText, false}, // text-like
		{"input", "number", cssbox.CtrlText, false},
		{"input", "search", cssbox.CtrlText, false},
		{"input", "tel", cssbox.CtrlText, false},
		{"input", "url", cssbox.CtrlText, false},
		{"input", "password", cssbox.CtrlPassword, false},
		{"input", "checkbox", cssbox.CtrlCheckbox, false},
		{"input", "radio", cssbox.CtrlRadio, false},
		{"input", "submit", cssbox.CtrlButton, false},
		{"input", "button", cssbox.CtrlButton, false},
		{"input", "reset", cssbox.CtrlButton, false},
		{"input", "hidden", cssbox.CtrlNone, true}, // no box
		{"input", "file", cssbox.CtrlText, false},  // fallback to text field
		{"input", "image", cssbox.CtrlText, false},
		{"input", "color", cssbox.CtrlText, false}, // unknown → text
		{"input", "range", cssbox.CtrlText, false},
		{"button", "", cssbox.CtrlButton, false},
		{"textarea", "", cssbox.CtrlTextarea, false},
		{"select", "", cssbox.CtrlSelect, false},
		{"div", "", cssbox.CtrlNone, false}, // not a control
		{"img", "", cssbox.CtrlNone, false},
	}
	for _, c := range cases {
		attrs := map[string]string{}
		if c.typ != "" {
			attrs["type"] = c.typ
		}
		got, skip := classifyControl(c.tag, attrs)
		if got != c.want || skip != c.skip {
			t.Errorf("classifyControl(%q,type=%q) = (%v,%v), want (%v,%v)",
				c.tag, c.typ, got, skip, c.want, c.skip)
		}
	}
}

// firstElement parses src and returns the first element matching tag (depth-first).
func firstElement(t *testing.T, src, tag string) *html.Element {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var find func(n *html.Element) *html.Element
	find = func(n *html.Element) *html.Element {
		if n == nil {
			return nil
		}
		if n.Tag() == tag {
			return n
		}
		for _, c := range n.Children() {
			if ce, ok := c.(*html.Element); ok {
				if r := find(ce); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return find(doc.Root)
}

func TestControlText(t *testing.T) {
	if got := controlText(firstElement(t, `<button>Click Me</button>`, "button"), cssbox.CtrlButton); got != "Click Me" {
		t.Errorf("button text = %q, want %q", got, "Click Me")
	}
	if got := controlText(firstElement(t, "<textarea>line one</textarea>", "textarea"), cssbox.CtrlTextarea); got != "line one" {
		t.Errorf("textarea text = %q, want %q", got, "line one")
	}
	// select: selected option wins.
	sel := `<select><option>One</option><option selected>Two</option></select>`
	if got := controlText(firstElement(t, sel, "select"), cssbox.CtrlSelect); got != "Two" {
		t.Errorf("select text = %q, want %q (selected option)", got, "Two")
	}
	// select: no selected → first option.
	sel2 := `<select><option>Alpha</option><option>Beta</option></select>`
	if got := controlText(firstElement(t, sel2, "select"), cssbox.CtrlSelect); got != "Alpha" {
		t.Errorf("select text = %q, want %q (first option)", got, "Alpha")
	}
	// empty select → empty string, no panic.
	if got := controlText(firstElement(t, `<select></select>`, "select"), cssbox.CtrlSelect); got != "" {
		t.Errorf("empty select text = %q, want empty", got)
	}
}

// ctrlBox builds a minimal replaced control box with the given kind, attrs, and a
// default 13px font (the UA control default), for sizing tests.
func ctrlBox(kind cssbox.ControlKind, attrs map[string]string) *cssbox.Box {
	st := gcss.ComputedStyle{
		FontFamily: "sans-serif",
		FontSizePt: 13,
		Width:      gcss.Length{Unit: gcss.UnitAuto},
		Height:     gcss.Length{Unit: gcss.UnitAuto},
		MaxWidth:   gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight:  gcss.Length{Unit: gcss.UnitAuto},
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	return &cssbox.Box{
		Kind:     cssbox.BoxReplaced,
		Display:  cssbox.DisplayInlineBlock,
		Style:    st,
		Replaced: &cssbox.ReplacedContent{Tag: "input", Control: kind, Attrs: attrs},
	}
}

func TestControlIntrinsicSizeNonZero(t *testing.T) {
	eng := New(nil, nil, nil)
	ctx := context.Background()
	cases := []struct {
		name string
		box  *cssbox.Box
	}{
		{"text-size0", ctrlBox(cssbox.CtrlText, map[string]string{"size": "0"})},
		{"text-bare", ctrlBox(cssbox.CtrlText, nil)},
		{"button-empty", func() *cssbox.Box {
			b := ctrlBox(cssbox.CtrlButton, nil)
			b.Replaced.Text = ""
			return b
		}()},
		{"textarea-bare", ctrlBox(cssbox.CtrlTextarea, nil)},
		{"checkbox", ctrlBox(cssbox.CtrlCheckbox, nil)},
		{"select-empty", ctrlBox(cssbox.CtrlSelect, nil)},
		{"radio", ctrlBox(cssbox.CtrlRadio, nil)},
		{"password-empty", ctrlBox(cssbox.CtrlPassword, nil)},
	}
	for _, c := range cases {
		w, h := eng.controlIntrinsicSize(ctx, c.box)
		if w <= 0 || h <= 0 {
			t.Errorf("%s: intrinsic size = (%.1f, %.1f), want both > 0", c.name, w, h)
		}
	}
}

// buildControlBox parses src, builds the cssbox tree, and returns the first
// BoxReplaced whose Replaced.Control != CtrlNone (depth-first), or nil.
func buildControlBox(t *testing.T, src string) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var find func(b *cssbox.Box) *cssbox.Box
	find = func(b *cssbox.Box) *cssbox.Box {
		if b == nil {
			return nil
		}
		if b.Kind == cssbox.BoxReplaced && b.Replaced != nil && b.Replaced.Control != cssbox.CtrlNone {
			return b
		}
		for _, c := range b.Children {
			if r := find(c); r != nil {
				return r
			}
		}
		return nil
	}
	return find(root)
}

func TestBuildControlBoxes(t *testing.T) {
	// A checkbox becomes a replaced leaf with no children.
	cb := buildControlBox(t, `<body><input type=checkbox checked></body>`)
	if cb == nil || cb.Replaced.Control != cssbox.CtrlCheckbox {
		t.Fatalf("checkbox not generated as a control replaced box")
	}
	if len(cb.Children) != 0 {
		t.Errorf("control box has %d children, want 0 (leaf)", len(cb.Children))
	}
	if _, ok := cb.Replaced.Attrs["checked"]; !ok {
		t.Errorf("checked attribute not snapshotted")
	}
	// A button carries its label and generates no child boxes (no leakage).
	bt := buildControlBox(t, `<body><button>Go</button></body>`)
	if bt == nil || bt.Replaced.Text != "Go" || len(bt.Children) != 0 {
		t.Errorf("button box = %+v, want Text=Go and no children", bt)
	}
	// A select carries the selected option text and no children.
	sl := buildControlBox(t, `<body><select><option>A</option><option selected>B</option></select></body>`)
	if sl == nil || sl.Replaced.Text != "B" || len(sl.Children) != 0 {
		t.Errorf("select box = %+v, want Text=B and no children", sl)
	}
	// type=hidden generates no box at all.
	hid := buildControlBox(t, `<body><input type=hidden value=x></body>`)
	if hid != nil {
		t.Errorf("hidden input generated a box, want none")
	}
}

func TestControlUsedSizeDefaultsAndOverride(t *testing.T) {
	eng := New(nil, nil, nil)
	ctx := context.Background()

	// No CSS width → character-count default (well above zero).
	def := ctrlBox(cssbox.CtrlText, map[string]string{"size": "10"})
	w, h := eng.replacedUsedSize(ctx, def, 1000)
	if w < ctrlMinTextW-1 || h <= 0 {
		t.Errorf("default text size = (%.1f,%.1f), want char-count width and >0 height", w, h)
	}

	// Explicit CSS width:50px overrides the intrinsic default.
	over := ctrlBox(cssbox.CtrlText, map[string]string{"size": "10"})
	over.Style.Width = gcss.Length{Value: 50, Unit: gcss.UnitPx}
	w2, _ := eng.replacedUsedSize(ctx, over, 1000)
	if w2 != 50 {
		t.Errorf("CSS-width override = %.1f, want 50", w2)
	}

	// Explicit width:0 is honored (deliberate), NOT floored.
	zero := ctrlBox(cssbox.CtrlText, nil)
	zero.Style.Width = gcss.Length{Value: 0, Unit: gcss.UnitPx}
	w3, _ := eng.replacedUsedSize(ctx, zero, 1000)
	if w3 != 0 {
		t.Errorf("explicit width:0 = %.1f, want 0 (author override wins)", w3)
	}
}

func TestControlIntrinsicSizeScalesWithChars(t *testing.T) {
	eng := New(nil, nil, nil)
	ctx := context.Background()
	nw, _ := eng.controlIntrinsicSize(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "5"}))
	ww, _ := eng.controlIntrinsicSize(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "40"}))
	if !(ww > nw) {
		t.Errorf("size=40 width %.1f should exceed size=5 width %.1f", ww, nw)
	}
}

func TestControlUADefaultsInlineBlock(t *testing.T) {
	b := buildControlBox(t, `<body><input type=text></body>`)
	if b == nil {
		t.Fatal("no control box")
	}
	if b.Display != cssbox.DisplayInlineBlock {
		t.Errorf("input Display = %v, want DisplayInlineBlock (UA default)", b.Display)
	}
}

// renderControlItems lays out a single control and returns its flattened paint items.
func renderControlItems(t *testing.T, src string) []layout.Item {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	eng := New(nil, nil, nil)
	frag := eng.layoutTree(context.Background(), root, 400)
	var items []layout.Item
	if frag != nil {
		items = frag.AppendItems(items)
	}
	return items
}

func countKind(items []layout.Item, k layout.ItemKind) int {
	n := 0
	for _, it := range items {
		if it.Kind == k {
			n++
		}
	}
	return n
}

func hasBorderStyle(items []layout.Item, s layout.BorderStyle) bool {
	for _, it := range items {
		if it.Kind == layout.BorderKind && it.Border.Style == s {
			return true
		}
	}
	return false
}

// A control NOT at the page origin must paint its widget at its real position, not
// at (0,0). Regression test: previously the control's paint coords were not shifted
// when the fragment was repositioned, so every control painted at the top-left.
func TestControlPaintShiftedToPosition(t *testing.T) {
	// Two stacked block divs, each holding a text input; the second input's widget
	// must paint well below the first (its background fill Y must be > the first's).
	src := `<body style="margin:0">` +
		`<div><input type="text" value="a"></div>` +
		`<div><input type="text" value="b"></div>` +
		`</body>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, 400)
	var items []layout.Item
	if frag != nil {
		items = frag.AppendItems(items)
	}
	// Collect the Y of each control background fill (the field face is the widget's
	// BackgroundKind item). There should be two, at clearly different Y positions.
	var bgYs []float64
	for _, it := range items {
		if it.Kind == layout.BackgroundKind {
			bgYs = append(bgYs, it.Rule.YPt)
		}
	}
	if len(bgYs) < 2 {
		t.Fatalf("expected >=2 background fills (one per field), got %d", len(bgYs))
	}
	// The two field fills must be at different Y (the second below the first), not
	// both at the same top position.
	maxY, minY := bgYs[0], bgYs[0]
	for _, y := range bgYs {
		if y > maxY {
			maxY = y
		}
		if y < minY {
			minY = y
		}
	}
	if maxY-minY < 10 {
		t.Errorf("control fills span only %.1f pt in Y (%v); the second field did not shift down — widgets overlap at the top", maxY-minY, bgYs)
	}
}

// A form control as a flex item or grid item must lay out (not collapse the
// container). Regression for the anonymous-item fixup coalescing replaced boxes
// into one inline-run item (which then measured zero), plus measureMaxContent
// returning 0 for a replaced box. Two controls in a flex/grid container must each
// paint their own field background at DISTINCT x positions (side by side).
func TestControlAsFlexAndGridItem(t *testing.T) {
	cases := map[string]string{
		"flex": `<body style="margin:0"><div style="display:flex">` +
			`<input type=text value=a><input type=text value=b></div></body>`,
		"grid": `<body style="margin:0"><div style="display:grid;grid-template-columns:1fr 1fr">` +
			`<input type=text value=a><input type=text value=b></div></body>`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			items := renderControlItems(t, src)
			// Each control field emits a BackgroundKind fill; two side-by-side controls
			// must produce two fills at different X (the container did not collapse).
			var xs []float64
			for _, it := range items {
				if it.Kind == layout.BackgroundKind {
					xs = append(xs, it.Rule.XPt)
				}
			}
			if len(xs) < 2 {
				t.Fatalf("%s: expected >=2 control field fills, got %d (container collapsed — replaced item not sized)", name, len(xs))
			}
			minX, maxX := xs[0], xs[0]
			for _, x := range xs {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
			}
			if maxX-minX < 10 {
				t.Errorf("%s: control fills span only %.1f pt in X (%v); the two controls did not lay out side by side", name, maxX-minX, xs)
			}
		})
	}
}

// TestControlCellNotTreatedEmpty asserts that isEmptyCellFragment returns false for a
// fragment that has only f.Control set (regression: the guard was missing f.Control == nil).
// A cell whose only content is a form control would be misclassified as empty, causing
// empty-cells:hide to suppress its border and background.
func TestControlCellNotTreatedEmpty(t *testing.T) {
	// A genuinely empty fragment — all nil/zero — is empty.
	empty := &Fragment{}
	if !isEmptyCellFragment(empty) {
		t.Error("bare empty fragment should be treated as empty")
	}

	// A fragment with only Control set must NOT be empty (regression guard).
	withControl := &Fragment{
		Control: &ControlContent{Kind: 1}, // any non-nil ControlContent
	}
	if isEmptyCellFragment(withControl) {
		t.Error("fragment with f.Control set must not be treated as empty (isEmptyCellFragment missing f.Control==nil check)")
	}
}

func TestButtonLabelSources(t *testing.T) {
	// <button>Go</button> → label "Go" (2 glyphs).
	btnEl := renderControlItems(t, `<body><button>Go</button></body>`)
	// <input type=submit value=Send> → label "Send" (4 glyphs) via the value attr.
	inSubmit := renderControlItems(t, `<body><input type=submit value=Send></body>`)
	// bare <input type=submit> → default label "Submit" (6 glyphs).
	inDefault := renderControlItems(t, `<body><input type=submit></body>`)
	// <input type=button> with no value/label → empty (0 glyphs).
	inEmpty := renderControlItems(t, `<body><input type=button></body>`)

	if countKind(btnEl, layout.GlyphKind) == 0 {
		t.Error("<button>Go</button> emitted no label glyphs")
	}
	if countKind(inSubmit, layout.GlyphKind) == 0 {
		t.Error("<input type=submit value=Send> emitted no label glyphs (value not used)")
	}
	if countKind(inDefault, layout.GlyphKind) == 0 {
		t.Error("bare <input type=submit> emitted no label glyphs (no default 'Submit')")
	}
	if countKind(inEmpty, layout.GlyphKind) != 0 {
		t.Errorf("<input type=button> with no label should emit 0 glyphs, got %d", countKind(inEmpty, layout.GlyphKind))
	}
}

// A button is sized to its rendered label (buttonLabel), not just b.Replaced.Text:
// an <input type=submit value="..."> whose label comes from the value attribute must
// be wider than the minimum button width, so the label does not overflow the button.
func TestButtonSizedToValueLabel(t *testing.T) {
	eng := New(nil, nil, nil)
	ctx := context.Background()
	// A submit input with a long value label.
	b := ctrlBox(cssbox.CtrlButton, map[string]string{"type": "submit", "value": "Submit Changes Now"})
	w, _ := eng.controlIntrinsicSize(ctx, b)
	if w <= ctrlMinButtonW {
		t.Errorf("submit button width = %.1f, want > min %d (sized to its value label)", w, ctrlMinButtonW)
	}
	// Its width must cover the label text + padding: at least the measured label width.
	labelW := eng.textWidth(b, "Submit Changes Now")
	if w < labelW {
		t.Errorf("submit button width %.1f is narrower than its label %.1f — label would overflow", w, labelW)
	}
}

func TestControlPaintChrome(t *testing.T) {
	// Text field: a background fill + inset (sunken) borders.
	tf := renderControlItems(t, `<body><input type=text value=hi></body>`)
	if countKind(tf, layout.BackgroundKind) == 0 || !hasBorderStyle(tf, layout.BorderInset) {
		t.Errorf("text field: want a background + inset borders; got bg=%d inset=%v",
			countKind(tf, layout.BackgroundKind), hasBorderStyle(tf, layout.BorderInset))
	}
	// Button: outset (raised) borders.
	bt := renderControlItems(t, `<body><button>Go</button></body>`)
	if !hasBorderStyle(bt, layout.BorderOutset) {
		t.Errorf("button: want outset borders")
	}
	// Checked checkbox paints more than an empty one (the checkmark).
	cbChecked := renderControlItems(t, `<body><input type=checkbox checked></body>`)
	cbEmpty := renderControlItems(t, `<body><input type=checkbox></body>`)
	paintedChecked := countKind(cbChecked, layout.GlyphKind) + countKind(cbChecked, layout.BackgroundKind)
	paintedEmpty := countKind(cbEmpty, layout.GlyphKind) + countKind(cbEmpty, layout.BackgroundKind)
	if paintedChecked <= paintedEmpty {
		t.Errorf("checked checkbox should paint more than an empty one (the checkmark)")
	}
}

func TestRadioPaintsCircularChrome(t *testing.T) {
	// A radio paints synthesized circular outlines (glyph items), NOT the
	// checkbox's rectangular fill+bevel: no BackgroundKind rect and no bevel
	// borders, and at least two glyph outlines (gray ring + field disc).
	rd := renderControlItems(t, `<body><input type=radio></body>`)
	if n := countKind(rd, layout.BorderKind); n != 0 {
		t.Errorf("radio painted %d bevel border strips; circular chrome must not use rect bevels", n)
	}
	if n := countKind(rd, layout.GlyphKind); n < 2 {
		t.Errorf("radio painted %d glyph outlines, want >= 2 (ring + disc)", n)
	}
	// A checked radio adds the dot.
	checked := renderControlItems(t, `<body><input type=radio checked></body>`)
	if countKind(checked, layout.GlyphKind) != countKind(rd, layout.GlyphKind)+1 {
		t.Errorf("checked radio should paint exactly one more outline (the dot): %d vs %d",
			countKind(checked, layout.GlyphKind), countKind(rd, layout.GlyphKind))
	}
}

func TestCheckboxMarkIsSyntheticAndCentered(t *testing.T) {
	// The checkmark is a synthesized outline positioned so its unit box maps onto
	// the control's content box (baseline at the box bottom, size = box height) —
	// never a font glyph subject to per-face metrics.
	items := renderControlItems(t, `<body><input type=checkbox checked></body>`)
	var glyphs []layout.GlyphItem
	for _, it := range items {
		if it.Kind == layout.GlyphKind {
			glyphs = append(glyphs, it.Glyph)
		}
	}
	if len(glyphs) != 1 {
		t.Fatalf("checked checkbox painted %d glyph outlines, want exactly 1 (the mark)", len(glyphs))
	}
	mark := glyphs[0]
	if mark.Outline == nil {
		t.Fatal("mark has no outline")
	}
	if mark.SizePt != ctrlCheckSize {
		t.Errorf("mark SizePt = %v, want the box side %v (unit box maps onto the content box)", mark.SizePt, float64(ctrlCheckSize))
	}
}
