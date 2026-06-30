package css

import "testing"

func TestFormatCounter(t *testing.T) {
	cases := []struct {
		val   int
		style string
		want  string
	}{
		{1, "decimal", "1"}, {42, "decimal", "42"},
		{7, "decimal-leading-zero", "07"}, {10, "decimal-leading-zero", "10"},
		{1, "lower-roman", "i"}, {4, "lower-roman", "iv"}, {1990, "lower-roman", "mcmxc"},
		{4, "upper-roman", "IV"}, {2024, "upper-roman", "MMXXIV"},
		{1, "lower-alpha", "a"}, {26, "lower-alpha", "z"}, {27, "lower-alpha", "aa"}, {28, "lower-alpha", "ab"},
		{1, "upper-alpha", "A"}, {27, "upper-alpha", "AA"},
		{1, "lower-latin", "a"}, {1, "upper-latin", "A"}, // aliases
		{0, "lower-roman", "0"},   // roman has no 0 → decimal fallback
		{-3, "upper-roman", "-3"}, // negative → decimal fallback
		{5, "disc", "•"},          // bullet glyphs ignore the value
		{5, "circle", "◦"},
		{5, "square", "▪"},
		{5, "none", ""},
		{5, "bogus", "5"}, // unknown numeric-ish → decimal
	}
	for _, c := range cases {
		if got := FormatCounter(c.val, c.style); got != c.want {
			t.Errorf("FormatCounter(%d, %q) = %q, want %q", c.val, c.style, got, c.want)
		}
	}
}
