package css

import (
	"math"
	"strings"
)

// CSS `transform` — the 2D subset.
//
// A transform is a PAINT-TIME effect: it does not change layout, and the box keeps the
// space it occupied untransformed (CSS Transforms 1 §3). That is what lets the engine
// implement it as a matrix bracket over the box's already-flattened items rather than
// as a layout property.
//
// Implemented: translate/translateX/translateY (lengths and percentages of the box's
// own size), scale/scaleX/scaleY, rotate, skew/skewX/skewY, and matrix(). 3D functions
// are deliberately absent — this engine has no 3D pipeline, and approximating them by
// dropping the Z terms would silently render the wrong thing.

// Transform is a parsed CSS transform: the 2x3 matrix, plus any percentage terms that
// can only be resolved once the box's size is known.
//
// PctX/PctY are the translate percentages, kept separate because a percentage in
// `translate` resolves against the BOX's own border-box size, which the cascade does
// not know. Layout adds them when it builds the final matrix.
type Transform struct {
	A, B, C, D, E, F float64
	PctX, PctY       float64
}

// IsIdentity reports whether the transform does nothing.
func (t Transform) IsIdentity() bool {
	return t.A == 1 && t.B == 0 && t.C == 0 && t.D == 1 && t.E == 0 && t.F == 0 &&
		t.PctX == 0 && t.PctY == 0
}

// identityTransform is the no-op transform.
func identityTransform() Transform { return Transform{A: 1, D: 1} }

// mul returns the composition that applies `in` first, then t — matching the order CSS
// applies a function list (left to right, each in the previous one's coordinate space).
func (t Transform) mul(in Transform) Transform {
	return Transform{
		A:    in.A*t.A + in.B*t.C,
		B:    in.A*t.B + in.B*t.D,
		C:    in.C*t.A + in.D*t.C,
		D:    in.C*t.B + in.D*t.D,
		E:    in.E*t.A + in.F*t.C + t.E,
		F:    in.E*t.B + in.F*t.D + t.F,
		PctX: in.PctX*t.A + in.PctY*t.C + t.PctX,
		PctY: in.PctX*t.B + in.PctY*t.D + t.PctY,
	}
}

// parseTransform parses a `transform` value into a single matrix. ok=false for "none",
// an empty value, or any function this engine does not implement — so the declaration
// is dropped and the previous value stands, rather than a partial list being applied.
func parseTransform(value string, fontSizePt float64) (Transform, bool) {
	v := strings.TrimSpace(value)
	if v == "" || strings.EqualFold(v, "none") {
		return identityTransform(), false
	}
	out := identityTransform()
	rest := v
	any := false
	for {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		open := strings.IndexByte(rest, '(')
		if open < 0 {
			return identityTransform(), false
		}
		name := strings.ToLower(strings.TrimSpace(rest[:open]))
		// matchParen takes the index just AFTER the opening paren, not the paren
		// itself — its own doc says so, and passing the paren makes it never match.
		close, found := matchParen(rest, open+1)
		if !found {
			return identityTransform(), false
		}
		fn, ok := parseTransformFunc(name, rest[open+1:close], fontSizePt)
		if !ok {
			return identityTransform(), false
		}
		out = out.mul(fn)
		any = true
		rest = rest[close+1:]
	}
	if !any {
		return identityTransform(), false
	}
	return out, true
}

