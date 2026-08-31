package render

import "math"

// Matrix is a 2-D affine transform in PDF's six-element form [a b c d e f],
// representing:
//
//	| a b 0 |
//	| c d 0 |
//	| e f 1 |
//
// A point (x,y) maps to (a*x + c*y + e, b*x + d*y + f).
type Matrix struct {
	A, B, C, D, E, F float64
}

// Identity is the identity transform.
var Identity = Matrix{A: 1, B: 0, C: 0, D: 1, E: 0, F: 0}

// Translate returns a translation matrix.
func Translate(tx, ty float64) Matrix { return Matrix{A: 1, D: 1, E: tx, F: ty} }

// Scale returns a scaling matrix.
func Scale(sx, sy float64) Matrix { return Matrix{A: sx, D: sy} }

// Mul returns m * n, i.e. the transform that applies m first, then n. In PDF the
// concatenation order for "cm" is newCTM = cm × CTM, which is Mul(cm, ctm).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.B*n.C,
		B: m.A*n.B + m.B*n.D,
		C: m.C*n.A + m.D*n.C,
		D: m.C*n.B + m.D*n.D,
		E: m.E*n.A + m.F*n.C + n.E,
		F: m.E*n.B + m.F*n.D + n.F,
	}
}

// Apply transforms a point by the matrix.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// ApplyVector transforms a vector (ignoring translation), used for distances and
// directions such as line widths.
func (m Matrix) ApplyVector(x, y float64) (float64, float64) {
	return m.A*x + m.C*y, m.B*x + m.D*y
}

// ScaleFactor returns an approximate uniform scale factor of the transform,
// useful for converting a 1-D measure (e.g. line width) into device space. It
// uses the square root of the absolute determinant.
func (m Matrix) ScaleFactor() float64 {
	det := m.A*m.D - m.B*m.C
	return math.Sqrt(math.Abs(det))
}

// Invert returns the matrix that undoes m, and ok=false if m is singular
// (zero determinant, e.g. a zero scale) rather than dividing by zero.
func (m Matrix) Invert() (Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 {
		return Matrix{}, false
	}
	inv := 1 / det
	a := m.D * inv
	b := -m.B * inv
	c := -m.C * inv
	d := m.A * inv
	e := -(m.E*a + m.F*c)
	f := -(m.E*b + m.F*d)
	return Matrix{A: a, B: b, C: c, D: d, E: e, F: f}, true
}

// Rotate returns a rotation by theta radians. In the Y-down device space used
// throughout, a positive theta rotates clockwise (matching SVG/CSS rotate()).
func Rotate(theta float64) Matrix {
	s, c := math.Sincos(theta)
	return Matrix{A: c, B: s, C: -s, D: c}
}

// Skew returns a shear transform: tanX shears X by Y (CSS/SVG skewX), tanY
// shears Y by X (skewY). Arguments are tangents of the skew angles.
func Skew(tanX, tanY float64) Matrix {
	return Matrix{A: 1, B: tanY, C: tanX, D: 1}
}
