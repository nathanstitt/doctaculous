package css

import (
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// renderInlineText lays src out and returns the concatenated Runes of every laid-out
// glyph (in fragment/line/glyph order), i.e. the actual rendered text after any
// text-transform is applied at shaping time.
func renderInlineText(t *testing.T, src string) string {
	t.Helper()
	root := layoutWithLoader(t, src, 400, resource.MapLoader{}, nil)
	var b strings.Builder
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		for li := range f.Lines {
			for gi := range f.Lines[li].Glyphs {
				b.WriteString(string(f.Lines[li].Glyphs[gi].Runes))
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	return b.String()
}

// TestTextTransformUppercase asserts an uppercased box renders glyphs for the
// uppercased text (the shaped glyphs carry the transformed string).
func TestTextTransformUppercase(t *testing.T) {
	got := renderInlineText(t, `<body><p><span style="text-transform:uppercase">hello</span></p></body>`)
	if !strings.Contains(got, "HELLO") {
		t.Fatalf("rendered text = %q, want to contain HELLO", got)
	}
	if strings.Contains(got, "hello") {
		t.Fatalf("rendered text = %q, still contains lowercase 'hello'", got)
	}
}

// TestTextTransformLowercase asserts lowercase transforms the rendered text.
func TestTextTransformLowercase(t *testing.T) {
	got := renderInlineText(t, `<body><p><span style="text-transform:lowercase">HELLO</span></p></body>`)
	if !strings.Contains(got, "hello") {
		t.Fatalf("rendered text = %q, want to contain hello", got)
	}
}

// TestTextTransformCapitalize uppercases the first letter of each word.
func TestTextTransformCapitalize(t *testing.T) {
	got := renderInlineText(t, `<body><p><span style="text-transform:capitalize">hello world</span></p></body>`)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Fatalf("rendered text = %q, want Hello and World capitalized", got)
	}
}

// TestTextTransformCapitalizeApostrophe confirms an intra-word apostrophe does not
// start a new word: "it's a test" capitalizes to "It's A Test", NOT "It'S A Test"
// (the letter after the apostrophe stays lowercase).
func TestTextTransformCapitalizeApostrophe(t *testing.T) {
	got := renderInlineText(t, `<body><p><span style="text-transform:capitalize">it's a test</span></p></body>`)
	if !strings.Contains(got, "It's") {
		t.Fatalf("rendered text = %q, want It's (apostrophe not a word boundary)", got)
	}
	if strings.Contains(got, "It'S") {
		t.Fatalf("rendered text = %q, apostrophe wrongly capitalized the following letter", got)
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "Test") {
		t.Fatalf("rendered text = %q, want A and Test capitalized", got)
	}
}

// TestTextTransformNoneUnchanged confirms the default (no transform) leaves the text
// case untouched — the byte-identical baseline. (Whitespace glyphs carry no Runes, so
// the collected rune stream is space-free; assert on the case-preserved words.)
func TestTextTransformNoneUnchanged(t *testing.T) {
	got := renderInlineText(t, `<body><p><span>Hello World</span></p></body>`)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Fatalf("rendered text = %q, want unchanged Hello and World", got)
	}
	if strings.Contains(got, "HELLO") || strings.Contains(got, "hello") {
		t.Fatalf("rendered text = %q, case should be unchanged", got)
	}
}
