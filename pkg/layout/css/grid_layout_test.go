package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// orAutoLen returns l, or auto when l is the zero-value px length (so a caller can
// leave a width unset and have it fill the containing block like a normal block).
func orAutoLen(l gcss.Length) gcss.Length {
	if l.Unit == gcss.UnitPx && l.Value == 0 {
		return gcss.Length{Unit: gcss.UnitAuto}
	}
	return l
}

// mustTrackList builds a TrackList of fixed-px tracks for the geometry tests.
func mustTrackList(px ...float64) gcss.TrackList {
	return gcss.TrackListOfPx(px...)
}

// assertRect compares a fragment's border-box rect against (x,y,w,h) with a small
// epsilon, reporting the actual rect on mismatch.
func assertRect(t *testing.T, f *Fragment, x, y, w, h float64) {
	t.Helper()
	const eps = 0.01
	if f == nil {
		t.Fatalf("nil fragment; want rect (%v,%v,%v,%v)", x, y, w, h)
	}
	if absf(f.X-x) > eps || absf(f.Y-y) > eps || absf(f.W-w) > eps || absf(f.H-h) > eps {
		t.Errorf("rect = (%v,%v,%v,%v), want (%v,%v,%v,%v)", f.X, f.Y, f.W, f.H, x, y, w, h)
	}
}

// gridItemBox builds a block-level grid item with optional fixed width/height and a
// placement. wPx/hPx <= 0 means auto.
func gridItemBox(wPx, hPx float64, p gcss.GridPlacement) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	w, h := auto, auto
	if wPx > 0 {
		w = gcss.Length{Value: wPx, Unit: gcss.UnitPx}
	}
	if hPx > 0 {
		h = gcss.Length{Value: hPx, Unit: gcss.UnitPx}
	}
	st := gcss.ComputedStyle{
		Width: w, Height: h, MaxWidth: auto, MaxHeight: auto,
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
		AlignSelf: "auto", JustifySelf: "auto", GridPlacement: p,
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: st}
}

// gridContainerTL builds a display:grid box with the given template columns/rows
// (already-parsed TrackLists) and children. width auto fills the containing block.
// (gridContainer — children-only — is the fixup test's helper in gridfix_test.go.)
func gridContainerTL(cols, rowsTL gcss.TrackList, style gcss.ComputedStyle, items ...*cssbox.Box) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	style.Width = orAutoLen(style.Width)
	style.MaxWidth, style.MaxHeight = auto, auto
	style.GridTemplateColumns = cols
	style.GridTemplateRows = rowsTL
	style.GridAutoFlow = orDefault(style.GridAutoFlow, "row")
	style.JustifyItems = orDefault(style.JustifyItems, "stretch")
	style.AlignItems = orDefault(style.AlignItems, "stretch")
	style.AlignContent = orDefault(style.AlignContent, "start")
	style.JustifyContent = orDefault(style.JustifyContent, "start")
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayGrid,
		Formatting: cssbox.GridFC, Style: style, Children: items}
}

// gridFrags lays out a grid container in a body at viewport and returns the item
// fragments. paddingPx wraps the grid in a padded block so the container's content box
// is NOT at page x=0 (the regression guard).
func gridFrags(t *testing.T, container *cssbox.Box, viewport, paddingPx float64) []*Fragment {
	t.Helper()
	e := New(nil, nil, nil)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	wrapStyle := gcss.ComputedStyle{Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto}
	if paddingPx > 0 {
		wrapStyle.PaddingLeft = gcss.Length{Value: paddingPx, Unit: gcss.UnitPx}
		wrapStyle.PaddingTop = gcss.Length{Value: paddingPx, Unit: gcss.UnitPx}
	}
	wrap := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: wrapStyle, Children: []*cssbox.Box{container}}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: wrapStyle, Children: []*cssbox.Box{wrap}}
	root := e.layoutTree(context.Background(), body, viewport)
	var gc *Fragment
	var find func(f *Fragment)
	find = func(f *Fragment) {
		if f == nil || gc != nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayGrid {
			gc = f
			return
		}
		for _, c := range f.Children {
			find(c)
		}
	}
	find(root)
	if gc == nil {
		t.Fatal("no grid container fragment")
	}
	return gc.Children
}

func TestGridTwoByTwoFixed(t *testing.T) {
	// 2x2 grid, columns 100px 100px, rows 50px 50px, four auto-placed items.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50, 50)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 400, 0)
	if len(frags) != 4 {
		t.Fatalf("got %d frags want 4", len(frags))
	}
	// Auto-placement row-flow: (0,0),(1,0),(0,1),(1,1) => rects:
	want := [][4]float64{{0, 0, 100, 50}, {100, 0, 100, 50}, {0, 50, 100, 50}, {100, 50, 100, 50}}
	for i, w := range want {
		assertRect(t, frags[i], w[0], w[1], w[2], w[3])
	}
}

func TestGridFixedNestedUnderPadding(t *testing.T) {
	// Same 2x2 grid but the container's content box is at x=40,y=40 (two padding
	// wrappers). Regression guard: item rects must be offset by the padding, NOT
	// collapsed to x=0.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50, 50)
	items := []*cssbox.Box{gridItemBox(0, 0, gcss.GridPlacement{}), gridItemBox(0, 0, gcss.GridPlacement{})}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 400, 20)
	// body padding 20 + wrap padding 20 => content origin at x=40,y=40.
	assertRect(t, frags[0], 40, 40, 100, 50)
	assertRect(t, frags[1], 140, 40, 100, 50)
}
