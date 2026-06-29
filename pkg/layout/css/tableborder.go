package css

import (
	"image/color"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// edgeOwner ranks the box contributing a border in the collapse conflict-resolution
// tie-break: a border on the element closer to the cell wins (CSS 17.6.2.1). Lower
// value = closer to the cell = wins ties.
type edgeOwner int

const (
	ownerCell edgeOwner = iota
	ownerRow
	ownerRowGroup
	ownerColumn
	ownerColumnGroup
	ownerTable
)

// collapsedBorder is one candidate border for a shared grid edge.
type collapsedBorder struct {
	width float64
	style string // "none"/"hidden"/"solid"/"dashed"/"dotted"/"double"/...
	color color.RGBA
	owner edgeOwner
}

// styleRank ranks border styles for the collapse tie-break (CSS 17.6.2.1): at equal
// width a higher rank wins. double is highest, then solid/dashed/dotted/ridge/outset/
// groove/inset, then none. (hidden is handled separately — it suppresses the edge.)
func styleRank(style string) int {
	switch style {
	case "double":
		return 8
	case "solid":
		return 7
	case "dashed":
		return 6
	case "dotted":
		return 5
	case "ridge":
		return 4
	case "outset":
		return 3
	case "groove":
		return 2
	case "inset":
		return 1
	default: // "none" and unknown
		return 0
	}
}

// resolveCollapsedEdge picks the winning border between two adjacent candidates for a
// shared edge, per CSS 17.6.2.1: (1) a `hidden` border suppresses the edge; (2) the
// wider border wins; (3) higher style rank wins; (4) the owner closer to the cell
// wins. A "none" border loses to any real border.
func resolveCollapsedEdge(a, b collapsedBorder) collapsedBorder {
	if a.style == "hidden" || b.style == "hidden" {
		return collapsedBorder{style: "hidden"} // suppressed (width 0)
	}
	if a.style == "none" && b.style != "none" {
		return b
	}
	if b.style == "none" && a.style != "none" {
		return a
	}
	if a.width != b.width {
		if a.width > b.width {
			return a
		}
		return b
	}
	if ra, rb := styleRank(a.style), styleRank(b.style); ra != rb {
		if ra > rb {
			return a
		}
		return b
	}
	if a.owner <= b.owner {
		return a
	}
	return b
}

// borderStyleToLayout maps a CSS border-style string to a layout.BorderStyle for
// painting. The 3D styles (ridge/outset/groove/inset) map to their layout styles and are
// painted as bevels by the paint layer (shared with non-collapse borders via
// mapBorderStyle).
func borderStyleToLayout(style string) layout.BorderStyle {
	switch style {
	case "outset":
		return layout.BorderOutset
	case "inset":
		return layout.BorderInset
	case "ridge":
		return layout.BorderRidge
	case "groove":
		return layout.BorderGroove
	case "solid":
		return layout.BorderSolid
	case "dashed":
		return layout.BorderDashed
	case "dotted":
		return layout.BorderDotted
	case "double":
		return layout.BorderDouble
	default: // none/hidden -> none (not painted)
		return layout.BorderNone
	}
}

// cellEdge reads box b's border on side s (a layout.EdgeSide) as a collapsedBorder
// candidate owned by owner. A zero-width or "none"/missing style yields a none
// candidate (width 0).
func cellEdge(b *cssbox.Box, s layout.EdgeSide, owner edgeOwner) collapsedBorder {
	var w gcss.Length
	var style string
	var col color.RGBA
	switch s {
	case layout.EdgeTop:
		w, style, col = b.Style.BorderTopWidth, b.Style.BorderTopStyle, b.Style.BorderTopColor
	case layout.EdgeRight:
		w, style, col = b.Style.BorderRightWidth, b.Style.BorderRightStyle, b.Style.BorderRightColor
	case layout.EdgeBottom:
		w, style, col = b.Style.BorderBottomWidth, b.Style.BorderBottomStyle, b.Style.BorderBottomColor
	case layout.EdgeLeft:
		w, style, col = b.Style.BorderLeftWidth, b.Style.BorderLeftStyle, b.Style.BorderLeftColor
	}
	width, _ := resolveLen(w, b.Style.FontSizePt, 0)
	if style == "" {
		style = "none"
	}
	return collapsedBorder{width: width, style: style, color: col, owner: owner}
}

// buildCollapsedBorders resolves every shared edge of a border-collapse:collapse table
// into a flat list of border strips (layout.BorderItem) in page space, each centered on
// its grid line. cellAt maps a (row,col) slot to its originating cell (nil for an empty
// slot). It reads each cell's positioned fragment rect (gc.frag) for geometry. Returns
// nil if the grid is empty.
func (g *tableGrid) buildCollapsedBorders(cellAt func(r, c int) *gridCell) []layout.BorderItem {
	var out []layout.BorderItem
	emit := func(cbd collapsedBorder, x, y, w, h float64, side layout.EdgeSide) {
		if cbd.style == "hidden" || cbd.width <= 0 {
			return
		}
		ls := borderStyleToLayout(cbd.style)
		if ls == layout.BorderNone {
			return
		}
		// Center the strip on the grid line: for a vertical edge (Left/Right side) the
		// strip is width `cbd.width` centered on x; for a horizontal edge centered on y.
		switch side {
		case layout.EdgeLeft, layout.EdgeRight:
			out = append(out, layout.BorderItem{XPt: x - cbd.width/2, YPt: y, WPt: cbd.width, HPt: h, Color: cbd.color, Style: ls, Side: side})
		default: // Top/Bottom
			out = append(out, layout.BorderItem{XPt: x, YPt: y - cbd.width/2, WPt: w, HPt: cbd.width, Color: cbd.color, Style: ls, Side: side})
		}
	}
	for _, gc := range g.cells {
		if gc.frag == nil {
			continue
		}
		x, y, w, h := gc.frag.X, gc.frag.Y, gc.frag.W, gc.frag.H
		// LEFT edge of this cell, resolved against the left neighbor (or the table).
		left := cellEdge(gc.box, layout.EdgeLeft, ownerCell)
		if gc.col == 0 {
			left = resolveCollapsedEdge(left, cellEdge(g.table, layout.EdgeLeft, ownerTable))
		} else if nb := cellAt(gc.row, gc.col-1); nb != nil {
			left = resolveCollapsedEdge(left, cellEdge(nb.box, layout.EdgeRight, ownerCell))
		}
		emit(left, x, y, w, h, layout.EdgeLeft)
		// TOP edge, resolved against the cell above (or the table).
		top := cellEdge(gc.box, layout.EdgeTop, ownerCell)
		if gc.row == 0 {
			top = resolveCollapsedEdge(top, cellEdge(g.table, layout.EdgeTop, ownerTable))
		} else if nb := cellAt(gc.row-1, gc.col); nb != nil {
			top = resolveCollapsedEdge(top, cellEdge(nb.box, layout.EdgeBottom, ownerCell))
		}
		emit(top, x, y, w, h, layout.EdgeTop)
		// Outer RIGHT edge (last column).
		if gc.col+gc.colSpan == len(g.cols) {
			right := resolveCollapsedEdge(cellEdge(gc.box, layout.EdgeRight, ownerCell), cellEdge(g.table, layout.EdgeRight, ownerTable))
			emit(right, x+w, y, w, h, layout.EdgeRight)
		}
		// Outer BOTTOM edge (last row).
		if gc.row+gc.rowSpan == len(g.rows) {
			bot := resolveCollapsedEdge(cellEdge(gc.box, layout.EdgeBottom, ownerCell), cellEdge(g.table, layout.EdgeBottom, ownerTable))
			emit(bot, x, y+h, w, h, layout.EdgeBottom)
		}
	}
	return out
}
