// Package boxwalk holds the format-neutral cssbox tree analysis shared by the markdown and htmlwrite writers.
package boxwalk

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// IsBlockContainer reports whether a box participates as a block-level box (so its
// content should be treated as one or more blocks, not inline).
func IsBlockContainer(b *cssbox.Box) bool {
	return b.Kind.IsBlockLevel()
}

// HasInlineContent reports whether a block box's children are inline-level (text /
// inline boxes), i.e. it forms a single paragraph rather than containing further
// blocks. A box with no children is treated as inline (an empty paragraph, dropped by
// the writers' inline serialization). A box mixing levels is normalized by box
// generation, so checking the first non-anonymous child is sufficient in practice; we
// scan for any block-level child to be safe.
func HasInlineContent(b *cssbox.Box) bool {
	if len(b.Children) == 0 {
		return false
	}
	for _, c := range b.Children {
		if c.Kind.IsBlockLevel() && c.Display != cssbox.DisplayInline {
			return false
		}
	}
	return true
}

// CollectRows returns the table's rows in document order (descending through row
// groups) along with a parallel slice flagging which rows are header rows (a row inside
// a DisplayTableHeaderGroup, or whose cells are all header cells).
func CollectRows(table *cssbox.Box) ([]*cssbox.Box, []bool) {
	var rows []*cssbox.Box
	var header []bool
	var walk func(b *cssbox.Box, inHeader bool)
	walk = func(b *cssbox.Box, inHeader bool) {
		for _, c := range b.Children {
			switch c.Display {
			case cssbox.DisplayTableRow:
				rows = append(rows, c)
				header = append(header, inHeader || RowIsAllHeader(c))
			case cssbox.DisplayTableHeaderGroup:
				walk(c, true)
			case cssbox.DisplayTableRowGroup, cssbox.DisplayTableFooterGroup:
				walk(c, false)
			}
		}
	}
	walk(table, false)
	return rows, header
}

// RowIsAllHeader reports whether every cell in a row is a header cell (a <th>).
func RowIsAllHeader(row *cssbox.Box) bool {
	cells := CellBoxesOf(row)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !IsHeaderCell(c) {
			return false
		}
	}
	return true
}

// IsHeaderCell reports whether a table cell is a header cell. HTML <th> gets
// font-weight:bold from the UA sheet; that alone is ambiguous, so we treat a cell as a
// header only when it is bold (the UA <th> signal). This is the pragmatic detector; a
// dedicated SemTag for <th> is a possible refinement.
func IsHeaderCell(cell *cssbox.Box) bool {
	return cell.Style.Bold
}

// CellBoxesOf returns the DisplayTableCell children of a row box.
func CellBoxesOf(row *cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	for _, c := range row.Children {
		if c.Display == cssbox.DisplayTableCell {
			out = append(out, c)
		}
	}
	return out
}

// ClampSpan reads a span value (0 = absent) as at least 1.
func ClampSpan(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// FilterEmpty returns the non-empty strings of in, order-preserving.
func FilterEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// IsListContainer reports whether a box is a list container (a block whose direct
// children include a list item) — used to detect a nested list inside an <li> and to
// dispatch a top-level list.
func IsListContainer(b *cssbox.Box) bool {
	if b.Display == cssbox.DisplayListItem {
		return false
	}
	for _, c := range b.Children {
		if c.Display == cssbox.DisplayListItem {
			return true
		}
	}
	return false
}

// WithoutNestedLists returns a shallow copy of item whose children exclude nested list
// containers, so the writers' inline serialization renders just the item's own inline
// content (which, in the box tree, may sit inside an anonymous block wrapper — we keep
// that; only the nested list subtree is removed). The leading marker run that box
// generation prepends is stripped from the rendered string afterwards by
// StripMarkerPrefix.
func WithoutNestedLists(item *cssbox.Box) *cssbox.Box {
	clone := *item
	clone.Children = nil
	for _, c := range item.Children {
		if IsListContainer(c) {
			continue // nested list handled separately
		}
		clone.Children = append(clone.Children, c)
	}
	return &clone
}

// StripMarkerPrefix removes a leading marker string from an item's rendered inline text.
// Box generation prepends the marker as the item's first inline run (HTML via
// resolveCounters, DOCX via lowerListParagraph); the writers' inline serialization
// collapses whitespace, so the rendered text begins with the marker's non-space glyphs. We
// match against the marker's trimmed form to tolerate the collapsed trailing space.
func StripMarkerPrefix(text string, marker *cssbox.MarkerContent) string {
	if marker == nil {
		return text
	}
	m := strings.TrimSpace(marker.Text)
	if m != "" && strings.HasPrefix(text, m) {
		return strings.TrimSpace(text[len(m):])
	}
	return text
}

// LeadingCheckbox reports whether a list item's content begins with a checkbox control
// (a <input type=checkbox>), and whether it is checked. It scans the item's non-list
// descendants for the first replaced control; a checkbox there marks a task-list item.
func LeadingCheckbox(item *cssbox.Box) (checked, ok bool) {
	var found *cssbox.ReplacedContent
	var walk func(b *cssbox.Box)
	walk = func(b *cssbox.Box) {
		if found != nil || IsListContainer(b) {
			return
		}
		if b.Kind == cssbox.BoxReplaced && b.Replaced != nil {
			found = b.Replaced
			return
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	for _, c := range item.Children {
		walk(c)
		if found != nil {
			break
		}
	}
	if found == nil || found.Control != cssbox.CtrlCheckbox {
		return false, false
	}
	_, checked = found.Attrs["checked"]
	return checked, true
}

// IsOrderedMarker reports whether a marker string looks ordered (digits/roman/alpha
// followed by "." or ")"), as opposed to a bullet glyph.
func IsOrderedMarker(t string) bool {
	if t == "" {
		return false
	}
	if !strings.HasSuffix(t, ".") && !strings.HasSuffix(t, ")") {
		return false
	}
	body := strings.TrimRight(t, ".)")
	if body == "" {
		return false
	}
	for _, r := range body {
		if !strings.ContainsRune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", r) {
			return false
		}
	}
	return true
}
