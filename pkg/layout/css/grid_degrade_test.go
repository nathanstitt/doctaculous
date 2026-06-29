package css

// grid_degrade_test.go — degradation tests for the CSS Grid layout engine.
//
// Each test covers one documented deferral or degenerate case from the spec's
// "Degradation & error handling" table in
// docs/superpowers/specs/2026-06-26-html-grid-design.md. The test asserts:
//   - no panic (the test itself fails if layoutGrid panics)
//   - the ACTUAL documented fallback geometry (not "an item exists")
//
// Tests live alongside grid_layout_test.go and share its helpers (gridFrags,
// gridContainerTL, gridItemBox, gridTextItem, mustTrackList, assertRect). No new
// helpers needed.

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// --- 1. subgrid ---

// TestGridSubgridDegradesToImplicit covers the documented "subgrid" deferral:
// `grid-template-columns: subgrid` (or any value that parses to ok=false, leaving
// GridTemplateColumns as the zero TrackList) causes the grid to use all-implicit auto
// tracks sized by grid-auto-columns. Items must still place and lay out (no panic).
//
// Mechanism: parseTrackList("subgrid") returns ok=false → the cascade leaves
// GridTemplateColumns empty (IsEmpty() == true) → layoutGrid expands an empty track
// list to zero explicit columns → auto-placement (row flow, the default) with 0
// explicit columns treats the flow extent as 1 (one implicit column), so items stack
// one-per-row into a single implicit column, each row implicit-sized by grid-auto-rows.
//
// We confirm this by setting GridTemplateColumns to the zero TrackList and asserting
// that two auto-placed items each land in a different row (stacked vertically), both
// in the single implicit column at x=0.
func TestGridSubgridDegradesToImplicit(t *testing.T) {
	// Simulate what the cascade produces for "grid-template-columns: subgrid":
	// an empty TrackList (IsEmpty() == true, no explicit tracks).
	//
	// Two items, no explicit columns, no explicit rows.
	// Auto-placement row-flow with 0 explicit columns: flow extent = 1 (one implicit
	// column). Each item gets its own implicit column-1 slot → they stack in rows.
	// We supply grid-auto-rows:40px and grid-auto-columns:100px for determinism.
	autoColTrack := gcss.TrackListOfPx(100).Expand(0) // grid-auto-columns: 100px
	autoRowTrack := gcss.TrackListOfPx(40).Expand(0)  // grid-auto-rows: 40px

	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}

	// GridTemplateColumns is the zero TrackList (empty — simulates "subgrid" deferral).
	// gridContainerTL sets GridTemplateColumns directly, so we pass a zero TrackList.
	// GridTemplateRows is also empty (no explicit rows — all implicit).
	st := gcss.ComputedStyle{
		GridAutoColumns: autoColTrack,
		GridAutoRows:    autoRowTrack,
	}
	var emptyTL gcss.TrackList // IsEmpty() == true (simulates "subgrid" → treated as none)
	frags := gridFrags(t, gridContainerTL(emptyTL, emptyTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("subgrid deferral: got %d item frags, want 2 (items still place)", len(frags))
	}
	// Row-flow with 0 explicit cols: flow extent=1, so items stack vertically:
	// item0 → col[0,1) row[0,1): x=0, y=0, w=100, h=40.
	// item1 → col[0,1) row[1,2): x=0, y=40, w=100, h=40.
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 0, 40, 100, 40)
}

// --- 2. RTL → LTR ---

