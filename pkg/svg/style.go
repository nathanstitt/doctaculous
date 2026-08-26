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
	hasFill     bool // false: fill="none" (or an unsupported url() ref)
	fill        color.RGBA
	fillOpacity float64
	fillRule    render.FillRule

	hasStroke     bool
	stroke        color.RGBA
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
	}
}

// apply returns parent overridden by el's resolved style. ctx supplies the
// cascade: with a nil ctx (or one built from a document with no stylesheets
// or style="" attributes anywhere) attr resolution falls back to el's
// presentation attributes alone, exactly PR 1's behavior — every one of PR
// 1's 148 golden fixtures uses no CSS and so exercises only this path. ctx's
// logf receives a debug line for a url() paint reference (gradients land in
// a later PR) and for any attribute value that fails to parse — the
// attribute is then ignored, per SVG's error-handling model, and the
// inherited value is kept. opacity is not inherited: it resets to el's own
// value (default 1) on every call. Every other listed property is inherited.
func (parent Style) apply(el *element, ctx *cascadeCtx) Style {
	s := parent
	s.opacity = 1 // not inherited; may be overridden below

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

	// 'color' must resolve before fill/stroke so currentColor sees the
	// element's own (already-updated) color, not the parent's.
	applyColorProp(&s, attr, logf)

	applyPaint("fill", &s.hasFill, &s.fill, s.color, attr, logf)
	applyOpacityProp("fill-opacity", &s.fillOpacity, attr, logf)
	applyFillRule(&s, attr, logf)

	applyPaint("stroke", &s.hasStroke, &s.stroke, s.color, attr, logf)
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
// *has, "currentColor" resolves against cur, "url(...)" degrades to none
// plus a log line (paint servers land in a later PR), "inherit" keeps the
// parent's value, and anything else is parsed as a color or logged and
// ignored.
func applyPaint(name string, has *bool, c *color.RGBA, cur color.RGBA, attr func(string) (string, bool), logf func(string, ...any)) {
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
		return
	case val == "currentColor":
		*has = true
		*c = cur
		return
	case strings.HasPrefix(strings.ToLower(val), "url("):
		logf("svg: ignoring %s=%q: paint servers are not supported", name, val)
		*has = false
		return
	}
	parsed, ok := parseColorValue(val)
	if !ok {
		logf("svg: ignoring %s=%q: unparseable", name, val)
		return
	}
	*has = true
	*c = parsed
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

// FillPaint returns the element's composed fill paint (color with
// fill-opacity folded into alpha, plus the fill rule). ok is false when
// there is no fill to paint: fill="none", an unsupported url() paint-server
// reference (which applyPaint already degrades to hasFill=false), or the
// element is invisible (visibility:hidden). display:none is not checked
// here — the scene builder never reaches this far for a display:none
// subtree; see the tree-walker's subtree skip.
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
// an unsupported url() reference, the element is invisible, or the resolved
// stroke-width is <= 0 (a zero-width stroke paints nothing, per SVG).
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

// Opacity returns the element's own (non-inherited) opacity in [0,1].
func (s Style) Opacity() float64 {
	return s.opacity
}
