package css

import (
	"image/color"
	"sort"
	"strconv"
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

	// ObjectFit is the replaced-element fitting mode (CSS object-fit):
	// "fill" (default) | "contain" | "cover" | "none" | "scale-down".
	ObjectFit string

	// Overflow is the CSS overflow shorthand: "visible" (default) | "hidden" |
	// "scroll" | "auto". Not inherited. overflow≠visible establishes a block
	// formatting context and clips the box's content to its padding box. In this
	// no-scrollbars single-tall-page model, scroll/auto clip exactly like hidden
	// (there is no scroll position or scrollbar chrome). overflow-x/overflow-y are
	// not modeled (a single shorthand value suffices since every clip keyword clips
	// identically here).
	Overflow string

	// Float is the CSS float value: "none" (default) | "left" | "right". Not
	// inherited. The box generator maps it to cssbox.FloatKind.
	Float string
	// Clear is the CSS clear value: "none" (default) | "left" | "right" | "both".
	// Not inherited. The layout engine lowers a cleared box below matching floats.
	Clear string

	// Position is the CSS position value: "static" (default) | "relative" |
	// "absolute" | "fixed". Not inherited. The box generator maps it to
	// cssbox.PositionKind.
	Position string
	// Top/Right/Bottom/Left are the positioning offset properties (CSS 9.3.2),
	// UnitAuto = "auto" (the initial value). Not inherited. Meaningful only on a
	// positioned box (relative: paint offset; absolute/fixed: placement against
	// the containing block).
	Top, Right, Bottom, Left Length
	// ZIndex is the stack level of a positioned box; ZIndexAuto models the "auto"
	// initial value (ZIndex is read only when ZIndexAuto is false). Not inherited.
	// Parsed now; the minimal stacking pass does not yet sort on it (positioned
	// boxes paint in document order) — full z-index ordering is a later slice.
	ZIndex     int
	ZIndexAuto bool

	// Flexbox (CSS Flexbox L1). Container properties read on a display:flex box;
	// item properties read on each flex item. Defaults set in initialStyle.
	FlexDirection  string // row | row-reverse | column | column-reverse
	FlexWrap       string // nowrap | wrap | wrap-reverse (only nowrap acted on today)
	JustifyContent string // flex-start | flex-end | center | space-between | space-around | space-evenly
	AlignItems     string // stretch | flex-start | flex-end | center | baseline
	AlignSelf      string // auto | stretch | flex-start | flex-end | center | baseline
	ColumnGap      Length // main-axis gap for row, cross-axis gap for column
	RowGap         Length // cross-axis gap for row, main-axis gap for column
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      Length // length | percentage | UnitAuto ("auto") | UnitContent ("content")
	Order          int

	// Grid (CSS Grid L1). Container properties read on a display:grid box; item
	// properties read on each grid item. Defaults set in initialStyle.
	GridTemplateColumns TrackList
	GridTemplateRows    TrackList
	GridTemplateAreas   GridAreas
	GridAutoColumns     []TrackSize   // implicit column tracks (nil = one auto track)
	GridAutoRows        []TrackSize   // implicit row tracks (nil = one auto track)
	GridAutoFlow        string        // "row" | "column" | "row dense" | "column dense"
	JustifyItems        string        // start|end|center|stretch|baseline
	JustifySelf         string        // auto|start|end|center|stretch|baseline
	AlignContent        string        // start|end|center|space-between|space-around|space-evenly|stretch
	GridPlacement       GridPlacement // an item's resolved col+row endpoints + optional area name

	// Table properties (CSS 2.1 §17).
	// BorderCollapse: "separate" (initial) | "collapse". Inherited.
	BorderCollapse string
	// BorderSpacingH/V: the two axes of border-spacing in points (initial 0,0).
	// Inherited; used only in border-collapse:separate.
	BorderSpacingH, BorderSpacingV float64
	// TableLayout: "auto" (initial) | "fixed". On the table box.
	TableLayout string
	// VerticalAlign: "baseline" (initial) | "top" | "middle" | "bottom" (+ sub/
	// super/text-top/text-bottom parsed, mapped to baseline for table-cell purposes).
	VerticalAlign string
	// CaptionSide: "top" (initial) | "bottom". Inherited.
	CaptionSide string
	// Direction: "ltr" (initial) | "rtl". Inherited. Parsed but NOT acted on (RTL
	// deferred); a non-ltr value on a table is logged by the layout engine.
	Direction string
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
	cs.BorderCollapse = parent.BorderCollapse
	cs.BorderSpacingH = parent.BorderSpacingH
	cs.BorderSpacingV = parent.BorderSpacingV
	cs.CaptionSide = parent.CaptionSide
	cs.Direction = parent.Direction
	// table-layout and vertical-align are NOT inherited (per CSS).
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
		ObjectFit:   "fill",    // CSS initial object-fit
		Overflow:    "visible", // CSS initial overflow
		Float:       "none",    // CSS initial float
		Clear:       "none",    // CSS initial clear
		Position:    "static",  // CSS initial position
		Top:         Length{Unit: UnitAuto},
		Right:       Length{Unit: UnitAuto},
		Bottom:      Length{Unit: UnitAuto},
		Left:        Length{Unit: UnitAuto},
		ZIndexAuto:  true, // CSS initial z-index is auto
		MarginTop:   Length{Unit: UnitPx},
		MarginRight: Length{Unit: UnitPx},
		// remaining margins/paddings default to zero px (the zero value of Length is {0,UnitPx})
		FlexDirection:  "row",
		FlexWrap:       "nowrap",
		JustifyContent: "flex-start",
		AlignItems:     "stretch",
		AlignSelf:      "auto",
		FlexGrow:       0,
		FlexShrink:     1,
		FlexBasis:      Length{Unit: UnitAuto},
		Order:          0,
		// ColumnGap, RowGap default to the zero Length ({0, UnitPx}) = no gap.
		GridAutoFlow: "row",
		JustifyItems: "stretch",
		JustifySelf:  "auto",
		AlignContent: "start",
		// GridAutoColumns/GridAutoRows default to nil: layout treats nil as one auto track.
		// GridTemplateColumns/Rows/Areas default to zero value (empty = no explicit template).
		BorderCollapse: "separate",
		TableLayout:    "auto",
		VerticalAlign:  "baseline",
		CaptionSide:    "top",
		Direction:      "ltr",
		// BorderSpacingH/V default to 0 (zero value).
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
	case "object-fit":
		switch d.Value {
		case "fill", "contain", "cover", "none", "scale-down":
			cs.ObjectFit = d.Value
		}
	case "overflow":
		switch d.Value {
		case "visible", "hidden", "scroll", "auto":
			cs.Overflow = d.Value
		}
	case "border-collapse":
		switch d.Value {
		case "separate", "collapse":
			cs.BorderCollapse = d.Value
		}
	case "border-spacing":
		applyBorderSpacing(cs, d.Value)
	case "table-layout":
		switch d.Value {
		case "auto", "fixed":
			cs.TableLayout = d.Value
		}
	case "vertical-align":
		switch d.Value {
		case "baseline", "top", "middle", "bottom",
			"sub", "super", "text-top", "text-bottom":
			cs.VerticalAlign = d.Value
		}
	case "caption-side":
		switch d.Value {
		case "top", "bottom":
			cs.CaptionSide = d.Value
		}
	case "direction":
		switch d.Value {
		case "ltr", "rtl":
			cs.Direction = d.Value
		}
	case "float":
		switch d.Value {
		case "left", "right", "none":
			cs.Float = d.Value
		}
	case "clear":
		switch d.Value {
		case "left", "right", "both", "none":
			cs.Clear = d.Value
		}
	case "position":
		switch d.Value {
		case "static", "relative", "absolute", "fixed":
			cs.Position = d.Value
		}
	case "top":
		setLength(&cs.Top, d.Value)
	case "right":
		setLength(&cs.Right, d.Value)
	case "bottom":
		setLength(&cs.Bottom, d.Value)
	case "left":
		setLength(&cs.Left, d.Value)
	case "z-index":
		applyZIndex(cs, d.Value)
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
	case "flex-direction":
		switch d.Value {
		case "row", "row-reverse", "column", "column-reverse":
			cs.FlexDirection = d.Value
		}
	case "flex-wrap":
		switch d.Value {
		case "nowrap", "wrap", "wrap-reverse":
			cs.FlexWrap = d.Value
		}
	case "justify-content":
		switch d.Value {
		case "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly",
			"start", "end", "stretch", "normal":
			cs.JustifyContent = d.Value
		}
	case "align-items":
		switch d.Value {
		case "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "normal":
			cs.AlignItems = d.Value
		}
	case "align-self":
		switch d.Value {
		case "auto", "stretch", "flex-start", "flex-end", "center", "baseline",
			"start", "end", "normal":
			cs.AlignSelf = d.Value
		}
	case "column-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.ColumnGap = l
		}
	case "row-gap":
		if l, ok := parseGapLength(d.Value); ok {
			cs.RowGap = l
		}
	case "flex-grow":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexGrow = v
		}
	case "flex-shrink":
		if v, ok := parseNonNegNumber(d.Value); ok {
			cs.FlexShrink = v
		}
	case "flex-basis":
		if l, ok := parseFlexBasis(d.Value); ok {
			cs.FlexBasis = l
		}
	case "order":
		if n, ok := parseInt(d.Value); ok {
			cs.Order = n
		}
	case "flex":
		applyFlexShorthand(cs, d.Value)
	case "gap":
		applyGapShorthand(cs, d.Value)
	case "grid-template-columns":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridTemplateColumns = tl
		}
		// "none" or "subgrid" leave the zero value (empty list = no explicit tracks).
	case "grid-template-rows":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridTemplateRows = tl
		}
	case "grid-template-areas":
		if ga, ok := parseTemplateAreas(d.Value); ok {
			cs.GridTemplateAreas = ga
		}
	case "grid-auto-columns":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridAutoColumns = tl.Expand(0)
		}
	case "grid-auto-rows":
		if tl, ok := parseTrackList(d.Value); ok {
			cs.GridAutoRows = tl.Expand(0)
		}
	case "grid-auto-flow":
		if v := normalizeAutoFlow(d.Value); v != "" {
			cs.GridAutoFlow = v
		}
	case "grid-column":
		if s, e, ok := parseGridColumnRow(d.Value); ok {
			cs.GridPlacement.ColStart, cs.GridPlacement.ColEnd = s, e
		}
	case "grid-row":
		if s, e, ok := parseGridColumnRow(d.Value); ok {
			cs.GridPlacement.RowStart, cs.GridPlacement.RowEnd = s, e
		}
	case "grid-area":
		if p, ok := parseGridArea(d.Value); ok {
			cs.GridPlacement = p
		}
	case "justify-items":
		switch d.Value {
		case "start", "end", "center", "stretch", "baseline", "flex-start", "flex-end", "normal":
			cs.JustifyItems = d.Value
		}
	case "justify-self":
		switch d.Value {
		case "auto", "start", "end", "center", "stretch", "baseline", "flex-start", "flex-end", "normal":
			cs.JustifySelf = d.Value
		}
	case "align-content":
		switch d.Value {
		case "start", "end", "center", "space-between", "space-around", "space-evenly", "stretch",
			"flex-start", "flex-end", "normal":
			cs.AlignContent = d.Value
		}
	case "place-items":
		applyPlaceItems(cs, d.Value)
	case "place-content":
		applyPlaceContent(cs, d.Value)
	case "place-self":
		applyPlaceSelf(cs, d.Value)
	case "grid-template":
		applyGridTemplate(cs, d.Value)
	case "grid":
		applyGridShorthand(cs, d.Value)
	}
	// default: unsupported property — ignored on purpose.
}

