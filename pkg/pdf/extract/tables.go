package extract

import (
	"sort"
	"strings"
)

// This file detects tables from a page region and represents them as a cell grid.
// Two strategies are tried, auto-selected per region (matching pdfplumber /
// camelot's lattice vs. stream split):
//
//	LATTICE — recover a ruling grid from the captured rules (thin strokes / filled
//	          bars). Snap near-parallel edges together, take the intersections of the
//	          horizontal and vertical rules, and derive the minimal grid of cells.
//	          Words are assigned to the cell rectangle containing their center; a
//	          cell that spans several grid tracks becomes a colspan/rowspan. Used
//	          only when a >=2x2 ruling grid is present.
//
//	STREAM  — for a region with no rules, detect column boundaries from vertical
//	          whitespace gaps that recur (and align on word edges) across several
//	          lines. Rows are the lines; each word is assigned to the column its
//	          center falls in. Guarded so ordinary prose (which has no consistent
//	          multi-column gap structure) is not mistaken for a table.
//
// detect prefers lattice when a grid is found, else tries stream, else returns nil.

// cell is one table cell: the recovered text plus its span across grid tracks.
type cell struct {
	text     string
	colSpan  int // >=1
	rowSpan  int // >=1
	occupied bool
}

// table is a detected table as a dense grid of cells (rows × cols). Cells covered
// by a spanning neighbor carry occupied=false so the lowering step emits only the
// origin cell with its span.
type table struct {
	rows [][]cell
	cols int
	// yTop/yBottom bound the table region vertically, so the caller can excise the
	// table's lines from the surrounding flow.
	yTop, yBottom float64
}

// snapTol is how close (points) two parallel rule coordinates must be to snap
// together into one grid line. Producers draw a shared edge as two nearly-coincident
// strokes; snapping merges them.
const snapTol = 3.0

// minStreamCols / minStreamRows gate stream detection: a region needs at least this
// many consistent columns and rows to be called a table (below that it is prose).
const (
	minStreamCols = 2
	minStreamRows = 3
)

// detect attempts to recover a table from a region's lines and the page's rules.
// It prefers the lattice strategy when a ruling grid is present, else falls back to
// stream detection, else returns nil. logf, if non-nil, records which strategy
// fired.
func detect(lines []line, rules []rule, logf func(string, ...any)) *table {
	if t := detectLattice(lines, rules); t != nil {
		if logf != nil {
			logf("extract: table detected via lattice strategy (%dx%d)", len(t.rows), t.cols)
		}
		return t
	}
	if t := detectStream(lines); t != nil {
		if logf != nil {
			logf("extract: table detected via stream strategy (%dx%d)", len(t.rows), t.cols)
		}
		return t
	}
	return nil
}

// detectLattice recovers a table from ruling lines. It snaps the horizontal rules to
// a sorted set of row grid-lines (Y coordinates) and the vertical rules to column
// grid-lines (X coordinates), forms the cell rectangles between adjacent grid-lines,
// and assigns words to the cell containing their center. A grid must be at least
// 2x2 (>=3 row lines and >=3 column lines) to qualify. Returns nil otherwise.
func detectLattice(lines []line, rules []rule) *table {
	var hys, vxs []float64
	for _, r := range rules {
		if r.horizontal {
			hys = append(hys, r.a)
		} else {
			vxs = append(vxs, r.a)
		}
	}
	rowLines := snap(hys)
	colLines := snap(vxs)
	if len(rowLines) < 3 || len(colLines) < 3 {
		return nil // need at least a 2x2 grid of cells
	}
	nrows := len(rowLines) - 1
	ncols := len(colLines) - 1

	// Collect the words falling inside the grid's bounding rectangle.
	words := wordsInRegion(lines, rowLines[0], rowLines[len(rowLines)-1], colLines[0], colLines[len(colLines)-1])

	// Bucket each word into its (row,col) grid cell by its center.
	buckets := make([][]string, nrows*ncols)
	for _, w := range words {
		cx := (w.x0 + w.x1) / 2
		cy := w.y
		ci := trackIndex(colLines, cx)
		ri := trackIndex(rowLines, cy)
		if ri < 0 || ci < 0 {
			continue
		}
		buckets[ri*ncols+ci] = append(buckets[ri*ncols+ci], w.text)
	}

	// Build the dense grid. Spanning (a cell rectangle covering several tracks) is
	// inferred from missing interior rules: if the vertical rule between two adjacent
	// columns is absent at a given row band, the two cells merge (colspan); likewise
	// for rowspan. We approximate spans by scanning for adjacent empty cells that a
	// filled neighbor's rule does not separate. For robustness in this first cut we
	// build a plain grid (span 1) and then merge a run of empty cells to the left of
	// a filled cell into that cell's colspan when no vertical rule divides them.
	present := ruleCoverage(rules, rowLines, colLines)
	grid := make([][]cell, nrows)
	for ri := 0; ri < nrows; ri++ {
		grid[ri] = make([]cell, ncols)
		for ci := 0; ci < ncols; ci++ {
			grid[ri][ci] = cell{
				text:     strings.Join(buckets[ri*ncols+ci], " "),
				colSpan:  1,
				rowSpan:  1,
				occupied: true,
			}
		}
	}
	applySpans(grid, present, nrows, ncols)

	return &table{
		rows:    grid,
		cols:    ncols,
		yTop:    rowLines[0],
		yBottom: rowLines[len(rowLines)-1],
	}
}

