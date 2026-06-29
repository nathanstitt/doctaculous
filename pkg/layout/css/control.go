package css

import (
	"context"
	"image/color"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
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

// ControlContent is a form control's paint payload carried on a Fragment, painted
// in the content box (CX,CY,CW,CH, page space, shifting with the fragment).
type ControlContent struct {
	Kind           cssbox.ControlKind
	Text           string
	Placeholder    bool
	Checked        bool
	Disabled       bool
	Face           *font.Face
	FontSizePt     float64
	CX, CY, CW, CH float64
}

// classic-native chrome colors.
var (
	ctrlFieldBG  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ctrlButtonBG = color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	ctrlInk      = color.RGBA{0x10, 0x10, 0x10, 0xff}
	ctrlGray     = color.RGBA{0x99, 0x99, 0x99, 0xff}
	ctrlDisabled = color.RGBA{0xee, 0xee, 0xee, 0xff}
)

// controlContentFor builds the paint payload for control box b, given its content
// box (cx,cy,w,h) in page space. The face is resolved synchronously.
func (e *Engine) controlContentFor(b *cssbox.Box, cx, cy, w, h float64) *ControlContent {
	face, _ := e.faces.Resolve(b.Style.FontFamily, styleFor(b))
	cc := &ControlContent{
		Kind:       b.Replaced.Control,
		Face:       face,
		FontSizePt: b.Style.FontSizePt,
		CX:         cx, CY: cy, CW: w, CH: h,
	}
	_, cc.Checked = b.Replaced.Attrs["checked"]
	_, cc.Disabled = b.Replaced.Attrs["disabled"]
	switch cc.Kind {
	case cssbox.CtrlButton:
		cc.Text = buttonLabel(b)
	case cssbox.CtrlTextarea, cssbox.CtrlSelect:
		cc.Text = b.Replaced.Text
	default:
		if v, ok := b.Replaced.Attrs["value"]; ok && v != "" {
			cc.Text = v
		} else if p, ok := b.Replaced.Attrs["placeholder"]; ok {
			cc.Text, cc.Placeholder = p, true
		}
	}
	if cc.Kind == cssbox.CtrlPassword && !cc.Placeholder {
		cc.Text = strings.Repeat("•", len([]rune(cc.Text)))
	}
	return cc
}

// buttonLabel returns a button control's label: the extracted text of a <button>
// element if present, else an <input> button's value attribute, else a type-based
// default ("Submit"/"Reset" for submit/reset inputs, matching browsers; empty
// otherwise).
func buttonLabel(b *cssbox.Box) string {
	if b.Replaced.Text != "" {
		return b.Replaced.Text
	}
	if v, ok := b.Replaced.Attrs["value"]; ok && v != "" {
		return v
	}
	switch strings.ToLower(strings.TrimSpace(b.Replaced.Attrs["type"])) {
	case "submit":
		return "Submit"
	case "reset":
		return "Reset"
	default:
		return ""
	}
}

type ctrlAlign int

const (
	alignLeft ctrlAlign = iota
	alignCenter
)

// append emits the control's chrome + text as paint items into dst, using only
// existing item kinds.
func (cc *ControlContent) append(dst []layout.Item) []layout.Item {
	fill := func(x, y, w, h float64, c color.RGBA) {
		dst = append(dst, layout.Item{Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: x, YPt: y, WPt: w, HPt: h, Color: c}})
	}
	bevel := func(style layout.BorderStyle) {
		for _, s := range [...]layout.EdgeSide{layout.EdgeTop, layout.EdgeRight, layout.EdgeBottom, layout.EdgeLeft} {
			dst = append(dst, layout.Item{Kind: layout.BorderKind, Border: cc.edge(s, style)})
		}
	}
	switch cc.Kind {
	case cssbox.CtrlCheckbox, cssbox.CtrlRadio:
		bg := ctrlFieldBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderInset)
		if cc.Checked {
			ink := ctrlInk
			if cc.Disabled {
				ink = ctrlGray
			}
			if cc.Kind == cssbox.CtrlRadio {
				d := cc.CW * 0.4
				fill(cc.CX+(cc.CW-d)/2, cc.CY+(cc.CH-d)/2, d, d, ink)
			} else if !cc.appendGlyphCentered(&dst, '✓', ink) {
				t := cc.CW * 0.12
				fill(cc.CX+cc.CW*0.2, cc.CY+cc.CH*0.5, cc.CW*0.25, t, ink)
				fill(cc.CX+cc.CW*0.4, cc.CY+cc.CH*0.3, t, cc.CH*0.4, ink)
			}
		}
		return dst
	case cssbox.CtrlButton:
		bg := ctrlButtonBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderOutset)
		cc.appendText(&dst, alignCenter)
		return dst
	default: // text/password/textarea/select fields
		bg := ctrlFieldBG
		if cc.Disabled {
			bg = ctrlDisabled
		}
		fill(cc.CX, cc.CY, cc.CW, cc.CH, bg)
		bevel(layout.BorderInset)
		dst = append(dst, layout.Item{Kind: layout.ClipPushKind,
			Rule: layout.RuleItem{XPt: cc.CX, YPt: cc.CY, WPt: cc.CW, HPt: cc.CH}})
		cc.appendText(&dst, alignLeft)
		dst = append(dst, layout.Item{Kind: layout.ClipPopKind})
		if cc.Kind == cssbox.CtrlSelect {
			ink := ctrlInk
			if cc.Disabled {
				ink = ctrlGray
			}
			cc.appendTriangle(&dst, ink)
		}
		return dst
	}
}

