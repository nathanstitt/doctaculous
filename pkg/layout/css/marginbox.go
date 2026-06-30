package css

import (
	"image/color"
	"strconv"
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
func (e *Engine) appendMarginBoxes(items []layout.Item, g pageGeom, pageIndex, pageCount int) []layout.Item {
	if !g.used.HasRule || len(g.used.MarginBoxes) == 0 {
		return items
	}
	for _, mb := range g.used.MarginBoxes {
		text := resolveMarginContent(mb.Content, pageIndex+1, pageCount)
		if text == "" {
			continue // no content (or an unsupported content value → empty, logged below)
		}
		r := marginBoxRect(mb.Slot, g)
		if r.w <= 0 || r.h <= 0 {
			continue // a degenerate band (zero margin on that side) — nothing to draw
		}
		items = e.appendMarginText(items, text, mb.Decls, r)
	}
	return items
}

// resolveMarginContent resolves an @page margin box `content` value to a string for the
// given 1-based page number and total page count. It supports a sequence of components:
// a quoted "literal", counter(page), counter(pages), and counter(page, decimal) (only
// the decimal style; other styles fall back to decimal). Components are concatenated.
// An unsupported component (e.g. string(), element(), attr()) contributes nothing — the
// running-string descriptors are a documented deferral (see the design doc). An empty or
// `normal`/`none` value yields "".
func resolveMarginContent(content string, page, pages int) string {
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
			name := strings.TrimSuffix(strings.TrimPrefix(comp, "counter("), ")")
			// counter(page) / counter(pages) [, style] — only the name's first token
			// matters; an optional style argument is ignored (decimal).
			arg := strings.TrimSpace(strings.SplitN(name, ",", 2)[0])
			switch arg {
			case "page":
				b.WriteString(strconv.Itoa(page))
			case "pages":
				b.WriteString(strconv.Itoa(pages))
			}
		default:
			// string(...) / element(...) / attr(...) / unknown: contribute nothing
			// (deferred). Bare unquoted idents are not valid content; skipped.
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
