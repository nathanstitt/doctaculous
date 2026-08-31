package svg

import (
	"math"
	"strconv"
	"strings"
)

// User units follow the engine's 96dpi px-as-pt scalar (see pkg/css/pagesize.go):
// 1 SVG user unit = 1 CSS px = 1 layout pt, 96 per inch.
const pxPerIn = 96.0

// parseNumber parses one SVG number (int/decimal/scientific, optional sign).
// SVG's <number> grammar has no NaN/Infinity literals, so a result that
// parses but is non-finite is rejected: strconv.ParseFloat accepts Go's own
// spellings ("nan", "inf", "+Inf", "infinity", case-insensitively) which are
// not valid SVG numbers, and an overflowing literal like "1e400" also comes
// back as ±Inf (ParseFloat returns it alongside a range error, but nothing
// here should depend on checking err for that — the finite check catches it
// directly). Every caller in this package (parseNumberList, and transitively
// every attribute parser built on it) relies on this to keep a non-finite
// value from ever reaching downstream matrix/geometry math.
func parseNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// parseNumberList splits s on commas and whitespace and parses each token as a
// number. Any bad token makes the whole list nil (the attribute is ignored, per
// SVG's error handling for list attributes).
func parseNumberList(s string) []float64 {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
	if len(fields) == 0 {
		return nil
	}
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, ok := parseNumber(f)
		if !ok {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// parseLength parses an SVG length with an optional unit into user units.
// Percentages resolve against ref. em/ex use the UA default font metrics
// (16px em, 8px ex), which matches how a unitless browser default resolves
// them at the root. Real resolution needs a font-size on the resolved style;
// the CSS cascade landed without one because font-size is only meaningful
// once SVG text ships, so these stay fixed until the text slice adds it.
func parseLength(s string, ref float64) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	unit := ""
	num := s
	for _, u := range [...]string{"px", "pt", "pc", "mm", "cm", "in", "em", "ex", "%"} {
		if strings.HasSuffix(s, u) {
			unit, num = u, s[:len(s)-len(u)]
			break
		}
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	switch unit {
	case "", "px":
		return v, true
	case "pt":
		return v * pxPerIn / 72, true
	case "pc":
		return v * pxPerIn / 6, true
	case "in":
		return v * pxPerIn, true
	case "cm":
		return v * pxPerIn / 2.54, true
	case "mm":
		return v * pxPerIn / 25.4, true
	case "em":
		return v * 16, true
	case "ex":
		return v * 8, true
	case "%":
		return v / 100 * ref, true
	}
	return 0, false
}
