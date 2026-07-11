// Package csvwrite renders the TABLES of a cssbox tree (the shared box model
// produced by the HTML, DOCX, Markdown, XLSX, and PDF-extraction frontends) as
// delimiter-separated values. A spreadsheet is a grid: the document's tables
// are the content, emitted in document order (multiple tables are separated by
// one blank line), and non-table content is dropped with a logged count — a
// table-less document produces empty output and a loud log. Spanned cells are
// expanded by duplicating their text into every covered slot (CSV is
// rectangular), the same strategy the Markdown writer uses for GFM.
package csvwrite

import (
	"encoding/csv"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// Options configures CSV/TSV rendering.
type Options struct {
	// Comma is the field delimiter; 0 defaults to ',' (use '\t' for TSV).
	Comma rune
	// Logf receives degradation diagnostics (dropped non-table content, a
	// document with no tables). nil -> no-op.
	Logf func(string, ...any)
}

// Write renders the tables of the cssbox tree rooted at root to w. A nil root
// writes nothing.
func Write(root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Comma == 0 {
		opts.Comma = ','
	}
	tables, dropped := boxwalk.CollectTables(root)
	if len(tables) == 0 {
		opts.Logf("csvwrite: document has no tables; output is empty")
		return nil
	}
	if dropped > 0 {
		opts.Logf("csvwrite: dropped %d non-table block(s); CSV carries tables only", dropped)
	}

	cw := csv.NewWriter(w)
	cw.Comma = opts.Comma
	for i, table := range tables {
		if i > 0 {
			// A blank line between tables: flush, then write the separator raw
			// (an empty CSV record would be a quoted empty field).
			cw.Flush()
			if err := cw.Error(); err != nil {
				return err
			}
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if err := writeTable(cw, table); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeTable emits one table through the occupancy grid, duplicating spanned
// cells into every covered slot.
func writeTable(cw *csv.Writer, table *cssbox.Box) error {
	grid := boxwalk.BuildOccupancyGrid(table)
	if grid.Cols == 0 {
		return nil
	}
	// Render each origin cell's text once; covered slots reuse it.
	texts := make([]string, len(grid.Cells))
	for i, cell := range grid.Cells {
		texts[i] = boxwalk.CellPlainText(cell.Box)
	}
	record := make([]string, grid.Cols)
	for _, row := range grid.Slots {
		for c := 0; c < grid.Cols; c++ {
			if idx := row[c]; idx >= 0 {
				record[c] = texts[idx]
			} else {
				record[c] = ""
			}
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	return nil
}
