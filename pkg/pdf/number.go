package pdf

import (
	"fmt"
	"strconv"
)

// parseNumber parses a PDF numeric token. PDF is lenient about number syntax
// (leading "+", multiple signs occasionally seen in the wild, trailing/leading
// dots), so we normalize before handing to strconv.
func parseNumber(text []byte) (float64, error) {
	s := string(text)
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	// Fast path for plain integers.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return float64(i), nil
	}
	// Strip a single leading sign and remember it.
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	// Some producers emit stray extra signs (e.g. "--12"); drop them.
	for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			neg = !neg
		}
		s = s[1:]
	}
	if s == "" || s == "." {
		return 0, nil // "." and lone signs map to 0, matching common readers
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		f = -f
	}
	return f, nil
}
