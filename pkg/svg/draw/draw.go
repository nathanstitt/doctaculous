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
// degradation notices have already been emitted. It is allocated fresh per
// call (never stored on Renderer) so concurrent DrawVector calls on the same
// Renderer never share it and cannot race on it.
type warnFlags struct {
	opacity        bool
	strokeGradient bool
}

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
	} else if fp, ok := s.Style.FillPaint(); ok {
		fp.Color.A = scaleAlpha(fp.Color.A, alpha)
		dev.Fill(dp, fp)
	}

	if s.StrokeGradient != nil {
		// No stroke-to-outline conversion exists in pkg/render/raster (see
		// stroke.go) to clip a shading against, so a gradient stroke cannot
		// be painted as a gradient today: degrade to the fallback solid
		// color instead (Style.StrokePaint already reflects a url() paint's
		// fallback color, or reports ok=false when there is none — see
		// applyPaint), with a one-per-document warning. This is a documented
		// follow-up, not final behavior; gradient FILLS are unaffected.
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
