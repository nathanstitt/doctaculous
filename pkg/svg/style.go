package svg

import (
	"image/color"
	"math"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Style is the resolved presentation state for one element, inherited
// parent→child per SVG/CSS cascade rules. fill/stroke hold the un-multiplied
// paint color; fillOpacity/strokeOpacity are the (also inherited) opacity
// properties. The two compose only in the fillRGBA/strokeRGBA readers, since
// fill-opacity is an ordinary inherited property (a child's own value
// replaces the parent's) rather than a multiplicative product down the tree.
//
// Style is exported so pkg/svg/draw (a separate package) can read a shape's
// resolved paint via FillPaint/StrokePaint/Opacity; its fields stay
// unexported since only those three accessors are part of the contract.
type Style struct {
	hasFill     bool // false: fill="none" (or a url() ref with no fallback color)
	fill        color.RGBA
	fillServer  string // "#id" fragment from fill="url(#id)"; "" = none referenced
	fillOpacity float64
	fillRule    render.FillRule

	hasStroke     bool
	stroke        color.RGBA
	strokeServer  string // "#id" fragment from stroke="url(#id)"; "" = none referenced
	strokeOpacity float64
	strokeWidth   float64 // user units
	cap           render.LineCap
	join          render.LineJoin
	miterLimit    float64
	dashes        []float64 // user units; nil = solid
	dashOffset    float64

	color   color.RGBA // the 'color' property, backing currentColor
	opacity float64    // element opacity [0,1]; NOT inherited
	display bool       // display != none
	visible bool       // visibility: visible (inherited)

	clipRule render.FillRule // clip-rule (inherited); which winding rule a clipPath child uses for itself

	// clipPathRef is the raw, unresolved clip-path property value ("none",
	// "url(#id)", or an invalid/unrecognized value), NOT inherited (clip-path
	// is a non-inherited property per SVG/CSS Masking). It is resolved
	// against the document index by the scene builder (see resolveClipPath in
	// clippath.go), not here: Style.apply only ever sees the cascade, never
	// docIndex, so it cannot look the id up itself — the same reason
	// fill/stroke url() references are recorded as a string here and
	// resolved later (see fillServer/strokeServer).
	clipPathRef string

	// maskRef is the raw, unresolved mask property value ("none",
	// "url(#id)", or an invalid/unrecognized value), NOT inherited (mask is
	// a non-inherited property per SVG/CSS Masking, exactly like
	// clip-path). Resolved against the document index by the scene builder
	// (see resolveMask in mask.go), not here — see clipPathRef's doc
	// comment for why.
	maskRef string

	// maskType is mask-type (SVG2: "luminance" (default) | "alpha"),
	// non-inherited. Unlike maskRef, this IS a plain enum with no
	// document-index dependency, so it is fully resolved here rather than
	// deferred to the scene builder.
	maskType string

	// overflow is the CSS overflow property, non-inherited, meaningful only
	// on a viewport-establishing element (<marker>, <symbol>, <svg>,
	// <pattern>). SVG's UA stylesheet gives those elements the initial value
	// "hidden" — the OPPOSITE of CSS's general "visible" default — so this
	// defaults to "hidden" and only an explicit "visible"/"scroll" turns the
	// viewport clip off. It is a plain enum with no document-index
	// dependency, so it resolves here (like maskType) rather than in the
	// scene builder; going through the cascade rather than reading the raw
	// attribute is what makes style="overflow:visible" and an `overflow`
	// sheet rule work, not just the presentation attribute.
	overflow string

	// markerStartRef, markerMidRef, markerEndRef are the raw, unresolved
	// marker-start/-mid/-end property values ("none", "url(#id)", or an
	// invalid/unrecognized value). Resolved against the document index by
	// the scene builder (see resolveMarkerRef in marker.go), not here — same
	// reason as clipPathRef/maskRef. UNLIKE clipPathRef/maskRef, these ARE
	// inherited (SVG marker properties inherit like an ordinary presentation
	// property — see the resvg inheritance-1/inheritance-2 fixtures), so
	// apply() does not reset them to "" on every call.
	markerStartRef string
	markerMidRef   string
	markerEndRef   string

	// fontFamily is the resolved font-family list, verbatim from the cascade
	// minus quoting (e.g. `"Noto Sans", sans-serif`). Inherited. It is passed
	// straight to layout/font.FaceCache.Resolve, which does its own
	// comma-splitting and per-candidate fallback, so no per-name resolution
	// happens here.
	fontFamily string

	// fontSizePt is the resolved font-size in user units (= CSS px = layout
	// pt in this engine). Inherited, and — unlike every other length property
	// here — resolved RELATIVELY: em/% resolve against the PARENT's
	// fontSizePt, ex against half of it, and the CSS absolute-size keywords
	// (xx-small..xx-large) against the 16px medium. That is why font-size is
	// applied before every other property in apply(): "1em" on any other
	// property must see this element's own already-resolved size.
	fontSizePt float64

	// fontBold and fontItalic are the weight/slant request handed to
	// pkgfont.Style. Both inherited. font-weight collapses to a boolean here
	// because the bundled families ship regular/bold only (see
	// pkg/font/standard) — a numeric weight >= 600 is bold, anything less is
	// not, and bolder/lighter step one notch from the inherited numeric
	// weight, which is why fontWeight below keeps the numeric value the CSS
	// relative keywords need.
	fontBold   bool
	fontItalic bool

	// fontWeight is the resolved numeric CSS font-weight (100..900),
	// inherited, kept alongside fontBold because "bolder"/"lighter" are
	// defined as a step relative to the INHERITED numeric weight, not to a
	// boolean. Nothing outside applyFontWeight reads it.
	fontWeight int

	// textAnchor is "start" (initial) | "middle" | "end", inherited. It
	// shifts a text chunk's whole advance about its start position; see
	// pkg/svg/draw's paintText.
	textAnchor string

	// direction is "ltr" (initial) | "rtl", inherited: the base paragraph
	// direction a <text>'s glyphs are bidi-reordered against (inline.DirLTR /
	// DirRTL).
	direction string

	// unicodeBidi is "normal" (initial) | "embed" | "bidi-override",
	// non-inherited per CSS. Only "bidi-override" changes behavior here: it
	// suppresses the bidi reorder so glyphs stay in logical order (the
	// override is applied by wrapping the text in the LRO/RLO control pair —
	// see textRunsFor).
	unicodeBidi string

	// letterSpacingPt and wordSpacingPt are the resolved letter-spacing /
	// word-spacing in user units, inherited. Both are resolved eagerly here
	// (em/ex/% against this element's own already-computed fontSizePt, which
	// is why applyFontSize runs first) so pkg/svg/draw never has to re-resolve
	// a length.
	//
	// DELIBERATE ASYMMETRY, not an oversight: these two properties exist
	// NOWHERE ELSE in this engine — not in pkg/css, not in pkg/layout/css — so
	// after this change letter-spacing works in SVG and silently does nothing
	// in HTML/DOCX. SVG text applies them as a post-shaping advance adjustment
	// on a flat glyph slice, which is self-contained; CSS reflow would have to
	// thread them through line-breaking and justification, which is a
	// materially larger job and is explicitly out of scope here. See the
	// design's decision 2.
	letterSpacingPt float64
	wordSpacingPt   float64

	// dominantBaseline and alignmentBaseline are the resolved baseline-
	// SELECTION keywords ("auto" initial). Both resolve to a physical offset in
	// pkg/svg/draw, where a Face and its metrics exist.
	//
	// Neither INHERITS. CSS Inline Layout §5 defines both as per-box
	// properties, and resvg agrees in a way its corpus pins twice over:
	// dominant-baseline/inherit.svg wraps a <text dominant-baseline="inherit">
	// in a <g dominant-baseline="middle"> and renders it UNSHIFTED (so the
	// keyword did not reach the text on its own), while
	// alignment-baseline/inherit.svg does the same with "hanging" and DOES
	// shift (so the explicit `inherit` pulled it). Non-inherited plus an
	// explicit-inherit that copies the parent is the one model that satisfies
	// both — see applyBaselineKeyword. dominant-baseline/nested.svg needs the
	// non-inheritance too: a sibling tspan's own value must win over an
	// uncle's.
	//
	// parentDominantBaseline / parentAlignmentBaseline carry the value an
	// explicit `inherit` reaches for, captured before the reset at the top of
	// apply(). setDominantBaseline records that THIS element wrote the
	// property itself, which the <text> boundary reset needs in order to
	// distinguish "declared here" from "arrived from an ancestor". None of
	// the three is read by the render path.
	dominantBaseline        string
	alignmentBaseline       string
	parentDominantBaseline  string
	parentAlignmentBaseline string
	setDominantBaseline     bool

	// baselineShiftPt is the CUMULATIVE baseline shift in user units, positive
	// = UP (away from the text's baseline). Unlike every other property here
	// it does not simply replace the parent's value: a shift inside a shift
	// ADDS (SVG2 §11.10.2 — the shifted baseline becomes the reference for a
	// nested shift), which is what resvg's nested-super/mixed-nested fixtures
	// assert. See applyBaselineShift.
	baselineShiftPt float64

	// decorations are the text-decoration lines DECLARED at this element,
	// each paired with the paint and font metrics in effect where it was
	// declared. SVG resolves a decoration's fill/stroke and its
	// position/thickness from the DECLARING element, not from the descendant
	// characters it happens to cover, so <text fill="red"
	// text-decoration="underline"><tspan fill="blue">x</tspan></text>
	// underlines in RED — resvg's style-resolving-1..4 fixtures assert exactly
	// this. Carrying the declaration (rather than a bare keyword set) is what
	// makes that possible.
	//
	// It is a slice rather than a bitmask because nested elements can declare
	// DIFFERENT lines with DIFFERENT paint (indirect-with-multiple-colors.svg:
	// a line-through from a <g> in the text's paint, plus an underline from a
	// tspan in the tspan's). Sharing the backing array across a cascade is
	// safe because apply() only ever appends through a fresh slice header —
	// see addDecoration.
	decorations []decoration

	// fontStretchIgnored, fontVariantIgnored, and kerningIgnored record that a
	// font-stretch / font-variant / kerning|font-kerning value reached the
	// cascade and was DEGRADED (logged and dropped) rather than honored — see
	// the three appliers for why none of them can do anything real with the
	// bundled faces. Nothing in the render path reads them; they exist so a
	// test can assert the degradation actually happened at the property that
	// requested it, which a log line alone cannot pin down. Inherited, exactly
	// like the properties they track.
	fontStretchIgnored bool
	fontVariantIgnored bool
	kerningIgnored     bool
}

// decoration is one declared text-decoration line together with the resolved
// presentation state of the element that declared it. See Style.decorations.
type decoration struct {
	// line is "underline", "overline", or "line-through".
	line string

	// style is the paint/metrics state at the DECLARING element, captured by
	// value. It is a *Style rather than an embedded Style purely to keep the
	// type acyclic (a Style cannot contain itself).
	style *Style
}

// DecorationLines returns, for each text-decoration line declared at or above
// this element (within the <text> subtree), the line keyword and the resolved
// Style of the element that declared it — the style whose fill/stroke paints
// the line and whose font-size positions it.
//
// The two slices are parallel and in declaration order (outermost first). The
// returned styles are copies; mutating one cannot reach the scene graph.
func (s Style) DecorationLines() (lines []string, styles []Style) {
	if len(s.decorations) == 0 {
		return nil, nil
	}
	lines = make([]string, 0, len(s.decorations))
	styles = make([]Style, 0, len(s.decorations))
	for _, d := range s.decorations {
		lines = append(lines, d.line)
		styles = append(styles, *d.style)
	}
	return lines, styles
}

// defaultStyle returns the SVG initial presentation state: black fill, no
// stroke, 1-unit stroke width, butt caps, miter joins with a 4 miter limit,
// nonzero fill rule, solid dashes, full opacity, black 'color', and a
// displayed/visible element.
func defaultStyle() Style {
	return Style{
		hasFill:       true,
		fill:          color.RGBA{0, 0, 0, 255},
		fillOpacity:   1,
		fillRule:      render.NonZero,
		hasStroke:     false,
		stroke:        color.RGBA{0, 0, 0, 255},
		strokeOpacity: 1,
		strokeWidth:   1,
		cap:           render.ButtCap,
		join:          render.MiterJoin,
		miterLimit:    4,
		dashes:        nil,
		dashOffset:    0,
		color:         color.RGBA{0, 0, 0, 255},
		opacity:       1,
		display:       true,
		visible:       true,
		clipRule:      render.NonZero,
		clipPathRef:   "", // not inherited; reset every apply() call below
		maskRef:       "", // not inherited; reset every apply() call below
		maskType:      "luminance",
		overflow:      "hidden", // not inherited; reset every apply() call below
		// markerStartRef/markerMidRef/markerEndRef default to "" (no
		// marker) and, being inherited, are NOT reset in apply() below.
		fontFamily:  "sans-serif",
		fontSizePt:  defaultFontSizePt,
		fontBold:    false,
		fontItalic:  false,
		fontWeight:  400,
		textAnchor:  "start",
		direction:   "ltr",
		unicodeBidi: "normal", // not inherited; reset every apply() call below
		// letterSpacingPt/wordSpacingPt/baselineShiftPt default to 0 and
		// decorations to nil, all of which are the zero value.
		// Both baseline-selection properties (and the two shadow fields an
		// explicit `inherit` reads) are non-inherited and reset on every
		// apply() call below.
		dominantBaseline:        "auto",
		alignmentBaseline:       "auto",
		parentDominantBaseline:  "auto",
		parentAlignmentBaseline: "auto",
	}
}

// defaultFontSizePt is the initial font-size (CSS "medium"), in user units.
// It is also the reference the CSS absolute-size keywords scale against.
const defaultFontSizePt = 16.0

// apply returns parent overridden by el's resolved style. ctx supplies the
// cascade: with a nil ctx (or one built from a document with no stylesheets
// or style="" attributes anywhere) attr resolution falls back to el's
// presentation attributes alone, exactly PR 1's behavior — every one of PR
// 1's 148 golden fixtures uses no CSS and so exercises only this path. A
// url() paint reference is recorded (not resolved: apply has no index to
// resolve it against — see Style.FillServer/StrokeServer and the scene
// builder). ctx's logf receives a debug line for any attribute value that
// fails to parse — the attribute is then ignored, per SVG's error-handling
// model, and the inherited value is kept. opacity is not inherited: it
// resets to el's own value (default 1) on every call. Every other listed
// property is inherited.
func (parent Style) apply(el *element, ctx *cascadeCtx) Style {
	s := parent
	s.opacity = 1            // not inherited; may be overridden below
	s.clipPathRef = ""       // not inherited; may be overridden below
	s.maskRef = ""           // not inherited; may be overridden below
	s.maskType = "luminance" // not inherited; may be overridden below
	s.overflow = "hidden"    // not inherited; may be overridden below
	s.unicodeBidi = "normal" // not inherited; may be overridden below
	// alignment-baseline is per-box (CSS Inline §5) and does NOT inherit:
	// resvg's dominant-baseline/nested.svg puts it on one tspan and expects a
	// SIBLING tspan's own dominant-baseline to win, which only works if the
	// value does not leak into a grandchild. dominant-baseline DOES propagate
	// down a <text> subtree (dummy-tspan.svg shifts a plain tspan with its
	// parent), so it is left inherited here and is instead reset once at the
	// <text> boundary — see Style.resetBaselines.
	//
	// The pre-reset value is kept so an explicit `inherit` can still reach for
	// it; see applyBaselineKeyword.
	s.parentDominantBaseline = parent.dominantBaseline
	s.parentAlignmentBaseline = parent.alignmentBaseline
	s.alignmentBaseline = "auto"
	s.setDominantBaseline = false

	if el == nil {
		return s
	}
	attr := ctx.resolve(el)
	var logf func(string, ...any)
	if ctx != nil {
		logf = ctx.logf
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// font-size must resolve FIRST: its own em/ex/% values resolve against
	// the PARENT's size (still in s at this point), and every other
	// font-relative length on this element would want this element's own
	// already-resolved size. Likewise applyFontWeight reads the inherited
	// numeric weight for bolder/lighter before overwriting it.
	// The `font` SHORTHAND runs first of all, so an explicit longhand on the
	// same element still wins (CSS shorthand-then-longhand order within one
	// declaration block is source order, but the corpus only exercises the
	// far more common "shorthand alone" case, and losing to a longhand is the
	// safer of the two possible orders).
	applyFontShorthand(&s, parent.fontSizePt, parent.fontWeight, attr, logf)
	applyFontSize(&s, parent.fontSizePt, attr, logf)
	applyFontFamily(&s, attr, logf)
	applyFontWeight(&s, parent.fontWeight, attr, logf)
	applyFontStyle(&s, attr, logf)
	applyFontStretch(&s, attr, logf)
	applyFontVariant(&s, attr, logf)
	applyKerning(&s, attr, logf)
	applyTextAnchor(&s, attr, logf)
	applyDirection(&s, attr, logf)
	applyUnicodeBidi(&s, attr, logf)
	// letter-spacing/word-spacing resolve AFTER font-size: their em/ex/%
	// values are relative to this element's own already-computed size.
	applySpacing("letter-spacing", &s.letterSpacingPt, s.fontSizePt, attr, logf)
	applySpacing("word-spacing", &s.wordSpacingPt, s.fontSizePt, attr, logf)
	applyDominantBaseline(&s, attr, logf)
	applyAlignmentBaseline(&s, attr, logf)
	applyBaselineShift(&s, parent.baselineShiftPt, attr, logf)

	// 'color' must resolve before fill/stroke so currentColor sees the
	// element's own (already-updated) color, not the parent's.
	applyColorProp(&s, attr, logf)

	applyPaint("fill", &s.hasFill, &s.fill, &s.fillServer, s.color, attr, logf)
	applyOpacityProp("fill-opacity", &s.fillOpacity, attr, logf)
	applyFillRule(&s, attr, logf)

	applyPaint("stroke", &s.hasStroke, &s.stroke, &s.strokeServer, s.color, attr, logf)
	applyOpacityProp("stroke-opacity", &s.strokeOpacity, attr, logf)
	applyStrokeWidth(&s, attr, logf)
	applyLineCap(&s, attr, logf)
	applyLineJoin(&s, attr, logf)
	applyMiterLimit(&s, attr, logf)
	applyDashArray(&s, attr, logf)
	applyDashOffset(&s, attr, logf)

	applyOpacityProp("opacity", &s.opacity, attr, logf)
	applyDisplay(&s, attr, logf)
	applyVisibility(&s, attr, logf)
	applyClipRule(&s, attr, logf)
	applyClipPathProp(&s, attr, logf)
	applyMaskProp(&s, attr, logf)
	applyMaskType(&s, attr, logf)
	applyOverflow(&s, attr, logf)
	// There is deliberately no applyMarkerShorthand here: the "marker"
	// shorthand is expanded into these three longhands inside the cascade
	// (see setResolved in cascade.go), which is the only place with the
	// origin and source-order information needed to rank a shorthand
	// against its own longhands correctly in both directions. By the time
	// attr() is readable here the cascade has already collapsed to one
	// value per property, so any shorthand handling at this layer could
	// only impose a fixed, and therefore sometimes wrong, precedence.
	applyMarkerProp("marker-start", &s.markerStartRef, attr, logf)
	applyMarkerProp("marker-mid", &s.markerMidRef, attr, logf)
	applyMarkerProp("marker-end", &s.markerEndRef, attr, logf)

	// text-decoration LAST: a decoration declared here is painted with THIS
	// element's fill/stroke and positioned by THIS element's font metrics, so
	// the snapshot it takes must see every paint and font property already
	// resolved above.
	applyTextDecoration(&s, attr, logf)

	return s
}

// fillRGBA composes the fill color with fill-opacity into the alpha channel.
func (s Style) fillRGBA() color.RGBA {
	return composeAlpha(s.fill, s.fillOpacity)
}

// strokeRGBA composes the stroke color with stroke-opacity into the alpha channel.
func (s Style) strokeRGBA() color.RGBA {
	return composeAlpha(s.stroke, s.strokeOpacity)
}

// composeAlpha scales c's alpha channel by opacity (clamped to [0,1]).
func composeAlpha(c color.RGBA, opacity float64) color.RGBA {
	opacity = clamp(opacity, 0, 1)
	c.A = uint8(math.Round(float64(c.A) * opacity))
	return c
}

// applyColorProp resolves the 'color' property, which backs currentColor.
// It is an ordinary inherited color property with no special keywords.
func applyColorProp(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("color")
	if !ok || val == "inherit" {
		return
	}
	c, ok := parseColorValue(val)
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", "color", val)
		return
	}
	s.color = c
}

// applyPaint resolves a fill/stroke-like paint attribute: "none" clears
// *has and *server, "currentColor" resolves against cur, "url(#id)" (with
// SVG's optional fallback-color syntax, "url(#id) red") records the
// referenced fragment id in *server for the scene builder to resolve
// against the document index — plus the fallback color, if given, applied
// exactly as an ordinary color would be so *has/*c reflect it. A url() with
// NO fallback clears *has (mirroring the "none" case): per SVG, the
// fallback is only ever the explicit color written in the attribute value
// itself, never the inherited fill/stroke, so FillPaint/StrokePaint must
// not paint the parent's solid color for a still-unresolved reference — the
// scene builder (buildShape) is the one place with the document index to
// resolve *server into an actual gradient/pattern, and does so entirely
// independently of *has/*c. "inherit" keeps the parent's value, and
// anything else is parsed as a color or logged and ignored.
func applyPaint(name string, has *bool, c *color.RGBA, server *string, cur color.RGBA, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr(name)
	if !ok {
		return
	}
	val = strings.TrimSpace(val)
	switch {
	case val == "inherit":
		return
	case val == "none":
		*has = false
		*server = ""
		return
	case val == "currentColor":
		*has = true
		*server = ""
		*c = cur
		return
	case strings.HasPrefix(strings.ToLower(val), "url("):
		id, fallback, ok := parsePaintServerRef(val)
		if !ok {
			logf("svg: ignoring %s=%q: unparseable url() reference", name, val)
			return
		}
		if fallback == "" {
			*server = id
			*has = false
			return
		}
		parsed, ok := parseColorValue(fallback)
		if !ok {
			// The whole value is invalid per SVG/CSS error handling: neither
			// the reference nor a fallback commits, and the property keeps
			// whatever it already had (inherited *has/*c/*server), not "no
			// paint" — mirroring the unparseable-plain-color branch below.
			logf("svg: ignoring %s=%q: unparseable fallback color", name, val)
			return
		}
		*server = id
		*has = true
		*c = parsed
		return
	}
	*server = ""
	parsed, ok := parseColorValue(val)
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", name, val)
		return
	}
	*has = true
	*c = parsed
}

