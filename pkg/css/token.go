package css

import (
	"fmt"
	"math"
	"strconv"
)

// TokenKind enumerates the CSS token types this engine recognizes. It is a
// pragmatic subset of CSS Syntax §4 sufficient for selectors and declarations.
type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenWhitespace
	TokenIdent     // a name: div, margin-top, red
	TokenHash      // #name  (id selector / hex color)
	TokenString    // "..." or '...'
	TokenNumber    // 12, 1.5, -3
	TokenDimension // 12px, 1.5em
	TokenPercent   // 50%
	TokenDelim     // a single significant char not matched above: . > * etc.
	TokenColon     // :
	TokenSemicolon // ;
	TokenComma     // ,
	TokenLBrace    // {
	TokenRBrace    // }
	TokenLParen    // (
	TokenRParen    // )
)

var tokenKindNames = []string{
	"EOF", "Whitespace", "Ident", "Hash", "String", "Number",
	"Dimension", "Percent", "Delim", "Colon", "Semicolon", "Comma",
	"LBrace", "RBrace", "LParen", "RParen",
}

// String returns the token kind's short name, for readable test output and debugging.
func (k TokenKind) String() string {
	if int(k) >= 0 && int(k) < len(tokenKindNames) {
		return tokenKindNames[k]
	}
	return fmt.Sprintf("TokenKind(%d)", int(k))
}

// Token is one lexical unit. Text holds the token's source text (for Ident/String
// the decoded value; for Dimension the numeric+unit text); Num and Unit are set
// for Number/Dimension/Percent.
type Token struct {
	Kind TokenKind
	Text string
	Num  float64
	Unit string
}

type tokenizer struct {
	src string
	pos int
}

func newTokenizer(src string) *tokenizer { return &tokenizer{src: src} }

// rest returns the un-consumed remainder of the input, without tokenizing it.
// It is the escape hatch for grammars defined over a whole value rather than
// over a token stream — the colour grammar (pkg/css/color.go) is the one such
// case: CSS Color 4 syntax like `rgb(79 156 255 / 35%)` is far cleaner to parse
// from raw text than by re-assembling numbers, solidi and percentages out of
// tokens, and re-tokenizing it would only be a lossy round-trip. Callers must be
// positioned where the ENTIRE remainder is the production they want to parse.
func (t *tokenizer) rest() string { return t.src[t.pos:] }

func (t *tokenizer) next() Token {
	for {
		if t.pos >= len(t.src) {
			return Token{Kind: TokenEOF}
		}
		c := t.src[t.pos]
		// Skip comments iteratively so runs of consecutive comments can't blow
		// the goroutine stack via recursion.
		if c == '/' && t.pos+1 < len(t.src) && t.src[t.pos+1] == '*' {
			t.skipComment()
			continue
		}
		switch {
		case isWhitespace(c):
			start := t.pos
			for t.pos < len(t.src) && isWhitespace(t.src[t.pos]) {
				t.pos++
			}
			return Token{Kind: TokenWhitespace, Text: t.src[start:t.pos]}
		case c == ',':
			t.pos++
			return Token{Kind: TokenComma, Text: ","}
		case c == '#':
			t.pos++
			id := t.readName()
			return Token{Kind: TokenHash, Text: id}
		case c == '"' || c == '\'':
			return t.readString(c)
		case c == '-' && t.pos+1 < len(t.src) && (isDigit(t.src[t.pos+1]) || t.src[t.pos+1] == '.'):
			return t.readNumeric()
		case isDigit(c):
			return t.readNumeric()
		case c == '.' && t.pos+1 < len(t.src) && isDigit(t.src[t.pos+1]):
			return t.readNumeric()
		case isNameStart(c):
			return t.readIdent()
		case c == ':':
			t.pos++
			return Token{Kind: TokenColon, Text: ":"}
		case c == ';':
			t.pos++
			return Token{Kind: TokenSemicolon, Text: ";"}
		case c == '{':
			t.pos++
			return Token{Kind: TokenLBrace, Text: "{"}
		case c == '}':
			t.pos++
			return Token{Kind: TokenRBrace, Text: "}"}
		case c == '(':
			t.pos++
			return Token{Kind: TokenLParen, Text: "("}
		case c == ')':
			t.pos++
			return Token{Kind: TokenRParen, Text: ")"}
		default:
			t.pos++
			return Token{Kind: TokenDelim, Text: string(c)}
		}
	}
}

func (t *tokenizer) skipComment() {
	t.pos += 2 // consume /*
	for t.pos+1 < len(t.src) {
		if t.src[t.pos] == '*' && t.src[t.pos+1] == '/' {
			t.pos += 2
			return
		}
		t.pos++
	}
	t.pos = len(t.src) // unterminated comment: consume to EOF
}

func (t *tokenizer) readIdent() Token {
	start := t.pos
	for t.pos < len(t.src) && isNameChar(t.src[t.pos]) {
		t.pos++
	}
	return Token{Kind: TokenIdent, Text: t.src[start:t.pos]}
}

func (t *tokenizer) readName() string {
	start := t.pos
	for t.pos < len(t.src) && isNameChar(t.src[t.pos]) {
		t.pos++
	}
	return t.src[start:t.pos]
}

func (t *tokenizer) readString(quote byte) Token {
	t.pos++ // opening quote
	start := t.pos
	for t.pos < len(t.src) && t.src[t.pos] != quote {
		t.pos++
	}
	s := t.src[start:t.pos]
	if t.pos < len(t.src) {
		t.pos++ // closing quote
	}
	return Token{Kind: TokenString, Text: s}
}

func (t *tokenizer) readNumeric() Token {
	start := t.pos
	if t.src[t.pos] == '-' {
		t.pos++
	}
	for t.pos < len(t.src) && (isDigit(t.src[t.pos]) || t.src[t.pos] == '.') {
		t.pos++
	}
	numText := t.src[start:t.pos]
	num := parseFloat(numText)
	if t.pos < len(t.src) && t.src[t.pos] == '%' {
		t.pos++
		return Token{Kind: TokenPercent, Text: numText + "%", Num: num, Unit: "%"}
	}
	if t.pos < len(t.src) && isNameStart(t.src[t.pos]) {
		unit := t.readName()
		return Token{Kind: TokenDimension, Text: numText + unit, Num: num, Unit: unit}
	}
	return Token{Kind: TokenNumber, Text: numText, Num: num}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseFloat parses a CSS number, returning 0 on malformed input (never panics).
//
// An out-of-range literal returns 0 rather than the ±Inf strconv reports it as.
// readNumeric only ever hands us digits, "." and a leading "-", so "inf" cannot
// be spelled here -- but a long enough run of digits still overflows to
// infinity, and an infinite Token.Num poisons every downstream consumer (an
// infinite flex-grow, for one, makes the flex free-space loop never converge).
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	return f
}

func isWhitespace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func isNameStart(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }
