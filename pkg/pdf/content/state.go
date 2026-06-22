package content

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// gstate is the PDF graphics state subset the interpreter tracks. It is copied
// by value on "q" and restored on "Q".
type gstate struct {
	ctm render.Matrix

	fill   color.RGBA
	stroke color.RGBA

	// Constant alpha from the ExtGState "gs" operator: /ca for fills (and text),
	// /CA for strokes. 1.0 is fully opaque. Kept separate from the fill/stroke
	// color so that color operators (g/rg/k, sc/scn), which reset the color's
	// alpha channel, don't clobber it; it is multiplied in at paint time.
	fillAlpha   float64
	strokeAlpha float64

	lineWidth  float64
	lineCap    render.LineCap
	lineJoin   render.LineJoin
	miterLimit float64
	dashArray  []float64
	dashPhase  float64

	// Color spaces currently selected for fill/stroke (by name), needed to
	// interpret sc/scn operands. Empty means a device space implied by the
	// most recent g/rg/k operator.
	fillCS   colorSpace
	strokeCS colorSpace

	text textState
}

// newGState returns the initial graphics state for a page, with the given base
// transform mapping PDF user space to device pixels.
func newGState(base render.Matrix) gstate {
	return gstate{
		ctm:         base,
		fill:        color.RGBA{0, 0, 0, 255},
		stroke:      color.RGBA{0, 0, 0, 255},
		fillAlpha:   1,
		strokeAlpha: 1,
		lineWidth:   1,
		miterLimit:  10,
		fillCS:      deviceGray,
		strokeCS:    deviceGray,
		text:        newTextState(),
	}
}

// clone returns a copy safe to push on the stack. Slices are copied so a child
// state mutating its dash array cannot corrupt the parent.
func (g gstate) clone() gstate {
	c := g
	if g.dashArray != nil {
		c.dashArray = append([]float64(nil), g.dashArray...)
	}
	return c
}

// applyExtGState handles the "gs" operator: it looks the named ExtGState up in
// the page resources and applies the parameters we support (/ca, /CA constant
// alpha). Parameters we do not interpret (blend modes, soft masks) are logged
// once so the unsupported behavior is visible but non-fatal.
func (it *Interpreter) applyExtGState(operands []pdf.Object) {
	name := nameOperand(operands)
	if name == "" || it.res == nil {
		return
	}
	params, ok := it.res.ExtGState(name)
	if !ok {
		it.logf("content: /ExtGState %q not found", name)
		return
	}
	if params.HasFillAlpha {
		it.gs.fillAlpha = params.FillAlpha
	}
	if params.HasStrokeAlpha {
		it.gs.strokeAlpha = params.StrokeAlpha
	}
	if params.HasUnsupported {
		it.logf("content: /ExtGState (gs) not applied: blend mode / soft mask unsupported")
	}
}

// withAlpha scales c's alpha channel by a in [0,1], returning the adjusted color.
// Used to fold the ExtGState constant alpha into a fill/stroke color at paint
// time without disturbing the stored color.
func withAlpha(c color.RGBA, a float64) color.RGBA {
	c.A = uint8(float64(c.A)*a + 0.5)
	return c
}
