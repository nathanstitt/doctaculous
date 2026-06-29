package css

import (
	"image/color"
	"strings"
)

// This file implements expansion of the common CSS shorthand properties into the
// longhand ComputedStyle fields. Real pages author boxes almost exclusively with
// shorthands (margin/padding/border/background), so without this the engine would
// see almost no box styling. Each shorthand is expanded IMMEDIATELY into longhand
// fields inside applyDeclaration, so normal cascade source-order semantics hold: a
// later longhand overrides an earlier shorthand and vice-versa, for free.
//
// Degradation policy (CSS-correct, "drop the whole declaration"): for the
// box-list shorthands (margin/padding/border-width/-style/-color) a single
// invalid component voids the entire declaration, leaving the prior longhand
// values intact — this matches how browsers treat an invalid shorthand. For the
// border / border-<side> triple, an unrecognized component is skipped while the
// recognized ones still apply (the triple has no fixed arity, so per-component
// tolerance is the pragmatic reading); see applyBorderSide.
//
// Not implemented here (future work): the `font` shorthand (size/family/weight/
// style/line-height in one value) and the full `background` shorthand
// (image/position/repeat/size) — `background` currently sets only the color.

// borderStyleKeywords is the set of CSS border-style idents recognized by the
// border / border-<side> shorthands when classifying an unlabelled component.
// Values are stored as-is on the Border*Style fields, mirroring the per-side
// longhand cases.
var borderStyleKeywords = map[string]bool{
	"none":   true,
	"hidden": true,
	"dotted": true,
	"dashed": true,
	"solid":  true,
	"double": true,
	"groove": true,
	"ridge":  true,
	"inset":  true,
	"outset": true,
}

// borderMediumWidth is the CSS initial ("medium") border width, used when the
// border / border-<side> shorthand omits the width component. CSS defines medium
// as roughly 3px; we model it as a 3px length.
var borderMediumWidth = Length{Value: 3, Unit: UnitPx}

// splitComponents tokenizes a shorthand value into its top-level, space-separated
// components, treating a parenthesized group such as rgb(1, 2, 3) (including its
// internal commas and spaces) as part of the single component it belongs to.
// Whitespace tokens at the top level are component separators. The returned
// strings are each re-tokenizable with newTokenizer (e.g. by parseLength /
// parseColor). It never panics; an unterminated "(" simply runs to end of input.
func splitComponents(value string) []string {
	tz := newTokenizer(value)
	var out []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			break
		}
		switch tok.Kind {
		case TokenWhitespace:
			if depth == 0 {
				flush() // a top-level space ends the current component
			} else {
				cur.WriteByte(' ') // preserve a single space inside a (...) group
			}
		case TokenLParen:
			depth++
			cur.WriteString(tokenText(tok))
		case TokenRParen:
			if depth > 0 {
				depth--
			}
			cur.WriteString(tokenText(tok))
		default:
			cur.WriteString(tokenText(tok))
		}
	}
	flush()
	return out
}

// tokenText returns the source text a token should contribute back to a rebuilt
// component string. Most tokens carry their literal text in Text; the punctuation
// tokens whose Text the tokenizer leaves as the literal char are passed through
// unchanged. Hash tokens lose their leading '#' in Text, so it is restored here so
// the rebuilt component re-tokenizes to the same hash (needed by parseColor).
func tokenText(tok Token) string {
	if tok.Kind == TokenHash {
		return "#" + tok.Text
	}
	return tok.Text
}

// expandBox applies the CSS 1–4 value box shorthand expansion to a list of
// components, returning the (top, right, bottom, left) assignment. The mapping is
// the standard clockwise rule:
//
//	1 value  -> all four sides
//	2 values -> top/bottom = a, right/left = b
//	3 values -> top = a, right/left = b, bottom = c
//	4 values -> top, right, bottom, left
//
// ok is false for an empty or over-length (5+) component list.
func expandBox(comps []string) (top, right, bottom, left string, ok bool) {
	switch len(comps) {
	case 1:
		return comps[0], comps[0], comps[0], comps[0], true
	case 2:
		return comps[0], comps[1], comps[0], comps[1], true
	case 3:
		return comps[0], comps[1], comps[2], comps[1], true
	case 4:
		return comps[0], comps[1], comps[2], comps[3], true
	}
	return "", "", "", "", false
}

// applyBoxLengths expands a 1–4 length box shorthand (margin/padding/
// border-width) and writes the four sides via parse. parse reports per-component
// validity; if ANY component is invalid the whole declaration is dropped and the
// destinations are left unchanged (CSS whole-declaration-drop). The four dst
// pointers are in top/right/bottom/left order.
func applyBoxLengths(value string, parse func(string) (Length, bool), top, right, bottom, left *Length) {
	t, r, b, l, ok := expandBox(splitComponents(value))
	if !ok {
		return
	}
	tv, ok1 := parse(t)
	rv, ok2 := parse(r)
	bv, ok3 := parse(b)
	lv, ok4 := parse(l)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return // an invalid component voids the entire shorthand
	}
	*top, *right, *bottom, *left = tv, rv, bv, lv
}

