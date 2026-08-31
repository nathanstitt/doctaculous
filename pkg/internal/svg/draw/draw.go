// Package draw renders a parsed svg.Document onto a render.Device.
package draw

import (
	"image/color"
	"math"
	"sync"

	layoutfont "github.com/nathanstitt/omnidoc/pkg/internal/layout/font"
	"github.com/nathanstitt/omnidoc/pkg/internal/svg"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// Renderer draws one svg.Document. It is stateless and safe for concurrent
// use across pages/devices (the Document is read-only after Parse), so a
// single Renderer may be shared across the engine's parallel page-render
// fan-out. No mutable state is stored on the struct; per-render bookkeeping
// (such as the log-once flags) is threaded through the walk as a local
// instead.
type Renderer struct {
	// Doc is the parsed document this Renderer paints.
	Doc *svg.Document
	// Logf receives one debug line for degraded fidelity encountered while
	// painting: the gradient-stroke fallback and the pattern
	// nesting/cell-count/draw-call-budget caps (each at most once per
	// DrawVector call), plus every TEXT degradation the shaper reports — a
	// family with no bundled face, and a rune no face can map, which draws
	// .notdef.
	//
	// nil means silent, and silent is the wrong default for a renderer that can
	// draw a page of tofu. Leaving this unset is what made CJK text in SVG
	// render as columns of empty boxes with no diagnostic anywhere, while the
	// identical text through the CSS path logged once per rune. Prefer
	// NewWithLogf over assigning the field after New, so a caller that has a
	// logger cannot silently skip it.
	Logf func(string, ...any)

	// faceOnce/faceCache lazily hold the font-face cache SVG text shaping
	// resolves families through. This is the one exception to the "no mutable
	// state on Renderer" rule above, and it is a safe one: FaceCache is
	// itself mutex-protected and explicitly safe for concurrent use, the
	// sync.Once guarantees exactly one is ever created, and the field is
	// never reassigned afterward — so concurrent DrawVector calls share a
	// single cache rather than racing on it. Sharing is the point: parsing a
	// font program is the expensive step, and every <text> in a document
	// resolves the same handful of families.
	faceOnce  sync.Once
	faceCache *layoutfont.FaceCache
}

// New returns a Renderer for doc that reports no diagnostics. Prefer
// NewWithLogf wherever a logger is available; see Renderer.Logf for why silence
// is a poor default here.
func New(doc *svg.Document) *Renderer {
	return &Renderer{Doc: doc}
}

// NewWithLogf returns a Renderer for doc that reports its degradations to logf.
// A nil logf is equivalent to New.
//
// It exists as a constructor rather than leaving callers to set the field
// because every one of them had a logger in hand and none of them passed it —
// the field defaulted to nil at three separate call sites, and the resulting
// silence was only noticed when a page of tofu rendered without a word. Making
// the logger a constructor argument puts it where a caller has to decide about
// it. This mirrors layoutfont.NewOSFontProvider / NewOSFontProviderWithLogf.
func NewWithLogf(doc *svg.Document, logf func(string, ...any)) *Renderer {
	return &Renderer{Doc: doc, Logf: logf}
}

// warnFlags tracks, for one DrawVector call, which one-per-document
// degradation notices have already been emitted, plus the recursion-guard
// counters that protect one DrawVector call against pathological input. It is
// allocated fresh per call (never stored on Renderer) so concurrent
// DrawVector calls on the same Renderer never share it and cannot race on
// it — the same reason the guard counters live here rather than on Renderer,
// which must stay stateless for concurrent use.
type warnFlags struct {
	strokeGradient bool
	patternCap     bool
	depthCap       bool
	budgetCap      bool
	groupDepthCap  bool

	// filterUnsupported/filterNoRaster/filterRegionCap track the three ways
	// a filter degrades to painting its element unfiltered: a primitive
	// this engine does not implement, a backend with no offscreen raster
	// (pdfwrite), and a filter region past the allocation cap. Each logs at
	// most once per DrawVector call, like every other flag here.
	filterUnsupported bool
	filterNoRaster    bool
	filterRegionCap   bool
	filterNestingCap  bool
	filterBBoxCap     bool

	// filterDepth counts filters currently being applied on the stack: a
	// filtered element inside another filter's source content. Each level
	// holds a full-canvas offscreen RGBA plus a per-primitive float32
	// buffer live at once, so depth bounds MEMORY the same way groupDepth
	// does for compositing groups.
	filterDepth int

	// patternDepth counts nested <pattern> fills currently on the stack: a
	// tile painted while resolving pattern P that itself fills with a
	// DIFFERENT pattern Q is not a cycle (buildingPattern in pkg/svg only
	// catches a pattern tile referencing itself, directly or through
	// another pattern), so nothing at scene-build time bounds a chain of
	// distinct patterns P0 -> P1 -> P2 -> .... Each level multiplies draw
	// calls by its own cell count, so an 8-deep chain of ~400-cell tiles
	// blows past any reasonable render time long before it would ever
	// legitimately occur (real SVG nests patterns at most 2-3 deep).
	patternDepth int

	// drawCalls counts every leaf paint op (Fill/Stroke/FillShading, plus
	// one per pattern-tile-cell placement) issued so far in this
	// DrawVector call. It bounds total work across the WHOLE call, not
	// just one shape's pattern fan-out, so it also protects future
	// draw-time expansion graphs (e.g. <use>/<symbol>) that don't run
	// through fillPattern at all.
	drawCalls int

	// groupDepth counts BeginGroup/EndGroup nesting currently open on the
	// device (a nested <g opacity>/clip-path/mask, or a masked/clipped
	// shape reached while already inside one of those). Each level
	// allocates a full-canvas scratch surface (see
	// raster.Device.BeginGroup) that lives until the matching EndGroup, so
	// depth — not just per-call count — is what bounds memory: an
	// adversarial chain of nested groups can hold arbitrarily many
	// scratch buffers live at once even though each individual DrawVector
	// call is otherwise cheap. See maxGroupNestingDepth.
	groupDepth int
}

// maxPatternNestingDepth bounds how many <pattern> fills may be nested
// (a pattern's tile containing a shape filled with a different pattern,
// containing a shape filled with yet another, ...) within one DrawVector
// call. buildingPattern (pkg/svg) only catches a pattern tile that
// eventually references itself; a chain of otherwise-unrelated patterns is
// not a cycle and passes that guard every time, so this is the only bound on
// it. Real-world SVG never nests patterns more than 2-3 deep; 4 is generous
// headroom above that while still stopping the exponential blowup (each
// level multiplies draw calls by its own cell count) in well under a second.
const maxPatternNestingDepth = 4

// maxGroupNestingDepth bounds how many offscreen compositing groups
// (BeginGroup/EndGroup pairs — opened for a <g opacity>/clip-path/mask, or a
// masked/clipped shape with both a fill and a stroke — may be open at once
// on the device during one DrawVector call. Every open group holds a
// full-canvas scratch RGBA alive until its matching EndGroup (see
// raster.Device.BeginGroup), so unbounded nesting depth is an unbounded
// number of live full-canvas buffers, not just unbounded CPU time like
// maxPatternNestingDepth guards against — this is reachable from untrusted
// input via Open/OpenBytes, through nested <g>, nested <mask>/<clipPath>
// content, or any combination stacking clip+mask+opacity several levels
// deep. Real-world SVG nests transparency groups at most a handful deep;
// this is generous headroom above that while still capping worst-case
// concurrent scratch-buffer memory to a small constant multiple of one
// page's canvas size.
const maxGroupNestingDepth = 16

// maxDrawCalls bounds the total number of leaf paint operations
// (Fill/Stroke/FillShading/pattern-cell placements) one DrawVector call may
// issue. It is a backstop against any draw-time expansion blowing past a
// sane render budget — not just nested patterns, but any future
// draw-time-recursive feature (e.g. <use>/<symbol> chains) that doesn't run
// through the pattern-depth guard at all. Sized comfortably above what any
// legitimate document in the corpus needs (worst case today is on the order
// of maxPatternTileCells for a single heavily-tiled shape) while still
// tripping well before a pathological chain reaches multi-second render
// times.
const maxDrawCalls = 200_000

// maxPatternTileCells bounds how many tile repetitions fillPattern will draw
// to cover one shape's fill region. A tile cell this small relative to the
// shape it paints is a pathological document (or a percentage/unit
// mistake), not a legitimate use — real-world patterns tile a visibly-sized
// swatch a few dozen times per axis at most. Bounding the repeat count keeps
// a single shape from generating an unbounded number of draw calls;
// fillPattern logs once and clips the grid to a centered window of this many
// cells when a document would otherwise exceed it.
const maxPatternTileCells = 10_000

// DrawVector renders the document with ctm mapping the document's viewport
// coordinates (points, origin top-left) into device space. It satisfies
// layout.VectorScene, the seam Task 12 uses to place an SVG as a replaced
// element inside the page-layout pipeline.
//
// A nil Renderer, nil Doc, nil root group, or nil dev is a no-op: rendering
// degrades gracefully rather than panicking.
func (r *Renderer) DrawVector(dev render.Device, ctm render.Matrix) {
	if r == nil || r.Doc == nil || dev == nil {
		return
	}
	rootM, root := r.Doc.Root()
	if root == nil {
		return
	}
	warned := &warnFlags{}
	m0 := rootM.Mul(ctm)
	r.paint(dev, root, m0, 1.0, warned)
}

// paint recurses through the scene graph rooted at n, painting each Shape
// found. m is the accumulated user-space-to-device matrix in effect at n;
// alpha is the accumulated group/element-opacity factor still handled by the
// cheap per-paint path (see paintShape). warned tracks which one-per-document
// degradation notices have already been emitted for this DrawVector call.
func (r *Renderer) paint(dev render.Device, n svg.Node, m render.Matrix, alpha float64, warned *warnFlags) {
	switch node := n.(type) {
	case *svg.Group:
		if node == nil {
			return
		}
		gm := node.M.Mul(m)
		if node.ViewportClip != nil {
			// A <symbol> instantiation's default overflow:hidden (see
			// svg.Group.ViewportClip's doc comment): a plain axis-aligned
			// rect PushClip under gm (the SAME matrix the clip's own
			// [0,w]x[0,h] local space composes under, exactly like a Shape's
			// Path composes under its own accumulated matrix), wrapping
			// EVERYTHING below — the opacity/clip-path/mask compositing
			// paths included — rather than requiring its own BeginGroup/
			// EndGroup pair. Save/Restore scope it to just this subtree so
			// it cannot leak into a sibling painted afterward.
			dev.Save()
			dev.PushClip(render.TransformPath(node.ViewportClip, gm), render.NonZero)
			r.paintGroupBody(dev, node, gm, alpha, warned)
			dev.Restore()
			return
		}
		r.paintGroupBody(dev, node, gm, alpha, warned)
	case *svg.Shape:
		r.paintShape(dev, node, m, alpha, warned)
	case *svg.Text:
		r.paintText(dev, node, m, alpha, warned)
	}
}

// paintGroupBody paints node's opacity/clip-path/mask compositing and
// children, given gm (node.M already composed with the caller's
// accumulated matrix) — the part of paint's *svg.Group case that is common
// to both the ViewportClip-wrapped and unwrapped paths, factored out so
// ViewportClip's Save/PushClip/Restore wraps this whole body exactly once
// rather than needing to be duplicated across both the fast (no
// BeginGroup) and compositing (BeginGroup/EndGroup) branches below.
func (r *Renderer) paintGroupBody(dev render.Device, node *svg.Group, gm render.Matrix, alpha float64, warned *warnFlags) {
	if node.Filter != nil {
		// A filter applies BEFORE clip-path, mask, and opacity: the group's
		// content is rendered and filtered as a unit, and the filtered
		// RESULT is then clipped/masked/faded.
		//
		// All three must therefore be stripped from the SOURCE pass, not just
		// opacity. Leaving clip-path on the nested node applies it to the
		// filter's INPUT, which clips the content before the blur can spread
		// past the clip edge — the blur then fades to nothing inside the
		// shape instead of being cut off hard at its boundary (the corpus's
		// with-clip-path fixture shows the difference plainly: a star whose
		// points fade out, versus one whose points are sliced).
		unfiltered := *node
		unfiltered.Filter = nil
		unfiltered.Opacity = 1
		unfiltered.ClipPath = nil
		unfiltered.Mask = nil
		outAlpha := alpha * clamp01(node.Opacity)
		clipBounds, _ := r.groupUserBounds(node, warned)
		filterBounds, filterBoundsTruncated := r.groupUserBounds(node, warned)
		r.paintFilteredThenClip(dev, node.ClipPath, node.Mask, gm, clipBounds, outAlpha, func(target render.Device, a float64) {
			// A subtree too deep to measure yields a box covering only part
			// of it. Filtering against that would silently omit content (or,
			// with nothing measured at all, drop the element), so degrade to
			// unfiltered with a diagnostic — the same trade every other cap
			// in filter.go makes.
			if filterBounds != nil {
				if _, _, _, _, _ = filterBounds(); *filterBoundsTruncated {
					r.logFilterBBoxDepthCapOnce(warned)
					paintUnfiltered(target, a, func(inner render.Device) {
						r.paintGroupBody(inner, &unfiltered, gm, 1, warned)
					})
					return
				}
			}
			r.paintFilteredAlpha(target, node.Filter, gm, filterBounds, warned, a, func(inner render.Device) {
				r.paintGroupBody(inner, &unfiltered, gm, 1, warned)
			})
		})
		return
	}
	if node.Opacity >= 1 && node.ClipPath == nil && node.Mask == nil {
		// The common case (a plain <g> with no opacity, clip-path, or
		// mask): skip BeginGroup/EndGroup entirely. Opening a group
		// allocates a full-page offscreen scratch buffer (see
		// raster.Device.BeginGroup), so paying that cost for every plain
		// <g> in a document would be a serious, needless performance
		// regression — there is nothing for a group with no opacity,
		// clip, or mask to composite that per-paint alpha (unchanged,
		// alpha=alpha) doesn't already produce correctly.
		for _, kid := range node.Kids {
			r.paint(dev, kid, gm, alpha, warned)
		}
		return
	}
	if node.Opacity <= 0 {
		return // fully transparent: nothing to paint at all
	}
	// True compositing: children paint at full (alpha=1) opacity into an
	// isolated offscreen group, and the group's OWN opacity (and/or
	// clip-path mask) is applied exactly once, to the flattened result,
	// in EndGroup. This is what makes two overlapping opaque children
	// under <g opacity="0.5"> come out identical at the overlap and
	// elsewhere, instead of the overlap double-darkening the way
	// per-paint alpha would produce; the same isolation is what lets a
	// clip-path's union apply to the group as a unit rather than to each
	// child's own paint calls separately.
	//
	// The incoming alpha (e.g. from an enclosing <pattern> tile's own
	// fill alpha — see fillPattern) is folded into EndGroup's factor
	// alongside node.Opacity, rather than threaded into the children as
	// alpha=alpha: alpha is uniform across the whole group with no
	// internal overlap to protect against (unlike node.Opacity, which is
	// exactly what creates the overlap risk), so multiplying it in at
	// composite time is equally correct and avoids yet another nested
	// group just to carry it.
	if warned.groupDepth >= maxGroupNestingDepth {
		// Every open BeginGroup holds a full-canvas scratch RGBA alive
		// until its EndGroup (see raster.Device.BeginGroup) — unlike
		// maxPatternNestingDepth/maxDrawCalls, which bound total CPU
		// work, this bounds concurrently-live MEMORY, so it must stop
		// opening a NEW group rather than merely stop recursing further.
		// Degrade like a backend that cannot composite offscreen (see
		// render.Device.BeginGroup's doc comment): paint children
		// directly, without the isolation this group would have given
		// them, rather than drop the subtree's content entirely.
		r.logGroupDepthCapOnce(warned)
		for _, kid := range node.Kids {
			r.paint(dev, kid, gm, alpha, warned)
		}
		return
	}
	dev.Save()
	dev.BeginGroup()
	warned.groupDepth++
	for _, kid := range node.Kids {
		r.paint(dev, kid, gm, 1.0, warned)
	}
	warned.groupDepth--
	var clipMask, softMask render.GroupMask
	if node.ClipPath != nil {
		// gm (not m): clip-path applies in the clipped element's OWN
		// user space, i.e. AFTER its own transform has been established
		// (SVG's clip-path is defined relative to the referencing
		// element's user coordinate system at the point of reference) —
		// the same reason paintShapeGrouped below uses sm (post-M), not
		// the pre-M matrix, for a Shape target.
		//
		// A Group has no single Path of its own, so an objectBoundingBox
		// clipPathUnits target has no PRE-transform geometry to measure
		// (unlike a Shape's Path — see paintShape below): degrade to
		// userSpaceOnUse (Identity mapping) for a Group target, a
		// documented, narrow approximation until a group-subtree bbox
		// helper exists.
		clipMask = r.buildClipMask(dev, node.ClipPath, gm, nil)
	}
	if node.Mask != nil {
		// Composite order is clip -> mask -> opacity (see the design
		// doc). clipMask and softMask are passed to EndGroup SEPARATELY,
		// not pre-combined here (see render.Device's doc comment on
		// EndGroup): a backend gets to decide how to apply both — the
		// raster backend multiplies their per-pixel coverage at
		// composite time (the correct product, not a min-based
		// intersection — see pkg/render/raster/device.go's EndGroup),
		// while pdfwrite represents each with its own native PDF
		// construct (a `W n` clip vs. an ExtGState /SMask) rather than
		// forcing one into the other. Pre-combining them into a single
		// GroupMask here, as an earlier revision did, broke pdfwrite's
		// sentinel-identity recognition of its own luminosity mask the
		// moment a clip-path also applied — see EndGroup's doc comment
		// in pkg/render/pdfwrite/group.go for the regression this
		// caused and the fix. Same nil-target approximation as ClipPath
		// just above: a Group has no single Path for an
		// objectBoundingBox maskUnits/maskContentUnits target.
		softMask = r.buildMask(dev, node.Mask, gm, nil)
	}
	dev.EndGroup(alpha*node.Opacity, "", clipMask, softMask)
	dev.Restore()
}

// paintShape paints one Shape: fill first, then stroke (SVG's default paint
// order), each with the accumulated group opacity folded into its alpha.
//
// A shape with BOTH a fill and a stroke, at element opacity < 1, is routed
// through an offscreen group (paintShapeGrouped) instead of the cheap
// per-paint path below: the stroke overlaps the fill along the stroke's
// inner edge, and folding opacity into each paint independently would dim
// that overlap twice, producing a visible darker ring exactly like the
// per-child-group-opacity artifact this feature exists to fix, just one
// level down (a single element's own fill/stroke rather than a group's
// children). A fill-only or stroke-only shape has no such overlap, so it
// keeps the cheap path — routing every opaque shape through a group would
// pay an offscreen-buffer allocation for the overwhelmingly common case that
// doesn't need it.
func (r *Renderer) paintShape(dev render.Device, s *svg.Shape, m render.Matrix, alpha float64, warned *warnFlags) {
	if s == nil || s.Path == nil {
		return
	}
	if warned.drawCalls >= maxDrawCalls {
		// Backstop for the whole DrawVector call, not just pattern fan-out:
		// see maxDrawCalls's doc comment. Checked here too so a document
		// that reaches the budget via patterns doesn't keep silently
		// painting plain shapes afterward.
		r.logDrawBudgetCapOnce(warned)
		return
	}
	warned.drawCalls++

	if s.Filter != nil {
		// SVG's rendering model applies a filter FIRST, then clip-path,
		// mask, and finally opacity — to the FILTERED RESULT, not to the
		// filter's input. That ordering is observable whenever a primitive
		// discards its input: an feFlood under opacity="0.5" must come out
		// half-transparent, but folding the opacity into the source paint
		// would let feFlood throw it away and produce a fully opaque flood
		// (the resvg with-opacity-on-target-element fixture pins exactly
		// this, and an earlier revision of this code failed it).
		//
		// So the source is painted at FULL opacity inside the offscreen
		// buffer, and both this element's own opacity and the caller's
		// accumulated alpha are applied afterward, to the composited
		// result — the same "apply once, to the flattened result" rule
		// paintGroupBody follows for a group.
		//
		// clip-path and mask are stripped from the source pass for the same
		// ordering reason and re-applied to the RESULT — see
		// paintFilteredThenClip.
		unfiltered := *s
		unfiltered.Filter = nil
		unfiltered.Style = unfiltered.Style.SetOpacity(1)
		unfiltered.ClipPath = nil
		unfiltered.Mask = nil
		outAlpha := alpha * clamp01(s.Style.Opacity())
		sm := s.M.Mul(m)
		r.paintFilteredThenClip(dev, s.ClipPath, s.Mask, sm, s.Path.Bounds, outAlpha, func(target render.Device, a float64) {
			r.paintFilteredAlpha(target, s.Filter, sm, s.Path.Bounds, warned, a, func(inner render.Device) {
				r.paintShape(inner, &unfiltered, m, 1, warned)
			})
		})
		return
	}

	opacity := clamp01(s.Style.Opacity())

	sm := s.M.Mul(m)
	dp := render.TransformPath(s.Path, sm)
	if dp == nil {
		return
	}

	_, hasFillPaint := s.Style.FillPaint()
	hasFill := s.FillGradient != nil || s.FillPattern != nil || hasFillPaint
	_, hasStroke := s.Style.StrokePaint()

	if s.ClipPath != nil || s.Mask != nil || (opacity < 1 && hasFill && hasStroke) {
		// alpha (the caller's incoming, e.g. from an enclosing pattern
		// tile's own fill alpha) and opacity (this shape's own element
		// opacity) both need to reach the final composite, but only ONE of
		// them may be applied per-paint without reintroducing the seam:
		// opacity is what creates the fill/stroke overlap in the first
		// place, so it must apply once, to the group. alpha carries no such
		// overlap risk (it is uniform across this whole shape, fill and
		// stroke alike), so folding it into the paints INSIDE the group is
		// equally correct and avoids a second nested group.
		//
		// A clip-path or mask forces this same grouped path even when
		// opacity is 1 or the shape has only a fill or only a stroke:
		// EndGroup is the only place a GroupMask can be applied, so clipping
		// or masking a shape at all requires opening a group regardless of
		// whether opacity itself would have needed one.
		r.paintShapeGrouped(dev, s, dp, sm, alpha, opacity, warned)
		r.paintMarkers(dev, s, sm, alpha*opacity, warned)
		return
	}
	alpha *= opacity
	r.paintFill(dev, s, dp, sm, alpha, warned)
	r.paintStroke(dev, s, dp, sm, alpha, warned)
	r.paintMarkers(dev, s, sm, alpha, warned)
}

// paintShapeGrouped paints s's fill and stroke at innerAlpha (the caller's
// incoming alpha, e.g. from an enclosing pattern tile — see paintShape) into
// an isolated offscreen group, then applies opacity — s's own element
// opacity alone — and s's resolved clip-path mask (if any), once, to the
// flattened result. See paintShape's doc comment for why the fill/stroke
// overlap requires this split. dp/sm are s's already-device-space path and
// accumulated matrix, computed once by the caller.
func (r *Renderer) paintShapeGrouped(dev render.Device, s *svg.Shape, dp *render.Path, sm render.Matrix, innerAlpha, opacity float64, warned *warnFlags) {
	if warned.groupDepth >= maxGroupNestingDepth {
		// See the matching guard in paint's Group case for why this bounds
		// memory, not just CPU time. Degrade to painting fill/stroke
		// directly, without isolation, clip-path, or mask (best-effort: a
		// clip-path or mask on a shape that also needed the fill/stroke
		// overlap isolation cannot get both without a group) rather than
		// drop the shape entirely.
		r.logGroupDepthCapOnce(warned)
		r.paintFill(dev, s, dp, sm, innerAlpha*opacity, warned)
		r.paintStroke(dev, s, dp, sm, innerAlpha*opacity, warned)
		return
	}
	dev.Save()
	dev.BeginGroup()
	warned.groupDepth++
	r.paintFill(dev, s, dp, sm, innerAlpha, warned)
	r.paintStroke(dev, s, dp, sm, innerAlpha, warned)
	warned.groupDepth--
	var clipMask, softMask render.GroupMask
	if s.ClipPath != nil {
		// objectBoundingBox target: s.Path is the shape's own PRE-transform
		// geometry, matching resolveGradient's identical use of it for a
		// gradient's objectBoundingBox mapping (see gradient.go) — the exact
		// reuse the design calls for.
		clipMask = r.buildClipMask(dev, s.ClipPath, sm, s.Path.Bounds)
	}
	if s.Mask != nil {
		// Composite order is clip -> mask -> opacity (see the design doc).
		// clipMask and softMask reach EndGroup SEPARATELY, not pre-combined
		// here — see the matching comment in paint's Group case, and
		// render.Device's EndGroup doc comment, for why: pre-combining broke
		// pdfwrite's sentinel-identity recognition of its own luminosity
		// mask whenever a clip-path also applied, silently erasing content.
		// Same objectBoundingBox target as ClipPath above.
		softMask = r.buildMask(dev, s.Mask, sm, s.Path.Bounds)
	}
	dev.EndGroup(opacity, "", clipMask, softMask)
	dev.Restore()
}

// paintFill paints s's fill alone (gradient, pattern, or solid color) with
// alpha already fully resolved by the caller.
func (r *Renderer) paintFill(dev render.Device, s *svg.Shape, dp *render.Path, sm render.Matrix, alpha float64, warned *warnFlags) {
	if s.FillGradient != nil {
		r.fillGradient(dev, dp, s.Style, s.FillGradient, sm, alpha)
	} else if s.FillPattern != nil {
		r.fillPattern(dev, dp, s.Style, s.FillPattern, sm, alpha, warned)
	} else if fp, ok := s.Style.FillPaint(); ok {
		fp.Color.A = scaleAlpha(fp.Color.A, alpha)
		dev.Fill(dp, fp)
	}
}

// paintStroke paints s's stroke alone, with alpha already fully resolved by
// the caller. A gradient/pattern stroke has no outline-conversion path (see
// the inline comment below) and degrades to StrokePaint's fallback color.
func (r *Renderer) paintStroke(dev render.Device, s *svg.Shape, dp *render.Path, sm render.Matrix, alpha float64, warned *warnFlags) {
	if s.StrokeGradient != nil || s.StrokePattern != nil {
		// No stroke-to-outline conversion exists in pkg/render/raster (see
		// stroke.go) to clip a shading/tile against, so a gradient or
		// pattern stroke cannot be painted as such today: degrade to the
		// fallback solid color instead (Style.StrokePaint already reflects a
		// url() paint's fallback color, or reports ok=false when there is
		// none — see applyPaint), with a one-per-document warning. This is a
		// documented follow-up, not final behavior; gradient/pattern FILLS
		// are unaffected.
		r.logStrokeGradientOnce(warned)
	}
	sp, ok := s.Style.StrokePaint()
	if !ok {
		return
	}
	sf := sm.ScaleFactor()
	if sf == 0 {
		return
	}
	sp.Color.A = scaleAlpha(sp.Color.A, alpha)
	sp.Width *= sf
	sp.DashPhase *= sf
	if sp.DashArray != nil {
		scaled := make([]float64, len(sp.DashArray))
		for i, d := range sp.DashArray {
			scaled[i] = d * sf
		}
		sp.DashArray = scaled
	}
	dev.Stroke(dp, sp)
}

// gradient is the accessor surface pkg/draw needs from a resolved paint
// server, satisfied by *svg.Shape's FillGradient/StrokeGradient fields
// without either package exporting the concrete paintServer type — the same
// pattern as svg.Style's FillPaint/StrokePaint accessors.
type gradient interface {
	Shader() render.Shader
	Matrix() render.Matrix
}

// fillGradient paints dp (already in device space) with a resolved gradient,
// following the same Save/PushClip/FillShading/Restore pattern
// pkg/pdf/content/paths.go's fillPath uses for a PDF shading-pattern fill:
// the clip scopes the shading to dp's interior so Save/Restore can cleanly
// undo it without leaking into later drawing. The fill rule comes from
// st.FillPaint() so the clip honors the same nonzero/evenodd rule a solid
// fill would have used; FillPaint's ok result is otherwise ignored here
// since st.FillServer already told the caller a gradient is in play, and
// FillPaint's ok only reflects whether a FALLBACK solid color exists, not
// the fill rule (which is always meaningful, fallback or not). g's Matrix
// maps the gradient's local space into the shape's own user space (Shape.M
// and the walk matrix not yet applied); composed with sm (the shape's
// already-fully-accumulated device matrix) that gives the full
// local-space-to-device CTM FillShading needs.
func (r *Renderer) fillGradient(dev render.Device, dp *render.Path, st svg.Style, g gradient, sm render.Matrix, alpha float64) {
	rule := render.NonZero
	if fp, ok := st.FillPaint(); ok {
		rule = fp.Rule
	}
	ctm := g.Matrix().Mul(sm)
	shader := g.Shader()
	if alpha < 1 {
		shader = alphaShader{inner: shader, alpha: alpha}
	}
	dev.Save()
	dev.PushClip(dp, rule)
	dev.FillShading(shader, ctm, "")
	dev.Restore()
}

// pattern is the accessor surface pkg/draw needs from a resolved pattern
// paint server, satisfied by *svg.Shape's FillPattern/StrokePattern fields
// without either package exporting the concrete patternPaint type — the same
// pattern as the gradient interface above.
type pattern interface {
	Tile() *svg.Group
	Matrix() render.Matrix
	Cell() (x, y, w, h float64)
	ContentMatrix() render.Matrix
}

// fillPattern paints dp (already in device space) with a resolved pattern by
// repeated clipped draws of its tile content: no offscreen raster buffer is
// involved (pkg/svg/draw only has a render.Device, which cannot produce one
// without importing a backend — see the package doc), so this instead clips
// to dp once, then for every tile cell overlapping dp's device-space bounds,
// clips further to that one cell and paints the tile Group under the
// composed cell-placement matrix. This is more, smaller draw calls than an
// offscreen-image approach for a small tile over a large area, but it is
// exact (no resampling) and requires no new Device capability.
//
// alpha folds in accumulated group/element opacity exactly like fillGradient
// (Device.FillShading/Fill/Stroke each take their own alpha; painting a
// Group has no single alpha sink, so this multiplies alpha into the
// recursive paint call instead, matching how group opacity already
// propagates through r.paint).
//
// The recursive r.paint(dev, tile, ..., alpha, ...) call below does NOT
// double-apply opacity now that r.paint's Group case can open a real
// compositing group: p.Tile() is always built via buildKidsGroup (see
// pkg/svg's resolvePattern), which sets Opacity: 1 unconditionally — a
// <pattern> element has no opacity property of its own to apply to its tile
// as a whole, only the shape that REFERENCES it has fill/stroke-opacity,
// already folded into alpha here. So the tile Group always takes r.paint's
// Opacity>=1 fast path (no BeginGroup) and alpha reaches the tile's shapes
// exactly once, via the per-paint multiplication in paintShape — unaffected
// by the grouping added for real <g opacity> elements.
func (r *Renderer) fillPattern(dev render.Device, dp *render.Path, st svg.Style, p pattern, sm render.Matrix, alpha float64, warned *warnFlags) {
	if warned.patternDepth >= maxPatternNestingDepth {
		// A chain of DISTINCT patterns (p0's tile fills with p1, p1's with
		// p2, ...) is not a cycle, so pkg/svg's buildingPattern guard (which
		// only catches a pattern tile eventually referencing itself) never
		// fires for it. Each level multiplies draw calls by its own cell
		// count, so left unbounded this is exponential; stop descending
		// rather than let it run away.
		r.logPatternDepthCapOnce(warned)
		return
	}
	if warned.drawCalls >= maxDrawCalls {
		r.logDrawBudgetCapOnce(warned)
		return
	}

	rule := render.NonZero
	if fp, ok := st.FillPaint(); ok {
		rule = fp.Rule
	}

	// ctm maps the shape's user space (where p.Cell()'s X/Y/CellW/CellH
	// already live, patternUnits already resolved) into device space:
	// p.Matrix() (patternTransform alone) composed with sm (shape user
	// space -> device, already fully accumulated). Per spec,
	// patternTransform applies to the whole established pattern coordinate
	// system, so it is applied here rather than per-cell below.
	ctm := p.Matrix().Mul(sm)
	inv, ok := ctm.Invert()
	if !ok {
		return // degenerate (e.g. zero-scale) CTM: nothing sensible to tile
	}

	x0, y0, cw, ch := p.Cell()
	if cw <= 0 || ch <= 0 {
		return // resolvePattern already guards this, but never divide below
	}

	minX, minY, maxX, maxY, ok := dp.Bounds()
	if !ok {
		return
	}
	// Map dp's four device-space corners back into tile space to find the
	// index range covering them (a rotated/skewed ctm means the device-space
	// AABB doesn't map to a tile-space AABB directly, so all four corners
	// are considered, not just two).
	corners := [4][2]float64{{minX, minY}, {maxX, minY}, {minX, maxY}, {maxX, maxY}}
	tMinX, tMinY := math.Inf(1), math.Inf(1)
	tMaxX, tMaxY := math.Inf(-1), math.Inf(-1)
	for _, c := range corners {
		tx, ty := inv.Apply(c[0], c[1])
		tMinX, tMaxX = math.Min(tMinX, tx), math.Max(tMaxX, tx)
		tMinY, tMaxY = math.Min(tMinY, ty), math.Max(tMaxY, ty)
	}

	iMin := int(math.Floor((tMinX - x0) / cw))
	iMax := int(math.Ceil((tMaxX - x0) / cw))
	jMin := int(math.Floor((tMinY - y0) / ch))
	jMax := int(math.Ceil((tMaxY - y0) / ch))
	if iMax < iMin || jMax < jMin {
		return
	}
	cols, rows := iMax-iMin+1, jMax-jMin+1
	if cols <= 0 || rows <= 0 {
		return
	}
	if cols*rows > maxPatternTileCells {
		r.logPatternCellCapOnce(warned)
		// Clip to a centered window of at most maxPatternTileCells cells
		// rather than refusing to paint at all: a capped, honestly-partial
		// tiling degrades far better than either an unbounded draw-call
		// storm or a blank shape.
		side := int(math.Sqrt(float64(maxPatternTileCells)))
		if side < 1 {
			side = 1
		}
		iMax = iMin + min(cols, side) - 1
		jMax = jMin + min(rows, side) - 1
	}

	dev.Save()
	dev.PushClip(dp, rule)

	contentM := p.ContentMatrix()
	tile := p.Tile()
	cellPath := cellRectPath(cw, ch)
	warned.patternDepth++
cellLoop:
	for j := jMin; j <= jMax; j++ {
		for i := iMin; i <= iMax; i++ {
			if warned.drawCalls >= maxDrawCalls {
				r.logDrawBudgetCapOnce(warned)
				break cellLoop
			}
			// cellM places THIS cell (i,j) in device space: translate the
			// unit cell box to its (x0+i*cw, y0+j*ch) origin, then apply the
			// shared pattern-space -> device ctm. Recomputed per cell (not
			// hoisted out of the loop) since every cell needs its own
			// translation composed with ctm — reusing one ctm for both the
			// clip AND the content would paint every cell with cell (0,0)'s
			// content translated only by the CLIP, never actually moving the
			// tile's own drawing commands.
			cellM := render.Translate(x0+float64(i)*cw, y0+float64(j)*ch).Mul(ctm)
			dp2 := render.TransformPath(cellPath, cellM)
			if dp2 == nil {
				continue
			}
			warned.drawCalls++
			dev.Save()
			dev.PushClip(dp2, render.NonZero)
			r.paint(dev, tile, contentM.Mul(cellM), alpha, warned)
			dev.Restore()
		}
	}
	warned.patternDepth--

	dev.Restore()
}

// cellRectPath builds the unit rect [0,w]x[0,h] path used to clip one tile
// cell before painting its content, so a tile whose content extends past its
// own cell bounds (a common, spec-legal pattern authoring choice) does not
// bleed into a neighboring cell.
func cellRectPath(w, h float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(w, 0)
	p.LineTo(w, h)
	p.LineTo(0, h)
	p.Close()
	return p
}

// logPatternCellCapOnce emits the pattern-tile-count-capped notice the first
// time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logPatternCellCapOnce(warned *warnFlags) {
	if warned.patternCap || r.Logf == nil {
		return
	}
	warned.patternCap = true
	r.Logf("svg: <pattern> tile count exceeded %d cells; painting was truncated to a centered window", maxPatternTileCells)
}

