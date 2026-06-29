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

// applyFlexShorthand expands the `flex` shorthand into flex-grow/flex-shrink/flex-basis
// (CSS Flexbox 7.2). Keyword forms: none=>0 0 auto, auto=>1 1 auto, initial=>0 1 auto.
// Numeric forms: <g> => g 1 0%; <g> <s> => g s 0%; <length> => 1 1 <length>;
// <g> <basis> => g 1 basis; <g> <s> <basis> => as written. A token is a "basis" if it
// is a length/percentage/auto/content; otherwise it is a number (grow then shrink).
func applyFlexShorthand(cs *ComputedStyle, val string) {
	v := strings.TrimSpace(val)
	switch v {
	case "none":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 0, 0, Length{Unit: UnitAuto}
		return
	case "auto":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 1, 1, Length{Unit: UnitAuto}
		return
	case "initial":
		cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = 0, 1, Length{Unit: UnitAuto}
		return
	}
	fields := splitComponents(v)
	if len(fields) == 0 || len(fields) > 3 {
		return // malformed: leave prior values
	}
	// Defaults per the spec when a component is omitted from a non-keyword flex value:
	// grow 1, shrink 1, basis 0%.
	grow, shrink := 1.0, 1.0
	basis := Length{0, UnitPercent}
	var nums []float64
	for _, f := range fields {
		if b, ok := parseFlexBasis(f); ok && isBasisToken(f) {
			basis = b
			continue
		}
		n, ok := parseNonNegNumber(f)
		if !ok {
			return // unrecognized token: leave prior values
		}
		nums = append(nums, n)
	}
	if len(nums) >= 1 {
		grow = nums[0]
	}
	if len(nums) >= 2 {
		shrink = nums[1]
	}
	cs.FlexGrow, cs.FlexShrink, cs.FlexBasis = grow, shrink, basis
}

// isBasisToken reports whether a `flex` shorthand token should be read as the basis
// (a length, percentage, or the auto/content keyword) rather than a grow/shrink number.
func isBasisToken(f string) bool {
	switch strings.TrimSpace(f) {
	case "auto", "content":
		return true
	}
	t := newTokenizer(f).next()
	return t.Kind == TokenDimension || t.Kind == TokenPercent
}

// applyGapShorthand expands `gap: <row> [<column>]` (a single value sets both).
func applyGapShorthand(cs *ComputedStyle, val string) {
	fields := splitComponents(val)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	row, ok := parseGapLength(fields[0])
	if !ok {
		return
	}
	col := row
	if len(fields) == 2 {
		c, ok := parseGapLength(fields[1])
		if !ok {
			return
		}
		col = c
	}
	cs.RowGap, cs.ColumnGap = row, col
}

// applyPlaceItems expands `place-items: <align-items> [<justify-items>]`.
// One value sets both. Two values set align-items then justify-items.
func applyPlaceItems(cs *ComputedStyle, val string) {
	fields := splitComponents(val)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyDeclaration(cs, Declaration{Property: "align-items", Value: fields[0]})
	ji := fields[0]
	if len(fields) == 2 {
		ji = fields[1]
	}
	applyDeclaration(cs, Declaration{Property: "justify-items", Value: ji})
}

// applyPlaceContent expands `place-content: <align-content> [<justify-content>]`.
// One value sets both. Two values set align-content then justify-content.
func applyPlaceContent(cs *ComputedStyle, val string) {
	fields := splitComponents(val)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyDeclaration(cs, Declaration{Property: "align-content", Value: fields[0]})
	jc := fields[0]
	if len(fields) == 2 {
		jc = fields[1]
	}
	applyDeclaration(cs, Declaration{Property: "justify-content", Value: jc})
}

// applyPlaceSelf expands `place-self: <align-self> [<justify-self>]`.
// One value sets both. Two values set align-self then justify-self.
func applyPlaceSelf(cs *ComputedStyle, val string) {
	fields := splitComponents(val)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyDeclaration(cs, Declaration{Property: "align-self", Value: fields[0]})
	js := fields[0]
	if len(fields) == 2 {
		js = fields[1]
	}
	applyDeclaration(cs, Declaration{Property: "justify-self", Value: js})
}

