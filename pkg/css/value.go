package css

import (
	"image/color"
	"strconv"
	"strings"
)

// LengthUnit is the unit of a CSS length value.
type LengthUnit int

const (
	UnitPx LengthUnit = iota
	UnitPt
	UnitEm
	UnitPercent
	UnitAuto    // the "auto" keyword, modeled as a length so width/margin can carry it
	UnitContent // the flex-basis "content" keyword (only produced/read by flex-basis)
)

// Length is a CSS length value: a magnitude plus its unit. Percentages and the
// "auto" keyword are represented here too so a single type covers width/height/
// margin/padding values. Resolution to absolute points (resolving em/% against a
// context) happens in the layout engine, not here.
type Length struct {
	Value float64
	Unit  LengthUnit
}

// parseTextDecorationLine extracts the supported text-decoration line from a value
// (the longhand or the shorthand). It returns "underline" if the underline keyword is
// present, "line-through" if that keyword is present, else "none" — the other line
// keyword (overline) and the color/style/thickness tokens are not modeled, so a value
// carrying neither reads as none. When both underline and line-through are present the
// first matched keyword wins (a run rarely has both; the glyph flags are independent).
func parseTextDecorationLine(value string) string {
	for _, f := range strings.Fields(strings.ToLower(value)) {
		if f == "underline" {
			return "underline"
		}
		if f == "line-through" {
			return "line-through"
		}
	}
	return "none"
}

// parseLength interprets a single token as a length. A unitless 0 is a valid
// zero length; the "auto" keyword yields UnitAuto. ok is false for tokens that
// are not lengths (e.g. a color keyword).
func parseLength(tok Token) (Length, bool) {
	switch tok.Kind {
	case TokenDimension:
		switch tok.Unit {
		case "px":
			return Length{tok.Num, UnitPx}, true
		case "pt":
			return Length{tok.Num, UnitPt}, true
		case "em", "rem":
			return Length{tok.Num, UnitEm}, true
		default:
			return Length{}, false
		}
	case TokenPercent:
		return Length{tok.Num, UnitPercent}, true
	case TokenNumber:
		if tok.Num == 0 {
			return Length{0, UnitPx}, true
		}
		return Length{}, false // non-zero unitless is not a valid length
	case TokenIdent:
		if tok.Text == "auto" {
			return Length{0, UnitAuto}, true
		}
	}
	return Length{}, false
}

// namedColors is the minimal CSS named-color set this sub-project recognizes.
// Extend as needed; unknown names fail parseColor (the declaration is dropped).
var namedColors = map[string]color.RGBA{
	"black":       {0, 0, 0, 255},
	"white":       {255, 255, 255, 255},
	"red":         {255, 0, 0, 255},
	"green":       {0, 128, 0, 255},
	"blue":        {0, 0, 255, 255},
	"gray":        {128, 128, 128, 255},
	"silver":      {192, 192, 192, 255},
	"transparent": {0, 0, 0, 0},
}

// parseColor reads a color value from the tokenizer: a #hex hash, an rgb(r,g,b)
// function, or a named color. ok is false for anything unrecognized, so the
// caller drops the declaration.
func parseColor(tz *tokenizer) (color.RGBA, bool) {
	tok := tz.next()
	switch tok.Kind {
	case TokenHash:
		return parseHex(tok.Text)
	case TokenIdent:
		if strings.ToLower(tok.Text) == "rgb" {
			return parseRGBFunc(tz)
		}
		c, ok := namedColors[strings.ToLower(tok.Text)]
		return c, ok
	}
	return color.RGBA{}, false
}

func parseHex(h string) (color.RGBA, bool) {
	switch len(h) {
	case 3:
		r := hexNibble(h[0])
		g := hexNibble(h[1])
		b := hexNibble(h[2])
		if r < 0 || g < 0 || b < 0 {
			return color.RGBA{}, false
		}
		return color.RGBA{uint8(r*16 + r), uint8(g*16 + g), uint8(b*16 + b), 255}, true
	case 6:
		r, err1 := strconv.ParseUint(h[0:2], 16, 8)
		g, err2 := strconv.ParseUint(h[2:4], 16, 8)
		b, err3 := strconv.ParseUint(h[4:6], 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return color.RGBA{}, false
		}
		return color.RGBA{uint8(r), uint8(g), uint8(b), 255}, true
	}
	return color.RGBA{}, false
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// parseRGBFunc parses the remainder of rgb(r,g,b) after the "rgb" ident, with the
// tokenizer positioned at the "(".
func parseRGBFunc(tz *tokenizer) (color.RGBA, bool) {
	if tz.next().Kind != TokenLParen {
		return color.RGBA{}, false
	}
	var comps [3]uint8
	// Each component is a single TokenNumber. A negative value must be written
	// with the sign adjacent to the digit (e.g. -10); the tokenizer emits a bare
	// "-" as a delimiter otherwise, which correctly fails the kind check below.
	for i := 0; i < 3; i++ {
		// skip whitespace, read a number
		tok := nextNonWhitespace(tz)
		if tok.Kind != TokenNumber {
			return color.RGBA{}, false
		}
		comps[i] = clampByte(tok.Num)
		if i < 2 {
			if nextNonWhitespace(tz).Kind != TokenComma {
				return color.RGBA{}, false
			}
		}
	}
	if nextNonWhitespace(tz).Kind != TokenRParen {
		return color.RGBA{}, false
	}
	return color.RGBA{comps[0], comps[1], comps[2], 255}, true
}

func nextNonWhitespace(tz *tokenizer) Token {
	for {
		t := tz.next()
		if t.Kind != TokenWhitespace {
			return t
		}
	}
}

func clampByte(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f)
}
