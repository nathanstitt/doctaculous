package docxwrite

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
)

// writeHTML builds a cssbox tree from src (the markdown writer's test pattern)
// and renders it to .docx bytes.
func writeHTML(t *testing.T, src string, opts Options) []byte {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := layoutcss.Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var buf bytes.Buffer
	if err := Write(context.Background(), root, &buf, opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

// reopen parses the produced package with the repo's own DOCX reader — the
// CI-enforced consumer every mapping must satisfy.
func reopen(t *testing.T, data []byte) *docx.Document {
	t.Helper()
	d, err := docx.OpenBytes(data)
	if err != nil {
		t.Fatalf("docx.OpenBytes rejects the produced package: %v", err)
	}
	return d
}

// paragraphs flattens the body's top-level paragraphs.
func paragraphs(d *docx.Document) []*docx.Paragraph {
	var out []*docx.Paragraph
	for _, b := range d.Body {
		if b.Paragraph != nil {
			out = append(out, b.Paragraph)
		}
	}
	return out
}

// textOf concatenates a paragraph's run text (including hyperlink runs).
func textOf(p *docx.Paragraph) string {
	var sb strings.Builder
	for _, c := range p.Content {
		if c.Run != nil {
			sb.WriteString(c.Run.Text)
		}
		if c.Hyperlink != nil {
			for _, r := range c.Hyperlink.Runs {
				sb.WriteString(r.Text)
			}
		}
	}
	return sb.String()
}

func TestHeadingsAndParagraph(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><h1>Title</h1><h2>Sub</h2><p>Body text.</p></body></html>`, Options{}))
	ps := paragraphs(d)
	if len(ps) != 3 {
		t.Fatalf("got %d paragraphs, want 3", len(ps))
	}
	if ps[0].Props.StyleID != "Heading1" || ps[1].Props.StyleID != "Heading2" || ps[2].Props.StyleID != "" {
		t.Errorf("pStyle ids = %q, %q, %q", ps[0].Props.StyleID, ps[1].Props.StyleID, ps[2].Props.StyleID)
	}
	if textOf(ps[0]) != "Title" || textOf(ps[2]) != "Body text." {
		t.Errorf("text lost: %q / %q", textOf(ps[0]), textOf(ps[2]))
	}
	// The Heading1 style must resolve to bold at a larger size through the
	// styles part (the visual half of the heading contract).
	if d.Styles == nil {
		t.Fatalf("no styles part")
	}
	h1 := d.Styles.ByID["Heading1"]
	if h1 == nil || !h1.Run.Bold || h1.Run.SizeHalfPts != 64 {
		t.Errorf("Heading1 style run props wrong: %+v", h1)
	}
}

func TestEmphasisRuns(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><p>a <strong>bold</strong> <em>ital</em> <s>gone</s> word</p></body></html>`, Options{}))
	ps := paragraphs(d)
	if len(ps) != 1 {
		t.Fatalf("got %d paragraphs, want 1", len(ps))
	}
	var bold, italic, strike bool
	for _, c := range ps[0].Content {
		if c.Run == nil {
			continue
		}
		switch strings.TrimSpace(c.Run.Text) {
		case "bold":
			bold = c.Run.Props.HasBold && c.Run.Props.Bold
		case "ital":
			italic = c.Run.Props.HasItalic && c.Run.Props.Italic
		case "gone":
			strike = c.Run.Props.HasStrike && c.Run.Props.Strike
		}
	}
	if !bold || !italic || !strike {
		t.Errorf("emphasis lost: bold=%v italic=%v strike=%v", bold, italic, strike)
	}
	if got := textOf(ps[0]); got != "a bold ital gone word" {
		t.Errorf("paragraph text = %q", got)
	}
}

func TestInlineCode(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><p>run <code>go test</code> now</p></body></html>`, Options{}))
	ps := paragraphs(d)
	var found bool
	for _, c := range ps[0].Content {
		if c.Run != nil && c.Run.Text == "go test" {
			found = true
			if c.Run.Props.StyleID != "CodeChar" {
				t.Errorf("code run rStyle = %q, want CodeChar", c.Run.Props.StyleID)
			}
			if c.Run.Props.Family != "Courier New" {
				t.Errorf("code run family = %q, want Courier New", c.Run.Props.Family)
			}
		}
	}
	if !found {
		t.Errorf("code run text lost")
	}
}

func TestHyperlink(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><p>see <a href="https://x.test/page">here</a>.</p></body></html>`, Options{}))
	ps := paragraphs(d)
	var link *docx.Hyperlink
	for _, c := range ps[0].Content {
		if c.Hyperlink != nil {
			link = c.Hyperlink
		}
	}
	if link == nil {
		t.Fatalf("hyperlink lost")
	}
	rel, ok := d.Rels[link.RelID]
	if !ok {
		t.Fatalf("hyperlink rel %q not in document rels", link.RelID)
	}
	if rel.Target != "https://x.test/page" || !rel.External {
		t.Errorf("hyperlink rel = %+v", rel)
	}
	if len(link.Runs) == 0 || link.Runs[0].Text != "here" {
		t.Errorf("hyperlink text lost: %+v", link.Runs)
	}
}