// logPatternDepthCapOnce emits the pattern-nesting-depth-capped notice the
// first time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logPatternDepthCapOnce(warned *warnFlags) {
	if warned.depthCap || r.Logf == nil {
		return
	}
	warned.depthCap = true
	r.Logf("svg: <pattern> nesting exceeded %d levels; deeper tiles were not painted", maxPatternNestingDepth)
}

// logDrawBudgetCapOnce emits the total-draw-call-budget-exceeded notice the
// first time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logDrawBudgetCapOnce(warned *warnFlags) {
	if warned.budgetCap || r.Logf == nil {
		return
	}
	warned.budgetCap = true
	r.Logf("svg: draw call budget of %d exceeded; remaining content was not painted", maxDrawCalls)
}

// logGroupDepthCapOnce emits the group-nesting-depth-capped notice the first
// time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logGroupDepthCapOnce(warned *warnFlags) {
	if warned.groupDepthCap || r.Logf == nil {
		return
	}
	warned.groupDepthCap = true
	r.Logf("svg: transparency group nesting exceeded %d levels; deeper groups painted without isolation, opacity, clip, or mask", maxGroupNestingDepth)
}

// alphaShader wraps a render.Shader to scale every returned color's alpha by
// a constant factor, so SVG element/group opacity reaches a gradient fill
// exactly as it reaches a solid one (Device.FillShading itself takes no
// alpha parameter — see pkg/render/device.go — unlike Fill/Stroke/DrawImage).
type alphaShader struct {
	inner render.Shader
	alpha float64
}

