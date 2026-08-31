package svg

import "github.com/nathanstitt/omnidoc/pkg/render"

// pathScanner reads numbers and flags from SVG path data, where separators are
// optional wherever the grammar is unambiguous ("M.5.5", "1-2", "01" after a
// flag). It never backtracks more than one byte.
type pathScanner struct {
	s string
	i int
}

func (sc *pathScanner) skipSep() {
	for sc.i < len(sc.s) {
		switch sc.s[sc.i] {
		case ' ', '\t', '\n', '\r', '\f', ',':
			sc.i++
		default:
			return
		}
	}
}

// number scans one number: sign, integer part, fraction, exponent. A second
// '.' starts a new number (SVG allows ".5.5").
func (sc *pathScanner) number() (float64, bool) {
	sc.skipSep()
	start := sc.i
	if sc.i < len(sc.s) && (sc.s[sc.i] == '+' || sc.s[sc.i] == '-') {
		sc.i++
	}
	digits := false
	for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
		sc.i++
		digits = true
	}
	if sc.i < len(sc.s) && sc.s[sc.i] == '.' {
		sc.i++
		for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
			sc.i++
			digits = true
		}
	}
	if !digits {
		sc.i = start
		return 0, false
	}
	if sc.i < len(sc.s) && (sc.s[sc.i] == 'e' || sc.s[sc.i] == 'E') {
		j := sc.i + 1
		if j < len(sc.s) && (sc.s[j] == '+' || sc.s[j] == '-') {
			j++
		}
		if j < len(sc.s) && sc.s[j] >= '0' && sc.s[j] <= '9' {
			for j < len(sc.s) && sc.s[j] >= '0' && sc.s[j] <= '9' {
				j++
			}
			sc.i = j
		}
	}
	v, ok := parseNumber(sc.s[start:sc.i])
	return v, ok
}

// flag scans an arc flag: exactly one '0' or '1' byte.
func (sc *pathScanner) flag() (bool, bool) {
	sc.skipSep()
	if sc.i >= len(sc.s) {
		return false, false
	}
	switch sc.s[sc.i] {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}

// command scans the next command letter, or 0 when the next token is a number
// (implicit repetition) or the input is exhausted/invalid.
func (sc *pathScanner) command() byte {
	sc.skipSep()
	if sc.i >= len(sc.s) {
		return 0
	}
	c := sc.s[sc.i]
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		sc.i++
		return c
	}
	return 0
}

