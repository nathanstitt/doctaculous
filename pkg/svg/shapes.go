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
	// lacuna value for every attribute used below) and logging when a
	// percentage was used, since it resolves against 0 rather than the
	// real viewport here.
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
			return 0
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
		rx, ry := length("rx"), length("ry")
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

// finite reports whether v is neither NaN nor ±Inf. Non-finite lengths are
// reachable today only via parseNumber's acceptance of strconv's "nan"/"inf"
// literals (a known upstream wart, not fixed here); they must never reach the
// arc math below, which would either silently fail every comparison (NaN) or
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
	// A non-finite radius (NaN, or ±Inf via parseNumber's acceptance of
	// strconv's "inf"/"nan" literals) can't be clamped or used in the arc
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
// valid prefix, per SVG's list error-handling rule. A non-finite coordinate
// (NaN/±Inf, reachable via parseNumber's acceptance of strconv's "nan"/"inf"
// literals) is treated the same way: truncate at the first bad coordinate
// pair and render the valid prefix, rather than dropping the whole shape —
// consistent with the odd-trailing-number rule above and with how
// parsePathData keeps whatever prefix parsed cleanly on the first error.
// Empty or wholly-invalid input (nothing usable in the prefix) yields nil.
func pointsPath(points string, closed bool, logf func(string, ...any)) *render.Path {
	nums := parseNumberList(points)
	if len(nums)%2 != 0 {
		nums = nums[:len(nums)-1]
	}
	for i, v := range nums {
		if !finite(v) {
			// Truncate to the last complete, finite coordinate pair
			// before this one.
			nums = nums[:i-i%2]
			logf("svg: non-finite coordinate in points list, rendering valid prefix")
			break
		}
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