func TestLists(t *testing.T) {
	src := `<html><body>
	<ul><li>one</li><li>two<ul><li>inner</li></ul></li></ul>
	<ol><li>first</li><li>second</li></ol>
	<ol><li>again</li></ol>
	</body></html>`
	d := reopen(t, writeHTML(t, src, Options{}))
	ps := paragraphs(d)
	if len(ps) != 6 {
		t.Fatalf("got %d paragraphs, want 6", len(ps))
	}
	for i, p := range ps {
		if !p.Props.HasNum || p.Props.StyleID != "ListParagraph" {
			t.Errorf("item %d: not a numbered ListParagraph: %+v", i, p.Props)
		}
	}
	// Nesting depth carried in ilvl.
	if ps[2].Props.ILvl != 1 {
		t.Errorf("nested item ilvl = %d, want 1", ps[2].Props.ILvl)
	}
	// Bullets share one instance; each ordered list gets its own so numbering
	// restarts.
	if ps[0].Props.NumID != ps[1].Props.NumID {
		t.Errorf("bullet items diverge: %d vs %d", ps[0].Props.NumID, ps[1].Props.NumID)
	}
	if ps[3].Props.NumID == ps[0].Props.NumID {
		t.Errorf("ordered list shares the bullet instance")
	}
	if ps[5].Props.NumID == ps[3].Props.NumID {
		t.Errorf("two ordered lists share one instance (numbering would continue)")
	}
	// The numbering part must resolve formats.
	if d.Numbering == nil {
		t.Fatalf("no numbering part")
	}
	if lvl, ok := d.Numbering.Level(ps[0].Props.NumID, 0); !ok || lvl.Format != docx.NumFmtBullet {
		t.Errorf("bullet level = %+v ok=%v", lvl, ok)
	}
	if lvl, ok := d.Numbering.Level(ps[3].Props.NumID, 0); !ok || lvl.Format != docx.NumFmtDecimal || !strings.Contains(lvl.Text, "%1") {
		t.Errorf("decimal level = %+v ok=%v", lvl, ok)
	}
	// The item text must not carry a doubled marker.
	if got := textOf(ps[0]); got != "one" {
		t.Errorf("item text = %q, want %q (marker must be stripped)", got, "one")
	}
}

func TestCodeBlockOneParagraph(t *testing.T) {
	d := reopen(t, writeHTML(t, "<html><body><pre>line one\nline two</pre></body></html>", Options{}))
	ps := paragraphs(d)
	if len(ps) != 1 {
		t.Fatalf("got %d paragraphs, want 1 (a code block is a single paragraph)", len(ps))
	}
	if ps[0].Props.StyleID != "CodeBlock" {
		t.Errorf("pStyle = %q, want CodeBlock", ps[0].Props.StyleID)
	}
	var text []string
	var breaks int
	for _, c := range ps[0].Content {
		if c.Run == nil {
			continue
		}
		if c.Run.Break != docx.BreakNone {
			breaks++
			continue
		}
		text = append(text, c.Run.Text)
	}
	if breaks != 1 || strings.Join(text, "|") != "line one|line two" {
		t.Errorf("code block runs wrong: breaks=%d text=%v", breaks, text)
	}
}

func TestBlockquoteAndRule(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><blockquote><p>quoted</p></blockquote><hr></body></html>`, Options{}))
	ps := paragraphs(d)
	if len(ps) != 2 {
		t.Fatalf("got %d paragraphs, want 2", len(ps))
	}
	if ps[0].Props.StyleID != "Quote" || textOf(ps[0]) != "quoted" {
		t.Errorf("quote paragraph = %q %q", ps[0].Props.StyleID, textOf(ps[0]))
	}
	if ps[1].Props.StyleID != "HorizontalRule" {
		t.Errorf("hr paragraph style = %q", ps[1].Props.StyleID)
	}
}

func TestSectionGeometry(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><p>x</p></body></html>`,
		Options{PageWidthPt: 500, PageHeightPt: 700, MarginPt: 36}))
	if d.Section.PageW != 10000 || d.Section.PageH != 14000 {
		t.Errorf("page size = %dx%d twips, want 10000x14000", d.Section.PageW, d.Section.PageH)
	}
	if d.Section.MarginLeft != 720 || d.Section.MarginTop != 720 {
		t.Errorf("margins = %d/%d twips, want 720", d.Section.MarginLeft, d.Section.MarginTop)
	}
	// Defaults: Letter + 1in margins.
	d = reopen(t, writeHTML(t, `<html><body><p>x</p></body></html>`, Options{}))
	if d.Section.PageW != 12240 || d.Section.MarginLeft != 1440 {
		t.Errorf("default geometry = %d/%d, want 12240/1440", d.Section.PageW, d.Section.MarginLeft)
	}
}

func TestDeterministicOutput(t *testing.T) {
	src := `<html><body><h1>T</h1><ul><li>a</li></ul><p>see <a href="https://x.test">x</a></p></body></html>`
	a := writeHTML(t, src, Options{})
	b := writeHTML(t, src, Options{})
	if !bytes.Equal(a, b) {
		t.Errorf("two writes of the same tree differ (%d vs %d bytes)", len(a), len(b))
	}
}

func TestNilRootEmptyDocument(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(context.Background(), nil, &buf, Options{}); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	d := reopen(t, buf.Bytes())
	if len(paragraphs(d)) != 0 {
		t.Errorf("empty document has %d paragraphs", len(paragraphs(d)))
	}
}