// applyGridTemplate expands `grid-template: <rows> / <columns>`. It locates the
// slash delimiter and parses each side as a track list. The areas-bearing form
// (`"name" 1fr / auto`) is a best-effort parse: quoted strings are consumed as
// grid-template-areas (the leading rows) and the trailing track list after the
// slash becomes grid-template-columns; the track portion before the slash becomes
// grid-template-rows. If the slash is absent or the track lists are invalid, the
// shorthand is silently dropped (graceful degradation per project policy).
func applyGridTemplate(cs *ComputedStyle, val string) {
	parts := splitSlashParts(val)
	if parts == nil || len(parts) != 2 {
		return
	}
	rowPart := strings.TrimSpace(parts[0])
	colPart := strings.TrimSpace(parts[1])
	// Parse columns (the part after the slash) — always a track list.
	if tl, ok := parseTrackList(colPart); ok {
		cs.GridTemplateColumns = tl
	}
	// Parse rows. Check for quoted-string rows (template-areas form):
	// if the row part contains a quoted string, extract track sizes interleaved
	// with the strings. Best-effort: collect just the track-list tokens (non-strings).
	if strings.ContainsRune(rowPart, '"') || strings.ContainsRune(rowPart, '\'') {
		// Areas-bearing form: parse quoted strings for grid-template-areas,
		// and the non-string portions as the row track list.
		tz := newTokenizer(rowPart)
		var areaStr strings.Builder
		var trackStr strings.Builder
		for {
			tok := tz.next()
			if tok.Kind == TokenEOF {
				break
			}
			if tok.Kind == TokenWhitespace {
				areaStr.WriteByte(' ')
				trackStr.WriteByte(' ')
				continue
			}
			if tok.Kind == TokenString {
				areaStr.WriteString(`"`)
				areaStr.WriteString(tok.Text)
				areaStr.WriteString(`" `)
			} else {
				trackStr.WriteString(tokenText(tok))
				trackStr.WriteByte(' ')
			}
		}
		if ga, ok := parseTemplateAreas(areaStr.String()); ok {
			cs.GridTemplateAreas = ga
		}
		if tl, ok := parseTrackList(trackStr.String()); ok {
			cs.GridTemplateRows = tl
		}
		return
	}
	// Non-areas form: reset grid-template-areas to none (CSS Grid §7.4).
	cs.GridTemplateAreas = GridAreas{}
	if tl, ok := parseTrackList(rowPart); ok {
		cs.GridTemplateRows = tl
	}
}

