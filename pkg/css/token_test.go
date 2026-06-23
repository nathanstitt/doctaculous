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
