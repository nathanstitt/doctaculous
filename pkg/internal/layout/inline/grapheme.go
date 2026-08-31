package inline

import (
	"unicode"

	"github.com/benoitkugler/textlayout/unicodedata"
)

// Grapheme-cluster boundary detection (UAX #29, extended grapheme clusters).
//
// Mid-word breaking (overflow-wrap / word-break) must never split a user-perceived
// character: an "e" and its combining acute, a Hangul syllable's jamo, a flag's two
// regional indicators, or an emoji ZWJ sequence like the family glyph all have to move
// to the next line together. CSS Text 3 §5.3 says so explicitly ("in no case may
// characters be broken within a grapheme cluster"), and getting it wrong is visible: a
// break between a base and its mark paints a lone floating accent at the start of the
// line.
//
// The property tables come from github.com/benoitkugler/textlayout/unicodedata, which
// is already a dependency (it is the font/harfbuzz layer's own UCD). No new dependency
// is pulled in for this, and no table is hand-copied — a hand-rolled "is it a combining
// mark" heuristic would be wrong for Hangul, regional indicators, and ZWJ sequences,
// all of which are ordinary letters/symbols by category.

// gbClass is a rune's Grapheme_Cluster_Break property value. It is a small enum rather
// than the *unicode.RangeTable the UCD lookup returns so the pair table below can switch
// on it: comparing table pointers works but reads far worse, and Any/Extended_Pictographic
// are not table lookups at all.
type gbClass uint8

const (
	gbOther gbClass = iota
	gbCR
	gbLF
	gbControl
	gbExtend
	gbZWJ
	gbRegionalIndicator
	gbPrepend
	gbSpacingMark
	gbL
	gbV
	gbT
	gbLV
	gbLVT
)

// graphemeClass maps a rune to its Grapheme_Cluster_Break property. Runes with no
// assigned property are gbOther ("Any" in the UAX #29 rule table).
func graphemeClass(r rune) gbClass {
	switch unicodedata.LookupGraphemeBreakClass(r) {
	case nil:
		return gbOther
	case unicodedata.GraphemeBreakCR:
		return gbCR
	case unicodedata.GraphemeBreakLF:
		return gbLF
	case unicodedata.GraphemeBreakControl:
		return gbControl
	case unicodedata.GraphemeBreakExtend:
		return gbExtend
	case unicodedata.GraphemeBreakZWJ:
		return gbZWJ
	case unicodedata.GraphemeBreakRegional_Indicator:
		return gbRegionalIndicator
	case unicodedata.GraphemeBreakPrepend:
		return gbPrepend
	case unicodedata.GraphemeBreakSpacingMark:
		return gbSpacingMark
	case unicodedata.GraphemeBreakL:
		return gbL
	case unicodedata.GraphemeBreakV:
		return gbV
	case unicodedata.GraphemeBreakT:
		return gbT
	case unicodedata.GraphemeBreakLV:
		return gbLV
	case unicodedata.GraphemeBreakLVT:
		return gbLVT
	default:
		return gbOther
	}
}

// graphemeState is the small amount of history the two context-sensitive UAX #29 rules
// need. Everything else is decidable from the adjacent pair alone.
//
//   - GB11 (do not break inside an emoji ZWJ sequence) needs to know whether the ZWJ
//     before the current rune was itself preceded by Extended_Pictographic Extend*.
//   - GB12/GB13 (do not break between the two halves of a regional-indicator PAIR, but
//     DO break between pairs) needs the parity of the current unbroken RI run.
type graphemeState struct {
	// pictoZWJ is true when the run so far ends in Extended_Pictographic Extend* ZWJ,
	// i.e. the next rune, if Extended_Pictographic, continues an emoji ZWJ sequence.
	pictoZWJ bool
	// inPicto is true when the run so far ends in Extended_Pictographic Extend* (with
	// no intervening ZWJ yet) — the prefix that pictoZWJ is waiting on.
	inPicto bool
	// riRun counts consecutive Regional_Indicator runes ending at the current position.
	riRun int
}

// isExtendedPictographic reports whether r has the Extended_Pictographic property
// (emoji and friends), which rule GB11 keys on.
func isExtendedPictographic(r rune) bool {
	return unicode.Is(unicodedata.Extended_Pictographic, r)
}

// advance folds r into the state, and must be called for every rune in order —
// including the first, for which graphemeBoundary is not consulted.
func (s *graphemeState) advance(r rune, c gbClass) {
	// The regional-indicator run length is what GB12/GB13 read for parity; any other
	// rune ends the run.
	if c == gbRegionalIndicator {
		s.riRun++
	} else {
		s.riRun = 0
	}
	switch {
	case isExtendedPictographic(r):
		// Starts (or, after a ZWJ, continues) an emoji sequence.
		s.inPicto, s.pictoZWJ = true, false
	case c == gbExtend:
		// Extend* after Extended_Pictographic keeps the prefix alive; elsewhere it
		// does not start one.
		s.pictoZWJ = false
	case c == gbZWJ:
		// Only a ZWJ that follows Extended_Pictographic Extend* arms GB11.
		s.pictoZWJ = s.inPicto
		s.inPicto = false
	default:
		s.inPicto, s.pictoZWJ = false, false
	}
}

// graphemeBoundary reports whether there is an extended-grapheme-cluster boundary
// BETWEEN the preceding rune (class prevC) and next (class nextC), given the state
// accumulated through the preceding rune inclusive. It implements the UAX #29 rules
// GB3–GB13 in order; GB999 ("break everywhere else") is the fallthrough. The preceding
// rune enters only through prevC and s, so it is not a parameter.
func graphemeBoundary(next rune, prevC, nextC gbClass, s graphemeState) bool {
	switch {
	case prevC == gbCR && nextC == gbLF:
		return false // GB3: CR × LF
	case prevC == gbControl || prevC == gbCR || prevC == gbLF:
		return true // GB4: (Control | CR | LF) ÷
	case nextC == gbControl || nextC == gbCR || nextC == gbLF:
		return true // GB5: ÷ (Control | CR | LF)
	case prevC == gbL && (nextC == gbL || nextC == gbV || nextC == gbLV || nextC == gbLVT):
		return false // GB6: Hangul L × (L | V | LV | LVT)
	case (prevC == gbLV || prevC == gbV) && (nextC == gbV || nextC == gbT):
		return false // GB7: (LV | V) × (V | T)
	case (prevC == gbLVT || prevC == gbT) && nextC == gbT:
		return false // GB8: (LVT | T) × T
	case nextC == gbExtend || nextC == gbZWJ:
		return false // GB9: × (Extend | ZWJ)
	case nextC == gbSpacingMark:
		return false // GB9a: × SpacingMark
	case prevC == gbPrepend:
		return false // GB9b: Prepend ×
	case s.pictoZWJ && isExtendedPictographic(next):
		// GB11: \p{Extended_Pictographic} Extend* ZWJ × \p{Extended_Pictographic}.
		// This is why the family/profession emoji (a ZWJ sequence of several people)
		// stays whole.
		return false
	case prevC == gbRegionalIndicator && nextC == gbRegionalIndicator && s.riRun%2 == 1:
		// GB12/GB13: break between regional-indicator PAIRS but not within one, so
		// two adjacent flags may be split from each other but neither flag is halved.
		return false
	}
	return true // GB999
}
