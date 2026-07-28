package extract

import "testing"

// rtlLine lays a string out on one baseline the way a PDF stores it: each word is
// placed at ascending x (visual order), and a right-to-left word has its characters
// written right-to-left, so its FIRST logical character sits at the largest x.
//
// visual is the sequence of words as they appear left-to-right on the page; each is
// spelled as the PDF stores it. The helper only positions them — it does no
// reordering, so a test asserting on the extracted text is asserting on the real
// pipeline.
func rtlLine(t *testing.T, visual []string) string {
	t.Helper()
	const adv, gap = 10.0, 6.0 // gap > wordGapFrac*size (0.25*10) splits words
	var g []glyph
	x := 0.0
	for wi, w := range visual {
		if wi > 0 {
			x += gap
		}
		for _, r := range w {
			g = append(g, mkGlyph(r, x, 100, adv))
			x += adv
		}
	}
	lines := buildLines(g)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	return lines[0].text()
}

// TestExtractRTLWordReversed: a Hebrew word is stored with its first character
// rightmost, so extracting by ascending x yields it backwards. It must come out in
// logical order — the order it would be typed and read.
func TestExtractRTLWordReversed(t *testing.T) {
	// The PDF paints ג, ב, א left-to-right; the word is "אבג".
	if got, want := rtlLine(t, []string{"גבא"}), "אבג"; got != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

// TestExtractRTLWordOrderReversed: consecutive right-to-left WORDS are also stored
// mirrored — the first word of the phrase sits rightmost — so the word order
// reverses too, not just the characters inside each word.
func TestExtractRTLWordOrderReversed(t *testing.T) {
	// "שלום עולם" is painted as [םלוע] [םולש] left-to-right.
	if got, want := rtlLine(t, []string{"םלוע", "םולש"}), "שלום עולם"; got != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

// TestExtractLatinUnaffected pins the no-op path: a Latin-only line must extract
// exactly as before, which is what keeps every existing extraction golden stable.
func TestExtractLatinUnaffected(t *testing.T) {
	if got, want := rtlLine(t, []string{"the", "quick", "fox"}), "the quick fox"; got != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

// TestExtractMixedLineKeepsLatinInPlace: a right-to-left phrase inside an otherwise
// left-to-right line reverses on its own; the Latin around it keeps both its
// position and its spelling.
func TestExtractMixedLineKeepsLatinInPlace(t *testing.T) {
	// Painted: "The" [םולש] "here" — the Hebrew word is stored reversed.
	got := rtlLine(t, []string{"The", "םולש", "here"})
	if want := "The שלום here"; got != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

// TestExtractArabicWordReversed: the same applies to Arabic.
func TestExtractArabicWordReversed(t *testing.T) {
	// "مرحبا" painted left-to-right is "ابحرم".
	if got, want := rtlLine(t, []string{"ابحرم"}), "مرحبا"; got != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

func TestWordIsRTL(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"שלום", true},
		{"مرحبا", true},
		{"hello", false},
		{"", false},
		{"123", false},      // digits alone are neutral, not RTL
		{"!?", false},       // punctuation alone is neutral
		{"שלום7", true},     // a digit does not make a Hebrew word LTR
		{"שlom", false},     // mixed scripts: left alone rather than scrambled
		{"Ελληνικά", false}, // Greek is LTR
	}
	for _, c := range cases {
		if got := wordIsRTL(word{text: c.text}); got != c.want {
			t.Errorf("wordIsRTL(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestReorderWordsPreservesGeometry: reordering changes the reading order, not where
// each word was painted. Downstream consumers (table recognition, block detection)
// key off x0/x1, so the geometry must ride along with its word.
func TestReorderWordsPreservesGeometry(t *testing.T) {
	in := []word{
		{text: "םולש", x0: 0, x1: 40},
		{text: "םלוע", x0: 50, x1: 90},
	}
	out := reorderWordsToLogical(in)
	if len(out) != 2 {
		t.Fatalf("got %d words, want 2", len(out))
	}
	// The words swapped order, so the FIRST word out is the one painted at x0=50.
	if out[0].x0 != 50 || out[1].x0 != 0 {
		t.Errorf("geometry did not follow the reorder: got x0 %v then %v, want 50 then 0",
			out[0].x0, out[1].x0)
	}
	// And the input slice must not be mutated.
	if in[0].text != "םולש" {
		t.Errorf("input was mutated: in[0].text = %q", in[0].text)
	}
}
