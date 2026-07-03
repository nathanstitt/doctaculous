package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func lowerDoc(t *testing.T, d *docx.Document) *lcssbox.Box {
	t.Helper()
	return Lower(d, style.NewResolver(d, nil))
}

func TestLowerTableStructure(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Table: &docx.Table{
			Grid: []docx.Twips{2000, 3000},
			Rows: []docx.TableRow{
				{Cells: []docx.TableCell{
					{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("A1")}}},
					{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("B1")}}},
				}},
			},
		}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if len(body.Children) != 1 {
		t.Fatalf("body children = %d, want 1 table", len(body.Children))
	}
	tbl := body.Children[0]
	if tbl.Display != lcssbox.DisplayTable {
		t.Fatalf("table Display = %v, want DisplayTable", tbl.Display)
	}
	if len(tbl.Children) != 1 || tbl.Children[0].Display != lcssbox.DisplayTableRow {
		t.Fatalf("table child = %+v, want one DisplayTableRow", tbl.Children)
	}
	row := tbl.Children[0]
	if len(row.Children) != 2 {
		t.Fatalf("row cells = %d, want 2", len(row.Children))
	}
	cell := row.Children[0]
	if cell.Display != lcssbox.DisplayTableCell {
		t.Fatalf("cell Display = %v, want DisplayTableCell", cell.Display)
	}
	// cell content recursion: a paragraph block holding a text box "A1".
	if len(cell.Children) != 1 || len(cell.Children[0].Children) != 1 || cell.Children[0].Children[0].Text != "A1" {
		t.Fatalf("cell content = %+v, want paragraph->text A1", cell.Children)
	}
}

func TestLowerTableVMergeToRowSpan(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Table: &docx.Table{
			Grid: []docx.Twips{2000},
			Rows: []docx.TableRow{
				{Cells: []docx.TableCell{{GridSpan: 1, VMerge: docx.VMergeRestart, Blocks: []docx.Block{{Paragraph: paraWith("m")}}}}},
				{Cells: []docx.TableCell{{GridSpan: 1, VMerge: docx.VMergeContinue, Blocks: []docx.Block{{Paragraph: paraWith("")}}}}},
			},
		}}},
	}
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	tbl := body.Children[0]
	if len(tbl.Children) != 2 {
		t.Fatalf("table rows = %d, want 2", len(tbl.Children))
	}
	// row 0 keeps its cell with RowSpan 2; row 1's continue cell is dropped.
	if got := tbl.Children[0].Children[0].RowSpan; got != 2 {
		t.Fatalf("restart cell RowSpan = %d, want 2", got)
	}
	if n := len(tbl.Children[1].Children); n != 0 {
		t.Fatalf("continue row cells = %d, want 0 (dropped)", n)
	}
}

// paraWith is a test helper building a one-run paragraph (empty text -> no run).
func paraWith(text string) *docx.Paragraph {
	p := &docx.Paragraph{}
	if text != "" {
		p.Content = []docx.ParaChild{{Run: &docx.Run{Text: text}}}
	}
	return p
}
