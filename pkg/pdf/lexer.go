package pdf

import (
	"fmt"
)

// tokenKind enumerates the lexical token categories of PDF object syntax.
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokInteger
	tokReal
	tokString // literal (..) or hex <..>, value already decoded
	tokName   // /Name, value without leading slash, escapes decoded
	tokArrayOpen
	tokArrayClose
	tokDictOpen  // <<
	tokDictClose // >>
	tokKeyword   // bare word: obj, endobj, stream, R, true, false, null, xref, ...
)

// token is a single lexical token.
type token struct {
	kind tokenKind
	// val holds decoded bytes for strings/names, and the raw text for keywords.
	val []byte
	// num holds the parsed value for tokInteger/tokReal.
	num float64
	// pos is the byte offset of the token start within the source.
	pos int
}

// lexer tokenizes a byte slice of PDF object syntax. It operates on an in-memory
// slice so callers can seek freely (the parser needs random access for xref).
type lexer struct {
	src []byte
	pos int
}

func newLexer(src []byte) *lexer { return &lexer{src: src} }

func isWhitespace(b byte) bool {
	switch b {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		return true
	}
	return false
}

func isDelimiter(b byte) bool {
	switch b {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isRegular(b byte) bool {
	return !isWhitespace(b) && !isDelimiter(b)
}

// skipSpace advances past whitespace and comments (% to end of line).
func (l *lexer) skipSpace() {
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		switch {
		case isWhitespace(b):
			l.pos++
		case b == '%':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
		default:
			return
		}
	}
}

// next returns the next token. At end of input it returns a tokEOF token.
func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, pos: l.pos}, nil
	}
	start := l.pos
	b := l.src[l.pos]
	switch {
	case b == '[':
		l.pos++
		return token{kind: tokArrayOpen, pos: start}, nil
	case b == ']':
		l.pos++
		return token{kind: tokArrayClose, pos: start}, nil
	case b == '<':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '<' {
			l.pos += 2
			return token{kind: tokDictOpen, pos: start}, nil
		}
		return l.lexHexString()
	case b == '>':
		if l.pos+1 < len(l.src) && l.src[l.pos+1] == '>' {
			l.pos += 2
			return token{kind: tokDictClose, pos: start}, nil
		}
		return token{}, fmt.Errorf("pdf: unexpected '>' at offset %d", l.pos)
	case b == '(':
		return l.lexLiteralString()
	case b == '/':
		return l.lexName()
	case b == '{' || b == '}':
		// PostScript-calculator delimiters; treat as standalone keywords so the
		// parser can skip them gracefully if they appear.
		l.pos++
		return token{kind: tokKeyword, val: l.src[start : start+1], pos: start}, nil
	case b == '+' || b == '-' || b == '.' || (b >= '0' && b <= '9'):
		return l.lexNumber()
	default:
		return l.lexKeyword()
	}
}

func (l *lexer) lexName() (token, error) {
	start := l.pos
	l.pos++ // consume '/'
	var out []byte
	for l.pos < len(l.src) && isRegular(l.src[l.pos]) {
		c := l.src[l.pos]
		if c == '#' && l.pos+2 < len(l.src) {
			h1, ok1 := hexDigit(l.src[l.pos+1])
			h2, ok2 := hexDigit(l.src[l.pos+2])
			if ok1 && ok2 {
				out = append(out, h1<<4|h2)
				l.pos += 3
				continue
			}
		}
		out = append(out, c)
		l.pos++
	}
	return token{kind: tokName, val: out, pos: start}, nil
}

func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	isReal := false
	if l.src[l.pos] == '+' || l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c >= '0' && c <= '9' {
			l.pos++
		} else if c == '.' {
			isReal = true
			l.pos++
		} else {
			break
		}
	}
	text := l.src[start:l.pos]
	f, err := parseNumber(text)
	if err != nil {
		return token{}, fmt.Errorf("pdf: bad number %q at offset %d: %w", text, start, err)
	}
	kind := tokInteger
	if isReal {
		kind = tokReal
	}
	return token{kind: kind, num: f, val: text, pos: start}, nil
}

func (l *lexer) lexKeyword() (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isRegular(l.src[l.pos]) {
		l.pos++
	}
	if l.pos == start {
		// A lone delimiter we don't otherwise handle; consume one byte to avoid
		// an infinite loop and report it.
		l.pos++
		return token{}, fmt.Errorf("pdf: unexpected byte %q at offset %d", l.src[start], start)
	}
	return token{kind: tokKeyword, val: l.src[start:l.pos], pos: start}, nil
}

func (l *lexer) lexLiteralString() (token, error) {
	start := l.pos
	l.pos++ // consume '('
	var out []byte
	depth := 1
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case '\\':
			l.pos++
			if l.pos >= len(l.src) {
				return token{}, fmt.Errorf("pdf: unterminated string escape at offset %d", start)
			}
			e := l.src[l.pos]
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '(', ')', '\\':
				out = append(out, e)
			case '\r':
				// line continuation: skip the newline (and a following \n)
				if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
					l.pos++
				}
			case '\n':
				// line continuation: nothing emitted
			default:
				if e >= '0' && e <= '7' {
					// up to 3 octal digits
					val := int(e - '0')
					for range 2 {
						if l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '7' {
							l.pos++
							val = val*8 + int(l.src[l.pos]-'0')
						} else {
							break
						}
					}
					out = append(out, byte(val))
				} else {
					// backslash before a non-escape char: the char is literal
					out = append(out, e)
				}
			}
			l.pos++
		case '(':
			depth++
			out = append(out, c)
			l.pos++
		case ')':
			depth--
			if depth == 0 {
				l.pos++
				return token{kind: tokString, val: out, pos: start}, nil
			}
			out = append(out, c)
			l.pos++
		default:
			out = append(out, c)
			l.pos++
		}
	}
	return token{}, fmt.Errorf("pdf: unterminated literal string at offset %d", start)
}

func (l *lexer) lexHexString() (token, error) {
	start := l.pos
	l.pos++ // consume '<'
	var nibbles []byte
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '>' {
			l.pos++
			// odd number of digits: last is treated as if followed by 0
			if len(nibbles)%2 == 1 {
				nibbles = append(nibbles, 0)
			}
			out := make([]byte, len(nibbles)/2)
			for i := range out {
				out[i] = nibbles[2*i]<<4 | nibbles[2*i+1]
			}
			return token{kind: tokString, val: out, pos: start}, nil
		}
		if isWhitespace(c) {
			l.pos++
			continue
		}
		h, ok := hexDigit(c)
		if !ok {
			return token{}, fmt.Errorf("pdf: bad hex digit %q at offset %d", c, l.pos)
		}
		nibbles = append(nibbles, h)
		l.pos++
	}
	return token{}, fmt.Errorf("pdf: unterminated hex string at offset %d", start)
}

func hexDigit(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}