// parsePaintServerRef splits an SVG paint value beginning with "url(" into
// the referenced fragment id (with its leading "#", e.g. "#g") and the
// optional trailing fallback color text (SVG's "url(#g) red" syntax, empty
// when absent). ok is false when the "url(" is not closed by a matching
// ")", which degrades safely (the caller ignores the whole value) rather
// than panicking or guessing at a truncated id.
func parsePaintServerRef(val string) (id, fallback string, ok bool) {
	end := strings.IndexByte(val, ')')
	if end < 0 {
		return "", "", false
	}
	id = strings.TrimSpace(val[len("url("):end])
	id = strings.Trim(id, `"'`)
	fallback = strings.TrimSpace(val[end+1:])
	return id, fallback, true
}

// applyOpacityProp resolves an opacity-like attribute (fill-opacity,
// stroke-opacity, opacity) into *dst, clamped to [0,1].
func applyOpacityProp(name string, dst *float64, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr(name)
	if !ok || val == "inherit" {
		return
	}
	v, ok := parseNumber(strings.TrimSpace(strings.TrimSuffix(val, "%")))
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", name, val)
		return
	}
	if strings.HasSuffix(strings.TrimSpace(val), "%") {
		v /= 100
	}
	*dst = clamp(v, 0, 1)
}

