package css

import (
	"image/color"
	"sort"
	"strings"
)

// inlineImportantIDs is the synthetic specificity IDs value given to inline
// !important declarations. CSS places inline !important above all author
// !important rules regardless of selector specificity; we model that with an IDs
// count (2^20) far larger than any specificity reachable from parsed CSS (which
// would need 2^20 id qualifiers — impossible in practice).
const inlineImportantIDs = 1 << 20

// Origin is a cascade origin. CSS orders declarations by origin first:
// UA-normal < author-normal < author-important < UA-important. Origin is the
// outermost cascade key, dominating specificity and source order.
type Origin int

const (
	// OriginUA is the user-agent default stylesheet.
	OriginUA Origin = iota
	// OriginAuthor is page-supplied CSS: <style>, <link>, and style="".
	OriginAuthor
)

// OriginSheet pairs a parsed stylesheet with its cascade origin.
type OriginSheet struct {
	Sheet  Stylesheet
	Origin Origin
}

// ComputedStyle is the resolved style of one element: the normal-flow property
// subset this sub-project supports, with every value concrete. Lengths remain in
// their CSS unit here (px/pt/em/%); the layout engine resolves em/% to absolute
// points against a containing context. Raw, unrecognized declarations are not on
// this struct — they are retained on the Rule for later sub-projects.
//
// Inherited properties (CSS) are Color, FontFamily, FontSizePt, Bold, Italic,
// LineHeight, and TextAlign; inheritFrom must be kept in sync with this set.
type ComputedStyle struct {
	Display string // "block" | "inline" | "none" | "list-item" | raw value

	Color           color.RGBA
	BackgroundColor color.RGBA // zero-alpha means transparent / not set

	FontFamily string
	FontSizePt float64 // resolved to an absolute size (px treated 1:1 as pt for now)
	Bold       bool
	Italic     bool
	LineHeight Length // UnitAuto = "normal"

	TextAlign string // "left" | "right" | "center" | "justify"

	MarginTop, MarginRight, MarginBottom, MarginLeft     Length
	PaddingTop, PaddingRight, PaddingBottom, PaddingLeft Length

	BorderTopWidth, BorderRightWidth, BorderBottomWidth, BorderLeftWidth Length
	BorderTopColor, BorderRightColor, BorderBottomColor, BorderLeftColor color.RGBA
	BorderTopStyle, BorderRightStyle, BorderBottomStyle, BorderLeftStyle string

	Width, Height Length // UnitAuto = "auto"

	MinWidth, MaxWidth   Length // MinWidth: UnitPx zero = no min; MaxWidth: UnitAuto = "none" (no max)
	MinHeight, MaxHeight Length // same convention as the width pair
	BoxSizing            string // "content-box" (default) | "border-box"
}

// Resolver computes the ComputedStyle of any node against parsed stylesheets
// tagged by origin. Build one with NewResolver; it is read-only after
// construction and safe for concurrent use. logf may be nil.
type Resolver struct {
	sheets []OriginSheet
	logf   func(string, ...any)
}

// NewResolver builds a Resolver over origin-tagged stylesheets. Sheets may be
// given in any order; the cascade applies origin/specificity/source-order rules.
func NewResolver(sheets []OriginSheet, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Resolver{sheets: sheets, logf: logf}
}

// ComputeRoot returns the ComputedStyle of a root element (one with no parent),
// using the CSS initial values as the inheritance base. Box generation calls
// this for the document root, then threads each result down to children via
// Compute, so callers never need the CSS initial values themselves.
func (r *Resolver) ComputeRoot(n Node) ComputedStyle {
	return r.Compute(n, initialStyle())
}

