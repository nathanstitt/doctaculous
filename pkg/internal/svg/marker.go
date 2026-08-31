package svg

import (
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// maxMarkerChainDepth bounds a chain of marker="url(#...)" references (a
// marker's own content drawing a shape that itself carries a marker-*
// property, and so on), independent of the cycle guard in resolveMarker: a
// cycle is caught in O(1) hops via sceneBuilder.buildingMarker, but this
// instead guards a long ACYCLIC chain — 64 DISTINCT marker ids each
// referencing the next — from costing unbounded recursion depth, mirroring
// maxClipPathChainDepth/maxMaskChainDepth's rationale exactly.
//
// The depth is counted on sceneBuilder.markerDepth rather than passed down
// as a parameter, because a nested marker reference re-enters through the
// ordinary scene walk (resolveMarker -> buildKidsGroup -> buildNode ->
// resolveMarkerRef) which has no depth argument to thread.
const maxMarkerChainDepth = 64

// markerableElements are the SVG-namespace element local names a
// marker-start/-mid/-end/marker property actually paints on, per SVG2
// §11.6.7: "path, line, polyline and polygon elements". A <circle>/<rect>
// (rounded or not) never gets markers even though shapePath happily
// produces a *render.Path for them — the corpus's marker-on-circle.svg,
// marker-on-rect.svg, and marker-on-rounded-rect.svg all assert NO markers
// are drawn — so this check must key off the ELEMENT NAME, before
// shapePath has flattened everything into indistinguishable geometry, not
// off any property of the resulting Path.
var markerableElements = map[string]bool{
	"path":     true,
	"line":     true,
	"polyline": true,
	"polygon":  true,
}

// MarkerOrient selects how a Marker's content is rotated at each vertex it
// paints: a fixed angle, or one of the two auto modes that follow the
// path's own tangent (SVG2 §11.6.7).
type MarkerOrient struct {
	// Auto is true for orient="auto" or orient="auto-start-reverse": the
	// marker's rotation is computed per vertex from the path's tangent
	// (Reversed additionally flips marker-start by 180°), rather than fixed.
	Auto bool
	// Reversed is true only for orient="auto-start-reverse", and only takes
	// effect at the path's marker-start vertex (SVG2: "the effect is as if
	// the marker were rotated 180 degrees from its natural direction ...
	// this attribute value has no effect on marker-mid or marker-end").
	Reversed bool
	// Angle is the fixed rotation in radians, meaningful only when Auto is
	// false: a parsed orient="<angle>" value, or 0 for an absent/invalid
	// orient (SVG's lacuna value).
	Angle float64
}

// Marker is a resolved <marker> element, ready to paint at a path vertex. It
// is built once per referenced id (see sceneBuilder.resolveMarkerRef) and
// shared by every marker-start/-mid/-end reference to the same id, since a
// <marker>'s own content resolution is idempotent (no per-referencer style
// dependency — unlike <use>, nothing about a <marker>'s rendered content
// depends on which shape is pointing at it, only its own attributes and
// document-order-fixed content).
type Marker struct {
	// RefX, RefY are the point (in the marker's own content coordinate
	// system, i.e. AFTER ViewBox's mapping if present) that lands exactly on
	// the path vertex being marked — SVG2 §11.6.7's "reference point".
	RefX, RefY float64

	// Width, Height are markerWidth/markerHeight (default 3), the size of
	// the marker's clipped viewport in strokeWidth or user units, per Units.
	Width, Height float64

	// Units selects how Width/Height/the whole marker scale against the
	// referencing element: true for the default "strokeWidth" (multiply by
	// the target's resolved stroke-width), false for "userSpaceOnUse"
	// (Width/Height are already absolute user-unit lengths, no
	// stroke-width scaling).
	UnitsStrokeWidth bool

	// Orient selects the per-vertex or fixed rotation (see MarkerOrient).
	Orient MarkerOrient

	// ViewBox is the marker's own viewBox->[0,Width]x[0,Height] mapping, or
	// render.Identity when the marker has no viewBox attribute (the common
	// case — content coordinates ARE marker-viewport coordinates then).
	ViewBoxM render.Matrix

	// Kids is the marker's rendered content, built via the ordinary
	// buildGroup/buildNode machinery (a <marker> may contain any paintable
	// content), exactly like a <mask>'s Kids. A nil/empty Kids (an empty
	// <marker>, or one whose only children resolve to nothing — see
	// empty.svg/invalid-child.svg) paints nothing, which is simply the
	// ordinary "Group with no Kids" no-op, not a distinguished case the way
	// an empty <mask> is.
	Kids *Group

	// ClipToViewport clips Kids to the [0,Width]x[0,Height] marker viewport
	// rect. It is true for the overwhelming majority of markers, since SVG2
	// gives a <marker> the initial overflow "hidden" — the opposite default
	// from most SVG elements — and false only when the marker's own
	// resolved overflow is "visible"/"scroll" (see the resvg "nested.svg"
	// fixture's marker2, which sets overflow="visible"). The value comes
	// from the marker's own CASCADED style (Style.WantsViewportClip), so
	// style="overflow:visible" and an `overflow` stylesheet rule work, not
	// just the presentation attribute; <symbol> resolves the identical
	// default the same way through wantsViewportClip in use.go.
	ClipToViewport bool
}

// resolveMarkerRef resolves a marker-start/-mid/-end property's raw value
// (as recorded by Style.MarkerStartRef/MarkerMidRef/MarkerEndRef) into a
// *Marker, or nil for "none", an invalid FuncIRI, or an id that does not
// resolve to a <marker> element.
func (b *sceneBuilder) resolveMarkerRef(ref string) *Marker {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "url(") {
		// Not a url() reference at all: unresolvable, so "no marker" per
		// SVG's error-handling model.
		return nil
	}
	id, _, ok := parsePaintServerRef(ref)
	if !ok {
		b.warnOnceMsg("svg-marker-bad-funciri", "svg: ignoring marker reference: unparseable url() reference")
		return nil
	}
	fragID, ok := fragmentID(id)
	if !ok {
		return nil
	}
	return b.resolveMarker(fragID)
}

