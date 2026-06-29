package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/html"
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

// controlText extracts a control's display text from its parsed element. For a
// <select> it returns the selected <option>'s text (the first option with a
// "selected" attribute), else the first option's text, else "". For button and
// textarea it returns the concatenated descendant text, trimmed. For input kinds it
// returns "" (their text comes from the value attribute, handled in box generation).
func controlText(e *html.Element, kind cssbox.ControlKind) string {
	if e == nil {
		return ""
	}
	switch kind {
	case cssbox.CtrlSelect:
		var first *html.Element
		for _, opt := range childElements(e, "option") {
			if first == nil {
				first = opt
			}
			if _, ok := opt.Attr("selected"); ok {
				return strings.TrimSpace(textOf(opt))
			}
		}
		if first != nil {
			return strings.TrimSpace(textOf(first))
		}
		return ""
	case cssbox.CtrlButton, cssbox.CtrlTextarea:
		return strings.TrimSpace(textOf(e))
	default:
		return ""
	}
}

// childElements returns e's direct child elements with the given tag.
func childElements(e *html.Element, tag string) []*html.Element {
	var out []*html.Element
	for _, c := range e.Children() {
		if ce, ok := c.(*html.Element); ok && ce.Tag() == tag {
			out = append(out, ce)
		}
	}
	return out
}

// textOf returns the concatenated text of e's descendant text nodes.
func textOf(e *html.Element) string {
	var b strings.Builder
	var walk func(n *html.Element)
	walk = func(n *html.Element) {
		for _, c := range n.Children() {
			switch cc := c.(type) {
			case *html.Text:
				b.WriteString(cc.Data) // Data is an exported FIELD, not a method
			case *html.Element:
				walk(cc)
			}
		}
	}
	walk(e)
	return b.String()
}