// applyFillRule resolves fill-rule (nonzero|evenodd).
func applyFillRule(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("fill-rule")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "nonzero":
		s.fillRule = render.NonZero
	case "evenodd":
		s.fillRule = render.EvenOdd
	default:
		logf("svg: ignoring %s=%q: unparseable", "fill-rule", val)
	}
}

// applyClipRule resolves clip-rule (nonzero|evenodd), the winding rule a
// clipPath CHILD uses to determine its own interior — distinct from
// fill-rule, which governs how the same shape paints when it is not being
// used as clip geometry. clip-rule is inherited, exactly like fill-rule.
func applyClipRule(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("clip-rule")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "nonzero":
		s.clipRule = render.NonZero
	case "evenodd":
		s.clipRule = render.EvenOdd
	default:
		logf("svg: ignoring %s=%q: unparseable", "clip-rule", val)
	}
}

// applyClipPathProp records the raw clip-path property value for the scene
// builder to resolve against the document index. "none" and "inherit" both
// clear/keep clipPathRef as appropriate; anything else (including a
// syntactically invalid url()) is recorded verbatim — resolveClipPath is
// where an invalid FuncIRI is distinguished from a valid one that doesn't
// resolve, both of which mean "no clipping" per SVG's error-handling model.
func applyClipPathProp(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("clip-path")
	if !ok || val == "inherit" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "none" {
		s.clipPathRef = ""
		return
	}
	s.clipPathRef = val
}

// applyMaskProp records the raw mask property value for the scene builder to
// resolve against the document index, mirroring applyClipPathProp exactly:
// "none" and "inherit" both clear/keep maskRef as appropriate; anything else
// (including a syntactically invalid url()) is recorded verbatim —
// resolveMask is where an invalid FuncIRI is distinguished from a valid one
// that doesn't resolve, both of which mean "no masking" per SVG's
// error-handling model.
func applyMaskProp(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("mask")
	if !ok || val == "inherit" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "none" {
		s.maskRef = ""
		return
	}
	s.maskRef = val
}

// applyMarkerProp records the raw value of one marker-start/-mid/-end
// longhand for the scene builder to resolve against the document index,
// mirroring applyClipPathProp/applyMaskProp's recording shape exactly
// ("none"/absent clear *dst, "inherit" keeps the parent's value, anything
// else — including a syntactically invalid url() — is recorded verbatim).
// The key difference from clip-path/mask: markers ARE inherited, so *dst is
// never reset to "" at the top of apply() the way clipPathRef/maskRef are —
// this function is the ONLY place a marker-*-ref field changes, and it only
// runs when the attribute is actually present on this element.
func applyMarkerProp(name string, dst *string, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr(name)
	if !ok || val == "inherit" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "none" {
		*dst = ""
		return
	}
	*dst = val
}

// applyMaskType resolves mask-type (SVG2: luminance|alpha), non-inherited.
// An unrecognized value is logged and ignored, keeping the default
// (luminance) — matching applyFillRule's error-handling shape for an
// unparseable enum.
func applyMaskType(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("mask-type")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "luminance", "alpha":
		s.maskType = val
	default:
		logf("svg: ignoring %s=%q: unparseable", "mask-type", val)
	}
}

// applyOverflow resolves the overflow property, non-inherited. The value is
// trimmed and lowercased first: overflow reaches here from a presentation
// attribute as well as CSS, and both are whitespace-tolerant while CSS
// keywords are ASCII case-insensitive — the raw-attribute read this
// replaced honored neither. An unrecognized value is logged and ignored,
// keeping SVG's "hidden" default for a viewport-establishing element,
// matching applyMaskType's error-handling shape.
func applyOverflow(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("overflow")
	if !ok || val == "inherit" {
		return
	}
	switch val = strings.ToLower(strings.TrimSpace(val)); val {
	case "visible", "scroll", "hidden", "auto":
		s.overflow = val
	default:
		logf("svg: ignoring %s=%q: unparseable", "overflow", val)
	}
}

// applyStrokeWidth resolves stroke-width as a length in user units.
func applyStrokeWidth(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-width")
	if !ok || val == "inherit" {
		return
	}
	v, ok := parseLength(val, 0)
	if !ok || v < 0 {
		logf("svg: ignoring %s=%q: unparseable", "stroke-width", val)
		return
	}
	s.strokeWidth = v
}

// applyLineCap resolves stroke-linecap (butt|round|square).
func applyLineCap(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-linecap")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "butt":
		s.cap = render.ButtCap
	case "round":
		s.cap = render.RoundCap
	case "square":
		s.cap = render.SquareCap
	default:
		logf("svg: ignoring %s=%q: unparseable", "stroke-linecap", val)
	}
}

// applyLineJoin resolves stroke-linejoin (miter|round|bevel); the SVG2
// "arcs" and "miter-clip" values map to miter (with a log line) since the
// renderer does not implement those join geometries.
func applyLineJoin(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-linejoin")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "miter":
		s.join = render.MiterJoin
	case "round":
		s.join = render.RoundJoin
	case "bevel":
		s.join = render.BevelJoin
	case "arcs", "miter-clip":
		logf("svg: %s=%q: unsupported join geometry, using miter", "stroke-linejoin", val)
		s.join = render.MiterJoin
	default:
		logf("svg: ignoring %s=%q: unparseable", "stroke-linejoin", val)
	}
}

// applyMiterLimit resolves stroke-miterlimit, a bare number >= 1.
func applyMiterLimit(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-miterlimit")
	if !ok || val == "inherit" {
		return
	}
	v, ok := parseNumber(val)
	if !ok || v < 1 {
		logf("svg: ignoring %s=%q: unparseable", "stroke-miterlimit", val)
		return
	}
	s.miterLimit = v
}

// applyDashArray resolves stroke-dasharray. "none" (or the absent attribute)
// means solid. A list containing a negative value, or whose values sum to
// zero, is treated as solid per the SVG spec's error-handling rule for this
// property. Odd-length lists are kept as-is; consumers (rasterx) repeat them
// to even length.
func applyDashArray(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-dasharray")
	if !ok || val == "inherit" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "none" {
		s.dashes = nil
		return
	}
	list := parseNumberList(val)
	if list == nil {
		logf("svg: ignoring %s=%q: unparseable", "stroke-dasharray", val)
		return
	}
	sum := 0.0
	for _, v := range list {
		if v < 0 {
			s.dashes = nil
			return
		}
		sum += v
	}
	if sum == 0 {
		s.dashes = nil
		return
	}
	s.dashes = list
}

// applyDashOffset resolves stroke-dashoffset as a length in user units.
func applyDashOffset(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("stroke-dashoffset")
	if !ok || val == "inherit" {
		return
	}
	v, ok := parseLength(val, 0)
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", "stroke-dashoffset", val)
		return
	}
	s.dashOffset = v
}

// applyDisplay resolves display: "none" hides the element (and its
// subtree); any other value (or the attribute's absence) leaves it shown.
// display is not an inherited CSS property, but non-none values don't need
// tracking beyond "shown", so resetting isn't necessary: a parent's
// display:none is enforced by the tree walker skipping the subtree, not by
// child inheritance.
func applyDisplay(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("display")
	if !ok || val == "inherit" {
		return
	}
	s.display = val != "none"
}

// applyVisibility resolves visibility (visible|hidden|collapse), which is
// an inherited property.
func applyVisibility(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("visibility")
	if !ok || val == "inherit" {
		return
	}
	switch val {
	case "visible":
		s.visible = true
	case "hidden", "collapse":
		s.visible = false
	default:
		logf("svg: ignoring %s=%q: unparseable", "visibility", val)
	}
}

// FillServer returns the "#id" fragment of a fill="url(#id)" (or
// "url(#id) fallback") reference, and whether one is present. The scene
// builder resolves id against the document index; applyPaint never does,
// since it only has the element's attributes, not the index.
func (s Style) FillServer() (id string, ok bool) {
	return s.fillServer, s.fillServer != ""
}

// StrokeServer returns the "#id" fragment of a stroke="url(#id)" (or
// "url(#id) fallback") reference, and whether one is present. The scene
// builder resolves id against the document index; applyPaint never does,
// since it only has the element's attributes, not the index.
func (s Style) StrokeServer() (id string, ok bool) {
	return s.strokeServer, s.strokeServer != ""
}

// FillPaint returns the element's composed fill paint (color with
// fill-opacity folded into alpha, plus the fill rule). ok is false when
// there is no fill to paint: fill="none", a url() paint-server reference
// with no fallback color (FillServer reports that case so the scene
// builder can still resolve and paint it), or the element is invisible
// (visibility:hidden). display:none is not checked here — the scene
// builder never reaches this far for a display:none subtree; see the
// tree-walker's subtree skip.
func (s Style) FillPaint() (render.FillPaint, bool) {
	if !s.hasFill || !s.visible {
		return render.FillPaint{}, false
	}
	return render.FillPaint{Color: s.fillRGBA(), Rule: s.fillRule}, true
}

// StrokePaint returns the element's stroke paint in USER UNITS: Width,
// DashArray, and DashPhase are not yet scaled into device space, since only
// the caller (pkg/svg/draw) knows the local→device transform in effect for
// this shape. ok is false when there is no stroke to paint: stroke="none",
// a url() paint-server reference with no fallback color (StrokeServer
// reports that case so the scene builder can still resolve and paint it),
// the element is invisible, or the resolved stroke-width is <= 0 (a
// zero-width stroke paints nothing, per SVG).
func (s Style) StrokePaint() (render.StrokePaint, bool) {
	if !s.hasStroke || !s.visible || s.strokeWidth <= 0 {
		return render.StrokePaint{}, false
	}
	return render.StrokePaint{
		Color:      s.strokeRGBA(),
		Width:      s.strokeWidth,
		Cap:        s.cap,
		Join:       s.join,
		MiterLimit: s.miterLimit,
		DashArray:  s.dashes,
		DashPhase:  s.dashOffset,
	}, true
}

