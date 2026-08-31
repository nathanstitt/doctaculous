package svgwrite

import (
	"math"
	"strconv"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// maxCoord bounds the magnitude of a coordinate this writer will emit. SVG
// numbers have no spec'd range, but a viewer parsing "1e300" into a float and
// multiplying it through a transform reaches infinity and drops the whole
// element, so an absurd value is clamped rather than propagated. The bound
// mirrors pdfwrite's maxRealMagnitude for the same reason.
const maxCoord = 3.403e38

// formatCoord renders f as a compact SVG number.
//
// It is the single choke point every numeric value in the emitted markup passes
// through, so the non-finite guard here covers all producers (the PDF
// interpreter, the reflow paint layer, and the SVG painter) without each having
// to pre-validate. A NaN or ±Inf reaching an attribute is not a cosmetic
// problem: "d=\"M NaN 0\"" makes a viewer discard the entire path, so a
// degenerate value is clamped to something drawable instead.
func formatCoord(f float64) string {
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		f = 0
	case f > maxCoord:
		f = maxCoord
	case f < -maxCoord:
		f = -maxCoord
	}
	s := strconv.FormatFloat(f, 'f', 4, 64)
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// appendPathData appends p's geometry to sb as an SVG "d" attribute value.
//
// render.Path carries only MoveTo/LineTo/CubeTo/Close (no arcs, no quadratics),
// which is exactly SVG's M/L/C/Z, so this is a direct transcription with no
// curve conversion. Coordinates are already in device space, which shares SVG's
// top-left origin and Y-down axis, so they are emitted verbatim.
func appendPathData(sb *strings.Builder, p *render.Path) {
	if p == nil {
		return
	}
	// Separation is tracked per PATH, not by the builder's length: callers
	// append into a shared buffer (the defs block), where sb.Len() > 0 is true
	// from the very first segment and would emit a leading space inside the
	// d attribute.
	for i, s := range p.Segments {
		if i > 0 {
			sb.WriteByte(' ')
		}
		switch s.Kind {
		case render.MoveTo:
			sb.WriteByte('M')
			writeCoords(sb, s.P0.X, s.P0.Y)
		case render.LineTo:
			sb.WriteByte('L')
			writeCoords(sb, s.P0.X, s.P0.Y)
		case render.CubeTo:
			sb.WriteByte('C')
			writeCoords(sb, s.P0.X, s.P0.Y, s.P1.X, s.P1.Y, s.P2.X, s.P2.Y)
		case render.Close:
			sb.WriteByte('Z')
		}
	}
}

// pathData renders p as a complete "d" attribute value.
func pathData(p *render.Path) string {
	var sb strings.Builder
	appendPathData(&sb, p)
	return sb.String()
}

// writeCoords appends space-separated numbers after a path command letter.
func writeCoords(sb *strings.Builder, vals ...float64) {
	for _, v := range vals {
		sb.WriteByte(' ')
		sb.WriteString(formatCoord(v))
	}
}

// matrixAttr renders m as an SVG transform attribute value.
//
// render.Matrix uses the same six-element affine convention as SVG's
// matrix(a b c d e f) with the same row-vector multiplication order, so the
// fields map across positionally with no rearrangement.
func matrixAttr(m render.Matrix) string {
	var sb strings.Builder
	sb.WriteString("matrix(")
	for i, v := range [...]float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(formatCoord(v))
	}
	sb.WriteByte(')')
	return sb.String()
}

// isIdentity reports whether m is close enough to the identity that emitting a
// transform attribute for it would be pure noise.
func isIdentity(m render.Matrix) bool {
	return m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1 && m.E == 0 && m.F == 0
}
