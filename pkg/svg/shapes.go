package svg

import (
	"math"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// shapePath converts an SVG basic-shape element (rect, circle, ellipse, line,
// polyline, polygon, path) into device-space geometry. It returns nil when el
// is not one of those elements, or when the shape is degenerate per the SVG
// spec (a zero or negative width/height/r/rx/ry "disables rendering of the
// element" — SVG 1.1 §9.2, §9.3, §9.4). logf may be nil.
//
// Percentage lengths on shape attributes resolve against 0 in this pass: real
// SVG viewport-relative resolution needs the style cascade (a later PR); a
// percentage here is logged so the degradation is visible rather than silent.
func shapePath(el *element, logf func(string, ...any)) *render.Path {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if el == nil || el.space != svgNS {
		return nil
	}

	// length resolves an attribute as a length, defaulting to 0 (SVG's
	// lacuna value for every attribute used below) when the attribute is
	// absent, and logging when a percentage was used, since it resolves
	// against 0 rather than the real viewport here.
	//
	// A present-but-unparseable value (bad syntax, or a non-finite result —
	// parseLength itself now rejects NaN/±Inf, since SVG's <number> grammar
	// has no such literals) reports NaN rather than silently falling back to
	// 0: the finite()/finitePositive() guards below must still see and
	// reject it, exactly as they do for any other non-finite coordinate,
	// rather than have a garbled attribute quietly render as if it were
	// absent.
	length := func(name string) float64 {
		v, ok := el.attrs[name]
		if !ok {
			return 0
		}
		if strings.HasSuffix(v, "%") {
			logf("svg: %s %q on <%s> resolved against 0 (viewport-relative %% not yet supported)", name, v, el.local)
		}
		n, ok := parseLength(v, 0)
		if !ok {
			return math.NaN()
		}
		return n
	}

	switch el.local {
	case "rect":
		return rectPath(el, length, logf)
	case "circle":
		r := length("r")
		if !finitePositive(r) {
			return nil
		}
		cx, cy := length("cx"), length("cy")
		if !finite(cx) || !finite(cy) {
			logf("svg: non-finite cx/cy on <circle>, dropping shape")
			return nil
		}
		return ellipsePath(cx, cy, r, r)
	case "ellipse":
		rx, ry := ellipseRadii(el, length)
		if !finitePositive(rx) || !finitePositive(ry) {
			return nil
		}
		cx, cy := length("cx"), length("cy")
		if !finite(cx) || !finite(cy) {
			logf("svg: non-finite cx/cy on <ellipse>, dropping shape")
			return nil
		}
		return ellipsePath(cx, cy, rx, ry)
	case "line":
		x1, y1 := length("x1"), length("y1")
		x2, y2 := length("x2"), length("y2")
		if !finite(x1) || !finite(y1) || !finite(x2) || !finite(y2) {
			logf("svg: non-finite coordinate on <line>, dropping shape")
			return nil
		}
		p := &render.Path{}
		p.MoveTo(x1, y1)
		p.LineTo(x2, y2)
		return p
	case "polyline":
		return pointsPath(el.attrs["points"], false, logf)
	case "polygon":
		return pointsPath(el.attrs["points"], true, logf)
	case "path":
		return parsePathData(el.attrs["d"])
	default:
		return nil
	}
}

// finite reports whether v is neither NaN nor ±Inf. parseNumber/parseLength
// reject non-finite results outright, but shapePath's length() helper still
// surfaces a NaN sentinel for a present-but-unparseable attribute (see its
// doc comment) so that guard, not a silent fallback to 0, is what decides
// whether the shape renders; a non-finite value must never reach the arc
// math below, which would either silently fail every comparison (NaN) or
// produce an infinite path that poisons downstream layout/rasterization.
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// finitePositive reports whether v is a finite value strictly greater than
// zero — the degenerate-shape guard for width/height/r/rx/ry (SVG 1.1 §9.2,
// §9.3: a value of zero "disables rendering of the element"; a negative
// value is likewise treated as unrenderable rather than causing a mirrored
// or otherwise undefined shape).
func finitePositive(v float64) bool {
	return finite(v) && v > 0
}

// rectPath builds the path for a <rect>, honoring rx/ry per SVG 1.1 §9.2: a
// missing radius takes the other's value, both clamp to half the
// corresponding side, and a plain (non-rounded) rect is emitted when both end
// up zero.
func rectPath(el *element, length func(string) float64, logf func(string, ...any)) *render.Path {
	x, y := length("x"), length("y")
	w, h := length("width"), length("height")
	if !finitePositive(w) || !finitePositive(h) {
		return nil
	}
	if !finite(x) || !finite(y) {
		logf("svg: non-finite x/y on <rect>, dropping shape")
		return nil
	}

	_, hasRX := el.attrs["rx"]
	_, hasRY := el.attrs["ry"]
	var rx, ry float64
	if hasRX {
		rx = length("rx")
	}
	if hasRY {
		ry = length("ry")
	}
	switch {
	case hasRX && !hasRY:
		ry = rx
	case hasRY && !hasRX:
		rx = ry
	}
	// A non-finite radius (the NaN sentinel length() returns for a
	// present-but-unparseable rx/ry) can't be clamped or used in the arc
	// math sanely; treat it as no rounding rather than emit an arc to/from
	// a non-finite point.
	if !finite(rx) || !finite(ry) {
		rx, ry = 0, 0
	}
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	if rx > w/2 {
		rx = w / 2
	}
	if ry > h/2 {
		ry = h / 2
	}

	p := &render.Path{}
	if rx == 0 && ry == 0 {
		p.MoveTo(x, y)
		p.LineTo(x+w, y)
		p.LineTo(x+w, y+h)
		p.LineTo(x, y+h)
		p.Close()
		return p
	}

	p.MoveTo(x+rx, y)
	p.LineTo(x+w-rx, y)
	arcSegments(p, x+w-rx, y, rx, ry, 0, false, true, x+w, y+ry)
	p.LineTo(x+w, y+h-ry)
	arcSegments(p, x+w, y+h-ry, rx, ry, 0, false, true, x+w-rx, y+h)
	p.LineTo(x+rx, y+h)
	arcSegments(p, x+rx, y+h, rx, ry, 0, false, true, x, y+h-ry)
	p.LineTo(x, y+ry)
	arcSegments(p, x, y+ry, rx, ry, 0, false, true, x+rx, y)
	p.Close()
	return p
}

// ellipseRadii resolves an <ellipse>'s rx/ry per SVG 2 §10.4: when exactly one
// of rx/ry is present, the missing one takes the other's (already-resolved)
// value — the same "auto" defaulting rectPath already applies to <rect>'s
// rx/ry, but without rect's half-side clamp (an ellipse's radii have no
// enclosing box to clamp against). When both are absent, both resolve to 0,
// which finitePositive's degenerate-shape guard then rejects (SVG 1
// behavior: "Error in SVG 1, but not in SVG 2" per this rule only kicking in
// for the single-missing-attribute case). Presence is checked on el.attrs
// directly, not by comparing the resolved value to 0, since length() already
// defaults an absent attribute to 0 — indistinguishable from an explicit
// "0" — and only an absent attribute should trigger the substitution.
func ellipseRadii(el *element, length func(string) float64) (rx, ry float64) {
	_, hasRX := el.attrs["rx"]
	_, hasRY := el.attrs["ry"]
	rx, ry = length("rx"), length("ry")
	switch {
	case hasRX && !hasRY:
		ry = rx
	case hasRY && !hasRX:
		rx = ry
	}
	return rx, ry
}

// ellipsePath builds a closed ellipse (circle when rx == ry) as four 90° arcs
// via arcSegments, starting at (cx+rx, cy) and sweeping in the positive
// (clockwise, in SVG's y-down space) direction.
func ellipsePath(cx, cy, rx, ry float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(cx+rx, cy)
	arcSegments(p, cx+rx, cy, rx, ry, 0, false, true, cx, cy+ry)
	arcSegments(p, cx, cy+ry, rx, ry, 0, false, true, cx-rx, cy)
	arcSegments(p, cx-rx, cy, rx, ry, 0, false, true, cx, cy-ry)
	arcSegments(p, cx, cy-ry, rx, ry, 0, false, true, cx+rx, cy)
	p.Close()
	return p
}

// pointsPath builds a polyline/polygon path from a "points" attribute. An odd
// number of coordinates drops the trailing unpaired number and renders the
// valid prefix, per SVG's list error-handling rule. A bad token — including a
// non-finite one (NaN/±Inf; parseNumber itself now rejects these as
// unparseable, since SVG's <number> grammar has no such literals) — is
// treated the same way: the scan stops at the first bad token and the valid
// prefix (truncated to a whole coordinate pair) still renders, rather than
// dropping the whole shape. This mirrors how parsePathData keeps whatever
// prefix parsed cleanly on the first error, and is why this function scans
// tokens itself instead of using parseNumberList's all-or-nothing list
// parse. Empty or wholly-invalid input (nothing usable in the prefix) yields
// nil.
func pointsPath(points string, closed bool, logf func(string, ...any)) *render.Path {
	fields := strings.FieldsFunc(points, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
	nums := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, ok := parseNumber(f)
		if !ok {
			logf("svg: bad coordinate %q in points list, rendering valid prefix", f)
			break
		}
		nums = append(nums, v)
	}
	if len(nums)%2 != 0 {
		nums = nums[:len(nums)-1]
	}
	if len(nums) < 2 {
		return nil
	}
	p := &render.Path{}
	p.MoveTo(nums[0], nums[1])
	for i := 2; i+1 < len(nums); i += 2 {
		p.LineTo(nums[i], nums[i+1])
	}
	if closed {
		p.Close()
	}
	return p
}
