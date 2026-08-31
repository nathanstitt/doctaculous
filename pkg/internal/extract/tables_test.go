package extract

import "testing"

// wordAt builds a synthetic word centered so its center falls at (cx,y).
func wordAt(text string, cx, y float64) word {
	return word{text: text, x0: cx - 5, x1: cx + 5, y: y, size: 10}
}

// gridRules builds the rules for a lattice grid with the given vertical x-lines and
// horizontal y-lines (each a full-length segment across the opposite extent).
func gridRules(xs, ys []float64) []rule {
	var rs []rule
	y0, y1 := ys[0], ys[len(ys)-1]
	x0, x1 := xs[0], xs[len(xs)-1]
	for _, x := range xs {
		rs = append(rs, rule{horizontal: false, a: x, b0: y0, b1: y1})
	}
	for _, y := range ys {
		rs = append(rs, rule{horizontal: true, a: y, b0: x0, b1: x1})
	}
	return rs
}

func TestDetectLattice2x2(t *testing.T) {
	// A 2x2 grid: columns at x=0,50,100; rows at y=0,20,40.
	xs := []float64{0, 50, 100}
	ys := []float64{0, 20, 40}
	rules := gridRules(xs, ys)

	// One word centered in each of the four cells.
	lines := []line{
		{y: 10, x0: 0, x1: 100, words: []word{wordAt("A", 25, 10), wordAt("B", 75, 10)}},
		{y: 30, x0: 0, x1: 100, words: []word{wordAt("C", 25, 30), wordAt("D", 75, 30)}},
	}
	tbl := detect(lines, rules, nil)
	if tbl == nil {
		t.Fatal("detect returned nil, want a 2x2 lattice table")
	}
	if len(tbl.rows) != 2 || tbl.cols != 2 {
		t.Fatalf("grid = %dx%d, want 2x2", len(tbl.rows), tbl.cols)
	}
	want := [][]string{{"A", "B"}, {"C", "D"}}
	for r := range want {
		for c := range want[r] {
			if got := tbl.rows[r][c].text; got != want[r][c] {
				t.Errorf("cell[%d][%d] = %q, want %q", r, c, got, want[r][c])
			}
			if sp := tbl.rows[r][c].colSpan; sp != 1 {
				t.Errorf("cell[%d][%d] colSpan = %d, want 1", r, c, sp)
			}
		}
	}
}

func TestDetectLatticeColspan(t *testing.T) {
	// A 2-row, 2-col grid where the top row's interior vertical rule is absent, so the
	// top cell spans both columns (colspan=2).
	ys := []float64{0, 20, 40}
	var rules []rule
	// Horizontal rules: all three, full width.
	for _, y := range ys {
		rules = append(rules, rule{horizontal: true, a: y, b0: 0, b1: 100})
	}
	// Vertical rules: left and right edges span both rows; the MIDDLE vertical rule
	// only spans the BOTTOM row band (y=20..40), leaving the top row merged.
	rules = append(rules, rule{horizontal: false, a: 0, b0: 0, b1: 40})
	rules = append(rules, rule{horizontal: false, a: 100, b0: 0, b1: 40})
	rules = append(rules, rule{horizontal: false, a: 50, b0: 20, b1: 40})

	lines := []line{
		{y: 10, words: []word{wordAt("Header", 50, 10)}},
		{y: 30, words: []word{wordAt("L", 25, 30), wordAt("R", 75, 30)}},
	}
	tbl := detect(lines, rules, nil)
	if tbl == nil {
		t.Fatal("detect returned nil, want a lattice table with a colspan")
	}
	// Top-left origin cell should span 2 columns; its neighbor is unoccupied.
	if tbl.rows[0][0].colSpan != 2 {
		t.Errorf("top cell colSpan = %d, want 2", tbl.rows[0][0].colSpan)
	}
	if tbl.rows[0][1].occupied {
		t.Errorf("top-right cell should be covered by the colspan (occupied=false)")
	}
	if tbl.rows[0][0].text != "Header" {
		t.Errorf("spanned cell text = %q, want Header", tbl.rows[0][0].text)
	}
}

func TestDetectStreamColumns(t *testing.T) {
	// Three rows, two whitespace-aligned columns, no rules: stream detection.
	// Column 1 centered at x=20, column 2 centered at x=120 (wide gutter between).
	lines := []line{
		{y: 100, words: []word{wordAt("Name", 20, 100), wordAt("Age", 120, 100)}},
		{y: 120, words: []word{wordAt("Alice", 20, 120), wordAt("30", 120, 120)}},
		{y: 140, words: []word{wordAt("Bob", 20, 140), wordAt("25", 120, 140)}},
	}
	tbl := detect(lines, nil, nil)
	if tbl == nil {
		t.Fatal("detect returned nil, want a stream table")
	}
	if tbl.cols != 2 {
		t.Fatalf("stream cols = %d, want 2", tbl.cols)
	}
	if len(tbl.rows) != 3 {
		t.Fatalf("stream rows = %d, want 3", len(tbl.rows))
	}
	if tbl.rows[0][0].text != "Name" || tbl.rows[0][1].text != "Age" {
		t.Errorf("header row = %q,%q; want Name,Age", tbl.rows[0][0].text, tbl.rows[0][1].text)
	}
	if tbl.rows[1][0].text != "Alice" || tbl.rows[1][1].text != "30" {
		t.Errorf("data row = %q,%q; want Alice,30", tbl.rows[1][0].text, tbl.rows[1][1].text)
	}
}

func TestDetectStreamRejectsProse(t *testing.T) {
	// Ordinary prose lines (words with only normal single-space gaps, no consistent
	// gutter) must not be detected as a table.
	lines := []line{
		{y: 100, words: []word{wordAt("the", 10, 100), wordAt("quick", 25, 100)}},
		{y: 112, words: []word{wordAt("brown", 12, 112), wordAt("fox", 30, 112)}},
		{y: 124, words: []word{wordAt("jumps", 11, 124), wordAt("over", 28, 124)}},
	}
	if tbl := detect(lines, nil, nil); tbl != nil {
		t.Errorf("detect found a table in prose: %+v", tbl.rows)
	}
}

func TestDetectLatticeRejectsSmallGrid(t *testing.T) {
	// A single horizontal + single vertical rule is not a >=2x2 grid.
	rules := []rule{
		{horizontal: true, a: 10, b0: 0, b1: 100},
		{horizontal: false, a: 50, b0: 0, b1: 40},
	}
	if tbl := detectLattice(nil, rules); tbl != nil {
		t.Errorf("detectLattice accepted a sub-2x2 grid: %+v", tbl)
	}
}

func TestSnapMergesNearbyLines(t *testing.T) {
	// Two rules 1pt apart should snap to one grid line (< snapTol).
	got := snap([]float64{10, 10.5, 50})
	if len(got) != 2 {
		t.Fatalf("snap = %v, want 2 lines", got)
	}
	if got[0] < 10 || got[0] > 10.5 {
		t.Errorf("snapped line = %v, want ~10.25", got[0])
	}
}
