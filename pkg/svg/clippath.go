package svg

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// maxClipPathChainDepth bounds a chain of clip-path="url(#...)" references
// (a shape clipped by clipPath A, whose own clip-path refers to clipPath B,
// and so on) independent of the cycle guard in resolveClipPathRef: a cycle
// is caught in O(1) hops via sceneBuilder.buildingClip, but this instead
// guards a long ACYCLIC chain from costing unbounded recursion depth,
// mirroring maxHrefChainDepth's rationale for paint servers.
const maxClipPathChainDepth = 64

// ClipPath is a resolved <clipPath> element, ready to intersect against
// whatever it clips. It is built once per referenced id (see
// sceneBuilder.resolveClipPathRef) and shared by every element that
// references the same clipPath, since Document must stay read-only and
// side-table-free after Parse.
//
// Units selects how a referencing element's bounding box maps ClipPath's
// (and its children's) geometry into that element's user space:
// "userSpaceOnUse" (the default — geometry is already in the referencing
// element's user space, only ClipPath's own M applies) or
// "objectBoundingBox" (geometry is defined in the unit [0,1]x[0,1] box of
// the referencing element's bounding box, exactly like a gradient's
// objectBoundingBox — see resolveGradient's bboxM for the identical
// composition this mirrors). pkg/svg/draw resolves this against the
// specific shape/group being clipped, since only the draw-time caller knows
// that target's bounding box.
type ClipPath struct {
	// M is the <clipPath> element's own transform attribute, applied to
	// every child on top of Units' bbox-or-identity mapping.
	M render.Matrix

	// Units is "userSpaceOnUse" (default) or "objectBoundingBox".
	Units string

	// Kids is the union: every valid child contributes its own region,
	// unioned together (NOT intersected — see render.Device.BuildClipMask's
	// doc comment on why a union must flatten to a mask rather than being
	// expressed as repeated PushClip calls). An empty Kids means the
	// <clipPath> had no valid children at all, which clips its target to
	// NOTHING (not "no clip") per SVG.
	Kids []ClipPathChild

	// Self is this <clipPath> element's OWN clip-path="url(#...)"
	// reference, if any: the whole union of Kids is additionally
	// intersected with Self's resolved region (see pkg/svg/draw, which
	// applies Self as an additional PushClip-equivalent restriction after
	// building the Kids union mask). nil means no additional restriction.
	Self *ClipPath
}

// ClipPathChild is one <clipPath> child's contribution to the union: a
// shape's geometry (pre-transform, in the child element's own local space —
// mirrors Shape.Path), the child's own transform, and the child's own
// clip-rule (inherited, but resolved per-child since a mixed-clip-rule
// clipPath is a real corpus case — see Style.ClipRule). fill, stroke,
// opacity, filter, and mask on a clipPath child have NO effect (per SVG)
// and are simply never read here.
type ClipPathChild struct {
	Path *render.Path
	M    render.Matrix
	Rule render.FillRule

	// Self is the child's OWN clip-path="url(#...)" reference: per SVG, a
	// clipPath child's own clip-path intersects that child's region BEFORE
	// it joins the union (a child clipped to nothing contributes nothing to
	// the union, rather than the union being intersected with it
	// afterward — those differ whenever there is more than one child).
	Self *ClipPath
}

// clipPathChildKinds are the SVG-namespace element types buildClipChild
// accepts as <clipPath> children: the basic shapes, plus <text> and <use>
// (not yet implemented as of this task — see the design's "out of scope"
// list — so they never actually contribute a Path today, but are named here
// so a later PR can slot them in additively without restructuring this
// allowlist). This is DELIBERATELY separate from buildNode's forgiving
// "unknown element becomes a container" default: a <clipPath> must not
// recurse into an invalid child (a <g>, <image>, <switch>, or any other
// element not named here) as if it were a plain container — such a child is
// simply dropped, per SVG's explicit restriction of clipPath content to
// shapes/text/use.
var clipPathChildKinds = map[string]bool{
	"rect":     true,
	"circle":   true,
	"ellipse":  true,
	"line":     true,
	"polyline": true,
	"polygon":  true,
	"path":     true,
	"text":     true, // not yet implemented (PR 6): contributes no geometry today
	"use":      true, // not yet implemented (PR 5): contributes no geometry today
}

