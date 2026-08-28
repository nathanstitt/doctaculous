package layout

import "image/color"

// GradientKind distinguishes the gradient geometries a background gradient may
// have. It is the layout-local mirror of the css GradientKind, mapped when the
// item is built — layout must not import css (the same rule BgSizeKind follows).
type GradientKind int

const (
	// GradientLinear runs the colour ramp along a straight gradient line.
	GradientLinear GradientKind = iota
	// GradientRadial runs the ramp outward from a centre through an ellipse.
	GradientRadial
)

// GradientStop is one colour stop with its position ALREADY resolved to a
// fraction of the gradient's parameter range. CSS's stop-normalization rules
// (fill in omitted positions, correct decreasing ones, spread unpositioned runs)
// have all been applied by the time a stop reaches here, so the painter needs no
// CSS knowledge — it only lerps.
//
// A position may fall outside [0,1]: `linear-gradient(red -20%, blue 120%)` is
// valid CSS and simply pushes the ramp past the gradient line's ends. The ramp
// handles that by extrapolating the parameter, not by clamping the stop.
type GradientStop struct {
	Pos   float64
	Color color.RGBA
}

// BackgroundGradient is a CSS background gradient resolved into pure geometry,
// in the coordinate space of ONE background tile: the tile's own box, with the
// origin at its top-left, Y down, sized in points.
//
// Tile space rather than page space is the right frame because a gradient is a
// background IMAGE: `background-size` sets the tile's box, and the gradient is
// laid out inside THAT box, then positioned and repeated exactly like a bitmap
// would be. Resolving into page space instead would make a sized or repeated
// gradient wrong in a way that only shows up once someone combines the two
// properties.
//
// The geometry is stored in the same shape render.ShadingDesc uses (an axis for
// linear, a focal-plus-outer circle for radial), so building the shader is a
// direct hand-off to the existing shading seam rather than a second gradient
// implementation.
type BackgroundGradient struct {
	Kind GradientKind

	// X0,Y0 → X1,Y1 is the gradient LINE for GradientLinear: the point where the
	// ramp parameter is 0 and the point where it is 1, in tile space.
	X0, Y0, X1, Y1 float64

	// CX,CY is the ending shape's centre and RX,RY its two semi-axes, for
	// GradientRadial. RX and RY differ for an ellipse; the painter renders an
	// ellipse by scaling a unit circle, since the shared radial shader is
	// circular.
	CX, CY, RX, RY float64

	// Stops is the normalized colour-stop list, non-decreasing in Pos and
	// carrying at least two entries (CSS requires two; a shorter list never
	// reaches here because parsing rejects it).
	Stops []GradientStop

	// Repeating selects the repeating-* variant: the ramp tiles outside [0,1]
	// instead of holding the endpoint colours. It maps directly onto the
	// existing shading spread modes (repeat vs. pad).
	Repeating bool
}

// Reparameterize rescales the gradient's GEOMETRY so that parameter values lo
// and hi land where 0 and 1 previously did. It is the geometric half of
// normalizing a repeating gradient: the stop list is rescaled so its range
// becomes exactly [0,1] (the range the shader's repeat spread folds into), and
// this moves the line or ending shape to match, so the ramp still starts and
// ends where the author placed it.
//
// For a linear gradient the axis endpoints move along the line. For a radial one
// the ramp runs outward from the centre, so lo and hi scale the RADII — and a
// non-zero lo would mean the ramp starts at a non-zero radius, which the shared
// circular evaluator (whose inner circle is fixed at r=0) cannot express. That
// case leaves the radii scaled to hi and lets the caller's stop rescaling carry
// the offset, which is exact when lo is 0 (the overwhelmingly common case) and
// an approximation otherwise.
func (g *BackgroundGradient) Reparameterize(lo, hi float64) {
	switch g.Kind {
	case GradientRadial:
		if hi != 0 {
			g.RX *= hi
			g.RY *= hi
		}
	default:
		dx, dy := g.X1-g.X0, g.Y1-g.Y0
		x0, y0 := g.X0+dx*lo, g.Y0+dy*lo
		x1, y1 := g.X0+dx*hi, g.Y0+dy*hi
		g.X0, g.Y0, g.X1, g.Y1 = x0, y0, x1, y1
	}
}
