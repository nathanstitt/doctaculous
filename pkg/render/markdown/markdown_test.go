package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
)

// renderHTML parses src, builds a cssbox tree, and renders it to Markdown (or plain
// text if plain).
func renderHTML(t *testing.T, src string, plain bool) string {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := layoutcss.Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var sb strings.Builder
	if err := Write(root, &sb, Options{Plain: plain}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return sb.String()
}

func TestHeadings(t *testing.T) {
	got := renderHTML(t, `<html><body><h1>Title</h1><h2>Sub</h2><p>Body.</p></body></html>`, false)
	want := "# Title\n\n## Sub\n\nBody.\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestEmphasis(t *testing.T) {
	got := renderHTML(t, `<html><body><p>a <strong>bold</strong> and <em>italic</em> word</p></body></html>`, false)
	want := "a **bold** and _italic_ word\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestLink(t *testing.T) {
	got := renderHTML(t, `<html><body><p>see <a href="https://x.test/p">here</a>.</p></body></html>`, false)
	want := "see [here](https://x.test/p).\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInlineCode(t *testing.T) {
	got := renderHTML(t, `<html><body><p>run <code>go test</code> now</p></body></html>`, false)
	want := "run `go test` now\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestUnorderedList(t *testing.T) {
	got := renderHTML(t, `<html><body><ul><li>one</li><li>two</li></ul></body></html>`, false)
	want := "- one\n- two\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestOrderedList(t *testing.T) {
	got := renderHTML(t, `<html><body><ol><li>one</li><li>two</li></ol></body></html>`, false)
	want := "1. one\n2. two\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNestedList(t *testing.T) {
	got := renderHTML(t, `<html><body><ul><li>a<ul><li>a1</li></ul></li><li>b</li></ul></body></html>`, false)
	want := "- a\n  - a1\n- b\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestBlockquote(t *testing.T) {
	got := renderHTML(t, `<html><body><blockquote>quoted</blockquote></body></html>`, false)
	want := "> quoted\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestCodeBlock(t *testing.T) {
	got := renderHTML(t, "<html><body><pre>line1\nline2</pre></body></html>", false)
	want := "```\nline1\nline2\n```\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSimpleTable(t *testing.T) {
	got := renderHTML(t, `<html><body><table>
		<tr><th>A</th><th>B</th></tr>
		<tr><td>1</td><td>2</td></tr>
	</table></body></html>`, false)
	want := "| A | B |\n| --- | --- |\n| 1 | 2 |\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestColspanTable(t *testing.T) {
	// A colspan=2 cell duplicates its content across both columns.
	got := renderHTML(t, `<html><body><table>
		<tr><th>A</th><th>B</th></tr>
		<tr><td colspan="2">wide</td></tr>
	</table></body></html>`, false)
	want := "| A | B |\n| --- | --- |\n| wide | wide |\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRowspanTable(t *testing.T) {
	// A rowspan=2 cell duplicates down both rows.
	got := renderHTML(t, `<html><body><table>
		<tr><td rowspan="2">side</td><td>x</td></tr>
		<tr><td>y</td></tr>
	</table></body></html>`, false)
	want := "| side | x |\n| --- | --- |\n| side | y |\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStrikethrough(t *testing.T) {
	got := renderHTML(t, `<html><body><p>a <del>gone</del> and <s>old</s> word</p></body></html>`, false)
	want := "a ~~gone~~ and ~~old~~ word\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestImage(t *testing.T) {
	got := renderHTML(t, `<html><body><p>logo: <img src="/a b.png" alt="Our logo"></p></body></html>`, false)
	want := "logo: ![Our logo](/a%20b.png)\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestHorizontalRule(t *testing.T) {
	got := renderHTML(t, `<html><body><p>above</p><hr><p>below</p></body></html>`, false)
	want := "above\n\n---\n\nbelow\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestTaskList(t *testing.T) {
	got := renderHTML(t, `<html><body><ul>
		<li><input type="checkbox" checked> done</li>
		<li><input type="checkbox"> todo</li>
	</ul></body></html>`, false)
	want := "- [x] done\n- [ ] todo\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPlainText(t *testing.T) {
	got := renderHTML(t, `<html><body><h1>Title</h1><p>a <strong>bold</strong> <a href="u">link</a></p></body></html>`, true)
	want := "Title\n\na bold link\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStrikethroughSemantic(t *testing.T) {
	// <s>/<del> must strike via their semantic role even when author CSS overrides
	// the UA line-through (SemTag "s" — previously only honored by htmlwrite).
	got := renderHTML(t, `<html><body><p>old <s style="text-decoration: underline">gone</s> text</p></body></html>`, false)
	want := "old ~~gone~~ text\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	// The styled path (UA line-through) must keep working too.
	got = renderHTML(t, `<html><body><p>a <del>cut</del> word</p></body></html>`, false)
	want = "a ~~cut~~ word\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}
