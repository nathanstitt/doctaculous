package docxwrite

import (
	"context"
	"fmt"
	"image/color"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// table emits a DisplayTable box as a w:tbl: native gridSpan for column spans
// and vMerge restart/continue chains for row spans (the reader reconstructs the
// spans from exactly these). A caption becomes a Caption-styled paragraph above
// the table.
func (w *writer) table(ctx context.Context, b *cssbox.Box) error {
	if cap := captionOf(b); cap != nil {
		w.paragraph(cap, "Caption")
	}
	grid := boxwalk.BuildOccupancyGrid(b)
	if grid.Cols == 0 || len(grid.Slots) == 0 {
		return nil
	}

	w.body.WriteString("<w:tbl><w:tblPr><w:tblLayout w:type=\"fixed\"/></w:tblPr><w:tblGrid>")
	// Column widths: the writer runs pre-layout, so content-driven widths are
	// not available; the page content width splits evenly across the columns (a
	// documented v1 approximation).
	colTw := w.contentWidthTwips() / grid.Cols
	for c := 0; c < grid.Cols; c++ {
		fmt.Fprintf(&w.body, `<w:gridCol w:w="%d"/>`, colTw)
	}
	w.body.WriteString("</w:tblGrid>")

	for r := range grid.Slots {
		w.body.WriteString("<w:tr>")
		if grid.HeaderRow[r] {
			w.body.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
		}
		for c := 0; c < grid.Cols; {
			idx := grid.Slots[r][c]
			if idx < 0 {
				// A gap from a short row: an empty cell keeps the grid rectangular.
				w.body.WriteString("<w:tc><w:tcPr></w:tcPr><w:p/></w:tc>")
				c++
				continue
			}
			cell := grid.Cells[idx]
			if err := w.tableCell(ctx, cell, cell.Row == r); err != nil {
				return err
			}
			c += cell.ColSpan
		}
		w.body.WriteString("</w:tr>")
	}
	w.body.WriteString("</w:tbl>")
	return nil
}

// tableCell emits one w:tc. origin distinguishes the cell's first row (real
// content, vMerge restart when it spans rows) from a covered row below it (an
// explicit vMerge continue with an empty paragraph — a bare vMerge would mean
// restart).
func (w *writer) tableCell(ctx context.Context, cell boxwalk.GridCell, origin bool) error {
	w.body.WriteString("<w:tc><w:tcPr>")
	if cell.ColSpan > 1 {
		fmt.Fprintf(&w.body, `<w:gridSpan w:val="%d"/>`, cell.ColSpan)
	}
	if cell.RowSpan > 1 {
		if origin {
			w.body.WriteString(`<w:vMerge w:val="restart"/>`)
		} else {
			w.body.WriteString(`<w:vMerge w:val="continue"/>`)
		}
	}
	w.body.WriteString(cellBorders(cell.Box.Style))
	if bg := cell.Box.Style.BackgroundColor; bg.A != 0 {
		fmt.Fprintf(&w.body, `<w:shd w:val="clear" w:fill="%s"/>`, hexColor(bg))
	}
	w.body.WriteString("</w:tcPr>")

	if !origin {
		w.body.WriteString("<w:p/></w:tc>")
		return nil
	}
	before := w.body.Len()
	if boxwalk.HasInlineContent(cell.Box) {
		w.paragraph(cell.Box, "")
	} else if err := w.children(ctx, cell.Box); err != nil {
		return err
	}
	if w.body.Len() == before {
		// A w:tc must contain at least one block; an empty paragraph stands in.
		w.body.WriteString("<w:p/>")
	}
	w.body.WriteString("</w:tc>")
	return nil
}

// cellBorders maps the cell box's computed borders to w:tcBorders (per-cell —
// the reader does not read table-level insideH/insideV). Border sizes are in
// eighths of a point.
func cellBorders(cs gcss.ComputedStyle) string {
	edge := func(name string, widthPt float64, col color.RGBA, style string) string {
		if widthPt <= 0 || style == "none" || style == "hidden" || style == "" {
			return ""
		}
		return fmt.Sprintf(`<w:%s w:val="single" w:sz="%d" w:color="%s"/>`,
			name, int(widthPt*8+0.5), hexColor(col))
	}
	s := edge("top", lengthPt(cs.BorderTopWidth, cs.FontSizePt), cs.BorderTopColor, cs.BorderTopStyle) +
		edge("left", lengthPt(cs.BorderLeftWidth, cs.FontSizePt), cs.BorderLeftColor, cs.BorderLeftStyle) +
		edge("bottom", lengthPt(cs.BorderBottomWidth, cs.FontSizePt), cs.BorderBottomColor, cs.BorderBottomStyle) +
		edge("right", lengthPt(cs.BorderRightWidth, cs.FontSizePt), cs.BorderRightColor, cs.BorderRightStyle)
	if s == "" {
		return ""
	}
	return "<w:tcBorders>" + s + "</w:tcBorders>"
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

// hexColor renders an RGBA as the RRGGBB form OOXML colors use.
func hexColor(c color.RGBA) string {
	if c.A == 0 {
		return "auto"
	}
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

// contentWidthTwips is the section content width (page minus margins) in twips.
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
