package css

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/colornames"
)

// This file is the engine's SINGLE CSS colour grammar. Every colour that reaches
// the paint stack — from the HTML cascade, from a shorthand component, from an
// SVG presentation attribute, from inside a filter function — resolves here, so
// there is exactly one answer to "does this engine accept that colour?".
//
// It used to be two answers. pkg/css carried a hand-written parser covering
// #rgb, #rrggbb, rgb(r,g,b) and about eight named keywords, while pkg/svg
// carried a complete CSS Color 4 implementation. The consequence was not a
// cosmetic inconsistency: an alpha-bearing value such as rgba(0,0,0,0.9) failed
// the cascade's parser, the whole declaration was DROPPED per CSS error
// handling, and the element painted NOTHING — measured at 0 non-white pixels
// where the same box with background:black painted 6400. The SVG grammar,
// meanwhile, parsed that value correctly a package away.
//
// The two were merged in this direction (svg's grammar moved into css, and
// pkg/svg delegates here) rather than the reverse because pkg/svg already
// depends on pkg/css while pkg/css depends on no internal package, so it is the
// only direction that does not invert the layering or introduce a cycle.

// namedColors is the complete CSS Color Module Level 4 §6.1 named-color keyword
// table (147 colour keywords plus "transparent"). The 147 colours match the SVG
// 1.1 / CSS3 extended keyword set shipped by golang.org/x/image's colornames
// package (already a direct dependency), including both spellings of the seven
// gray/grey aliases. "rebeccapurple" (added in CSS Color 4) and "transparent"
// (rgba(0,0,0,0)) are not part of that table and are added here explicitly.
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

