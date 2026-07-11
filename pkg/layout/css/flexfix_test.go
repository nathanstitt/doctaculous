package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func flexContainer(children ...*cssbox.Box) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex,
		Formatting: cssbox.FlexFC, Style: gcss.ComputedStyle{FlexDirection: "row"}, Children: children}
}

func TestFlexFixupWrapsBareText(t *testing.T) {
	txt := &cssbox.Box{Kind: cssbox.BoxText, Text: "hello"}
	fc := flexContainer(txt)
	fixupFlexGrid(fc)
	if len(fc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(fc.Children))
	}
	c := fc.Children[0]
	if c.Kind != cssbox.BoxAnonFlexItem {
		t.Errorf("child kind = %v, want BoxAnonFlexItem", c.Kind)
	}
	if len(c.Children) != 1 || c.Children[0] != txt {
		t.Errorf("anon item should wrap the original text box")
	}
}

func TestFlexFixupBlockifiesInlineChild(t *testing.T) {
	span := &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInline,
		Formatting: cssbox.InlineFC, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: "x"}}}
	fc := flexContainer(span)
	fixupFlexGrid(fc)
	if len(fc.Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(fc.Children))
	}
	if !fc.Children[0].Kind.IsBlockLevel() {
		t.Errorf("inline child should be blockified into a block-level flex item")
	}
}

func TestFlexFixupDropsWhitespaceBetweenBlocks(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	ws := &cssbox.Box{Kind: cssbox.BoxText, Text: "   "}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	fc := flexContainer(a, ws, b)
	fixupFlexGrid(fc)
	if len(fc.Children) != 2 {
		t.Fatalf("whitespace between block items should be dropped; want 2 children, got %d", len(fc.Children))
	}
}

func TestFlexFixupLeavesBlockChildren(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	fc := flexContainer(a, b)
	fixupFlexGrid(fc)
	if len(fc.Children) != 2 || fc.Children[0] != a || fc.Children[1] != b {
		t.Errorf("block children should pass through unchanged")
	}
}

func TestFlexFixupInlineElementsAreSeparateItems(t *testing.T) {
	// CSS Flexbox §4 / Grid §6: every ELEMENT child is its own item (blockified);
	// only contiguous child text wraps in one anonymous item. Two <label>-like
	// inline children must yield two items, not one coalesced run (the htmldoc
	// fieldset-grid regression).
	mkSpan := func(txt string) *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInline,
			Formatting: cssbox.InlineFC, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: txt}}}
	}
	a, b := mkSpan("a"), mkSpan("b")
	fc := flexContainer(a, b)
	fixupFlexGrid(fc)
	if len(fc.Children) != 2 {
		t.Fatalf("want 2 items (one per inline element child), got %d", len(fc.Children))
	}
	for i, c := range fc.Children {
		if !c.Kind.IsBlockLevel() {
			t.Errorf("item %d not blockified: kind %v", i, c.Kind)
		}
		if c.Formatting != cssbox.InlineFC {
			t.Errorf("item %d Formatting = %v, want InlineFC (holds inline content)", i, c.Formatting)
		}
	}
	if fc.Children[0] != a || fc.Children[1] != b {
		t.Errorf("items should be the original element boxes, blockified in place")
	}
}

func TestFlexFixupTextRunsSplitAroundElements(t *testing.T) {
	// text <span> text → three items: anon(text), span, anon(text).
	txt1 := &cssbox.Box{Kind: cssbox.BoxText, Text: "before "}
	span := &cssbox.Box{Kind: cssbox.BoxInline, Display: cssbox.DisplayInline, Formatting: cssbox.InlineFC}
	txt2 := &cssbox.Box{Kind: cssbox.BoxText, Text: " after"}
	fc := flexContainer(txt1, span, txt2)
	fixupFlexGrid(fc)
	if len(fc.Children) != 3 {
		t.Fatalf("want 3 items (anon text / element / anon text), got %d", len(fc.Children))
	}
	if fc.Children[0].Kind != cssbox.BoxAnonFlexItem || fc.Children[2].Kind != cssbox.BoxAnonFlexItem {
		t.Errorf("text runs should wrap in anonymous items: %v / %v", fc.Children[0].Kind, fc.Children[2].Kind)
	}
	if fc.Children[1] != span {
		t.Errorf("middle item should be the element box")
	}
}
