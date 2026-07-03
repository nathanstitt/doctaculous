package cssbox

import (
	"image/color"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
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

// tableDoc wraps a Table in a Document with a Letter section, mirroring the other
// table tests.
func tableDoc(tb *docx.Table) *docx.Document {
	return &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body:    []docx.Block{{Table: tb}},
	}
}

// firstTable lowers d and returns its single table box (body is the last child of
// the root wrapper).
func firstTable(t *testing.T, d *docx.Document) *lcssbox.Box {
	t.Helper()
	root := lowerDoc(t, d)
	body := root.Children[len(root.Children)-1]
	if len(body.Children) == 0 {
		t.Fatalf("body has no children, want a table")
	}
	return body.Children[0]
}

func TestLowerTableStyleMapping(t *testing.T) {
	red := color.RGBA{R: 0xFF, A: 0xFF}
	tblFill := color.RGBA{R: 0xDD, G: 0xDD, B: 0xDD, A: 0xFF}
	cellFill := color.RGBA{R: 0xEE, G: 0xEE, B: 0xEE, A: 0xFF}
	tb := &docx.Table{
		Grid: []docx.Twips{5000},
		Props: docx.TableProps{
			WidthDxa: 5000,
			Borders:  docx.BoxBorders{Top: docx.Border{SizeEighthPt: 8, Color: red, HasColor: true}},
			Shading:  docx.Shading{Fill: tblFill, HasFill: true},
		},
		Rows: []docx.TableRow{{Cells: []docx.TableCell{{
			GridSpan: 1,
			Props:    docx.CellProps{VAlign: docx.VAlignCenter, Shading: docx.Shading{Fill: cellFill, HasFill: true}},
			Blocks:   []docx.Block{{Paragraph: paraWith("A1")}},
		}}}},
	}
	tbl := firstTable(t, tableDoc(tb))

	// Table width: 5000 dxa -> 250pt, in points.
	if want := (gcss.Length{Value: docx.Twips(5000).Points(), Unit: gcss.UnitPt}); tbl.Style.Width != want {
		t.Errorf("table Width = %+v, want %+v", tbl.Style.Width, want)
	}
	if tbl.Style.Width.Value != 250 {
		t.Errorf("table Width value = %v, want 250", tbl.Style.Width.Value)
	}
	if tbl.Style.BorderCollapse != "collapse" {
		t.Errorf("table BorderCollapse = %q, want collapse", tbl.Style.BorderCollapse)
	}
	// Top border: sz 8 eighths -> 1.0pt, solid, red.
	if tbl.Style.BorderTopWidth != (gcss.Length{Value: 1.0, Unit: gcss.UnitPt}) {
		t.Errorf("table BorderTopWidth = %+v, want 1pt", tbl.Style.BorderTopWidth)
	}
	if tbl.Style.BorderTopStyle != "solid" {
		t.Errorf("table BorderTopStyle = %q, want solid", tbl.Style.BorderTopStyle)
	}
	if tbl.Style.BorderTopColor != red {
		t.Errorf("table BorderTopColor = %+v, want %+v", tbl.Style.BorderTopColor, red)
	}
	if tbl.Style.BackgroundColor != tblFill {
		t.Errorf("table BackgroundColor = %+v, want %+v", tbl.Style.BackgroundColor, tblFill)
	}

	cell := tbl.Children[0].Children[0]
	if cell.Style.VerticalAlign != "middle" {
		t.Errorf("cell VerticalAlign = %q, want middle", cell.Style.VerticalAlign)
	}
	if cell.Style.BackgroundColor != cellFill {
		t.Errorf("cell BackgroundColor = %+v, want %+v", cell.Style.BackgroundColor, cellFill)
	}
}

func TestLowerTableColSpan(t *testing.T) {
	tb := &docx.Table{
		Grid: []docx.Twips{2000, 2000, 2000},
		Rows: []docx.TableRow{{Cells: []docx.TableCell{{
			GridSpan: 3,
			Blocks:   []docx.Block{{Paragraph: paraWith("wide")}},
		}}}},
	}
	tbl := firstTable(t, tableDoc(tb))
	cell := tbl.Children[0].Children[0]
	if cell.ColSpan != 3 {
		t.Errorf("cell ColSpan = %d, want 3", cell.ColSpan)
	}
}

