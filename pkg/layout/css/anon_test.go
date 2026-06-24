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
