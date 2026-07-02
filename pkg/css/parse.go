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

// Rule is a style rule: a selector group plus its declarations. Media is the media
// context the rule applies in (MediaAll for a top-level rule, or the type of an
// enclosing @media block); RulesForMedia filters on it.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration
	Media        Media
}

// Stylesheet is a parsed CSS document: an ordered list of style rules plus any
// captured @font-face and @page rules. Source order is preserved (the cascade uses it
// as a tie-breaker; @font-face order is the fallback order within a family, and @page
// order is a cascade tie-breaker resolved by ResolvePage).
type Stylesheet struct {
	Rules     []Rule
	FontFaces []FontFace
	Pages     []PageRule
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
			if strings.EqualFold(strings.TrimSpace(prelude), "@font-face") {
				if ff, ok := parseFontFace(parseDeclarations(body)); ok {
					sheet.FontFaces = append(sheet.FontFaces, ff)
				}
			} else if rest, ok := atKeyword(prelude, "@page"); ok {
				if pr, ok := parsePageRule(rest, body, len(sheet.Pages)); ok {
					sheet.Pages = append(sheet.Pages, pr)
				}
			} else if rest, ok := atKeyword(prelude, "@media"); ok {
				// Capture the block's rules, tagged with its media type, so a media
				// context (e.g. print, for PDF output) can select them. The inner body is
				// itself a stylesheet; parse it and fold its rules (and any nested
				// @font-face/@page) up, tagging each rule with this block's media. Rules
				// already tagged (a nested @media) keep their inner tag.
				m := mediaFromPrelude(rest)
				inner := Parse(body)
				for _, r := range inner.Rules {
					if r.Media == MediaAll {
						r.Media = m
					}
					sheet.Rules = append(sheet.Rules, r)
				}
				sheet.FontFaces = append(sheet.FontFaces, inner.FontFaces...)
				sheet.Pages = append(sheet.Pages, inner.Pages...)
			}
			continue // any other at-rule: block already consumed by the scanner
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
	var b strings.Builder
	spanStart := s.pos
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			b.WriteString(s.src[spanStart:s.pos]) // flush the text before the comment
			s.skipComment()
			spanStart = s.pos // resume after the comment
		case s.src[s.pos] == '{':
			b.WriteString(s.src[spanStart:s.pos]) // flush the final span (prelude minus comments)
			s.pos++                               // consume {
			body = s.readBody()
			return b.String(), body, true
		default:
			s.pos++
		}
	}
	return "", "", false
}

// readBody returns the text up to the matching close brace, consuming it, with
// /* */ comments stripped (so a comment before a property name does not corrupt
// the declaration). It depth-tracks nested braces so an at-rule block like
// @media{ p{} } is fully consumed even though Parse then discards it.
func (s *ruleScanner) readBody() string {
	var b strings.Builder
	spanStart := s.pos
	depth := 0
	for s.pos < len(s.src) {
		switch {
		case s.atComment():
			b.WriteString(s.src[spanStart:s.pos]) // flush text before the comment
			s.skipComment()
			spanStart = s.pos // resume after the comment
		case s.src[s.pos] == '{':
			depth++
			s.pos++
		case s.src[s.pos] == '}':
			if depth == 0 {
				b.WriteString(s.src[spanStart:s.pos]) // flush the final span
				s.pos++                               // consume }
				return b.String()
			}
			depth--
			s.pos++
		default:
			s.pos++
		}
	}
	b.WriteString(s.src[spanStart:s.pos]) // unterminated body: flush what remains
	return b.String()
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
