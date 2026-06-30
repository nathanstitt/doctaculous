package css

import (
	"strings"
)

// PagePseudo is the page-selector pseudo-class on an @page rule (CSS Paged Media
// §3.1). It narrows which generated pages the rule matches.
type PagePseudo int

const (
	// PageNone is an @page rule with no pseudo (matches every page — the base).
	PageNone PagePseudo = iota
	// PageFirst is @page :first (matches only the first page).
	PageFirst
	// PageLeft is @page :left (matches left/verso pages — even page numbers).
	PageLeft
	// PageRight is @page :right (matches right/recto pages — odd page numbers; page 1
	// is :right per the CSS default for a left-to-right document).
	PageRight
	// PageBlank is @page :blank (matches a page with no content — reachable only via a
	// forced break that produces an empty page).
	PageBlank
)

// MarginBoxSlot identifies one of the 16 @page margin boxes (CSS Paged Media §8).
// The slots ring the page content area: four corners, three per edge.
type MarginBoxSlot int

const (
	MarginTopLeftCorner MarginBoxSlot = iota
	MarginTopLeft
	MarginTopCenter
	MarginTopRight
	MarginTopRightCorner
	MarginRightTop
	MarginRightMiddle
	MarginRightBottom
	MarginBottomRightCorner
	MarginBottomRight
	MarginBottomCenter
	MarginBottomLeft
	MarginBottomLeftCorner
	MarginLeftBottom
	MarginLeftMiddle
	MarginLeftTop
	marginBoxCount // sentinel: number of slots
)

// marginBoxNames maps an @-rule name (without the leading @) to its slot. Names are
// matched case-insensitively (CSS at-rule names are ASCII case-insensitive).
var marginBoxNames = map[string]MarginBoxSlot{
	"top-left-corner":     MarginTopLeftCorner,
	"top-left":            MarginTopLeft,
	"top-center":          MarginTopCenter,
	"top-right":           MarginTopRight,
	"top-right-corner":    MarginTopRightCorner,
	"right-top":           MarginRightTop,
	"right-middle":        MarginRightMiddle,
	"right-bottom":        MarginRightBottom,
	"bottom-right-corner": MarginBottomRightCorner,
	"bottom-right":        MarginBottomRight,
	"bottom-center":       MarginBottomCenter,
	"bottom-left":         MarginBottomLeft,
	"bottom-left-corner":  MarginBottomLeftCorner,
	"left-bottom":         MarginLeftBottom,
	"left-middle":         MarginLeftMiddle,
	"left-top":            MarginLeftTop,
}

// PageRule is one captured @page rule: its selector (optional name + pseudo), the
// page-box declarations (size / margin / any normal property on the page box), and
// the nested margin-box rules. The cascade does not match @page against elements;
// ResolvePage combines the matching rules for a given page into a UsedPage. Order is
// the rule's document position, a cascade tie-breaker (mirroring FontFace.Order's
// role within a family).
type PageRule struct {
	Name   string          // "" for an un-named @page
	Pseudo PagePseudo      // PageNone / PageFirst / PageLeft / PageRight / PageBlank
	Decls  []Declaration   // size, margin*, and any normal property on the page box
	Margin []MarginBoxRule // @top-left … @bottom-right content rules
	Order  int             // document order
}

// MarginBoxRule is one @page margin box (@top-center, @bottom-right, …) with its
// declarations (content, plus text styling: font / color / text-align / …).
type MarginBoxRule struct {
	Box   MarginBoxSlot
	Decls []Declaration
}

// atKeyword reports whether prelude begins with the at-keyword kw (case-insensitive,
// e.g. "@page"), and if so returns the remaining prelude text after it (trimmed of the
// single separating run of whitespace). The match must be the whole keyword followed
// by a word boundary (whitespace, ':', or end) so "@page" does not match "@pages".
func atKeyword(prelude, kw string) (rest string, ok bool) {
	if len(prelude) < len(kw) || !strings.EqualFold(prelude[:len(kw)], kw) {
		return "", false
	}
	rest = prelude[len(kw):]
	if rest == "" {
		return "", true
	}
	switch rest[0] {
	case ' ', '\t', '\n', '\r', '\f', ':':
		return strings.TrimSpace(rest), true
	}
	return "", false // a longer keyword (e.g. "@page-break"): not a match
}

// parsePageRule parses an @page prelude + body into a PageRule. The prelude is the
// text after "@page" (a name and/or a :pseudo); the body holds top-level declarations
// interleaved with nested margin-box @-blocks. ok is false only for a body that yields
// neither declarations nor margin boxes AND an unparseable selector — a bare
// "@page {}" still captures (an empty base rule is harmless).
func parsePageRule(prelude, body string, order int) (PageRule, bool) {
	pr := PageRule{Order: order}
	pr.Name, pr.Pseudo = parsePageSelector(prelude)
	pr.Decls, pr.Margin = parsePageBody(body)
	return pr, true
}

