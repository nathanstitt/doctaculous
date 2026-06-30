package css

import (
	"image/color"
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
)

// appendMarginBoxes lays out and appends a page's @page margin boxes (running
// headers/footers) to its item list, AFTER the page content so they paint over the
// margin band. g carries the page geometry (size + margins + the resolved UsedPage);
// pageIndex is the zero-based page number and pageCount the total — both feed the page
// counters counter(page) / counter(pages). It is a no-op when the page has no @page
// rule or no margin boxes (the common case → byte-identical).
func (e *Engine) appendMarginBoxes(items []layout.Item, g pageGeom, pageIndex, pageCount int, ps pageStrings) []layout.Item {
	if !g.used.HasRule || len(g.used.MarginBoxes) == 0 {
		return items
	}
	// First pass: resolve each box's text + measure its width (for edge distribution).
	type mbItem struct {
		slot  gcss.MarginBoxSlot
		text  string
		decls []gcss.Declaration
		width float64
	}
	var items2 []mbItem
	boxW := map[gcss.MarginBoxSlot]float64{}
	for _, mb := range g.used.MarginBoxes {
		text := resolveMarginContentWithStrings(mb.Content, pageIndex+1, pageCount, ps)
		if text == "" {
			continue
		}
		cs := gcss.Stylesheet{}.ComputeMarginBox(mb.Decls, marginBoxBaseStyle())
		run := inline.Run{Text: text, Family: cs.FontFamily, Bold: cs.Bold, Italic: cs.Italic, SizePt: cs.FontSizePt, Color: cs.Color}
		glyphs := inline.Shape(e.faces, []inline.Run{run}, e.logf)
		w := 0.0
		if len(glyphs) > 0 {
			w = inline.MakeLine(glyphs).WidthPt
		}
		boxW[mb.Slot] = w
		items2 = append(items2, mbItem{slot: mb.Slot, text: text, decls: mb.Decls, width: w})
	}
	// Second pass: place each box in its distributed rect.
	for _, it := range items2 {
		r := marginBoxRectShared(it.slot, g, boxW)
		if r.w <= 0 || r.h <= 0 {
			continue
		}
		items = e.appendMarginText(items, it.text, it.decls, r)
	}
	return items
}

// resolveMarginContent resolves an @page margin box `content` value to a string for the
// given 1-based page number and total page count, with NO string-set values available
// (string(name) resolves to ""). It is a thin wrapper over resolveMarginContentWithStrings
// for callers (and the early-return single-page paths) that carry no per-page snapshot.
func resolveMarginContent(content string, page, pages int) string {
	return resolveMarginContentWithStrings(content, page, pages, pageStrings{})
}

// resolveMarginContentWithStrings resolves an @page margin box `content` value to a string
// for the given 1-based page number and total page count, using ps (the page's string-set
// snapshot) to resolve string(). It supports a sequence of components:
// a quoted "literal", counter(page), counter(pages), counter(page|pages, <style>) where
// <style> is any list-style the css.FormatCounter helper handles (decimal,
// decimal-leading-zero, lower/upper-roman, lower/upper-alpha; an unknown style falls back
// to decimal), and string(name) / string(name, first|last|start|first-except) (CSS GCPM
// running strings — the value carried into / first set on / last set through this page).
// Components are concatenated.
// An unsupported component (e.g. element(), attr()) contributes nothing — a documented
// deferral (see the design doc). An empty or `normal`/`none` value yields "".
func resolveMarginContentWithStrings(content string, page, pages int, ps pageStrings) string {
	content = strings.TrimSpace(content)
	if content == "" || content == "normal" || content == "none" {
		return ""
	}
	var b strings.Builder
	for _, comp := range splitContentComponents(content) {
		switch {
		case len(comp) >= 2 && (comp[0] == '"' || comp[0] == '\''):
			b.WriteString(unquote(comp))
		case strings.HasPrefix(comp, "counter("):
			inner := strings.TrimSuffix(strings.TrimPrefix(comp, "counter("), ")")
			parts := strings.SplitN(inner, ",", 2)
			name := strings.TrimSpace(parts[0])
			style := "decimal"
			if len(parts) == 2 {
				if s := strings.TrimSpace(parts[1]); s != "" {
					style = s
				}
			}
			switch name {
			case "page":
				b.WriteString(gcss.FormatCounter(page, style))
			case "pages":
				b.WriteString(gcss.FormatCounter(pages, style))
			}
		case strings.HasPrefix(comp, "string("):
			inner := strings.TrimSuffix(strings.TrimPrefix(comp, "string("), ")")
			parts := strings.SplitN(inner, ",", 2)
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			keyword := ""
			if len(parts) == 2 {
				keyword = strings.ToLower(strings.TrimSpace(parts[1]))
			}
			b.WriteString(ps.stringValue(name, keyword))
		default:
			// element(...) / attr(...) / unknown: contribute nothing (deferred). Bare
			// unquoted idents are not valid content; skipped.
		}
	}
	return b.String()
}

