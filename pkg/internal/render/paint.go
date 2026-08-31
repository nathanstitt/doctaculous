package render

import "image/color"

// FillRule selects how a path's interior is determined.
type FillRule int

const (
	// NonZero is the nonzero winding rule (PDF "f"/"F").
	NonZero FillRule = iota
	// EvenOdd is the even-odd rule (PDF "f*").
	EvenOdd
)

// LineCap is the shape drawn at the ends of open subpaths.
type LineCap int

const (
	// ButtCap ends the stroke squarely at the endpoint.
	ButtCap LineCap = iota
	// RoundCap ends the stroke with a semicircle.
	RoundCap
	// SquareCap ends the stroke with a half-square extending past the endpoint.
	SquareCap
)

// LineJoin is the shape drawn where two stroke segments meet.
type LineJoin int

const (
	// MiterJoin extends the outer edges to a point.
	MiterJoin LineJoin = iota
	// RoundJoin rounds the corner.
	RoundJoin
	// BevelJoin cuts the corner off.
	BevelJoin
)

// FillPaint describes how to fill a region. BlendMode is the ExtGState /BM blend
// mode name ("" or "Normal" = source-over).
type FillPaint struct {
	Color     color.RGBA
	Rule      FillRule
	BlendMode string
}

// StrokePaint describes how to stroke a path. Width and DashArray/DashPhase are
// already expressed in device space. BlendMode is the /BM blend mode name.
type StrokePaint struct {
	Color      color.RGBA
	Width      float64
	Cap        LineCap
	Join       LineJoin
	MiterLimit float64
	DashArray  []float64
	DashPhase  float64
	BlendMode  string
}
