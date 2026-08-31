package svg

import (
	"math"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// parseTransform parses an SVG transform list. Per SVG error handling a single
// malformed entry invalidates the whole attribute (Identity, false). In a list
// "A B" the later entry applies to the point first, so entries accumulate as
// m = entry.Mul(m).
func parseTransform(s string) (render.Matrix, bool) {
	m := render.Identity
	rest := strings.TrimSpace(s)
	for rest != "" {
		open := strings.IndexByte(rest, '(')
		close := strings.IndexByte(rest, ')')
		if open < 0 || close < open {
			return render.Identity, false
		}
		name := strings.TrimSpace(rest[:open])
		args := parseNumberList(rest[open+1 : close])
		rest = strings.TrimLeft(rest[close+1:], " \t\n\r\f,")

		var t render.Matrix
		switch name {
		case "translate":
			switch len(args) {
			case 1:
				t = render.Translate(args[0], 0)
			case 2:
				t = render.Translate(args[0], args[1])
			default:
				return render.Identity, false
			}
		case "scale":
			switch len(args) {
			case 1:
				t = render.Scale(args[0], args[0])
			case 2:
				t = render.Scale(args[0], args[1])
			default:
				return render.Identity, false
			}
		case "rotate":
			switch len(args) {
			case 1:
				t = render.Rotate(args[0] * math.Pi / 180)
			case 3:
				// rotate about (cx,cy): translate(-c) first, then rotate, then translate(+c).
				t = render.Translate(-args[1], -args[2]).
					Mul(render.Rotate(args[0] * math.Pi / 180)).
					Mul(render.Translate(args[1], args[2]))
			default:
				return render.Identity, false
			}
		case "skewX":
			if len(args) != 1 {
				return render.Identity, false
			}
			t = render.Skew(math.Tan(args[0]*math.Pi/180), 0)
		case "skewY":
			if len(args) != 1 {
				return render.Identity, false
			}
			t = render.Skew(0, math.Tan(args[0]*math.Pi/180))
		case "matrix":
			if len(args) != 6 {
				return render.Identity, false
			}
			t = render.Matrix{A: args[0], B: args[1], C: args[2], D: args[3], E: args[4], F: args[5]}
		default:
			return render.Identity, false
		}
		m = t.Mul(m)
	}
	return m, true
}

// parseAngle parses a CSS <angle> (used by marker's orient="<angle>" form,
// SVG2 §11.6.7): a number followed by one of deg/grad/rad/turn, or a bare
// number (no unit), which SVG's own <angle> grammar (unlike CSS's, which
// requires a unit except for zero) allows and treats as degrees — the same
// bare-number-means-degrees convention parseTransform already uses for
// rotate()/skewX()/skewY(). Returns radians and ok=false for an empty,
// unparseable, or unrecognized-unit string; no existing parser in this
// package handles anything but degrees, so this is the first angle-unit
// parser in the package (see the design's task list for why: orient is the
// only property with grad/rad/turn units anywhere in the corpus this engine
// targets).
func parseAngle(s string) (radians float64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	unit := ""
	num := s
	for _, u := range [...]string{"deg", "grad", "rad", "turn"} {
		if strings.HasSuffix(s, u) {
			unit, num = u, s[:len(s)-len(u)]
			break
		}
	}
	v, ok := parseNumber(num)
	if !ok {
		return 0, false
	}
	switch unit {
	case "", "deg":
		return v * math.Pi / 180, true
	case "grad":
		return v * math.Pi / 200, true
	case "rad":
		return v, true
	case "turn":
		return v * 2 * math.Pi, true
	}
	return 0, false
}