// StrokeWidthValue returns the element's resolved (inherited) stroke-width
// property value in user units, REGARDLESS of whether the element has a
// paintable stroke (stroke="none", or no stroke color set at all — unlike
// StrokePaint, which reports ok=false in exactly those cases). A
// markerUnits="strokeWidth" marker scales by this raw property value per
// SVG2 §11.6.7 ("the value of the stroke-width property"), not by whether a
// stroke actually gets painted — see with-a-large-stroke.svg, which sets
// stroke-width="10" with no stroke color at all and still expects markers
// scaled by 10, not by StrokePaint's zero-stroke fallback.
func (s Style) StrokeWidthValue() float64 {
	return s.strokeWidth
}

// Opacity returns the element's own (non-inherited) opacity in [0,1].
func (s Style) Opacity() float64 {
	return s.opacity
}

// ClipRule returns the element's resolved (inherited) clip-rule, used only
// when this element appears as a <clipPath> child to determine its own
// interior for the union.
func (s Style) ClipRule() render.FillRule {
	return s.clipRule
}

// ClipPathRef returns the element's raw, unresolved (non-inherited)
// clip-path property value ("url(#id)" or an invalid value) and whether one
// is present ("" / absent / "none" report ok=false).
func (s Style) ClipPathRef() (string, bool) {
	return s.clipPathRef, s.clipPathRef != ""
}

// MaskRef returns the element's raw, unresolved (non-inherited) mask
// property value ("url(#id)" or an invalid value) and whether one is
// present ("" / absent / "none" report ok=false).
func (s Style) MaskRef() (string, bool) {
	return s.maskRef, s.maskRef != ""
}

// MaskTypeValue returns the element's resolved (non-inherited) mask-type
// value: "luminance" (default) or "alpha".
func (s Style) MaskTypeValue() string {
	return s.maskType
}

// WantsViewportClip reports whether a viewport-establishing element's
// resolved overflow clips its content to that viewport: true for the SVG
// default "hidden" (and for "auto"), false only for "visible"/"scroll".
// Callers are <marker> and <symbol>, which share the identical default.
func (s Style) WantsViewportClip() bool {
	switch s.overflow {
	case "visible", "scroll":
		return false
	default:
		return true
	}
}

// MarkerStartRef returns the element's raw, unresolved (INHERITED) marker-start
// property value ("url(#id)" or an invalid value) and whether one is present
// ("" / absent / "none" report ok=false).
func (s Style) MarkerStartRef() (string, bool) {
	return s.markerStartRef, s.markerStartRef != ""
}

// MarkerMidRef returns the element's raw, unresolved (INHERITED) marker-mid
// property value, mirroring MarkerStartRef.
func (s Style) MarkerMidRef() (string, bool) {
	return s.markerMidRef, s.markerMidRef != ""
}

// MarkerEndRef returns the element's raw, unresolved (INHERITED) marker-end
// property value, mirroring MarkerStartRef.
func (s Style) MarkerEndRef() (string, bool) {
	return s.markerEndRef, s.markerEndRef != ""
}

// absoluteFontSizes maps the CSS absolute-size keywords to their scale factor
// against defaultFontSizePt ("medium"), per CSS Fonts §3.5's suggested
// ratios. SVG's font-size accepts these keywords as well as lengths.
var absoluteFontSizes = map[string]float64{
	"xx-small": 3.0 / 5,
	"x-small":  3.0 / 4,
	"small":    8.0 / 9,
	"medium":   1,
	"large":    6.0 / 5,
	"x-large":  3.0 / 2,
	"xx-large": 2,
}

// relativeFontSizes maps the two CSS relative-size keywords to their factor
// against the PARENT's computed size.
var relativeFontSizes = map[string]float64{
	"smaller": 5.0 / 6,
	"larger":  6.0 / 5,
}

// applyFontSize resolves font-size into s.fontSizePt (user units), inherited.
// parentPt is the parent's already-computed size, which em/ex/percentage and
// the smaller/larger keywords all resolve against — s.fontSizePt still holds
// it on entry, but taking it explicitly makes that dependency impossible to
// break by reordering the appliers.
//
// A negative resolved size is invalid per SVG and is ignored (the inherited
// value is kept); a resolved size of exactly zero IS valid and is kept, since
// SVG's zero-size fixtures require the text to vanish rather than fall back
// to the inherited size.
func applyFontSize(s *Style, parentPt float64, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-size")
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if f, ok := absoluteFontSizes[val]; ok {
		s.fontSizePt = defaultFontSizePt * f
		return
	}
	if f, ok := relativeFontSizes[val]; ok {
		s.fontSizePt = parentPt * f
		return
	}
	// splitLengthUnit reports ok only for a bare number or a FONT-RELATIVE
	// unit; on "40px" its parseNumber fails and it returns ok=false. That is
	// not an error — it means "not font-relative", which is exactly what the
	// default branch below delegates to parseLength. Bailing on !ok here (as
	// this did before) silently dropped every absolute-unit font-size,
	// including the very common font-size="40px", back to the inherited value.
	v, unit, numOK := splitLengthUnit(val)
	if unit != "" && !numOK {
		// A font-relative unit whose NUMBER did not parse ("xem", "%"): the
		// value is genuinely invalid, and falling through to parseLength would
		// only fail again after silently computing against a zero v.
		logf("svg: ignoring %s=%q: unparseable", "font-size", val)
		return
	}
	var pt float64
	switch unit {
	case "em":
		pt = v * parentPt
	case "ex":
		// No face is resolved at cascade time, so ex uses the conventional
		// 0.5em approximation rather than the face's real x-height. Matching
		// parseLength's own em/ex handling; a real x-height would need the
		// resolved face, which lives a layer away in pkg/svg/draw.
		pt = v * parentPt * 0.5
	case "%":
		pt = v / 100 * parentPt
	default:
		// An absolute unit (px/pt/pc/mm/cm/in) or none: reuse parseLength so
		// the unit table stays in one place. ref is unused for these.
		abs, ok := parseLength(val, 0)
		if !ok {
			logf("svg: ignoring %s=%q: unparseable", "font-size", val)
			return
		}
		pt = abs
	}
	if pt < 0 {
		logf("svg: ignoring %s=%q: negative size", "font-size", val)
		return
	}
	s.fontSizePt = pt
}

// splitLengthUnit splits an already-trimmed, lowercased length into its
// numeric part and unit suffix. ok is false when the numeric part does not
// parse as a finite SVG number. It exists so applyFontSize can branch on the
// FONT-RELATIVE units (em/ex/%) before delegating the absolute ones to
// parseLength, which hardcodes its own em/ex approximation against the UA
// default rather than against the parent's computed size.
func splitLengthUnit(s string) (v float64, unit string, ok bool) {
	for _, u := range [...]string{"em", "ex", "%"} {
		if strings.HasSuffix(s, u) {
			n, ok := parseNumber(strings.TrimSuffix(s, u))
			return n, u, ok
		}
	}
	n, ok := parseNumber(s)
	return n, "", ok
}

// applyFontFamily resolves font-family into s.fontFamily, inherited. The value
// is kept as a whole comma-separated list with per-name quoting stripped:
// layout/font.FaceCache.Resolve splits and falls back through the list itself,
// so splitting here would only duplicate that. An empty/whitespace-only value
// is ignored (the inherited family is kept), matching SVG error handling.
func applyFontFamily(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-family")
	if !ok || val == "inherit" {
		return
	}
	cleaned := cleanFamilyList(val)
	if cleaned == "" {
		logf("svg: ignoring %s=%q: no usable family name", "font-family", val)
		return
	}
	s.fontFamily = cleaned
}

