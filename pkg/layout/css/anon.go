package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// normalize rewrites the tree so every box satisfies the layout invariant: a
// block container's children are either all block-level or all inline-level.
// It runs three passes per box, bottom-up:
//  1. block-in-inline: split an inline box that contains a block-level
//     descendant so the block breaks out to block level.
//  2. whitespace: collapse internal whitespace in text runs and drop runs that
//     are entirely whitespace adjacent to block boundaries.
//  3. inline-in-block: wrap maximal runs of inline-level children of a block
//     container (that also has block-level children) in anonymous block boxes.
func normalize(b *cssbox.Box) {
	// Recurse first (bottom-up) so children are already normalized.
	for _, c := range b.Children {
		normalize(c)
	}
	b.Children = splitBlockInInline(b.Children)
	b.Children = handleWhitespace(b.Children, b)
	if b.Kind.IsBlockLevel() {
		b.Children = wrapInlineRuns(b.Children)
	}
}

// splitBlockInInline lifts block-level boxes out of inline boxes. For each inline
// child that contains block-level descendants, it is replaced by a sequence: the
// inline pieces before the block (as an anonymous inline box), the block itself
// (promoted to this level), then the inline pieces after, etc. Inline children
// with no block descendant are left unchanged.
func splitBlockInInline(children []*cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	for _, c := range children {
		if c.Kind == cssbox.BoxInline && containsBlockLevel(c) {
			out = append(out, splitOneInline(c)...)
			continue
		}
		out = append(out, c)
	}
	return out
}

// containsBlockLevel reports whether any direct child of b is block-level.
func containsBlockLevel(b *cssbox.Box) bool {
	for _, c := range b.Children {
		if c.Kind.IsBlockLevel() {
			return true
		}
	}
	return false
}

// splitOneInline splits a single inline box around its block-level children,
// producing a flat slice of block-level boxes and anonymous-inline boxes that
// carry the inline fragments. The inline's own style is copied onto each
// anonymous-inline fragment so styling is preserved.
//
// The style is copied by value (css.ComputedStyle is a value type), so the
// fragments do not alias the source inline's style. If ComputedStyle ever gains
// a mutable reference field (map/slice/pointer), this would need a deep copy to
// avoid the fragments sharing it.
func splitOneInline(inline *cssbox.Box) []*cssbox.Box {
	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, &cssbox.Box{
			Kind:       cssbox.BoxAnonInline,
			Style:      inline.Style,
			Display:    cssbox.DisplayInline,
			Formatting: cssbox.InlineFC,
			Children:   run,
		})
		run = nil
	}
	for _, c := range inline.Children {
		if c.Kind.IsBlockLevel() {
			flush()
			out = append(out, c) // promote the block to this level
			continue
		}
		run = append(run, c)
	}
	flush()
	return out
}

// wrapInlineRuns wraps maximal runs of inline-level children in anonymous block
// boxes, but only when the container also has at least one block-level child. If
// all children are inline-level, they are left as-is (the block establishes an
// inline formatting context directly).
func wrapInlineRuns(children []*cssbox.Box) []*cssbox.Box {
	hasBlock := false
	for _, c := range children {
		if c.Kind.IsBlockLevel() {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		return children // all inline: no anonymous blocks needed
	}

	var out []*cssbox.Box
	var run []*cssbox.Box
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, &cssbox.Box{
			Kind:       cssbox.BoxAnonBlock,
			Display:    cssbox.DisplayBlock,
			Formatting: cssbox.InlineFC, // an anon block holds inline content
			Children:   run,
		})
		run = nil
	}
	for _, c := range children {
		if c.Kind.IsBlockLevel() {
			flush()
			out = append(out, c)
			continue
		}
		run = append(run, c)
	}
	flush()
	return out
}

// handleWhitespace collapses internal whitespace in text runs and drops text
// boxes that are entirely collapsible whitespace when they sit adjacent to a
// block boundary (so inter-block whitespace does not create spurious anonymous
// blocks). Non-whitespace text has its internal whitespace runs collapsed to a
// single space.
func handleWhitespace(children []*cssbox.Box, parent *cssbox.Box) []*cssbox.Box {
	// First collapse internal whitespace in every text box.
	for _, c := range children {
		if c.Kind == cssbox.BoxText {
			c.Text = collapseWS(c.Text)
		}
	}
	// Then drop whitespace-only text boxes adjacent to block-level siblings (or at
	// the edges of a block container).
	parentIsBlockContainer := parent.Kind.IsBlockLevel()
	var out []*cssbox.Box
	for i, c := range children {
		if c.Kind == cssbox.BoxText && isAllWS(c.Text) {
			if parentIsBlockContainer && adjacentToBlock(children, i) {
				continue // drop inter-block whitespace
			}
		}
		out = append(out, c)
	}
	return out
}

// adjacentToBlock reports whether the child at index i has a block-level neighbor
// or is at an edge of the slice (treating container edges as block boundaries
// when the container is a block container).
func adjacentToBlock(children []*cssbox.Box, i int) bool {
	if i == 0 || i == len(children)-1 {
		return true
	}
	prevBlock := children[i-1].Kind.IsBlockLevel()
	nextBlock := children[i+1].Kind.IsBlockLevel()
	return prevBlock || nextBlock
}

// collapseWS collapses runs of ASCII whitespace to a single space.
func collapseWS(s string) string {
	var b strings.Builder
	inWS := false
	for _, r := range s {
		if isWSRune(r) {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
			continue
		}
		b.WriteRune(r)
		inWS = false
	}
	return b.String()
}

func isAllWS(s string) bool {
	for _, r := range s {
		if !isWSRune(r) {
			return false
		}
	}
	return true
}

func isWSRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}
