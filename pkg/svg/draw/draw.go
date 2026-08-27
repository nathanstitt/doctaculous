// Package draw renders a parsed svg.Document onto a render.Device.
package draw

import (
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// Renderer draws one svg.Document. It is stateless and safe for concurrent
// use across pages/devices (the Document is read-only after Parse), so a
// single Renderer may be shared across the engine's parallel page-render
// fan-out. No mutable state is stored on the struct; per-render bookkeeping
// (such as the opacity log-once flag) is threaded through the walk as a
// local instead.
type Renderer struct {
	// Doc is the parsed document this Renderer paints.
	Doc *svg.Document
	// Logf receives one debug line for degraded fidelity encountered while
	// painting (currently: the group-opacity approximation notice, logged at
	// most once per DrawVector call). nil means silent.
	Logf func(string, ...any)
}

// New returns a Renderer for doc.
func New(doc *svg.Document) *Renderer {
	return &Renderer{Doc: doc}
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
	logged := new(bool) // per-call, not stored on Renderer: keeps concurrent DrawVector calls race-free
	m0 := rootM.Mul(ctm)
	r.paint(dev, root, m0, 1.0, logged)
}

// paint recurses through the scene graph rooted at n, painting each Shape
// found. m is the accumulated user-space-to-device matrix in effect at n;
// alpha is the accumulated group-opacity factor (the PR-1 approximation:
// multiplied directly into each paint's color alpha rather than composited
// via an offscreen group). logged tracks whether the one-per-document
// opacity-approximation notice has already been emitted for this DrawVector
// call.
func (r *Renderer) paint(dev render.Device, n svg.Node, m render.Matrix, alpha float64, logged *bool) {
	switch node := n.(type) {
	case *svg.Group:
		if node == nil {
			return
		}
		gm := node.M.Mul(m)
		for _, kid := range node.Kids {
			r.paint(dev, kid, gm, alpha, logged)
		}
	case *svg.Shape:
		r.paintShape(dev, node, m, alpha, logged)
	}
}

// paintShape paints one Shape: fill first, then stroke (SVG's default paint
// order), each with the accumulated group opacity folded into its alpha.
func (r *Renderer) paintShape(dev render.Device, s *svg.Shape, m render.Matrix, alpha float64, logged *bool) {
	if s == nil || s.Path == nil {
		return
	}

	alpha *= clamp01(s.Style.Opacity())
	if alpha < 1 {
		r.logOpacityOnce(logged)
	}

	sm := s.M.Mul(m)
	dp := render.TransformPath(s.Path, sm)
	if dp == nil {
		return
	}

	if fp, ok := s.Style.FillPaint(); ok {
		fp.Color.A = scaleAlpha(fp.Color.A, alpha)
		dev.Fill(dp, fp)
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

// logOpacityOnce emits the group-opacity approximation notice the first
// time it is needed for the current DrawVector call, and is a no-op on
// subsequent calls. logged is a pointer local to one DrawVector invocation
// (never a Renderer field), so concurrent DrawVector calls on the same
// Renderer never share it and cannot race on it.
func (r *Renderer) logOpacityOnce(logged *bool) {
	if *logged || r.Logf == nil {
		return
	}
	*logged = true
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
