package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// flexItemBox builds a block-level flex item with the given fixed cross size (height)
// and flex grow/shrink/basis. width auto so the main size comes from flex.
func flexItemBox(hPx, grow, shrink float64, basis gcss.Length) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width:     gcss.Length{Unit: gcss.UnitAuto},
		Height:    gcss.Length{Value: hPx, Unit: gcss.UnitPx},
		MaxWidth:  gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		MinWidth:  gcss.Length{Value: 0, Unit: gcss.UnitPx},
		FlexGrow:  grow, FlexShrink: shrink, FlexBasis: basis,
		AlignSelf: "auto",
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: st}
}

func flexRow(style gcss.ComputedStyle, items ...*cssbox.Box) *cssbox.Box {
	style.FlexDirection = orDefault(style.FlexDirection, "row")
	style.AlignItems = orDefault(style.AlignItems, "stretch")
	style.JustifyContent = orDefault(style.JustifyContent, "flex-start")
	style.FlexWrap = orDefault(style.FlexWrap, "nowrap")
	// Default to auto width so the container fills its containing block.
	if style.Width.Unit == gcss.UnitPx && style.Width.Value == 0 {
		style.Width = gcss.Length{Unit: gcss.UnitAuto}
	}
	style.MaxWidth = gcss.Length{Unit: gcss.UnitAuto}
	style.MaxHeight = gcss.Length{Unit: gcss.UnitAuto}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex,
		Formatting: cssbox.FlexFC, Style: style, Children: items}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// flexFrags lays out a flex container inside a body at the given viewport and returns
// the flex item fragments (direct children of the flex container's fragment), in order.
func flexFrags(t *testing.T, container *cssbox.Box, viewport float64) []*Fragment {
	t.Helper()
	e := New(nil, nil, nil)
	// The body uses auto width+height so it fills the viewport (a zero-value Length would
	// resolve to width:0px, not the viewport fill that block normal flow gives).
	bodyStyle := gcss.ComputedStyle{
		Width:     gcss.Length{Unit: gcss.UnitAuto},
		Height:    gcss.Length{Unit: gcss.UnitAuto},
		MaxWidth:  gcss.Length{Unit: gcss.UnitAuto},
		MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
	}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: bodyStyle, Children: []*cssbox.Box{container}}
	root := e.layoutTree(context.Background(), body, viewport)
	if root == nil {
		t.Fatal("nil root fragment")
	}
	// The flex container is the body's only child; its fragment children are the items.
	var fc *Fragment
	var find func(f *Fragment)
	find = func(f *Fragment) {
		if f == nil || fc != nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayFlex {
			fc = f
			return
		}
		for _, c := range f.Children {
			find(c)
		}
	}
	find(root)
	if fc == nil {
		t.Fatal("no flex container fragment found")
	}
	return fc.Children
}

func TestFlexRowGrowDistributesWidth(t *testing.T) {
	// viewport 300, two items, basis 0, grow 1 and 3 => widths 75 and 225, at x 0 and 75.
	a := flexItemBox(40, 1, 1, gcss.Length{Value: 0, Unit: gcss.UnitPx})
	b := flexItemBox(40, 3, 1, gcss.Length{Value: 0, Unit: gcss.UnitPx})
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 item fragments, got %d", len(frags))
	}
	if frags[0].W != 75 || frags[0].X != 0 {
		t.Errorf("item a = x%v w%v, want x0 w75", frags[0].X, frags[0].W)
	}
	if frags[1].W != 225 || frags[1].X != 75 {
		t.Errorf("item b = x%v w%v, want x75 w225", frags[1].X, frags[1].W)
	}
}
