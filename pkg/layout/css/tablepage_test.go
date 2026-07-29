package css

import (
	"context"
	"strings"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeTable builds a table fragment as the CSS table engine produces it: the table
// fragment's direct children are CELL fragments (DisplayTableCell), two per row, each row
// a vertical band of height rowH stacked from y0. (The engine flattens rows into a grid of
// cell fragments; tableRowBands recovers the rows as Y bands.)
func makeTable(y0, rowH float64, rowCount int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable}
	t := &Fragment{Y: y0, H: float64(rowCount) * rowH, Box: box}
	for i := 0; i < rowCount; i++ {
		cy := y0 + float64(i)*rowH
		for c := 0; c < 2; c++ { // two cells per row, same Y band
			cb := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell}
			t.Children = append(t.Children, &Fragment{X: float64(c) * 50, Y: cy, W: 50, H: rowH, Box: cb})
		}
	}
	return t
}

func TestSplitTableBetweenRows(t *testing.T) {
	// 6 rows of 20pt at y0=0 (table 120pt), 2 cells each. Page bottom 65 ⇒ 3 rows fit
	// (bottoms 20/40/60). Head keeps rows 1-3 (6 cells), tail rows 4-6 (6 cells).
	tbl := makeTable(0, 20, 6)
	res := splitTableForPage(tbl, 65)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a table split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 6 {
		t.Errorf("head cells = %d, want 6 (3 rows × 2)", len(res.head.Children))
	}
	if len(res.tail.Children) != 6 {
		t.Errorf("tail cells = %d, want 6 (3 rows × 2)", len(res.tail.Children))
	}
	// The split lands cleanly on a row boundary: head ends at row 3's bottom (60), the tail
	// starts at row 4's top (60).
	if res.head.H != 60 {
		t.Errorf("head H = %.1f, want 60 (rows 1-3)", res.head.H)
	}
	if res.tail.Y != 60 {
		t.Errorf("tail Y = %.1f, want 60 (row 4 top)", res.tail.Y)
	}
}

// A rowspanning cell (a tall cell overlapping two row bands) keeps its rows together: the
// band merge means the splitter never cuts through it.
func TestSplitTableRowspanStaysWhole(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable}
	tbl := &Fragment{Y: 0, H: 80, Box: box}
	cell := func(x, y, h float64) *Fragment {
		return &Fragment{X: x, Y: y, W: 50, H: h, Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell}}
	}
	// Rows at y 0,20,40,60 (each 20 tall). Left column of rows 1-2 is a single rowspan=2
	// cell (y 0, h 40), so rows 1 and 2 merge into one band [0,40).
	tbl.Children = []*Fragment{
		cell(0, 0, 40), cell(50, 0, 20), // row1: rowspan cell + normal cell
		cell(50, 20, 20), // row2: right cell only (left is spanned)
		cell(0, 40, 20), cell(50, 40, 20),
		cell(0, 60, 20), cell(50, 60, 20),
	}
	// Page bottom 50 ⇒ band [0,40) fits, band [40,60) does not. The rowspan band is not cut.
	res := splitTableForPage(tbl, 50)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if res.head.H != 40 {
		t.Errorf("head H = %.1f, want 40 (the merged rowspan band)", res.head.H)
	}
	// Head holds the 3 cells of the merged band; tail holds the remaining 4.
	if len(res.head.Children) != 3 || len(res.tail.Children) != 4 {
		t.Errorf("split %d/%d cells, want 3/4", len(res.head.Children), len(res.tail.Children))
	}
}

// All rows fit ⇒ no split (head == tbl).
func TestSplitTableAllRowsFit(t *testing.T) {
	tbl := makeTable(0, 20, 3)
	res := splitTableForPage(tbl, 1000)
	if res.head != tbl || res.tail != nil {
		t.Errorf("all-fit should yield head=tbl,tail=nil; got head=%v tail=%v", res.head, res.tail)
	}
}

