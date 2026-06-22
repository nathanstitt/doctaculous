package content

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// gstate is the PDF graphics state subset the interpreter tracks. It is copied
// by value on "q" and restored on "Q".
type gstate struct {
	ctm render.Matrix

	fill   color.RGBA
	stroke color.RGBA

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
		ctm:        base,
		fill:       color.RGBA{0, 0, 0, 255},
		stroke:     color.RGBA{0, 0, 0, 255},
		lineWidth:  1,
		miterLimit: 10,
		fillCS:     deviceGray,
		strokeCS:   deviceGray,
		text:       newTextState(),
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