// resolveClipPathRef resolves a clip-path property's raw value (as recorded
// by Style.ClipPathRef) into a *ClipPath, or nil for "none", an invalid
// FuncIRI, or an id that does not resolve to a <clipPath> element. Results
// are memoized by id in b.clipMemo.
func (b *sceneBuilder) resolveClipPathRef(ref string) *ClipPath {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "url(") {
		// Not a url() reference at all (some other invalid value):
		// unresolvable, so "no clipping" per SVG's error-handling model.
		return nil
	}
	id, _, ok := parsePaintServerRef(ref)
	if !ok {
		b.warnOnceMsg("svg-clip-path-bad-funciri", "svg: ignoring clip-path: unparseable url() reference")
		return nil
	}
	fragID, ok := fragmentID(id)
	if !ok {
		return nil
	}
	return b.resolveClipPath(fragID, 0)
}

// resolveClipPath resolves id against the document index into a *ClipPath,
// memoizing by id (see sceneBuilder.clipMemo) and guarding against a
// self-referencing or cyclic clipPath chain via buildingClip (mirrors
// buildingPattern exactly — see that field's doc comment). depth bounds an
// acyclic chain via maxClipPathChainDepth, independent of the cycle guard.
//
// Returns nil when: id is not present in the document index, the element it
// names is not a <clipPath>, a cycle or excessive chain depth is detected,
// or the resolved <clipPath> is currently being built (self-reference).
func (b *sceneBuilder) resolveClipPath(id string, depth int) *ClipPath {
	if cp, ok := b.clipMemo[id]; ok {
		return cp
	}
	if depth >= maxClipPathChainDepth || b.buildingClip[id] {
		return nil
	}
	el, ok := b.idx.ids[id]
	if !ok || el.space != svgNS || el.local != "clipPath" {
		return nil
	}

	b.buildingClip[id] = true
	defer delete(b.buildingClip, id)

	cp := &ClipPath{
		M:     elementTransform(el, b.logf),
		Units: clipPathUnits(el),
	}

	// A <clipPath> is not part of the RENDERED tree (it contributes no scene
	// nodes of its own — see skippedElements), but it IS part of the
	// ordinary element tree for INHERITANCE purposes: an inherited property
	// like clip-rule set on an ancestor (e.g. the root <svg>) must still
	// reach a <clipPath>'s children, exactly as it would for any other
	// element at that position in the DOM (the corpus's
	// clip-rule-from-parent-node fixture asserts exactly this). The main
	// scene walk never visits a <clipPath> (buildNode's skippedElements
	// short-circuit means it's never reached via inherited parentStyle), so
	// resolveClipPath must compute that inherited style itself by walking
	// el's own DOM ancestor chain from the root down — see
	// inheritedStyleFor.
	ctx := &cascadeCtx{idx: b.idx, logf: b.logf}
	inherited := b.inheritedStyleFor(el, ctx)
	selfStyle := inherited.apply(el, ctx)
	if ref, ok := selfStyle.ClipPathRef(); ok {
		cp.Self = b.resolveClipPathRefAt(ref, depth+1)
	}

	for _, kid := range el.kids {
		if child, ok := b.buildClipChild(kid, selfStyle, ctx, depth); ok {
			cp.Kids = append(cp.Kids, child)
		}
	}

	b.clipMemo[id] = cp
	return cp
}

// resolveClipPathRefAt is resolveClipPathRef with an explicit chain depth,
// for a clip-path reference discovered while already resolving another
// clipPath (the clipPath's own self-reference, or a child's).
func (b *sceneBuilder) resolveClipPathRefAt(ref string, depth int) *ClipPath {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "url(") {
		return nil
	}
	id, _, ok := parsePaintServerRef(ref)
	if !ok {
		return nil
	}
	fragID, ok := fragmentID(id)
	if !ok {
		return nil
	}
	return b.resolveClipPath(fragID, depth)
}

