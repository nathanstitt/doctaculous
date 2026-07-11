package docxwrite

import (
	"context"
	"fmt"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// bulletNumID is the shared numbering instance for every bullet list (bullet
// markers carry no counter state, so one instance serves all). Ordered lists
// each allocate their own instance so numbering restarts per list — the
// reader's counters are per-numId, and Word restarts per instance.
const bulletNumID = 1

// list emits a list container's items as numbered paragraphs (w:numPr), depth
// being the w:ilvl. Nested lists follow their parent item's paragraph at
// depth+1, sharing the same numbering instance (per-level counters keep them
// independent).
func (w *writer) list(ctx context.Context, container *cssbox.Box, depth int) error {
	numID := bulletNumID
	if containerIsOrdered(container) {
		w.orderedLists++
		numID = bulletNumID + w.orderedLists
	}
	return w.listItems(ctx, container, depth, numID)
}

// listItems emits the container's items with the given numbering instance.
func (w *writer) listItems(ctx context.Context, container *cssbox.Box, depth, numID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, item := range container.Children {
		if item.Display != cssbox.DisplayListItem {
			continue
		}
		runs := w.inlineRuns(boxwalk.WithoutNestedLists(item))
		runs = stripMarkerRuns(runs, item.Marker)
		// A task-list item: the checkbox control has no DOCX equivalent, so a
		// ballot glyph stands in (one-way; the glyph survives as text).
		if checked, ok := boxwalk.LeadingCheckbox(item); ok {
			box := "☐"
			if checked {
				box = "☒"
			}
			prefix := boxwalk.StyledRun{Text: box + " "}
			runs = append([]boxwalk.StyledRun{prefix}, runs...)
		}
		numPr := fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, depth, numID)
		w.emitParagraph(runs, "ListParagraph", numPr, "")

		// Nested lists render beneath their parent item, one level deeper. An
		// ordered sublist keeps its own restart-at-1 semantics via per-level
		// counters within the same instance, matching the reader's model.
		for _, c := range item.Children {
			if boxwalk.IsListContainer(c) {
				sub := numID
				if containerIsOrdered(c) != (numID != bulletNumID) {
					// The sublist's kind differs from its parent's instance
					// (bullets under an ordered list or vice versa); pick the
					// matching instance.
					if containerIsOrdered(c) {
						w.orderedLists++
						sub = bulletNumID + w.orderedLists
					} else {
						sub = bulletNumID
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
// Markdown writer uses).
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

// stripMarkerRuns removes the item's resolved marker text from the front of its
// runs (box generation prepends the marker as a leading inline run; DOCX
// numbering re-synthesizes it, so keeping it would double the marker).
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