// applyGridShorthand expands the `grid` shorthand. For the explicit-grid subset
// (a track list / track list), it delegates to applyGridTemplate. For the
// `auto-flow` forms, it sets grid-auto-flow plus the auto-track dimension
// (best-effort; logged via graceful-degradation — if parsing fails, the field
// keeps its prior value). The shorthand also resets grid-template-areas,
// grid-template-rows/columns, grid-auto-rows/columns, and grid-auto-flow to
// their initial values before applying the new value, per CSS Grid §8.
func applyGridShorthand(cs *ComputedStyle, val string) {
	v := strings.TrimSpace(val)
	if v == "" {
		return
	}
	// Reset all grid properties to initial before applying (CSS shorthand reset).
	init := initialStyle()
	cs.GridTemplateColumns = init.GridTemplateColumns
	cs.GridTemplateRows = init.GridTemplateRows
	cs.GridTemplateAreas = init.GridTemplateAreas
	cs.GridAutoColumns = init.GridAutoColumns
	cs.GridAutoRows = init.GridAutoRows
	cs.GridAutoFlow = init.GridAutoFlow

	// Check for the auto-flow keyword forms:
	//   grid: auto-flow [dense] [<track-size>] / <template-columns>
	//   grid: <template-rows> / auto-flow [dense] [<track-size>]
	// Detect by presence of "auto-flow" in the value.
	lower := strings.ToLower(v)
	if strings.Contains(lower, "auto-flow") {
		parts := splitSlashParts(v)
		if parts == nil || len(parts) != 2 {
			return // malformed; already reset, leave initial
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		leftLower := strings.ToLower(left)
		rightLower := strings.ToLower(right)
		if strings.HasPrefix(leftLower, "auto-flow") {
			// auto-flow [dense] [<track-size>] / <columns>
			rest := strings.TrimSpace(left[len("auto-flow"):])
			hasDense := strings.HasPrefix(strings.ToLower(rest), "dense")
			if hasDense {
				cs.GridAutoFlow = "row dense"
				rest = strings.TrimSpace(rest[len("dense"):])
			} else {
				cs.GridAutoFlow = "row"
			}
			if rest != "" {
				if tl, ok := parseTrackList(rest); ok {
					cs.GridAutoRows = tl.Expand(0)
				}
			}
			if tl, ok := parseTrackList(right); ok {
				cs.GridTemplateColumns = tl
			}
		} else if strings.HasPrefix(rightLower, "auto-flow") {
			// <rows> / auto-flow [dense] [<track-size>]
			rest := strings.TrimSpace(right[strings.Index(rightLower, "auto-flow")+len("auto-flow"):])
			hasDense := strings.HasPrefix(strings.ToLower(rest), "dense")
			if hasDense {
				cs.GridAutoFlow = "column dense"
				rest = strings.TrimSpace(rest[len("dense"):])
			} else {
				cs.GridAutoFlow = "column"
			}
			if rest != "" {
				if tl, ok := parseTrackList(rest); ok {
					cs.GridAutoColumns = tl.Expand(0)
				}
			}
			if tl, ok := parseTrackList(left); ok {
				cs.GridTemplateRows = tl
			}
		}
		return
	}

	// Explicit-grid subset: delegate to grid-template.
	applyGridTemplate(cs, v)
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

// fontKeywordSizes are the CSS absolute-size keywords recognized in the `font`
// shorthand's size slot, mapped to a px value (a coarse scale; the exact CSS
// medium-relative ratios are not modeled). Relative sizes (larger/smaller) and the
// system-font keywords (caption/menu/…) are not supported and leave the shorthand's
// size unset.
var fontKeywordSizes = map[string]float64{
	"xx-small": 9,
	"x-small":  10,
	"small":    13,
	"medium":   16,
	"large":    18,
	"x-large":  24,
	"xx-large": 32,
}

// expandFont expands the CSS `font` shorthand into its longhand declarations, applied
// in order through applyDeclaration so normal cascade semantics hold (a later longhand
// still overrides, and vice-versa). The supported grammar is the CSS 2.1 form
//
//	[ <style> || <variant> || <weight> ]?  <size>[ / <line-height> ]?  <family>
//
// where <size> and <family> are mandatory. The size is the first component that is a
// length or an absolute-size keyword; components before it that are a weight
// (bold / 100–900) or style (italic / oblique) set font-weight / font-style (variant
// is ignored), and every component after the size is the family list. A size component
// may carry an inline line-height as size/line-height. The system-font keywords
// (caption / icon / menu / …) and the global keywords (inherit / initial) are not
// expanded — the shorthand is dropped, leaving the longhands intact. A shorthand with
// no resolvable size or no family is left unapplied (treated as invalid), matching how
// browsers reject a malformed font shorthand.
func expandFont(cs *ComputedStyle, value string) {
	comps := splitComponents(value)
	if len(comps) < 2 {
		return // need at least size + family
	}
	// Locate the size component: the first one that resolves to a length or an
	// absolute-size keyword (its left side, if it carries /line-height).
	sizeIdx := -1
	for i, c := range comps {
		sizePart, _, _ := strings.Cut(c, "/")
		if _, ok := fontSizeValue(sizePart); ok {
			sizeIdx = i
			break
		}
	}
	// A valid font shorthand needs a size and at least one family component after it.
	if sizeIdx < 0 || sizeIdx == len(comps)-1 {
		return
	}

	// Components before the size: weight / style (any order; variant ignored).
	for _, c := range comps[:sizeIdx] {
		switch {
		case isFontWeightToken(c):
			applyDeclaration(cs, Declaration{Property: "font-weight", Value: c})
		case c == "italic" || c == "oblique":
			applyDeclaration(cs, Declaration{Property: "font-style", Value: c})
		}
		// "normal", "small-caps", and other variant/normal tokens carry no longhand here.
	}

	// The size (and optional inline line-height). Resolve the size HERE (it may be an
	// absolute-size keyword the font-size longhand does not accept) and set it directly,
	// px:pt 1:1 like the longhand.
	sizePart, lhPart, hasLH := strings.Cut(comps[sizeIdx], "/")
	if v, ok := fontSizeValue(sizePart); ok {
		cs.FontSizePt = v
	}
	if hasLH && lhPart != "" {
		applyDeclaration(cs, Declaration{Property: "line-height", Value: lhPart})
	}

	// Everything after the size is the family list (re-join with spaces; the family
	// longhand reads the first family).
	family := strings.Join(comps[sizeIdx+1:], " ")
	applyDeclaration(cs, Declaration{Property: "font-family", Value: family})
}

// fontSizeValue resolves the `font` shorthand's size slot to a px value: a length
// (px:pt 1:1, matching font-size's longhand) or an absolute-size keyword. Returns
// ok=false for a relative size, a system keyword, or an unparseable token (so the
// caller does not treat it as the size component).
func fontSizeValue(s string) (float64, bool) {
	if l, ok := parseLength(newTokenizer(s).next()); ok && l.Unit != UnitAuto {
		return l.Value, true
	}
	if v, ok := fontKeywordSizes[s]; ok {
		return v, true
	}
	return 0, false
}

// isFontWeightToken reports whether a `font` shorthand component is a font-weight
// value (the keyword bold / bolder / lighter / normal or a numeric 100–900). normal
// and the relative keywords carry no Bold longhand effect (only bold / 700–900 do, per
// applyDeclaration), but they are still classified here so they are not mistaken for a
// family token.
func isFontWeightToken(s string) bool {
	switch s {
	case "bold", "bolder", "lighter", "normal":
		return true
	case "100", "200", "300", "400", "500", "600", "700", "800", "900":
		return true
	}
	return false
}