// ruleCoverage records, for each interior grid line, whether a rule actually covers
// the segment between adjacent perpendicular grid lines. present.vert[ri][ci] is
// true when a vertical rule separates columns ci-1 and ci within row band ri;
// present.horiz[ri][ci] is true when a horizontal rule separates rows ri-1 and ri
// within column band ci. Missing coverage implies a merged (spanning) cell.
type coverage struct {
	vert  [][]bool // [row band][column line]
	horiz [][]bool // [row line][column band]
}

func ruleCoverage(rules []rule, rowLines, colLines []float64) coverage {
	nrows := len(rowLines) - 1
	ncols := len(colLines) - 1
	cov := coverage{
		vert:  make([][]bool, nrows),
		horiz: make([][]bool, len(rowLines)),
	}
	for i := range cov.vert {
		cov.vert[i] = make([]bool, len(colLines))
	}
	for i := range cov.horiz {
		cov.horiz[i] = make([]bool, ncols)
	}
	for _, r := range rules {
		if !r.horizontal {
			ci := nearestIndex(colLines, r.a)
			if ci < 0 {
				continue
			}
			for ri := 0; ri < nrows; ri++ {
				if overlaps(r.b0, r.b1, rowLines[ri], rowLines[ri+1]) {
					cov.vert[ri][ci] = true
				}
			}
		} else {
			ri := nearestIndex(rowLines, r.a)
			if ri < 0 {
				continue
			}
			for ci := 0; ci < ncols; ci++ {
				if overlaps(r.b0, r.b1, colLines[ci], colLines[ci+1]) {
					cov.horiz[ri][ci] = true
				}
			}
		}
	}
	return cov
}

// applySpans merges cells across missing interior rules into colspan/rowspan. A
// horizontal run of cells not separated by a covered vertical rule becomes one cell
// with colSpan; a vertical run not separated by a covered horizontal rule becomes
// rowSpan. Merged-away cells are marked occupied=false. This is the inverse of the
// grid builder: a producer that draws a spanned cell simply omits the interior rule.
func applySpans(grid [][]cell, cov coverage, nrows, ncols int) {
	// Column spans: scan each row left-to-right, extending an origin cell over
	// following columns whose left border (cov.vert at that column line) is absent.
	for ri := 0; ri < nrows; ri++ {
		ci := 0
		for ci < ncols {
			if !grid[ri][ci].occupied {
				ci++
				continue
			}
			span := 1
			for ci+span < ncols && !cov.vert[ri][ci+span] {
				// Merge the next column into this cell.
				if grid[ri][ci+span].text != "" {
					grid[ri][ci].text = strings.TrimSpace(grid[ri][ci].text + " " + grid[ri][ci+span].text)
				}
				grid[ri][ci+span].occupied = false
				span++
			}
			grid[ri][ci].colSpan = span
			ci += span
		}
	}
	// Row spans: scan each column top-to-bottom over the origin cells, extending over
	// following rows whose top border (cov.horiz at that row line) is absent.
	for ci := 0; ci < ncols; ci++ {
		ri := 0
		for ri < nrows {
			c := &grid[ri][ci]
			if !c.occupied {
				ri++
				continue
			}
			span := 1
			for ri+span < nrows && !cov.horiz[ri+span][ci] && grid[ri+span][ci].occupied {
				if grid[ri+span][ci].text != "" {
					c.text = strings.TrimSpace(c.text + " " + grid[ri+span][ci].text)
				}
				grid[ri+span][ci].occupied = false
				span++
			}
			c.rowSpan = span
			ri += span
		}
	}
}

