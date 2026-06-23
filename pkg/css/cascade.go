package css

import (
	"image/color"
	"sort"
	"strings"
)

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
}

// Resolver computes the ComputedStyle of any node against a parsed stylesheet.
// Build one per stylesheet with NewResolver; it is read-only after construction
// and safe for concurrent use. logf may be nil.
type Resolver struct {
	sheet Stylesheet
	logf  func(string, ...any)
}

// NewResolver builds a Resolver over a parsed stylesheet.
func NewResolver(sheet Stylesheet, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Resolver{sheet: sheet, logf: logf}
}

// Compute returns node n's ComputedStyle. parentStyle is the already-computed
// style of n's parent (use initialStyle() for the root). The cascade is: start
// from the inheritance base, then apply every matching declaration in increasing
// (specificity, source-order) order, with !important declarations applied last.
func (r *Resolver) Compute(n Node, parentStyle ComputedStyle) ComputedStyle {
	cs := inheritFrom(parentStyle)

	type matched struct {
		decl  Declaration
		spec  Specificity
		order int
	}
	var normal, important []matched

	order := 0
	for ri := range r.sheet.Rules {
		rule := &r.sheet.Rules[ri]
		spec, ok := bestMatch(rule.Selectors, n)
		if !ok {
			continue
		}
		for _, d := range rule.Declarations {
			m := matched{decl: d, spec: spec, order: order}
			if d.Important {
				important = append(important, m)
			} else {
				normal = append(normal, m)
			}
			order++
		}
	}

	less := func(a, b matched) bool {
		if a.spec.Less(b.spec) {
			return true
		}
		if b.spec.Less(a.spec) {
			return false
		}
		return a.order < b.order // later source order wins, so sort ascending and apply in order
	}
	sort.SliceStable(normal, func(i, j int) bool { return less(normal[i], normal[j]) })

	// 1. normal author rules, lowest to highest (specificity, then source order).
	for _, m := range normal {
		applyDeclaration(&cs, m.decl)
	}

	// 2. inline style="" attribute. Its normal declarations overlay all normal
	//    rules regardless of their specificity; its !important declarations join
	//    the important set with an outsized specificity so inline-important is the
	//    strongest author origin (matching the CSS cascade origin order). Because
	//    the `important` slice is sorted in step 3 (below, after this block), the
	//    appended entries are included in that sort — no re-sorting needed.
	if styleAttr, ok := n.Attr("style"); ok {
		for _, d := range parseDeclarations(styleAttr) {
			if d.Important {
				important = append(important, matched{decl: d, spec: Specificity{IDs: 1 << 20}, order: order})
				order++
				continue
			}
			applyDeclaration(&cs, d)
		}
	}

	// 3. !important declarations overlay last so they always win. Sorting happens
	//    here — after the inline block so inline-important is included.
	sort.SliceStable(important, func(i, j int) bool { return less(important[i], important[j]) })
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
	case "padding-top":
		setLength(&cs.PaddingTop, d.Value)
	case "padding-right":
		setLength(&cs.PaddingRight, d.Value)
	case "padding-bottom":
		setLength(&cs.PaddingBottom, d.Value)
	case "padding-left":
		setLength(&cs.PaddingLeft, d.Value)
	case "width":
		setLength(&cs.Width, d.Value)
	case "height":
		setLength(&cs.Height, d.Value)
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
	}
	// default: unsupported property — ignored on purpose.
}

// setLength parses val as a length and writes it to dst when valid.
func setLength(dst *Length, val string) {
	if l, ok := parseLength(newTokenizer(val).next()); ok {
		*dst = l
	}
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