// parsePageSelector parses an @page selector (the prelude after "@page") into a name
// and pseudo. Forms: "" (base) · ":first"/":left"/":right"/":blank" · "name" ·
// "name:first". An unrecognized pseudo is ignored (PageNone). The name is lowercased
// to match the `page:` property (also lowercased by the cascade).
func parsePageSelector(prelude string) (name string, pseudo PagePseudo) {
	s := strings.TrimSpace(prelude)
	if s == "" {
		return "", PageNone
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		name = strings.TrimSpace(s[:i])
		switch strings.ToLower(strings.TrimSpace(s[i+1:])) {
		case "first":
			pseudo = PageFirst
		case "left":
			pseudo = PageLeft
		case "right":
			pseudo = PageRight
		case "blank":
			pseudo = PageBlank
		}
	} else {
		name = s
	}
	return strings.ToLower(name), pseudo
}

// parsePageBody splits an @page rule body into its top-level declarations and its
// nested margin-box rules. It re-scans the body with the shared ruleScanner so a
// nested "@top-center { … }" block is recognized (and its braces not mistaken for a
// declaration) — parseDeclarations alone splits on ';' and cannot see nested blocks.
// Text spans between nested blocks are parsed as declarations.
func parsePageBody(body string) (decls []Declaration, boxes []MarginBoxRule) {
	s := &ruleScanner{src: body}
	for {
		// nextRule returns (prelude, innerBody, true) for each top-level { } block —
		// prelude being the text preceding it — and ("", "", false) once no further
		// block exists, having scanned to EOF (the trailing text is NOT returned by
		// nextRule's failure path). So before each iteration record where the scanner
		// starts; on the no-more-blocks return, the body from that mark to EOF is the
		// trailing declaration text.
		mark := s.pos
		prelude, inner, ok := s.nextRule()
		if !ok {
			// Trailing text after the last block (or the whole body when there is no
			// block at all): its declarations belong to the page box.
			decls = append(decls, parseDeclarations(body[mark:])...)
			break
		}
		// prelude holds: page-box declarations (terminated by ';') followed by an
		// at-keyword introducing this block. Split at the last '@'.
		at := strings.LastIndexByte(prelude, '@')
		if at < 0 {
			// No at-keyword before a '{' — a stray nested block (e.g. an unknown
			// at-rule). Its declarations (if the prelude text is declarations) still
			// count; the block body is dropped.
			decls = append(decls, parseDeclarations(prelude)...)
			continue
		}
		decls = append(decls, parseDeclarations(prelude[:at])...)
		name := strings.ToLower(strings.TrimSpace(prelude[at+1:]))
		if slot, isBox := marginBoxNames[name]; isBox {
			boxes = append(boxes, MarginBoxRule{Box: slot, Decls: parseDeclarations(inner)})
		}
		// An unrecognized nested @-name inside @page: body already consumed, dropped.
	}
	return decls, boxes
}

// UsedMarginBox is a resolved @page margin box: the raw content value (resolved to a
// per-page string later, since counter(pages) needs the final page count) plus the
// text styling read off its declarations.
type UsedMarginBox struct {
	Slot    MarginBoxSlot
	Content string        // the raw `content` value (e.g. `counter(page) " / " counter(pages)`)
	Decls   []Declaration // all declarations (font/color/text-align read at layout)
}

// UsedPage is the resolved geometry + chrome for one page: its size and margins (in
// the engine's px-as-pt scalar) and the margin boxes that carry content. HasRule is
// false when no @page rule matched (the caller falls back to its API/Letter default).
type UsedPage struct {
	WidthPt, HeightPt                                float64
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
	MarginBoxes                                      []UsedMarginBox
	HasSize                                          bool    // an explicit `size` was resolved
	HasRule                                          bool    // at least one @page rule matched
	Marks                                            string  // CSS `marks`: "crop", "cross", or "crop cross" (lowercased); "" = none
	Bleed                                            float64 // CSS `bleed` in the px-as-pt scalar (the trim→media-box inset on each side)
}