// detectStream recovers a borderless (whitespace-separated) table from a region's
// lines. It finds column boundaries as vertical whitespace gaps that recur across
// most lines, then assigns each line's words to columns. A region qualifies only
// when at least minStreamCols columns are consistent across at least minStreamRows
// lines — the guard against treating prose as a table.
func detectStream(lines []line) *table {
	if len(lines) < minStreamRows {
		return nil
	}
	ls := make([]line, len(lines))
	copy(ls, lines)
	sort.Slice(ls, func(i, j int) bool { return ls[i].y < ls[j].y })

	bounds := columnBoundaries(ls)
	ncols := len(bounds) + 1
	if ncols < minStreamCols {
		return nil
	}

	// Assign words to columns; every line becomes a row.
	grid := make([][]cell, 0, len(ls))
	nonEmptyRows := 0
	for _, l := range ls {
		row := make([]cell, ncols)
		for ci := range row {
			row[ci] = cell{colSpan: 1, rowSpan: 1, occupied: true}
		}
		filled := 0
		for _, w := range l.words {
			cx := (w.x0 + w.x1) / 2
			// bucket cx into the column whose boundary interval contains it —
			// SearchFloat64s returns the insertion index (column i is between
			// bounds[i-1] and bounds[i]).
			ci := sort.SearchFloat64s(bounds, cx)
			if row[ci].text == "" {
				row[ci].text = w.text
			} else {
				row[ci].text += " " + w.text
			}
			filled++
		}
		if filled > 0 {
			nonEmptyRows++
		}
		grid = append(grid, row)
	}
	if nonEmptyRows < minStreamRows {
		return nil
	}
	// Require that the table is genuinely multi-column: at least two columns hold
	// content on some row (a single populated column is just a paragraph).
	if populatedColumns(grid) < minStreamCols {
		return nil
	}
	return &table{
		rows:    grid,
		cols:    ncols,
		yTop:    ls[0].y,
		yBottom: ls[len(ls)-1].y,
	}
}

// columnBoundaries finds the x-positions of column gutters shared across the
// region's lines. It builds, per line, the set of inter-word gaps, then keeps a
// gutter x only when a whitespace gap straddles it on a majority of lines AND no
// word crosses it on any line. The returned boundaries are sorted ascending.
func columnBoundaries(lines []line) []float64 {
	if len(lines) == 0 {
		return nil
	}
	// Candidate gutters: midpoints of every inter-word gap on every line.
	type cand struct {
		x     float64
		count int
	}
	var cands []cand
	addCand := func(x float64) {
		for i := range cands {
			if nearly(cands[i].x, x, snapTol) {
				cands[i].count++
				return
			}
		}
		cands = append(cands, cand{x: x, count: 1})
	}
	for _, l := range lines {
		for i := 0; i+1 < len(l.words); i++ {
			gap := l.words[i+1].x0 - l.words[i].x1
			if gap <= 0 {
				continue
			}
			mid := (l.words[i].x1 + l.words[i+1].x0) / 2
			// Only sizable gaps are gutter candidates (a normal word space is small
			// relative to the words' size).
			if gap >= 1.5*maxf(l.words[i].size, l.words[i+1].size) {
				addCand(mid)
			}
		}
	}
	majority := (len(lines) + 1) / 2
	var boundaries []float64
	for _, c := range cands {
		if c.count < majority {
			continue
		}
		if crossesAnyWord(lines, c.x) {
			continue // a real column gutter is never crossed by a word
		}
		boundaries = append(boundaries, c.x)
	}
	sort.Float64s(boundaries)
	return dedupeSorted(boundaries, snapTol)
}