// TestGridRTLDegradesToLTR covers the documented RTL deferral: direction:rtl on a
// grid acts as LTR (column line 1 remains leftmost). The test asserts the same item
// positions as a LTR grid (NOT mirrored). layoutGrid logs the RTL deferral but the
// test harness does not capture logs, so we assert geometry only.
func TestGridRTLDegradesToLTR(t *testing.T) {
	// 2-column 80px 120px grid (distinct widths so a LTR vs RTL difference is clear):
	// LTR: item0 at x=0 (80px wide), item1 at x=80 (120px wide).
	// RTL (if honored): item0 at x=120, item1 at x=0 (reversed).
	// Documented fallback: acts as LTR → item0 at x=0, item1 at x=80.
	cols := mustTrackList(80, 120)
	rowsTL := mustTrackList(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	st := gcss.ComputedStyle{Direction: "rtl"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("RTL grid: got %d frags, want 2", len(frags))
	}
	// LTR fallback: first item (80px) at x=0, second (120px) at x=80.
	assertRect(t, frags[0], 0, 0, 80, 40)
	assertRect(t, frags[1], 80, 0, 120, 40)
}

// --- 3. repeat(auto-fill) with indefinite container size (row axis) ---

// TestGridAutoFillRowsIndefiniteSizeFallback covers the documented
// repeat(auto-fill) deferral: when the container size on the auto-fill axis is
// indefinite (rowAvail == 0, no definite height on the container), the spec fallback
// is 1 repetition instead of computing a fill count.
//
// Column axis is always definite (contentW), so the definite vs indefinite distinction
// is only observable on the ROW axis. We use grid-template-rows: repeat(auto-fill,50px)
// on a container with no explicit height. The fallback produces exactly 1 explicit row
// of 50px. We place one item and assert it ends up in that single 50px row.
func TestGridAutoFillRowsIndefiniteSizeFallback(t *testing.T) {
	// repeat(auto-fill, 50px) on the row axis, container height = auto (indefinite).
	// Documented fallback: 1 repetition → 1 explicit row of 50px.
	// We use one explicit column (100px) and one item to make the assertion clean.
	cols := mustTrackList(100)
	// gcss.TrackListAutoFill(50) builds a repeat(auto-fill, 50px) track list.
	rowsTL := gcss.TrackListAutoFill(50)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	// No Height on the container → definiteHeight() returns 0 → rowAvail = 0 → indefinite.
	// ExpandGap(0, 0) on a repeat(auto-fill, 50px) list returns 1 track of 50px.
	st := gcss.ComputedStyle{}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("auto-fill indefinite row: got %d item frags, want 1", len(frags))
	}
	// Item placed in col[0,1) row[0,1): x=0, y=0, w=100, h=50 (the one explicit row).
	assertRect(t, frags[0], 0, 0, 100, 50)
}

// TestGridAutoFillColumnsDefiniteSize confirms the non-degenerate (definite) case for
// contrast: repeat(auto-fill, 100px) at a 350px container should yield 3 columns
// (floor(350/100)=3). This is NOT a degradation — it documents the intended behavior
// alongside the indefinite-size fallback test above.
func TestGridAutoFillColumnsDefiniteSize(t *testing.T) {
	// repeat(auto-fill, 100px) at viewport 350px: 3 columns of 100px each.
	// The fourth would require 400px total so it is not added.
	cols := gcss.TrackListAutoFill(100)
	rowsTL := mustTrackList(40)
	items := []*cssbox.Box{
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
		gridItemBox(0, 0, gcss.GridPlacement{}),
	}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}, items...), 350, 0)
	if len(frags) != 3 {
		t.Fatalf("auto-fill definite: got %d item frags, want 3 (floor(350/100)=3 tracks)", len(frags))
	}
	// Three items at x=0, 100, 200, each 100px wide, 40px tall.
	assertRect(t, frags[0], 0, 0, 100, 40)
	assertRect(t, frags[1], 100, 0, 100, 40)
	assertRect(t, frags[2], 200, 0, 100, 40)
}

// --- 4. Malformed grid-template-areas ---

// TestGridMalformedTemplateAreasIgnored covers the documented "malformed
// grid-template-areas" deferral: a ragged or non-rectangular template-areas value
// causes parseTemplateAreas to return ok=false, so the cascade leaves
// GridTemplateAreas as the zero value (no Named areas). Items auto-place (no panic).
//
// For example, "a a" / "a b" is non-rectangular (area 'a' appears in col1 of row1 but
// only col1 of row2 — the shape is not a rectangle). The cascade produces an empty
// GridAreas{}. Items fall through to auto-placement.
//
// We simulate this directly by using an empty GridAreas{} (zero value, no Named map)
// in the ComputedStyle, which is exactly what the cascade produces when
// parseTemplateAreas returns ok=false. Two items placed with AreaName references that
// would normally hit the areas map fall through to auto-placement.
func TestGridMalformedTemplateAreasIgnored(t *testing.T) {
	// GridTemplateAreas = zero value (empty, no Named areas) — simulates a malformed
	// "a a" / "a b" template that the cascade rejects and leaves empty.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50)

	// Items that reference area names — with an empty areas map, they auto-place.
	pA := gcss.GridPlacement{AreaName: "hd"}
	pB := gcss.GridPlacement{AreaName: "main"}
	items := []*cssbox.Box{
		gridItemBox(0, 0, pA),
		gridItemBox(0, 0, pB),
	}
	// Empty GridAreas (no Named map) — exactly what the cascade leaves on rejection.
	st := gcss.ComputedStyle{GridTemplateAreas: gcss.GridAreas{}}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, items...), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("malformed areas: got %d frags, want 2 (items auto-placed)", len(frags))
	}
	// Auto-placement row-flow: item0 → col[0,1) row[0,1), item1 → col[1,2) row[0,1).
	// Both area names are unknown so both fall through to auto-placement.
	assertRect(t, frags[0], 0, 0, 100, 50)
	assertRect(t, frags[1], 100, 0, 100, 50)
}

