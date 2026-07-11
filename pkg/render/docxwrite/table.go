package docxwrite

import (
	"context"
	"image/color"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// table emits a DisplayTable box as a w:tbl: native gridSpan for column spans
// and vMerge restart/continue chains for row spans (the reader reconstructs the
// spans from exactly these). A caption becomes a Caption-styled paragraph above
// the table.
func (w *writer) table(ctx context.Context, b *cssbox.Box, out *[]docx.Block) error {
	if cap := captionOf(b); cap != nil {
		w.appendParagraph(out, cap, "Caption")
	}
	grid := boxwalk.BuildOccupancyGrid(b)
	if grid.Cols == 0 || len(grid.Slots) == 0 {
		return nil
	}

	// Fixed layout: the writer runs pre-layout, so content-driven widths are
	// not available; the page content width splits evenly across the columns (a
	// documented v1 approximation) and the grid widths are authoritative.
	tbl := &docx.Table{Props: docx.TableProps{LayoutFixed: true}}
	colTw := w.contentWidthTwips() / docx.Twips(grid.Cols)
	for c := 0; c < grid.Cols; c++ {
		tbl.Grid = append(tbl.Grid, colTw)
	}

	for r := range grid.Slots {
		row := docx.TableRow{Props: docx.RowProps{IsHeader: grid.HeaderRow[r]}}
		for c := 0; c < grid.Cols; {
			idx := grid.Slots[r][c]
			if idx < 0 {
				// A gap from a short row: an empty cell keeps the grid
				// rectangular (docx.Write supplies its empty paragraph).
				row.Cells = append(row.Cells, docx.TableCell{GridSpan: 1})
				c++
				continue
			}
			cell := grid.Cells[idx]
			tc, err := w.tableCell(ctx, cell, cell.Row == r)
			if err != nil {
				return err
			}
			row.Cells = append(row.Cells, tc)
			c += cell.ColSpan
		}
		tbl.Rows = append(tbl.Rows, row)
	}
	*out = append(*out, docx.Block{Table: tbl})
	return nil
}

// tableCell builds one w:tc. origin distinguishes the cell's first row (real
// content, vMerge restart when it spans rows) from a covered row below it (an
// explicit vMerge continue with no content — a bare vMerge would mean restart).
func (w *writer) tableCell(ctx context.Context, cell boxwalk.GridCell, origin bool) (docx.TableCell, error) {
	tc := docx.TableCell{GridSpan: cell.ColSpan}
	if cell.RowSpan > 1 {
		if origin {
			tc.VMerge = docx.VMergeRestart
		} else {
			tc.VMerge = docx.VMergeContinue
		}
	}
	tc.Props.Borders = cellBorders(cell.Box.Style)
	if bg := cell.Box.Style.BackgroundColor; bg.A != 0 {
		tc.Props.Shading = docx.Shading{Fill: bg, HasFill: true}
	}
	if !origin {
		return tc, nil
	}
	if boxwalk.HasInlineContent(cell.Box) {
		w.appendParagraph(&tc.Blocks, cell.Box, "")
	} else if err := w.children(ctx, cell.Box, &tc.Blocks); err != nil {
		return tc, err
	}
	return tc, nil
}

// cellBorders maps the cell box's computed borders to w:tcBorders (per-cell —
// the reader does not read table-level insideH/insideV). Border sizes are in
// eighths of a point.
func cellBorders(cs gcss.ComputedStyle) docx.BoxBorders {
	edge := func(widthPt float64, col color.RGBA, style string) docx.Border {
		if widthPt <= 0 || style == "none" || style == "hidden" || style == "" {
			return docx.Border{}
		}
		return docx.Border{
			Style:        "single",
			SizeEighthPt: int(widthPt*8 + 0.5),
			Color:        col,
			HasColor:     col.A != 0,
		}
	}
	return docx.BoxBorders{
		Top:    edge(lengthPt(cs.BorderTopWidth, cs.FontSizePt), cs.BorderTopColor, cs.BorderTopStyle),
		Left:   edge(lengthPt(cs.BorderLeftWidth, cs.FontSizePt), cs.BorderLeftColor, cs.BorderLeftStyle),
		Bottom: edge(lengthPt(cs.BorderBottomWidth, cs.FontSizePt), cs.BorderBottomColor, cs.BorderBottomStyle),
		Right:  edge(lengthPt(cs.BorderRightWidth, cs.FontSizePt), cs.BorderRightColor, cs.BorderRightStyle),
	}
}

// lengthPt resolves a border-width Length to points (px:pt 1:1 in this engine).
func lengthPt(l gcss.Length, fontSizePt float64) float64 {
	switch l.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return l.Value
	case gcss.UnitEm:
		return l.Value * fontSizePt
	default:
		return 0
	}
}

// contentWidthTwips is the section content width (page minus margins) in twips.
func (w *writer) contentWidthTwips() docx.Twips {
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
