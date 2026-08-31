package css

import "strings"

// FontFace is one captured @font-face rule. The cascade does not use it; it is a
// side table consumed at face-resolution time (which face a family name maps to).
type FontFace struct {
	Family  string       // font-family descriptor, unquoted and trimmed
	Sources []FontSource // src: list, in declared (fallback) order
	Weight  string       // font-weight descriptor (e.g. "normal","bold","700"); "" if absent
	Style   string       // font-style descriptor (e.g. "normal","italic"); "" if absent
}

// FontSource is one entry in an @font-face src: list: either a url() reference or
// a local() family name (mutually exclusive).
type FontSource struct {
	URL    string // url() ref; "" for a local() source
	Local  string // local() family name; "" for a url() source
	Format string // format(...) hint, lowercased and unquoted; "" if absent
}

// Descriptors this parser recognizes but does not implement. They are reported
// rather than dropped in silence, because both change what an author sees.
const (
	unsupportedUnicodeRange = "@font-face unicode-range"
	unsupportedFontDisplay  = "@font-face font-display"
)

// parseFontFace maps an @font-face block's declarations to a FontFace. ok is false
// when the rule lacks a font-family or has no usable src (the caller drops it).
//
// diag records descriptors that were dropped and would change rendering if they
// were honored, through the same carried-as-data channel as UnsupportedSelector
// (see Stylesheet.Unsupported for why Parse cannot take a logger).
//
// unicode-range is the one that matters: it selects WHICH runes a face covers, so
// ignoring it means the whole face is used for every rune — a document that splits
// a family across subsetted faces then renders from the wrong file, with no
// indication why. font-display only affects load-time swap behaviour, which does
// not apply to a batch renderer, but it is reported too so an author is not left
// guessing which of the two descriptors was honored.
func parseFontFace(decls []Declaration) (ff FontFace, diag []UnsupportedSelector, ok bool) {
	for _, d := range decls {
		switch strings.ToLower(d.Property) {
		case "font-family":
			ff.Family = unquote(strings.TrimSpace(d.Value))
		case "src":
			ff.Sources = parseSrcList(d.Value)
		case "font-weight":
			ff.Weight = strings.ToLower(strings.TrimSpace(d.Value))
		case "font-style":
			ff.Style = strings.ToLower(strings.TrimSpace(d.Value))
		case "unicode-range":
			diag = append(diag, UnsupportedSelector{
				Construct: unsupportedUnicodeRange, Selector: strings.TrimSpace(d.Value), Descriptor: true})
		case "font-display":
			diag = append(diag, UnsupportedSelector{
				Construct: unsupportedFontDisplay, Selector: strings.TrimSpace(d.Value), Descriptor: true})
		}
	}
	if ff.Family == "" || len(ff.Sources) == 0 {
		return FontFace{}, nil, false
	}
	return ff, diag, true
}

// parseSrcList parses an @font-face src descriptor value into ordered sources.
// Entries are comma-separated at the top level (commas inside (), "" or ” do not
// split). A malformed entry (neither url() nor local()) is skipped; the rest
// survive.
func parseSrcList(val string) []FontSource {
	var out []FontSource
	for _, entry := range splitTopLevel(val, ',') {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		src, ok := parseSrcEntry(entry)
		if !ok {
			continue
		}
		out = append(out, src)
	}
	return out
}

// parseSrcEntry parses one src entry: "url(x) format(y)", "local(x)", or "url(x)".
func parseSrcEntry(entry string) (FontSource, bool) {
	switch {
	case strings.HasPrefix(entry, "url("):
		ref, rest, ok := takeFunc(entry, "url")
		if !ok {
			return FontSource{}, false
		}
		src := FontSource{URL: unquote(strings.TrimSpace(ref))}
		// Optional trailing format(...).
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "format(") {
			if f, _, ok := takeFunc(rest, "format"); ok {
				src.Format = strings.ToLower(unquote(strings.TrimSpace(f)))
			}
		}
		if src.URL == "" {
			return FontSource{}, false
		}
		return src, true
	case strings.HasPrefix(entry, "local("):
		name, _, ok := takeFunc(entry, "local")
		if !ok {
			return FontSource{}, false
		}
		name = unquote(strings.TrimSpace(name))
		if name == "" {
			return FontSource{}, false
		}
		return FontSource{Local: name}, true
	default:
		return FontSource{}, false
	}
}

// takeFunc consumes a leading fn(...) from s, returning the inner argument text
// and the remainder after the closing paren. ok is false if s does not start with
// fn( or has no matching ).
func takeFunc(s, fn string) (arg, rest string, ok bool) {
	prefix := fn + "("
	if !strings.HasPrefix(s, prefix) {
		return "", "", false
	}
	depth := 0
	for i := len(fn); i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[len(prefix):i], s[i+1:], true
			}
		}
	}
	return "", "", false
}

// splitTopLevel splits s on sep, ignoring sep inside (), "" or ”.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// unquote strips one matching pair of surrounding single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
