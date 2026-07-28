package extract

// Visual-to-logical reordering for extracted text.
//
// A PDF stores glyphs by POSITION, not by reading order: a right-to-left word is
// written with its first character at the largest x. Extraction sorts a line's
// glyphs left-to-right — which is the correct VISUAL order and, for Latin, also the
// logical one — so right-to-left text comes out reversed unless it is put back.
//
// This is the inverse of the layout engine's problem. There, logical text is
// reordered to visual for painting (UAX#9 rule L2, see pkg/layout/inline/bidi.go);
// here, visual glyphs are reordered back to logical for the extracted string.
//
// The inverse of L2 cannot be derived by running the bidi algorithm over the
// extracted text, because that text is already scrambled — the algorithm's input
// must be logical order, which is precisely what is missing. What CAN be recovered
// is the run structure: a maximal run of right-to-left text was laid out
// right-to-left, so reversing each such run restores logical order. That is exact
// for the common cases (a right-to-left phrase in a left-to-right line and vice
// versa) and is what PDF viewers do when copying text.
//
// This runs AFTER word grouping, not before. Grouping splits words on the x-gap
// between a glyph and the previous glyph's right edge, which assumes ascending x;
// reordering glyphs first would produce negative gaps and break the split. Working
// on assembled words also means each word keeps the geometry a caller needs.

import "unicode"

// isRTLRune reports whether r is a strong right-to-left character: the Hebrew,
// Arabic, Syriac, Thaana, N'Ko, Samaritan, and Mandaic blocks plus the Hebrew and
// Arabic presentation forms and the RTL supplementary planes.
//
// It deliberately excludes neutrals (spaces, punctuation, digits): those take their
// direction from context, and treating them as strong would extend a reversal past
// the run that actually needs it.
func isRTLRune(r rune) bool {
	switch {
	case r < 0x0590:
		return false // ASCII, Latin, Greek, Cyrillic — never RTL
	case r <= 0x05FF: // Hebrew
		return true
	case r >= 0x0600 && r <= 0x06FF, // Arabic
		r >= 0x0700 && r <= 0x074F, // Syriac
		r >= 0x0750 && r <= 0x077F, // Arabic Supplement
		r >= 0x0780 && r <= 0x07BF, // Thaana
		r >= 0x07C0 && r <= 0x07FF, // NKo
		r >= 0x0800 && r <= 0x083F, // Samaritan
		r >= 0x0840 && r <= 0x085F, // Mandaic
		r >= 0x08A0 && r <= 0x08FF: // Arabic Extended-A
		return true
	case r >= 0xFB1D && r <= 0xFDFF, // Hebrew + Arabic presentation forms A
		r >= 0xFE70 && r <= 0xFEFF: // Arabic presentation forms B
		return true
	case r >= 0x10800 && r <= 0x10FFF, // Cypriot, Phoenician, Kharoshthi, …
		r >= 0x1E800 && r <= 0x1EFFF: // Mende Kikakui, Adlam, Arabic Math
		return true
	}
	return false
}

// wordIsRTL reports whether w's text is predominantly right-to-left: it contains at
// least one strong RTL character and no strong LTR one. A word mixing both (rare —
// usually a transliteration) is treated as left-to-right and left alone, since
// reversing it would scramble the Latin part.
func wordIsRTL(w word) bool {
	sawRTL := false
	for _, r := range w.text {
		switch {
		case isRTLRune(r):
			sawRTL = true
		case isStrongLTRRune(r):
			return false
		}
	}
	return sawRTL
}

// isStrongLTRRune reports whether r is a strong left-to-right character. Everything
// that is neither strong-LTR nor strong-RTL (spaces, punctuation, digits) is neutral
// and takes its direction from context.
func isStrongLTRRune(r rune) bool {
	return unicode.IsLetter(r) && !isRTLRune(r)
}

// reorderWordsToLogical takes a line's words in VISUAL order (left to right, as the
// PDF placed them) and returns them in LOGICAL order — the order they would be read
// and typed.
//
// Two reversals are needed, because the PDF stores right-to-left text mirrored at
// BOTH levels:
//
//  1. the characters WITHIN each right-to-left word, and
//  2. the ORDER of consecutive right-to-left words.
//
// A run of right-to-left words is therefore reversed as a block, and each of its
// words has its own characters reversed. Left-to-right words keep their position and
// their spelling, so a Latin phrase embedded in a right-to-left line survives intact.
//
// Returns the input untouched when the line has no right-to-left word — the
// overwhelmingly common case, which keeps Latin extraction byte-identical.
func reorderWordsToLogical(words []word) []word {
	if len(words) < 1 {
		return words
	}
	any := false
	for _, w := range words {
		if wordIsRTL(w) {
			any = true
			break
		}
	}
	if !any {
		return words
	}

	// Classify ONCE, up front. Reversing a word's text does not change its character
	// classes, so re-testing afterwards would give the same answer — but relying on
	// that would silently couple the run-detection below to the spelling above.
	rtl := make([]bool, len(words))
	for i, w := range words {
		rtl[i] = wordIsRTL(w)
	}

	out := make([]word, len(words))
	copy(out, words)
	for i := range out {
		if rtl[i] {
			out[i].text = reverseString(out[i].text)
		}
	}
	// Reverse each maximal run of right-to-left words. Their geometry rides along, so
	// a caller reading x0/x1 still sees where each word was painted.
	for i := 0; i < len(out); {
		if !rtl[i] {
			i++
			continue
		}
		end := i + 1
		for end < len(out) && rtl[end] {
			end++
		}
		reverseWords(out[i:end])
		i = end
	}
	return out
}

// reverseString returns s with its runes in reverse order.
func reverseString(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// reverseWords reverses a slice of words in place.
func reverseWords(w []word) {
	for i, j := 0, len(w)-1; i < j; i, j = i+1, j-1 {
		w[i], w[j] = w[j], w[i]
	}
}