func TestLowerTablePercentageWidth(t *testing.T) {
	// OOXML pct is fiftieths of a percent, so 5000 == 100%.
	tb := &docx.Table{
		Grid:  []docx.Twips{5000},
		Props: docx.TableProps{WidthPct: 5000},
		Rows:  []docx.TableRow{{Cells: []docx.TableCell{{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("A1")}}}}}},
	}
	tbl := firstTable(t, tableDoc(tb))
	if tbl.Style.Width.Unit != gcss.UnitPercent {
		t.Errorf("table Width unit = %v, want UnitPercent", tbl.Style.Width.Unit)
	}
	if tbl.Style.Width.Value != 100 {
		t.Errorf("table Width value = %v, want 100", tbl.Style.Width.Value)
	}
}

func TestComputeRowSpansMultiColumn(t *testing.T) {
	// 2 columns x 2 rows; the SECOND column vertically merges (restart then
	// continue) while the FIRST column has normal cells in both rows. This proves
	// computeRowSpans matches the continue cell to the restart by grid COLUMN, not
	// by cell index (both merged cells are index 1 in their row).
	tb := &docx.Table{
		Grid: []docx.Twips{2000, 2000},
		Rows: []docx.TableRow{
			{Cells: []docx.TableCell{
				{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("r0c0")}}},
				{GridSpan: 1, VMerge: docx.VMergeRestart, Blocks: []docx.Block{{Paragraph: paraWith("merge")}}},
			}},
			{Cells: []docx.TableCell{
				{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("r1c0")}}},
				{GridSpan: 1, VMerge: docx.VMergeContinue, Blocks: []docx.Block{{Paragraph: paraWith("")}}},
			}},
		},
	}
	tbl := firstTable(t, tableDoc(tb))
	if len(tbl.Children) != 2 {
		t.Fatalf("table rows = %d, want 2", len(tbl.Children))
	}
	// Row 0: both cells kept; the 2nd (merged) cell spans 2 rows.
	row0 := tbl.Children[0]
	if len(row0.Children) != 2 {
		t.Fatalf("row 0 cells = %d, want 2", len(row0.Children))
	}
	if got := row0.Children[1].RowSpan; got != 2 {
		t.Errorf("row 0 col 1 RowSpan = %d, want 2", got)
	}
	// Row 1: the continue cell (col 1) is dropped; only the normal col-0 cell stays.
	row1 := tbl.Children[1]
	if len(row1.Children) != 1 {
		t.Fatalf("row 1 cells = %d, want 1 (continue dropped)", len(row1.Children))
	}
	if txt := row1.Children[0].Children[0].Children[0].Text; txt != "r1c0" {
		t.Errorf("row 1 surviving cell text = %q, want r1c0", txt)
	}
}

func TestComputeRowSpansGridSpanOffset(t *testing.T) {
	// Row 0: [gridSpan=2 cell][vMerge-restart cell] -> the restart cell sits at
	// grid column 2. Row 1: [normal][normal][vMerge-continue] -> the continue cell
	// sits at grid column 2. The continue must match the restart above by column,
	// which only works if the gridSpan advances the column tracking.
	tb := &docx.Table{
		Grid: []docx.Twips{1500, 1500, 1500},
		Rows: []docx.TableRow{
			{Cells: []docx.TableCell{
				{GridSpan: 2, Blocks: []docx.Block{{Paragraph: paraWith("span2")}}},
				{GridSpan: 1, VMerge: docx.VMergeRestart, Blocks: []docx.Block{{Paragraph: paraWith("merge")}}},
			}},
			{Cells: []docx.TableCell{
				{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("r1c0")}}},
				{GridSpan: 1, Blocks: []docx.Block{{Paragraph: paraWith("r1c1")}}},
				{GridSpan: 1, VMerge: docx.VMergeContinue, Blocks: []docx.Block{{Paragraph: paraWith("")}}},
			}},
		},
	}
	tbl := firstTable(t, tableDoc(tb))
	// Row 0's 2nd (restart) cell spans 2 rows.
	if got := tbl.Children[0].Children[1].RowSpan; got != 2 {
		t.Errorf("restart cell RowSpan = %d, want 2", got)
	}
	// Row 1 keeps 2 cells; the 3rd (continue) cell is dropped.
	row1 := tbl.Children[1]
	if len(row1.Children) != 2 {
		t.Fatalf("row 1 cells = %d, want 2 (continue dropped)", len(row1.Children))
	}
}
