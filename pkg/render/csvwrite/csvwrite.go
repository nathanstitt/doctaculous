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
	"strings"

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
	var tables []*cssbox.Box
	dropped := 0
	if root != nil {
		collectTables(root, &tables, &dropped)
	}
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

// collectTables gathers DisplayTable boxes in document order, not descending
// into a found table (a nested table's text is already part of its host cell).
// It counts dropped non-table content blocks for the degradation log.
func collectTables(b *cssbox.Box, tables *[]*cssbox.Box, dropped *int) {
	if b.Display == cssbox.DisplayTable {
		*tables = append(*tables, b)
		return
	}
	if boxwalk.IsBlockContainer(b) && boxwalk.HasInlineContent(b) {
		*dropped++
		return
	}
	for _, c := range b.Children {
		collectTables(c, tables, dropped)
	}
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
		texts[i] = cellText(cell.Box)
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

// cellText renders a cell's content as one plain-text field, mirroring the
// Markdown writer's renderCell: each block child renders to a collapsed inline
// string (runs concatenated without separators, so styled runs inside a word do
// not split it), then blocks join with a space.
func cellText(cell *cssbox.Box) string {
	var parts []string
	if boxwalk.HasInlineContent(cell) {
		parts = append(parts, inlinePlain(cell))
	} else {
		for _, c := range cell.Children {
			parts = append(parts, inlinePlain(c))
		}
	}
	return strings.Join(boxwalk.FilterEmpty(parts), " ")
}

// inlinePlain flattens a box's inline subtree to collapsed plain text; an image
// contributes its alt text.
func inlinePlain(b *cssbox.Box) string {
	var runs []boxwalk.StyledRun
	boxwalk.CollectRuns(b, boxwalk.InlineState{}, func(rc *cssbox.ReplacedContent) string {
		return rc.Attrs["alt"]
	}, &runs)
	var sb strings.Builder
	for _, r := range runs {
		if r.Literal != "" {
			sb.WriteString(r.Literal)
			continue
		}
		sb.WriteString(r.Text)
	}
	return boxwalk.CollapseSpaces(sb.String())
}