// The first row alone overflows the page ⇒ move the whole table (it overflows; a row is
// not split mid-cell).
func TestSplitTableFirstRowOverflows(t *testing.T) {
	tbl := makeTable(0, 100, 3)
	res := splitTableForPage(tbl, 50)
	if res.head != nil || res.tail != tbl {
		t.Errorf("first-row-overflow should move whole table; got head=%v tail=%v", res.head, res.tail)
	}
}

// lineSplittable accepts a table fragment (so the bucketer routes it to the table splitter).
func TestLineSplittableTable(t *testing.T) {
	tbl := makeTable(0, 20, 3)
	if !lineSplittable(tbl) {
		t.Errorf("a table fragment should be splittable")
	}
	// break-inside: avoid on the table disqualifies it.
	tbl.Box.Style = gcss.ComputedStyle{BreakInside: "avoid"}
	if lineSplittable(tbl) {
		t.Errorf("break-inside:avoid table must not be splittable")
	}
}

// TestSplitTableThroughOverTallCell is the N1c case: a table with a SINGLE row whose
// cell is taller than the page.
//
// There is no row boundary to break at, so the table previously moved whole and
// overflowed — the content past the page bottom was simply clipped. A cell's content
// is an ordinary fragment spine rather than an opaque nested formatting context, so the
// recursive splitter breaks it with no relayout.
func TestSplitTableThroughOverTallCell(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
	  body { margin: 0; font-family: serif; font-size: 16px; }
	  table { border-collapse: collapse; width: 300px; }
	  td { padding: 0; vertical-align: top; }
	</style></head><body>
	  <table><tr><td><p>` + strings.Repeat("word ", 300) + `</p></td></tr></table>
	</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, 400, 300)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if len(pages.Pages) < 2 {
		t.Fatalf("an over-tall cell should paginate: got %d page(s), want >= 2", len(pages.Pages))
	}
	// Every page carries text, and nothing is lost.
	total := 0
	for i, p := range pages.Pages {
		n := 0
		for _, it := range p.Items {
			if len(it.Glyph.Runes) > 0 {
				n++
			}
		}
		if n == 0 {
			t.Errorf("page %d has no glyphs", i)
		}
		total += n
	}
	if total < 1000 {
		t.Errorf("pages carry %d glyphs in total; content appears to have been lost", total)
	}
}

// TestSplitTableBetweenRowsStillPreferred pins that a multi-row table still breaks at a
// ROW boundary when one is available — splitting through cells is the fallback for a
// row that straddles, not the primary strategy.
func TestSplitTableBetweenRowsStillPreferred(t *testing.T) {
	tbl := &Fragment{Y: 0, H: 120, W: 100,
		Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable}}
	cellBox := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell}
	for r := 0; r < 3; r++ { // rows at y=0,40,80 — no lines, so no cell can split
		tbl.Children = append(tbl.Children,
			&Fragment{Y: float64(r) * 40, H: 40, W: 100, Box: cellBox})
	}
	res := splitTableForPage(tbl, 80)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split; got head=%v tail=%v", res.head, res.tail)
	}
	// The break lands on the row boundary at y=80, not inside a row.
	if res.tail.Y != 80 {
		t.Errorf("tail starts at y=%v, want 80 (the row boundary)", res.tail.Y)
	}
	if len(res.head.Children) != 2 || len(res.tail.Children) != 1 {
		t.Errorf("head=%d tail=%d cells, want 2 and 1",
			len(res.head.Children), len(res.tail.Children))
	}
}

// TestSplitTableUnsplittableRowMovesWhole: a straddling first row whose cells cannot
// break still moves whole rather than being clipped mid-cell.
func TestSplitTableUnsplittableRowMovesWhole(t *testing.T) {
	tbl := &Fragment{Y: 0, H: 200, W: 100,
		Box: &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable}}
	cellBox := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell}
	// One row, one cell, no lines and no children: nothing to split on.
	tbl.Children = append(tbl.Children, &Fragment{Y: 0, H: 200, W: 100, Box: cellBox})
	res := splitTableForPage(tbl, 100)
	if res.head != nil {
		t.Errorf("an unsplittable single row should move whole; got a head of h=%v", res.head.H)
	}
	if res.tail != tbl {
		t.Error("expected the original table as the tail")
	}
}
