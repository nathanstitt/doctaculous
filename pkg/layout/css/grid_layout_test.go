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

func TestGridFrColumns(t *testing.T) {
	// 1fr 1fr at viewport 200 => two equal columns of 100px each.
	cols := gcss.TrackListOfFr(1, 1)
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 200, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 100, 0, 100, 40)
}

// pxTrackSize builds a fixed-length (minmax(px,px)) track for composing mixed track
// lists via gcss.TrackListOf in tests. frTrackSize builds a bare-flex (min auto / max
// Nfr) track. minmaxTrackSize builds a minmax(<px>, <fr>) track.
func pxTrackSize(px float64) gcss.TrackSize {
	fn := gcss.SizingFn{Kind: gcss.TrackLength, Len: gcss.Length{Value: px, Unit: gcss.UnitPx}}
	return gcss.TrackSize{Min: fn, Max: fn}
}

func frTrackSize(fr float64) gcss.TrackSize {
	return gcss.TrackSize{Min: gcss.SizingFn{Kind: gcss.TrackAuto}, Max: gcss.SizingFn{Kind: gcss.TrackFlex, Fr: fr}}
}

func minmaxTrackSize(minPx, fr float64) gcss.TrackSize {
	return gcss.TrackSize{
		Min: gcss.SizingFn{Kind: gcss.TrackLength, Len: gcss.Length{Value: minPx, Unit: gcss.UnitPx}},
		Max: gcss.SizingFn{Kind: gcss.TrackFlex, Fr: fr},
	}
}

func TestGridMixedFixedAndFr(t *testing.T) {
	// 100px 1fr at viewport 300 => col0=100, col1=200.
	cols := gcss.TrackListOf(pxTrackSize(100), frTrackSize(1))
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 300, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 100, 0, 200, 40)
}

func TestGridColumnGap(t *testing.T) {
	// 1fr 1fr with column-gap:20px at viewport 220 =>
	// available for fr = 220-20 = 200 => each fr track = 100.
	// Col positions: 0, 100+20=120.
	cols := gcss.TrackListOfFr(1, 1)
	rowsTL := gcss.TrackListOfPx(40)
	st := gcss.ComputedStyle{
		ColumnGap: gcss.Length{Value: 20, Unit: gcss.UnitPx},
	}
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 220, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 120, 0, 100, 40)
}

func TestGridMinmax(t *testing.T) {
	// minmax(50px, 1fr) single column at viewport 200 => width 200
	// (min=50px fixed floor, max=1fr grows to fill).
	cols := gcss.TrackListOf(minmaxTrackSize(50, 1))
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 200, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 200, 40)
}

func TestGridPercentColumns(t *testing.T) {
	// 50% 50% at viewport 200 => each column 100px.
	cols := gcss.TrackListOfPercent(50, 50)
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 200, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 100, 0, 100, 40)
}

func TestGridSpanningItem(t *testing.T) {
	// 3-column 100px 100px 100px grid; an item with grid-column: 1/3 (span 2 columns).
	// Width = 100+100 = 200 at x=0 (no gap).
	cols := gcss.TrackListOfPx(100, 100, 100)
	rowsTL := gcss.TrackListOfPx(40)
	p := gcss.GridPlacement{
		ColStart: numLine(1), ColEnd: numLine(3),
	}
	items := []*cssbox.Box{
		gridItemBox(0, 0, p),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 200, 40)
}

func TestGridSpanningItemWithGap(t *testing.T) {
	// 3-column 100px 100px 100px grid with column-gap:10px; span 2 item.
	// Width = 100+10+100 = 210 at x=0.
	cols := gcss.TrackListOfPx(100, 100, 100)
	rowsTL := gcss.TrackListOfPx(40)
	p := gcss.GridPlacement{
		ColStart: numLine(1), ColEnd: numLine(3),
	}
	st := gcss.ComputedStyle{
		ColumnGap: gcss.Length{Value: 10, Unit: gcss.UnitPx},
	}
	items := []*cssbox.Box{
		gridItemBox(0, 0, p),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 210, 40)
}

