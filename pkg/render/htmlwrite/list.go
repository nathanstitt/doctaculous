package htmlwrite

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// list renders a list container (a box whose children are DisplayListItem boxes) as a
// <ul> or <ol> element (chosen from the first item's marker), each item a <li>, with a
// nested list emitted inside its parent <li>. Mirrors markdown/list.go.
func (w *writer) list(container *cssbox.Box, depth int) {
	tag := "ul"
	for _, item := range container.Children {
		if item.Display == cssbox.DisplayListItem {
			if isOrderedMarker(markerText(item)) {
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
	text := strings.TrimSpace(inner.inline(withoutNestedLists(item)))
	text = stripMarkerPrefix(text, item.Marker)
	if checked, ok := leadingCheckbox(item); ok {
		box := `<input type="checkbox">`
		if checked {
			box = `<input type="checkbox" checked>`
		}
		text = strings.TrimSpace(box + " " + text)
	}

	// Collect nested lists to render inside this <li>.
	var nested []*cssbox.Box
	for _, c := range item.Children {
		if isListContainer(c) {
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

// isOrderedMarker reports whether a marker string looks ordered (digits/roman/alpha
// followed by "." or ")"), as opposed to a bullet glyph. Mirrors markdown/list.go.
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
// containers, so inline() renders just the item's own inline content. The leading marker
// run that box generation prepends is stripped afterwards by stripMarkerPrefix. Mirrors
// markdown/list.go.
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
// Box generation prepends the marker as the item's first inline run; inline() collapses
// whitespace, so the rendered text begins with the marker's non-space glyphs. Mirrors
// markdown/list.go.
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
// (an <input type=checkbox>), and whether it is checked. Mirrors markdown/list.go.
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

// isListContainer reports whether a box is a list container (a block whose direct children
// include a list item) — used to detect a nested list inside an <li> and to dispatch a
// top-level list. Mirrors markdown/list.go.
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
