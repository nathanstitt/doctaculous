package css

import "strings"

// Declaration is one property: value pair from a rule body, with the !important
// flag. Value is the raw value text (trimmed); typed interpretation happens in
// the cascade so unknown properties are retained losslessly.
type Declaration struct {
	Property  string
	Value     string
	Important bool
}

// Rule is a style rule: a selector group plus its declarations.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration
}

// Stylesheet is a parsed CSS document: an ordered list of style rules. Source
// order is preserved because the cascade uses it as a tie-breaker.
type Stylesheet struct {
	Rules []Rule
}

// Parse parses a CSS stylesheet. It is total: malformed rules and unsupported
// at-rules are skipped (their block consumed) rather than aborting the parse, so
// a single bad construct cannot discard the sheet. Rule boundaries are found by a
// brace-matching pass that skips /* */ comments.
func Parse(src string) Stylesheet {
	var sheet Stylesheet
	s := &ruleScanner{src: src}
	for {
		prelude, body, ok := s.nextRule()
		if !ok {
			break
		}
		prelude = strings.TrimSpace(prelude)
		if prelude == "" {
			continue
		}
		if strings.HasPrefix(prelude, "@") {
			continue // unsupported at-rule: block already consumed by the scanner
		}
		sels := parseSelectorList(prelude)
		if len(sels) == 0 {
			continue
		}
		sheet.Rules = append(sheet.Rules, Rule{
			Selectors:    sels,
			Declarations: parseDeclarations(body),
		})
	}
	return sheet
}

// ruleScanner walks the source returning (prelude, body) pairs for each top-level
// {...} block. It skips /* */ comments so braces inside comments do not confuse
// boundary detection.
type ruleScanner struct {
	src string
	pos int
}

func (s *ruleScanner) nextRule() (prelude, body string, ok bool) {
	start := s.pos
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			s.skipComment()
		case s.src[s.pos] == '{':
			prelude = s.src[start:s.pos]
			s.pos++ // consume {
			body = s.readBody()
			return prelude, body, true
		default:
			s.pos++
		}
	}
	return "", "", false
}

// readBody returns the text up to the matching close brace, consuming it, and
// handling one level of nesting (so an at-rule block like @media{ p{} } is fully
// consumed even though we then discard it).
func (s *ruleScanner) readBody() string {
	start := s.pos
	depth := 0
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			s.skipComment()
		case s.src[s.pos] == '{':
			depth++
			s.pos++
		case s.src[s.pos] == '}':
			if depth == 0 {
				body := s.src[start:s.pos]
				s.pos++ // consume }
				return body
			}
			depth--
			s.pos++
		default:
			s.pos++
		}
	}
	return s.src[start:s.pos]
}

func (s *ruleScanner) atComment() bool {
	return s.pos+1 < len(s.src) && s.src[s.pos] == '/' && s.src[s.pos+1] == '*'
}

func (s *ruleScanner) skipComment() {
	s.pos += 2
	for s.pos+1 < len(s.src) {
		if s.src[s.pos] == '*' && s.src[s.pos+1] == '/' {
			s.pos += 2
			return
		}
		s.pos++
	}
	s.pos = len(s.src)
}

// parseDeclarations parses a rule body (the text between { and }) into
// declarations. Malformed declarations (no colon, empty property, empty value)
// are skipped so one bad declaration cannot void the rest.
func parseDeclarations(body string) []Declaration {
	var out []Declaration
	// NOTE: the body is split naively on ';'. A value containing a literal
	// semicolon (e.g. a data: URI in url(...)) will be split incorrectly; that is
	// an accepted limitation for the CSS subset this engine targets.
	for _, chunk := range strings.Split(body, ";") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		colon := strings.IndexByte(chunk, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(chunk[:colon])
		val := strings.TrimSpace(chunk[colon+1:])
		if prop == "" || val == "" {
			continue
		}
		important := false
		// Match !important only as the trailing token (suffix + preceding
		// whitespace), case-insensitively; the suffix is ASCII so cutting len(bang)
		// bytes off the original is safe. This avoids the substring false-positive
		// where "url(x!important.png)" would otherwise be flagged.
		const bang = "!important"
		if strings.HasSuffix(strings.ToLower(val), bang) {
			candidate := val[:len(val)-len(bang)]
			if candidate == "" || isWhitespace(candidate[len(candidate)-1]) {
				important = true
				val = strings.TrimSpace(candidate)
			}
		}
		if val == "" {
			continue
		}
		out = append(out, Declaration{Property: strings.ToLower(prop), Value: val, Important: important})
	}
	return out
}