// cleanFamilyList strips surrounding quotes from each comma-separated family
// name and drops empty entries, returning the rejoined list ("" when nothing
// usable remains). Quoting is CSS syntax the face cache does not expect.
func cleanFamilyList(val string) string {
	parts := strings.Split(val, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '"' || p[0] == '\'') && p[len(p)-1] == p[0] {
			p = strings.TrimSpace(p[1 : len(p)-1])
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// applyFontWeight resolves font-weight into s.fontWeight (numeric, 100..900)
// and s.fontBold, inherited. parentWeight is the inherited numeric weight the
// relative "bolder"/"lighter" keywords step from — CSS defines them against
// the PARENT's computed weight, not against a boolean, which is why the
// numeric value is carried on Style at all.
//
// The bundled families ship regular and bold only, so the numeric weight
// collapses to a boolean at face-resolution time: >= 600 is bold. An invalid
// number (out of 1..1000, or unparseable) is ignored per SVG error handling.
func applyFontWeight(s *Style, parentWeight int, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-weight")
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	// Declared without an initializer: every switch arm either assigns w or
	// returns, so seeding it with parentWeight was dead (and misleading — the
	// bolder/lighter arms step from parentWeight explicitly).
	var w int
	switch val {
	case "normal":
		w = 400
	case "bold":
		w = 700
	case "bolder":
		w = stepWeight(parentWeight, +1)
	case "lighter":
		w = stepWeight(parentWeight, -1)
	default:
		n, ok := parseNumber(val)
		if !ok || n < 1 || n > 1000 {
			logf("svg: ignoring %s=%q: unparseable", "font-weight", val)
			return
		}
		w = int(n)
	}
	s.fontWeight = w
	s.fontBold = w >= boldWeightThreshold
}

// boldWeightThreshold is the numeric font-weight at or above which a run
// resolves to the bundled bold face. CSS 600 (semibold) is the conventional
// cut: the bundled families have no semibold, so 600 must round to bold
// rather than to regular (resvg's font-weight/650.svg asserts exactly this).
const boldWeightThreshold = 600

// stepWeight implements CSS font-weight's bolder/lighter as one step along
// the 100..900 ladder from the inherited weight, CLAMPED at both ends
// (bolder from 900 stays 900, lighter from 100 stays 100 — the corpus's
// bolder-with-clamping/lighter-with-clamping fixtures). CSS's full relative
// weight table is coarser than a strict ±100 step for the extremes; a single
// clamped 100-unit step matches it across the whole range the bundled faces
// can actually distinguish.
func stepWeight(w, dir int) int {
	w += dir * 100
	if w < 100 {
		return 100
	}
	if w > 900 {
		return 900
	}
	return w
}

// applyFontStyle resolves font-style (normal|italic|oblique) into
// s.fontItalic, inherited. "oblique" maps to the bundled italic face: no
// synthetic obliquing exists (see the repo's base-14 residuals note), and an
// italic face is a far closer match than upright.
func applyFontStyle(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-style")
	if !ok || val == "inherit" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "normal":
		s.fontItalic = false
	case "italic":
		s.fontItalic = true
	case "oblique":
		s.fontItalic = true
	default:
		logf("svg: ignoring %s=%q: unparseable", "font-style", val)
	}
}

// applyTextAnchor resolves text-anchor (start|middle|end), inherited. An
// unrecognized value is logged and ignored, keeping the inherited value —
// which is what the corpus's invalid-value-on-text.svg asserts.
func applyTextAnchor(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("text-anchor")
	if !ok || val == "inherit" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "start", "middle", "end":
		s.textAnchor = strings.ToLower(strings.TrimSpace(val))
	default:
		logf("svg: ignoring %s=%q: unparseable", "text-anchor", val)
	}
}

// applyDirection resolves direction (ltr|rtl), inherited: the base paragraph
// direction for bidi reordering.
func applyDirection(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("direction")
	if !ok || val == "inherit" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "ltr", "rtl":
		s.direction = strings.ToLower(strings.TrimSpace(val))
	default:
		logf("svg: ignoring %s=%q: unparseable", "direction", val)
	}
}

// applyUnicodeBidi resolves unicode-bidi (normal|embed|bidi-override),
// non-inherited per CSS. "embed" is accepted and behaves as "normal" here: a
// single <text> is one bidi paragraph, and an embedding that only restates
// the paragraph direction changes no glyph order. "bidi-override" is the one
// value that changes output — see Style.BidiOverride.
func applyUnicodeBidi(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("unicode-bidi")
	if !ok || val == "inherit" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "normal", "embed", "bidi-override":
		s.unicodeBidi = strings.ToLower(strings.TrimSpace(val))
	default:
		logf("svg: ignoring %s=%q: unparseable", "unicode-bidi", val)
	}
}

// FontFamily returns the element's resolved (inherited) font-family list, as
// a comma-separated string ready for layout/font.FaceCache.Resolve.
func (s Style) FontFamily() string { return s.fontFamily }

// FontSizePt returns the element's resolved (inherited) font-size in user
// units. It is never negative; zero is a legal value meaning "paint nothing".
func (s Style) FontSizePt() float64 { return s.fontSizePt }

// FontBold reports whether the element's resolved (inherited) font-weight
// selects the bold face (numeric weight >= 600).
func (s Style) FontBold() bool { return s.fontBold }

// FontItalic reports whether the element's resolved (inherited) font-style is
// italic or oblique.
func (s Style) FontItalic() bool { return s.fontItalic }

// TextAnchor returns the element's resolved (inherited) text-anchor:
// "start" (initial), "middle", or "end".
func (s Style) TextAnchor() string { return s.textAnchor }

// DirectionRTL reports whether the element's resolved (inherited) direction
// is rtl, i.e. whether bidi reordering uses an RTL base paragraph direction.
func (s Style) DirectionRTL() bool { return s.direction == "rtl" }

// BidiOverride reports whether unicode-bidi resolved (non-inherited) to
// "bidi-override", which forces every character into the base direction's
// order rather than letting the UAX#9 algorithm choose per character.
func (s Style) BidiOverride() bool { return s.unicodeBidi == "bidi-override" }

// applySpacing resolves letter-spacing or word-spacing into *dst, in user
// units. "normal" is the initial value and means zero extra spacing. em/ex/%
// resolve against sizePt, this element's OWN already-computed font-size —
// which is why the caller runs this after applyFontSize.
//
// Negative values are legal and meaningful (they tighten). An unparseable
// value is logged and ignored, keeping the inherited spacing, per SVG error
// handling. The magnitude is clamped to maxSpacingPt: a hostile
// letter-spacing="1e300" would otherwise push a glyph's pen position to a
// coordinate that overflows to Inf inside the rasterizer's edge list, and the
// resvg corpus itself labels its own letter-spacing="-10000" fixture
// "undefined behaviour", so clamping costs no legitimate document anything.
func applySpacing(name string, dst *float64, sizePt float64, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr(name)
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "normal" {
		*dst = 0
		return
	}
	v, ok := parseFontRelLength(val, sizePt)
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", name, val)
		return
	}
	*dst = clamp(v, -maxSpacingPt, maxSpacingPt)
}

// maxSpacingPt bounds the magnitude of a resolved letter-spacing,
// word-spacing, or baseline-shift, in user units. It is a DoS guard, not a
// fidelity choice: these values feed straight into a glyph's pen position, and
// an unbounded one lets a 200-byte document place geometry at 1e300, where the
// rasterizer's coordinate arithmetic overflows. A million user units is
// already far outside any viewport a document can meaningfully address.
const maxSpacingPt = 1e6

// parseFontRelLength parses a length whose FONT-RELATIVE units (em, ex, %)
// resolve against sizePt — the element's own computed font-size — rather than
// against parseLength's hardcoded 16px/8px UA defaults. Absolute units
// (px/pt/pc/mm/cm/in and a bare number) delegate to parseLength so the unit
// table stays in one place.
//
// It exists because every property added in this tranche (letter-spacing,
// word-spacing, baseline-shift, textLength) takes a length whose percentage
// and em basis is the current font-size, and reading them through parseLength
// would silently resolve "5%" against zero.
func parseFontRelLength(s string, sizePt float64) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	// splitLengthUnit only reports ok for a bare number or a font-relative
	// unit; an absolute unit ("42px") makes its parseNumber fail. So a false
	// ok here is NOT an error — it means "not font-relative", which is
	// precisely the case parseLength handles.
	if v, unit, ok := splitLengthUnit(s); ok {
		switch unit {
		case "em":
			return v * sizePt, true
		case "ex":
			// The same conventional 0.5em approximation applyFontSize uses: no
			// face is resolved at cascade time, so a real x-height is
			// unavailable here.
			return v * sizePt * 0.5, true
		case "%":
			return v / 100 * sizePt, true
		}
	}
	return parseLength(s, sizePt)
}

// baselineKeywords is the set of dominant-baseline/alignment-baseline values
// this engine recognizes, mapped to whether it can compute the keyword from
// Face.Metrics() alone.
//
// The false entries are the honest degradations: "ideographic" and
// "mathematical" are defined against the font's OS/2 and BASE tables, which
// pkg/font does not parse, so they resolve to "alphabetic" with a warn-once
// rather than being guessed at. "no-change" and "reset-size" were DEPRECATED
// in SVG 2 (the corpus labels reset-size "UB") and likewise degrade.
var baselineKeywords = map[string]bool{
	"auto":             true,
	"alphabetic":       true,
	"baseline":         true, // alignment-baseline's spelling of "alphabetic"
	"middle":           true,
	"central":          true,
	"hanging":          true,
	"text-before-edge": true,
	"text-after-edge":  true,
	"before-edge":      true, // alignment-baseline's shorter spellings
	"after-edge":       true,
	"ideographic":      false,
	"mathematical":     false,
	"no-change":        false,
	"reset-size":       false,
	"use-script":       false,
}

