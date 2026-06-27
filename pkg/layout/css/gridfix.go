package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupGrid walks the box tree and repairs every grid container's children into
// proper grid items (CSS Grid §6): contiguous runs of inline-level content /
// text are gathered into an anonymous block-level grid item, and whitespace-only
// text between block-level items is dropped. Block-level children pass through
// unchanged. Called from Build after fixupFlex.
func fixupGrid(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupGrid(c)
	}
	if b.Display == cssbox.DisplayGrid || b.Display == cssbox.DisplayInlineGrid {
		b.Children = gridItems(b.Children)
	}
}

// gridItems converts a grid container's raw children into grid items. A maximal
// run of inline-level boxes becomes one anonymous grid item; a block-level box
// is kept as its own item; whitespace-only text outside an inline run is
// discarded.
func gridItems(kids []*cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		item := &cssbox.Box{
			Kind:       cssbox.BoxAnonGridItem,
			Display:    cssbox.DisplayBlock,
			Formatting: cssbox.BlockFC,
			Children:   run,
		}
		out = append(out, item)
		run = nil
	}
	for _, c := range kids {
		switch {
		case isWSText(c) && len(run) == 0:
			// Whitespace-only text BETWEEN block items (no inline run open) collapses
			// away. Whitespace that arrives while an inline run IS open falls through to
			// the default arm and stays in the run (correct per CSS Grid §6 — mirrors
			// flexfix.go behavior).
		case c.Kind.IsBlockLevel():
			flush()
			out = append(out, c)
		default:
			// Inline-level box or non-whitespace text: part of the current inline run.
			run = append(run, c)
		}
	}
	flush()
	return out
}
