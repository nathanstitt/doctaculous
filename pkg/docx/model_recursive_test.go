package docx

import "testing"

// TestParagraphContentHoldsRuns verifies the new Content slice replaces Runs and
// that a bare run round-trips through a ParaChild.
func TestParagraphContentHoldsRuns(t *testing.T) {
	p := Paragraph{Content: []ParaChild{{Run: &Run{Text: "hi"}}}}
	if len(p.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(p.Content))
	}
	if p.Content[0].Run == nil || p.Content[0].Run.Text != "hi" {
		t.Fatalf("Content[0].Run = %+v, want Run{Text:hi}", p.Content[0].Run)
	}
}

// TestBlockHoldsTable verifies Block can carry a table.
func TestBlockHoldsTable(t *testing.T) {
	b := Block{Table: &Table{Rows: []TableRow{{Cells: []TableCell{{}}}}}}
	if b.Paragraph != nil {
		t.Fatalf("Paragraph = %+v, want nil", b.Paragraph)
	}
	if b.Table == nil || len(b.Table.Rows) != 1 {
		t.Fatalf("Table = %+v, want 1 row", b.Table)
	}
}

// TestTableCellHoldsBlocks verifies cell content recursion (cells hold blocks).
func TestTableCellHoldsBlocks(t *testing.T) {
	c := TableCell{Blocks: []Block{{Paragraph: &Paragraph{}}}}
	if len(c.Blocks) != 1 || c.Blocks[0].Paragraph == nil {
		t.Fatalf("cell blocks = %+v, want 1 paragraph block", c.Blocks)
	}
}