// parsePathData parses SVG path data into a user-space path. On the first
// error it returns the segments parsed so far (SVG error handling: render the
// valid prefix). The result is never nil.
func parsePathData(d string) *render.Path {
	p := &render.Path{}
	sc := &pathScanner{s: d}
	var (
		cmd        byte    // current command (for implicit repetition)
		cx, cy     float64 // current point
		sx, sy     float64 // subpath start (for Z and relative after Z)
		lastCX     float64 // last cubic control (for S)
		lastCY     float64
		lastQX     float64 // last quadratic control, absolute (for T)
		lastQY     float64
		havePrevC  bool // previous segment was C/S (S reflects)
		havePrevQ  bool // previous segment was Q/T (T reflects)
		haveMoveTo bool
	)
	num := func(n int) ([]float64, bool) {
		out := make([]float64, n)
		for i := range out {
			v, ok := sc.number()
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	}
	for {
		c := sc.command()
		if c == 0 {
			sc.skipSep()
			if sc.i >= len(sc.s) || cmd == 0 {
				return p
			}
			c = cmd // implicit repetition
			switch c {
			case 'M':
				c = 'L'
			case 'm':
				c = 'l'
			case 'Z', 'z':
				// closepath takes no arguments, so repeating it implicitly
				// consumes nothing and the scanner never advances -- an
				// infinite loop appending Close() forever. SVG's path grammar
				// has no implicit repetition for closepath either (it is
				// followed by a moveto or the end of the data), so stopping
				// here is both the correct parse and the terminating one.
				//
				// Found by fuzzing: `<path d="M0 0Z0 0l 0 0">`, 64 bytes,
				// hung inside the public Parse.
				return p
			}
		}
		rel := c >= 'a'
		uc := c
		if rel {
			uc = c - 'a' + 'A'
		}
		if uc != 'M' && !haveMoveTo {
			return p // path data must start with a moveto
		}
		prevC, prevQ := havePrevC, havePrevQ
		havePrevC, havePrevQ = false, false
		switch uc {
		case 'M':
			a, ok := num(2)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
			}
			cx, cy = a[0], a[1]
			sx, sy = cx, cy
			p.MoveTo(cx, cy)
			haveMoveTo = true
		case 'L':
			a, ok := num(2)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
			}
			cx, cy = a[0], a[1]
			p.LineTo(cx, cy)
		case 'H':
			a, ok := num(1)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
			}
			cx = a[0]
			p.LineTo(cx, cy)
		case 'V':
			a, ok := num(1)
			if !ok {
				return p
			}
			if rel {
				a[0] += cy
			}
			cy = a[0]
			p.LineTo(cx, cy)
		case 'C':
			a, ok := num(6)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
				a[2] += cx
				a[3] += cy
				a[4] += cx
				a[5] += cy
			}
			p.CubeTo(a[0], a[1], a[2], a[3], a[4], a[5])
			lastCX, lastCY = a[2], a[3]
			cx, cy = a[4], a[5]
			havePrevC = true
		case 'S':
			a, ok := num(4)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
				a[2] += cx
				a[3] += cy
			}
			c1x, c1y := cx, cy
			if prevC {
				c1x, c1y = 2*cx-lastCX, 2*cy-lastCY
			}
			p.CubeTo(c1x, c1y, a[0], a[1], a[2], a[3])
			lastCX, lastCY = a[0], a[1]
			cx, cy = a[2], a[3]
			havePrevC = true
		case 'Q':
			a, ok := num(4)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
				a[2] += cx
				a[3] += cy
			}
			quadTo(p, cx, cy, a[0], a[1], a[2], a[3])
			lastQX, lastQY = a[0], a[1]
			cx, cy = a[2], a[3]
			havePrevQ = true
		case 'T':
			a, ok := num(2)
			if !ok {
				return p
			}
			if rel {
				a[0] += cx
				a[1] += cy
			}
			qx, qy := cx, cy
			if prevQ {
				qx, qy = 2*cx-lastQX, 2*cy-lastQY
			}
			quadTo(p, cx, cy, qx, qy, a[0], a[1])
			lastQX, lastQY = qx, qy
			cx, cy = a[0], a[1]
			havePrevQ = true
		case 'A':
			a, ok := num(3)
			if !ok {
				return p
			}
			large, ok1 := sc.flag()
			sweep, ok2 := sc.flag()
			if !ok1 || !ok2 {
				return p
			}
			end, ok := num(2)
			if !ok {
				return p
			}
			if rel {
				end[0] += cx
				end[1] += cy
			}
			arcSegments(p, cx, cy, a[0], a[1], a[2], large, sweep, end[0], end[1])
			cx, cy = end[0], end[1]
		case 'Z':
			p.Close()
			cx, cy = sx, sy
		default:
			return p // unknown command: stop, keep prefix
		}
		cmd = c
	}
}

// quadTo appends a quadratic Bézier (control qx,qy) as its exact cubic
// elevation: C1 = P0 + 2/3(Q-P0), C2 = P2 + 2/3(Q-P2).
func quadTo(p *render.Path, x0, y0, qx, qy, x2, y2 float64) {
	p.CubeTo(
		x0+2.0/3.0*(qx-x0), y0+2.0/3.0*(qy-y0),
		x2+2.0/3.0*(qx-x2), y2+2.0/3.0*(qy-y2),
		x2, y2,
	)
}
