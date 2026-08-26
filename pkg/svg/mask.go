package svg

import (
	"strings"
)

// maxMaskChainDepth bounds a chain of mask="url(#...)" references (an
// element masked by mask A, whose own mask attribute refers to mask B, and
// so on), independent of the cycle guard in resolveMaskRef: a cycle is
// caught in O(1) hops via sceneBuilder.buildingMask, but this instead guards
// a long ACYCLIC chain from costing unbounded recursion depth, mirroring
// maxClipPathChainDepth's rationale exactly.
const maxMaskChainDepth = 64

// MaskType selects how a Mask's rendered content converts to per-pixel
// coverage: Luminance (the default) reads the sRGB luminance of each rendered
// pixel, Alpha reads the pixel's own alpha channel directly. See
// render.Device.BuildLuminanceMask's doc comment for the exact formula.
type MaskType int

const (
	// MaskLuminance is the SVG default: coverage is 0.2126*R + 0.7152*G +
	// 0.0722*B (sRGB coefficients on sRGB values, per this engine's decision
	// to follow browsers/SVG2/resvg rather than SVG 1.1's linearRGB), times
	// the pixel's own alpha.
	MaskLuminance MaskType = iota
	// MaskAlpha is the SVG2 mask-type="alpha" value: coverage is the
	// rendered pixel's own alpha channel, ignoring color entirely.
	MaskAlpha
)

// Mask is a resolved <mask> element, ready to render offscreen and convert
// into a render.GroupMask. It is built once per referenced id (see
// sceneBuilder.resolveMaskRef) and shared by every element that references
// the same mask, since Document must stay read-only and side-table-free
// after Parse.
//
// A <mask> element's OWN transform attribute is deliberately not carried
// here: per SVG, a transform on the <mask> element itself has no effect
// (only a transform on the MASKED element applies) — see the resvg
// transform-has-no-effect fixture.
type Mask struct {
	// Units is maskUnits: "objectBoundingBox" (the default) or
	// "userSpaceOnUse". It maps RegionX/Y/W/H (the mask REGION rect,
	// outside which content is fully masked out) into the masked element's
	// user space.
	Units string

	// ContentUnits is maskContentUnits: "userSpaceOnUse" (the default —
	// the OPPOSITE default from Units) or "objectBoundingBox". It maps
	// Kids' own geometry into the masked element's user space, exactly like
	// ClipPath.Units does for a <clipPath>'s children.
	ContentUnits string

	// RegionX, RegionY, RegionW, RegionH are the maskUnits-relative mask
	// region rect (x/y/width/height attributes), defaulting to -10%, -10%,
	// 120%, 120% per SVG — a mask region deliberately bleeds 10% past the
	// masked element's own bounding box on each side. In objectBoundingBox
	// units these are fractions of the bbox (e.g. -0.1); in userSpaceOnUse
	// they are already resolved to user-unit lengths (percentages resolved
	// against the ambient viewport at Parse time — see resolveMask).
	RegionX, RegionY, RegionW, RegionH float64

	// Type selects the luminance-vs-alpha conversion (mask-type, SVG2).
	Type MaskType

	// Kids is the mask's rendered content, built via the ordinary
	// buildGroup/buildNode machinery (a <mask> may contain any paintable
	// content — shapes, groups, gradients, patterns — unlike a <clipPath>'s
	// shape-only allowlist), so pkg/svg/draw paints it exactly like any
	// other Group. A nil/empty Kids (an empty <mask>, or one whose only
	// children resolve to nothing) means the mask has NO content, which per
	// SVG makes its target FULLY TRANSPARENT — the caller must distinguish
	// this from "mask absent" (a nil *Mask), never treat empty Kids as "no
	// masking".
	Kids *Group

	// Self is this <mask> element's OWN mask="url(#...)" reference, if any:
	// the rendered content mask is additionally intersected (multiplied)
	// with Self's resolved mask. nil means no additional restriction. See
	// pkg/svg/draw's buildMask for how this composes.
	Self *Mask
}

// resolveMaskRef resolves a mask property's raw value (as recorded by
// Style.MaskRef) into a *Mask, or nil for "none", an invalid FuncIRI, or an
// id that does not resolve to a <mask> element.
func (b *sceneBuilder) resolveMaskRef(ref string) *Mask {
	return b.resolveMaskRefAt(ref, 0)
}

// resolveMaskRefAt is resolveMaskRef with an explicit chain depth, for a
// mask reference discovered while already resolving another mask (its own
// self-reference).
func (b *sceneBuilder) resolveMaskRefAt(ref string, depth int) *Mask {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "url(") {
		// Not a url() reference at all (some other invalid value, e.g. a bare
		// keyword other than "none"): unresolvable, so "no masking" per SVG's
		// error-handling model.
		return nil
	}
	id, _, ok := parsePaintServerRef(ref)
	if !ok {
		b.warnOnceMsg("svg-mask-bad-funciri", "svg: ignoring mask: unparseable url() reference")
		return nil
	}
	fragID, ok := fragmentID(id)
	if !ok {
		return nil
	}
	return b.resolveMask(fragID, depth)
}

