package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestInlineInBlockWrapsRuns(t *testing.T) {
	// A block div with mixed children: text, a block child, more text. The two
	// inline runs must each be wrapped in an anonymous block.
	root := build(t, `<html><body><div>before<p>para</p>after</div></body></html>`, nil)
	div := root.Children[0].Children[0] // html > body > div
	// div children should be: [AnonBlock(before), Block(p), AnonBlock(after)]
	if len(div.Children) != 3 {
		t.Fatalf("div has %d children, want 3 (anon, p, anon): %s", len(div.Children), dump(div))
	}
	if div.Children[0].Kind != cssbox.BoxAnonBlock {
		t.Errorf("child 0 = %v, want BoxAnonBlock", div.Children[0].Kind)
	}
	if div.Children[1].Kind != cssbox.BoxBlock {
		t.Errorf("child 1 = %v, want BoxBlock (the p)", div.Children[1].Kind)
	}
	if div.Children[2].Kind != cssbox.BoxAnonBlock {
		t.Errorf("child 2 = %v, want BoxAnonBlock", div.Children[2].Kind)
	}
	// the anon block wraps the text run
	if len(div.Children[0].Children) != 1 || div.Children[0].Children[0].Text != "before" {
		t.Errorf("anon block 0 should wrap text 'before': %s", dump(div.Children[0]))
	}
}

func TestAllInlineChildrenNotWrapped(t *testing.T) {
	// A block with only inline children needs no anonymous blocks.
	root := build(t, `<html><body><p>just <span>inline</span> text</p></body></html>`, nil)
	p := root.Children[0].Children[0] // html>body>p
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("all-inline block should not get anonymous blocks: %s", dump(p))
		}
	}
}

func TestBlockInInlineSplitsInline(t *testing.T) {
	// An inline span containing a block div: the span splits around the block.
	root := build(t, `<html><body><div><span>a<div>B</div>c</span></div></body></html>`, nil)
	outer := root.Children[0].Children[0] // html>body>div(outer)
	// After block-in-inline, outer's children should contain a block (from the
	// split-out inner div) flanked by anonymous boxes carrying the inline pieces.
	var sawBlock bool
	for _, c := range outer.Children {
		if c.Kind == cssbox.BoxBlock && len(c.Children) > 0 && c.Children[0].Text == "B" {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Errorf("block inside inline should break out to a block-level box: %s", dump(outer))
	}
	// The outer block's children must satisfy the all-block-or-all-inline
	// invariant: since a block broke out, every child must be block-level.
	for _, c := range outer.Children {
		if !c.Kind.IsBlockLevel() {
			t.Errorf("after block-in-inline split, all children must be block-level: %s", dump(outer))
		}
	}
}

func TestBlockInInlinePreservesInlineStyle(t *testing.T) {
	// The split-out anonymous-inline fragments must carry the original inline's
	// style (here, the span's red color) so styling survives the split. After the
	// inline-in-block wrap pass the anon-inline fragments are nested inside
	// anon-blocks, so search the whole subtree rather than just direct children.
	root := build(t, `<html><head><style>span{color:red}</style></head><body><div><span>a<div>B</div>c</span></div></body></html>`, nil)
	outer := root.Children[0].Children[0] // html>body>div(outer)
	anon := firstByKind(outer, cssbox.BoxAnonInline)
	if anon == nil {
		t.Fatalf("expected an anonymous-inline fragment from the split: %s", dump(outer))
	}
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	if anon.Style.Color != red {
		t.Errorf("anon-inline color = %v, want span's red (style must be preserved across the split)", anon.Style.Color)
	}
}

func TestWhitespaceBetweenBlocksDropped(t *testing.T) {
	// Whitespace-only text between block children must not create anon blocks.
	root := build(t, "<html><body><div><p>a</p>\n   <p>b</p></div></body></html>", nil)
	div := root.Children[0].Children[0]
	if len(div.Children) != 2 {
		t.Errorf("div should have 2 block children (no anon from inter-block whitespace), got %d: %s", len(div.Children), dump(div))
	}
	for _, c := range div.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("inter-block whitespace should be dropped, not wrapped: %s", dump(div))
		}
	}
}

func TestInternalWhitespaceCollapsed(t *testing.T) {
	root := build(t, "<html><body><p>a    b\t\nc</p></body></html>", nil)
	p := root.Children[0].Children[0]
	var text string
	for _, c := range p.Children {
		if c.Kind == cssbox.BoxText {
			text += c.Text
		}
	}
	if text != "a b c" {
		t.Errorf("collapsed text = %q, want %q", text, "a b c")
	}
}

