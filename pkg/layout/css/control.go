package css

import (
	"context"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/font"
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

// Control chrome metrics, in points (browser-typical defaults).
const (
	ctrlPadX         = 2   // text field internal horizontal padding (each side)
	ctrlPadY         = 1   // text field internal vertical padding (each side)
	ctrlBtnPadX      = 6   // button internal horizontal padding (each side)
	ctrlBorder       = 1   // chrome border thickness (each side)
	ctrlCheckSize    = 13  // checkbox/radio fixed square side
	ctrlSelectTri    = 16  // select dropdown-triangle box width
	ctrlMinTextW     = 120 // minimum text-field / select width, points
	ctrlMinTextareaW = 150 // minimum textarea width, points
	ctrlMinTextareaH = 40  // minimum textarea height, points
	ctrlMinButtonW   = 24  // minimum button width, points
	ctrlMinFieldH    = 16  // minimum field/button height (one line + chrome), points
)

// controlIntrinsicSize returns the intrinsic content-box size (points) of a form
// control, measured from its resolved font and floored to a per-control minimum so
// it is NEVER zero on either axis (a degenerate measurement — size=0, empty
// content, an unresolvable font — still yields the standard default control size).
// replacedUsedSize applies CSS width/height overrides and min/max on top, so an
// explicit author width/height still wins.
func (e *Engine) controlIntrinsicSize(ctx context.Context, b *cssbox.Box) (w, h float64) {
	kind := cssbox.CtrlNone
	if b.Replaced != nil {
		kind = b.Replaced.Control
	}
	if kind == cssbox.CtrlCheckbox || kind == cssbox.CtrlRadio {
		return ctrlCheckSize, ctrlCheckSize
	}
	ch := e.charWidth(b)           // one '0' advance in points
	line := e.controlLineHeight(b) // one line box height in points

	switch kind {
	case cssbox.CtrlButton:
		labelW := e.textWidth(b, b.Replaced.Text)
		w = labelW + 2*ctrlBtnPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinButtonW), max2(h, ctrlMinFieldH)
	case cssbox.CtrlTextarea:
		cols := attrIntOr(b, "cols", 20)
		rows := attrIntOr(b, "rows", 2)
		w = float64(cols)*ch + 2*ctrlPadX + 2*ctrlBorder
		h = float64(rows)*line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextareaW), max2(h, ctrlMinTextareaH)
	case cssbox.CtrlSelect:
		textW := e.textWidth(b, b.Replaced.Text)
		w = textW + ctrlSelectTri + 2*ctrlPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextW), max2(h, ctrlMinFieldH)
	default: // CtrlText, CtrlPassword
		size := attrIntOr(b, "size", 20)
		w = float64(size)*ch + 2*ctrlPadX + 2*ctrlBorder
		h = line + 2*ctrlPadY + 2*ctrlBorder
		return max2(w, ctrlMinTextW), max2(h, ctrlMinFieldH)
	}
}

// charWidth returns the width of one '0' in the control's resolved font (the CSS ch
// unit), in points. Falls back to 0.5em when the face or glyph is unavailable.
func (e *Engine) charWidth(b *cssbox.Box) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if ok && face != nil {
		if _, adv, ok := face.Glyph('0'); ok && adv > 0 {
			return adv * fs
		}
	}
	return 0.5 * fs
}

// controlLineHeight returns one line box height (points) for the control's font.
func (e *Engine) controlLineHeight(b *cssbox.Box) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if ok && face != nil {
		asc, desc, _ := face.Metrics()
		if asc+desc > 0 {
			return (asc + desc) * 1.15 * fs
		}
	}
	return 1.2 * fs
}

// textWidth measures the width (points) of s in the control's resolved font.
// Sum per-rune advances, silently skipping runes the face has no glyph for
// (matching the shaper's glyph-skip behavior). A string of all-missing glyphs
// yields 0; callers floor the result so a control never collapses to zero width.
func (e *Engine) textWidth(b *cssbox.Box, s string) float64 {
	fs := b.Style.FontSizePt
	face, ok := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	if !ok || face == nil {
		return float64(len([]rune(s))) * 0.5 * fs
	}
	total := 0.0
	for _, r := range s {
		if _, adv, ok := face.Glyph(r); ok {
			total += adv * fs
		}
	}
	return total
}

// styleFor maps a box's computed weight/slant to a font.Style.
func styleFor(b *cssbox.Box) font.Style {
	return font.Style{Bold: b.Style.Bold, Italic: b.Style.Italic}
}

// attrIntOr returns the positive integer value of attribute key on b's replaced
// content, or def when absent/invalid.
func attrIntOr(b *cssbox.Box, key string, def int) int {
	if b.Replaced == nil {
		return def
	}
	if v, ok := b.Replaced.Attrs[key]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
