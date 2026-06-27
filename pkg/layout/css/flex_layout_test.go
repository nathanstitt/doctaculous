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

func TestFlexBasisAutoUsesWidth(t *testing.T) {
	// basis auto, width 120 => base 120; no grow/shrink => stays 120 at x0.
	st := gcss.ComputedStyle{
		Width: gcss.Length{Value: 120, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
	}
	item := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, item), 300)
	if frags[0].W != 120 {
		t.Errorf("basis:auto width:120 => w%v, want 120", frags[0].W)
	}
}

func TestFlexAutoMinimumFloorsShrink(t *testing.T) {
	// Two text items, basis auto (=> content size), flex-shrink 1, no explicit min.
	// The container is narrow enough that naive shrink would crush them below their
	// min-content; the automatic minimum must floor each at its min-content width.
	mk := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Unit: gcss.UnitAuto}, FontFamily: "serif", FontSizePt: 16,
			MinWidth: gcss.Length{Unit: gcss.UnitAuto}, // auto => automatic minimum applies
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			FlexGrow: 0, FlexShrink: 1, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: text, Style: st}}}
	}
	a := mk("Wonderful")
	b := mk("Magnificent")
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 80) // intentionally too narrow
	// Each item must be at least its min-content width (the longest word), so the two
	// together overflow 80 rather than shrinking to fit. Assert neither is crushed to ~0.
	if frags[0].W < 40 || frags[1].W < 40 {
		t.Errorf("auto-minimum should floor items at min-content (~70/78pt); got w %v and %v", frags[0].W, frags[1].W)
	}
}

func TestFlexExplicitMinZeroAllowsFullShrink(t *testing.T) {
	// Same as above but min-width:0 explicitly => items MAY shrink below content.
	mk := func(text string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Unit: gcss.UnitAuto}, FontFamily: "serif", FontSizePt: 16,
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx}, // explicit 0 => no automatic minimum
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			FlexGrow: 0, FlexShrink: 1, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.InlineFC,
			Style: st, Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: text, Style: st}}}
	}
	a := mk("Wonderful")
	b := mk("Magnificent")
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, a, b), 80)
	total := frags[0].W + frags[1].W
	if total > 81 {
		t.Errorf("with min-width:0 items should shrink to fit ~80; total w = %v", total)
	}
}

func TestFlexColumnStacksVertically(t *testing.T) {
	// column, two items width 100 height 40 and 60, basis auto, no grow/shrink.
	// They stack vertically: item0 at y0 h40, item1 at y40 h60. Both x0.
	mk := func(w, h float64) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: h, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinHeight: gcss.Length{Value: 0, Unit: gcss.UnitPx}, MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "column"}, mk(100, 40), mk(100, 60)), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].Y != 0 || frags[0].H != 40 {
		t.Errorf("col item0 = y%v h%v, want y0 h40", frags[0].Y, frags[0].H)
	}
	if frags[1].Y != 40 || frags[1].H != 60 {
		t.Errorf("col item1 = y%v h%v, want y40 h60", frags[1].Y, frags[1].H)
	}
}

func TestFlexRowReversePlacesFromEnd(t *testing.T) {
	// row-reverse, viewport 300, two fixed-width items 100 and 50, no grow/shrink.
	// Reverse packs from the main-end: first item's main-start edge is at the right.
	// item0 (100) occupies x[200..300]; item1 (50) occupies x[150..200].
	mk := func(w float64) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "row-reverse"}, mk(100), mk(50)), 300)
	if frags[0].X != 200 || frags[0].W != 100 {
		t.Errorf("row-reverse item0 = x%v w%v, want x200 w100", frags[0].X, frags[0].W)
	}
	if frags[1].X != 150 || frags[1].W != 50 {
		t.Errorf("row-reverse item1 = x%v w%v, want x150 w50", frags[1].X, frags[1].W)
	}
}

// justifyFrags lays out three fixed 50px-wide items in a 300px row with the given
// justify-content and returns their X positions.
func justifyFrags(t *testing.T, jc string) []float64 {
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{JustifyContent: jc}, mk(), mk(), mk()), 300)
	xs := make([]float64, len(frags))
	for i, f := range frags {
		xs[i] = f.X
	}
	return xs
}

func TestJustifyContent(t *testing.T) {
	// 3 items × 50 = 150 used, 150 free in a 300 container.
	cases := []struct {
		jc   string
		want []float64
	}{
		{"flex-start", []float64{0, 50, 100}},
		{"flex-end", []float64{150, 200, 250}},
		{"center", []float64{75, 125, 175}},
		{"space-between", []float64{0, 125, 250}},     // gaps of 75 between
		{"space-around", []float64{25, 125, 225}},     // half-gap 25 at ends, 50 between
		{"space-evenly", []float64{37.5, 125, 212.5}}, // equal 37.5 everywhere
	}
	for _, c := range cases {
		got := justifyFrags(t, c.jc)
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("justify-content:%s item %d X = %v, want %v (all: %v)", c.jc, i, got[i], c.want[i], got)
			}
		}
	}
}
