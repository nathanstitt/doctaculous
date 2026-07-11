package docxwrite

import (
	"context"
	"fmt"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/docx"
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
func (w *writer) list(ctx context.Context, container *cssbox.Box, depth int, out *[]docx.Block) error {
	numID := bulletNumID
	if containerIsOrdered(container) {
		w.orderedLists++
		numID = bulletNumID + w.orderedLists
	}
	return w.listItems(ctx, container, depth, numID, out)
}

// listItems emits the container's items with the given numbering instance.
func (w *writer) listItems(ctx context.Context, container *cssbox.Box, depth, numID int, out *[]docx.Block) error {
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
		if p := w.buildParagraph(runs, "ListParagraph", &numPr{numID: numID, ilvl: depth}, docx.JustifyLeft, false); p != nil {
			*out = append(*out, docx.Block{Paragraph: p})
		}

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
				if err := w.listItems(ctx, c, depth+1, sub, out); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// buildNumbering constructs the word/numbering.xml model: one bullet abstract
// definition (glyph rotating by depth) shared by every bullet list, one
// decimal abstract definition, and one w:num instance per ordered list
// encountered — numbering restarts per instance both in the reader (per-numId
// counters) and in Word (startOverride, which the reader ignores but Word
// requires). Per-level indents give Word the familiar nested-list geometry.
func buildNumbering(orderedLists int) *docx.Numbering {
	num := docx.NewNumbering()
	// Abstract 0: bullets. The glyph rotates •/◦/▪ by depth, matching the UA
	// list rendering. Abstract 1: decimal; the %N placeholder is per-level, as
	// the reader's single-placeholder substitution expects.
	bulletGlyphs := []string{"•", "◦", "▪"}
	bullet, decimal := map[int]docx.NumLevel{}, map[int]docx.NumLevel{}
	for lvl := 0; lvl < 9; lvl++ {
		ind := docx.Twips(720 * (lvl + 1))
		bullet[lvl] = docx.NumLevel{
			Format: docx.NumFmtBullet, Text: bulletGlyphs[lvl%len(bulletGlyphs)],
			IndentLeft: ind, HasIndentLeft: true, Hanging: 360, HasHanging: true,
		}
		decimal[lvl] = docx.NumLevel{
			Format: docx.NumFmtDecimal, Text: fmt.Sprintf("%%%d.", lvl+1),
			Start: 1, HasStart: true,
			IndentLeft: ind, HasIndentLeft: true, Hanging: 360, HasHanging: true,
		}
	}
	num.Abstract[0] = bullet
	num.Abstract[1] = decimal

	// Instance 1 = bullets; instances 2..N+1 = one per ordered list.
	num.Instances[bulletNumID] = docx.NumInstance{AbstractID: 0}
	for i := 1; i <= orderedLists; i++ {
		num.Instances[bulletNumID+i] = docx.NumInstance{
			AbstractID: 1,
			Overrides:  map[int]docx.LevelOverride{0: {Start: 1, HasStart: true}},
		}
	}
	return num
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
