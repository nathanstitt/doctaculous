package css

import (
	"testing"

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
