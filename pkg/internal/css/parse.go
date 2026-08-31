package css

import (
	"slices"
	"strings"
)

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
	// Unsupported names the CSS selector constructs this parser does not
	// implement and that caused a selector to be DROPPED from this sheet,
	// deduplicated in first-seen order (see UnsupportedSelector). A dropped
	// selector fails safe — its rule never matches, so it can never mis-apply —
	// but it fails SILENTLY, and an author whose `.icon > path` rule was ignored
	// has nothing to go on. This is the record that lets a caller with a logger
	// say so.
	//
	// It is carried as DATA rather than reported through a logf parameter because
	// Parse has none and cannot gain one: html.UAStylesheet is a package-level
	// var initialized by Parse, so there is no caller at that point to hold a
	// logger. NewResolver drains this for the HTML/DOCX path; pkg/svg's index
	// drains it for SVG-internal <style>.
	Unsupported []UnsupportedSelector
}

// UnsupportedSelector records one selector this parser could not represent, and
// therefore dropped. Construct is a short stable name for the CSS feature (see
// the unsupported* constants); Selector is the offending source text, trimmed,
// so a diagnostic can quote what the author actually wrote.
type UnsupportedSelector struct {
	Construct string
	Selector  string
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
				if ff, ok := parseFontFace(ParseDeclarations(body)); ok {
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
				sheet.addUnsupported(inner.Unsupported)
			}
			continue // any other at-rule: block already consumed by the scanner
		}
		sels, diag := parseSelectorListDiag(prelude)
		sheet.addUnsupported(diag)
		if len(sels) == 0 {
			continue
		}
		sheet.Rules = append(sheet.Rules, Rule{
			Selectors:    sels,
			Declarations: ParseDeclarations(body),
		})
	}
	return sheet
}

// maxUnsupportedSelectors caps how many dropped-selector records one sheet
// retains. A caller logs at most one line per distinct construct, so the list is
// a diagnostic aid, not data anyone iterates in full; the cap keeps a
// machine-generated sheet with thousands of `>` rules from retaining a slice
// proportional to the source.
const maxUnsupportedSelectors = 64

// addUnsupported appends dropped-selector records, skipping any whose (construct,
// selector) pair is already recorded and stopping at maxUnsupportedSelectors. The
// dedupe is quadratic in the retained count, which the cap bounds to a trivial
// amount of work.
func (s *Stylesheet) addUnsupported(diag []UnsupportedSelector) {
	for _, d := range diag {
		if len(s.Unsupported) >= maxUnsupportedSelectors {
			return
		}
		if slices.Contains(s.Unsupported, d) {
			continue
		}
		s.Unsupported = append(s.Unsupported, d)
	}
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

// stripComments removes every /* */ comment from src, unconditionally (no
// brace-depth tracking needed: a declaration list has no nested blocks). An
// unterminated comment consumes the rest of the string, matching
// ruleScanner.skipComment's behavior at end of input.
func stripComments(src string) string {
	if !strings.Contains(src, "/*") {
		return src // fast path: no comment marker at all
	}
	var b strings.Builder
	for {
		start := strings.Index(src, "/*")
		if start < 0 {
			b.WriteString(src)
			break
		}
		b.WriteString(src[:start])
		end := strings.Index(src[start+2:], "*/")
		if end < 0 {
			break // unterminated comment: drop the remainder
		}
		src = src[start+2+end+2:]
	}
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

// ParseDeclarations parses a declaration list (the body of a CSS rule or
// the value of a style="" attribute) into declarations. The !important flag
// is honored; malformed declarations (no colon, empty property, empty value)
// are dropped individually per CSS error recovery, so one bad declaration
// cannot void the rest. /* */ comments are stripped first, so e.g.
// `style="/*c*/fill:green/*c*/"` parses identically to `style="fill:green"` —
// Parse's ruleScanner already strips comments from a <style> sheet's rule
// bodies before they ever reach here, but a style="" attribute's text is
// handed to this function directly, so the stripping has to happen here too.
func ParseDeclarations(body string) []Declaration {
	body = stripComments(body)
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
		// Match !important as the trailing token, case-insensitively; the
		// suffix is ASCII so cutting len(bang) bytes off the original is
		// safe. A plain suffix match cannot false-positive on something like
		// "url(x!important.png)": that string does not end in "!important"
		// at all (it ends in ".png)"), so HasSuffix already rejects it.
		// CSS allows any amount of whitespace (including none) between the
		// value and "!important", so no separator is required before the
		// suffix once it truly is one — "red!important" and "red !important"
		// both flag the same way. Whitespace WITHIN the token ("red ! important")
		// is also legal CSS but is not recognized here; it is vanishingly rare
		// in real stylesheets and would cost a tokenizer pass to support.
		const bang = "!important"
		if strings.HasSuffix(strings.ToLower(val), bang) {
			important = true
			val = strings.TrimSpace(val[:len(val)-len(bang)])
		}
		if val == "" {
			continue
		}
		// Normal property names are ASCII case-insensitive and are normalized to
		// lower case here so the cascade can switch on them directly. Custom
		// property names are NOT: CSS Variables 1 §2 makes --Foo and --foo two
		// distinct properties, so their case must survive parsing verbatim.
		if !IsCustomProperty(prop) {
			prop = strings.ToLower(prop)
		}
		out = append(out, Declaration{Property: prop, Value: val, Important: important})
	}
	return out
}