// ResolvePage returns the UsedPage for the zero-based page index i (page 1 == i 0)
// with optional named page `name` (from a box's `page:` property). It applies the CSS
// Paged Media cascade over every PageRule whose selector matches this page:
//
//   - an un-named, un-pseudo @page matches every page (the base);
//   - @page :first matches only i == 0;
//   - @page :left / :right match by page-number parity (page number = i+1; :right is
//     odd, :left is even — page 1 is :right);
//   - @page :blank matches when blank is true (an empty forced-break page);
//   - @page name matches when name != "" and equals the rule's name.
//
// Matching rules cascade by specificity (a pseudo and/or name beats the bare base;
// among equal specificity, later source order wins), then size + margins resolve with
// Paged Media defaults. A named rule's pseudo must also match (a "@page name:first"
// applies only to a named first page). Returns the zero UsedPage (HasRule false) when
// nothing matches.
func (s Stylesheet) ResolvePage(i int, name string, blank bool) UsedPage {
	matched := s.matchingPageRules(i, name, blank)
	if len(matched) == 0 {
		return UsedPage{}
	}
	up := UsedPage{HasRule: true}
	// Cascade: apply in ascending specificity then source order so the strongest wins
	// last. Declarations are merged by property (last write wins); margin boxes merge
	// by slot.
	bySlot := map[MarginBoxSlot]UsedMarginBox{}
	for _, pr := range matched {
		applyPageDecls(&up, pr.Decls)
		for _, mb := range pr.Margin {
			ub := bySlot[mb.Box]
			ub.Slot = mb.Box
			ub.Decls = append(ub.Decls, mb.Decls...)
			if c, ok := lastDecl(mb.Decls, "content"); ok {
				ub.Content = c
			}
			bySlot[mb.Box] = ub
		}
	}
	for _, mb := range bySlot {
		up.MarginBoxes = append(up.MarginBoxes, mb)
	}
	return up
}

// matchingPageRules returns the @page rules matching page index i (named `name`,
// blank or not), sorted by ascending cascade strength: base rules first, then
// pseudo/named rules, ties broken by source order. So applying them in slice order
// lets the strongest win last.
func (s Stylesheet) matchingPageRules(i int, name string, blank bool) []PageRule {
	var out []PageRule
	for _, pr := range s.Pages {
		if pr.Name != "" && !strings.EqualFold(pr.Name, name) {
			continue
		}
		switch pr.Pseudo {
		case PageNone:
		case PageFirst:
			if i != 0 {
				continue
			}
		case PageLeft:
			if (i+1)%2 == 0 { // even page number is left
			} else {
				continue
			}
		case PageRight:
			if (i+1)%2 == 1 { // odd page number is right
			} else {
				continue
			}
		case PageBlank:
			if !blank {
				continue
			}
		}
		out = append(out, pr)
	}
	// Stable sort by specificity (pseudo and/or name) then order. A simple key:
	// named+pseudo = 3, pseudo only = 2, named only = 1, base = 0.
	spec := func(pr PageRule) int {
		s := 0
		if pr.Name != "" {
			s += 1
		}
		if pr.Pseudo != PageNone {
			s += 2
		}
		return s
	}
	// insertion sort (stable, tiny n)
	for a := 1; a < len(out); a++ {
		for b := a; b > 0; b-- {
			pa, pb := out[b-1], out[b]
			if spec(pa) > spec(pb) || (spec(pa) == spec(pb) && pa.Order > pb.Order) {
				out[b-1], out[b] = out[b], out[b-1]
			} else {
				break
			}
		}
	}
	return out
}

// applyPageDecls applies a rule's page-box declarations onto up: `size` (keyword/
// length + orientation), the `margin` shorthand and longhands. Unknown properties are
// ignored here (a background on the page box is a follow-up). Later calls override
// earlier ones (the caller orders rules weak→strong).
func applyPageDecls(up *UsedPage, decls []Declaration) {
	for _, d := range decls {
		switch d.Property {
		case "size":
			if w, h, ok := parsePageSize(d.Value); ok {
				up.WidthPt, up.HeightPt, up.HasSize = w, h, true
			}
		case "margin":
			if t, r, b, l, ok := parsePageMarginShorthand(d.Value); ok {
				up.MarginTop, up.MarginRight, up.MarginBottom, up.MarginLeft = t, r, b, l
			}
		case "margin-top":
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.MarginTop = v
			}
		case "margin-right":
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.MarginRight = v
			}
		case "margin-bottom":
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.MarginBottom = v
			}
		case "margin-left":
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.MarginLeft = v
			}
		case "marks":
			up.Marks = strings.ToLower(strings.TrimSpace(d.Value))
		case "bleed":
			// `bleed: auto` (the initial) means 6pt when marks are present; an explicit
			// length sets the bleed directly. We store an explicit length; auto stays 0
			// (the paginator synthesizes a default inset when marks are set with no bleed).
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.Bleed = v
			}
		}
	}
}

// ComputeMarginBox resolves an @page margin box's declarations into a ComputedStyle,
// applying them over the CSS initial style (then over a provided base for inheritance —
// font/color of the page context). It is the styling source for a running header/footer:
// the layout engine reads FontFamily / FontSizePt / Color / TextAlign from the result.
// Properties the margin box does not set keep base's value (so a footer inherits the
// document font/color when base carries them).
func (s Stylesheet) ComputeMarginBox(decls []Declaration, base ComputedStyle) ComputedStyle {
	cs := base
	for _, d := range decls {
		applyDeclaration(&cs, d)
	}
	return cs
}

// lastDecl returns the value of the last declaration with the given property (the one
// the cascade keeps), and whether any was present.
func lastDecl(decls []Declaration, prop string) (string, bool) {
	val, ok := "", false
	for _, d := range decls {
		if d.Property == prop {
			val, ok = d.Value, true
		}
	}
	return val, ok
}
