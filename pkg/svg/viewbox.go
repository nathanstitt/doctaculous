package svg

import (
	"math"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// viewBox is a parsed viewBox attribute: min point and extent in user units.
type viewBox struct {
	MinX, MinY, W, H float64
}

// parseViewBox parses "min-x min-y width height". A negative or zero extent
// reports false (SVG: zero disables rendering, negative is an error). Callers
// must not pass a viewBox to viewBoxMatrix unless parseViewBox accepted it:
// viewBoxMatrix divides by W and H, so a non-positive extent would divide by
// zero or produce a nonsensical (negative/inverted) scale.
func parseViewBox(s string) (viewBox, bool) {
	n := parseNumberList(s)
	if len(n) != 4 || n[2] <= 0 || n[3] <= 0 {
		return viewBox{}, false
	}
	return viewBox{n[0], n[1], n[2], n[3]}, true
}

// viewBoxMatrix returns the matrix mapping viewBox user space into a
// vpW×vpH viewport per preserveAspectRatio (raw attribute value; "" and
// unrecognized values fall back to the spec default "xMidYMid meet"). vb
// must come from a parseViewBox that reported true (see its doc comment).
func viewBoxMatrix(vb viewBox, vpW, vpH float64, par string) render.Matrix {
	align, meet := "xMidYMid", true
	fields := strings.Fields(par)
	if len(fields) >= 1 && fields[0] != "" {
		switch fields[0] {
		case "none", "xMinYMin", "xMidYMin", "xMaxYMin", "xMinYMid", "xMidYMid",
			"xMaxYMid", "xMinYMax", "xMidYMax", "xMaxYMax":
			align = fields[0]
		}
	}
	if len(fields) >= 2 && fields[1] == "slice" {
		meet = false
	}

	sx, sy := vpW/vb.W, vpH/vb.H
	tx, ty := 0.0, 0.0
	if align != "none" {
		// Uniform scale: "meet" fits entirely inside the viewport (the
		// smaller axis scale wins), "slice" fills the viewport and overflows
		// (the larger axis scale wins).
		s := math.Min(sx, sy)
		if !meet {
			s = math.Max(sx, sy)
		}
		sx, sy = s, s

		fx, fy := 0.5, 0.5 // Mid alignment on either axis is the default.
		switch {
		case strings.HasPrefix(align, "xMin"):
			fx = 0
		case strings.HasPrefix(align, "xMax"):
			fx = 1
		}
		switch {
		case strings.HasSuffix(align, "YMin"):
			fy = 0
		case strings.HasSuffix(align, "YMax"):
			fy = 1
		}
		tx = fx * (vpW - vb.W*sx)
		ty = fy * (vpH - vb.H*sy)
	}

	// Shift the viewBox origin to zero, scale into the viewport, then apply
	// the meet/slice alignment offset (zero for "none").
	return render.Translate(-vb.MinX, -vb.MinY).
		Mul(render.Scale(sx, sy)).
		Mul(render.Translate(tx, ty))
}
