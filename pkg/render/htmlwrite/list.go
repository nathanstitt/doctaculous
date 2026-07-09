package htmlwrite

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// list renders a list container (a box whose children are DisplayListItem boxes) as a
// <ul> or <ol> element (chosen from the first item's marker), each item a <li>, with a
// nested list emitted inside its parent <li>.
func (w *writer) list(container *cssbox.Box, depth int) {
	tag := "ul"
	for _, item := range container.Children {
		if item.Display == cssbox.DisplayListItem {
			if boxwalk.IsOrderedMarker(markerText(item)) {
				tag = "ol"
			}
			break
		}
	}
	var body strings.Builder
	inner := &writer{opts: w.opts}
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		inner.item(item, depth+1)
	}
	body.WriteString(inner.sb.String())
	if body.Len() == 0 {
		return
	}
	w.line(depth, "<"+tag+">")
	w.sb.WriteString(strings.TrimRight(body.String(), "\n"))
	w.sb.WriteByte('\n')
	w.line(depth, "</"+tag+">")
}

// item renders one list item as a <li>. Its own inline content (with the prepended marker
// run and any nested lists stripped) forms the item text; a nested list is rendered inside
// the same <li> at a deeper indent. A GFM-style leading checkbox becomes an
// <input type="checkbox"> control.
func (w *writer) item(item *cssbox.Box, depth int) {
	inner := &writer{opts: w.opts}
	text := strings.TrimSpace(inner.inline(boxwalk.WithoutNestedLists(item)))
	text = boxwalk.StripMarkerPrefix(text, item.Marker)
	if checked, ok := boxwalk.LeadingCheckbox(item); ok {
		box := `<input type="checkbox">`
		if checked {
			box = `<input type="checkbox" checked>`
		}
		text = strings.TrimSpace(box + " " + text)
	}

	// Collect nested lists to render inside this <li>.
	var nested []*cssbox.Box
	for _, c := range item.Children {
		if boxwalk.IsListContainer(c) {
			nested = append(nested, c)
		}
	}
	if len(nested) == 0 {
		w.line(depth, "<li>"+text+"</li>")
		return
	}
	w.line(depth, "<li>"+text)
	for _, n := range nested {
		w.list(n, depth+1)
	}
	w.line(depth, "</li>")
}

// markerText returns an item's resolved marker text, trimmed, or "" when absent.
func markerText(b *cssbox.Box) string {
	if b.Marker == nil {
		return ""
	}
	return strings.TrimSpace(b.Marker.Text)
}
