package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
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

func TestFlexWrapDegradesToNowrap(t *testing.T) {
	// flex-wrap:wrap with three wide items that don't fit: they must stay on one line and
	// overflow (no second row). Assert all three share the same Y and the last overflows.
	mk := func() *cssbox.Box {
		st := gcss.ComputedStyle{
			Width: gcss.Length{Value: 100, Unit: gcss.UnitPx}, Height: gcss.Length{Value: 40, Unit: gcss.UnitPx},
			MaxWidth: gcss.Length{Unit: gcss.UnitAuto}, MaxHeight: gcss.Length{Unit: gcss.UnitAuto},
			MinWidth: gcss.Length{Value: 0, Unit: gcss.UnitPx},
			FlexGrow: 0, FlexShrink: 0, FlexBasis: gcss.Length{Unit: gcss.UnitAuto}, AlignSelf: "auto",
		}
		return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock, Formatting: cssbox.BlockFC, Style: st}
	}
	frags := flexFrags(t, flexRow(gcss.ComputedStyle{FlexWrap: "wrap"}, mk(), mk(), mk()), 150)
	if len(frags) != 3 {
		t.Fatalf("want 3 frags on one line, got %d", len(frags))
	}
	if frags[0].Y != frags[1].Y || frags[1].Y != frags[2].Y {
		t.Errorf("wrap should degrade to nowrap (one line, same Y); got %v %v %v", frags[0].Y, frags[1].Y, frags[2].Y)
	}
	if frags[2].X != 200 { // 0,100,200 — overflows the 150 viewport (nowrap)
		t.Errorf("third item should overflow at x200; got %v", frags[2].X)
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

func TestFlexBaselineApproximatesFlexStart(t *testing.T) {
	// align-items:baseline (deferred) must not panic; approximate to flex-start (y0).
	f := alignFrags(t, "baseline", "auto")
	if f[0].Y != 0 {
		t.Errorf("baseline approximates flex-start (y0); got %v", f[0].Y)
	}
}
