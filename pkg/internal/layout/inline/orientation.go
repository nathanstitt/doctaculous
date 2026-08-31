package inline

import (
	"math"
	"unicode"
)

// QuarterTurn is the clockwise rotation a sideways glyph takes in a vertical line:
// 90 degrees, so the glyph's own baseline runs down the page.
//
// The sign convention is the one the paint stage composes in — inside the em scale,
// applied about the glyph's own origin. At +QuarterTurn the em-space advance direction
// (1,0) maps to page (0,+1), so successive glyphs march DOWN, and em-space up (0,1)
// maps to page (+1,0), so glyph tops face right. That is what CSS specifies for a
// vertical writing mode, and it is measured rather than reasoned: the em scale carries
// a -1 Y flip that invites a compensating negation, and adding one turns the text
// upside down while still painting plausibly.
const QuarterTurn = math.Pi / 2

// GlyphRotation resolves CSS text-orientation for one glyph in a vertical line,
// returning its clockwise rotation in radians and whether it ended up sideways.
//
//   - "upright"  — no glyph rotates. This is what a short Latin label wants.
//   - "sideways" — every glyph rotates a quarter turn, including CJK.
//   - "mixed"    — the INITIAL value: upright scripts stay upright, everything else
//     rotates. This is the CJK default, where Han/Kana/Hangul read down the page in
//     their normal orientation and embedded Latin lies on its side.
//
// An unrecognized or empty value is treated as mixed, matching the initial value. Both
// the CSS cascade and the SVG cascade normalize to these spellings and store only
// values they recognized, so the fallthrough is a defensive default rather than a
// parsing path.
//
// The `sideways` return exists because the caller must ALSO change which advance it
// walks: a recumbent glyph advances the pen by its horizontal extent, an upright one by
// the font's vertical advance. Getting that wrong spaces sideways text one em per
// letter, which is the fixed-pitch look of `upright` — a difference that survives
// review because it renders plausibly.
func GlyphRotation(orient string, runes []rune) (radians float64, sideways bool) {
	switch orient {
	case "upright":
		return 0, false
	case "sideways":
		return QuarterTurn, true
	}
	if UprightInVertical(runes) {
		return 0, false
	}
	return QuarterTurn, true
}

// UprightInVertical reports whether a glyph's runes belong to a script that stays
// upright in a vertical line under text-orientation: mixed.
//
// This APPROXIMATES the Unicode Vertical_Orientation property (UAX #50), which is the
// spec's actual authority and which neither the standard library nor the textlayout
// dependency ships a table for. Vendoring the real table is the correct long-term fix
// and is recorded as such in docs/CSS-LAYOUT.md; what is here covers the scripts a
// vertical line is actually set in — Han, Hiragana, Katakana, Hangul, Bopomofo, Yi —
// plus the CJK punctuation and full-width forms that stdlib's script tables exclude but
// which must stay upright with the text they punctuate.
//
// Where it differs from UAX #50 it errs toward ROTATING, which is the safer error: a
// wrongly-rotated glyph is visibly odd, whereas a wrongly-upright one in a Latin run
// silently produces the fixed-pitch stacking that `upright` is for, and reads as
// intentional.
//
// A glyph with no runes (a synthetic bullet, an inline-box edge) is not upright; it has
// no script to consult, and rotating a zero-ink glyph is a no-op anyway.
func UprightInVertical(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		if !uprightRune(r) {
			return false
		}
	}
	return true
}

// uprightRune is UprightInVertical's per-rune test. See that function for what this
// approximates and why.
func uprightRune(r rune) bool {
	switch {
	// The blocks stdlib's script tables miss: CJK symbols and punctuation (U+3000-303F,
	// the ideographic space, brackets and full stop), and the halfwidth/fullwidth forms
	// (U+FF00-FFEF) whose full-width members are set upright with CJK text.
	case r >= 0x3000 && r <= 0x303F, r >= 0xFF00 && r <= 0xFFEF:
		return true
	}
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Bopomofo, r) ||
		unicode.Is(unicode.Yi, r)
}

// VerticalAdvancePt is a glyph's advance down the page, in points, for a glyph that
// stands UPRIGHT in a vertical line. A sideways glyph advances by its horizontal extent
// instead and must not call this.
//
// There is deliberately no fallback to the horizontal advance for a glyph that has a
// face: pkg/font synthesizes one em for any face stating no vertical metric, so the
// not-ok path is unreachable there. Falling back would space a vertical line by how
// WIDE each letter is — an 'i' and a 'W' getting different vertical gaps — which looks
// plausible enough to survive review and is wrong.
//
// A glyph with no face (whitespace, .notdef) keeps its own advance; there is no font to
// ask.
func VerticalAdvancePt(g *Glyph) float64 {
	if g.Face == nil {
		return g.Advance
	}
	adv, ok := g.Face.GlyphVAdvance(g.GID)
	if !ok {
		return g.Advance
	}
	return adv * g.SizePt
}
