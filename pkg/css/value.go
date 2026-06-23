package css

// LengthUnit is the unit of a CSS length value.
type LengthUnit int

const (
	UnitPx LengthUnit = iota
	UnitPt
	UnitEm
	UnitPercent
	UnitAuto // the "auto" keyword, modeled as a length so width/margin can carry it
)

// Length is a CSS length value: a magnitude plus its unit. Percentages and the
// "auto" keyword are represented here too so a single type covers width/height/
// margin/padding values. Resolution to absolute points (resolving em/% against a
// context) happens in the layout engine, not here.
type Length struct {
	Value float64
	Unit  LengthUnit
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
