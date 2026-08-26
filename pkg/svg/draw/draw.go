// Package draw renders a parsed svg.Document onto a render.Device.
package draw

import (
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
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
	// painting (currently: the group-opacity approximation, and the
	// gradient-stroke fallback, each logged at most once per DrawVector
	// call). nil means silent.
	Logf func(string, ...any)
}

// New returns a Renderer for doc.
func New(doc *svg.Document) *Renderer {
	return &Renderer{Doc: doc}
}

// warnFlags tracks, for one DrawVector call, which one-per-document
// degradation notices have already been emitted, plus the recursion-guard
// counters that protect one DrawVector call against pathological input. It is
// allocated fresh per call (never stored on Renderer) so concurrent
// DrawVector calls on the same Renderer never share it and cannot race on
// it — the same reason the guard counters live here rather than on Renderer,
// which must stay stateless for concurrent use.
type warnFlags struct {
	opacity        bool
	strokeGradient bool
	patternCap     bool
	depthCap       bool
	budgetCap      bool

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
// alpha is the accumulated group-opacity factor (the PR-1 approximation:
// multiplied directly into each paint's color alpha rather than composited
// via an offscreen group). warned tracks which one-per-document degradation
// notices have already been emitted for this DrawVector call.
func (r *Renderer) paint(dev render.Device, n svg.Node, m render.Matrix, alpha float64, warned *warnFlags) {
	switch node := n.(type) {
	case *svg.Group:
		if node == nil {
			return
		}
		gm := node.M.Mul(m)
		for _, kid := range node.Kids {
			r.paint(dev, kid, gm, alpha, warned)
		}
	case *svg.Shape:
		r.paintShape(dev, node, m, alpha, warned)
	}
}

// paintShape paints one Shape: fill first, then stroke (SVG's default paint
// order), each with the accumulated group opacity folded into its alpha.
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

	alpha *= clamp01(s.Style.Opacity())
	if alpha < 1 {
		r.logOpacityOnce(warned)
	}

	sm := s.M.Mul(m)
	dp := render.TransformPath(s.Path, sm)
	if dp == nil {
		return
	}

	if s.FillGradient != nil {
		r.fillGradient(dev, dp, s.Style, s.FillGradient, sm, alpha)
	} else if s.FillPattern != nil {
		r.fillPattern(dev, dp, s.Style, s.FillPattern, sm, alpha, warned)
	} else if fp, ok := s.Style.FillPaint(); ok {
		fp.Color.A = scaleAlpha(fp.Color.A, alpha)
		dev.Fill(dp, fp)
	}

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
	if sp, ok := s.Style.StrokePaint(); ok {
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

// logOpacityOnce emits the group-opacity approximation notice the first
// time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls.
func (r *Renderer) logOpacityOnce(warned *warnFlags) {
	if warned.opacity || r.Logf == nil {
		return
	}
	warned.opacity = true
	r.Logf("svg: group opacity approximated per-paint until compositing lands")
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
