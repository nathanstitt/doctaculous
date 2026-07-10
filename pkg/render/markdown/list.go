package markdown

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
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
	text := strings.TrimSpace(inner.inline(boxwalk.WithoutNestedLists(item)))
	text = boxwalk.StripMarkerPrefix(text, item.Marker)
	// GFM task list: a list item that leads with a checkbox becomes "- [ ]"/"- [x]".
	if checked, ok := boxwalk.LeadingCheckbox(item); ok && !w.opts.Plain {
		box := "[ ]"
		if checked {
			box = "[x]"
		}
		text = strings.TrimSpace(box + " " + text)
	}
	lines := []string{indent + marker + text}
	for _, c := range item.Children {
		if boxwalk.IsListContainer(c) {
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
	if boxwalk.IsOrderedMarker(t) {
		return t + " "
	}
	return "- "
}
