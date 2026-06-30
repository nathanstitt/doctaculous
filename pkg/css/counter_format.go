package css

import "strconv"

// formatCounter renders an integer counter value as a string in the given CSS
// counter/list style: decimal, decimal-leading-zero, lower/upper-roman,
// lower/upper-alpha (= latin), or one of the bullet styles disc/circle/square (whose
// glyph ignores the value). "none" yields "". An unrecognized style, or a value
// outside a style's representable range (e.g. roman/alpha for <= 0), falls back to
// decimal. This is the single source of truth for marker and counter() text.
func formatCounter(value int, style string) string {
	switch style {
	case "disc":
		return "•" // •
	case "circle":
		return "◦" // ◦
	case "square":
		return "▪" // ▪
	case "none":
		return ""
	case "decimal-leading-zero":
		if value >= 0 && value < 10 {
			return "0" + strconv.Itoa(value)
		}
		return strconv.Itoa(value)
	case "lower-roman":
		if r, ok := roman(value); ok {
			return lower(r)
		}
		return strconv.Itoa(value)
	case "upper-roman":
		if r, ok := roman(value); ok {
			return r
		}
		return strconv.Itoa(value)
	case "lower-alpha", "lower-latin":
		if a, ok := alpha(value); ok {
			return lower(a)
		}
		return strconv.Itoa(value)
	case "upper-alpha", "upper-latin":
		if a, ok := alpha(value); ok {
			return a
		}
		return strconv.Itoa(value)
	default: // decimal and any unknown numeric style
		return strconv.Itoa(value)
	}
}

// roman returns the uppercase Roman numeral for v (1..3999), ok=false otherwise.
func roman(v int) (string, bool) {
	if v <= 0 || v >= 4000 {
		return "", false
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b []byte
	for i, n := range vals {
		for v >= n {
			b = append(b, syms[i]...)
			v -= n
		}
	}
	return string(b), true
}

// alpha returns the uppercase bijective base-26 sequence for v (1→A, 26→Z, 27→AA),
// ok=false for v <= 0.
func alpha(v int) (string, bool) {
	if v <= 0 {
		return "", false
	}
	var b []byte
	for v > 0 {
		v--
		b = append([]byte{byte('A' + v%26)}, b...)
		v /= 26
	}
	return string(b), true
}

// lower ASCII-lowercases a string of A–Z (the only letters roman/alpha produce).
func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