// edge returns one 1pt border strip for side s with the given 3D style, around the
// control's border box (the content box CX/CY/CW/CH is already inset by ctrlBorder).
func (cc *ControlContent) edge(s layout.EdgeSide, style layout.BorderStyle) layout.BorderItem {
	x, y, w, h := cc.CX-ctrlBorder, cc.CY-ctrlBorder, cc.CW+2*ctrlBorder, cc.CH+2*ctrlBorder
	bi := layout.BorderItem{Color: ctrlGray, Style: style, Side: s}
	switch s {
	case layout.EdgeTop:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y, w, ctrlBorder
	case layout.EdgeBottom:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y+h-ctrlBorder, w, ctrlBorder
	case layout.EdgeLeft:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x, y, ctrlBorder, h
	case layout.EdgeRight:
		bi.XPt, bi.YPt, bi.WPt, bi.HPt = x+w-ctrlBorder, y, ctrlBorder, h
	}
	return bi
}

// appendText emits cc.Text as a single baseline row of glyphs, left- or
// center-aligned within the content box. A nil face or missing glyph is skipped.
func (cc *ControlContent) appendText(dst *[]layout.Item, align ctrlAlign) {
	if cc.Text == "" || cc.Face == nil {
		return
	}
	ink := ctrlInk
	if cc.Placeholder || cc.Disabled {
		ink = ctrlGray
	}
	asc, _, _ := cc.Face.Metrics()
	baseline := cc.CY + asc*cc.FontSizePt + ctrlPadY
	width := 0.0
	for _, r := range cc.Text {
		if _, adv, ok := cc.Face.Glyph(r); ok {
			width += adv * cc.FontSizePt
		}
	}
	x := cc.CX + ctrlPadX
	if align == alignCenter {
		if extra := cc.CW - width; extra > 0 {
			x = cc.CX + extra/2
		}
	}
	for _, r := range cc.Text {
		outline, adv, ok := cc.Face.Glyph(r)
		if ok && outline != nil {
			*dst = append(*dst, layout.Item{Kind: layout.GlyphKind,
				Glyph: layout.GlyphItem{Outline: outline, XPt: x, YPt: baseline, SizePt: cc.FontSizePt, Color: ink}})
		}
		if ok {
			x += adv * cc.FontSizePt
		}
	}
}

// appendGlyphCentered emits a single glyph centered in the content box; returns
// false (drawing nothing) when the face lacks the glyph, so the caller can fall back.
func (cc *ControlContent) appendGlyphCentered(dst *[]layout.Item, r rune, ink color.RGBA) bool {
	if cc.Face == nil {
		return false
	}
	outline, adv, ok := cc.Face.Glyph(r)
	if !ok || outline == nil {
		return false
	}
	asc, desc, _ := cc.Face.Metrics()
	gw := adv * cc.FontSizePt
	baseline := cc.CY + (cc.CH+(asc-desc)*cc.FontSizePt)/2
	x := cc.CX + (cc.CW-gw)/2
	*dst = append(*dst, layout.Item{Kind: layout.GlyphKind,
		Glyph: layout.GlyphItem{Outline: outline, XPt: x, YPt: baseline, SizePt: cc.FontSizePt, Color: ink}})
	return true
}

// appendTriangle emits a small downward triangle in the select's right-side box,
// drawn as stacked strokes (a glyph-free approximation that always renders).
func (cc *ControlContent) appendTriangle(dst *[]layout.Item, ink color.RGBA) {
	boxX := cc.CX + cc.CW - ctrlSelectTri
	cxp := boxX + ctrlSelectTri/2
	cyp := cc.CY + cc.CH/2
	for i := 0; i < 3; i++ {
		half := float64(3 - i)
		*dst = append(*dst, layout.Item{Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: cxp - half, YPt: cyp - 2 + float64(i), WPt: 2 * half, HPt: 1, Color: ink}})
	}
}