// parseTransformFunc parses one transform function's arguments.
func parseTransformFunc(name, args string, fontSizePt float64) (Transform, bool) {
	parts := splitTopLevelCommas(args)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// A single argument may also be space-separated in some functions; CSS uses commas,
	// so only commas are accepted here.
	num := func(i int) (float64, bool) {
		if i >= len(parts) {
			return 0, false
		}
		return parseColorNumber(parts[i])
	}
	// length resolves a <length-percentage> to points, or to a percentage term.
	length := func(i int) (abs, pct float64, ok bool) {
		if i >= len(parts) {
			return 0, 0, false
		}
		tok := newTokenizer(parts[i]).next()
		l, k := parseLength(tok)
		if !k {
			return 0, 0, false
		}
		switch l.Unit {
		case UnitPercent:
			return 0, l.Value / 100, true
		case UnitEm:
			return l.Value * fontSizePt, 0, true
		case UnitPx, UnitPt:
			return l.Value, 0, true
		}
		return 0, 0, false
	}
	angle := func(i int) (float64, bool) {
		if i >= len(parts) {
			return 0, false
		}
		return parseAngleRad(parts[i])
	}

	switch name {
	case "translate", "translatex", "translatey":
		ax, px, ok := length(0)
		if !ok {
			return Transform{}, false
		}
		t := identityTransform()
		switch name {
		case "translatex":
			t.E, t.PctX = ax, px
		case "translatey":
			t.F, t.PctY = ax, px
		default:
			t.E, t.PctX = ax, px
			if len(parts) > 1 {
				ay, py, ok2 := length(1)
				if !ok2 {
					return Transform{}, false
				}
				t.F, t.PctY = ay, py
			}
		}
		return t, true

	case "scale", "scalex", "scaley":
		sx, ok := num(0)
		if !ok {
			return Transform{}, false
		}
		t := identityTransform()
		switch name {
		case "scalex":
			t.A = sx
		case "scaley":
			t.D = sx
		default:
			sy := sx // a single argument scales both axes
			if len(parts) > 1 {
				v, ok2 := num(1)
				if !ok2 {
					return Transform{}, false
				}
				sy = v
			}
			t.A, t.D = sx, sy
		}
		return t, true

	case "rotate":
		r, ok := angle(0)
		if !ok {
			return Transform{}, false
		}
		cos, sin := math.Cos(r), math.Sin(r)
		return Transform{A: cos, B: sin, C: -sin, D: cos}, true

	case "skew", "skewx", "skewy":
		r, ok := angle(0)
		if !ok {
			return Transform{}, false
		}
		t := identityTransform()
		switch name {
		case "skewx":
			t.C = math.Tan(r)
		case "skewy":
			t.B = math.Tan(r)
		default:
			t.C = math.Tan(r)
			if len(parts) > 1 {
				r2, ok2 := angle(1)
				if !ok2 {
					return Transform{}, false
				}
				t.B = math.Tan(r2)
			}
		}
		return t, true

	case "matrix":
		if len(parts) != 6 {
			return Transform{}, false
		}
		var v [6]float64
		for i := 0; i < 6; i++ {
			n, ok := num(i)
			if !ok {
				return Transform{}, false
			}
			v[i] = n
		}
		return Transform{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}, true
	}
	// A 3D function (translate3d, rotateX, perspective…) or an unknown name. Refused
	// rather than approximated: this engine has no 3D pipeline, and dropping the Z
	// terms would silently paint the wrong thing.
	return Transform{}, false
}

// parseAngleRad parses a CSS <angle> to radians.
func parseAngleRad(s string) (float64, bool) {
	t := strings.ToLower(strings.TrimSpace(s))
	// "grad" MUST be tested before "rad": it ends in those three letters, so a
	// rad-first check matches "100grad", strips "rad", and then fails to parse the
	// leftover "100g" — a unit silently becoming invalid rather than wrong.
	switch {
	case strings.HasSuffix(t, "grad"):
		v, ok := parseColorNumber(strings.TrimSuffix(t, "grad"))
		return v * math.Pi / 200, ok
	case strings.HasSuffix(t, "deg"):
		v, ok := parseColorNumber(strings.TrimSuffix(t, "deg"))
		return v * math.Pi / 180, ok
	case strings.HasSuffix(t, "rad"):
		v, ok := parseColorNumber(strings.TrimSuffix(t, "rad"))
		return v, ok
	case strings.HasSuffix(t, "turn"):
		v, ok := parseColorNumber(strings.TrimSuffix(t, "turn"))
		return v * 2 * math.Pi, ok
	}
	// A bare 0 is a valid angle.
	if v, ok := parseColorNumber(t); ok && v == 0 {
		return 0, true
	}
	return 0, false
}
