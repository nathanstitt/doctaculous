package css

import (
	"strconv"
	"strings"
)

// CounterOp is one (name, value) entry of a counter-reset / counter-increment /
// counter-set declaration (e.g. "section 2" → {Name:"section", Value:2}).
type CounterOp struct {
	Name  string
	Value int
}

// ContentPart is one rendered piece of a CSS `content` value. Exactly one of the
// fields applies per Kind: a literal string (Kind=ContentString, Text), a
// counter(name, style) (Kind=ContentCounter, Name + Style), or a
// counters(name, sep, style) (Kind=ContentCounters, Name + Sep + Style). Other
// content pieces (attr(), images) are not parsed and never produce a part.
type ContentPart struct {
	Kind  ContentKind
	Text  string // ContentString: the literal text
	Name  string // ContentCounter/Counters: the counter name
	Sep   string // ContentCounters: the separator
	Style string // ContentCounter/Counters: the counter style (default "decimal")
}

// ContentKind discriminates a ContentPart.
type ContentKind int

const (
	// ContentString is a literal string piece.
	ContentString ContentKind = iota
	// ContentCounter is a counter(name, style) function.
	ContentCounter
	// ContentCounters is a counters(name, sep, style) function.
	ContentCounters
)

// parseCounterOps parses a counter-reset/increment/set value: space-separated
// name[ integer] pairs, e.g. "section 2 page" → [{section,2},{page,defaultVal}].
// A name not followed by an integer takes defaultVal (1 for increment, 0 for
// reset/set). "none" yields no ops.
func parseCounterOps(val string, defaultVal int) []CounterOp {
	fields := strings.Fields(val)
	if len(fields) == 0 || (len(fields) == 1 && fields[0] == "none") {
		return nil
	}
	var ops []CounterOp
	for i := 0; i < len(fields); i++ {
		name := fields[i]
		if name == "none" {
			continue
		}
		v := defaultVal
		if i+1 < len(fields) {
			if n, err := strconv.Atoi(fields[i+1]); err == nil {
				v = n
				i++
			}
		}
		ops = append(ops, CounterOp{Name: name, Value: v})
	}
	return ops
}

// applyListStyleShorthand expands the `list-style` shorthand into its longhands. It
// accepts the type, position (inside/outside), and image (url(...)|none) tokens in
// any order; recognized tokens set the matching longhand, others are ignored. An
// image token is recognized but not stored (image markers are deferred — the type
// marker is used). A bare "none" sets list-style-type:none.
func applyListStyleShorthand(cs *ComputedStyle, val string) {
	for _, tok := range strings.Fields(val) {
		switch tok {
		case "inside", "outside":
			cs.ListStylePosition = tok
		case "none":
			cs.ListStyleType = "none"
		default:
			if strings.HasPrefix(tok, "url(") {
				continue // image marker deferred; keep the type marker
			}
			cs.ListStyleType = tok // assume a list-style-type keyword
		}
	}
}

// parseContent parses the subset of a CSS `content` value we render: double/single
// quoted literal strings and counter(name[, style]) / counters(name, sep[, style])
// functions, in sequence. Unrecognized pieces are skipped. "none"/"normal" yield no
// parts.
func parseContent(val string) []ContentPart {
	s := strings.TrimSpace(val)
	if s == "" || s == "none" || s == "normal" {
		return nil
	}
	var parts []ContentPart
	i := 0
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n':
			i++
		case s[i] == '"' || s[i] == '\'':
			q := s[i]
			j := i + 1
			for j < len(s) && s[j] != q {
				j++
			}
			parts = append(parts, ContentPart{Kind: ContentString, Text: s[i+1 : min(j, len(s))]})
			i = j + 1
		case hasFoldPrefix(s[i:], "counters("):
			inner, next, ok := parenArgs(s, i+len("counters("))
			if !ok {
				return parts
			}
			args := splitArgs(inner)
			p := ContentPart{Kind: ContentCounters, Style: "decimal"}
			if len(args) > 0 {
				p.Name = unquote(args[0])
			}
			if len(args) > 1 {
				p.Sep = unquote(args[1])
			}
			if len(args) > 2 {
				p.Style = unquote(args[2])
			}
			parts = append(parts, p)
			i = next
		case hasFoldPrefix(s[i:], "counter("):
			inner, next, ok := parenArgs(s, i+len("counter("))
			if !ok {
				return parts
			}
			args := splitArgs(inner)
			p := ContentPart{Kind: ContentCounter, Style: "decimal"}
			if len(args) > 0 {
				p.Name = unquote(args[0])
			}
			if len(args) > 1 {
				p.Style = unquote(args[1])
			}
			parts = append(parts, p)
			i = next
		default:
			i++ // skip an unrecognized token character
		}
	}
	return parts
}

// parenArgs reads a function's argument text starting just after its '(' at index
// start: it returns the text up to the matching top-level ')' (skipping ')' inside a
// quoted string), and the index just past that ')'. ok is false if no closing ')' is
// found. It does not handle nested parens (counter args contain none).
func parenArgs(s string, start int) (inner string, next int, ok bool) {
	var quote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == ')':
			return s[start:i], i + 1, true
		}
	}
	return "", 0, false
}

// hasFoldPrefix reports whether s begins with prefix, case-insensitively, without
// allocating a lowercased copy of s (prefix must be ASCII lowercase).
func hasFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

// splitArgs splits a counter()/counters() argument list on commas that are outside a
// quoted string, then trims each argument. The separator argument is commonly a quoted
// string that itself contains a comma (e.g. counters(item, ", ")), so a naive split on
// every comma would mis-parse it; the quote-aware scan keeps such a separator intact.
func splitArgs(s string) []string {
	var parts []string
	var start int
	var quote byte // 0 when not inside a quoted string, else the opening quote char
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == ',':
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// FormatCounter renders an integer counter value as a string in the given CSS
// counter/list style: decimal, decimal-leading-zero, lower/upper-roman,
// lower/upper-alpha (= latin), or one of the bullet styles disc/circle/square (whose
// glyph ignores the value). "none" yields "". An unrecognized style, or a value
// outside a style's representable range (e.g. roman/alpha for <= 0), falls back to
// decimal. This is the single source of truth for marker and counter() text.
func FormatCounter(value int, style string) string {
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
