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
	// x=0 (start alignment), w=30 (max-content from the 30px inline-block child),
	// h=60 (row height, block axis stretch default), y=0.
	assertRect(t, frags[0], 0, 0, 30, 60)
}

func TestGridDefiniteWidthAutoHeightStretchAlign(t *testing.T) {
	// Regression test for the Phase 5b block-stretch relayout proxy bug.
	//
	// Bug summary: the condition `math.Abs(itemUsedW-itemW[i]) > 0.01` was used as a
	// proxy for "the inline branch already relaid out the item at itemUsedW". For a
	// DEFINITE-width item with non-stretch inline alignment (e.g. justify-items:center),
	// the inline branch is NOT entered (guarded by gridItemHasAutoWidth). After reading
	// frag.W (e.g. 50), itemUsedW=50 ≠ itemW[i]=200, so the proxy returned true and the
	// block-stretch path skipped the relayout — leaving the item's content laid out at
	// the area width (200) from Phase 5a instead of relaying at the definite width (50).
	//
	// Why the bug is not unit-observable via child-width assertions: layoutBlock respects
	// the item's CSS `width:50px` regardless of the `cbWidth` (containing-block width)
	// parameter passed to layoutGridItem. So `layoutGridItem(ctx, it, 200)` and
	// `layoutGridItem(ctx, it, 50)` produce the same item content — a child's `width:100%`
	// resolves against the item's CONTENT box (50px), not against `cbWidth`. Therefore
	// the geometry is identical with or without the fix for this simple case. The fix is
	// structural: it removes the incorrect proxy and uses an explicit relaidOutInline flag,
	// guaranteeing the block-stretch relayout occurs even for definite-width items.
	//
	// This test asserts the border-box geometry (x=75,w=50,y=0,h=100) as a regression
	// guard. The fix itself is verified by inspecting the code path, not by geometry
	// alone, since `layoutGridItem(ctx, it, 200)` and `layoutGridItem(ctx, it, 50)` are
	// equivalent for a definite-width item.
	cols := gcss.TrackListOfPx(200)
	rowsTL := gcss.TrackListOfPx(100)
	auto := gcss.Length{Unit: gcss.UnitAuto}

	// Grid item: definite width:50px, auto height, no children (geometry check only).
	itemStyle := gcss.ComputedStyle{
		Width:    gcss.Length{Value: 50, Unit: gcss.UnitPx},
		Height:   auto,
		MaxWidth: auto, MaxHeight: auto,
		MinWidth:  gcss.Length{Unit: gcss.UnitPx},
		MinHeight: gcss.Length{Unit: gcss.UnitPx},
		AlignSelf: "auto", JustifySelf: "auto",
	}
	item := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: itemStyle}

	// justify-items:center (non-stretch inline), align-items:stretch (default).
	st := gcss.ComputedStyle{JustifyItems: "center"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, item), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// Item border-box: width=50 (definite), centered in 200px column => x=(200-50)/2=75,
	// height=100 (block axis stretched to row), y=0.
	assertRect(t, frags[0], 75, 0, 50, 100)
}

// --- Task 10: content-distribution alignment tests ---

func TestGridJustifyContentCenter(t *testing.T) {
	// 3 columns 50px 50px 50px (total 150px) in a 300px container.
	// justify-content: center => leftover=150, leading=75, between=0.
	// colPos: 75, 125, 175.
	// Each item: width=50, height=40 (row track).
	// x offsets (no padding): 75, 125, 175.
	cols := gcss.TrackListOfPx(50, 50, 50)
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{JustifyContent: "center"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 300, 0)
	if len(frags) != 3 {
		t.Fatalf("got %d frags want 3", len(frags))
	}
	assertRect(t, frags[0], 75, 0, 50, 40)
	assertRect(t, frags[1], 125, 0, 50, 40)
	assertRect(t, frags[2], 175, 0, 50, 40)
}