func TestFormattingReconciledToChildren(t *testing.T) {
	// A block whose children are all inline-level must report InlineFC, matching
	// the FC an anonymous block holding inline runs reports — classifyDisplay
	// seeds BlockFC from the keyword, and normalize must correct it.
	root := build(t, `<html><body><p>hello <span>world</span></p><div><p>a</p><p>b</p></div></body></html>`, nil)
	body := root.Children[0]
	p := body.Children[0]   // all-inline content
	div := body.Children[1] // all-block content (two p children)
	if p.Formatting != cssbox.InlineFC {
		t.Errorf("p (all-inline children) Formatting = %d, want InlineFC", p.Formatting)
	}
	if div.Formatting != cssbox.BlockFC {
		t.Errorf("div (all-block children) Formatting = %d, want BlockFC", div.Formatting)
	}
	// The anon-block wrapper around inline runs in a mixed div also reports InlineFC.
	mixed := build(t, `<html><body><div>txt<p>x</p></div></body></html>`, nil)
	mdiv := mixed.Children[0].Children[0]
	anon := firstByKind(mdiv, cssbox.BoxAnonBlock)
	if anon == nil || anon.Formatting != cssbox.InlineFC {
		t.Errorf("anon block should report InlineFC, got %+v", anon)
	}
}

func TestRootDisplayNoneDegrades(t *testing.T) {
	// If the root computes to display:none, Build must degrade to an empty block
	// root rather than panicking inside normalize(nil).
	root := build(t, `<html style="display:none"><body><p>x</p></body></html>`, nil)
	if root == nil {
		t.Fatal("Build returned nil root for display:none root")
	}
	if root.Kind != cssbox.BoxBlock {
		t.Errorf("degraded root kind = %v, want BoxBlock", root.Kind)
	}
	if len(root.Children) != 0 {
		t.Errorf("display:none root should have no children, got %d", len(root.Children))
	}
}

// --- inline-block participates inline (outer-level) ---

// TestInlineBlockFlowsInline is the core fix: a container of only inline-blocks
// establishes an INLINE formatting context (the inline-blocks flow inline as
// atomics), not a block one — even though each inline-block's Kind is BoxBlock.
func TestInlineBlockFlowsInline(t *testing.T) {
	root := build(t, `<html><body><div><span style="display:inline-block">a</span><span style="display:inline-block">b</span></div></body></html>`, nil)
	div := root.Children[0].Children[0] // html>body>div
	if div.Formatting != cssbox.InlineFC {
		t.Errorf("div with only inline-blocks Formatting = %d, want InlineFC: %s", div.Formatting, dump(div))
	}
	// The two inline-block boxes are the direct children, unwrapped (no anon block).
	if len(div.Children) != 2 {
		t.Fatalf("div children = %d, want 2 inline-blocks (unwrapped): %s", len(div.Children), dump(div))
	}
	for i, c := range div.Children {
		if c.Kind != cssbox.BoxBlock || c.Display != cssbox.DisplayInlineBlock {
			t.Errorf("child %d = (kind %v, display %v), want (BoxBlock, DisplayInlineBlock): %s", i, c.Kind, c.Display, dump(div))
		}
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("inline-block should not be wrapped in an anon block: %s", dump(div))
		}
	}
}

// TestInlineBlockMixedWithTextStaysInline: an inline-block among text is inline-
// level outer, so the div stays InlineFC and nothing is wrapped (no real block
// sibling forces an anonymous block).
func TestInlineBlockMixedWithTextStaysInline(t *testing.T) {
	root := build(t, `<html><body><div>text <span style="display:inline-block">x</span> more</div></body></html>`, nil)
	div := root.Children[0].Children[0]
	if div.Formatting != cssbox.InlineFC {
		t.Errorf("div Formatting = %d, want InlineFC: %s", div.Formatting, dump(div))
	}
	for _, c := range div.Children {
		if c.Kind == cssbox.BoxAnonBlock {
			t.Errorf("no anon block expected (no real block sibling): %s", dump(div))
		}
	}
	// The inline-block survives as an inline-level child alongside the text runs.
	ib := firstByDisplay(div, cssbox.DisplayInlineBlock)
	if ib == nil || ib.Kind != cssbox.BoxBlock {
		t.Errorf("expected an inline-block child (BoxBlock/DisplayInlineBlock): %s", dump(div))
	}
}

// TestInlineBlockAlongsideRealBlockWrapped: with a real block sibling (<p>), the
// all-block-or-all-inline invariant forces the inline-level run — which now
// INCLUDES the inline-block — into an anonymous block, and the div is BlockFC.
func TestInlineBlockAlongsideRealBlockWrapped(t *testing.T) {
	root := build(t, `<html><body><div><span style="display:inline-block">x</span><p>block</p></div></body></html>`, nil)
	div := root.Children[0].Children[0]
	if div.Formatting != cssbox.BlockFC {
		t.Errorf("div with a real block sibling Formatting = %d, want BlockFC: %s", div.Formatting, dump(div))
	}
	// Children: [AnonBlock(inline-block), Block(p)].
	if len(div.Children) != 2 {
		t.Fatalf("div children = %d, want 2 (anon-block, p): %s", len(div.Children), dump(div))
	}
	if div.Children[0].Kind != cssbox.BoxAnonBlock {
		t.Errorf("child 0 = %v, want BoxAnonBlock wrapping the inline-block: %s", div.Children[0].Kind, dump(div))
	}
	if div.Children[1].Kind != cssbox.BoxBlock || div.Children[1].Display != cssbox.DisplayBlock {
		t.Errorf("child 1 = (%v,%v), want the block p: %s", div.Children[1].Kind, div.Children[1].Display, dump(div))
	}
	// The anon block holds the inline-block as its inline content.
	anon := div.Children[0]
	if len(anon.Children) != 1 || anon.Children[0].Display != cssbox.DisplayInlineBlock {
		t.Errorf("anon block should wrap the inline-block: %s", dump(anon))
	}
}

// TestInlineBlockInInlineDoesNotSplit: an inline-block nested in an inline element
// is inline-level outer, so it does NOT cause a block-in-inline split — the outer
// span stays a single inline box with the inline-block among its children.
func TestInlineBlockInInlineDoesNotSplit(t *testing.T) {
	root := build(t, `<html><body><span>before<span style="display:inline-block">x</span>after</span></body></html>`, nil)
	body := root.Children[0]
	// The outer span is the body's lone inline child (no split promoted a block to
	// body level). body is then InlineFC with the single inline box as its child.
	if body.Formatting != cssbox.InlineFC {
		t.Errorf("body Formatting = %d, want InlineFC (no block broke out): %s", body.Formatting, dump(body))
	}
	if firstByKind(body, cssbox.BoxAnonInline) != nil {
		t.Errorf("inline-block in an inline must not trigger a block-in-inline split: %s", dump(body))
	}
	outer := body.Children[0]
	if outer.Kind != cssbox.BoxInline {
		t.Fatalf("body child = %v, want a single BoxInline (the outer span): %s", outer.Kind, dump(body))
	}
	// The outer span keeps the inline-block nested (it was not lifted out).
	if firstByDisplay(outer, cssbox.DisplayInlineBlock) == nil {
		t.Errorf("inline-block should remain nested in the outer span: %s", dump(outer))
	}
}

// TestWhitespaceAroundInlineBlockPreserved: whitespace between two inline-blocks
// is significant (they are inline-level), so the space text node survives — it is
// NOT stripped like inter-block whitespace.
func TestWhitespaceAroundInlineBlockPreserved(t *testing.T) {
	root := build(t, `<html><body><div><span style="display:inline-block">a</span> <span style="display:inline-block">b</span></div></body></html>`, nil)
	div := root.Children[0].Children[0]
	if div.Formatting != cssbox.InlineFC {
		t.Errorf("div Formatting = %d, want InlineFC: %s", div.Formatting, dump(div))
	}
	// Children: [inline-block, text(" "), inline-block]; the space must survive.
	if len(div.Children) != 3 {
		t.Fatalf("div children = %d, want 3 (ib, space, ib): %s", len(div.Children), dump(div))
	}
	mid := div.Children[1]
	if mid.Kind != cssbox.BoxText || mid.Text != " " {
		t.Errorf("middle child = %s, want a text node with a single space (whitespace around inline-block preserved)", dump(mid))
	}
}

// dump renders a box subtree compactly for failure messages.
func dump(b *cssbox.Box) string {
	return dumpIndent(b, 0)
}

func dumpIndent(b *cssbox.Box, depth int) string {
	pad := ""
	for i := 0; i < depth; i++ {
		pad += "  "
	}
	s := pad + kindName(b.Kind)
	if b.Kind == cssbox.BoxText {
		s += " " + quote(b.Text)
	}
	s += "\n"
	for _, c := range b.Children {
		s += dumpIndent(c, depth+1)
	}
	return s
}

func kindName(k cssbox.BoxKind) string {
	switch k {
	case cssbox.BoxBlock:
		return "Block"
	case cssbox.BoxInline:
		return "Inline"
	case cssbox.BoxAnonBlock:
		return "AnonBlock"
	case cssbox.BoxAnonInline:
		return "AnonInline"
	case cssbox.BoxReplaced:
		return "Replaced"
	case cssbox.BoxText:
		return "Text"
	}
	return "?"
}

func quote(s string) string { return "\"" + s + "\"" }