// --- 5. Empty grid container ---

// TestGridEmptyContainerNoPanic covers the documented "empty / no-in-flow-children
// grid container" deferral: a display:grid with no children produces a zero-size
// interior (contentHeight 0) and no item fragments, without panicking.
//
// layoutGrid returns early with `interior{contentHeight: 0}` when len(items) == 0.
func TestGridEmptyContainerNoPanic(t *testing.T) {
	// A display:grid container with no children at all.
	cols := mustTrackList(100, 100)
	rowsTL := mustTrackList(50)
	gc := gridContainerTL(cols, rowsTL, gcss.ComputedStyle{}) // no items

	e := New(nil, nil, nil)
	auto := gcss.Length{Unit: gcss.UnitAuto}
	bodySt := gcss.ComputedStyle{Width: auto, Height: auto, MaxWidth: auto, MaxHeight: auto}
	body := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC, Style: bodySt, Children: []*cssbox.Box{gc}}

	// Must not panic.
	root := e.layoutTree(context.Background(), body, 400)
	if root == nil {
		t.Fatal("layoutTree returned nil root")
	}

	// Find the grid container fragment.
	var gcFrag *Fragment
	var find func(f *Fragment)
	find = func(f *Fragment) {
		if gcFrag != nil {
			return
		}
		if f != nil && f.Box != nil && f.Box.Display == cssbox.DisplayGrid {
			gcFrag = f
			return
		}
		if f != nil {
			for _, c := range f.Children {
				find(c)
			}
		}
	}
	find(root)
	if gcFrag == nil {
		t.Fatal("no grid container fragment found in layout tree")
	}
	// Empty grid: no item children, zero content height.
	if len(gcFrag.Children) != 0 {
		t.Errorf("empty grid: got %d children, want 0", len(gcFrag.Children))
	}
	if gcFrag.H != 0 {
		t.Errorf("empty grid: H = %v, want 0 (zero-size fragment)", gcFrag.H)
	}
}

// --- 6. baseline alignment on a text-free (baseline-free) item ---

// TestGridBaselineFreeItemFallsBackToStart covers the documented "baseline on a
// text-free item" deferral: align-items:baseline on a grid item that has no text and
// no line boxes (firstBaselineOffset returns ok=false) falls back to start alignment
// (the item sits at the row top, y=areaTop). No panic.
//
// alignBaselineGroup skips items where firstBaselineOffset returns ok=false (they do
// not contribute to the group max baseline and are not shifted). The net effect is
// that a text-free item with baseline alignment lands at the same Y as a start-aligned
// item — the row top.
//
// NOTE: when align-items is "baseline", the item does NOT get block-axis stretch (that
// is the default for align-items:"stretch", not for "baseline"). An empty box with
// "baseline" alignment and no content has a natural height of 0 and lands at y=0 (row
// top). This is the correct documented fallback: the item is at start (y=0, h=0) not
// shifted down at all.
func TestGridBaselineFreeItemFallsBackToStart(t *testing.T) {
	// 1-row 100px grid. One item: empty box (no children, no text), align-items:baseline.
	// Expected: item is not shifted (ok=false → not in baseline group → stays at start).
	// Block axis: "baseline" → falls back to start (y = row top = 0).
	// Inline axis: "stretch" (default from JustifyItems in gridContainerTL).
	// Natural height of the empty item = 0 (no content, no definite height).
	// So the item lands at y=0, h=0 (start fallback with zero content height).
	cols := mustTrackList(100)
	rowsTL := mustTrackList(100)

	// An empty item with no children (no text, no inline content) — baseline-free.
	auto := gcss.Length{Unit: gcss.UnitAuto}
	emptyItem := &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{
			Width:    auto,
			Height:   auto,
			MaxWidth: auto, MaxHeight: auto,
			MinWidth:  gcss.Length{Unit: gcss.UnitPx},
			MinHeight: gcss.Length{Unit: gcss.UnitPx},
			AlignSelf: "auto", JustifySelf: "auto",
		},
	}
	st := gcss.ComputedStyle{AlignItems: "baseline"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, emptyItem), 400, 0)
	if len(frags) != 1 {
		t.Fatalf("baseline-free item: got %d frags, want 1", len(frags))
	}
	// Fallback to start: item at row top (y=0), width=100 (inline stretch from JustifyItems
	// default "stretch"), height=0 (natural height of an empty box). NOT shifted down.
	assertRect(t, frags[0], 0, 0, 100, 0)
	// Verify the fragment has no Lines (confirms it is truly baseline-free).
	if len(frags[0].Lines) != 0 {
		t.Errorf("baseline-free item should have no Lines; got %d", len(frags[0].Lines))
	}
}

