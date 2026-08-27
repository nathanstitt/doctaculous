package svg

import (
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/colornames"
)

// namedColors is the complete CSS Color Module Level 4 §6.1 named-color
// keyword table (147 color keywords plus "transparent"). The 147 colors match
// the SVG 1.1 / CSS3 extended keyword set shipped by golang.org/x/image's
// colornames package (already a direct dependency), including both spellings
// of the seven gray/grey aliases. "rebeccapurple" (added in CSS Color 4) and
// "transparent" (rgba(0,0,0,0)) are not part of that table and are added
// here explicitly.
var namedColors = buildNamedColors()

func buildNamedColors() map[string]color.RGBA {
	m := make(map[string]color.RGBA, len(colornames.Map)+2)
	for name, c := range colornames.Map {
		m[name] = c
	}
	m["rebeccapurple"] = color.RGBA{0x66, 0x33, 0x99, 0xff}
	m["transparent"] = color.RGBA{0x00, 0x00, 0x00, 0x00}
	return m
}

// parseColorValue parses an SVG/CSS color value: a named color keyword
// (case-insensitive, the full CSS Color 4 table), a hex color (#rgb, #rgba,
// #rrggbb, #rrggbbaa), or an rgb()/rgba()/hsl()/hsla() functional notation
// (comma or space syntax, integer or percentage channels, optional alpha as
// a fourth argument or after a "/"). "url(...)" paint-server references and
// any unrecognized value return ok=false so the caller can degrade
// gracefully (e.g. skip the fill/stroke).
func parseColorValue(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return color.RGBA{}, false
	}
	if s[0] == '#' {
		return parseHexColor(s)
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "rgb") || strings.HasPrefix(lower, "hsl") {
		return parseFunctionalColor(lower)
	}
	c, ok := namedColors[lower]
	return c, ok
}

// parseHexColor parses #rgb, #rgba, #rrggbb, and #rrggbbaa forms.
func parseHexColor(s string) (color.RGBA, bool) {
	hex := s[1:]
	expand := func(c byte) (byte, bool) {
		v, ok := hexNibble(c)
		if !ok {
			return 0, false
		}
		return v<<4 | v, true
	}
	switch len(hex) {
	case 3, 4:
		r, ok1 := expand(hex[0])
		g, ok2 := expand(hex[1])
		b, ok3 := expand(hex[2])
		a := byte(0xff)
		ok4 := true
		if len(hex) == 4 {
			a, ok4 = expand(hex[3])
		}
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return color.RGBA{}, false
		}
		return color.RGBA{r, g, b, a}, true
	case 6, 8:
		r, ok1 := hexByte(hex[0], hex[1])
		g, ok2 := hexByte(hex[2], hex[3])
		b, ok3 := hexByte(hex[4], hex[5])
		a := byte(0xff)
		ok4 := true
		if len(hex) == 8 {
			a, ok4 = hexByte(hex[6], hex[7])
		}
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return color.RGBA{}, false
		}
		return color.RGBA{r, g, b, a}, true
	}
	return color.RGBA{}, false
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func hexByte(hi, lo byte) (byte, bool) {
	h, ok1 := hexNibble(hi)
	l, ok2 := hexNibble(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

// parseFunctionalColor parses rgb()/rgba()/hsl()/hsla() notation. s is
// already lower-cased.
func parseFunctionalColor(s string) (color.RGBA, bool) {
	isHSL := strings.HasPrefix(s, "hsl")
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return color.RGBA{}, false
	}
	inner := s[open+1 : len(s)-1]

	// Split on "/" first to separate a trailing alpha, then split the
	// remainder on commas/whitespace for the channel arguments.
	var alphaPart string
	hasAlphaPart := false
	if slash := strings.IndexByte(inner, '/'); slash >= 0 {
		alphaPart = strings.TrimSpace(inner[slash+1:])
		inner = inner[:slash]
		hasAlphaPart = true
	}

	fields := strings.FieldsFunc(inner, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
	if hasAlphaPart {
		if alphaPart == "" {
			return color.RGBA{}, false
		}
		fields = append(fields, alphaPart)
	}

	if len(fields) != 3 && len(fields) != 4 {
		return color.RGBA{}, false
	}

	alpha := 1.0
	if len(fields) == 4 {
		a, ok := parsePercentOrNumber(fields[3], 1.0)
		if !ok {
			return color.RGBA{}, false
		}
		alpha = clamp(a, 0, 1)
	}

	if isHSL {
		h, ok1 := parseNumber(strings.TrimSuffix(fields[0], "deg"))
		s2, ok2 := parsePercent(fields[1])
		l, ok3 := parsePercent(fields[2])
		if !ok1 || !ok2 || !ok3 {
			return color.RGBA{}, false
		}
		return hslToRGBA(h, clamp(s2, 0, 1), clamp(l, 0, 1), alpha), true
	}

	r, ok1 := parsePercentOrNumber(fields[0], 255)
	g, ok2 := parsePercentOrNumber(fields[1], 255)
	b, ok3 := parsePercentOrNumber(fields[2], 255)
	if !ok1 || !ok2 || !ok3 {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(clamp(math.Round(r), 0, 255)),
		G: uint8(clamp(math.Round(g), 0, 255)),
		B: uint8(clamp(math.Round(b), 0, 255)),
		A: uint8(math.Round(alpha * 255)),
	}, true
}

// parsePercentOrNumber parses a channel value that is either a bare number
// (already in the target [0,scale] range) or a percentage of scale.
func parsePercentOrNumber(s string, scale float64) (float64, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseNumber(strings.TrimSuffix(s, "%"))
		if !ok {
			return 0, false
		}
		return v / 100 * scale, true
	}
	return parseNumber(s)
}

// parsePercent parses a required percentage into [0,1] (e.g. "50%" -> 0.5).
func parsePercent(s string) (float64, bool) {
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, ok := parseNumber(strings.TrimSuffix(s, "%"))
	if !ok {
		return 0, false
	}
	return v / 100, true
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// hslToRGBA converts HSL (h in degrees, s/l in [0,1]) plus a [0,1] alpha to
// RGBA using the standard C/X/m formula (CSS Color 4 §7.1).
func hslToRGBA(h, s, l, alpha float64) color.RGBA {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}

	return color.RGBA{
		R: uint8(clamp(math.Round((r1+m)*255), 0, 255)),
		G: uint8(clamp(math.Round((g1+m)*255), 0, 255)),
		B: uint8(clamp(math.Round((b1+m)*255), 0, 255)),
		A: uint8(math.Round(alpha * 255)),
	}
}
