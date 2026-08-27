package svg

import (
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// maxUseDepth bounds <use> tree-recursion depth (a <use> instantiating a
// target that itself contains another <use>, and so on), independent of
// buildingUse's cycle detection. A cycle is caught in O(1) hops via
// buildingUse (an id already "in progress" further up the stack); this
// instead guards a long ACYCLIC chain of distinct targets (#a uses #b uses
// #c uses ...) from costing unbounded recursion depth, mirroring
// maxHrefChainDepth/maxClipPathChainDepth's rationale exactly.
const maxUseDepth = 64

// buildUse resolves a <use> element (el.local == "use") into a Group
// instantiating its href target, or nil when the reference is absent,
// unresolvable, targets an unsupported element (nested <svg> — see the
// design's decision 3), or a cycle/depth cap fires.
//
// useSiteStyle is el's OWN resolved style (parentStyle.apply(el, ctx), i.e.
// the cascade already applied at the <use> element itself by buildNode's
// caller) — NOT defaultStyle(). Per SVG, <use> instantiates its target as if
// it were a deep clone spliced in at the <use>'s position: the clone's
// parent for inheritance purposes is the <use> element, not the target's
// original document parent. So the target's own attributes (resolved by
// buildNode's parentStyle.apply(target, ctx) call one level down) override
// useSiteStyle exactly where the target sets its own value, and inherit
// useSiteStyle everywhere else. This is why <use> instantiation is NEVER
// memoized by target id (see buildingUse's doc comment): two <use>s of the
// same target with different inherited style must produce genuinely
// different Shapes — style-inheritance-1.svg and
// complex-style-resolving-order.svg are the fixtures that pin this down.
//
// b.useDepth tracks recursion depth across nested instantiations (a
// <symbol>'s content containing another <use>, or a <use> target that is
// itself a <use>): every buildUse/buildUseSymbol call increments it for the
// duration of building that instantiation's content, so a long chain of
// DISTINCT targets (#a uses #b uses #c uses ...) trips maxUseDepth exactly
// once, from whichever call happens to cross the cap, rather than each
// level resetting to zero.
func (b *sceneBuilder) buildUse(el *element, useSiteStyle Style, ctx *cascadeCtx) Node {
	href, ok := el.attrs["href"]
	if !ok {
		return nil
	}
	id, ok := fragmentID(href)
	if !ok {
		return nil // external or unparseable reference: not yet supported
	}
	target, ok := b.idx.ids[id]
	if !ok || target.space != svgNS {
		return nil
	}
	if target.local == "svg" {
		// Nested <svg> as a <use> target is explicitly out of scope for this
		// PR (design decision 3): unsupportedElements no longer carries
		// "use"/"symbol", but "svg" itself still does, and buildUse is a
		// separate dispatch branch that never routes through
		// unsupportedElements/warnOnce — so silently instantiating a nested
		// <svg> here would be wrong (it establishes its own viewport, which
		// nothing here implements) without ever producing the usual "not yet
		// supported" notice either. Degrade to "use resolves to nothing",
		// matching every other "target doesn't work yet" case below; this is
		// a deliberate, silent deferral (see the seven
		// xlink-to-svg-element*.svg corpus fixtures the design defers).
		return nil
	}

	// BOTH cycle shapes must terminate (design decision 2):
	//   - href chain: target is itself a <use> whose own href eventually
	//     comes back to el (or to any <use> already on this call stack).
	//   - tree recursion: target is an ANCESTOR of el in the DOM (a <use>
	//     targeting its own ancestor <g>), or el is nested inside a <use>
	//     that another nested <use> targets. Neither is reachable by reading
	//     a single element's href attribute — only a tree walk that tracks
	//     "which ids are currently being instantiated on this call stack"
	//     catches it, which is exactly what buildingUse (checked by id, not
	//     by pointer) provides: the tree-recursion case is caught because
	//     el's own id (if any) is already present in buildingUse from an
	//     enclosing buildUse frame by the time the tree walk reaches the
	//     inner <use> that targets it.
	if b.useDepth >= maxUseDepth {
		b.warnOnceMsg("use-depth-cap", "svg: <use> reference chain exceeded depth limit; treating as unresolved")
		return nil
	}
	if el.id != "" && b.buildingUse[el.id] {
		b.warnOnceMsg("use-cycle:"+el.id, "svg: <use> reference is cyclic; treating as unresolved")
		return nil
	}
	if b.buildingUse[id] {
		b.warnOnceMsg("use-cycle:"+id, "svg: <use> reference is cyclic; treating as unresolved")
		return nil
	}
	if el.id != "" {
		b.buildingUse[el.id] = true
		defer delete(b.buildingUse, el.id)
	}
	b.buildingUse[id] = true
	defer delete(b.buildingUse, id)
	b.useDepth++
	defer func() { b.useDepth-- }()

	x := gradientCoord(el.attrs, "x", 0, true, b.vp.w)
	y := gradientCoord(el.attrs, "y", 0, true, b.vp.h)
	m := render.Translate(x, y).Mul(elementTransform(el, b.logf))

	var content Node
	if target.local == "symbol" {
		content = b.buildSymbolInstance(el, target, useSiteStyle, ctx)
	} else {
		content = b.buildNode(target, useSiteStyle, ctx)
	}
	if content == nil {
		return nil
	}

	// The <use> element's own opacity (already resolved into
	// useSiteStyle.opacity by the caller's parentStyle.apply(el, ctx) — see
	// buildNode's "use" dispatch branch) applies to the wrapper Group, since
	// a plain Shape's Style.Opacity accessor is never consulted for a
	// non-shape use target (a <symbol> or <g>) and buildNode's own
	// per-element opacity handling only ever reaches el's CHILDREN, never
	// el itself for a <use> (<use> has no "children" of its own in the
	// rendered sense — its Group IS the element). Mirrors the
	// opacity-on-use.svg/opacity-on-use-and-symbol.svg corpus fixtures,
	// which require <use opacity="..."> to composite exactly like a <g
	// opacity="...">'s own element opacity does.
	g := &Group{M: m, Opacity: useSiteStyle.opacity}
	g.Kids = []Node{content}
	return g
}

// buildSymbolInstance instantiates a <symbol> target referenced by useEl,
// establishing the viewport a <symbol> defines (SVG2 §5.6): sized by useEl's
// own width/height (falling back to 100%, i.e. the current viewport extent,
// per spec), and mapped through symbolEl's viewBox exactly like the root
// <svg>'s own viewBox->viewport mapping (resolvePattern is the precedent
// call for reusing parseViewBox/viewBoxMatrix this way, including how it
// handles an unparseable viewBox: silently fall back to Identity rather than
// failing the whole instantiation).
//
// Default overflow:hidden clips content to the viewport rect — implemented
// as a Group.ViewportClip (a cheap axis-aligned rect fast path, see that
// field's doc comment) rather than a synthesized <clipPath>, since every
// <symbol> instantiation would otherwise need an extra offscreen
// compositing group (BeginGroup/EndGroup, a full-canvas scratch buffer —
// see maxGroupNestingDepth's doc comment on that cost) purely to hold a
// single always-axis-aligned rectangular clip that a plain
// Device.PushClip already expresses exactly. overflow other than
// "hidden"/"auto" (i.e. "visible") disables the clip per spec.
//
// This is also where sceneBuilder.vp's "exactly one viewport in play"
// invariant (see that field's doc comment) is broken and repaired: a
// <symbol> establishes a SECOND viewport for the duration of building its
// content (a userSpaceOnUse percentage inside the symbol resolves against
// the symbol's own w/h, not the outer document's), so b.vp is saved and
// restored around the buildGroup call below — the same save/restore
// discipline buildingPattern's set/clear pairing uses for its own per-call
// state.
func (b *sceneBuilder) buildSymbolInstance(useEl, symbolEl *element, useSiteStyle Style, ctx *cascadeCtx) Node {
	w, wOK := lengthOrPercentOfViewport(useEl.attrs["width"], b.vp.w)
	if !wOK {
		w = b.vp.w
	}
	h, hOK := lengthOrPercentOfViewport(useEl.attrs["height"], b.vp.h)
	if !hOK {
		h = b.vp.h
	}
	if w <= 0 || h <= 0 {
		// A zero/negative viewport size disables rendering of the element
		// referencing it, per SVG's replaced-element sizing rules (mirrors
		// resolvePattern's identical w<=0||h<=0 guard).
		return nil
	}

	vm := render.Identity
	if vbAttr, ok := symbolEl.attrs["viewBox"]; ok {
		if vb, ok := parseViewBox(vbAttr); ok {
			vm = viewBoxMatrix(vb, w, h, symbolEl.attrs["preserveAspectRatio"])
		} else {
			b.logf("svg: ignoring viewBox=%q on symbol %q: unparseable or non-positive extent", vbAttr, symbolEl.id)
		}
	}

	// A <symbol>'s own transform attribute has no effect per SVG 1.1 (resvg's
	// with-transform.svg fixture asserts exactly this — see its <desc>: "In
	// SVG 1.1, symbol cannot have a transform, so it should be ignored" —
	// this engine follows the SVG1.1/resvg-test-suite baseline the rest of
	// this package targets elsewhere), so symbolEl's transform attribute is
	// deliberately never read here.
	saved := b.vp
	b.vp = viewport{w: w, h: h}
	kids := b.buildGroup(symbolEl, useSiteStyle, ctx)
	b.vp = saved

	g := &Group{M: vm, Opacity: symbolElementOpacity(symbolEl, ctx), Kids: kids.Kids}
	if wantsViewportClip(symbolEl, ctx) {
		// The clip rect lives in the SAME local space vm maps FROM (i.e.
		// [0,w]x[0,h] in the <use>'s own user space, before the viewBox
		// mapping is applied) — not in the post-viewBox content space vm maps
		// TO. pkg/svg/draw composes ViewportClip with the group's pre-M
		// accumulated matrix, exactly like Shape.Path composes with Shape.M,
		// so the clip stays axis-aligned in the referencing <use>'s space
		// regardless of any skew/rotation baked into vm by a non-default
		// preserveAspectRatio.
		g.ViewportClip = viewportRectPath(w, h)
	}
	return g
}

// viewportRectPath builds the axis-aligned [0,w]x[0,h] rect path a
// <symbol>'s default overflow:hidden clips its instantiated content to. See
// Group.ViewportClip's doc comment for why this is a plain render.Path
// clipped via Device.PushClip rather than a synthesized ClipPath.
func viewportRectPath(w, h float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(w, 0)
	p.LineTo(w, h)
	p.LineTo(0, h)
	p.Close()
	return p
}

// lengthOrPercentOfViewport parses an SVG length attribute (present, absent,
// or a percentage resolved against ref), reporting ok=false when the
// attribute is absent, empty, or fails to parse — the caller substitutes its
// own default (100%, i.e. the ambient viewport extent, per <use>'s
// width/height lacuna value for a <symbol>/nested-<svg> target).
func lengthOrPercentOfViewport(s string, ref float64) (float64, bool) {
	if s == "" {
		return 0, false
	}
	return parseLength(s, ref)
}

// symbolElementOpacity resolves a <symbol>'s own opacity attribute exactly
// like rootOpacity resolves the root <svg>'s: a <symbol> is never part of
// the ordinary parentStyle-threading scene walk (buildNode never reaches it
// directly — it is only ever entered via buildSymbolInstance), so its own
// element opacity must be read here rather than falling out of the usual
// Style.apply pipeline the way a <g>'s does.
func symbolElementOpacity(symbolEl *element, ctx *cascadeCtx) float64 {
	s := Style{opacity: 1}
	attr := ctx.resolve(symbolEl)
	logf := ctx.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	applyOpacityProp("opacity", &s.opacity, attr, logf)
	return s.opacity
}

// wantsViewportClip reports whether el (a <symbol>, or any other
// viewport-establishing element) has an overflow that clips its viewport:
// true for the absent attribute (the CSS/SVG default for such an element is
// "hidden", not CSS's general "visible" default) and for the literal
// "hidden"/"auto" keywords; false only for an explicit "visible" (or
// "scroll", never meaningful for static SVG).
//
// It resolves overflow through the CASCADE, not off the raw attribute, so
// style="overflow:visible" and an `overflow` stylesheet rule work — and so
// whitespace and keyword case are handled — exactly as they do for every
// other property. See Style.WantsViewportClip / applyOverflow.
func wantsViewportClip(el *element, ctx *cascadeCtx) bool {
	s := Style{overflow: "hidden"}
	attr := ctx.resolve(el)
	logf := ctx.logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	applyOverflow(&s, attr, logf)
	return s.WantsViewportClip()
}
