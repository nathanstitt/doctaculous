package svg

import (
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// arcSegments appends the elliptical arc from (x1,y1) to (x2,y2) as cubic
// Bézier segments (SVG 1.1 F.6: endpoint to center parameterization, radii
// scaled up if too small, split into slices of at most 90° each approximated
// by one cubic with the 4/3·tan(δ/4) control distance).
func arcSegments(p *render.Path, x1, y1, rx, ry, phiDeg float64, largeArc, sweep bool, x2, y2 float64) {
	if x1 == x2 && y1 == y2 {
		return // F.6.2: zero-length arc draws nothing
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.LineTo(x2, y2) // F.6.2: degenerate radii -> straight line
		return
	}
	phi := phiDeg * math.Pi / 180
	sinP, cosP := math.Sincos(phi)

	// F.6.5.1: midpoint form in the ellipse's axis-aligned frame.
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy

	// F.6.6.3: scale radii up when no ellipse fits.
	lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}

	// F.6.5.2: center in the primed frame.
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := math.Sqrt(math.Max(0, num/den))
	if largeArc == sweep {
		co = -co
	}
	cxp := co * rx * y1p / ry
	cyp := -co * ry * x1p / rx

	// F.6.5.3: center in the original frame.
	cx := cosP*cxp - sinP*cyp + (x1+x2)/2
	cy := sinP*cxp + cosP*cyp + (y1+y2)/2

	// F.6.5.5/6: start angle and sweep extent on the unit circle.
	ang := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		l := math.Sqrt((ux*ux + uy*uy) * (vx*vx + vy*vy))
		a := math.Acos(math.Max(-1, math.Min(1, dot/l)))
		if ux*vy-uy*vx < 0 {
			a = -a
		}
		return a
	}
	ux, uy := (x1p-cxp)/rx, (y1p-cyp)/ry
	vx, vy := (-x1p-cxp)/rx, (-y1p-cyp)/ry
	theta1 := ang(1, 0, ux, uy)
	dTheta := math.Mod(ang(ux, uy, vx, vy), 2*math.Pi)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	}
	if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	// Map a unit-circle angle to the ellipse point and its tangent direction.
	point := func(t float64) (float64, float64) {
		st, ct := math.Sincos(t)
		ex, ey := rx*ct, ry*st
		return cosP*ex - sinP*ey + cx, sinP*ex + cosP*ey + cy
	}
	deriv := func(t float64) (float64, float64) {
		st, ct := math.Sincos(t)
		ex, ey := -rx*st, ry*ct
		return cosP*ex - sinP*ey, sinP*ex + cosP*ey
	}

	n := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dTheta / float64(n)
	t := theta1
	for i := 0; i < n; i++ {
		t2 := t + step
		k := 4.0 / 3.0 * math.Tan(step/4)
		p0x, p0y := point(t)
		p3x, p3y := point(t2)
		d0x, d0y := deriv(t)
		d3x, d3y := deriv(t2)
		p.CubeTo(p0x+k*d0x, p0y+k*d0y, p3x-k*d3x, p3y-k*d3y, p3x, p3y)
		t = t2
	}
}