// splitContentComponents splits a content value into its top-level components
// (whitespace-separated, but keeping quoted strings and counter(...) parens intact).
func splitContentComponents(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
				flush()
			}
		case c == '"' || c == '\'':
			flush()
			quote = c
			cur.WriteByte(c)
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			depth--
			cur.WriteByte(c)
			if depth == 0 {
				flush()
			}
		case (c == ' ' || c == '\t' || c == '\n') && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// marginRect is a margin box's rectangle in page space (points, Y-down).
type marginRect struct{ x, y, w, h float64 }

// marginBoxRect computes the rectangle of margin box slot within page geometry g (CSS
// Paged Media §8.3.1 corner/edge geometry). The corners occupy the intersection of the
// two margins; each edge box spans the page content width/height between the corners.
// For the common single-box-per-edge authoring (@top-center etc.) the three boxes of an
// edge share the edge band; this model gives each the FULL edge span (a simplification —
// see the design doc — adequate for one centered/edge-aligned box per side).
func marginBoxRect(slot gcss.MarginBoxSlot, g pageGeom) marginRect {
	mT, mL := g.marginT, g.marginL
	mR := g.pageW - g.marginL - g.contentW
	mB := g.pageH - g.marginT - g.contentH
	cw, ch := g.contentW, g.contentH
	switch slot {
	// Top edge boxes: y in [0, mT), x spanning the content width.
	case gcss.MarginTopLeft, gcss.MarginTopCenter, gcss.MarginTopRight:
		return marginRect{x: mL, y: 0, w: cw, h: mT}
	// Bottom edge boxes: y in [pageH-mB, pageH).
	case gcss.MarginBottomLeft, gcss.MarginBottomCenter, gcss.MarginBottomRight:
		return marginRect{x: mL, y: g.pageH - mB, w: cw, h: mB}
	// Left edge boxes: x in [0, mL), y spanning the content height.
	case gcss.MarginLeftTop, gcss.MarginLeftMiddle, gcss.MarginLeftBottom:
		return marginRect{x: 0, y: mT, w: mL, h: ch}
	// Right edge boxes.
	case gcss.MarginRightTop, gcss.MarginRightMiddle, gcss.MarginRightBottom:
		return marginRect{x: g.pageW - mR, y: mT, w: mR, h: ch}
	// Corners.
	case gcss.MarginTopLeftCorner:
		return marginRect{x: 0, y: 0, w: mL, h: mT}
	case gcss.MarginTopRightCorner:
		return marginRect{x: g.pageW - mR, y: 0, w: mR, h: mT}
	case gcss.MarginBottomLeftCorner:
		return marginRect{x: 0, y: g.pageH - mB, w: mL, h: mB}
	case gcss.MarginBottomRightCorner:
		return marginRect{x: g.pageW - mR, y: g.pageH - mB, w: mR, h: mB}
	}
	return marginRect{}
}

// marginBoxRectShared computes a margin box's rect, distributing each edge's three
// boxes (left/center/right) within the edge band by their measured content widths
// (CSS Paged Media §8.3.1): the leading box pins to the leading corner, the trailing
// box to the trailing corner, the center box centers. boxW maps a present slot to its
// laid-out content width (in points); a slot absent from boxW reserves no space. Corner
// slots delegate to marginBoxRect (their geometry is unaffected by siblings).
func marginBoxRectShared(slot gcss.MarginBoxSlot, g pageGeom, boxW map[gcss.MarginBoxSlot]float64) marginRect {
	band := marginBoxRect(slot, g) // full-span band for this slot's edge (or the corner rect)
	lead, center, trail, horizontal, ok := edgeTriple(slot)
	if !ok {
		return band // a corner: unchanged
	}
	w := boxW[slot]
	switch {
	case horizontal:
		switch slot {
		case lead:
			return marginRect{x: band.x, y: band.y, w: band.w, h: band.h}
		case trail:
			return marginRect{x: band.x + band.w - w, y: band.y, w: band.w, h: band.h}
		case center:
			return marginRect{x: band.x + (band.w-w)/2, y: band.y, w: band.w, h: band.h}
		}
	default: // vertical edge: distribute along Y
		switch slot {
		case lead:
			return marginRect{x: band.x, y: band.y, w: band.w, h: band.h}
		case trail:
			return marginRect{x: band.x, y: band.y + band.h - w, w: band.w, h: band.h}
		case center:
			return marginRect{x: band.x, y: band.y + (band.h-w)/2, w: band.w, h: band.h}
		}
	}
	return band
}