// ParseColorValue parses one complete CSS colour value: a named colour keyword
// (case-insensitive, the full CSS Color 4 table), a hex colour (#rgb, #rgba,
// #rrggbb, #rrggbbaa), or an rgb()/rgba()/hsl()/hsla() functional notation
// (comma or space syntax, integer or percentage channels, optional alpha as a
// fourth argument or after a "/"). The returned alpha is live — it reaches the
// rasterizer and composites.
//
// ok is false for anything unrecognized, including "url(...)" paint-server
// references, so the caller can degrade per CSS: a declaration whose value does
// not parse is dropped and the previous value stands.
//
// It is exported for consumers that must resolve a colour appearing INSIDE
// another property's value rather than as a declaration of its own, where the
// cascade's own parsing never sees it: `filter: drop-shadow(red 2px 2px)` is the
// case that forced it. pkg/filtereffects deliberately leaves such a colour
// unparsed (it has no colour grammar and no document), so the caller must resolve
// it, and duplicating a second hex/rgb/named parser to do so would be a silent
// divergence from what every other colour in the engine accepts.
func ParseColorValue(s string) (color.RGBA, bool) {
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

// parseColor reads a colour from a tokenizer positioned at the start of a value.
// It exists because the cascade and the shorthand expander read colours out of
// token streams; every such call site passes a tokenizer over one ALREADY-SPLIT
// component, so the whole remaining input is the colour and handing it to the
// string grammar is exact rather than approximate. Keeping this wrapper (instead
// of rewriting ~15 call sites) is what lets the grammar live in one place
// without a token-level duplicate of it.
func parseColor(tz *tokenizer) (color.RGBA, bool) {
	return ParseColorValue(tz.rest())
}

// parseColorToken reads exactly ONE colour from a tokenizer that may hold more
// value after it, leaving the tokenizer positioned just past the colour and
// rewinding it untouched on failure.
//
// parseColor above cannot serve this: it hands tz.rest() to the string grammar,
// which is exact only when the whole remainder is the colour. `box-shadow` is
// the case that is not — its grammar is an && list, so a colour can sit in the
// middle (`2px 3px inset red`) or at the front (`inset red 2px 3px`), and
// consuming the rest would swallow the lengths and reject the declaration.
//
// A colour is a single token — a #hash or a keyword ident — unless it is a
// function, where the whole rgb(…)/hsl(…) call including its parentheses is one
// colour. The nesting counter walks to the matching ")" so a comma or a nested
// function inside the arguments does not end it early.
func parseColorToken(tz *tokenizer) (color.RGBA, bool) {
	start := tz.pos
	tok := nextNonWhitespace(tz)
	if tok.Kind != TokenHash && tok.Kind != TokenIdent {
		tz.pos = start
		return color.RGBA{}, false
	}
	if tok.Kind == TokenIdent {
		save := tz.pos
		if nextNonWhitespace(tz).Kind == TokenLParen {
			for depth := 1; depth > 0; {
				switch tz.next().Kind {
				case TokenLParen:
					depth++
				case TokenRParen:
					depth--
				case TokenEOF:
					tz.pos = start
					return color.RGBA{}, false
				}
			}
		} else {
			tz.pos = save // a bare keyword: the colour was just the ident
		}
	}
	c, ok := ParseColorValue(strings.TrimSpace(tz.src[start:tz.pos]))
	if !ok {
		tz.pos = start
	}
	return c, ok
}

// parseHexColor parses #rgb, #rgba, #rrggbb, and #rrggbbaa forms. Lengths other
// than 3/4/6/8 nibbles, and any non-hex digit, are rejected.
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

// parseFunctionalColor parses rgb()/rgba()/hsl()/hsla() notation. s is already
// lower-cased and trimmed.
func parseFunctionalColor(s string) (color.RGBA, bool) {
	isHSL := strings.HasPrefix(s, "hsl")
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return color.RGBA{}, false
	}
	inner := s[open+1 : len(s)-1]

	// Split on "/" first to separate a trailing alpha, then split the
	// remainder on commas/whitespace for the channel arguments. Doing it in
	// this order means the modern space syntax and the legacy comma syntax
	// share one code path: both end up as three or four fields.
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
		alpha = clampFloat(a, 0, 1)
	}

	if isHSL {
		h, ok1 := parseColorNumber(strings.TrimSuffix(fields[0], "deg"))
		sat, ok2 := parsePercent(fields[1])
		l, ok3 := parsePercent(fields[2])
		if !ok1 || !ok2 || !ok3 {
			return color.RGBA{}, false
		}
		return hslToRGBA(h, clampFloat(sat, 0, 1), clampFloat(l, 0, 1), alpha), true
	}

	r, ok1 := parsePercentOrNumber(fields[0], 255)
	g, ok2 := parsePercentOrNumber(fields[1], 255)
	b, ok3 := parsePercentOrNumber(fields[2], 255)
	if !ok1 || !ok2 || !ok3 {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(clampFloat(math.Round(r), 0, 255)),
		G: uint8(clampFloat(math.Round(g), 0, 255)),
		B: uint8(clampFloat(math.Round(b), 0, 255)),
		A: uint8(math.Round(alpha * 255)),
	}, true
}

// parseColorNumber parses one number from a colour component. Non-finite results
// are rejected: strconv.ParseFloat accepts Go's own "nan"/"inf" spellings, which
// are not CSS numbers, and an overflowing literal such as 1e400 also comes back
// as ±Inf — either would poison the clamp-and-round arithmetic below.
func parseColorNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// parsePercentOrNumber parses a channel value that is either a bare number
// (already in the target [0,scale] range) or a percentage of scale.
func parsePercentOrNumber(s string, scale float64) (float64, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseColorNumber(strings.TrimSuffix(s, "%"))
		if !ok {
			return 0, false
		}
		return v / 100 * scale, true
	}
	return parseColorNumber(s)
}

// parsePercent parses a required percentage into [0,1] (e.g. "50%" -> 0.5).
func parsePercent(s string) (float64, bool) {
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, ok := parseColorNumber(strings.TrimSuffix(s, "%"))
	if !ok {
		return 0, false
	}
	return v / 100, true
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// hslToRGBA converts HSL (h in degrees, s/l in [0,1]) plus a [0,1] alpha to RGBA
// using the standard C/X/m formula (CSS Color 4 §7.1).
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
		R: uint8(clampFloat(math.Round((r1+m)*255), 0, 255)),
		G: uint8(clampFloat(math.Round((g1+m)*255), 0, 255)),
		B: uint8(clampFloat(math.Round((b1+m)*255), 0, 255)),
		A: uint8(math.Round(alpha * 255)),
	}
}
