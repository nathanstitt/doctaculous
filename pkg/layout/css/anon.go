package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// isBlockLevelOuter reports whether b participates in its PARENT's formatting
// context as a block-level box (its "outer" level). This differs from
// cssbox.BoxKind.IsBlockLevel, which describes a box's structural/interior role:
// an inline-block (Display == DisplayInlineBlock) has Kind BoxBlock — it IS a
// block container for its own children — yet it participates OUTWARDLY as an
// inline-level atomic in its parent's inline formatting context. So an
// inline-block is NOT block-level-outer despite its BoxBlock kind. Every other
// box's outer level matches its kind.
//
// The same applies to inline-flex (Display == DisplayInlineFlex): it too has Kind
// BoxBlock (a flex container for its own children) but participates outwardly as an
// inline-level atom in its parent's IFC, so it is not block-level-outer either.
//
// Box generation uses this (not Kind.IsBlockLevel) wherever it classifies a box
// AS A CHILD for its parent's FC partitioning: which children stack as blocks vs
// flow inline, whether an inline box must split around a block descendant,
// whether a sibling is a block boundary for whitespace stripping. The Kind
// predicate is reserved for a box's OWN interior role (see normalize and
// handleWhitespace, which ask whether a box is itself a block CONTAINER).
func isBlockLevelOuter(b *cssbox.Box) bool {
	if b.Display == cssbox.DisplayInlineBlock || b.Display == cssbox.DisplayInlineFlex || b.Display == cssbox.DisplayInlineGrid {
		return false
	}
	return b.Kind.IsBlockLevel()
}

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
	// Kind.IsBlockLevel (interior role), NOT isBlockLevelOuter: these passes operate
	// on a box that is itself a block CONTAINER. An inline-block is a block container
	// for its own children (Kind BoxBlock), so its interior gets the same wrap +
	// reconcile even though it is inline-level OUTERLY (its parent's FC partitioning,
	// handled by the per-child outer-level checks, is a separate question).
	if b.Kind.IsBlockLevel() {
		b.Children = wrapInlineRuns(b.Children)
		reconcileFormatting(b)
	}
}

// reconcileFormatting sets a block-level box's formatting context from its final
// child composition: a block container whose children are all inline-level
// establishes an inline formatting context; otherwise a block formatting context.
// classifyDisplay (in build.go) seeds Formatting from the display keyword alone,
// before children are known — this corrects it post-normalization so a real <p>
// with inline content and an anonymous block holding inline runs agree (both
// InlineFC). Boxes establishing a non-flow context (table/flex/grid) keep their
// keyword-derived Formatting and are left untouched.
func reconcileFormatting(b *cssbox.Box) {
	if b.Formatting != cssbox.BlockFC && b.Formatting != cssbox.InlineFC {
		return // table/flex/grid: keep the keyword-derived context
	}
	if len(b.Children) == 0 {
		b.Formatting = cssbox.BlockFC // empty container: a (degenerate) block context
		return
	}
	// A child's OUTER level decides the context: a container whose only "block"
	// children are inline-blocks (block-level-INTERIOR but inline-level-OUTER)
	// establishes an inline FC, so the inline-blocks flow inline as atomics.
	for _, c := range b.Children {
		if isBlockLevelOuter(c) {
			b.Formatting = cssbox.BlockFC
			return
		}
	}
	b.Formatting = cssbox.InlineFC
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

// containsBlockLevel reports whether any direct child of b is block-level-OUTER,
// i.e. a box that must break an enclosing inline. An inline-block child is NOT
// counted: it is inline-level outer, so it stays in the inline flow and does not
// trigger a block-in-inline split.
func containsBlockLevel(b *cssbox.Box) bool {
	for _, c := range b.Children {
		if isBlockLevelOuter(c) {
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
		// Only a block-level-OUTER child breaks the inline out to this level; an
		// inline-block stays inline-level and is kept in the surrounding run.
		if isBlockLevelOuter(c) {
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
	// "Block" here means block-level-OUTER: an inline-block is grouped INTO the
	// inline runs (wrapped alongside text/inlines when a real block sibling forces
	// the wrap), never treated as a block that separates runs.
	hasBlock := false
	for _, c := range children {
		if isBlockLevelOuter(c) {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		return children // all inline-level: no anonymous blocks needed
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
		if isBlockLevelOuter(c) {
			flush()
			out = append(out, c)
			continue
		}
		run = append(run, c) // inline-level (incl. inline-block) joins the run
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
	// the edges of a block container). This asks whether the PARENT is itself a block
	// container (interior role), so it uses Kind.IsBlockLevel — an inline-block parent
	// is a block container and strips inter-block whitespace among its own children.
	// The block-BOUNDARY test for the siblings (adjacentToBlock) uses the outer-level
	// predicate instead, so an inline-block sibling is not a boundary.
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

// adjacentToBlock reports whether the child at index i has a block-level-OUTER
// neighbor or is at an edge of the slice (treating container edges as block
// boundaries when the container is a block container). Outer level is what matters
// for whitespace: an inline-block neighbor is inline-level, so whitespace beside it
// is significant (not inter-block whitespace) and must NOT be stripped — only a
// genuine block-level-outer neighbor forms the boundary that drops the whitespace.
func adjacentToBlock(children []*cssbox.Box, i int) bool {
	if i == 0 || i == len(children)-1 {
		return true
	}
	prevBlock := isBlockLevelOuter(children[i-1])
	nextBlock := isBlockLevelOuter(children[i+1])
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