// resolveMarker resolves id against the document index into a *Marker,
// memoizing by id (see sceneBuilder.markerMemo) and guarding against a
// self-referencing or cyclic marker chain via buildingMarker (mirrors
// buildingClip/buildingMask exactly). sceneBuilder.markerDepth additionally
// bounds an acyclic chain via maxMarkerChainDepth, independent of the cycle
// guard.
//
// Returns nil when: id is not present in the document index, the element it
// names is not a <marker>, a cycle or excessive chain depth is detected, or
// the marker's own width/height resolve to zero/negative (SVG: a marker
// with zero-or-negative markerWidth/markerHeight is not rendered — see
// zero-sized.svg and marker-with-a-negative-size.svg).
//
// KNOWN DIVERGENCE (memoization vs. viewport): the memo is keyed by id
// alone, but a percentage markerWidth/markerHeight/refX/refY resolves
// against b.vp — whichever viewport happened to be current at the FIRST
// resolution of that id. A percentage-sized marker first reached from
// inside another <marker> or <symbol> (which each install their own
// viewport) therefore caches at that inner scale and is reused at that
// scale by every later root-level reference. resolveMask has the identical
// flaw. Keying by (id, viewport) would fix it; it is recorded rather than
// fixed here because no fixture in the corpus exercises the combination
// (percent-values.svg uses percentages only at the root viewport), and
// changing the memo key is a change to shared clip/mask/marker memo
// discipline that belongs with a fix to all three at once.
//
// KNOWN DIVERGENCE (memoization vs. depth cap): when the cap below truncates
// a chain, the guard returns nil without memoizing, but the partially-built
// parent IS memoized, so a later reference to that parent from a shallower
// point reuses the truncated build instead of resolving it in full.
// resolveMask and resolveClipPath have the identical shape; it only manifests
// past a maxMarkerChainDepth-deep chain, which no real document reaches.
func (b *sceneBuilder) resolveMarker(id string) *Marker {
	if m, ok := b.markerMemo[id]; ok {
		return m
	}
	if b.markerDepth >= maxMarkerChainDepth || b.buildingMarker[id] {
		return nil
	}
	el, ok := b.idx.ids[id]
	if !ok || el.space != svgNS || el.local != "marker" {
		return nil
	}

	width := gradientCoord(el.attrs, "markerWidth", 3, true, b.vp.w)
	height := gradientCoord(el.attrs, "markerHeight", 3, true, b.vp.h)
	if width <= 0 || height <= 0 {
		return nil
	}

	b.buildingMarker[id] = true
	defer delete(b.buildingMarker, id)

	refX := gradientCoord(el.attrs, "refX", 0, true, b.vp.w)
	refY := gradientCoord(el.attrs, "refY", 0, true, b.vp.h)

	vm := render.Identity
	if vbAttr, ok := el.attrs["viewBox"]; ok {
		if vb, ok := parseViewBox(vbAttr); ok {
			vm = viewBoxMatrix(vb, width, height, el.attrs["preserveAspectRatio"])
			// refX/refY are specified in the viewBox's own coordinate system
			// when a viewBox is present (SVG2 §11.6.7: "the reference point
			// ... represented in the coordinate system after the
			// application of the ‘viewBox’ ... attribute"), so map them
			// through vm before landing them on the path vertex.
			refX, refY = vm.Apply(refX, refY)
		} else {
			b.logf("svg: ignoring viewBox=%q on marker %q: unparseable or non-positive extent", vbAttr, id)
		}
	}

	// A <marker> is not part of the ordinary parentStyle-threaded scene walk
	// (buildNode never reaches it directly — see skippedElements), but its
	// content IS ordinary paintable content that inherits from the
	// <marker>'s own position in the DOM, exactly like a <mask>'s content —
	// see resolveMask's identical inheritedStyleFor use.
	ctx := &cascadeCtx{idx: b.idx, logf: b.logf}
	inherited := b.inheritedStyleFor(el, ctx)
	// The <marker> element's OWN presentation attributes and style are then
	// cascaded on top, so its children inherit them: <marker fill="blue">
	// makes an unstyled child path blue, matching browsers. Passing the
	// ancestors-only `inherited` straight through (as resolveMask still
	// does) would drop them. selfStyle also drives overflow, which is a
	// CSS property — style="overflow:visible" must work, not just the
	// presentation attribute.
	selfStyle := inherited.apply(el, ctx)

	m := &Marker{
		RefX:             refX,
		RefY:             refY,
		Width:            width,
		Height:           height,
		UnitsStrokeWidth: markerUnitsStrokeWidth(el),
		Orient:           markerOrient(el, b.logf),
		ViewBoxM:         vm,
		ClipToViewport:   selfStyle.WantsViewportClip(),
	}

	// A <marker>'s own viewport (for a percentage inside its content, or a
	// nested marker reference's userSpaceOnUse resolution) is the marker's
	// OWN width/height in the SAME way <symbol> establishes one — see
	// buildSymbolInstance's identical b.vp save/restore discipline.
	saved := b.vp
	b.vp = viewport{w: width, h: height}
	// markerDepth bounds an acyclic marker chain; it must wrap exactly the
	// content build, since that is the only path by which a nested
	// marker-* property can re-enter resolveMarker.
	b.markerDepth++
	m.Kids = b.buildKidsGroup(el.kids, selfStyle, ctx)
	b.markerDepth--
	b.vp = saved

	b.markerMemo[id] = m
	return m
}