// TestGridBaselineFreeVsTextItemNoShift confirms the baseline-free fallback for a
// MIXED row: one text item + one text-free item, align-items:baseline. The text item
// gets shifted to align its baseline with the group max; the text-free item is NOT
// shifted (ok=false excludes it from the group and it stays at the row top).
func TestGridBaselineFreeVsTextItemNoShift(t *testing.T) {
	// 2-column row: left item = text (has a baseline), right item = empty (no baseline).
	// align-items:baseline. Row height = auto (sizes to tallest item after alignment).
	//
	// Expected:
	//   left (text): gets baseline-aligned (may be shifted down if large font).
	//   right (empty): NOT shifted — stays at y = left_item_Y (both start from the same
	//     row-local origin, which after baseline adjustment equals the row top).
	//
	// We assert that the text-free item's Y <= the text item's Y + some slack — i.e. the
	// text-free item is NOT pushed down past the text item. Specifically, the text-free
	// item is at the row top (y stays at what the row origin is), while the text item may
	// be shifted down by the baseline group. So frags[1].Y <= frags[0].Y (text-free item
	// cannot be below the text item that was shifted down by baseline alignment).
	cols := mustTrackList(150, 150)
	rowsTL := gcss.TrackListOfAuto(1)

	textItem := gridTextItem("Hi", 16) // has Lines → firstBaselineOffset ok=true
	auto := gcss.Length{Unit: gcss.UnitAuto}
	emptyItem := &cssbox.Box{
		Kind:       cssbox.BoxBlock,
		Display:    cssbox.DisplayBlock,
		Formatting: cssbox.BlockFC,
		Style: gcss.ComputedStyle{
			Width:    auto,
			Height:   auto,
			MaxWidth: auto, MaxHeight: auto,
			MinWidth:  gcss.Length{Unit: gcss.UnitPx},
			MinHeight: gcss.Length{Unit: gcss.UnitPx},
			AlignSelf: "auto", JustifySelf: "auto",
		},
	}

	st := gcss.ComputedStyle{AlignItems: "baseline"}
	frags := gridFrags(t, gridContainerTL(cols, rowsTL, st, textItem, emptyItem), 400, 0)
	if len(frags) != 2 {
		t.Fatalf("got %d frags, want 2", len(frags))
	}

	// The empty item (frags[1]) is at the row top (y = frags[0].Y or lower).
	// It must NOT be shifted below the text item (frags[0]) that WAS baseline-shifted.
	// Concretely: empty item Y <= text item Y (empty stays at top; text item may move down).
	if frags[1].Y > frags[0].Y+0.01 {
		t.Errorf("baseline-free item should NOT be shifted below the text item; "+
			"empty item Y=%v, text item Y=%v (text was shifted down by baseline group)",
			frags[1].Y, frags[0].Y)
	}
	// Verify the empty item has no Lines (confirms it is truly baseline-free).
	if len(frags[1].Lines) != 0 {
		t.Errorf("empty item should have no Lines; got %d", len(frags[1].Lines))
	}
}

// --- 7. Unknown property value (cascade degrade, covered by cascade tests) ---

// NOTE: the "unknown property value → keeps default" deferral is a CASCADE concern
// (applyOne silently drops an unrecognized justify-items/align-* value, leaving the
// prior/default), not a layout concern — the engine never receives a bogus value in
// normal operation. It is tested where it actually lives, in
// pkg/css/grid_cascade_test.go (TestGridUnknownAlignmentValueKeepsDefault), rather than
// asserted indirectly through layoutGrid here.