// ColorAt implements render.Shader.
func (a alphaShader) ColorAt(x, y float64) (color.RGBA, bool) {
	c, ok := a.inner.ColorAt(x, y)
	if !ok {
		return c, false
	}
	c.A = scaleAlpha(c.A, a.alpha)
	return c, true
}

// DescribeShading implements render.ShadingDescriber by delegating to the
// wrapped shader and scaling each returned stop's alpha by a.alpha, using the
// same scaleAlpha helper ColorAt uses so the two paths always agree. This is
// the delegation render.ShadingDescriber's doc comment requires of any
// wrapper: a naive alphaShader that only forwarded ColorAt would hide the
// describable Shader underneath from a PDF writer's type-assertion, silently
// falling back to rasterizing exactly the gradients most likely to carry
// opacity (any gradient under a <g opacity> or with its own opacity).
//
// ok=false whenever the inner shader is not itself a ShadingDescriber, or
// declines to describe this instance — alphaShader has no geometry of its
// own to offer in that case.
func (a alphaShader) DescribeShading() (render.ShadingDesc, bool) {
	describer, ok := a.inner.(render.ShadingDescriber)
	if !ok {
		return render.ShadingDesc{}, false
	}
	desc, ok := describer.DescribeShading()
	if !ok {
		return render.ShadingDesc{}, false
	}
	stops := make([]render.ShadingStop, len(desc.Stops))
	for i, s := range desc.Stops {
		s.Color.A = scaleAlpha(s.Color.A, a.alpha)
		stops[i] = s
	}
	desc.Stops = stops
	return desc, true
}

// logStrokeGradientOnce emits the stroke-gradient degradation notice the
// first time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logStrokeGradientOnce(warned *warnFlags) {
	if warned.strokeGradient || r.Logf == nil {
		return
	}
	warned.strokeGradient = true
	r.Logf("svg: gradient strokes not yet supported; using the fallback color (or no stroke)")
}

// scaleAlpha scales an 8-bit alpha channel by factor (expected in [0,1]).
func scaleAlpha(a uint8, factor float64) uint8 {
	return uint8(math.Round(float64(a) * factor))
}

// clamp01 clamps v to [0,1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
