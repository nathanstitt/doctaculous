package css

import (
	"context"
	"strings"
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/html"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
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
	if len(frags) != 3 {
		t.Fatalf("justify-content:%s want 3 item fragments, got %d", jc, len(frags))
	}
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

// alignFrags lays out two items of heights 40 and 80 in a row with the given align-items
// and returns their Y positions and heights. The line cross size is 80 (the taller item).
func alignFrags(t *testing.T, alignItems, alignSelf0 string) []*Fragment {
	mk := func(h float64, self string) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Value: h, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: self,
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	return flexFrags(t, flexRow(gcss.ComputedStyle{AlignItems: alignItems}, mk(40, alignSelf0), mk(80, "auto")), 300)
}

func TestAlignItemsFlexStart(t *testing.T) {
	f := alignFrags(t, "flex-start", "auto")
	if f[0].Y != 0 || f[1].Y != 0 {
		t.Errorf("flex-start: both items at cross-start y0; got y%v, y%v", f[0].Y, f[1].Y)
	}
	if f[0].H != 40 {
		t.Errorf("flex-start short item keeps its height 40; got %v", f[0].H)
	}
}

func TestAlignItemsFlexEnd(t *testing.T) {
	f := alignFrags(t, "flex-end", "auto")
	// line cross 80; short item (40) sits at y = 80-40 = 40.
	if f[0].Y != 40 || f[0].H != 40 {
		t.Errorf("flex-end short item = y%v h%v, want y40 h40", f[0].Y, f[0].H)
	}
	if f[1].Y != 0 {
		t.Errorf("flex-end tall item at y0; got %v", f[1].Y)
	}
}

func TestAlignItemsCenter(t *testing.T) {
	f := alignFrags(t, "center", "auto")
	// short item centered in 80: y = (80-40)/2 = 20.
	if f[0].Y != 20 || f[0].H != 40 {
		t.Errorf("center short item = y%v h%v, want y20 h40", f[0].Y, f[0].H)
	}
}

func TestAlignItemsStretch(t *testing.T) {
	f := alignFrags(t, "stretch", "auto")
	// item0 has a definite height (40px), so the stretch guard does NOT relayout it —
	// per spec, stretch only applies when the cross size is auto. It stays h40 at y0.
	if f[0].H != 40 {
		t.Errorf("stretch with definite height keeps 40; got %v", f[0].H)
	}
}

func TestAlignStretchGrowsAutoHeight(t *testing.T) {
	// An item with auto height stretches to the line cross size.
	short := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Unit: gcss.UnitAuto},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx}, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	tall := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 80, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx}, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	f := flexFrags(t, flexRow(gcss.ComputedStyle{AlignItems: "stretch"}, short, tall), 300)
	if f[0].H != 80 {
		t.Errorf("stretch auto-height item should grow to line cross 80; got %v", f[0].H)
	}
}

func TestAlignSelfOverridesAlignItems(t *testing.T) {
	f := alignFrags(t, "flex-start", "center")
	// align-items flex-start but item0 align-self center => y = (80-40)/2 = 20.
	if f[0].Y != 20 {
		t.Errorf("align-self:center overrides align-items:flex-start; y = %v, want 20", f[0].Y)
	}
}

func TestFlexOrderReorders(t *testing.T) {
	// Three items given DISTINCT widths so position is identifiable by width: in document
	// order width 30 (order 2), 50 (order 0), 70 (order 1). After ordering, visual order
	// is the order-0 item (w50), order-1 (w70), order-2 (w30). With no grow, packed at
	// start: x 0, 50, 120. The returned frags are in visual order, so their widths must be
	// 50, 70, 30 — proving the reorder.
	mk := func(w float64, order int) *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto", Order: order,
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, mk(30, 2), mk(50, 0), mk(70, 1)), 300)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	wantW := []float64{50, 70, 30} // visual order after sorting by `order`
	for i, w := range wantW {
		if frags[i].W != w {
			t.Errorf("order position %d width = %v, want %v (widths: %v %v %v)", i, frags[i].W, w, frags[0].W, frags[1].W, frags[2].W)
		}
	}
	if frags[0].X != 0 || frags[1].X != 50 || frags[2].X != 120 {
		t.Errorf("ordered items packed at 0/50/120; got %v/%v/%v", frags[0].X, frags[1].X, frags[2].X)
	}
}

func TestFlexMainGapSpacesItems(t *testing.T) {
	// Two fixed 50px items, column-gap 20 => x0 and x70 (50 + 20 gap).
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{ColumnGap: gcss.Length{Value: 20, Unit: gcss.UnitPx}}, mk(), mk()), 300)
	if frags[0].X != 0 || frags[1].X != 70 {
		t.Errorf("column-gap:20 => x0,x70; got x%v,x%v", frags[0].X, frags[1].X)
	}
}

