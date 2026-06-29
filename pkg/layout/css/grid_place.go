package css

import gcss "github.com/nathanstitt/doctaculous/pkg/css"

// gridArea is a resolved item placement: 0-based half-open track ranges
// [colStart,colEnd) × [rowStart,rowEnd). Always normalized so end > start.
type gridArea struct {
	colStart, colEnd int
	rowStart, rowEnd int
}

func (a gridArea) colSpan() int { return a.colEnd - a.colStart }
func (a gridArea) rowSpan() int { return a.rowEnd - a.rowStart }

// placementInput is one item's placement request, read off its style.
type placementInput struct {
	placement gcss.GridPlacement // the four endpoints + optional area name
}

// gridDims carries the explicit grid size (the template track counts) so negative
// line numbers and "place in the last implicit row" resolve. explicitCols/Rows are
// the explicit track counts; areas is the parsed template-areas (may be empty).
type gridDims struct {
	explicitCols, explicitRows int
	areas                      gcss.GridAreas
}

// placeCursor is the §8.5 auto-placement cursor (0-based line positions).
type placeCursor struct{ col, row int }

// occupancy tracks which (row,col) cells are filled, growing implicitly. A map keyed
// by (row,col) avoids resizing a 2D slice as implicit tracks appear.
type occupancy struct {
	cells  map[[2]int]bool
	maxRow int // highest occupied row index + 1 (0 when empty); bounds the cursor walk
	maxCol int // highest occupied col index + 1 (0 when empty)
}

func newOccupancy() *occupancy { return &occupancy{cells: make(map[[2]int]bool)} }

// fill marks every cell of a (already-normalized) area occupied.
func (o *occupancy) fill(a gridArea) {
	for r := a.rowStart; r < a.rowEnd; r++ {
		for c := a.colStart; c < a.colEnd; c++ {
			o.cells[[2]int{r, c}] = true
		}
	}
	if a.rowEnd > o.maxRow {
		o.maxRow = a.rowEnd
	}
	if a.colEnd > o.maxCol {
		o.maxCol = a.colEnd
	}
}

// free reports whether every cell of a is unoccupied.
func (o *occupancy) free(a gridArea) bool {
	for r := a.rowStart; r < a.rowEnd; r++ {
		for c := a.colStart; c < a.colEnd; c++ {
			if o.cells[[2]int{r, c}] {
				return false
			}
		}
	}
	return true
}

// grow raises *cols/*rows to cover a's end lines (adds implicit tracks).
func grow(cols, rows *int, a gridArea) {
	if a.colEnd > *cols {
		*cols = a.colEnd
	}
	if a.rowEnd > *rows {
		*rows = a.rowEnd
	}
}

// placeItems resolves every item's gridArea using CSS Grid §8: definite placements
// first (explicit lines/spans/named areas), then auto-placement (§8.5) for the rest
// in `flow` order. flow is "row"|"column"|"row dense"|"column dense". It returns the
// resolved areas (item order) and the final grid size (cols, rows) AFTER any implicit
// tracks were added. It never panics; an unresolvable placement degrades to auto.
func placeItems(inputs []placementInput, dims gridDims, flow string) (areas []gridArea, cols, rows int) {
	dense := flow == "row dense" || flow == "column dense"
	columnFlow := flow == "column" || flow == "column dense"

	cols = dims.explicitCols
	rows = dims.explicitRows
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	areas = make([]gridArea, len(inputs))

	occ := newOccupancy()

	// Pass 1: items with a DEFINITE position on BOTH axes (and named areas). Mark them
	// occupied; grow cols/rows for implicit tracks they reach.
	autoMask := make([]bool, len(inputs))
	for i, in := range inputs {
		a, definite := resolveDefinite(in.placement, dims)
		if !definite {
			autoMask[i] = true
			continue
		}
		areas[i] = a
		grow(&cols, &rows, a)
		occ.fill(a)
	}

	// Pass 2: auto-placement for the rest, in document order, per flow + sparse/dense.
	cursor := placeCursor{}
	for i := range inputs {
		if !autoMask[i] {
			continue
		}
		a := autoPlace(inputs[i].placement, dims, &cursor, occ, columnFlow, dense, &cols, &rows)
		areas[i] = a
		occ.fill(a)
	}
	return areas, cols, rows
}

