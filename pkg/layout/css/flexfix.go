package css

import "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"

// fixupFlex walks the box tree and repairs every flex container's children into
// proper flex items (CSS Flexbox 4): contiguous runs of inline-level content /
// text are gathered into an anonymous block-level flex item, and whitespace-only
// text between block-level items is dropped. Block-level children pass through
// unchanged. Called from Build after fixupTables.
func fixupFlex(b *cssbox.Box) {
	for _, c := range b.Children {
		fixupFlex(c)
	}
	if b.Display == cssbox.DisplayFlex || b.Display == cssbox.DisplayInlineFlex {
		b.Children = flexItems(b.Children)
	}
}

// flexItems converts a flex container's raw children into flex items. A maximal
// run of inline-level boxes becomes one anonymous flex item; a block-level box
// is kept as its own item; whitespace-only text outside an inline run is
// discarded.
func flexItems(kids []*cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		item := &cssbox.Box{
			Kind:       cssbox.BoxAnonFlexItem,
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
			// the default arm and stays in the run (correct per CSS Flexbox §4 — unlike
			// tablefix.go, which has no inline-run concept and drops whitespace outright).
		case c.Kind.IsBlockLevel() || c.Kind == cssbox.BoxReplaced:
			// A block-level box, OR an atomic replaced box (an <img>/form control): each
			// becomes its OWN flex item (CSS Flexbox §4 — an atomic inline is a flex item,
			// not coalesced into an inline run like text). A replaced box keeps its Kind so
			// replaced sizing applies; an inline-block is already block-level by Kind.
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