func TestGridAutoRowSizedToContent(t *testing.T) {
	// 1-column grid, grid-template-rows: auto, item with height:50px.
	// The auto row should size to the item's content height (50px).
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfAuto(1)
	items := []*cssbox.Box{
		gridItemBox(0, 50, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 200, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// Item should be 100px wide (column track) and 50px tall (auto row sized to content).
	assertRect(t, frags[0], 0, 0, 100, 50)
}

// --- Task 9: item-level alignment tests ---

// gridItemBoxWithStyle builds a grid item with a full ComputedStyle (useful for setting
// alignment self-values and other per-item properties).
func gridItemBoxWithStyle(st gcss.ComputedStyle) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	if st.MaxWidth == (gcss.Length{}) {
		st.MaxWidth = auto
	}
	if st.MaxHeight == (gcss.Length{}) {
		st.MaxHeight = auto
	}
	if st.AlignSelf == "" {
		st.AlignSelf = "auto"
	}
	if st.JustifySelf == "" {
		st.JustifySelf = "auto"
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: st}
}

func TestGridJustifyItemsCenter(t *testing.T) {
	// 200px column, item with width:50px, justify-items:center.
	// Area width = 200. Item width = 50. x = (200-50)/2 = 75.
	cols := gcss.TrackListOfPx(200)
	rowsTL := gcss.TrackListOfPx(80)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	item := gridItemBoxWithStyle(gcss.ComputedStyle{
		Width:    gcss.Length{Value: 50, Unit: gcss.UnitPx},
		Height:   auto,
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	})
	st := gcss.ComputedStyle{JustifyItems: "center"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// x=75, w=50; y=0, h=80 (stretch default on block axis, item has no definite height)
	assertRect(t, frags[0], 75, 0, 50, 80)
}

func TestGridJustifyItemsEnd(t *testing.T) {
	// 200px column, item with width:50px, justify-items:end.
	// Area width = 200. Item width = 50. x = 200-50 = 150.
	cols := gcss.TrackListOfPx(200)
	rowsTL := gcss.TrackListOfPx(80)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	item := gridItemBoxWithStyle(gcss.ComputedStyle{
		Width:    gcss.Length{Value: 50, Unit: gcss.UnitPx},
		Height:   auto,
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	})
	st := gcss.ComputedStyle{JustifyItems: "end"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// x=150, w=50; y=0, h=80 (block axis stretch)
	assertRect(t, frags[0], 150, 0, 50, 80)
}

func TestGridAlignItemsCenter(t *testing.T) {
	// 100px row, item with height:40px, align-items:center.
	// Area height = 100. Item height = 40. y = (100-40)/2 = 30.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfPx(100)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	item := gridItemBoxWithStyle(gcss.ComputedStyle{
		Width:    auto,
		Height:   gcss.Length{Value: 40, Unit: gcss.UnitPx},
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	})
	st := gcss.ComputedStyle{AlignItems: "center"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// x=0, w=100 (inline stretch); y=30, h=40
	assertRect(t, frags[0], 0, 30, 100, 40)
}

func TestGridAlignItemsEnd(t *testing.T) {
	// 100px row, item with height:40px, align-items:end.
	// Area height = 100. Item height = 40. y = 100-40 = 60.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfPx(100)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	item := gridItemBoxWithStyle(gcss.ComputedStyle{
		Width:    auto,
		Height:   gcss.Length{Value: 40, Unit: gcss.UnitPx},
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	})
	st := gcss.ComputedStyle{AlignItems: "end"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// x=0, w=100 (inline stretch); y=60, h=40
	assertRect(t, frags[0], 0, 60, 100, 40)
}

func TestGridAlignSelfOverridesAlignItems(t *testing.T) {
	// container align-items:start, item align-self:end, 100px row, item h:40 => y=60.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfPx(100)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	item := gridItemBoxWithStyle(gcss.ComputedStyle{
		Width:     auto,
		Height:    gcss.Length{Value: 40, Unit: gcss.UnitPx},
		AlignSelf: "end",
		MinWidth:  gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	})
	// container align-items: start
	st := gcss.ComputedStyle{AlignItems: "start"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// align-self:end overrides align-items:start => y=60, h=40
	assertRect(t, frags[0], 0, 60, 100, 40)
}

func TestGridStretchDefault(t *testing.T) {
	// Auto item in a 100×50 cell with default stretch on both axes => fills it.
	// Already covered by TestGridTwoByTwoFixed (2x2, stretch default) — mirror it here
	// explicitly to anchor the regression guard.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfPx(50)
	item := gridItemBox(0, 0, gcss.GridPlacement{}) // auto w/h, no placement
	st := gcss.ComputedStyle{}                      // defaults to stretch/stretch
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 50)
}

func TestGridJustifyItemsStartShrinkToFit(t *testing.T) {
	// 200px column, auto-width item containing an inline-block child with width:30px.
	// justify-items:start => item shrinks to its max-content (30px), positioned at x=0.
	// Derivation: area width=200, max-content=30 => relayout at 30 => x=0, w=30.
	cols := gcss.TrackListOfPx(200)
	rowsTL := gcss.TrackListOfPx(60)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	// An inline-block child with an explicit width:30px gives the item a deterministic
	// max-content of 30px (the inline-block atom is the widest thing in the item).
	ibStyle := gcss.ComputedStyle{
		Width: gcss.Length{Value: 30, Unit: gcss.UnitPx}, Height: auto,
		MaxWidth: auto, MaxHeight: auto,
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
	}
	ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayInlineBlock,
		Formatting: cssbox.BlockFC, Style: ibStyle}
	// The item itself: auto width, contains the inline-block as a child.
	itemStyle := gcss.ComputedStyle{
		Width: auto, Height: auto,
		MaxWidth: auto, MaxHeight: auto,
		MinWidth: gcss.Length{Unit: gcss.UnitPx}, MinHeight: gcss.Length{Unit: gcss.UnitPx},
		AlignSelf: "auto", JustifySelf: "auto",
	}
	// Item has BlockFC containing an inline-block: it needs InlineFC to measure the
	// inline-block's width as max-content. Use an IFC paragraph containing the inline-block.
	itemPara := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC, Style: itemStyle, Children: []*cssbox.Box{ib}}
	// The grid item wraps the paragraph.
	item := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: itemStyle, Children: []*cssbox.Box{itemPara}}

	st := gcss.ComputedStyle{JustifyItems: "start"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	f := frags[0]
	// x=0 (start alignment); w should be the max-content width (~30px), NOT 200.
	if f.X > 0.01 {
		t.Errorf("x=%v want 0 (start alignment)", f.X)
	}
	if f.W > 100 {
		t.Errorf("w=%v want ~30 (shrink-to-fit, not 200)", f.W)
	}
}
