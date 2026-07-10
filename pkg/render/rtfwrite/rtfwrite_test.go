package rtfwrite

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestEscapeRTF(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain ascii`, `plain ascii`},
		{`a\b{c}d`, `a\\b\{c\}d`},
		{"tab\there", `tab\tab here`},
		{"line\nbreak", `line\line break`},
		{"café", `caf\u233?`},
		{"日本", `\u26085?\u26412?`},
		// An astral rune becomes a UTF-16 surrogate pair (both units negative
		// in RTF's signed-16-bit \u form).
		{"🙂", `\u-10179?\u-8638?`},
	}
	for _, c := range cases {
		if got := escapeRTF(c.in); got != c.want {
			t.Errorf("escapeRTF(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteNilRoot(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(context.Background(), nil, &buf, Options{}); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, `{\rtf1\ansi`) || !strings.HasSuffix(strings.TrimSpace(out), "}") {
		t.Errorf("nil root must still produce a complete document:\n%s", out)
	}
	for _, want := range []string{`\fonttbl`, `\stylesheet`, `\paperw12240`, `\margl1440`} {
		if !strings.Contains(out, want) {
			t.Errorf("empty document missing %s:\n%s", want, out)
		}
	}
}

func TestImageDegradesToAltWithoutLoader(t *testing.T) {
	root := &cssbox.Box{
		Kind:    cssbox.BoxBlock,
		Display: cssbox.DisplayBlock,
		Children: []*cssbox.Box{{
			Kind:     cssbox.BoxReplaced,
			Display:  cssbox.DisplayInline,
			Replaced: &cssbox.ReplacedContent{Tag: "img", Attrs: map[string]string{"src": "http://x.test/a.png", "alt": "a picture"}},
		}},
	}
	var logged bool
	var buf bytes.Buffer
	err := Write(context.Background(), root, &buf, Options{Logf: func(string, ...any) { logged = true }})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !logged {
		t.Error("image degrade must be logged")
	}
	if !strings.Contains(buf.String(), "a picture") {
		t.Errorf("alt text missing:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), `\pict`) {
		t.Errorf("no loader: nothing should embed:\n%s", buf.String())
	}
}