// crossesAnyWord reports whether any word on any line straddles the x position (so
// x cannot be a column gutter).
func crossesAnyWord(lines []line, x float64) bool {
	for _, l := range lines {
		for _, w := range l.words {
			if w.x0 < x-0.01 && w.x1 > x+0.01 {
				return true
			}
		}
	}
	return false
}

// populatedColumns counts how many columns hold non-empty text in at least one row.
func populatedColumns(grid [][]cell) int {
	if len(grid) == 0 {
		return 0
	}
	ncols := len(grid[0])
	used := make([]bool, ncols)
	for _, row := range grid {
		for ci, c := range row {
			if strings.TrimSpace(c.text) != "" {
				used[ci] = true
			}
		}
	}
	n := 0
	for _, u := range used {
		if u {
			n++
		}
	}
	return n
}

// wordsInRegion returns the words whose baseline+center fall inside the rectangle
// [x0,x1]×[y0,y1].
func wordsInRegion(lines []line, y0, y1, x0, x1 float64) []word {
	var out []word
	for _, l := range lines {
		for _, w := range l.words {
			cx := (w.x0 + w.x1) / 2
			if w.y >= y0-snapTol && w.y <= y1+snapTol && cx >= x0-snapTol && cx <= x1+snapTol {
				out = append(out, w)
			}
		}
	}
	return out
}

// snap collapses a set of coordinates into sorted grid lines, merging values within
// snapTol into their mean. It returns the sorted, de-duplicated grid-line
// positions.
func snap(vals []float64) []float64 {
	if len(vals) == 0 {
		return nil
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	var out []float64
	sum, count := cp[0], 1
	for _, v := range cp[1:] {
		if v-sum/float64(count) <= snapTol {
			sum += v
			count++
		} else {
			out = append(out, sum/float64(count))
			sum, count = v, 1
		}
	}
	out = append(out, sum/float64(count))
	return out
}

// trackIndex returns the track (band between adjacent grid lines) that value v
// falls into, or -1 when v is outside [lines[0], lines[last]]. lines must be sorted
// ascending with len>=2.
func trackIndex(lines []float64, v float64) int {
	if len(lines) < 2 || v < lines[0]-snapTol || v > lines[len(lines)-1]+snapTol {
		return -1
	}
	for i := 0; i+1 < len(lines); i++ {
		if v <= lines[i+1] {
			return i
		}
	}
	return len(lines) - 2
}

// nearestIndex returns the index of the grid line closest to v, or -1 when none is
// within snapTol.
func nearestIndex(lines []float64, v float64) int {
	best, bestD := -1, snapTol
	for i, l := range lines {
		d := l - v
		if d < 0 {
			d = -d
		}
		if d <= bestD {
			best, bestD = i, d
		}
	}
	return best
}

// overlaps reports whether the interval [a0,a1] materially overlaps [b0,b1]. It
// requires a positive overlap larger than snapTol so a rule that only touches a
// band's boundary (a shared grid line) is not counted as spanning the band — that
// distinction is what lets an interior rule covering only some row bands create a
// colspan/rowspan in the others.
func overlaps(a0, a1, b0, b1 float64) bool {
	lo, hi := minmax(a0, a1)
	blo, bhi := minmax(b0, b1)
	overlap := minf(hi, bhi) - maxf(lo, blo)
	return overlap > snapTol
}

// minf returns the smaller of two float64s.
func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// dedupeSorted removes near-duplicate consecutive values from a sorted slice,
// keeping the first of each cluster.
func dedupeSorted(vals []float64, tol float64) []float64 {
	if len(vals) == 0 {
		return nil
	}
	out := []float64{vals[0]}
	for _, v := range vals[1:] {
		if v-out[len(out)-1] > tol {
			out = append(out, v)
		}
	}
	return out
}
