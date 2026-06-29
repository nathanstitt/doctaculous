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
	fixupFlex(fc)
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
	fixupFlex(fc)
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
	fixupFlex(fc)
	if len(fc.Children) != 2 {
		t.Fatalf("whitespace between block items should be dropped; want 2 children, got %d", len(fc.Children))
	}
}

func TestFlexFixupLeavesBlockChildren(t *testing.T) {
	a := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	b := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC}
	fc := flexContainer(a, b)
	fixupFlex(fc)
	if len(fc.Children) != 2 || fc.Children[0] != a || fc.Children[1] != b {
		t.Errorf("block children should pass through unchanged")
	}
}