// normalizeAutoFlow canonicalizes a grid-auto-flow value to one of the four valid
// forms: "row", "column", "row dense", "column dense". It is order-insensitive:
// "dense" alone → "row dense"; "dense column"/"column dense" → "column dense";
// "dense row"/"row dense" → "row dense"; "row"/"column" → unchanged.
// Returns "" for any unrecognized value so the caller can skip the assignment.
func normalizeAutoFlow(val string) string {
	fields := splitComponents(strings.TrimSpace(val))
	if len(fields) == 0 || len(fields) > 2 {
		return ""
	}
	hasRow, hasColumn, hasDense := false, false, false
	for _, f := range fields {
		switch strings.ToLower(f) {
		case "row":
			hasRow = true
		case "column":
			hasColumn = true
		case "dense":
			hasDense = true
		default:
			return "" // unrecognized keyword
		}
	}
	// Ambiguous: both row and column not allowed.
	if hasRow && hasColumn {
		return ""
	}
	dir := "row"
	if hasColumn {
		dir = "column"
	}
	if hasDense {
		return dir + " dense"
	}
	return dir
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

// applyZIndex parses a z-index value: "auto" sets ZIndexAuto; an integer sets
// ZIndex (ZIndexAuto=false). A non-integer value is dropped, leaving the prior
// value. (Parsed now for the cascade; the minimal stacking pass does not yet sort
// on it.)
func applyZIndex(cs *ComputedStyle, val string) {
	if val == "auto" {
		cs.ZIndexAuto = true
		return
	}
	n, ok := parseInt(val)
	if !ok {
		return
	}
	cs.ZIndex, cs.ZIndexAuto = n, false
}

// applyBorderSpacing parses border-spacing: one length sets both axes, two lengths
// set horizontal then vertical. Percentages/auto are invalid here and dropped. A
// malformed value leaves the prior spacing intact.
func applyBorderSpacing(cs *ComputedStyle, value string) {
	tz := newTokenizer(value)
	var lens []Length
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			break
		}
		if tok.Kind == TokenWhitespace {
			continue
		}
		l, ok := parseLength(tok)
		if !ok || l.Unit == UnitAuto || l.Unit == UnitPercent {
			return // invalid component: drop the whole declaration
		}
		lens = append(lens, l)
	}
	switch len(lens) {
	case 1:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[0].Value
	case 2:
		cs.BorderSpacingH = lens[0].Value
		cs.BorderSpacingV = lens[1].Value
	}
}

// parseInt parses an optionally-signed base-10 integer, returning ok=false for any
// non-integer (including empty, a float, or trailing junk). Used for z-index.
func parseInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	i := 0
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
		if i == len(s) {
			return 0, false
		}
	}
	n := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, true
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

// parseNonNegNumber parses a unitless non-negative number (flex-grow/flex-shrink).
// A negative or non-numeric value yields ok=false (the property keeps its prior value).
func parseNonNegNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// parseGapLength parses a row-gap/column-gap value. "normal" is the initial value
// and means zero gap. Lengths/percentages parse normally; "auto" is invalid for gap.
func parseGapLength(s string) (Length, bool) {
	if strings.TrimSpace(s) == "normal" {
		return Length{0, UnitPx}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}

// parseFlexBasis parses a flex-basis value: "auto", "content", or a length/percentage.
func parseFlexBasis(s string) (Length, bool) {
	switch strings.TrimSpace(s) {
	case "auto":
		return Length{Unit: UnitAuto}, true
	case "content":
		return Length{Unit: UnitContent}, true
	}
	l, ok := parseLength(newTokenizer(s).next())
	if !ok || l.Unit == UnitAuto {
		return Length{}, false
	}
	return l, true
}
