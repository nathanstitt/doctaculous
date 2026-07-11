package css

import (
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
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
	// A replaced box (e.g. <img>) keeps Kind == BoxReplaced regardless of its display
	// (the replacedTags override in generate runs AFTER classifyDisplay), so
	// Kind.IsBlockLevel() is always false for it and a display:block <img> would
	// wrongly flow inline. Its OUTER level is its display: display:block (or any
	// block-level display) makes it block-level-outer so it stacks as a block (the
	// block stacker dispatches BoxReplaced to layoutBlockReplaced); the default/inline
	// and inline-block cases stay inline-level-outer (handled above + the fallthrough).
	if b.Kind == cssbox.BoxReplaced {
		return isBlockLevelOuterDisplay(b.Display)
	}
	return b.Kind.IsBlockLevel()
}

// isBlockLevelOuterDisplay reports whether a display value makes a box participate
// in its parent's formatting context as block-level. Used for a replaced box, whose
// Kind does not encode its outer level. The inline-level displays (inline,
// inline-block, inline-flex, inline-grid) are NOT block-level-outer; every other
// recognized display (block, list-item, the table display roles) is.
func isBlockLevelOuterDisplay(d cssbox.DisplayKind) bool {
	switch d {
	case cssbox.DisplayInline, cssbox.DisplayInlineBlock, cssbox.DisplayInlineFlex, cssbox.DisplayInlineGrid:
		return false
	default:
		return true
	}
}

// isBlockLevelReplaced reports whether b is a replaced box (Kind BoxReplaced) that
// participates as a block-level box in its parent's formatting context (its display
// is block-level-outer, e.g. <img style="display:block">). The block stacker uses
// this as the replaced exception to its Kind.IsBlockLevel() child guard, since a
// replaced box's Kind is inline-level regardless of its display.
func isBlockLevelReplaced(b *cssbox.Box) bool {
	return b.Kind == cssbox.BoxReplaced && isBlockLevelOuterDisplay(b.Display)
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
	// A flex/grid/table container does NOT wrap its inline children into anonymous
	// blocks here: its items are created by the format-specific fixups (fixupFlexGrid /
	// fixupTables), which coalesce inline runs into anonymous FLEX/GRID
	// items and keep block/replaced children as their own items. Running the generic
	// block-level wrap first would coalesce a row's label span AND its adjacent control
	// into one anonymous block, corrupting the item-to-cell mapping. (handleWhitespace
	// + splitBlockInInline above still apply.)
	if b.Kind.IsBlockLevel() && b.Formatting != cssbox.FlexFC &&
		b.Formatting != cssbox.GridFC && b.Formatting != cssbox.TableFC {
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
	// The CSS white-space governing a text run decides whether its whitespace
	// collapses: collapsing modes (normal/nowrap) squash runs (incl. newlines) to one
	// space; pre-line collapses spaces/tabs but keeps newlines; preserving modes
	// (pre/pre-wrap) keep everything (the shaper turns kept newlines into breaks and
	// kept tabs into tab stops). A text box carries its governing element's computed
	// style (makeTextBox copies the parent's), so the box's own value is authoritative
	// — for ordinary text it equals the containing element's, and for a synthesized
	// hard-break leaf (<br> → "\n" with white-space:pre-line) it preserves the newline
	// regardless of the container's mode. The empty/normal default reproduces the
	// prior collapse-everything.
	for _, c := range children {
		if c.Kind != cssbox.BoxText {
			continue
		}
		collapseSpaces, preserveNewlines, _ := gcss.WhiteSpaceFlags(c.Style.WhiteSpace)
		switch {
		case collapseSpaces && !preserveNewlines:
			c.Text = collapseWS(c.Text) // normal / nowrap
		case collapseSpaces && preserveNewlines:
			c.Text = collapseWSKeepNewlines(c.Text) // pre-line
		default:
			// pre / pre-wrap: keep the raw text verbatim.
		}
	}
	// Then drop whitespace-only text boxes adjacent to block-level siblings (or at
	// the edges of a block container). This asks whether the PARENT is itself a block
	// container (interior role), so it uses Kind.IsBlockLevel — an inline-block parent
	// is a block container and strips inter-block whitespace among its own children.
	// The block-BOUNDARY test for the siblings (adjacentToBlock) uses the outer-level
	// predicate instead, so an inline-block sibling is not a boundary.
	// In a preserving mode (pre/pre-wrap) a whitespace-only text box is significant
	// (e.g. a blank line in <pre>) and must NOT be dropped. Only collapse it away in
	// collapsing modes.
	parentIsBlockContainer := parent.Kind.IsBlockLevel()
	var out []*cssbox.Box
	for i, c := range children {
		if c.Kind == cssbox.BoxText && isAllWS(c.Text) {
			collapseSpaces, preserveNewlines, _ := gcss.WhiteSpaceFlags(c.Style.WhiteSpace)
			// A preserved newline (a <br> leaf, or pre-line text containing '\n')
			// is a hard break, never disposable inter-block whitespace.
			disposable := collapseSpaces && !(preserveNewlines && strings.Contains(c.Text, "\n"))
			if disposable && parentIsBlockContainer && adjacentToBlock(children, i) {
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

// collapseWSKeepNewlines collapses runs of spaces/tabs to a single space but PRESERVES
// each newline (white-space: pre-line). A run of horizontal whitespace around a newline
// collapses so the newline stands alone; the shaper turns the kept '\n' into a hard
// break. Consecutive newlines are preserved (blank lines survive in pre-line).
func collapseWSKeepNewlines(s string) string {
	var b strings.Builder
	inSpace := false // currently in a run of collapsible spaces/tabs (not newlines)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
			inSpace = false
		case r == ' ' || r == '\t' || isWSRune(r):
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
		default:
			b.WriteRune(r)
			inSpace = false
		}
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