// resolveMask resolves id against the document index into a *Mask,
// memoizing by id (see sceneBuilder.maskMemo) and guarding against a
// self-referencing or cyclic mask chain via buildingMask (mirrors
// buildingClip exactly — see that field's doc comment). depth bounds an
// acyclic chain via maxMaskChainDepth, independent of the cycle guard.
//
// Returns nil when: id is not present in the document index, the element it
// names is not a <mask>, a cycle or excessive chain depth is detected.
func (b *sceneBuilder) resolveMask(id string, depth int) *Mask {
	if m, ok := b.maskMemo[id]; ok {
		return m
	}
	if depth >= maxMaskChainDepth || b.buildingMask[id] {
		return nil
	}
	el, ok := b.idx.ids[id]
	if !ok || el.space != svgNS || el.local != "mask" {
		return nil
	}

	b.buildingMask[id] = true
	defer delete(b.buildingMask, id)

	ctx := &cascadeCtx{idx: b.idx, logf: b.logf}
	inherited := b.inheritedStyleFor(el, ctx)
	selfStyle := inherited.apply(el, ctx)

	m := &Mask{
		Units:        maskUnits(el),
		ContentUnits: maskContentUnits(el),
		Type:         maskType(selfStyle),
	}
	m.RegionX, m.RegionY, m.RegionW, m.RegionH = maskRegion(el, m.Units, b.vp)

	if ref, ok := selfStyle.MaskRef(); ok {
		m.Self = b.resolveMaskRefAt(ref, depth+1)
	}

	// A <mask>'s content is not part of the ordinary scene tree (a <mask>
	// contributes no scene nodes of its own — see skippedElements), but its
	// children ARE ordinary paintable content (any element, not a
	// clipPath's shape-only allowlist), built through the same
	// buildKidsGroup machinery a <pattern> tile uses. inherited style
	// (already computed above for MaskRef) is the parent style each child
	// cascades from, exactly like inheritedStyleFor's clipPath use.
	tile := b.buildKidsGroup(el.kids, inherited, ctx)
	m.Kids = tile

	b.maskMemo[id] = m
	return m
}

// maskUnits resolves a <mask>'s maskUnits attribute
// (objectBoundingBox|userSpaceOnUse), defaulting to objectBoundingBox per
// SVG (note this is the OPPOSITE default from maskContentUnits, a classic
// source of bugs the design doc calls out explicitly).
func maskUnits(el *element) string {
	if el.attrs["maskUnits"] == "userSpaceOnUse" {
		return "userSpaceOnUse"
	}
	return "objectBoundingBox"
}

// maskContentUnits resolves a <mask>'s maskContentUnits attribute
// (userSpaceOnUse|objectBoundingBox), defaulting to userSpaceOnUse per SVG —
// the opposite default from maskUnits.
func maskContentUnits(el *element) string {
	if el.attrs["maskContentUnits"] == "objectBoundingBox" {
		return "objectBoundingBox"
	}
	return "userSpaceOnUse"
}

// maskType resolves mask-type (SVG2: luminance|alpha), defaulting to
// luminance. It is read from the cascaded style (selfStyle) rather than a
// raw attribute lookup so `style="mask-type:alpha"` and a `mask-type`
// stylesheet rule both work, exactly like every other presentation
// property — see Style.MaskTypeValue.
func maskType(selfStyle Style) MaskType {
	if selfStyle.MaskTypeValue() == "alpha" {
		return MaskAlpha
	}
	return MaskLuminance
}

// maskRegion resolves a <mask>'s x/y/width/height attributes (the mask
// REGION rect) against SVG's defaults: -10%, -10%, 120%, 120%. In
// objectBoundingBox units (the default) these resolve to plain fractions of
// the masked element's bounding box (-0.1, -0.1, 1.2, 1.2); in
// userSpaceOnUse they resolve to absolute user-unit lengths, with a
// percentage value resolved against vp (the ambient viewport) exactly like
// a gradient's userSpaceOnUse percentage coordinates (see
// gradientCoord/resolveGradient).
func maskRegion(el *element, units string, vp viewport) (x, y, w, h float64) {
	userSpace := units == "userSpaceOnUse"
	if userSpace {
		x = gradientCoord(el.attrs, "x", -0.1*vp.w, true, vp.w)
		y = gradientCoord(el.attrs, "y", -0.1*vp.h, true, vp.h)
		w = gradientCoord(el.attrs, "width", 1.2*vp.w, true, vp.w)
		h = gradientCoord(el.attrs, "height", 1.2*vp.h, true, vp.h)
		return x, y, w, h
	}
	x = gradientCoord(el.attrs, "x", -0.1, false, 0)
	y = gradientCoord(el.attrs, "y", -0.1, false, 0)
	w = gradientCoord(el.attrs, "width", 1.2, false, 0)
	h = gradientCoord(el.attrs, "height", 1.2, false, 0)
	return x, y, w, h
}

