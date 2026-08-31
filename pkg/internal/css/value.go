package css

import (
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
	// UnitNumber is a UNITLESS number, which is a valid value for a few properties
	// (line-height most importantly) and invalid for lengths like width or margin.
	// parseLength never produces it — parseNumberOrLength does, and only the
	// properties that accept one call that — so `width: 5` keeps being rejected.
	UnitNumber
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
//
// A non-zero unitless number is NOT a length and is rejected here. The properties
// for which one is meaningful — line-height — use parseNumberOrLength instead, so
// that stays a per-property opt-in rather than a hole in every length.
func parseLength(tok Token) (Length, bool) {
	switch tok.Kind {
	case TokenDimension:
		switch tok.Unit {
		case "px":
			return Length{tok.Num, UnitPx}, true
		case "pt":
			return Length{tok.Num, UnitPt}, true
		// KNOWN DIVERGENCE: rem is folded into em here, so it resolves against
		// the ELEMENT's font size rather than the ROOT's. Measured: width:2rem
		// under a 5pt font renders 10pt where the spec requires 40pt at a 20pt
		// root. Correct only while the two happen to match.
		//
		// Modelling it properly needs a distinct UnitRem carried to the point
		// where the root font size is known, which every consumer of Length
		// would have to resolve. The CSS `filter` property does resolve rem
		// correctly (see Engine.rootFontSizePt) because its lengths are
		// resolved at paint time, where the root is reachable — so rem is
		// currently right for filter and wrong for everything else, which is
		// surprising enough to be worth stating at the fold itself.
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

// The CSS colour grammar (named keywords, every hex form, and the
// rgb()/rgba()/hsl()/hsla() functions) lives in color.go — it is shared with
// pkg/svg so the engine has exactly one definition of a valid colour.

func nextNonWhitespace(tz *tokenizer) Token {
	for {
		t := tz.next()
		if t.Kind != TokenWhitespace {
			return t
		}
	}
}

// parseNumberOrLength is parseLength plus the unitless-number form, for the
// properties whose grammar accepts a bare number.
//
// line-height is the case that forced it: `line-height: 1.2` is the commonest
// spelling of the property, and parseLength rejects it (correctly — `width: 1.2` is
// invalid). Before this the declaration was simply dropped, so every text block used
// the font-metric height and the property appeared to do nothing at all.
//
// The distinction is not cosmetic: a NUMBER inherits as a number, so a child with a
// larger font gets a proportionally larger line box, whereas an em value inherits the
// computed length. Keeping the unit distinct downstream is what makes that possible.
func parseNumberOrLength(tok Token) (Length, bool) {
	if tok.Kind == TokenNumber && tok.Num != 0 {
		return Length{tok.Num, UnitNumber}, true
	}
	return parseLength(tok)
}
