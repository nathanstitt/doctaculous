package css

import "testing"

func tokenKinds(src string) []TokenKind {
	var ks []TokenKind
	tz := newTokenizer(src)
	for {
		t := tz.next()
		if t.Kind == TokenEOF {
			break
		}
		ks = append(ks, t.Kind)
	}
	return ks
}

func TestTokenizeIdentsAndDelims(t *testing.T) {
	got := tokenKinds("div , .x")
	want := []TokenKind{TokenIdent, TokenWhitespace, TokenComma, TokenWhitespace, TokenDelim, TokenIdent}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d] = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestTokenizeIdentValue(t *testing.T) {
	tz := newTokenizer("margin-top")
	tok := tz.next()
	if tok.Kind != TokenIdent || tok.Text != "margin-top" {
		t.Fatalf("got %v %q, want Ident \"margin-top\"", tok.Kind, tok.Text)
	}
}

func TestTokenizeHashStringNumberDim(t *testing.T) {
	tz := newTokenizer(`#lead "hi" 12 1.5em 50% -3px`)
	type exp struct {
		k    TokenKind
		text string
		num  float64
		unit string
	}
	want := []exp{
		{TokenHash, "lead", 0, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenString, "hi", 0, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenNumber, "12", 12, ""},
		{TokenWhitespace, " ", 0, ""},
		{TokenDimension, "1.5em", 1.5, "em"},
		{TokenWhitespace, " ", 0, ""},
		{TokenPercent, "50%", 50, "%"},
		{TokenWhitespace, " ", 0, ""},
		{TokenDimension, "-3px", -3, "px"},
	}
	for i, w := range want {
		tok := tz.next()
		if tok.Kind != w.k || tok.Text != w.text || tok.Num != w.num || tok.Unit != w.unit {
			t.Fatalf("token[%d] = {%v %q %v %q}, want {%v %q %v %q}",
				i, tok.Kind, tok.Text, tok.Num, tok.Unit, w.k, w.text, w.num, w.unit)
		}
	}
}

func TestTokenizeCommentsAndPunctuation(t *testing.T) {
	// A comment is skipped entirely; punctuation gets its own kinds.
	got := tokenKinds("a /* note */ : ; { } ( )")
	want := []TokenKind{
		TokenIdent, TokenWhitespace, TokenWhitespace, TokenColon, TokenWhitespace,
		TokenSemicolon, TokenWhitespace, TokenLBrace, TokenWhitespace, TokenRBrace,
		TokenWhitespace, TokenLParen, TokenWhitespace, TokenRParen,
	}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v (len %d), want len %d", got, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d] = %v, want %v (all %v)", i, got[i], want[i], got)
		}
	}
}

func TestTokenizeCommentEdgeCases(t *testing.T) {
	// Unterminated comment consumes to EOF: no panic, no token, just EOF.
	if got := tokenKinds("/* no end"); len(got) != 0 {
		t.Errorf("unterminated comment kinds = %v, want none (consumed to EOF)", got)
	}
	// Bare "/*" at EOF: same.
	if got := tokenKinds("/*"); len(got) != 0 {
		t.Errorf(`bare "/*" kinds = %v, want none`, got)
	}
	// A lone "/" (not followed by "*") is a delimiter, not a comment.
	tz := newTokenizer("/")
	tok := tz.next()
	if tok.Kind != TokenDelim || tok.Text != "/" {
		t.Errorf(`lone "/" = {%v %q}, want {Delim "/"}`, tok.Kind, tok.Text)
	}
	// Consecutive comments are all skipped; the token after them is returned.
	got := tokenKinds("/*a*//*b*/x")
	if len(got) != 1 || got[0] != TokenIdent {
		t.Errorf(`"/*a*//*b*/x" kinds = %v, want [Ident]`, got)
	}
}

func TestTokenKindString(t *testing.T) {
	cases := map[TokenKind]string{
		TokenEOF:    "EOF",
		TokenIdent:  "Ident",
		TokenComma:  "Comma",
		TokenDelim:  "Delim",
		TokenLBrace: "LBrace",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("TokenKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
	// An out-of-range kind must not panic and should be clearly marked.
	if got := TokenKind(999).String(); got == "" {
		t.Errorf("out-of-range String() returned empty; want a non-empty placeholder")
	}
}
