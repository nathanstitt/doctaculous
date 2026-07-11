package doctaculous

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mdGoldens are HTML fixtures rendered to Markdown and plain text and compared to
// committed .md / .txt goldens. Each exercises one distinct slice of the conversion
// (headings, emphasis, links, lists, blockquote, code, and the table matrix — simple,
// colspan, rowspan, combined spans, header, no-header, alignment, caption). Run with
// -update to regenerate, then eyeball every changed golden in review, especially the
// span-table cases (confirm the grid is rectangular and content is not lost).
var mdGoldens = []struct {
	name string
	html string
}{
	{"headings", `<html><body>
		<h1>Chapter One</h1>
		<p>Intro paragraph.</p>
		<h2>Section</h2>
		<p>More text.</p>
		<h3>Subsection</h3>
	</body></html>`},
	{"emphasis", `<html><body><p>Plain, <strong>bold</strong>, <em>italic</em>,
		<b>also bold</b>, <i>also italic</i>, and <code>inline code</code>.</p></body></html>`},
	{"links", `<html><body><p>Visit <a href="https://example.com/docs">the docs</a>
		or <a href="mailto:a@b.test">email us</a>. A <a>bare anchor</a> stays text.</p></body></html>`},
	{"lists-unordered", `<html><body><ul><li>first</li><li>second</li><li>third</li></ul></body></html>`},
	{"lists-ordered", `<html><body><ol><li>alpha</li><li>beta</li><li>gamma</li></ol></body></html>`},
	{"lists-nested", `<html><body><ul>
		<li>top A<ul><li>nested A1</li><li>nested A2</li></ul></li>
		<li>top B</li>
	</ul></body></html>`},
	{"blockquote", `<html><body>
		<p>Before.</p>
		<blockquote>A quoted line with <strong>emphasis</strong>.</blockquote>
		<p>After.</p>
	</body></html>`},
	{"code-block", "<html><body><pre>func main() {\n\tprintln(\"hi\")\n}</pre></body></html>"},
	{"table-simple", `<html><body><table>
		<tr><th>Name</th><th>Role</th></tr>
		<tr><td>Ada</td><td>Engineer</td></tr>
		<tr><td>Grace</td><td>Admiral</td></tr>
	</table></body></html>`},
	{"table-colspan", `<html><body><table>
		<tr><th>Q1</th><th>Q2</th><th>Q3</th></tr>
		<tr><td colspan="2">First half</td><td>Q3 only</td></tr>
	</table></body></html>`},
	{"table-rowspan", `<html><body><table>
		<tr><th>Region</th><th>City</th></tr>
		<tr><td rowspan="2">West</td><td>Portland</td></tr>
		<tr><td>Seattle</td></tr>
	</table></body></html>`},
	{"table-combined-span", `<html><body><table>
		<tr><th>A</th><th>B</th><th>C</th></tr>
		<tr><td rowspan="2" colspan="2">big</td><td>c1</td></tr>
		<tr><td>c2</td></tr>
	</table></body></html>`},
	{"table-no-header", `<html><body><table>
		<tr><td>x</td><td>y</td></tr>
		<tr><td>1</td><td>2</td></tr>
	</table></body></html>`},
	{"table-aligned", `<html><body><table>
		<tr>
			<th style="text-align:left">L</th>
			<th style="text-align:center">C</th>
			<th style="text-align:right">R</th>
		</tr>
		<tr><td>a</td><td>b</td><td>c</td></tr>
	</table></body></html>`},
	{"table-caption", `<html><body><table>
		<caption>Quarterly totals</caption>
		<tr><th>Q</th><th>Total</th></tr>
		<tr><td>Q1</td><td>100</td></tr>
	</table></body></html>`},
	{"table-thead", `<html><body><table>
		<thead><tr><td>H1</td><td>H2</td></tr></thead>
		<tbody><tr><td>a</td><td>b</td></tr></tbody>
	</table></body></html>`},
	{"strikethrough", `<html><body><p>Status: <del>pending</del> <strong>shipped</strong>,
		and an <s>outdated</s> note.</p></body></html>`},
	{"image", `<html><body>
		<p>Inline <img src="/icons/star.png" alt="star"> icon.</p>
		<p><img src="https://cdn.test/banner.png" alt="Banner image"></p>
	</body></html>`},
	{"horizontal-rule", `<html><body>
		<p>Section one.</p>
		<hr>
		<p>Section two.</p>
	</body></html>`},
	{"task-list", `<html><body><ul>
		<li><input type="checkbox" checked> Write the parser</li>
		<li><input type="checkbox"> Write the writer</li>
		<li><input type="checkbox"> Ship it</li>
	</ul></body></html>`},
}

func TestMarkdownGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range mdGoldens {
		t.Run(f.name, func(t *testing.T) {
			checkGolden(t, dir, "md-"+f.name+".md", f.html, MarkdownOptions{})
			checkGolden(t, dir, "md-"+f.name+".txt", f.html, MarkdownOptions{Plain: true})
		})
	}
}

// checkGolden converts src to markdown/text per opts and compares (or updates) the
// golden file at dir/name.
func checkGolden(t *testing.T, dir, name, src string, opts MarkdownOptions) {
	t.Helper()
	var out bytes.Buffer
	if err := convertHTMLToMarkdown(context.Background(), bytes.NewReader([]byte(src)), &out, opts); err != nil {
		t.Fatalf("convert: %v", err)
	}
	path := filepath.Join(dir, name)
	if *update {
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestMarkdownGolden -update", path)
	}
	if !bytes.Equal(want, out.Bytes()) {
		t.Errorf("output differs from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, out.Bytes(), want)
	}
}