// resolveDefinite resolves a placement that is definite on BOTH axes (an explicit
// position the auto cursor never touches). It returns (area, true) for a named area
// that exists, or for a placement whose column AND row axes each resolve to a concrete
// range. Otherwise it returns (zero, false) and the item is auto-placed.
func resolveDefinite(p gcss.GridPlacement, dims gridDims) (gridArea, bool) {
	if p.AreaName != "" {
		if rect, ok := dims.areas.Named[p.AreaName]; ok {
			// GridRect is 1-based inclusive cells: col 1..2 inclusive → half-open [0,2).
			a := gridArea{
				colStart: rect.ColStart - 1, colEnd: rect.ColEnd,
				rowStart: rect.RowStart - 1, rowEnd: rect.RowEnd,
			}
			normalizeArea(&a)
			return a, true
		}
		// Named area not found: fall through to the endpoints (all auto for grid-area:
		// name) → not definite → auto-placed.
	}
	cl, ch, cdef := resolveAxis(p.ColStart, p.ColEnd, dims.explicitCols)
	rl, rh, rdef := resolveAxis(p.RowStart, p.RowEnd, dims.explicitRows)
	if !cdef || !rdef {
		return gridArea{}, false
	}
	a := gridArea{colStart: cl, colEnd: ch, rowStart: rl, rowEnd: rh}
	normalizeArea(&a)
	return a, true
}

// normalizeArea clamps an area to non-negative coordinates and ensures end > start.
func normalizeArea(a *gridArea) {
	if a.colStart < 0 {
		a.colStart = 0
	}
	if a.rowStart < 0 {
		a.rowStart = 0
	}
	if a.colEnd <= a.colStart {
		a.colEnd = a.colStart + 1
	}
	if a.rowEnd <= a.rowStart {
		a.rowEnd = a.rowStart + 1
	}
}

