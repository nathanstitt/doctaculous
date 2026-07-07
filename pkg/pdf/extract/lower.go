package extract

import (
	"strconv"
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// This file is the top-level of the extractor: it drives Collect over every page,
// recovers tables/blocks, and assembles a cssbox.Box tree the downstream Markdown/
// HTML writers consume. The tree mirrors the DOCX/HTML lowering shape — an outer
// root block wrapping a single body block — so code that locates the document's
// top-level content as root.Children[last].Children works unchanged.

// Lower reconstructs a cssbox tree from a PDF's positioned glyphs and vector
// graphics. For each page it collects glyphs+rules, detects tables (excising their
// content from the text flow), orders the remaining lines by XY-cut, classifies
// each block, and appends the resulting boxes to one shared body block. logf, if
// non-nil, receives diagnostics (which table strategy fired, recovered panics); it
// may be nil. A nil document returns an error; a document with no recoverable text
// yields the empty root/body pair (never nil, never a panic).
func Lower(doc *pdf.Document, logf func(string, ...any)) (*cssbox.Box, error) {
	if doc == nil {
		return nil, errNilDocument
	}
	root := blockBox(cssbox.BlockFC)
	body := blockBox(cssbox.BlockFC)
	root.Children = []*cssbox.Box{body}

	for i := 0; i < doc.PageCount(); i++ {
		pc, err := Collect(doc, i, logf)
		if err != nil {
			// A page that cannot even be opened is skipped, not fatal (a batch of
			// pages must survive one bad page).
			if logf != nil {
				logf("extract: skipping page %d: %v", i, err)
			}
			continue
		}
		body.Children = append(body.Children, lowerPage(pc, logf)...)
	}
	return root, nil
}

// lowerPage turns one page's collected content into a slice of block boxes in
// reading order. Tables are detected first (over the whole page) and their lines
// removed from the flow; the remaining lines are ordered and classified.
func lowerPage(pc *pageContent, logf func(string, ...any)) []*cssbox.Box {
	lines := buildLines(pc.glyphs)
	if len(lines) == 0 {
		return nil
	}
	bodySize := bodyFontSize(lines)

	// Detect tables over the ruled region(s). A page may draw one table; we detect a
	// single lattice/stream table spanning the ruled area, then remove its lines.
	var out []*cssbox.Box
	tbl := detect(lines, pc.rules, logf)
	if tbl != nil {
		out = append(out, lowerTable(tbl))
		lines = removeTableLines(lines, tbl)
	}

	blocks := orderBlocks(lines, pc.width, pc.height, bodySize)
	for _, b := range blocks {
		out = append(out, lowerBlock(b))
	}
	return out
}

// removeTableLines drops the lines that fall within the detected table's vertical
// band, so they are not re-emitted as prose after being consumed by the table.
func removeTableLines(lines []line, tbl *table) []line {
	var kept []line
	for _, l := range lines {
		if l.y >= tbl.yTop-snapTol && l.y <= tbl.yBottom+snapTol {
			continue
		}
		kept = append(kept, l)
	}
	return kept
}

// lowerBlock lowers one classified block into a cssbox box: a heading, a list item,
// or a paragraph.
func lowerBlock(b block) *cssbox.Box {
	switch b.kind {
	case blockHeading:
		return lowerHeading(b)
	case blockListItem:
		return lowerListItem(b)
	default:
		return lowerParagraph(b)
	}
}

// lowerHeading builds a heading block: a block box carrying SemTag "h<n>" and
// HeadingLvl, with a single inline text child holding the heading text.
func lowerHeading(b block) *cssbox.Box {
	box := blockBox(cssbox.InlineFC)
	box.SemTag = "h" + strconv.Itoa(b.level)
	box.HeadingLvl = b.level
	box.Style.FontSizePt = b.lines[0].size
	box.Style.Bold = true // headings are conventionally bold; the writer reads Style.Bold
	box.Children = []*cssbox.Box{textBox(b.blockText(), box.Style)}
	return box
}

// lowerParagraph builds a paragraph block: a block box (inline formatting context)
// with a single inline text child holding the reflowed paragraph text. Bold/italic
// are set when the whole paragraph's words are uniformly bold/italic.
func lowerParagraph(b block) *cssbox.Box {
	box := blockBox(cssbox.InlineFC)
	box.SemTag = "p"
	if sz := b.lines[0].size; sz > 0 {
		box.Style.FontSizePt = sz
	}
	bold, italic := blockEmphasis(b)
	box.Style.Bold = bold
	box.Style.Italic = italic
	box.Children = []*cssbox.Box{textBox(b.blockText(), box.Style)}
	return box
}

// lowerListItem builds a list-item block: a DisplayListItem box carrying the
// resolved Marker, whose leading inline child is the marker text (the markdown list
// writer strips this prefix and reads Marker), followed by the item's text. This
// mirrors how box generation prepends the marker as the item's first inline run.
func lowerListItem(b block) *cssbox.Box {
	box := blockBox(cssbox.InlineFC)
	box.Display = cssbox.DisplayListItem
	box.Style.Display = "list-item"
	box.Marker = &cssbox.MarkerContent{Text: b.marker, Outside: true}
	if sz := b.lines[0].size; sz > 0 {
		box.Style.FontSizePt = sz
	}
	// Prepend the marker as the leading inline run, then the item text, so a writer
	// that reads inline content sees "- text" (and strips the marker via Marker).
	box.Children = []*cssbox.Box{
		textBox(b.marker, box.Style),
		textBox(b.blockText(), box.Style),
	}
	return box
}

// lowerTable lowers a detected table into a DisplayTable > DisplayTableRow >
// DisplayTableCell subtree matching what the Markdown table writer's buildGrid
// expects: each origin cell carries its ColSpan/RowSpan, and cells covered by a
// spanning neighbor are omitted.
func lowerTable(t *table) *cssbox.Box {
	tableBox := &cssbox.Box{
		Kind: cssbox.BoxBlock, Display: cssbox.DisplayTable, Formatting: cssbox.TableFC,
		Style: tableStyle(),
	}
	for _, row := range t.rows {
		rowBox := &cssbox.Box{
			Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableRow, Formatting: cssbox.TableFC,
			Style: gcss.InitialStyle(),
		}
		for _, c := range row {
			if !c.occupied {
				continue // covered by a spanning neighbor; the origin cell carries the span
			}
			cellBox := &cssbox.Box{
				Kind: cssbox.BoxBlock, Display: cssbox.DisplayTableCell, Formatting: cssbox.InlineFC,
				Style:   cellStyle(),
				ColSpan: c.colSpan,
				RowSpan: c.rowSpan,
			}
			cellBox.Children = []*cssbox.Box{textBox(strings.TrimSpace(c.text), cellBox.Style)}
			rowBox.Children = append(rowBox.Children, cellBox)
		}
		tableBox.Children = append(tableBox.Children, rowBox)
	}
	return tableBox
}

// blockEmphasis reports whether every word in a block is bold / italic, so a
// wholly-bold or wholly-italic paragraph carries that emphasis onto its box.
func blockEmphasis(b block) (bold, italic bool) {
	bold, italic = true, true
	any := false
	for _, l := range b.lines {
		for _, w := range l.words {
			any = true
			bold = bold && w.bold
			italic = italic && w.italic
		}
	}
	if !any {
		return false, false
	}
	return bold, italic
}

// blockBox builds a block-level box with the given formatting context and the CSS
// initial style (Display "block"), mirroring the DOCX lowering's newWrapper. Using
// InitialStyle (not a bare literal) keeps Width/Height at auto so the block is not
// collapsed to zero size by a downstream layout pass.
func blockBox(fc cssbox.FormattingContext) *cssbox.Box {
	st := gcss.InitialStyle()
	st.Display = "block"
	return &cssbox.Box{
		Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: fc, Style: st,
	}
}

// textBox builds an inline text leaf carrying text, inheriting the parent block's
// style (so font size / emphasis flow into the run) with Display "inline".
func textBox(text string, parent gcss.ComputedStyle) *cssbox.Box {
	st := parent
	st.Display = "inline"
	return &cssbox.Box{Kind: cssbox.BoxText, Text: text, Style: st, Display: cssbox.DisplayInline}
}

// tableStyle is the block ComputedStyle for a recovered table (initial values with
// Display "table"). A PDF gives no reliable border/width info to the extractor
// beyond the rules themselves, so we keep the visual style neutral; the conversion
// path reads only structure (rows/cells/spans).
func tableStyle() gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table"
	return cs
}

// cellStyle is the block ComputedStyle for a recovered table cell (initial values
// with Display "table-cell").
func cellStyle() gcss.ComputedStyle {
	cs := gcss.InitialStyle()
	cs.Display = "table-cell"
	return cs
}
