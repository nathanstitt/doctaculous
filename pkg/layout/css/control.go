package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// classifyControl maps an element (its lowercased tag + attributes) to the form
// control it renders as. It returns CtrlNone for non-control elements. skip is true
// only for <input type=hidden>, which generates no box at all (matching browsers).
// An unknown or unsupported input type falls back to CtrlText (the browser default),
// so no <input> is ever dropped; type=file/image also fall back to a text field.
func classifyControl(tag string, attrs map[string]string) (kind cssbox.ControlKind, skip bool) {
	switch tag {
	case "textarea":
		return cssbox.CtrlTextarea, false
	case "select":
		return cssbox.CtrlSelect, false
	case "button":
		return cssbox.CtrlButton, false
	case "input":
		switch strings.ToLower(strings.TrimSpace(attrs["type"])) {
		case "hidden":
			return cssbox.CtrlNone, true
		case "password":
			return cssbox.CtrlPassword, false
		case "checkbox":
			return cssbox.CtrlCheckbox, false
		case "radio":
			return cssbox.CtrlRadio, false
		case "submit", "button", "reset":
			return cssbox.CtrlButton, false
		default:
			// text, email, url, tel, search, number, file, image, color, range, date,
			// missing/unknown — all render as a text field.
			return cssbox.CtrlText, false
		}
	}
	return cssbox.CtrlNone, false
}