// lineIndex converts a 1-based grid line number (positive or negative) to a 0-based
// line index. A positive line k → k-1. A negative line -k → explicitCount+1-k as a
// line index (line -1 = the last line = explicitCount). The result is clamped ≥ 0.
func lineIndex(n, explicitCount int) int {
	var idx int
	if n > 0 {
		idx = n - 1
	} else { // n < 0 (n == 0 is rejected at parse time)
		idx = explicitCount + 1 + n // explicitCount+1-k where k=-n
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}

// resolveAxis resolves one axis (column or row) of a placement to a 0-based half-open
// range [lo, hi). definite is true only when the axis position is fully determined
// without the auto cursor (at least one endpoint is a concrete line number). A
// span paired with auto (or auto/auto) is not definite — the cursor decides the
// position, though the span is honored later by autoPlace.
func resolveAxis(start, end gcss.GridLine, explicitCount int) (lo, hi int, definite bool) {
	sNum := start.Kind == gcss.LineNum
	eNum := end.Kind == gcss.LineNum
	switch {
	case sNum && eNum:
		a := lineIndex(start.N, explicitCount)
		b := lineIndex(end.N, explicitCount)
		if a > b {
			a, b = b, a
		}
		lo = a
		hi = b
		if hi <= lo {
			hi = lo + 1
		}
		return lo, hi, true
	case sNum && end.Kind == gcss.LineSpan:
		lo = lineIndex(start.N, explicitCount)
		n := end.N
		if n < 1 {
			n = 1
		}
		return lo, lo + n, true
	case start.Kind == gcss.LineSpan && eNum:
		hi = lineIndex(end.N, explicitCount)
		n := start.N
		if n < 1 {
			n = 1
		}
		lo = hi - n
		if lo < 0 {
			lo = 0
			hi = lo + n
		}
		return lo, hi, true
	case sNum: // one line number + auto (or unrecognized) → a 1-track range at the line.
		lo = lineIndex(start.N, explicitCount)
		return lo, lo + 1, true
	case eNum:
		hi = lineIndex(end.N, explicitCount)
		lo = hi - 1
		if lo < 0 {
			lo = 0
			hi = lo + 1
		}
		return lo, hi, true
	default:
		// auto/auto or span/auto → position decided by auto-placement.
		return 0, 0, false
	}
}

// axisSpan returns the track count an axis spans, given its two endpoints. A `span n`
// endpoint contributes n; a line-number pair contributes its width; otherwise the
// default span is 1.
func axisSpan(start, end gcss.GridLine) int {
	if start.Kind == gcss.LineSpan && start.N > 0 {
		return start.N
	}
	if end.Kind == gcss.LineSpan && end.N > 0 {
		return end.N
	}
	if start.Kind == gcss.LineNum && end.Kind == gcss.LineNum {
		w := end.N - start.N
		if w < 0 {
			w = -w
		}
		if w >= 1 {
			return w
		}
	}
	return 1
}

// lockedLine returns the 0-based start line an axis is locked to (when one endpoint is
// a concrete line number), or -1 when the axis is unlocked (auto/span+auto). It is
// used by autoPlace for an item that is definite on the CROSS axis but auto on the
// flow axis.
func lockedLine(start, end gcss.GridLine, explicitCount int) int {
	if start.Kind == gcss.LineNum {
		return lineIndex(start.N, explicitCount)
	}
	if end.Kind == gcss.LineNum {
		// A span/auto-ending at a numbered line: the start is end - span.
		idx := lineIndex(end.N, explicitCount) - axisSpan(start, end)
		if idx < 0 {
			idx = 0
		}
		return idx
	}
	return -1
}

// autoPlace runs the §8.5 cursor walk for one auto-placed item. flow is along columns
// for row flow (columnFlow=false) and along rows for column flow (columnFlow=true).
// The flow axis has a fixed extent (the current cols/rows count) and wraps; the cross
// axis grows implicit tracks. cursor persists across items for sparse packing and is
// reset to the origin per item for dense packing.
func autoPlace(p gcss.GridPlacement, dims gridDims, cursor *placeCursor, occ *occupancy,
	columnFlow, dense bool, cols, rows *int) gridArea {

	colSpan := axisSpan(p.ColStart, p.ColEnd)
	rowSpan := axisSpan(p.RowStart, p.RowEnd)

	var flowSpan, crossSpan int
	var flowExtent, crossLocked int
	if columnFlow {
		// Flow axis = rows; cross axis = columns.
		flowSpan, crossSpan = rowSpan, colSpan
		flowExtent = *rows
		crossLocked = lockedLine(p.ColStart, p.ColEnd, dims.explicitCols)
	} else {
		// Flow axis = columns; cross axis = rows.
		flowSpan, crossSpan = colSpan, rowSpan
		flowExtent = *cols
		crossLocked = lockedLine(p.RowStart, p.RowEnd, dims.explicitRows)
	}
	if flowSpan < 1 {
		flowSpan = 1
	}
	if crossSpan < 1 {
		crossSpan = 1
	}
	// An item whose flow span exceeds the flow extent grows the flow axis to fit it
	// (CSS Grid §8.5: the implicit grid grows in the flow axis when a span needs it).
	if flowSpan > flowExtent {
		flowExtent = flowSpan
	}

	// Starting cursor position (flow, cross). Dense reconsiders earlier holes by
	// resetting to the grid origin; sparse continues from the persistent cursor.
	var flowPos, crossPos int
	if columnFlow {
		flowPos, crossPos = cursor.row, cursor.col
	} else {
		flowPos, crossPos = cursor.col, cursor.row
	}
	if dense {
		flowPos, crossPos = 0, 0
	}
	if crossLocked >= 0 {
		// Locked to a definite cross line: the cross position is fixed; only the flow
		// position is searched. Documented simplification: scan the locked line from
		// its start rather than continuing from the persistent sparse cursor. This
		// always yields a valid, non-overlapping placement; it differs from a browser
		// only in which free slot a sparse locked item lands in along its line — a niche
		// case we accept. Do NOT "fix" this back to cursor-continuation without a test.
		crossPos = crossLocked
		flowPos = 0
	}

	// Bound the walk so the loop is provably finite. For the UNLOCKED path the cross
	// axis never needs to exceed the highest occupied cross line plus this item's cross
	// span (beyond there is an empty band that always fits); crossBound caps that. The
	// LOCKED path does not advance crossPos at all — it terminates instead because the
	// locked line has finite occupancy and flowPos increases monotonically until a free
	// band is guaranteed (growing flowExtent if needed).
	maxOccCross := occ.maxRow
	if columnFlow {
		maxOccCross = occ.maxCol
	}
	crossBound := maxOccCross + crossSpan + flowExtent + 2

	for crossPos <= crossBound {
		if flowPos+flowSpan > flowExtent {
			// No room left in this flow line: wrap to the next cross line.
			if crossLocked >= 0 {
				// Locked cross line is full for this flow span: grow the flow axis so the
				// item still lands on its locked line (it cannot move to another line).
				flowExtent = flowPos + flowSpan
			} else {
				flowPos = 0
				crossPos++
				continue
			}
		}
		area := bandArea(columnFlow, flowPos, flowSpan, crossPos, crossSpan)
		if occ.free(area) {
			grow(cols, rows, area)
			if !dense {
				// Sparse: advance the cursor to the item's flow-end on its cross-start
				// line (§8.5).
				if columnFlow {
					cursor.row = flowPos + flowSpan
					cursor.col = crossPos
				} else {
					cursor.col = flowPos + flowSpan
					cursor.row = crossPos
				}
			}
			return area
		}
		flowPos++
	}

	// Degraded fallback (should be unreachable given crossBound): place at the cursor.
	area := bandArea(columnFlow, flowPos, flowSpan, crossPos, crossSpan)
	normalizeArea(&area)
	grow(cols, rows, area)
	return area
}

// bandArea builds a gridArea from flow/cross positions and spans for the given flow
// direction.
func bandArea(columnFlow bool, flowPos, flowSpan, crossPos, crossSpan int) gridArea {
	if columnFlow {
		// Flow = rows, cross = columns.
		return gridArea{
			colStart: crossPos, colEnd: crossPos + crossSpan,
			rowStart: flowPos, rowEnd: flowPos + flowSpan,
		}
	}
	// Flow = columns, cross = rows.
	return gridArea{
		colStart: flowPos, colEnd: flowPos + flowSpan,
		rowStart: crossPos, rowEnd: crossPos + crossSpan,
	}
}
