package svg

import (
	"image/color"
	"strings"
)

// dropShadowChainLength is how many primitives one feDropShadow expands into:
// blur, offset, flood, composite, merge. The primitive cap is checked against
// this before expanding so a filter full of feDropShadow elements cannot slip
// five times its apparent primitive count past the bound.
const dropShadowChainLength = 5

// expandDropShadow lowers an <feDropShadow> into the equivalent primitive
// chain, per the SVG 2 definition of the shorthand:
//
//	feGaussianBlur  in=<in>       stdDeviation=<stdDeviation>  → blur
//	feOffset        in=blur       dx/dy=<dx>/<dy>              → offset
//	feFlood         flood-color/flood-opacity                  → flood
//	feComposite     in=flood in2=offset operator="in"          → shadow
//	feMerge         [shadow, <in>]                             → result
//
// Expanding rather than special-casing is deliberate: the shorthand then
// inherits the chain's behavior exactly (the blur's premultiplied box passes,
// the offset's fractional resampling, the flood's sRGB→working-space colour
// conversion, the composite's Porter-Duff `in`), instead of re-deriving all
// four and drifting from them.
//
// The composite's operator is `in`, NOT `atop` or a mask: the flood covers the
// whole subregion, and intersecting it with the OFFSET BLUR's alpha is what
// gives the shadow the source's silhouette. Using the un-offset blur there
// would produce a correctly-coloured shadow in the wrong place.
//
// base is the index the first expanded primitive will occupy, so the chain can
// wire its internal `in` references by absolute index — the same
// earlier-results-only invariant every hand-written graph obeys.
func expandDropShadow(el *element, ctx *cascadeCtx, in FilterInput, space FilterColorSpace, sub FilterPrimitive, base int) []FilterPrimitive {
	stdX, stdY := dropShadowStdDeviation(el)
	dx := plainNumberAttr(el, "dx", 2)
	dy := plainNumberAttr(el, "dy", 2)

	blurIdx := base
	offsetIdx := base + 1
	floodIdx := base + 2
	compositeIdx := base + 3

	// Only the FINAL primitive carries the shorthand's subregion. SVG clips a
	// primitive's output to its own subregion, and applying that clip at every
	// step of the expansion would trim the blur's falloff before the offset
	// could move it — the shadow would lose exactly the bleed the subregion
	// was sized to contain.
	blur := FilterPrimitive{Kind: PrimitiveGaussianBlur, In: in, Space: space, StdDevX: stdX, StdDevY: stdY}
	offset := FilterPrimitive{
		Kind:  PrimitiveOffset,
		In:    FilterInput{Kind: InputResult, Index: blurIdx},
		Space: space,
		Dx:    dx, Dy: dy,
	}
	flood := FilterPrimitive{
		Kind:       PrimitiveFlood,
		Space:      space,
		FloodColor: dropShadowColor(el, ctx),
	}
	shadow := FilterPrimitive{
		Kind:     PrimitiveComposite,
		In:       FilterInput{Kind: InputResult, Index: floodIdx},
		In2:      FilterInput{Kind: InputResult, Index: offsetIdx},
		Space:    space,
		Operator: CompositeIn,
	}
	merge := FilterPrimitive{
		Kind:  PrimitiveMerge,
		In:    FilterInput{Kind: InputResult, Index: compositeIdx},
		Space: space,
		MergeInputs: []FilterInput{
			{Kind: InputResult, Index: compositeIdx}, // shadow, underneath
			in,                                       // the source, on top
		},
		HasSubregion:     sub.HasSubregion,
		HasX:             sub.HasX,
		HasY:             sub.HasY,
		HasW:             sub.HasW,
		HasH:             sub.HasH,
		X:                sub.X,
		Y:                sub.Y,
		W:                sub.W,
		H:                sub.H,
		SubregionInvalid: sub.SubregionInvalid,
	}
	return []FilterPrimitive{blur, offset, flood, shadow, merge}
}

// dropShadowStdDeviation reads feDropShadow's stdDeviation, which shares
// feGaussianBlur's grammar but a DIFFERENT default: 2, not 0. A shorthand
// whose whole purpose is a soft shadow defaults to a soft one, and the
// corpus's with-offset fixture relies on it.
func dropShadowStdDeviation(el *element) (x, y float64) {
	v, ok := el.attrs["stdDeviation"]
	if !ok {
		return 2, 2
	}
	nums := parseNumberList(v)
	if len(nums) == 0 || len(nums) > 2 {
		return 2, 2
	}
	x = nums[0]
	y = x
	if len(nums) == 2 {
		y = nums[1]
	}
	if x < 0 || y < 0 {
		return 2, 2
	}
	return x, y
}

// dropShadowColor resolves feDropShadow's shadow colour.
//
// It is flood-color/flood-opacity, exactly as feFlood resolves them, but with
// a different INITIAL value: SVG 2 gives feDropShadow's flood-color an initial
// of `black` with the shadow otherwise following the element's own colour
// handling, and the corpus's with-flood-color, with-flood-opacity and
// hsla-color fixtures all exercise the same resolution feFlood uses. Sharing
// resolveFloodColor is what keeps the two from disagreeing about
// currentColor, `inherit`, and an alpha-carrying colour composing
// multiplicatively with flood-opacity.
func dropShadowColor(el *element, ctx *cascadeCtx) color.RGBA {
	return resolveFloodColor(el, ctx)
}

// isSVGFilterFunctionList reports whether a `filter` property value needs the
// CSS filter-function lowering, rather than the ordinary single-<filter> path.
//
// A LONE url() takes the <filter> path, which is not merely an optimisation:
// the two paths handle an unresolvable reference OPPOSITELY. A bare
// filter="url(#missing)" makes the element NOT RENDER (SVG's error handling for
// a filter reference), while the same url() inside a list is simply DROPPED and
// the rest of the list still applies. Routing a lone url() through the list
// path would silently turn the first case into the second.
func isSVGFilterFunctionList(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "none" {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(v), "url(") {
		return true
	}
	// A url() with anything after it is a list.
	end, ok := balancedEnd(v, strings.Index(v, "("))
	if !ok {
		return false
	}
	return strings.TrimSpace(v[end:]) != ""
}

// balancedEnd returns the index just past the ')' matching the '(' at open,
// counting nesting so a url() containing parentheses does not end early.
func balancedEnd(s string, open int) (end int, ok bool) {
	if open < 0 {
		return 0, false
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
