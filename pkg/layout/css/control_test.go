package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
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

func TestControlIntrinsicSizeScalesWithChars(t *testing.T) {
	eng := New(nil, nil, nil)
	ctx := context.Background()
	nw, _ := eng.controlIntrinsicSize(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "5"}))
	ww, _ := eng.controlIntrinsicSize(ctx, ctrlBox(cssbox.CtrlText, map[string]string{"size": "40"}))
	if !(ww > nw) {
		t.Errorf("size=40 width %.1f should exceed size=5 width %.1f", ww, nw)
	}
}