func TestInlineFlexFlowsInline(t *testing.T) {
	// An inline-flex container with two 30px items sits inline after some text. Assert the
	// flex items lay out (widths 30) — i.e. inline-flex reaches layoutFlex, not a fallback.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 30, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 20, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth:   gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow:   0,
			FlexShrink: 0,
			FlexBasis:  gcss.Length{Unit: gcss.UnitAuto},
			AlignSelf:  "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	ifx := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayInlineFlex, Formatting: cssbox.FlexFC,
		Style:    gcss.ComputedStyle{FlexDirection: "row", AlignItems: "stretch", JustifyContent: "flex-start", FlexWrap: "nowrap"},
		Children: []*cssbox.Box{mk(), mk()}}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{ifx}}
	root := e.layoutTree(context.Background(), body, 300)
	// Find the two 30×20 item fragments anywhere in the tree.
	var items []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 30 && f.H == 20 {
			items = append(items, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(items) != 2 {
		t.Fatalf("want 2 inline-flex item fragments (30x20), got %d", len(items))
	}
	// Prove flex-ROW layout (not a coincidental block stack): the two items share a Y
	// (same line) and sit at different X (side by side). Sort by X so walk order doesn't matter.
	if items[0].X > items[1].X {
		items[0], items[1] = items[1], items[0]
	}
	if items[0].Y != items[1].Y {
		t.Errorf("inline-flex items should share a Y (flex row); got y%v and y%v", items[0].Y, items[1].Y)
	}
	if items[0].X == items[1].X {
		t.Errorf("inline-flex items should sit at different X (side by side); both at x%v", items[0].X)
	}
}

// TestInlineFlexClassification verifies that display:inline-flex is classified as
// DisplayInlineFlex / FlexFC by the full HTML build path and that such a container
// flows inline (isBlockLevelOuter returns false, so a parent block with only inline-flex
// children reconciles to InlineFC).
func TestInlineFlexClassification(t *testing.T) {
	src := `<!DOCTYPE html><html><head><style>
		.ifx { display: inline-flex; width: 120px; height: 40px; }
		.item { width: 30px; height: 20px; }
	</style></head><body>
		<div class="ifx"><div class="item"></div><div class="item"></div></div>
	</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Walk the box tree to find the inline-flex box.
	var findFlex func(b *cssbox.Box) *cssbox.Box
	findFlex = func(b *cssbox.Box) *cssbox.Box {
		if b.Display == cssbox.DisplayInlineFlex {
			return b
		}
		for _, c := range b.Children {
			if f := findFlex(c); f != nil {
				return f
			}
		}
		return nil
	}
	ifx := findFlex(root)
	if ifx == nil {
		t.Fatal("no DisplayInlineFlex box found in tree")
	}
	if ifx.Kind != cssbox.BoxBlock {
		t.Errorf("inline-flex Kind = %v, want BoxBlock", ifx.Kind)
	}
	if ifx.Formatting != cssbox.FlexFC {
		t.Errorf("inline-flex Formatting = %v, want FlexFC", ifx.Formatting)
	}
	// The parent of an inline-flex-only block should reconcile to InlineFC: the
	// inline-flex box is inline-level-outer (isBlockLevelOuter returns false for it).
	var findParent func(b, target *cssbox.Box) *cssbox.Box
	findParent = func(b, target *cssbox.Box) *cssbox.Box {
		for _, c := range b.Children {
			if c == target {
				return b
			}
			if p := findParent(c, target); p != nil {
				return p
			}
		}
		return nil
	}
	parent := findParent(root, ifx)
	if parent == nil {
		t.Fatal("no parent found for inline-flex box")
	}
	if parent.Formatting != cssbox.InlineFC {
		t.Errorf("parent of inline-flex-only box Formatting = %v, want InlineFC", parent.Formatting)
	}
	// Verify the full layout path: lay out and find the two 30×20 item fragments.
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), root, 300)
	var items []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 30 && f.H == 20 {
			items = append(items, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(items) != 2 {
		t.Fatalf("HTML-path: want 2 inline-flex item fragments (30x20), got %d", len(items))
	}
}

// wrapItem is a fixed-size, non-flexing item for the wrap tests.
func wrapItem(w, h float64) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: h, Unit: gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
		FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
}

// TestFlexWrapBreaksLines: three 100px items in a 150px container wrap onto three
// lines, each starting back at the main-start. This replaces
// TestFlexWrapDegradesToNowrap, which asserted the single-line fallback.
func TestFlexWrapBreaksLines(t *testing.T) {
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"},
		wrapItem(100, 40), wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	// Each item is too wide to share a line, so all three stack.
	for i, f := range frags {
		if f.X != 0 {
			t.Errorf("item %d X = %v, want 0 (each line restarts at main-start)", i, f.X)
		}
		if want := float64(i * 40); f.Y != want {
			t.Errorf("item %d Y = %v, want %v (line %d)", i, f.Y, want, i)
		}
	}
}

// TestFlexNowrapStillOverflows pins that the default is unchanged: without wrap the
// items stay on one line and overflow, exactly as before.
func TestFlexNowrapStillOverflows(t *testing.T) {
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{},
		wrapItem(100, 40), wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	if frags[0].Y != frags[1].Y || frags[1].Y != frags[2].Y {
		t.Errorf("nowrap must keep one line; got Y %v %v %v", frags[0].Y, frags[1].Y, frags[2].Y)
	}
	if frags[2].X != 200 { // 0,100,200 — overflows the 150 viewport
		t.Errorf("third item should overflow at x200; got %v", frags[2].X)
	}
}

// TestFlexWrapPacksMultiplePerLine: items that DO fit together share a line, and the
// line breaks only when the next item would exceed the container.
func TestFlexWrapPacksMultiplePerLine(t *testing.T) {
	// Four 60px items in a 200px container: three fit (180), the fourth wraps.
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"},
		wrapItem(60, 30), wrapItem(60, 30), wrapItem(60, 30), wrapItem(60, 30)), 200)
	if len(frags) != 4 {
		t.Fatalf("want 4 frags, got %d", len(frags))
	}
	for i, want := range []float64{0, 60, 120, 0} {
		if frags[i].X != want {
			t.Errorf("item %d X = %v, want %v", i, frags[i].X, want)
		}
	}
	if frags[0].Y != frags[2].Y {
		t.Errorf("items 0-2 should share a line; Y %v vs %v", frags[0].Y, frags[2].Y)
	}
	if frags[3].Y != frags[0].Y+30 {
		t.Errorf("item 3 should be on the second line at Y %v; got %v", frags[0].Y+30, frags[3].Y)
	}
}

// TestFlexWrapOverwideItemGetsOwnLine: an item wider than the container still lands on
// a line of its own rather than looping forever or being dropped.
func TestFlexWrapOverwideItemGetsOwnLine(t *testing.T) {
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"},
		wrapItem(50, 20), wrapItem(300, 20), wrapItem(50, 20)), 100)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	// The over-wide item cannot share with either neighbour, so there are three lines.
	if frags[0].Y == frags[1].Y || frags[1].Y == frags[2].Y {
		t.Errorf("the over-wide item should occupy its own line; Y %v %v %v",
			frags[0].Y, frags[1].Y, frags[2].Y)
	}
}

func TestFlexEmptyContainerNoPanic(t *testing.T) {
	fc := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex, Formatting: cssbox.FlexFC,
		Style: gcss.ComputedStyle{FlexDirection: "row"}}
	e := New(nil, nil, nil)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{fc}}
	// Must not panic; produce a (zero-ish height) fragment.
	root := e.layoutTree(context.Background(), body, 300)
	if root == nil {
		t.Fatal("nil root")
	}
}

func TestFlexShrinkZeroOverflows(t *testing.T) {
	// Two 100px items, flex-shrink 0, in a 150 container: they cannot shrink, so they
	// overflow (x0,x100). No panic, no clamp below their size.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 100, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, mk(), mk()), 150)
	if frags[0].W != 100 || frags[1].W != 100 || frags[1].X != 100 {
		t.Errorf("shrink:0 items keep 100 and overflow; got w%v/w%v x%v", frags[0].W, frags[1].W, frags[1].X)
	}
}

func TestFlexBaselineAlignmentCoincidesFirstBaseline(t *testing.T) {
	// align-items:baseline on a row flex container: two items with DIFFERENT font sizes
	// have different first-baseline offsets from the top of their fragments. Real baseline
	// alignment must shift the smaller-font item down so both items' first baselines land
	// at the same page-space Y.
	mkText := func(text string, fontSizePt float64) *cssbox.Box {
		auto := gcss.Length{Unit: gcss.UnitAuto}
		px0 := gcss.Length{Unit: gcss.UnitPx}
		st := gcss.ComputedStyle{
			Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto,
			MinWidth: px0, MinHeight: px0,
			FontFamily: "serif", FontSizePt: fontSizePt,
			FlexGrow: 0, FlexShrink: 0, FlexBasis: auto, AlignSelf: "auto",
		}
		textSt := gcss.ComputedStyle{FontFamily: "serif", FontSizePt: fontSizePt}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
			Formatting: cssbox.InlineFC, Style: st,
			Children: []*cssbox.Box{{Kind: cssbox.BoxText, Text: text, Style: textSt}}}
	}
	itemSmall := mkText("Hi", 10)
	itemLarge := mkText("Hi", 24)
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{AlignItems: "baseline"}, itemSmall, itemLarge), 300)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	linesSmall := firstFragLines(frags[0])
	linesLarge := firstFragLines(frags[1])
	if len(linesSmall) == 0 {
		t.Fatal("small item fragment has no lines (no text baseline produced)")
	}
	if len(linesLarge) == 0 {
		t.Fatal("large item fragment has no lines (no text baseline produced)")
	}
	baselineSmall := linesSmall[0].BaselineY
	baselineLarge := linesLarge[0].BaselineY
	const eps = 0.01
	if absf(baselineSmall-baselineLarge) > eps {
		t.Errorf("flex baseline alignment: small item baseline=%v, large item baseline=%v; want equal (within %.2f)",
			baselineSmall, baselineLarge, eps)
	}
}

// rtlFlexItem builds a fixed-size, non-flexing flex item.
func rtlFlexItem(w float64) *cssbox.Box {
	st := gcss.ComputedStyle{
		Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
		MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
		MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
		FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
}

// TestFlexRTLRowReversesMainAxis: on a row container the main axis IS the inline axis,
// so direction:rtl packs items from the right edge inward. Two distinct-width items
// (60, 40) in a 300-wide container: item 0 occupies [240,300), item 1 [200,240).
func TestFlexRTLRowReversesMainAxis(t *testing.T) {
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{Direction: "rtl"}, rtlFlexItem(60), rtlFlexItem(40)), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].X != 240 || frags[0].W != 60 {
		t.Errorf("RTL row: first item should sit flush right at x240 w60; got x%v w%v", frags[0].X, frags[0].W)
	}
	if frags[1].X != 200 || frags[1].W != 40 {
		t.Errorf("RTL row: second item should sit at x200 w40 (leftward of the first); got x%v w%v", frags[1].X, frags[1].W)
	}
}

// TestFlexRTLComposesWithRowReverse is the double-negative check. RTL flips the main
// axis and so does row-reverse, so applying BOTH must cancel and reproduce plain LTR
// row order. A sign error in axisFor (e.g. assigning rather than XOR-ing reverseMain)
// passes the single-flip test above but fails here.
func TestFlexRTLComposesWithRowReverse(t *testing.T) {
	ltr := flexFrags(t, flexRow(gcss.ComputedStyle{}, rtlFlexItem(60), rtlFlexItem(40)), 300)
	both := flexFrags(t, flexRow(gcss.ComputedStyle{Direction: "rtl", FlexDirection: "row-reverse"},
		rtlFlexItem(60), rtlFlexItem(40)), 300)
	if len(ltr) != 2 || len(both) != 2 {
		t.Fatalf("want 2 frags each, got ltr=%d both=%d", len(ltr), len(both))
	}
	for i := range ltr {
		if ltr[i].X != both[i].X || ltr[i].W != both[i].W {
			t.Errorf("item %d: rtl+row-reverse = x%v w%v, want the LTR-row placement x%v w%v (two flips must cancel)",
				i, both[i].X, both[i].W, ltr[i].X, ltr[i].W)
		}
	}
}

// TestFlexRTLRowReverseIsLTROrder: the single-flip counterpart — row-reverse alone
// under LTR must NOT equal the same content under RTL+row-reverse (guards against a
// change that drops the direction input entirely and makes both tests trivially pass).
func TestFlexRTLRowReverseIsLTROrder(t *testing.T) {
	rev := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "row-reverse"}, rtlFlexItem(60), rtlFlexItem(40)), 300)
	if len(rev) != 2 {
		t.Fatalf("want 2 frags, got %d", len(rev))
	}
	// row-reverse under LTR packs from the right: same as RTL plain row.
	if rev[0].X != 240 {
		t.Errorf("row-reverse under LTR: first item at x%v, want x240", rev[0].X)
	}
}

// TestFlexRTLNoLongerLogs replaces the old degradation assertion.
func TestFlexRTLNoLongerLogs(t *testing.T) {
	var logged []string
	logf := func(f string, _ ...any) { logged = append(logged, f) }
	row := flexRow(gcss.ComputedStyle{Direction: "rtl"}, rtlFlexItem(60), rtlFlexItem(40))
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Children: []*cssbox.Box{row}}
	if frag := New(nil, nil, logf).layoutTree(context.Background(), body, 300); frag == nil {
		t.Fatal("RTL flex row should produce a fragment")
	}
	for _, m := range logged {
		if strings.Contains(m, "RTL") {
			t.Errorf("RTL flex row still logs a degradation: %q", m)
		}
	}
}

// flexItemsAt builds an HTML page placing a flex container inside a left/top-padded
// outer block (so the container's content box is NOT at the page origin), lays it out,
// and returns the two flex item fragments (the .a and .b boxes) found anywhere in the
// tree. dir is the flex-direction. This exercises the page-space origin mapping in
// placeFlexFragment for a container at a non-zero content-box X.
func flexItemsAt(t *testing.T, dir string) (a, b *Fragment) {
	t.Helper()
	src := `<!DOCTYPE html><html><head><style>
		body { margin: 0; }
		.outer { padding-left: 60px; padding-top: 10px; }
		.fc { display: flex; flex-direction: ` + dir + `; }
		.a { width: 100px; height: 40px; background: #553399; }
		.b { width: 80px; height: 60px; background: #339977; }
	</style></head><body>
		<div class="outer"><div class="fc"><div class="a"></div><div class="b"></div></div></div>
	</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	e := New(nil, nil, nil)
	frag := e.layoutTree(context.Background(), root, 600)
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.W == 100 && f.H == 40 {
			a = f
		}
		if f.W == 80 && f.H == 60 {
			b = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if a == nil || b == nil {
		t.Fatalf("flex items not found: a=%v b=%v", a, b)
	}
	return a, b
}

func TestFlexRowAtNonZeroX(t *testing.T) {
	// A row flex container whose content box starts at x=60 (outer padding-left) and y=10
	// (padding-top). Items flow horizontally from that origin: a at (60,10), b at (160,10).
	a, b := flexItemsAt(t, "row")
	if a.X != 60 || a.Y != 10 {
		t.Errorf("row item a = x%v y%v, want x60 y10", a.X, a.Y)
	}
	if b.X != 160 || b.Y != 10 {
		t.Errorf("row item b = x%v y%v, want x160 y10", b.X, b.Y)
	}
}

func TestFlexColumnAtNonZeroX(t *testing.T) {
	// A column flex container at the same non-zero content-box origin. Items stack
	// vertically AT that x: a at (60,10), b at (60,50). Regression for the page-space
	// origin mapping (rect maps cross→x for a column, so the contentX origin must ride
	// the CROSS axis, not the main axis — otherwise items collapse to x0 and shift down
	// by contentX).
	a, b := flexItemsAt(t, "column")
	if a.X != 60 || a.Y != 10 {
		t.Errorf("column item a = x%v y%v, want x60 y10", a.X, a.Y)
	}
	if b.X != 60 || b.Y != 50 {
		t.Errorf("column item b = x%v y%v, want x60 y50", b.X, b.Y)
	}
}

func TestAlignStretchColumnGrowsAutoWidth(t *testing.T) {
	// flex-direction:column, align-items:stretch (default). A narrow auto-WIDTH item and a
	// definite 80px-wide item, in a block-level column flex container laid out at content
	// width 300. The cross axis is the WIDTH, which is DEFINITE (300) for this container
	// (H3), so the line cross size is 300 and the auto-width item stretches to fill it.
	// Exercises the column branch of stretchFlexItem.
	narrow := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Unit: gcss.UnitAuto}, Height: gcss.Length{Value: 30, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx}, MinHeight: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	wide := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{Width: gcss.Length{Value: 80, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx}, MinHeight: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexDirection: "column", AlignItems: "stretch"}, narrow, wide), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	// frags[0] is the narrow auto-width item; it stretches to the container's definite
	// cross size (width) of 300, not just the widest item's 80.
	if frags[0].W != 300 {
		t.Errorf("column stretch: auto-width item should grow to the container cross width 300; got %v", frags[0].W)
	}
}

func TestFlexOrderTieStability(t *testing.T) {
	// Ties in `order` must preserve document order (stable sort). Items: w30 order1,
	// w50 order0, w70 order1. Visual order: the order-0 item (w50) first, then the two
	// order-1 items in DOCUMENT order (w30 before w70). No grow => packed at x 0,50,80.
	mk := func(w float64, order int) *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: w, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
				MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
				MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
				FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto", Order: order}}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{}, mk(30, 1), mk(50, 0), mk(70, 1)), 300)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	wantW := []float64{50, 30, 70} // order-0 first, then order-1 ties in document order
	for i, w := range wantW {
		if frags[i].W != w {
			t.Errorf("tie stability: position %d width = %v, want %v (widths %v %v %v)", i, frags[i].W, w, frags[0].W, frags[1].W, frags[2].W)
		}
	}
}

func TestFlexGapAndSpaceBetween(t *testing.T) {
	// column-gap AND justify-content:space-between compose: the `between` spacing adds on
	// top of the gap. Two 50px items, 300px container, gap 20: free = 300-100-20 = 180,
	// between = 180/(2-1) = 180. Second item at x = 50 + 20 + 180 = 250 (the right end).
	mk := func() *cssbox.Box {
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC,
			Style: gcss.ComputedStyle{Width: gcss.Length{Value: 50, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
				MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
				MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
				FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto"}}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{ColumnGap: gcss.Length{Value: 20, Unit: gcss.UnitPx}, JustifyContent: "space-between"}, mk(), mk()), 300)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].X != 0 || frags[1].X != 250 {
		t.Errorf("gap+space-between: want x0 and x250; got x%v and x%v", frags[0].X, frags[1].X)
	}
}

// TestFlexAlignCenterUsesDefiniteHeight pins H3: align-items:center on a ROW flex
// container with a DEFINITE height centers items within that height, not within the
// tallest item's extent. A 100px-tall row with a 20px and a 40px item centers them at
// y=40 and y=30 (within 100), not y=10/y=0 (within the 40px max item). Mutation-verify:
// remove the flexCrossSize clamp and the 20px item centers at y=10.
func TestFlexAlignCenterUsesDefiniteHeight(t *testing.T) {
	src := `<div style="display:flex;align-items:center;height:100px">` +
		`<div style="width:30px;height:20px;background:#a00">a</div>` +
		`<div style="width:30px;height:40px;background:#0a0">b</div>` +
		`</div>`
	root := layoutTreeFor(t, src, 300, nil)
	// Collect the two flex items (BFC children with the given heights).
	var items []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.H == 20 || f.H == 40 {
			if f.W == 30 {
				items = append(items, f)
			}
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(items) != 2 {
		t.Fatalf("want 2 flex items, got %d", len(items))
	}
	// Find the 20px item; its top should be ~40 (centered in 100), not ~10 (in 40).
	var small *Fragment
	for _, it := range items {
		if it.H == 20 {
			small = it
		}
	}
	if small == nil {
		t.Fatal("no 20px item")
	}
	// The flex container is at the body content top (Y 0). The 20px item centers at
	// (100-20)/2 = 40 within the container.
	if small.Y < 35 || small.Y > 45 {
		t.Errorf("20px item Y = %.1f, want ~40 (centered in the 100px row); the H3 bug gives ~10", small.Y)
	}
}

// TestFlexRTLColumnMirrorsCrossAxis: on a COLUMN container the main axis is vertical
// and direction-independent, but the CROSS axis is the inline one — so direction:rtl
// swaps cross-start and cross-end. align-items:flex-start must pin items to the RIGHT
// edge, and flex-end to the left. This is the case the old `&& !ax.vertical` log guard
// skipped entirely, so it used to be silently wrong rather than logged.
func TestFlexRTLColumnMirrorsCrossAxis(t *testing.T) {
	// Narrow items in a wide container so the cross offset is visible.
	mk := func(w float64) *cssbox.Box { return rtlFlexItem(w) }

	col := func(dir, align string) []*Fragment {
		return flexFrags(t, flexRow(gcss.ComputedStyle{
			FlexDirection: "column", Direction: dir, AlignItems: align,
		}, mk(60), mk(40)), 300)
	}

	// LTR baseline: flex-start pins to the left (x=0).
	ltrStart := col("ltr", "flex-start")
	if len(ltrStart) != 2 {
		t.Fatalf("want 2 frags, got %d", len(ltrStart))
	}
	if ltrStart[0].X != 0 || ltrStart[1].X != 0 {
		t.Fatalf("LTR column flex-start should pin left; got x%v and x%v", ltrStart[0].X, ltrStart[1].X)
	}

	// RTL: flex-start pins to the RIGHT — each item's right edge at the container's.
	rtlStart := col("rtl", "flex-start")
	if got, want := rtlStart[0].X+rtlStart[0].W, 300.0; got != want {
		t.Errorf("RTL column flex-start: item 0 right edge = %v, want %v (pinned to cross-start = right)", got, want)
	}
	if got, want := rtlStart[1].X+rtlStart[1].W, 300.0; got != want {
		t.Errorf("RTL column flex-start: item 1 right edge = %v, want %v", got, want)
	}

	// RTL flex-end pins to the LEFT.
	rtlEnd := col("rtl", "flex-end")
	if rtlEnd[0].X != 0 || rtlEnd[1].X != 0 {
		t.Errorf("RTL column flex-end should pin left; got x%v and x%v", rtlEnd[0].X, rtlEnd[1].X)
	}

	// center is direction-invariant.
	ltrC, rtlC := col("ltr", "center"), col("rtl", "center")
	for i := range ltrC {
		if ltrC[i].X != rtlC[i].X {
			t.Errorf("item %d: center should be direction-invariant; ltr x%v vs rtl x%v", i, ltrC[i].X, rtlC[i].X)
		}
	}
}

// TestFlexCrossOffsetAcceptsBoxAlignmentSpellings: the cascade parses the CSS Box
// Alignment keywords `start`/`end` for align-items/align-self, but crossOffset used to
// switch only on the Flexbox `flex-start`/`flex-end` spellings — so `align-items:end`
// silently fell through to flex-start. Pre-existing bug, fixed alongside RTL.
func TestFlexCrossOffsetAcceptsBoxAlignmentSpellings(t *testing.T) {
	ltr := flexAxis{}
	if got, want := crossOffset("end", 100, 40, ltr), crossOffset("flex-end", 100, 40, ltr); got != want {
		t.Errorf("crossOffset(\"end\") = %v, want %v (same as flex-end)", got, want)
	}
	if got, want := crossOffset("start", 100, 40, ltr), crossOffset("flex-start", 100, 40, ltr); got != want {
		t.Errorf("crossOffset(\"start\") = %v, want %v (same as flex-start)", got, want)
	}
}

// h4ColumnFrags lays out a column flex container whose items hold differing amounts
// of text, and returns the item fragments.
func h4ColumnFrags(t *testing.T, containerCSS string) []*Fragment {
	t.Helper()
	src := `<!DOCTYPE html><html><head><style>
	  body { margin: 0; }
	  .col { display: flex; flex-direction: column; width: 200px; ` + containerCSS + ` }
	  .item { background: #ccc; }
	</style></head><body>
	  <div class="col">
	    <div class="item">short</div>
	    <div class="item">this one has substantially more text so it wraps onto several lines at this width</div>
	  </div>
	</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, 400)
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
	find(frag)
	if fc == nil {
		t.Fatal("no flex container fragment")
	}
	return fc.Children
}

// TestFlexColumnItemsFitContainerWidth: an auto-width item in a COLUMN flex container
// is laid out at the container's inner cross size, not at its own max-content width.
//
// Regression for the second half of backlog H4: measureMaxContent returns the width of
// the whole unwrapped string, so a paragraph of prose was laid out several times wider
// than its container and overflowed it.
func TestFlexColumnItemsFitContainerWidth(t *testing.T) {
	frags := h4ColumnFrags(t, "height: 400px;")
	if len(frags) != 2 {
		t.Fatalf("want 2 items, got %d", len(frags))
	}
	for i, f := range frags {
		if f.W > 200.01 {
			t.Errorf("item %d width = %v, want <= 200 (the container's inner cross size); "+
				"an auto-width column item must not be laid out at its max-content width", i, f.W)
		}
	}
}

// TestFlexColumnBaseSizeIsContentHeight: on a COLUMN container the main axis is
// VERTICAL, so an auto/content flex-basis must resolve to the item's content HEIGHT.
// It used to resolve to measureMaxContent — a WIDTH — comparing a horizontal measure
// against a vertical budget (backlog H4).
//
// The container is auto-height so the items keep their hypothetical (base) main sizes
// rather than being flexed to fill: the item with more text must therefore end up
// TALLER than the one-liner, and both heights must be plausible line-height multiples
// rather than a leftover-space split.
func TestFlexColumnBaseSizeIsContentHeight(t *testing.T) {
	frags := h4ColumnFrags(t, "") // no height => indefinite main size
	if len(frags) != 2 {
		t.Fatalf("want 2 items, got %d", len(frags))
	}
	short, long := frags[0], frags[1]
	if short.H <= 0 || long.H <= 0 {
		t.Fatalf("items should have positive heights; got %v and %v", short.H, long.H)
	}
	if long.H <= short.H {
		t.Errorf("the item with more text should be taller: short h=%v, long h=%v", short.H, long.H)
	}
	// The discriminating check. The long item's text wraps to a handful of lines at
	// 200px, so its height is a small multiple of the line height (~74pt). Its
	// max-content WIDTH — the whole string unwrapped — is ~498pt. Using the width as
	// the main size therefore produces a height far taller than the item is wide, which
	// wrapped text can never be at this measure.
	if long.H > long.W {
		t.Errorf("wrapped item is taller (%v) than it is wide (%v): the main size looks like "+
			"a max-content WIDTH being used as a HEIGHT (backlog H4)", long.H, long.W)
	}
	// Sanity: a single line of text is about one line-height tall.
	if short.H > 40 {
		t.Errorf("single-line item height = %v, want ~1 line-height (<40)", short.H)
	}
}

// wrapContainer builds a wrapping row with a definite height, so align-content has
// leftover cross space to distribute.
func wrapContainer(st gcss.ComputedStyle, hPx float64, items ...*cssbox.Box) *cssbox.Box {
	st.FlexWrap = orDefault(st.FlexWrap, "wrap")
	c := flexRow(st, items...)
	c.Style.Height = gcss.Length{Value: hPx, Unit: gcss.UnitPx}
	return c
}

// TestFlexWrapCrossGapBetweenLines: row-gap separates the LINES of a wrapping row.
// The cross gap was previously computed and never read, because a single-line
// container has no between-lines gap.
func TestFlexWrapCrossGapBetweenLines(t *testing.T) {
	st := gcss.ComputedStyle{FlexWrap: "wrap", RowGap: gcss.Length{Value: 12, Unit: gcss.UnitPx}}
	frags := flexFrags(t, flexRow(st, wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if got, want := frags[1].Y-frags[0].Y, 52.0; got != want {
		t.Errorf("line stride = %v, want %v (40 item + 12 row-gap)", got, want)
	}
}

// TestFlexAlignContentStretch: the CSS Flexbox initial align-content is STRETCH, so
// lines grow to fill a definite-height container. ComputedStyle defaults the field to
// "start" (grid's convention), so this pins that flex maps it correctly — otherwise
// multi-line content packs to the cross-start and leaves the container's tail empty.
func TestFlexAlignContentStretch(t *testing.T) {
	// Two lines of 40px items in a 200px-tall container: 120px leftover, 60 per line.
	frags := flexFrags(t, wrapContainer(gcss.ComputedStyle{}, 200,
		wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].Y != 0 {
		t.Errorf("first line Y = %v, want 0", frags[0].Y)
	}
	// Each line stretched to 100 (40 + 60 share), so the second starts there.
	if got, want := frags[1].Y, 100.0; got != want {
		t.Errorf("second line Y = %v, want %v (lines stretch to fill the container)", got, want)
	}
}

// TestFlexAlignContentCenter: an explicit align-content packs the lines and centers
// the block of them, rather than stretching.
func TestFlexAlignContentCenter(t *testing.T) {
	st := gcss.ComputedStyle{AlignContent: "center"}
	frags := flexFrags(t, wrapContainer(st, 200, wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	// Two 40px lines = 80 tall, centered in 200 => leading 60.
	if got, want := frags[0].Y, 60.0; got != want {
		t.Errorf("first line Y = %v, want %v (centered)", got, want)
	}
	if got, want := frags[1].Y, 100.0; got != want {
		t.Errorf("second line Y = %v, want %v", got, want)
	}
}

// TestFlexAlignContentSpaceBetween: the lines pin to the container's cross edges.
func TestFlexAlignContentSpaceBetween(t *testing.T) {
	st := gcss.ComputedStyle{AlignContent: "space-between"}
	frags := flexFrags(t, wrapContainer(st, 200, wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(frags) != 2 {
		t.Fatalf("want 2 frags, got %d", len(frags))
	}
	if frags[0].Y != 0 {
		t.Errorf("first line Y = %v, want 0", frags[0].Y)
	}
	if got, want := frags[1].Y, 160.0; got != want {
		t.Errorf("second line Y = %v, want %v (pinned to the cross-end)", got, want)
	}
}

// TestFlexWrapReverseStacksFromCrossEnd: wrap-reverse puts the FIRST line at the
// cross-END, so the line order flips while each line's item order does not.
func TestFlexWrapReverseStacksFromCrossEnd(t *testing.T) {
	normal := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"},
		wrapItem(100, 40), wrapItem(100, 40)), 150)
	reversed := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap-reverse"},
		wrapItem(100, 40), wrapItem(100, 40)), 150)
	if len(normal) != 2 || len(reversed) != 2 {
		t.Fatalf("want 2 frags each, got %d and %d", len(normal), len(reversed))
	}
	// wrap: item 0 on the first line (top). wrap-reverse: item 0 on the LAST line.
	if !(normal[0].Y < normal[1].Y) {
		t.Errorf("wrap: item 0 should be above item 1; Y %v vs %v", normal[0].Y, normal[1].Y)
	}
	if !(reversed[0].Y > reversed[1].Y) {
		t.Errorf("wrap-reverse: item 0 should be BELOW item 1 (lines stack from the cross-end); Y %v vs %v",
			reversed[0].Y, reversed[1].Y)
	}
	// The items keep their main-axis position either way — only the line order flips.
	if reversed[0].X != 0 || reversed[1].X != 0 {
		t.Errorf("wrap-reverse must not move items along the main axis; X %v %v",
			reversed[0].X, reversed[1].X)
	}
}

// TestFlexWrapPerLineJustifyContent: justify-content distributes free space within
// EACH line independently (§9.5), not across the container.
func TestFlexWrapPerLineJustifyContent(t *testing.T) {
	st := gcss.ComputedStyle{FlexWrap: "wrap", JustifyContent: "flex-end"}
	// Three 80px items in a 200px row: two fit on line 1 (160), the third wraps.
	frags := flexFrags(t, flexRow(st, wrapItem(80, 30), wrapItem(80, 30), wrapItem(80, 30)), 200)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags, got %d", len(frags))
	}
	// Line 1 holds two items pushed right: 200-160 = 40 leading.
	if frags[0].X != 40 || frags[1].X != 120 {
		t.Errorf("line 1 flex-end: X %v %v, want 40 120", frags[0].X, frags[1].X)
	}
	// Line 2 holds ONE item, so its own free space is 120 — not the container's.
	if got, want := frags[2].X, 120.0; got != want {
		t.Errorf("line 2 flex-end: X = %v, want %v (justify-content is per-line)", got, want)
	}
	if frags[2].Y == frags[0].Y {
		t.Error("the third item should have wrapped to a second line")
	}
}