// Compute returns node n's ComputedStyle. parentStyle is the already-computed
// style of n's parent; for a root element (no parent) call ComputeRoot, which
// supplies the CSS initial values as the base. The cascade orders matching
// declarations by origin first (UA-normal < author-normal < author-important <
// UA-important), then specificity, then source order, starting from the
// inheritance base; !important declarations are applied last.
func (r *Resolver) Compute(n Node, parentStyle ComputedStyle) ComputedStyle {
	cs := inheritFrom(parentStyle)

	type matched struct {
		decl   Declaration
		origin Origin
		spec   Specificity
		order  int
	}
	var normal, important []matched

	order := 0
	for si := range r.sheets {
		origin := r.sheets[si].Origin
		sheet := &r.sheets[si].Sheet
		for ri := range sheet.Rules {
			rule := &sheet.Rules[ri]
			spec, ok := bestMatch(rule.Selectors, n)
			if !ok {
				continue
			}
			for _, d := range rule.Declarations {
				m := matched{decl: d, origin: origin, spec: spec, order: order}
				if d.Important {
					important = append(important, m)
				} else {
					normal = append(normal, m)
				}
				order++
			}
		}
	}

	// normalRank/importantRank place each origin on the unified cascade ladder so
	// the same comparison works for both passes:
	//   UA-normal(0) < author-normal(1) < author-important(2) < UA-important(3)
	normalRank := func(o Origin) int {
		if o == OriginAuthor {
			return 1
		}
		return 0 // UA
	}
	importantRank := func(o Origin) int {
		if o == OriginUA {
			return 3
		}
		return 2 // author
	}

	lessBy := func(rank func(Origin) int) func(a, b matched) bool {
		return func(a, b matched) bool {
			ra, rb := rank(a.origin), rank(b.origin)
			if ra != rb {
				return ra < rb
			}
			if a.spec.Less(b.spec) {
				return true
			}
			if b.spec.Less(a.spec) {
				return false
			}
			return a.order < b.order
		}
	}

	// 1. normal declarations, lowest to highest.
	sort.SliceStable(normal, func(i, j int) bool { return lessBy(normalRank)(normal[i], normal[j]) })
	for _, m := range normal {
		applyDeclaration(&cs, m.decl)
	}

	// 2. inline style="" (author origin). Normal inline declarations overlay all
	//    normal rules; inline !important joins the important set with an outsized
	//    specificity and author origin.
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range parseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matched{
					decl: d, origin: OriginAuthor,
					spec: Specificity{IDs: inlineImportantIDs}, order: order,
				})
				order++
				continue
			}
			applyDeclaration(&cs, d)
		}
	}

	// 3. important declarations overlay last.
	sort.SliceStable(important, func(i, j int) bool { return lessBy(importantRank)(important[i], important[j]) })
	for _, m := range important {
		applyDeclaration(&cs, m.decl)
	}
	return cs
}

// bestMatch returns the highest specificity among a rule's selectors that match
// n, and whether any matched.
func bestMatch(sels []Selector, n Node) (Specificity, bool) {
	var best Specificity
	found := false
	for _, s := range sels {
		if s.Matches(n) {
			if !found || best.Less(s.Specificity()) {
				best = s.Specificity()
				found = true
			}
		}
	}
	return best, found
}

// inheritFrom builds an element's base style: inherited properties carry over
// from the parent's computed style; everything else resets to initial.
func inheritFrom(parent ComputedStyle) ComputedStyle {
	cs := initialStyle()
	// Inherited properties (CSS): keep this set in sync with the ComputedStyle
	// doc comment. A property added to ComputedStyle but omitted here would
	// silently reset to initial instead of inheriting.
	cs.Color = parent.Color
	cs.FontFamily = parent.FontFamily
	cs.FontSizePt = parent.FontSizePt
	cs.Bold = parent.Bold
	cs.Italic = parent.Italic
	cs.LineHeight = parent.LineHeight
	cs.TextAlign = parent.TextAlign
	return cs
}

// initialStyle returns a ComputedStyle holding the CSS initial values, used as
// the base for the root element before any rule or inheritance is applied.
func initialStyle() ComputedStyle {
	black := color.RGBA{0, 0, 0, 255}
	return ComputedStyle{
		Display:     "inline",
		Color:       black,
		FontFamily:  "serif",
		FontSizePt:  16,
		LineHeight:  Length{Unit: UnitAuto},
		TextAlign:   "left",
		Width:       Length{Unit: UnitAuto},
		Height:      Length{Unit: UnitAuto},
		MinWidth:    Length{Unit: UnitPx},   // CSS initial min-width is 0
		MaxWidth:    Length{Unit: UnitAuto}, // models CSS "none" (no maximum)
		MinHeight:   Length{Unit: UnitPx},   // CSS initial min-height is 0
		MaxHeight:   Length{Unit: UnitAuto}, // models CSS "none" (no maximum)
		BoxSizing:   "content-box",
		MarginTop:   Length{Unit: UnitPx},
		MarginRight: Length{Unit: UnitPx},
		// remaining margins/paddings default to zero px (the zero value of Length is {0,UnitPx})
	}
}

