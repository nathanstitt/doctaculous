package rtfwrite

import (
	"context"
	"fmt"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// bulletListID is the shared \ls instance for every bullet list (bullet
// markers carry no counter state, so one instance serves all). Ordered lists
// each allocate their own so adjacent ordered lists stay separate through the
// reader.
const bulletListID = 1

// list emits a list container's items as \ls/\ilvl paragraphs with a literal
// \pntext marker (the marker text is what tells the reader — and any RTF
// viewer without list-table support — the item's kind). Nested lists follow
// their parent item's paragraph one level deeper.
func (w *writer) list(ctx context.Context, container *cssbox.Box, depth int) error {
	listID := bulletListID
	if containerIsOrdered(container) {
		w.orderedLists++
		listID = bulletListID + w.orderedLists
	}
	return w.listItems(ctx, container, depth, listID)
}

// listItems emits the container's items with the given list instance.
func (w *writer) listItems(ctx context.Context, container *cssbox.Box, depth, listID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		runs := w.inlineRuns(boxwalk.WithoutNestedLists(item))
		runs = stripMarkerRuns(runs, item.Marker)
		// A task-list item: the checkbox control has no RTF equivalent, so a
		// ballot glyph stands in (one-way; the glyph survives as text).
		if checked, ok := boxwalk.LeadingCheckbox(item); ok {
			box := "☐"
			if checked {
				box = "☒"
			}
			prefix := boxwalk.StyledRun{Text: box + " "}
			runs = append([]boxwalk.StyledRun{prefix}, runs...)
		}

		marker := `\bullet`
		if listID != bulletListID {
			m := "1."
			if item.Marker != nil {
				if t := strings.TrimSpace(item.Marker.Text); t != "" {
					m = t
				}
			}
			marker = escapeRTF(m)
		}
		indent := 720 * (depth + 1)
		fmt.Fprintf(&w.body, `{\pard{\pntext %s\tab}\ilvl%d\ls%d\li%d\fi-360 `,
			marker, depth, listID, indent)
		w.writeRuns(runs)
		w.body.WriteString(`\par}` + "\n")

		// Nested lists render beneath their parent item, one level deeper. A
		// sublist whose kind differs from its parent's instance gets a matching
		// instance of its own.
		for _, c := range item.Children {
			if boxwalk.IsListContainer(c) {
				sub := listID
				if containerIsOrdered(c) != (listID != bulletListID) {
					if containerIsOrdered(c) {
						w.orderedLists++
						sub = bulletListID + w.orderedLists
					} else {
						sub = bulletListID
					}
				}
				if err := w.listItems(ctx, c, depth+1, sub); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// containerIsOrdered reports whether a list container holds ordered items,
// judged from the first item's resolved marker text (the same signal the
// Markdown and DOCX writers use).
func containerIsOrdered(container *cssbox.Box) bool {
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		if item.Marker != nil {
			return boxwalk.IsOrderedMarker(strings.TrimSpace(item.Marker.Text))
		}
		return false
	}
	return false
}

// stripMarkerRuns removes the item's resolved marker text from the front of
// its runs (box generation prepends the marker as a leading inline run; the
// \pntext marker re-synthesizes it, so keeping it would double the marker).
func stripMarkerRuns(runs []boxwalk.StyledRun, marker *cssbox.MarkerContent) []boxwalk.StyledRun {
	if marker == nil || len(runs) == 0 {
		return runs
	}
	m := strings.TrimSpace(marker.Text)
	if m == "" {
		return runs
	}
	first := strings.TrimLeft(runs[0].Text, " ")
	if !strings.HasPrefix(first, m) {
		return runs
	}
	runs[0].Text = strings.TrimLeft(strings.TrimPrefix(first, m), " ")
	if runs[0].Text == "" {
		return runs[1:]
	}
	return runs
}
