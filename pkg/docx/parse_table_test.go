package docx

import "testing"

func TestParseTableGridRowsCells(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="3000"/></w:tblGrid>
  <w:tr>
    <w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc>
    <w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc>
  </w:tr>
  <w:tr>
    <w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>span</w:t></w:r></w:p></w:tc>
  </w:tr>
</w:tbl>
</w:body></w:document>`)
	if len(doc.Body) != 1 || doc.Body[0].Table == nil {
		t.Fatalf("body = %+v, want 1 table block", doc.Body)
	}
	tb := doc.Body[0].Table
	if len(tb.Grid) != 2 || tb.Grid[0] != 2000 || tb.Grid[1] != 3000 {
		t.Fatalf("grid = %v, want [2000 3000]", tb.Grid)
	}
	if len(tb.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(tb.Rows))
	}
	if len(tb.Rows[0].Cells) != 2 {
		t.Fatalf("row0 cells = %d, want 2", len(tb.Rows[0].Cells))
	}
	// cell content recursion: the cell holds a paragraph block with text "A1".
	c := tb.Rows[0].Cells[0]
	if len(c.Blocks) != 1 || c.Blocks[0].Paragraph == nil {
		t.Fatalf("cell A1 blocks = %+v, want 1 paragraph", c.Blocks)
	}
	if got := c.Blocks[0].Paragraph.Content[0].Run.Text; got != "A1" {
		t.Fatalf("cell A1 text = %q, want A1", got)
	}
	// gridSpan
	if got := tb.Rows[1].Cells[0].GridSpan; got != 2 {
		t.Fatalf("span cell GridSpan = %d, want 2", got)
	}
}

func TestParseTableProps(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tblPr>
    <w:tblW w:type="dxa" w:w="5000"/>
    <w:jc w:val="center"/>
    <w:tblBorders>
      <w:top w:sz="8" w:color="FF0000"/>
      <w:bottom w:sz="8" w:color="FF0000"/>
    </w:tblBorders>
  </w:tblPr>
  <w:tr>
    <w:tc>
      <w:tcPr>
        <w:tcW w:type="dxa" w:w="2500"/>
        <w:vAlign w:val="center"/>
        <w:shd w:fill="EEEEEE"/>
      </w:tcPr>
      <w:p><w:r><w:t>c</w:t></w:r></w:p>
    </w:tc>
  </w:tr>
</w:tbl>
</w:body></w:document>`)
	tb := doc.Body[0].Table
	if tb.Props.WidthDxa != 5000 {
		t.Fatalf("table WidthDxa = %d, want 5000", tb.Props.WidthDxa)
	}
	if tb.Props.Justify != JustifyCenter {
		t.Fatalf("table Justify = %v, want center", tb.Props.Justify)
	}
	if tb.Props.Borders.Top.None || tb.Props.Borders.Top.SizeEighthPt != 8 {
		t.Fatalf("table top border = %+v, want sz 8", tb.Props.Borders.Top)
	}
	if !tb.Props.Borders.Top.HasColor || tb.Props.Borders.Top.Color.R != 0xFF {
		t.Fatalf("table top border color = %+v, want red", tb.Props.Borders.Top)
	}
	cell := tb.Rows[0].Cells[0]
	if cell.Props.WidthDxa != 2500 {
		t.Fatalf("cell WidthDxa = %d, want 2500", cell.Props.WidthDxa)
	}
	if cell.Props.VAlign != VAlignCenter {
		t.Fatalf("cell VAlign = %v, want center", cell.Props.VAlign)
	}
	if !cell.Props.Shading.HasFill || cell.Props.Shading.Fill.R != 0xEE {
		t.Fatalf("cell shading = %+v, want #EEEEEE", cell.Props.Shading)
	}
}

func TestParseTableVMerge(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:tbl>
  <w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p/></w:tc></w:tr>
  <w:tr><w:tc><w:tcPr><w:vMerge w:val="continue"/></w:tcPr><w:p/></w:tc></w:tr>
</w:tbl>
</w:body></w:document>`)
	tb := doc.Body[0].Table
	if tb.Rows[0].Cells[0].VMerge != VMergeRestart {
		t.Fatalf("row0 VMerge = %v, want restart", tb.Rows[0].Cells[0].VMerge)
	}
	if tb.Rows[1].Cells[0].VMerge != VMergeContinue {
		t.Fatalf("row1 VMerge = %v, want continue", tb.Rows[1].Cells[0].VMerge)
	}
}
