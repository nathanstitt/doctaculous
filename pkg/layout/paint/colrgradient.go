package paint

import (
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// colrGradientShader evaluates a COLR v1 gradient for the shading pipeline.
//
// It exists because a colour-font gradient arrives in FONT UNITS with its own stop
// list and extend mode, none of which the PDF shading types the render layer already
// builds happen to describe. Implementing render.Shader directly is the smallest way
// in: FillShading maps each device pixel back into this space and asks for a colour.
type colrGradientShader struct {
	g    *font.ColorGradient
	upem float64
}

var _ render.Shader = (*colrGradientShader)(nil)

// newColorGradientShader builds a shader for g, or nil when the gradient is degenerate
// (no stops, or zero extent) and cannot be evaluated.
func newColorGradientShader(g *font.ColorGradient, upem float64) *colrGradientShader {
	if g == nil || len(g.Stops) == 0 || upem <= 0 {
		return nil
	}
	if g.Radial {
		if g.R0 == g.R1 && g.X0 == g.X1 && g.Y0 == g.Y1 {
			return nil
		}
	} else if g.X0 == g.X1 && g.Y0 == g.Y1 {
		return nil
	}
	return &colrGradientShader{g: g, upem: upem}
}

// ColorAt evaluates the gradient at a point in EM units (the space the layer's path
// was transformed into), converting to the font units the geometry uses.
func (s *colrGradientShader) ColorAt(x, y float64) (color.RGBA, bool) {
	fx, fy := x*s.upem, y*s.upem
	var t float64
	if s.g.Radial {
		var ok bool
		t, ok = s.radialParam(fx, fy)
		if !ok {
			return color.RGBA{}, false
		}
	} else {
		t = s.linearParam(fx, fy)
	}
	t, ok := applyExtend(t, s.g.Extend)
	if !ok {
		return color.RGBA{}, false
	}
	return s.colorAtOffset(t), true
}

// linearParam projects (x,y) onto the gradient's axis, returning the parameter along
// P0->P1. The rotation point P2 skews the axis: CSS-style linear gradients in COLR are
// defined by three points, where P0P2 sets the gradient's direction of constant colour.
func (s *colrGradientShader) linearParam(x, y float64) float64 {
	g := s.g
	dx, dy := g.X1-g.X0, g.Y1-g.Y0
	// Project onto the axis perpendicular to P0->P2 when P2 is meaningful, matching the
	// spec's construction; with a degenerate P2 this reduces to the plain projection.
	rx, ry := g.X2-g.X0, g.Y2-g.Y0
	if rx != 0 || ry != 0 {
		// Rotate the axis so it is perpendicular to P0P2 (the spec's "rotated" form).
		if d := rx*dx + ry*dy; d != 0 {
			// Remove the P0P2 component from the axis.
			l2 := rx*rx + ry*ry
			k := d / l2
			dx -= k * rx
			dy -= k * ry
		}
	}
	den := dx*dx + dy*dy
	if den == 0 {
		return 0
	}
	return ((x-g.X0)*dx + (y-g.Y0)*dy) / den
}

// radialParam solves for the parameter of the two-circle (cone) gradient at (x,y),
// the same construction PDF radial shadings and CSS radial gradients use. ok=false
// means the point is outside every circle in the family.
func (s *colrGradientShader) radialParam(x, y float64) (float64, bool) {
	g := s.g
	cdx, cdy, dr := g.X1-g.X0, g.Y1-g.Y0, g.R1-g.R0
	px, py := x-g.X0, y-g.Y0
	a := cdx*cdx + cdy*cdy - dr*dr
	b := px*cdx + py*cdy + g.R0*dr
	c := px*px + py*py - g.R0*g.R0
	if math.Abs(a) < 1e-9 {
		if b == 0 {
			return 0, false
		}
		t := c / (2 * b)
		return t, g.R0+t*dr >= 0
	}
	disc := b*b - a*c
	if disc < 0 {
		return 0, false
	}
	sq := math.Sqrt(disc)
	// Prefer the larger root with a non-negative radius, as PDF/CSS both do.
	for _, t := range [2]float64{(b + sq) / a, (b - sq) / a} {
		if g.R0+t*dr >= 0 {
			return t, true
		}
	}
	return 0, false
}

// applyExtend maps a raw parameter into [0,1] under the gradient's spread mode,
// reporting ok=false only when the point should not paint at all.
func applyExtend(t float64, mode string) (float64, bool) {
	if t >= 0 && t <= 1 {
		return t, true
	}
	switch mode {
	case "repeat":
		t -= math.Floor(t)
		return t, true
	case "reflect":
		t = math.Mod(math.Abs(t), 2)
		if t > 1 {
			t = 2 - t
		}
		return t, true
	default: // pad
		if t < 0 {
			return 0, true
		}
		return 1, true
	}
}

// colorAtOffset interpolates the stop list at t, which is already in [0,1].
func (s *colrGradientShader) colorAtOffset(t float64) color.RGBA {
	st := s.g.Stops
	if t <= st[0].Offset {
		return st[0].Color
	}
	last := st[len(st)-1]
	if t >= last.Offset {
		return last.Color
	}
	for i := 1; i < len(st); i++ {
		if t <= st[i].Offset {
			a, b := st[i-1], st[i]
			span := b.Offset - a.Offset
			if span <= 0 {
				return b.Color
			}
			k := (t - a.Offset) / span
			return lerpRGBA(a.Color, b.Color, k)
		}
	}
	return last.Color
}

// lerpRGBA interpolates two colours componentwise. COLR gradients interpolate in
// premultiplied alpha per the spec, which matters only when stops differ in alpha.
func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	aa, ba := float64(a.A)/255, float64(b.A)/255
	oa := aa + (ba-aa)*t
	if oa <= 0 {
		return color.RGBA{}
	}
	mix := func(ca, cb uint8) uint8 {
		pa := float64(ca) / 255 * aa
		pb := float64(cb) / 255 * ba
		v := (pa + (pb-pa)*t) / oa * 255
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v + 0.5)
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: uint8(oa*255 + 0.5)}
}