func TestGridJustifyContentSpaceBetween(t *testing.T) {
	// 3 columns 50px 50px 50px (total 150px) in a 300px container.
	// justify-content: space-between => leftover=150, leading=0, between=75.
	// colPos: 0, 0+50+75=125, 125+50+75=250.
	cols := gcss.TrackListOfPx(50, 50, 50)
	rowsTL := gcss.TrackListOfPx(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{JustifyContent: "space-between"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 300, 0)
	if len(frags) != 3 {
		t.Fatalf("got %d frags want 3", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 50, 40)
	assertRect(t, frags[1], 125, 0, 50, 40)
	assertRect(t, frags[2], 250, 0, 50, 40)
}

func TestGridAlignContentEnd(t *testing.T) {
	// 2 rows 40px 40px (total 80px) in a 200px-tall container.
	// align-content: end => leftover=120, leading=120, between=0.
	// rowPos: 120, 160.
	// Two items (one per row), each width=100 (column), height=40.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfPx(40, 40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{
		AlignContent: "end",
		Height:       gcss.Length{Value: 200, Unit: gcss.UnitPx},
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	assertRect(t, frags[0], 0, 120, 100, 40)
	assertRect(t, frags[1], 0, 160, 100, 40)
}

func TestGridAlignContentStretch(t *testing.T) {
	// 2 auto rows in a 200px-tall container; items have auto height (no content).
	// resolveTrackSizes: each auto row sizes to 0 (no content) => total 0px.
	// align-content: stretch => leftover=200, 2 auto-max rows, each gets +100 => row 100px.
	// rowPos: 0, 100.
	// Items: auto height => align-items:stretch (default) fills the 100px row => h=100.
	cols := gcss.TrackListOfPx(100)
	rowsTL := gcss.TrackListOfAuto(2)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}), // auto height, no content
		gridItemBox(0, 0, gcss.GridPlacement{}), // auto height, no content
	}
	st := gcss.ComputedStyle{
		AlignContent: "stretch",
		Height:       gcss.Length{Value: 200, Unit: gcss.UnitPx},
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	// Each row is stretched to 100px; auto-height items fill the row.
	assertRect(t, frags[0], 0, 0, 100, 100)
	assertRect(t, frags[1], 0, 100, 100, 100)
}

// --- Task 11: placement variety end-to-end geometry tests ---

