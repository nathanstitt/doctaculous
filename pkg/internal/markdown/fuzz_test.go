package markdown

import (
	"strings"
	"testing"
)

// FuzzToHTML exercises the Markdown front end.
//
// The parsing is goldmark's, so this mostly tests an approved dependency rather
// than code in this repo — which is the point. The HTML front end taught us that
// a dependency's cost model is our problem: x/net/html turned out to be
// quadratic in tag nesting, a denial of service we could only fix by declining
// the input before handing it over. If goldmark has an equivalent shape, this is
// where it surfaces, and the answer would be the same kind of bound.
//
// The property under test is that ToHTML returns. Markdown has no invalid
// input — every byte sequence is *some* document — so an error is unexpected but
// acceptable; what is not acceptable is a panic, a hang, or output sized by
// something other than the input's length.
func FuzzToHTML(f *testing.F) {
	for _, s := range markdownSeeds() {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		_, _ = ToHTML(src)
	})
}

// markdownSeeds covers the constructs whose nesting or repetition drives
// allocation, plus the GFM extensions this converter enables.
func markdownSeeds() []string {
	return []string{
		"# Title\n\nA paragraph with *emphasis* and `code`.\n",
		"| a | b |\n| --- | --- |\n| 1 | 2 |\n", // GFM table
		"- [ ] task\n- [x] done\n",
		"~~strike~~ and https://example.com/ autolink\n",
		"```go\nfunc main() {}\n```\n",
		"> quote\n> > nested\n",
		"[ref][1]\n\n[1]: https://example.com/\n",
		"<div>raw html passthrough</div>\n",

		// Nesting: each of these is a recursive construct in a Markdown parser.
		strings.Repeat("> ", 50000) + "deep quote\n",
		strings.Repeat("- ", 50000) + "deep list\n",
		strings.Repeat("*", 50000) + "unclosed emphasis\n",
		strings.Repeat("[", 50000) + "unclosed link\n",
		strings.Repeat("[a](", 20000) + "x\n",
		strings.Repeat("#", 50000) + " heading\n",
		strings.Repeat("`", 50000) + "code\n",
		strings.Repeat("<div>", 20000) + "x\n",

		// Reference definitions and link labels: the resolver is a map lookup
		// over attacker-named keys.
		strings.Repeat("[a]: /x\n", 20000) + "[a]\n",
		"[" + strings.Repeat("a", 100000) + "]: /x\n",

		// Tables: column counts come from the delimiter row.
		"|" + strings.Repeat(" a |", 10000) + "\n|" + strings.Repeat(" --- |", 10000) + "\n",

		// Degenerates.
		"",
		"\n\n\n",
		"\x00\x01\x02",
		strings.Repeat("\n", 100000),
	}
}