// inheritedStyleFor computes the inherited Style in effect at el, by walking
// el's DOM ancestor chain (via element.parent) from the document root down
// to (but not including) el, applying each ancestor's own attributes in
// turn. This is what lets an inherited property like clip-rule set on an
// ancestor OUTSIDE the <clipPath> subtree (e.g. the root <svg>) still reach
// a <clipPath>'s children, even though the ordinary scene walk never visits
// a <clipPath> at all (it is a skippedElements entry — see that map's doc
// comment) and so never threads a parentStyle down to it the normal way.
//
// This does NOT special-case display:none the way buildNode's walk does
// (short-circuiting a hidden subtree): a <clipPath> living inside a
// display:none ancestor must still resolve normally, since clip-path
// resolution is independent of whether the clipPath's OWN ancestors are
// rendered — only the REFERENCING element's visibility matters for whether
// the clip is ever actually applied.
func (b *sceneBuilder) inheritedStyleFor(el *element, ctx *cascadeCtx) Style {
	var chain []*element
	for a := el.parent; a != nil; a = a.parent {
		chain = append(chain, a)
	}
	s := defaultStyle()
	for i := len(chain) - 1; i >= 0; i-- {
		s = s.apply(chain[i], ctx)
	}
	return s
}

// clipPathUnits resolves a <clipPath>'s clipPathUnits attribute
// (userSpaceOnUse|objectBoundingBox), defaulting to userSpaceOnUse per SVG
// (note this is the OPPOSITE default from gradientUnits/patternUnits, which
// default to objectBoundingBox — a common point of confusion the spec itself
// calls out).
func clipPathUnits(el *element) string {
	if el.attrs["clipPathUnits"] == "objectBoundingBox" {
		return "objectBoundingBox"
	}
	return "userSpaceOnUse"
}

// buildClipChild converts one <clipPath> child element into a ClipPathChild,
// or ok=false when the child is not a valid clipPath child at all (a <g>,
// <image>, <switch>, or any other element not in clipPathChildKinds), is
// display:none, or is degenerate geometry. Unlike buildNode's forgiving
// "unknown element becomes a container" default, an invalid child here is
// simply dropped — a <clipPath> does not recurse into disallowed structural
// elements, per SVG's explicit restriction to shapes/text/use.
//
// Both display:none and visibility:hidden remove a child from the union
// (verified against the corpus's invisible-child-1/invisible-child-2
// reference renders, both of which clip their target rect to nothing): per
// SVG 1.1 §14.3.5 and SVG2's clipPath rendering model, a clipPath child that
// is not rendered — for either reason — does not contribute to the clip
// region. This mirrors buildShape's ordinary painted-shape visibility gate;
// clip-rule/fill/stroke/opacity on a clipPath child still have no rendering
// effect and are simply never read here.
func (b *sceneBuilder) buildClipChild(el *element, parentStyle Style, ctx *cascadeCtx, depth int) (ClipPathChild, bool) {
	if el == nil || el.space != svgNS {
		return ClipPathChild{}, false
	}
	if !clipPathChildKinds[el.local] {
		return ClipPathChild{}, false
	}

	st := parentStyle.apply(el, ctx)
	if !st.display || !st.visible {
		return ClipPathChild{}, false
	}

	path := shapePath(el, b.logf)
	if path == nil {
		// Degenerate geometry, OR a not-yet-implemented kind (text/use):
		// either way this child contributes nothing to the union, which is
		// exactly what "no Path" already means for the caller (append
		// nothing, not "clip to everything").
		return ClipPathChild{}, false
	}

	child := ClipPathChild{
		Path: path,
		M:    elementTransform(el, b.logf),
		Rule: st.ClipRule(),
	}
	if ref, ok := st.ClipPathRef(); ok {
		child.Self = b.resolveClipPathRefAt(ref, depth+1)
	}
	return child, true
}