// applyDominantBaseline resolves dominant-baseline, inherited. A keyword this
// engine cannot compute from Face.Metrics() degrades to "auto" (the alphabetic
// baseline) rather than being approximated; an unrecognized value is ignored
// entirely, keeping the inherited one.
func applyDominantBaseline(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	// An explicit `inherit` does NOT count as writing the property: it only
	// restates what an ancestor already said, so it must not survive the
	// <text> boundary reset any more than the bare inherited value would.
	// resvg's dominant-baseline/inherit.svg is exactly that case, and renders
	// unshifted.
	if v, ok := attr("dominant-baseline"); ok && strings.ToLower(strings.TrimSpace(v)) != "inherit" {
		s.setDominantBaseline = true
	}
	applyBaselineKeyword("dominant-baseline", &s.dominantBaseline, s.parentDominantBaseline, attr, logf)
}

// applyAlignmentBaseline resolves alignment-baseline, NOT inherited (see
// Style.alignmentBaseline). Same keyword handling as dominant-baseline.
func applyAlignmentBaseline(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	applyBaselineKeyword("alignment-baseline", &s.alignmentBaseline, s.parentAlignmentBaseline, attr, logf)
}

// applyBaselineKeyword is the shared body of the two baseline-selection
// properties, which take the identical keyword set.
//
// Because neither property inherits, an explicit `inherit` is not a no-op the
// way it is everywhere else in this file: it has to reach back for the
// parent's value, which the caller supplies. resvg's
// alignment-baseline/inherit.svg is what makes the difference visible — the
// <g>'s "hanging" reaches the <text> ONLY through the explicit keyword.
func applyBaselineKeyword(name string, dst *string, parentVal string, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr(name)
	if !ok {
		return
	}
	if val == "inherit" {
		*dst = parentVal
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	computable, known := baselineKeywords[val]
	if !known {
		logf("svg: ignoring %s=%q: unparseable", name, val)
		return
	}
	if !computable {
		logf("svg: %s=%q needs OS/2 or BASE table metrics pkg/font does not parse; using the alphabetic baseline", name, val)
		*dst = "auto"
		return
	}
	*dst = val
}

// applyBaselineShift resolves baseline-shift into s.baselineShiftPt, in user
// units, positive = UP.
//
// It is the one property here that ACCUMULATES rather than replaces:
// parentShift is the shift already in effect, and a nested <tspan>'s own value
// adds to it (SVG2 §11.10.2 — a shift is measured from the baseline currently
// in effect, which a parent shift has already moved). resvg's nested-super and
// mixed-nested fixtures assert the sum; nested-with-baseline-1 asserts that
// the "baseline" keyword contributes ZERO without RESETTING the accumulation,
// so 20% + baseline + 20% still lands at 40%.
//
// "sub"/"super" use the conventional ∓0.2em / ±0.4em offsets — SVG defines
// them against the font's OS/2 subscript/superscript offsets, which pkg/font
// does not parse; the fractions are the widely-used substitutes and are the
// same shape pkg/layout/inline already uses for vertical-align: super.
// Percentages resolve against the element's own font-size, per spec.
func applyBaselineShift(s *Style, parentShift float64, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("baseline-shift")
	if !ok || val == "inherit" {
		// "inherit" keeps the accumulated parent shift, which s already
		// carries — resvg's inheritance-3.svg asserts exactly that (the
		// explicitly-inheriting <text> renders identically to the one whose
		// shift was ignored, because BOTH end up at zero).
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	var delta float64
	switch val {
	case "baseline":
		delta = 0
	case "sub":
		delta = -subSuperFraction * s.fontSizePt
	case "super":
		delta = superFraction * s.fontSizePt
	default:
		v, ok := parseFontRelLength(val, s.fontSizePt)
		if !ok {
			logf("svg: ignoring %s=%q: unparseable", "baseline-shift", val)
			return
		}
		delta = v
	}
	s.baselineShiftPt = clamp(parentShift+delta, -maxSpacingPt, maxSpacingPt)
}

// subSuperFraction and superFraction are the em fractions baseline-shift's
// "sub" and "super" keywords resolve to. SVG defines them against the font's
// OS/2 ySubscriptYOffset / ySuperscriptYOffset, which pkg/font does not parse;
// these are the conventional substitutes.
const (
	subSuperFraction = 0.2
	superFraction    = 0.4
)

// applyTextDecoration resolves text-decoration, capturing the DECLARING
// element's fully-resolved style alongside each line keyword.
//
// The property is written as if inherited, but what actually propagates in SVG
// is the decoration together with the paint and metrics of the element that
// declared it (SVG2 §11.11): a decoration declared on a <text> keeps painting
// in the <text>'s fill even over a differently-filled <tspan>. Carrying a
// captured *Style per declaration is what expresses that; a plain inherited
// keyword set could not.
//
// "none" clears every accumulated decoration for this subtree, which is how
// an author turns an ancestor's decoration off. Multiple keywords in one value
// ("underline overline line-through", in any order, comma- or space-separated)
// all apply. An unrecognized keyword is skipped with a log; recognized ones in
// the same value still apply, matching CSS's per-keyword tolerance for this
// property.
func applyTextDecoration(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("text-decoration")
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "none" {
		s.decorations = nil
		return
	}
	// The snapshot is taken ONCE per declaring element and shared by every
	// line it declares: they are the same element, so they have the same paint
	// and metrics.
	snapshot := *s
	snapshot.decorations = nil // no self-reference; the snapshot only supplies paint/metrics
	for _, f := range strings.FieldsFunc(val, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	}) {
		switch f {
		case "underline", "overline", "line-through":
			s.addDecoration(f, &snapshot)
		case "blink", "none":
			// "blink" has no static rendering; a stray "none" inside a list is
			// contradictory. Both are skipped silently rather than logged —
			// they are legal CSS, just inert here.
		default:
			logf("svg: ignoring text-decoration keyword %q: unsupported", f)
		}
	}
}

// addDecoration appends one declared line, replacing any same-line declaration
// already inherited from an ancestor: a <tspan text-decoration="underline">
// inside a <text text-decoration="underline"> must draw ONE underline, in the
// tspan's paint, not two (resvg's indirect-with-multiple-colors.svg, whose
// inner tspan3 re-declares the underline its parent already set).
//
// It always writes through a FRESH slice header rather than appending in
// place, so a child's decoration can never be seen by a sibling that shares
// the parent's backing array.
func (s *Style) addDecoration(line string, declaring *Style) {
	out := make([]decoration, 0, len(s.decorations)+1)
	for _, d := range s.decorations {
		if d.line != line {
			out = append(out, d)
		}
	}
	s.decorations = append(out, decoration{line: line, style: declaring})
}

// rebaseDecorations returns s with every accumulated decoration re-pointed at
// s itself, so each line keeps its identity but adopts s's paint and font
// metrics.
//
// It is called once, on the <text> element, to implement the rule that a
// decoration inherited from an ANCESTOR of the <text> paints in the <text>'s
// colours rather than the ancestor's — see buildText's call site for the two
// resvg fixtures that pin this. A decoration declared on the <text> itself is
// already anchored there, so rebasing it is a no-op, and a <tspan>'s own
// declaration happens later in the walk and is unaffected.
func (s Style) rebaseDecorations() Style {
	if len(s.decorations) == 0 {
		return s
	}
	self := s
	self.decorations = nil // the snapshot supplies paint/metrics only
	out := make([]decoration, len(s.decorations))
	for i, d := range s.decorations {
		out[i] = decoration{line: d.line, style: &self}
	}
	s.decorations = out
	return s
}

// resetBaselines returns s with the two baseline properties that must not
// cross the <text> boundary cleared. It is called once, on the <text> element,
// after that element's own attributes have already been applied.
//
//   - baselineShiftPt goes to zero unconditionally, so the accumulation begins
//     at zero and only a <tspan> inward can contribute. A shift written on the
//     <text> itself is therefore inert, which is what resvg's inheritance-1,
//     -3, -4 and -5 all assert (each overlays an unshifted red reference the
//     black text must exactly cover).
//   - dominantBaseline goes to "auto" only when the <text> did NOT write the
//     property itself. It propagates freely INSIDE a <text> subtree
//     (dummy-tspan.svg shifts a plain tspan along with its parent), but does
//     not arrive from a <g> above (inherit.svg wraps the <text> in a <g
//     dominant-baseline="middle"> and renders it unshifted — even though the
//     <text> writes dominant-baseline="inherit", which pulls "middle" in and
//     then has it discarded here).
//
// alignment-baseline is deliberately NOT reset: alignment-baseline/inherit.svg
// is the same shape and DOES shift, so the <g>'s value must survive when the
// <text> asks for it explicitly.
func (s Style) resetBaselines() Style {
	s.baselineShiftPt = 0
	if !s.setDominantBaseline {
		s.dominantBaseline = "auto"
	}
	return s
}

// applyFontStretch resolves font-stretch. Every value DEGRADES: the bundled
// families (see pkg/font/standard) ship regular/bold/italic only, with no
// condensed or expanded variant and no synthetic horizontal scaling anywhere
// in the engine, so there is nothing real a condensed request can resolve to.
// Rather than pretend — a synthetic squeeze would change advances and glyph
// shapes in a way no font designer sanctioned, and would silently diverge from
// every other renderer — the property is logged once and ignored, leaving the
// normal-width face.
//
// It still goes through the cascade (rather than being dropped at the parser)
// so the diagnostic fires for a style="" or sheet rule too, not only for a
// presentation attribute.
func applyFontStretch(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-stretch")
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "normal" {
		return // the only value the bundled faces can actually honor
	}
	s.fontStretchIgnored = true
	logf("svg: font-stretch=%q ignored: no condensed/expanded face is bundled and no synthetic stretching exists", val)
}

// applyFontVariant resolves font-variant. Like font-stretch it degrades:
// "small-caps" needs either a real small-caps face or the OpenType `smcp`
// feature, and neither the bundled families nor the shaping path
// (font-feature-settings is not plumbed through pkg/layout/inline) can supply
// one. "normal" is honored trivially; anything else is logged and ignored.
func applyFontVariant(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font-variant")
	if !ok || val == "inherit" {
		return
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "normal" {
		return
	}
	s.fontVariantIgnored = true
	logf("svg: font-variant=%q ignored: no small-caps face is bundled and OpenType features are not plumbed through the shaper", val)
}

// applyKerning resolves the SVG 1.1 `kerning` property and its SVG 2 / CSS
// successor `font-kerning`. Both only ever turn kerning OFF or adjust it, and
// this engine applies NO GPOS kerning-pair pass to simple scripts in the first
// place (complex scripts get kerning inside harfbuzz, which these properties
// cannot reach) — so there is nothing to disable and the properties are inert.
//
// The SVG 1.1 form additionally accepts a LENGTH, which would replace the
// inter-glyph spacing wholesale. That one is not inert in principle, but it was
// removed from SVG 2 and is unimplemented in every current browser and in
// resvg, so honoring it would make this engine the outlier. Logged and ignored.
func applyKerning(s *Style, attr func(string) (string, bool), logf func(string, ...any)) {
	for _, name := range [...]string{"kerning", "font-kerning"} {
		val, ok := attr(name)
		if !ok || val == "inherit" {
			continue
		}
		val = strings.ToLower(strings.TrimSpace(val))
		if val == "auto" || val == "normal" {
			continue // the default behavior, which is what already happens
		}
		s.kerningIgnored = true
		logf("svg: %s=%q ignored: no GPOS kerning-pair pass runs for simple scripts, so there is nothing to disable or override", name, val)
	}
}

// applyFontShorthand expands the CSS `font` shorthand into the longhands this
// engine reads: font-style, font-weight, font-size, and font-family. The CSS
// grammar is `[ style || variant || weight || stretch ]? size [ / line-height ]?
// family`, so the value is scanned left to right taking optional leading
// keywords, then a size (with an optional /line-height this engine has no use
// for), then everything remaining as the family list.
//
// A value that does not yield BOTH a size and a family is invalid per CSS and
// is ignored wholesale — no partial application, which is what keeps
// `font="bold"` from silently making text bold when the author wrote an
// incomplete shorthand.
//
// The system-font keywords (caption, icon, menu, message-box, small-caption,
// status-bar) name platform UI fonts this engine cannot resolve; they are
// logged and ignored.
func applyFontShorthand(s *Style, parentPt float64, parentWeight int, attr func(string) (string, bool), logf func(string, ...any)) {
	val, ok := attr("font")
	if !ok || val == "inherit" {
		return
	}
	val = strings.TrimSpace(val)
	switch strings.ToLower(val) {
	case "caption", "icon", "menu", "message-box", "small-caption", "status-bar":
		logf("svg: ignoring font=%q: system font keywords name platform UI fonts this engine cannot resolve", val)
		return
	}

	fields := strings.Fields(val)
	italic, bold := s.fontItalic, s.fontBold
	weight := parentWeight
	i := 0
	// Leading optional keywords, in any order.
	for ; i < len(fields); i++ {
		switch strings.ToLower(fields[i]) {
		case "normal":
			// Ambiguous across style/variant/weight/stretch; CSS says it sets
			// whichever slots are still unset, and all three of ours default
			// to the non-normal-free value already.
		case "italic", "oblique":
			italic = true
		case "small-caps":
			// Recorded only as a diagnostic: see applyFontVariant.
			s.fontVariantIgnored = true
			logf("svg: font shorthand's small-caps ignored: no small-caps face is bundled")
		case "bold":
			weight, bold = 700, true
		case "bolder":
			weight = stepWeight(parentWeight, +1)
			bold = weight >= boldWeightThreshold
		case "lighter":
			weight = stepWeight(parentWeight, -1)
			bold = weight >= boldWeightThreshold
		case "ultra-condensed", "extra-condensed", "condensed", "semi-condensed",
			"semi-expanded", "expanded", "extra-expanded", "ultra-expanded":
			s.fontStretchIgnored = true
			logf("svg: font shorthand's stretch keyword %q ignored: no condensed/expanded face is bundled", fields[i])
		default:
			goto size
		}
	}
size:
	if i >= len(fields) {
		logf("svg: ignoring font=%q: no font-size in the shorthand", val)
		return
	}
	// The size may carry a "/line-height" suffix, which this engine discards:
	// SVG text does not wrap, so there is no line box for it to size.
	sizeTok := fields[i]
	if slash := strings.IndexByte(sizeTok, '/'); slash >= 0 {
		sizeTok = sizeTok[:slash]
	}
	sizePt, ok := resolveFontSizeToken(sizeTok, parentPt)
	if !ok {
		logf("svg: ignoring font=%q: unparseable font-size %q", val, sizeTok)
		return
	}
	i++
	family := cleanFamilyList(strings.Join(fields[i:], " "))
	if family == "" {
		logf("svg: ignoring font=%q: no font-family in the shorthand", val)
		return
	}

	s.fontSizePt = sizePt
	s.fontFamily = family
	s.fontItalic = italic
	s.fontWeight = weight
	s.fontBold = bold
}

// resolveFontSizeToken resolves one font-size token (a keyword or a length)
// against parentPt, sharing applyFontSize's rules. It exists so the `font`
// shorthand and the `font-size` longhand cannot drift apart on which keywords
// and units they accept.
func resolveFontSizeToken(tok string, parentPt float64) (float64, bool) {
	tok = strings.ToLower(strings.TrimSpace(tok))
	if f, ok := absoluteFontSizes[tok]; ok {
		return defaultFontSizePt * f, true
	}
	if f, ok := relativeFontSizes[tok]; ok {
		return parentPt * f, true
	}
	v, ok := parseFontRelLength(tok, parentPt)
	if !ok || v < 0 {
		return 0, false
	}
	return v, true
}

// LetterSpacingPt returns the element's resolved (inherited) letter-spacing in
// user units. See Style.letterSpacingPt for why this exists in SVG only.
func (s Style) LetterSpacingPt() float64 { return s.letterSpacingPt }

// WordSpacingPt returns the element's resolved (inherited) word-spacing in
// user units.
func (s Style) WordSpacingPt() float64 { return s.wordSpacingPt }

// DominantBaseline returns the element's resolved (inherited)
// dominant-baseline keyword; "auto" means the alphabetic baseline.
func (s Style) DominantBaseline() string { return s.dominantBaseline }

// AlignmentBaseline returns the element's resolved (NON-inherited)
// alignment-baseline keyword; "auto" means "defer to dominant-baseline".
func (s Style) AlignmentBaseline() string { return s.alignmentBaseline }

// BaselineShiftPt returns the element's CUMULATIVE baseline shift in user
// units, positive = UP. See Style.baselineShiftPt.
func (s Style) BaselineShiftPt() float64 { return s.baselineShiftPt }

// FontStretchIgnored reports whether a non-normal font-stretch reached this
// element and was degraded. See Style.fontStretchIgnored.
func (s Style) FontStretchIgnored() bool { return s.fontStretchIgnored }

// FontVariantIgnored reports whether a non-normal font-variant reached this
// element and was degraded. See Style.fontVariantIgnored.
func (s Style) FontVariantIgnored() bool { return s.fontVariantIgnored }

// KerningIgnored reports whether a kerning or font-kerning value reached this
// element and was degraded. See Style.kerningIgnored.
func (s Style) KerningIgnored() bool { return s.kerningIgnored }
