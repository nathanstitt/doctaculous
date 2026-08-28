package css

import "strings"

// This file implements the CSS Backgrounds 3 §5 `border-radius` shorthand and its
// four corner longhands. A corner's radius is ELLIPTICAL — two independent
// components, horizontal and vertical — so every value here is a pair, not a
// scalar. That is why border-radius cannot reuse applyBoxLengths (which expands one
// scalar per side): the shorthand's `/` form supplies the four horizontal radii
// before the slash and the four vertical radii after it, so a single declaration
// carries EIGHT lengths in two independently-expanded 1–4 value groups.
//
// Percentages are stored unresolved (UnitPercent) and resolved by the layout
// engine, because the two components of one corner resolve against DIFFERENT
// dimensions: a horizontal radius resolves against the border box's WIDTH, a
// vertical one against its HEIGHT (Backgrounds 3 §5.1). Collapsing them to one
// number here would lose the distinction and silently make every percentage radius
// on a non-square box wrong.

// CornerRadius is one corner's elliptical radius: the horizontal semi-axis (H) and
// the vertical semi-axis (V), each an unresolved Length so a percentage can be
// resolved against the right dimension at layout time (H against the border box's
// width, V against its height). A zero pair is a square corner.
type CornerRadius struct {
	H, V Length
}

// Zero reports whether the corner has no rounding at all. A corner is square when
// EITHER component is zero: an ellipse with a zero semi-axis is degenerate, and
// CSS Backgrounds 3 §5.1 says "if either length is zero, the corner is square".
func (c CornerRadius) Zero() bool {
	return c.H.Value == 0 || c.V.Value == 0
}

// parseRadiusComponent parses one border-radius component: a non-negative length or
// percentage. Unlike a margin, `auto` is not a valid radius, and a NEGATIVE radius
// is invalid rather than clamped — CSS Backgrounds 3 §5.1 makes a negative value a
// parse error, which (per the whole-declaration-drop rule the box shorthands
// already follow) voids the entire declaration and leaves the prior radii intact.
func parseRadiusComponent(s string) (Length, bool) {
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto || l.Unit == UnitContent || l.Value < 0 {
		return Length{}, false
	}
	return l, true
}

// splitRadiusGroups splits a border-radius value at its top-level `/` into the
// horizontal group and the vertical group, reporting ok=false for more than one
// slash (a parse error).
//
// It cannot simply scan the raw string for '/': that would also split inside a
// function such as a future `calc(10px/2)`. Splitting on the TOKEN stream instead
// keeps the slash detection paren-aware for free. It also handles `10px/20px`
// written without surrounding spaces, which splitComponents alone would glue into
// one component — the tokenizer emits the slash as its own TokenDelim regardless of
// adjacent whitespace, so the two forms parse identically.
func splitRadiusGroups(value string) (horiz, vert []string, ok bool) {
	tz := newTokenizer(value)
	groups := [][]string{{}}
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			groups[len(groups)-1] = append(groups[len(groups)-1], cur.String())
			cur.Reset()
		}
	}
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			break
		}
		switch {
		case tok.Kind == TokenWhitespace:
			if depth == 0 {
				flush()
			} else {
				cur.WriteByte(' ')
			}
		case depth == 0 && tok.Kind == TokenDelim && tok.Text == "/":
			flush()
			if len(groups) == 2 {
				return nil, nil, false // a second slash is a parse error
			}
			groups = append(groups, []string{})
		case tok.Kind == TokenLParen:
			depth++
			cur.WriteString(tokenText(tok))
		case tok.Kind == TokenRParen:
			if depth > 0 {
				depth--
			}
			cur.WriteString(tokenText(tok))
		default:
			cur.WriteString(tokenText(tok))
		}
	}
	flush()

	horiz = groups[0]
	if len(groups) == 2 {
		vert = groups[1]
	} else {
		// No slash: the horizontal group supplies BOTH components, making every
		// corner circular (Backgrounds 3 §5: the vertical radii default to the
		// horizontal ones).
		vert = horiz
	}
	if len(horiz) == 0 || len(vert) == 0 {
		return nil, nil, false // an empty group ("/ 5px", "5px /") is a parse error
	}
	return horiz, vert, true
}

// applyBorderRadius expands the `border-radius` shorthand into the four corner
// longhands. Each side of the `/` is independently expanded by the 1–4 value rule,
// but in CORNER order (top-left, top-right, bottom-right, bottom-left) rather than
// the side order expandBox implements — the two orders coincide in arity only, so
// reusing expandBox here would silently transpose the 3-value case.
//
// An invalid component anywhere voids the whole declaration, matching the
// box-shorthand policy documented at the top of shorthand.go.
func applyBorderRadius(cs *ComputedStyle, value string) {
	horiz, vert, ok := splitRadiusGroups(value)
	if !ok {
		return
	}
	hTL, hTR, hBR, hBL, ok1 := expandCorners(horiz)
	vTL, vTR, vBR, vBL, ok2 := expandCorners(vert)
	if !ok1 || !ok2 {
		return
	}
	pairs := [4][2]string{{hTL, vTL}, {hTR, vTR}, {hBR, vBR}, {hBL, vBL}}
	var out [4]CornerRadius
	for i, p := range pairs {
		h, okH := parseRadiusComponent(p[0])
		v, okV := parseRadiusComponent(p[1])
		if !okH || !okV {
			return // whole-declaration drop
		}
		out[i] = CornerRadius{H: h, V: v}
	}
	cs.BorderTopLeftRadius = out[0]
	cs.BorderTopRightRadius = out[1]
	cs.BorderBottomRightRadius = out[2]
	cs.BorderBottomLeftRadius = out[3]
}

// expandCorners applies the 1–4 value expansion in CORNER order per CSS
// Backgrounds 3 §5:
//
//	1 value  -> all four corners
//	2 values -> top-left/bottom-right = a, top-right/bottom-left = b
//	3 values -> top-left = a, top-right/bottom-left = b, bottom-right = c
//	4 values -> top-left, top-right, bottom-right, bottom-left
//
// Note the 2- and 3-value cases pair the DIAGONALLY OPPOSITE corners, which is what
// distinguishes this from expandBox's clockwise side rule.
func expandCorners(comps []string) (tl, tr, br, bl string, ok bool) {
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

// applyCornerRadius parses ONE corner longhand (border-top-left-radius and
// friends): one or two components, where a single component makes the corner
// circular (the vertical radius defaults to the horizontal one). Unlike the
// shorthand this longhand has no `/` — the two components are simply space
// separated — so a slash here is a parse error that voids the declaration.
func applyCornerRadius(dst *CornerRadius, value string) {
	comps := splitComponents(value)
	if len(comps) == 0 || len(comps) > 2 || strings.Contains(value, "/") {
		return
	}
	h, ok := parseRadiusComponent(comps[0])
	if !ok {
		return
	}
	v := h
	if len(comps) == 2 {
		if v, ok = parseRadiusComponent(comps[1]); !ok {
			return
		}
	}
	*dst = CornerRadius{H: h, V: v}
}
