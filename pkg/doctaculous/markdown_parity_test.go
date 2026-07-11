package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	docxcssbox "github.com/nathanstitt/doctaculous/pkg/docx/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	"github.com/nathanstitt/doctaculous/pkg/render/markdown"
)

// letterSection is a US-Letter DOCX section, matching the other DOCX tests.
var letterSection = docx.SectionProps{
	PageW: 12240, PageH: 15840,
	MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440,
}

// markdownOfDOCX lowers a DOCX document to its cssbox tree and renders Markdown,
// mirroring the reflow backend's WriteMarkdown path without the layout step.
func markdownOfDOCX(t *testing.T, d *docx.Document) string {
	t.Helper()
	root := docxcssbox.Lower(d, style.NewResolver(d, nil))
	var sb strings.Builder
	if err := markdown.Write(root, &sb, markdown.Options{}); err != nil {
		t.Fatalf("markdown.Write: %v", err)
	}
	return sb.String()
}

func markdownOfHTML(t *testing.T, src string) string {
	t.Helper()
	var out bytes.Buffer
	if err := convertHTMLToMarkdown(context.Background(), strings.NewReader(src), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("convertHTMLToMarkdown: %v", err)
	}
	return out.String()
}

// TestDOCXHTMLParityHeadingTable proves the single shared box-tree walker produces the
// same Markdown for a DOCX and an equivalent HTML document: a heading followed by a
// table. This is the core "one walker serves both" guarantee.
func TestDOCXHTMLParityHeadingTable(t *testing.T) {
	d := &docx.Document{
		Section: letterSection,
		Body: []docx.Block{
			{Paragraph: paraStyledText("Heading1", "Inventory")},
			{Table: &docx.Table{
				Grid: []docx.Twips{3000, 3000},
				Rows: []docx.TableRow{
					{Cells: []docx.TableCell{
						{Blocks: []docx.Block{{Paragraph: paraText("Item")}}},
						{Blocks: []docx.Block{{Paragraph: paraText("Qty")}}},
					}},
					{Cells: []docx.TableCell{
						{Blocks: []docx.Block{{Paragraph: paraText("Widgets")}}},
						{Blocks: []docx.Block{{Paragraph: paraText("5")}}},
					}},
				},
			}},
		},
	}
	gotDOCX := markdownOfDOCX(t, d)

	html := `<html><body>
		<h1>Inventory</h1>
		<table>
			<tr><td>Item</td><td>Qty</td></tr>
			<tr><td>Widgets</td><td>5</td></tr>
		</table>
	</body></html>`
	gotHTML := markdownOfHTML(t, html)

	if gotDOCX != gotHTML {
		t.Errorf("DOCX and HTML markdown differ:\n--- docx ---\n%s\n--- html ---\n%s", gotDOCX, gotHTML)
	}
	// Sanity: both actually produced the heading and table.
	if !strings.Contains(gotDOCX, "# Inventory") || !strings.Contains(gotDOCX, "| Item | Qty |") {
		t.Errorf("unexpected shared output:\n%s", gotDOCX)
	}
}

// paraText builds a plain paragraph with a single text run.
func paraText(text string) *docx.Paragraph {
	return &docx.Paragraph{Content: []docx.ParaChild{{Run: &docx.Run{Text: text}}}}
}

// paraStyledText builds a paragraph carrying a style id (e.g. "Heading1").
func paraStyledText(styleID, text string) *docx.Paragraph {
	p := paraText(text)
	p.Props.StyleID = styleID
	return p
}