// markerUnitsStrokeWidth resolves a <marker>'s markerUnits attribute
// (strokeWidth|userSpaceOnUse), defaulting to strokeWidth per SVG — and
// falling back to that SAME default for any unrecognized value (see
// with-invalid-markerUnits.svg, which asserts markerUnits="qwe" behaves
// exactly like the attribute were absent).
func markerUnitsStrokeWidth(el *element) bool {
	return el.attrs["markerUnits"] != "userSpaceOnUse"
}

// markerOrient resolves a <marker>'s orient attribute: "auto",
// "auto-start-reverse", a parsed <angle> (parseAngle handles the
// deg/grad/rad/turn units plus SVG's bare-number-means-degrees form), or the
// absent/unparseable lacuna value 0 (a fixed, unrotated marker) — mirroring
// every other enum-or-fallback attribute in this package's error handling
// (log once, keep the safe default) rather than propagating a parse failure.
func markerOrient(el *element, logf func(string, ...any)) MarkerOrient {
	val, ok := el.attrs["orient"]
	if !ok {
		return MarkerOrient{}
	}
	val = strings.TrimSpace(val)
	switch val {
	case "auto":
		return MarkerOrient{Auto: true}
	case "auto-start-reverse":
		return MarkerOrient{Auto: true, Reversed: true}
	}
	angle, ok := parseAngle(val)
	if !ok {
		logf("svg: ignoring orient=%q on marker: unparseable", val)
		return MarkerOrient{}
	}
	return MarkerOrient{Angle: angle}
}