func TestGridAutoSparseRowFlow(t *testing.T) {
	// 2-column 100px 100px grid, 1 explicit row of 50px, grid-auto-rows: 50px.
	// Three auto items: auto row-flow places item0 at (col0,row0), item1 at (col1,row0),
	// item2 wraps to an implicit row (sized by grid-auto-rows: 50px) at (col0,row1).
	// Derivation:
	//   colPos: [0, 100]; rowPos: [0, 50] (1 explicit) + implicit [50] => [0, 50].
	//   item0: col[0,1) row[0,1) => x=0, y=0, w=100, h=50.
	//   item1: col[1,2) row[0,1) => x=100, y=0, w=100, h=50.
	//   item2: col[0,1) row[1,2) => x=0, y=50, w=100, h=50.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50) // 1 explicit row
	// grid-auto-rows: 50px — set as []TrackSize so the implicit row is deterministic.
	autoRowTrack := gcss.TrackListOfPx(50).Expand(0)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{GridAutoRows: autoRowTrack}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 3 {
		t.Fatalf("got %d frags want 3", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 50)
	assertRect(t, frags[1], 100, 0, 100, 50)
	assertRect(t, frags[2], 0, 50, 100, 50) // implicit row at y=50
}

func TestGridColumnFlow(t *testing.T) {
	// grid-auto-flow: column, 1 explicit column (100px), 2 explicit rows (50px 50px).
	// Three auto items with column flow:
	//   item0 -> col[0,1) row[0,1) => x=0, y=0, w=100, h=50.
	//   item1 -> col[0,1) row[1,2) => x=0, y=50, w=100, h=50.
	//   item2 -> implicit col[1,2) row[0,1) => x=100, y=0, w=100, h=50.
	// grid-auto-columns: 100px so the implicit column is deterministic.
	cols := mustTrackList(100)
	rowsTL := mustTrackList(50, 50)
	autoColTrack := gcss.TrackListOfPx(100).Expand(0)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{
		GridAutoFlow:    "column",
		GridAutoColumns: autoColTrack,
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 3 {
		t.Fatalf("got %d frags want 3", len(frags))
	}
	assertRect(t, frags[0], 0, 0, 100, 50)
	assertRect(t, frags[1], 0, 50, 100, 50)
	assertRect(t, frags[2], 100, 0, 100, 50) // implicit column: x=100, y=0
}

func TestGridDenseBackfill(t *testing.T) {
	// 2-column 100px 100px grid, 1 explicit row of 50px.
	// Item A: definite position at col-line 2/3, row-line 1/2 => col[1,2) row[0,1).
	// Item B: auto with grid-auto-flow: row dense.
	// With dense: cursor resets to (0,0) per auto item.
	//   Item B: (0,0) is free => placed at col[0,1) row[0,1).
	// Derivation:
	//   colPos: [0, 100]; rowPos: [0].
	//   item A: x=100, y=0, w=100, h=50 (col1, row0).
	//   item B: x=0, y=0, w=100, h=50 (col0, row0 — backfilled).
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50)
	// Item A: explicitly at col 2 (line 2→3), row 1 (line 1→2). Both axes definite.
	pA := gcss.GridPlacement{
		ColStart: numLine(2), ColEnd: numLine(3),
		RowStart: numLine(1), RowEnd: numLine(2),
	}
	// Item B: fully auto, relies on dense backfill.
	pB := gcss.GridPlacement{}
	items := []*cssbox.Box{
		gridItemBox(0, 50, pA), // fixed height so row sizes to 50
		gridItemBox(0, 0, pB),
	}
	st := gcss.ComputedStyle{GridAutoFlow: "row dense"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags want 2", len(frags))
	}
	// Item A at col 1 (x=100).
	assertRect(t, frags[0], 100, 0, 100, 50)
	// Item B dense-backfilled to top-left (x=0).
	assertRect(t, frags[1], 0, 0, 100, 50)
}

func TestGridNamedAreas(t *testing.T) {
	// grid-template-areas: "hd hd" "main side"
	// grid-template-columns: 100px 100px, grid-template-rows: 40px 60px.
	// Items with grid-area: hd, main, side.
	// GridRect is 1-based inclusive:
	//   hd   => GridRect{RowStart:1, RowEnd:1, ColStart:1, ColEnd:2} => x=0, y=0, w=200, h=40.
	//   main => GridRect{RowStart:2, RowEnd:2, ColStart:1, ColEnd:1} => x=0, y=40, w=100, h=60.
	//   side => GridRect{RowStart:2, RowEnd:2, ColStart:2, ColEnd:2} => x=100, y=40, w=100, h=60.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(40, 60)
	areas := gcss.GridAreas{
		Named: map[string]gcss.GridRect{
			"hd":   {RowStart: 1, RowEnd: 1, ColStart: 1, ColEnd: 2},
			"main": {RowStart: 2, RowEnd: 2, ColStart: 1, ColEnd: 1},
			"side": {RowStart: 2, RowEnd: 2, ColStart: 2, ColEnd: 2},
		},
		Rows: 2, Cols: 2,
	}
	pHD := gcss.GridPlacement{AreaName: "hd"}
	pMain := gcss.GridPlacement{AreaName: "main"}
	pSide := gcss.GridPlacement{AreaName: "side"}
	items := []*cssbox.Box{
		gridItemBox(0, 0, pHD),
		gridItemBox(0, 0, pMain),
		gridItemBox(0, 0, pSide),
	}
	st := gcss.ComputedStyle{GridTemplateAreas: areas}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 3 {
		t.Fatalf("got %d frags want 3", len(frags))
	}
	// hd spans both columns: x=0, y=0, w=200, h=40.
	assertRect(t, frags[0], 0, 0, 200, 40)
	// main: left column, second row: x=0, y=40, w=100, h=60.
	assertRect(t, frags[1], 0, 40, 100, 60)
	// side: right column, second row: x=100, y=40, w=100, h=60.
	assertRect(t, frags[2], 100, 40, 100, 60)
}

// inlineGridContainerTL builds a display:inline-grid box with the given column/row
// track lists. Mirrors gridContainerTL but sets DisplayInlineGrid so the box is an
// inline-level atom in its parent's inline formatting context.
func inlineGridContainerTL(cols, rowsTL gcss.TrackList, items ...*cssbox.Box) *cssbox.Box {
	auto := gcss.Length{Unit: gcss.UnitAuto}
	st := gcss.ComputedStyle{
		Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto,
		GridTemplateColumns: cols,
		GridTemplateRows:    rowsTL,
		GridAutoFlow:        "row",
		JustifyItems:        "stretch",
		AlignItems:          "stretch",
		AlignContent:        "start",
		JustifyContent:      "start",
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayInlineGrid,
		Formatting: cssbox.GridFC, Style: st, Children: items}
}

// TestInlineGridSideBySide proves that two display:inline-grid containers
// flow side by side on the same line (not stacked) and that each container's
// border-box width equals the sum of its column tracks (shrink-to-fit = 100, not the
// containing-block fill of 300). Grid: 2 columns × 50px = 100px per container.
//
//	Container 1: x=0, w=100
//	Container 2: x=100, w=100
//
// If anon.go or inline.go did not handle DisplayInlineGrid, the containers would stack
// (both y=0 but x would be undefined / both at 0). If shrink-to-fit were missing,
// each container's border-box would be 300px (the viewport) and the second would
// overflow the line.
func TestInlineGridSideBySide(t *testing.T) {
	cols := mustTrackList(50, 50) // 2 columns × 50px = 100px track sum
	rowsTL := mustTrackList(30)   // 1 explicit row of 30px
	mk := func() *cssbox.Box {    // auto-placed item (fills stretch)
		return gridItemBox(0, 0, gcss.GridPlacement{})
	}
	igA := inlineGridContainerTL(cols, rowsTL, mk())
	igB := inlineGridContainerTL(cols, rowsTL, mk())

	e := New(nil, nil, nil)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	bodySt := gcss.ComputedStyle{Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto}
	// body is a block container whose children are all inline-level (two inline-grid boxes);
	// set Formatting: InlineFC so layoutInterior runs gatherInlineRuns → the containers flow
	// as inline atoms side by side. (In the full HTML path normalize/reconcileFormatting would
	// derive InlineFC from the children; here we build the box tree directly.)
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.InlineFC, Style: bodySt, Children: []*cssbox.Box{igA, igB}}
	root := e.layoutTree(context.Background(), body, 300)

	// Collect the two inline-grid container fragments.
	var grids []*Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f == nil {
			return
		}
		if f.Box != nil && f.Box.Display == cssbox.DisplayInlineGrid {
			grids = append(grids, f)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)

	if len(grids) != 2 {
		t.Fatalf("want 2 inline-grid container fragments, got %d", len(grids))
	}
	// Sort by X so walk order doesn't matter.
	if grids[0].X > grids[1].X {
		grids[0], grids[1] = grids[1], grids[0]
	}
	// Both containers must share the same Y (same line).
	if absf(grids[0].Y-grids[1].Y) > 0.01 {
		t.Errorf("inline-grid containers should share Y (same line); got y=%v and y=%v", grids[0].Y, grids[1].Y)
	}
	// Containers must be side by side: X of second == width of first.
	if absf(grids[0].X-0) > 0.01 {
		t.Errorf("first inline-grid X = %v, want 0", grids[0].X)
	}
	// Shrink-to-fit: width = track sum = 2×50 = 100, NOT the 300px viewport.
	if absf(grids[0].W-100) > 0.01 {
		t.Errorf("first inline-grid W = %v, want 100 (track sum, not container fill)", grids[0].W)
	}
	if absf(grids[1].X-100) > 0.01 {
		t.Errorf("second inline-grid X = %v, want 100 (== first container width)", grids[1].X)
	}
	if absf(grids[1].W-100) > 0.01 {
		t.Errorf("second inline-grid W = %v, want 100", grids[1].W)
	}
}

func TestGridMissingAreaFallsToAutoPlacement(t *testing.T) {
	// grid-area: name for a name NOT in grid-template-areas => falls through to
	// auto-placement (not dropped, not panicking). The item lands in the first free cell.
	// 2-column 100px 100px grid, 1 explicit row of 50px; one item with a missing area name.
	// Auto-placement (row flow): first cell is col[0,1) row[0,1) => x=0, y=0, w=100, h=50.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50)
	areas := gcss.GridAreas{
		Named: map[string]gcss.GridRect{
			"known": {RowStart: 1, RowEnd: 1, ColStart: 1, ColEnd: 1},
		},
		Rows: 1, Cols: 2,
	}
	// "unknown" is not in the areas map — should degrade to auto-placement.
	pUnknown := gcss.GridPlacement{AreaName: "unknown"}
	items := []*cssbox.Box{
		gridItemBox(0, 0, pUnknown),
	}
	st := gcss.ComputedStyle{GridTemplateAreas: areas}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("got %d frags want 1", len(frags))
	}
	// Auto-placed at the first free cell: col[0,1) row[0,1) => x=0, y=0, w=100, h=50.
	assertRect(t, frags[0], 0, 0, 100, 50)
}
