package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupFlexGrid walks the box tree and repairs every flex and grid container's
// children into proper items (CSS Flexbox §4 / Grid §6): contiguous runs of
// inline-level content become one anonymous block-level item; whitespace-only
// text between block-level items is dropped. Block-level children pass through
// unchanged. Called from Build after fixupTables.
func fixupFlexGrid(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupFlexGrid(c)
	}
	switch b.Display {
	case cssbox.DisplayFlex, cssbox.DisplayInlineFlex:
		b.Children = containerItems(b.Children, cssbox.BoxAnonFlexItem)
	case cssbox.DisplayGrid, cssbox.DisplayInlineGrid:
		b.Children = containerItems(b.Children, cssbox.BoxAnonGridItem)
	}
}

// containerItems converts a flex/grid container's raw children into items. Every
// element child becomes its own item — an inline-level element is blockified
// (CSS Flexbox §4 / Grid §6: "the display value of a flex item is blockified");
// only a maximal run of child TEXT (and anonymous inline splits) is wrapped in
// one anonymous item. Whitespace-only text outside a text run is discarded.
func containerItems(kids []*cssbox.Box, anonKind cssbox.BoxKind) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		item := &cssbox.Box{
			Kind:    anonKind,
			Display: cssbox.DisplayBlock,
			// The run holds only text-level content, so the anon item establishes an
			// INLINE formatting context; with BlockFC its text would never lay out
			// (zero size).
			Formatting: cssbox.InlineFC,
			Children:   run,
		}
		out = append(out, item)
		run = nil
	}
	for _, c := range kids {
		switch {
		case isWSText(c) && len(run) == 0:
			// Whitespace-only text BETWEEN items (no text run open) collapses away.
			// Whitespace that arrives while a run IS open falls through to the default
			// arm and stays in the run (correct per CSS Flexbox §4 / Grid §6 — unlike
			// tablefix.go, which has no inline-run concept and drops whitespace
			// outright).
		case c.Kind.IsBlockLevel() || c.Kind == cssbox.BoxReplaced:
			// A block-level box, OR an atomic replaced box (an <img>/form control): each
			// becomes its OWN flex/grid item (an atomic inline is an item, not coalesced
			// into a text run). A replaced box keeps its Kind so replaced sizing applies;
			// an inline-block is already block-level by Kind.
			flush()
			out = append(out, c)
		case c.Kind == cssbox.BoxInline:
			// An inline ELEMENT child (span/label/a/...) is its own item, blockified.
			// Its content is inline-level (block-in-inline splitting already ran), so
			// the blockified box holds an inline formatting context.
			flush()
			c.Kind, c.Display, c.Formatting = cssbox.BoxBlock, cssbox.DisplayBlock, cssbox.InlineFC
			out = append(out, c)
		default:
			// Text (or an anonymous inline split): part of the current text run.
			run = append(run, c)
		}
	}
	flush()
	return out
}
