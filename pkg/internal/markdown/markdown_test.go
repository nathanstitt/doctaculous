package markdown

import (
	"strings"
	"testing"
)

// convert runs ToHTML and returns the output as a string.
func convert(t *testing.T, src string) string {
	t.Helper()
	out, err := ToHTML([]byte(src))
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	return string(out)
}

func TestToHTMLDocumentScaffold(t *testing.T) {
	got := convert(t, "hello")
	for _, want := range []string{"<!DOCTYPE html>", "<style>", DefaultCSS, "<body>", "<p>hello</p>", "</html>"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestToHTMLConstructs exercises each CommonMark/GFM construct the frontend
// must lower to markup the layout engine understands.
func TestToHTMLConstructs(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"headings", "# One\n\n###### Six\n", []string{"<h1>One</h1>", "<h6>Six</h6>"}},
		{"emphasis", "*em* **strong** ~~gone~~\n", []string{"<em>em</em>", "<strong>strong</strong>", "<del>gone</del>"}},
		{"link", "[text](https://x.test)\n", []string{`<a href="https://x.test">text</a>`}},
		{"autolink", "visit https://auto.test now\n", []string{`<a href="https://auto.test">https://auto.test</a>`}},
		{"image", `![alt words](img.png "t")` + "\n", []string{`<img src="img.png" alt="alt words"`}},
		{"blockquote", "> quoted line\n", []string{"<blockquote>", "quoted line"}},
		{"fenced code", "```go\nx := 1\n```\n", []string{"<pre><code", "x := 1"}},
		{"inline code", "use `f()` here\n", []string{"<code>f()</code>"}},
		{"unordered list", "- a\n- b\n", []string{"<ul>", "<li>a</li>"}},
		{"ordered list", "1. a\n2. b\n", []string{"<ol>", "<li>a</li>"}},
		{"nested list", "- a\n  - inner\n", []string{"<ul>", "inner"}},
		{"task list", "- [x] done\n- [ ] todo\n", []string{`type="checkbox"`, "checked", "done", "todo"}},
		{"thematic break", "a\n\n---\n\nb\n", []string{"<hr>"}},
		// goldmark emits GFM column alignment as an inline style, which flows
		// straight through the cascade to Style.TextAlign (and back out as
		// ---:/:---: in the Markdown writer).
		{"table", "| A | B |\n| --- | ---: |\n| 1 | 2 |\n", []string{"<table>", "<th>A</th>", "text-align:right", "<td>1</td>"}},
		{"raw html passthrough", "<div class=\"keep\">raw</div>\n", []string{`<div class="keep">raw</div>`}},
	}
	for _, c := range cases {
		got := convert(t, c.src)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: output missing %q:\n%s", c.name, want, got)
			}
		}
	}
}

func TestToHTMLEscapesText(t *testing.T) {
	got := convert(t, "a < b & c\n")
	if !strings.Contains(got, "a &lt; b &amp; c") {
		t.Errorf("text not escaped:\n%s", got)
	}
}