// applyDeclaration interprets one declaration and writes it onto cs. Properties
// outside the supported normal-flow subset are ignored (left for later
// sub-projects). Malformed values are dropped, leaving the prior value intact.
func applyDeclaration(cs *ComputedStyle, d Declaration) {
	switch d.Property {
	case "display":
		cs.Display = d.Value
	case "color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.Color = c
		}
	case "background-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BackgroundColor = c
		}
	case "background":
		// Color-only support for now; see applyBackground.
		applyBackground(cs, d.Value)
	case "font-family":
		cs.FontFamily = firstFamily(d.Value)
	case "font-size":
		// "auto" is not a valid font-size, so the UnitAuto guard drops it.
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok && l.Unit != UnitAuto {
			cs.FontSizePt = l.Value // px:pt 1:1 for now; em/% resolution is the engine's job
		}
	case "font-weight":
		cs.Bold = d.Value == "bold" || d.Value == "700" || d.Value == "800" || d.Value == "900"
	case "font-style":
		cs.Italic = d.Value == "italic" || d.Value == "oblique"
	case "line-height":
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok {
			cs.LineHeight = l
		} else if d.Value == "normal" {
			cs.LineHeight = Length{Unit: UnitAuto}
		}
	case "text-align":
		switch d.Value {
		case "left", "right", "center", "justify":
			cs.TextAlign = d.Value
		}
	case "margin-top":
		setLength(&cs.MarginTop, d.Value)
	case "margin-right":
		setLength(&cs.MarginRight, d.Value)
	case "margin-bottom":
		setLength(&cs.MarginBottom, d.Value)
	case "margin-left":
		setLength(&cs.MarginLeft, d.Value)
	case "margin":
		applyBoxLengths(d.Value, parseMarginComponent,
			&cs.MarginTop, &cs.MarginRight, &cs.MarginBottom, &cs.MarginLeft)
	case "padding-top":
		setLength(&cs.PaddingTop, d.Value)
	case "padding-right":
		setLength(&cs.PaddingRight, d.Value)
	case "padding-bottom":
		setLength(&cs.PaddingBottom, d.Value)
	case "padding-left":
		setLength(&cs.PaddingLeft, d.Value)
	case "padding":
		applyBoxLengths(d.Value, parsePaddingComponent,
			&cs.PaddingTop, &cs.PaddingRight, &cs.PaddingBottom, &cs.PaddingLeft)
	case "width":
		setLength(&cs.Width, d.Value)
	case "height":
		setLength(&cs.Height, d.Value)
	case "min-width":
		setLength(&cs.MinWidth, d.Value)
	case "max-width":
		setMaxLength(&cs.MaxWidth, d.Value)
	case "min-height":
		setLength(&cs.MinHeight, d.Value)
	case "max-height":
		setMaxLength(&cs.MaxHeight, d.Value)
	case "box-sizing":
		switch d.Value {
		case "content-box", "border-box":
			cs.BoxSizing = d.Value
		}
	case "border-top-width":
		setLength(&cs.BorderTopWidth, d.Value)
	case "border-right-width":
		setLength(&cs.BorderRightWidth, d.Value)
	case "border-bottom-width":
		setLength(&cs.BorderBottomWidth, d.Value)
	case "border-left-width":
		setLength(&cs.BorderLeftWidth, d.Value)
	case "border-top-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderTopColor = c
		}
	case "border-right-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderRightColor = c
		}
	case "border-bottom-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderBottomColor = c
		}
	case "border-left-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderLeftColor = c
		}
	case "border-top-style":
		cs.BorderTopStyle = d.Value
	case "border-right-style":
		cs.BorderRightStyle = d.Value
	case "border-bottom-style":
		cs.BorderBottomStyle = d.Value
	case "border-left-style":
		cs.BorderLeftStyle = d.Value
	case "border-width":
		applyBoxLengths(d.Value, parseBorderWidthComponent,
			&cs.BorderTopWidth, &cs.BorderRightWidth, &cs.BorderBottomWidth, &cs.BorderLeftWidth)
	case "border-style":
		applyBorderStyle(cs, d.Value)
	case "border-color":
		applyBorderColor(cs, d.Value)
	case "border":
		// width||style||color applied to all four sides.
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderTopWidth, &cs.BorderTopColor, &cs.BorderTopStyle},
			borderSide{&cs.BorderRightWidth, &cs.BorderRightColor, &cs.BorderRightStyle},
			borderSide{&cs.BorderBottomWidth, &cs.BorderBottomColor, &cs.BorderBottomStyle},
			borderSide{&cs.BorderLeftWidth, &cs.BorderLeftColor, &cs.BorderLeftStyle})
	case "border-top":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderTopWidth, &cs.BorderTopColor, &cs.BorderTopStyle})
	case "border-right":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderRightWidth, &cs.BorderRightColor, &cs.BorderRightStyle})
	case "border-bottom":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderBottomWidth, &cs.BorderBottomColor, &cs.BorderBottomStyle})
	case "border-left":
		applyBorderSide(cs, d.Value,
			borderSide{&cs.BorderLeftWidth, &cs.BorderLeftColor, &cs.BorderLeftStyle})
	}
	// default: unsupported property — ignored on purpose.
}

// setLength parses val as a length and writes it to dst when valid.
func setLength(dst *Length, val string) {
	if l, ok := parseLength(newTokenizer(val).next()); ok {
		*dst = l
	}
}

// setMaxLength parses val as a max-* length and writes it to dst. The CSS keyword
// "none" (no maximum) is stored as a UnitAuto length; other values parse as
// ordinary lengths. Invalid values leave dst unchanged.
func setMaxLength(dst *Length, val string) {
	if val == "none" {
		*dst = Length{Unit: UnitAuto}
		return
	}
	setLength(dst, val)
}

// firstFamily returns the first family name from a font-family list, stripping
// quotes and whitespace (e.g. `"Helvetica Neue", Arial` -> `Helvetica Neue`).
func firstFamily(val string) string {
	for _, part := range splitComma(val) {
		part = trimQuotes(strings.TrimSpace(part))
		if part != "" {
			return part
		}
	}
	return val
}

// splitComma splits a comma-separated CSS value list (e.g. a font-family list).
func splitComma(s string) []string { return strings.Split(s, ",") }

func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