// parseMarginComponent parses one margin component: a length, percentage, or the
// "auto" keyword (all handled by parseLength).
func parseMarginComponent(s string) (Length, bool) {
	return parseLength(newTokenizer(s).next())
}

// parsePaddingComponent parses one padding component. Padding accepts lengths and
// percentages but NOT "auto" (and negative values are invalid per CSS); the
// UnitAuto guard drops "auto". Negative lengths are passed through and clamped by
// the layout engine, matching the per-side longhand behavior.
func parsePaddingComponent(s string) (Length, bool) {
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}

// parseBorderWidthComponent parses one border-width component: a length (the
// named widths thin/medium/thick are not modeled). "auto" is not a valid border
// width, so the UnitAuto guard drops it.
func parseBorderWidthComponent(s string) (Length, bool) {
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}

// applyBorderStyle expands a 1–4 ident border-style shorthand onto the four side
// style fields. Each component must be a recognized border-style keyword; an
// unrecognized one voids the whole declaration (CSS whole-declaration-drop).
func applyBorderStyle(cs *ComputedStyle, value string) {
	t, r, b, l, ok := expandBox(splitComponents(value))
	if !ok {
		return
	}
	for _, v := range [4]string{t, r, b, l} {
		if !borderStyleKeywords[strings.ToLower(v)] {
			return
		}
	}
	cs.BorderTopStyle, cs.BorderRightStyle, cs.BorderBottomStyle, cs.BorderLeftStyle = t, r, b, l
}

// applyBorderColor expands a 1–4 color border-color shorthand onto the four side
// color fields. Each component must parse as a color; an unparseable one voids the
// whole declaration (CSS whole-declaration-drop).
func applyBorderColor(cs *ComputedStyle, value string) {
	t, r, b, l, ok := expandBox(splitComponents(value))
	if !ok {
		return
	}
	tc, ok1 := parseColor(newTokenizer(t))
	rc, ok2 := parseColor(newTokenizer(r))
	bc, ok3 := parseColor(newTokenizer(b))
	lc, ok4 := parseColor(newTokenizer(l))
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}
	cs.BorderTopColor, cs.BorderRightColor, cs.BorderBottomColor, cs.BorderLeftColor = tc, rc, bc, lc
}

// borderSide selects one set of width/style/color longhand pointers for a side.
type borderSide struct {
	width *Length
	color *color.RGBA
	style *string
}

// applyBorderSide parses a `border` / `border-<side>` value — a width || style ||
// color triple whose components may appear in any order and are each optional —
// and writes the result to the given sides (one side for border-<side>, all four
// for border). A component is classified as: a length => width; a recognized
// border-style keyword => style; otherwise an attempted color. Per CSS the
// shorthand RESETS omitted longhands to their initial values, so this sets ALL
// THREE longhands on each target side: an omitted width becomes medium (3px), an
// omitted style becomes "none", and an omitted color becomes the element's current
// color (cs.Color, the CSS currentColor). Unrecognized components are skipped
// (the recognized parts still apply); a value with no recognized component leaves
// every target side reset to the initial triple.
func applyBorderSide(cs *ComputedStyle, value string, sides ...borderSide) {
	width := borderMediumWidth
	style := "none"
	col := cs.Color // CSS currentColor
	for _, comp := range splitComponents(value) {
		if l, ok := parseBorderWidthComponent(comp); ok {
			width = l
			continue
		}
		if borderStyleKeywords[strings.ToLower(comp)] {
			style = comp
			continue
		}
		if c, ok := parseColor(newTokenizer(comp)); ok {
			col = c
			continue
		}
		// Unrecognized component: skip it, keep the recognized pieces.
	}
	for _, s := range sides {
		*s.width = width
		*s.style = style
		*s.color = col
	}
}

// applyBackground expands the (currently color-only) `background` shorthand. The
// full background shorthand (image/position/repeat/size/attachment) is future
// work; ComputedStyle only carries BackgroundColor today. Behavior: scan the
// components and set BackgroundColor to the first one that parses as a color;
// non-color components (url(), gradients, position keywords) are ignored. If no
// component is a color the declaration is dropped, leaving BackgroundColor intact.
func applyBackground(cs *ComputedStyle, value string) {
	for _, comp := range splitComponents(value) {
		if c, ok := parseColor(newTokenizer(comp)); ok {
			cs.BackgroundColor = c
			return
		}
	}
}
