package svg

import (
	"math"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
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
