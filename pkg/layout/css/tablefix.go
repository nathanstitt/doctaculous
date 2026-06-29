package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupTables walks the whole tree and repairs every DisplayTable subtree per CSS
// 17.2.1 (anonymous table objects), so the grid builder can assume a well-formed
// table > (caption?) > (column/column-group)* > row-group > row > cell structure.
// Called from Build after normalize. Idempotent on a well-formed tree.
func fixupTables(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupTables(c)
	}
	if b.Display == cssbox.DisplayTable {
		fixupTable(b)
	}
}

// anonPart builds an anonymous table-part box of the given display with a fitting
// formatting context (a cell is a BlockFC container; structural parts are TableFC).
func anonPart(d cssbox.DisplayKind, kids []*cssbox.Box) *cssbox.Box {
	fc := cssbox.TableFC
	if d == cssbox.DisplayTableCell {
		fc = cssbox.BlockFC
	}
	return &cssbox.Box{Kind: cssbox.BoxAnonTablePart, Display: d, Formatting: fc, Children: kids}
}

// isWSText reports whether b is a text box that is entirely collapsible whitespace.
func isWSText(b *cssbox.Box) bool {
	return b.Kind == cssbox.BoxText && isAllWS(b.Text)
}

// isRowGroup reports whether d is one of the three row-group display kinds (body/header/footer).
func isRowGroup(d cssbox.DisplayKind) bool {
	return d == cssbox.DisplayTableRowGroup ||
		d == cssbox.DisplayTableHeaderGroup ||
		d == cssbox.DisplayTableFooterGroup
}

// isColumnPart reports whether d is a table-column or table-column-group.
func isColumnPart(d cssbox.DisplayKind) bool {
	return d == cssbox.DisplayTableColumn || d == cssbox.DisplayTableColumnGroup
}

// fixupTable repairs the direct children of a table box: captions and column parts
// stay as direct children; row-groups are recursed into (their rows/cells fixed);
// bare rows are gathered into an anonymous row-group; any other content (stray text,
// a stray cell, a block) is gathered into an anonymous row-group > row > cell. Inter-
// part whitespace is dropped.
func fixupTable(tbl *cssbox.Box) {
	var out []*cssbox.Box
	var looseRows []*cssbox.Box // bare table-row children awaiting an anon row-group
	var looseMisc []*cssbox.Box // non-row, non-group, non-caption, non-column children

	flushMisc := func() {
		if len(looseMisc) == 0 {
			return
		}
		cell := anonPart(cssbox.DisplayTableCell, looseMisc)
		row := anonPart(cssbox.DisplayTableRow, []*cssbox.Box{cell})
		looseRows = append(looseRows, row)
		looseMisc = nil
	}
	flushRows := func() {
		flushMisc()
		if len(looseRows) == 0 {
			return
		}
		out = append(out, anonPart(cssbox.DisplayTableRowGroup, looseRows))
		looseRows = nil
	}

	for _, c := range tbl.Children {
		switch {
		case isWSText(c):
			// drop inter-part whitespace
		case c.Display == cssbox.DisplayTableCaption:
			flushRows()
			out = append(out, c)
		case isColumnPart(c.Display):
			flushRows()
			out = append(out, c)
		case isRowGroup(c.Display):
			flushRows()
			fixupRowGroup(c)
			out = append(out, c)
		case c.Display == cssbox.DisplayTableRow:
			flushMisc()
			wrapStrayInRow(c)
			looseRows = append(looseRows, c)
		case c.Display == cssbox.DisplayTableCell:
			flushMisc()
			looseRows = append(looseRows, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{c}))
		default:
			looseMisc = append(looseMisc, c)
		}
	}
	flushRows()
	tbl.Children = out
}

// fixupRowGroup repairs a row-group: rows are recursed; bare cells/misc are gathered
// into anonymous rows; whitespace dropped.
func fixupRowGroup(rg *cssbox.Box) {
	var out []*cssbox.Box
	var misc []*cssbox.Box
	flushMisc := func() {
		if len(misc) == 0 {
			return
		}
		cell := anonPart(cssbox.DisplayTableCell, misc)
		out = append(out, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{cell}))
		misc = nil
	}
	for _, c := range rg.Children {
		switch {
		case isWSText(c):
			// drop
		case c.Display == cssbox.DisplayTableRow:
			flushMisc()
			wrapStrayInRow(c)
			out = append(out, c)
		case c.Display == cssbox.DisplayTableCell:
			flushMisc()
			out = append(out, anonPart(cssbox.DisplayTableRow, []*cssbox.Box{c}))
		default:
			misc = append(misc, c)
		}
	}
	flushMisc()
	rg.Children = out
}

// wrapStrayInRow replaces runs of non-cell children of a row with anonymous cells.
func wrapStrayInRow(row *cssbox.Box) {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, anonPart(cssbox.DisplayTableCell, run))
		run = nil
	}
	for _, c := range row.Children {
		switch {
		case isWSText(c):
			// drop inter-cell whitespace
		case c.Display == cssbox.DisplayTableCell:
			flush()
			out = append(out, c)
		default:
			run = append(run, c)
		}
	}
	flush()
	row.Children = out
}
