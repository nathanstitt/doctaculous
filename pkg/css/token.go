package css

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
	TokenDelim     // a single significant char: . > : * etc.
	TokenColon     // :
	TokenSemicolon // ;
	TokenComma     // ,
	TokenLBrace    // {
	TokenRBrace    // }
	TokenLParen    // (
	TokenRParen    // )
)

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

func (t *tokenizer) next() Token {
	if t.pos >= len(t.src) {
		return Token{Kind: TokenEOF}
	}
	c := t.src[t.pos]
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
	case isNameStart(c):
		return t.readIdent()
	default:
		t.pos++
		return Token{Kind: TokenDelim, Text: string(c)}
	}
}

func (t *tokenizer) readIdent() Token {
	start := t.pos
	for t.pos < len(t.src) && isNameChar(t.src[t.pos]) {
		t.pos++
	}
	return Token{Kind: TokenIdent, Text: t.src[start:t.pos]}
}

func isWhitespace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func isNameStart(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }
