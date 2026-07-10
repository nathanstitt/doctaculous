package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func gridContainer(children ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayGrid,
		Formatting: cssbox.GridFC, Style: gcss.ComputedStyle{}, Children: children}
}

func TestGridFixupWrapsBareText(t *testing.T) {
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "hello"}
	gc := gridContainer(txt)
	fixupFlexGrid(gc)
	if len(gc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(gc.Children))
	}
	c := gc.Children[0]
	if c.Kind != cssbox.BoxAnonGridItem {
		t.Errorf("child kind = %v, want BoxAnonGridItem", c.Kind)
	}
	if len(c.Children) != 1 || c.Children[0] != txt {
		t.Errorf("anon item should wrap the original text box")
	}
}

func TestGridFixupBlockifiesInlineChild(t *testing.T) {
	span := &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInline,
		Formatting: cssbox.InlineFC, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: "x"}}}
	gc := gridContainer(span)
	fixupFlexGrid(gc)
	if len(gc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(gc.Children))
	}
	if !gc.Children[0].Kind.IsBlockLevel() {
		t.Errorf("inline child should be blockified into a block-level grid item")
	}
}

func TestGridFixupDropsWhitespaceBetweenBlocks(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	ws := &cssbox.Box{Kind: cssbox.BoxText, Text: "   "}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	gc := gridContainer(a, ws, b)
	fixupFlexGrid(gc)
	if len(gc.Children) != 2 {
		t.Fatalf("whitespace between block items should be dropped; want 2 children, got %d", len(gc.Children))
	}
}

func TestGridFixupLeavesBlockChildren(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	gc := gridContainer(a, b)
	fixupFlexGrid(gc)
	if len(gc.Children) != 2 || gc.Children[0] != a || gc.Children[1] != b {
		t.Errorf("block children should pass through unchanged")
	}
}

// TestGridFixupInlineGridContainer exercises the DisplayInlineGrid branch of the
// fixup trigger (display:inline-grid wraps its inline runs into grid items too).
func TestGridFixupInlineGridContainer(t *testing.T) {
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "hi"}
	gc := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayInlineGrid,
		Formatting: cssbox.GridFC, Style: gcss.ComputedStyle{}, Children: []*cssbox.Box{txt}}
	fixupFlexGrid(gc)
	if len(gc.Children) != 1 || gc.Children[0].Kind != cssbox.BoxAnonGridItem {
		t.Fatalf("inline-grid should wrap bare text into a BoxAnonGridItem; got %d children", len(gc.Children))
	}
}
