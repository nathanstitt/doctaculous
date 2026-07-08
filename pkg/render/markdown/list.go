package markdown

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// list renders a list container (a box whose children are DisplayListItem boxes) as one
// block: every item on its own line, nested lists indented beneath their parent item.
// Keeping the whole list in a single emitted chunk is what prevents a blank line from
// splitting consecutive items (Markdown would otherwise start a new list).
func (w *writer) list(container *cssbox.Box, depth int) {
	var lines []string
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		lines = append(lines, w.itemLines(item, depth)...)
	}
	w.emit(strings.Join(lines, "\n"))
}

// itemLines renders one list item to its lines: the marker line, then the lines of any
// nested list indented one level deeper. depth controls the leading indent.
func (w *writer) itemLines(item *cssbox.Box, depth int) []string {
	indent := strings.Repeat("  ", depth) // two spaces per level (GFM sub-list indent)
	marker := w.itemMarker(item)
	inner := &writer{opts: w.opts}
	text := strings.TrimSpace(inner.inline(withoutNestedLists(item)))
	text = stripMarkerPrefix(text, item.Marker)
	// GFM task list: a list item that leads with a checkbox becomes "- [ ]"/"- [x]".
	if checked, ok := leadingCheckbox(item); ok && !w.opts.Plain {
		box := "[ ]"
		if checked {
			box = "[x]"
		}
		text = strings.TrimSpace(box + " " + text)
	}
	lines := []string{indent + marker + text}
	for _, c := range item.Children {
		if isListContainer(c) {
			for _, sub := range c.Children {
				if sub.Display == cssbox.DisplayListItem {
					lines = append(lines, w.itemLines(sub, depth+1)...)
				}
			}
		}
	}
	return lines
}

// itemMarker returns the Markdown list marker for an item ("- " for a bullet, "N. "
// for an ordered item), derived from the resolved Box.Marker.
func (w *writer) itemMarker(b *cssbox.Box) string {
	if b.Marker == nil {
		return "- "
	}
	t := strings.TrimSpace(b.Marker.Text)
	if isOrderedMarker(t) {
		return t + " "
	}
	return "- "
}

// isOrderedMarker reports whether a marker string looks ordered (digits/roman/alpha
// followed by "." or ")"), as opposed to a bullet glyph.
func isOrderedMarker(t string) bool {
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

// withoutNestedLists returns a shallow copy of item whose children exclude nested list
// containers, so inline() renders just the item's own inline content (which, in the box
// tree, may sit inside an anonymous block wrapper — we keep that; only the nested list
// subtree is removed). The leading marker run that box generation prepends is stripped
// from the rendered string afterwards by stripMarkerPrefix.
func withoutNestedLists(item *cssbox.Box) *cssbox.Box {
	clone := *item
	clone.Children = nil
	for _, c := range item.Children {
		if isListContainer(c) {
			continue // nested list handled separately
		}
		clone.Children = append(clone.Children, c)
	}
	return &clone
}

// stripMarkerPrefix removes a leading marker string from an item's rendered inline text.
// Box generation prepends the marker as the item's first inline run (HTML via
// resolveCounters, DOCX via lowerListParagraph); inline() collapses whitespace, so the
// rendered text begins with the marker's non-space glyphs. We match against the marker's
// trimmed form to tolerate the collapsed trailing space.
func stripMarkerPrefix(text string, marker *cssbox.MarkerContent) string {
	if marker == nil {
		return text
	}
	m := strings.TrimSpace(marker.Text)
	if m != "" && strings.HasPrefix(text, m) {
		return strings.TrimSpace(text[len(m):])
	}
	return text
}

// leadingCheckbox reports whether a list item's content begins with a checkbox control
// (a <input type=checkbox>), and whether it is checked. It scans the item's non-list
// descendants for the first replaced control; a checkbox there marks a GFM task item.
func leadingCheckbox(item *cssbox.Box) (checked, ok bool) {
	var found *cssbox.ReplacedContent
	var walk func(b *cssbox.Box)
	walk = func(b *cssbox.Box) {
		if found != nil || isListContainer(b) {
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

// isListContainer reports whether a box is a list container (a block whose direct
// children include a list item) — used to detect a nested list inside an <li> and to
// dispatch a top-level list.
func isListContainer(b *cssbox.Box) bool {
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
