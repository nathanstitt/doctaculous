package htmlwrite

import (
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
)

// renderHTML parses src, builds a cssbox tree, and re-serializes it to HTML with opts.
func renderHTML(t *testing.T, src string, opts Options) string {
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
	if err := Write(root, &sb, opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return sb.String()
}

// frag renders src as a body-only HTML fragment.
func frag(t *testing.T, src string) string {
	t.Helper()
	return renderHTML(t, src, Options{Fragment: true})
}

func TestHeadings(t *testing.T) {
	got := frag(t, `<html><body><h1>Title</h1><h2>Sub</h2><p>Body.</p></body></html>`)
	want := "<h1>Title</h1>\n<h2>Sub</h2>\n<p>Body.</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestEmphasis(t *testing.T) {
	got := frag(t, `<html><body><p>a <strong>bold</strong> and <em>italic</em> word</p></body></html>`)
	want := "<p>a <strong>bold</strong> and <em>italic</em> word</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStrikethrough(t *testing.T) {
	got := frag(t, `<html><body><p>a <del>gone</del> and <s>old</s> word</p></body></html>`)
	want := "<p>a <s>gone</s> and <s>old</s> word</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestLink(t *testing.T) {
	got := frag(t, `<html><body><p>see <a href="https://x.test/p">here</a>.</p></body></html>`)
	want := `<p>see <a href="https://x.test/p">here</a>.</p>` + "\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInlineCode(t *testing.T) {
	got := frag(t, `<html><body><p>run <code>go test</code> now</p></body></html>`)
	want := "<p>run <code>go test</code> now</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestEscaping(t *testing.T) {
	got := frag(t, `<html><body><p>1 &lt; 2 &amp;&amp; 3 &gt; 0</p></body></html>`)
	want := "<p>1 &lt; 2 &amp;&amp; 3 &gt; 0</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestImage(t *testing.T) {
	got := frag(t, `<html><body><p>logo: <img src="/a.png" alt="Our &amp; logo"></p></body></html>`)
	want := `<p>logo: <img src="/a.png" alt="Our &amp; logo"></p>` + "\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestUnorderedList(t *testing.T) {
	got := frag(t, `<html><body><ul><li>one</li><li>two</li></ul></body></html>`)
	want := "<ul>\n  <li>one</li>\n  <li>two</li>\n</ul>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestOrderedList(t *testing.T) {
	got := frag(t, `<html><body><ol><li>one</li><li>two</li></ol></body></html>`)
	want := "<ol>\n  <li>one</li>\n  <li>two</li>\n</ol>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNestedList(t *testing.T) {
	got := frag(t, `<html><body><ul><li>a<ul><li>a1</li></ul></li><li>b</li></ul></body></html>`)
	want := "<ul>\n  <li>a\n    <ul>\n      <li>a1</li>\n    </ul>\n  </li>\n  <li>b</li>\n</ul>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestTaskList(t *testing.T) {
	got := frag(t, `<html><body><ul>
		<li><input type="checkbox" checked> done</li>
		<li><input type="checkbox"> todo</li>
	</ul></body></html>`)
	want := "<ul>\n" +
		`  <li><input type="checkbox" checked> done</li>` + "\n" +
		`  <li><input type="checkbox"> todo</li>` + "\n" +
		"</ul>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestBlockquote(t *testing.T) {
	got := frag(t, `<html><body><blockquote>quoted</blockquote></body></html>`)
	want := "<blockquote>\n  <p>quoted</p>\n</blockquote>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestCodeBlock(t *testing.T) {
	got := frag(t, "<html><body><pre>line1\nline2</pre></body></html>")
	want := "<pre><code>line1\nline2</code></pre>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestHorizontalRule(t *testing.T) {
	got := frag(t, `<html><body><p>above</p><hr><p>below</p></body></html>`)
	want := "<p>above</p>\n<hr>\n<p>below</p>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSimpleTable(t *testing.T) {
	got := frag(t, `<html><body><table>
		<tr><th>A</th><th>B</th></tr>
		<tr><td>1</td><td>2</td></tr>
	</table></body></html>`)
	want := "<table>\n" +
		"  <thead>\n    <tr>\n      <th>A</th>\n      <th>B</th>\n    </tr>\n  </thead>\n" +
		"  <tbody>\n    <tr>\n      <td>1</td>\n      <td>2</td>\n    </tr>\n  </tbody>\n" +
		"</table>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestColspanTable(t *testing.T) {
	// The colspan=2 cell is emitted once with colspan="2" — NOT duplicated.
	got := frag(t, `<html><body><table>
		<tr><th>A</th><th>B</th></tr>
		<tr><td colspan="2">wide</td></tr>
	</table></body></html>`)
	if !strings.Contains(got, `<td colspan="2">wide</td>`) {
		t.Errorf("missing colspan cell in:\n%s", got)
	}
	if n := strings.Count(got, "wide"); n != 1 {
		t.Errorf("spanned content duplicated (%d occurrences of %q) in:\n%s", n, "wide", got)
	}
}

func TestRowspanTable(t *testing.T) {
	// The rowspan=2 cell is emitted once with rowspan="2" — NOT duplicated down rows.
	got := frag(t, `<html><body><table>
		<tr><td rowspan="2">side</td><td>x</td></tr>
		<tr><td>y</td></tr>
	</table></body></html>`)
	if !strings.Contains(got, `<td rowspan="2">side</td>`) {
		t.Errorf("missing rowspan cell in:\n%s", got)
	}
	if n := strings.Count(got, "side"); n != 1 {
		t.Errorf("spanned content duplicated (%d occurrences of %q) in:\n%s", n, "side", got)
	}
}

func TestHeaderRowTable(t *testing.T) {
	got := frag(t, `<html><body><table>
		<tr><th>Name</th><th>Age</th></tr>
		<tr><td>Ann</td><td>30</td></tr>
	</table></body></html>`)
	if !strings.Contains(got, "<thead>") {
		t.Errorf("missing <thead> in:\n%s", got)
	}
	if !strings.Contains(got, "<th>Name</th>") || !strings.Contains(got, "<th>Age</th>") {
		t.Errorf("missing <th> header cells in:\n%s", got)
	}
	if !strings.Contains(got, "<td>Ann</td>") {
		t.Errorf("missing body <td> cell in:\n%s", got)
	}
}

func TestTableCaption(t *testing.T) {
	got := frag(t, `<html><body><table>
		<caption>Totals</caption>
		<tr><td>1</td></tr>
	</table></body></html>`)
	if !strings.Contains(got, "<caption>Totals</caption>") {
		t.Errorf("missing caption in:\n%s", got)
	}
}

func TestTableCellAlign(t *testing.T) {
	got := frag(t, `<html><body><table>
		<tr><td style="text-align:center">mid</td></tr>
	</table></body></html>`)
	if !strings.Contains(got, `<td style="text-align:center">mid</td>`) {
		t.Errorf("missing centered cell in:\n%s", got)
	}
}

func TestFullDocument(t *testing.T) {
	got := renderHTML(t, `<html><body><p>hi</p></body></html>`, Options{})
	want := "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n</head>\n<body>\n" +
		"<p>hi</p>\n" +
		"</body>\n</html>\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFragmentVsFull(t *testing.T) {
	src := `<html><body><p>hi</p></body></html>`
	full := renderHTML(t, src, Options{})
	fr := renderHTML(t, src, Options{Fragment: true})
	if strings.Contains(fr, "<!DOCTYPE") || strings.Contains(fr, "<body>") {
		t.Errorf("fragment should have no scaffold:\n%s", fr)
	}
	if !strings.Contains(full, "<!DOCTYPE html>") || !strings.Contains(full, "<body>") {
		t.Errorf("full document missing scaffold:\n%s", full)
	}
	if fr != "<p>hi</p>\n" {
		t.Errorf("unexpected fragment: %q", fr)
	}
}
