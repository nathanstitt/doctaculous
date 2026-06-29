package css

import (
	"testing"

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
