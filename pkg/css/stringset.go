package css

import "strings"

// StringSetEntry is one `string-set` assignment: it names a CSS string and how to
// build its value when an element matching the owning rule is encountered in document
// order. Only the common forms are modeled: an optional literal Prefix, an optional
// content() (the element's text), and an optional literal Suffix. The page-margin
// string() function reads the most recently set value for a Name on or before a page.
type StringSetEntry struct {
	Name       string
	Prefix     string // literal before content()
	Suffix     string // literal after content()
	UseContent bool   // include the element's text (content())
}

// parseStringSet parses a `string-set` value: "name [<string>|content()]+ [, name ...]".
// Multiple comma-separated assignments are supported. An entry with no recognizable
// parts is dropped. This reuses the content-component splitter shape (quoted strings and
// content() kept intact).
func parseStringSet(value string) []StringSetEntry {
	var out []StringSetEntry
	for _, assign := range strings.Split(value, ",") {
		fields := splitStringSetTokens(strings.TrimSpace(assign))
		if len(fields) < 1 {
			continue
		}
		e := StringSetEntry{Name: strings.ToLower(fields[0])}
		seenContent := false
		for _, tok := range fields[1:] {
			switch {
			case tok == "content()" || tok == "content(text)":
				e.UseContent = true
				seenContent = true
			case len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\''):
				if seenContent {
					e.Suffix += unquote(tok)
				} else {
					e.Prefix += unquote(tok)
				}
			}
		}
		if e.Name != "" && (e.UseContent || e.Prefix != "" || e.Suffix != "") {
			out = append(out, e)
		}
	}
	return out
}

// splitStringSetTokens splits on whitespace but keeps quoted strings and `content(...)`
// parens intact.
func splitStringSetTokens(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
				flush()
			}
		case c == '"' || c == '\'':
			flush()
			quote = c
			cur.WriteByte(c)
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			depth--
			cur.WriteByte(c)
			if depth == 0 {
				flush()
			}
		case (c == ' ' || c == '\t') && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}
