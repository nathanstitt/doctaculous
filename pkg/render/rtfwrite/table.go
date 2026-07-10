package rtfwrite

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// table emits a DisplayTable box as \trowd/\cellx rows. Column and row spans
// are expanded by duplicating the spanned cell's content into every covered
// slot — the same strategy the Markdown and CSV writers use, so a table
// round-trips to the identical grid. Header rows carry \trhdr (the reader
// maps them to header cells; other viewers repeat them across pages). A
// caption becomes a bold paragraph above the table (the Markdown writer's
// bold-caption-line shape).
func (w *writer) table(b *cssbox.Box) error {
	if cap := captionOf(b); cap != nil {
		runs := w.inlineRuns(cap)
		for i := range runs {
			runs[i].Bold = true
		}
		w.emitParagraph(runs, "", justifyOf(cap))
	}
	grid := boxwalk.BuildOccupancyGrid(b)
	if grid.Cols == 0 || len(grid.Slots) == 0 {
		return nil
	}

	colTw := w.contentWidthTwips() / grid.Cols
	for r := range grid.Slots {
		w.body.WriteString(`\trowd\trgaph108`)
		if grid.HeaderRow[r] {
			w.body.WriteString(`\trhdr`)
		}
		for c := 0; c < grid.Cols; c++ {
			fmt.Fprintf(&w.body, `\cellx%d`, colTw*(c+1))
		}
		w.body.WriteString("\n")
		for c := 0; c < grid.Cols; c++ {
			idx := grid.Slots[r][c]
			if idx < 0 {
				// A gap from a short row: an empty cell keeps the grid rectangular.
				w.body.WriteString(`{\intbl\cell}`)
				continue
			}
			w.body.WriteString(`{\intbl `)
			w.writeRuns(w.inlineRuns(grid.Cells[idx].Box))
			w.body.WriteString(`\cell}`)
		}
		w.body.WriteString(`\row` + "\n")
	}
	return nil
}

// contentWidthTwips is the content width (page minus margins) in twips.
func (w *writer) contentWidthTwips() int {
	pageW := w.opts.PageWidthPt
	if pageW <= 0 {
		pageW = 612
	}
	margin := w.opts.MarginPt
	switch {
	case margin < 0:
		margin = 0
	case margin == 0:
		margin = 72
	}
	tw := twips(pageW - 2*margin)
	if tw < 20 {
		tw = 20
	}
	return tw
}

// captionOf returns the table's caption box, if any.
func captionOf(table *cssbox.Box) *cssbox.Box {
	for _, c := range table.Children {
		if c.Display == cssbox.DisplayTableCaption {
			return c
		}
	}
	return nil
}