// edgeTriple returns the three slots of slot's edge (lead, center, trail), whether the
// edge is horizontal (top/bottom) vs vertical (left/right), and ok=false for a corner.
func edgeTriple(slot gcss.MarginBoxSlot) (lead, center, trail gcss.MarginBoxSlot, horizontal, ok bool) {
	switch slot {
	case gcss.MarginTopLeft, gcss.MarginTopCenter, gcss.MarginTopRight:
		return gcss.MarginTopLeft, gcss.MarginTopCenter, gcss.MarginTopRight, true, true
	case gcss.MarginBottomLeft, gcss.MarginBottomCenter, gcss.MarginBottomRight:
		return gcss.MarginBottomLeft, gcss.MarginBottomCenter, gcss.MarginBottomRight, true, true
	case gcss.MarginLeftTop, gcss.MarginLeftMiddle, gcss.MarginLeftBottom:
		return gcss.MarginLeftTop, gcss.MarginLeftMiddle, gcss.MarginLeftBottom, false, true
	case gcss.MarginRightTop, gcss.MarginRightMiddle, gcss.MarginRightBottom:
		return gcss.MarginRightTop, gcss.MarginRightMiddle, gcss.MarginRightBottom, false, true
	}
	return 0, 0, 0, false, false
}

// appendMarginText lays out text on a single line within rect r (the margin box),
// aligned per the box's text-align, vertically centered in the band, and appends its
// glyphs to items. The text styling (font-family/size, color, text-align) is resolved by
// running the box's declarations through the CSS cascade over a default base (serif,
// 11pt, black) — so a margin box styled `font: bold 9pt sans-serif; color: gray` renders
// accordingly.
func (e *Engine) appendMarginText(items []layout.Item, text string, decls []gcss.Declaration, r marginRect) []layout.Item {
	cs := gcss.Stylesheet{}.ComputeMarginBox(decls, marginBoxBaseStyle())
	run := inline.Run{
		Text:   text,
		Family: cs.FontFamily,
		Bold:   cs.Bold,
		Italic: cs.Italic,
		SizePt: cs.FontSizePt,
		Color:  cs.Color,
	}
	glyphs := inline.Shape(e.faces, []inline.Run{run}, e.logf)
	if len(glyphs) == 0 {
		return items // no resolvable font/glyphs
	}
	line := inline.MakeLine(glyphs)
	p := inline.Place(alignOf(cs.TextAlign), r.x, r.w, line.WidthPt, inline.CountSpaces(line.Glyphs), true)
	// Vertical: center the line's text height in the band.
	asc := ascentOfLine(line)
	textH := line.AscentPt + line.DescentPt
	baselineY := r.y + (r.h-textH)/2 + asc
	x := p.StartX
	for gi := range line.Glyphs {
		g := &line.Glyphs[gi]
		if g.Outline != nil {
			items = append(items, layout.Item{
				Kind: layout.GlyphKind,
				Glyph: layout.GlyphItem{
					Outline: g.Outline, XPt: x, YPt: baselineY, SizePt: g.SizePt,
					Color: color.RGBA{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
				},
			})
		}
		x += g.Advance
		if g.Space {
			x += p.ExtraPerSpace
		}
	}
	return items
}

// marginBoxBaseStyle is the default style a margin box's declarations cascade over: the
// serif body font at 11pt, black, left-aligned. (Full inheritance from the page context
// — a footer in the document's font — is a refinement; the explicit defaults match a
// browser's UA margin-box rendering closely enough for running headers/footers.)
func marginBoxBaseStyle() gcss.ComputedStyle {
	return gcss.ComputedStyle{
		FontFamily: "serif",
		FontSizePt: 11,
		Color:      color.RGBA{0, 0, 0, 255},
		TextAlign:  "left",
	}
}

// alignOf maps a CSS text-align value to the inline core's Align.
func alignOf(textAlign string) inline.Align {
	switch textAlign {
	case "center":
		return inline.AlignCenter
	case "right":
		return inline.AlignRight
	case "justify":
		return inline.AlignJustify
	default:
		return inline.AlignLeft
	}
}
