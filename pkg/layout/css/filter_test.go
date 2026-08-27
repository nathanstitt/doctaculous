package css

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/filtereffects"
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// bracketBalance walks an item list and reports the running filter-bracket depth's
// final value plus whether it ever went negative (a pop with no matching push). A
// well-formed page ends at depth 0 and never dips below it — the invariant a page
// break must not be allowed to break.
func bracketBalance(items []layout.Item) (final int, wentNegative bool) {
	depth := 0
	for i := range items {
		switch items[i].Kind {
		case layout.FilterPushKind:
			depth++
		case layout.FilterPopKind:
			depth--
			if depth < 0 {
				wentNegative = true
			}
		}
	}
	return depth, wentNegative
}

// assertBalanced fails when any page's filter brackets are unbalanced. This is the
// assertion the whole feature turns on: an unbalanced Save/Restore-style bracket is
// INVISIBLE in a golden image until it corrupts a later page, so it must be checked
// on the emitted item list directly.
func assertBalanced(t *testing.T, pages []layout.Page) {
	t.Helper()
	for i := range pages {
		final, neg := bracketBalance(pages[i].Items)
		if final != 0 {
			t.Errorf("page %d ends at filter-bracket depth %d, want 0 (unbalanced push/pop)", i, final)
		}
		if neg {
			t.Errorf("page %d has a FilterPop with no matching FilterPush", i)
		}
	}
}

// TestFilterCascadesRaw: the `filter` declaration reaches the fragment as a parsed
// chain, and `none` / an unparseable value leave the fragment unfiltered.
func TestFilterCascadesRaw(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  int // expected chain length; 0 = unfiltered
	}{
		{"grayscale", "grayscale(1)", 1},
		{"two functions", "grayscale(0.5) blur(2px)", 2},
		{"none", "none", 0},
		{"empty", "", 0},
		// CSS error handling: an invalid entry invalidates the WHOLE declaration, so
		// the valid grayscale beside it must NOT survive.
		{"one bad entry kills the list", "grayscale(1) hue-rotate(random)", 0},
		{"bare token", "grayscale", 0},
		{"unbalanced parens", "blur(2px", 0},
		{"negative amount", "grayscale(-1)", 0},
		// blur() takes a <length>; a percentage is not one.
		{"percentage blur", "blur(10%)", 0},
		{"unitless nonzero blur", "blur(3)", 0},
		{"unitless zero blur", "blur(0)", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf(`<html><body style="margin:0">`+
				`<div style="height:20px;filter:%s">x</div></body></html>`, tc.value)
			frag := layoutTreeFor(t, src, 200, nil)
			var found *Fragment
			var walk func(f *Fragment)
			walk = func(f *Fragment) {
				if len(f.Filter) > 0 && found == nil {
					found = f
				}
				for _, c := range f.Children {
					walk(c)
				}
			}
			walk(frag)
			got := 0
			if found != nil {
				got = len(found.Filter)
			}
			if got != tc.want {
				t.Errorf("filter:%s → chain of %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// TestFilterNotInherited: a filter on a parent must NOT reach its children. Inheriting
// it would re-apply the effect at every descendant, compounding it.
func TestFilterNotInherited(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="height:40px;filter:grayscale(1)">` +
		`<div style="height:20px" id="kid">x</div>` +
		`</div></body></html>`
	frag := layoutTreeFor(t, src, 200, nil)
	filtered := 0
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if len(f.Filter) > 0 {
			filtered++
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if filtered != 1 {
		t.Errorf("%d fragments carry a filter, want exactly 1 (filter must not inherit)", filtered)
	}
}

// TestFilterBracketsOwnDecorations: unlike the clip bracket — which deliberately
// EXCLUDES the box's own border box — a filter bracket wraps the box's own background
// and border too, because a CSS filter applies to the element's whole rendering.
func TestFilterBracketsOwnDecorations(t *testing.T) {
	f := &Fragment{
		X: 0, Y: 0, W: 100, H: 50,
		Background: color.RGBA{1, 1, 1, 255},
		IsBFC:      true,
		Filter:     []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}},
	}
	items := f.AppendItems(nil)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (push, own bg, pop)", len(items))
	}
	if items[0].Kind != layout.FilterPushKind {
		t.Errorf("item[0] = %v, want FilterPushKind", items[0].Kind)
	}
	if items[1].Kind != layout.BackgroundKind {
		t.Errorf("item[1] = %v, want the box's OWN background INSIDE the bracket", items[1].Kind)
	}
	if items[2].Kind != layout.FilterPopKind {
		t.Errorf("item[2] = %v, want FilterPopKind", items[2].Kind)
	}
}

// TestFilterPushCarriesChainAndRect: the push item carries the parsed chain and the
// box's border box, which the paint stage needs to size the offscreen surface.
func TestFilterPushCarriesChainAndRect(t *testing.T) {
	chain := []filtereffects.Function{{Kind: filtereffects.FuncBlur, StdDeviation: 3}}
	f := &Fragment{X: 5, Y: 6, W: 90, H: 38, IsBFC: true, Filter: chain}
	items := f.AppendItems(nil)
	if len(items) == 0 || items[0].Kind != layout.FilterPushKind {
		t.Fatal("no FilterPushKind emitted")
	}
	fi := items[0].Filter
	if fi.XPt != 5 || fi.YPt != 6 || fi.WPt != 90 || fi.HPt != 38 {
		t.Errorf("filter rect = (%v,%v,%v,%v), want (5,6,90,38)", fi.XPt, fi.YPt, fi.WPt, fi.HPt)
	}
	if len(fi.Funcs) != 1 || fi.Funcs[0].Kind != filtereffects.FuncBlur {
		t.Errorf("filter chain = %+v, want one blur", fi.Funcs)
	}
	if !fi.Spatial() {
		t.Error("blur chain reported non-spatial")
	}
}

// TestFilterItemSpatial pins the per-pixel / spatial split the pagination log depends
// on: only blur and drop-shadow sample neighbouring pixels.
func TestFilterItemSpatial(t *testing.T) {
	for _, tc := range []struct {
		kind filtereffects.FunctionKind
		want bool
	}{
		{filtereffects.FuncBlur, true},
		{filtereffects.FuncDropShadow, true},
		{filtereffects.FuncGrayscale, false},
		{filtereffects.FuncInvert, false},
		{filtereffects.FuncSepia, false},
		{filtereffects.FuncSaturate, false},
		{filtereffects.FuncHueRotate, false},
		{filtereffects.FuncBrightness, false},
		{filtereffects.FuncContrast, false},
		{filtereffects.FuncOpacity, false},
	} {
		fi := layout.FilterItem{Funcs: []filtereffects.Function{{Kind: tc.kind}}}
		if got := fi.Spatial(); got != tc.want {
			t.Errorf("kind %v Spatial() = %v, want %v", tc.kind, got, tc.want)
		}
	}
	if (&layout.FilterItem{}).Spatial() {
		t.Error("an empty chain must not be spatial")
	}
}

// TestFilterNestingBrackets: a filtered box inside a filtered box nests its brackets
// (inner push strictly between the outer push and the outer pop).
func TestFilterNestingBrackets(t *testing.T) {
	gray := []filtereffects.Function{{Kind: filtereffects.FuncGrayscale, Amount: 1}}
	inner := &Fragment{X: 10, Y: 10, W: 50, H: 20, IsBFC: true, Filter: gray,
		Background: color.RGBA{3, 3, 3, 255}}
	outer := &Fragment{X: 0, Y: 0, W: 100, H: 50, IsBFC: true, Filter: gray,
		Background: color.RGBA{2, 2, 2, 255}, Children: []*Fragment{inner}}
	items := outer.AppendItems(nil)

	var pushes, pops []int
	for i := range items {
		switch items[i].Kind {
		case layout.FilterPushKind:
			pushes = append(pushes, i)
		case layout.FilterPopKind:
			pops = append(pops, i)
		}
	}
	if len(pushes) != 2 || len(pops) != 2 {
		t.Fatalf("got %d pushes / %d pops, want 2/2 (nested filters)", len(pushes), len(pops))
	}
	if pushes[0] >= pushes[1] || pushes[1] >= pops[0] || pops[0] >= pops[1] {
		t.Errorf("brackets not nested: pushes=%v pops=%v", pushes, pops)
	}
	if final, neg := bracketBalance(items); final != 0 || neg {
		t.Errorf("nested brackets unbalanced: final=%d negative=%v", final, neg)
	}
}

// TestFilterUnfilteredEmitsNothing is the byte-identical regression guard for every
// document that uses no filter: not one filter item may appear.
func TestFilterUnfilteredEmitsNothing(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="height:20px;background-color:rgb(1,2,3)">a</div>` +
		`<div style="height:20px;overflow:hidden">b</div>` +
		`</body></html>`
	frag := layoutTreeFor(t, src, 200, nil)
	page := frag.Page(200, frag.Y+frag.H)
	if n := countKind(page.Items, layout.FilterPushKind) + countKind(page.Items, layout.FilterPopKind); n != 0 {
		t.Errorf("an unfiltered document emitted %d filter items, want 0", n)
	}
}

// TestFilterNoneAndInvalidAreByteIdentical: `filter: none` and an unparseable value
// must produce EXACTLY the same item stream as no declaration at all. This is the
// regression guard that keeps every existing golden from moving.
func TestFilterNoneAndInvalidAreByteIdentical(t *testing.T) {
	const w = 240
	body := `<div style="height:30px;background-color:rgb(9,8,7);%s">hello</div>` +
		`<div style="height:30px;%s">world</div>`
	render := func(decl string) []layout.Item {
		src := `<html><body style="margin:0">` +
			fmt.Sprintf(body, decl, decl) + `</body></html>`
		frag := layoutTreeFor(t, src, w, nil)
		return frag.Page(w, frag.Y+frag.H).Items
	}
	base := render("")
	for _, decl := range []string{
		"filter:none",
		"filter:NONE",
		"filter:grayscale(1) hue-rotate(nonsense)", // invalid as a whole
		"filter:not-a-function(3)",
		"filter:blur(10%)", // a percentage is not a <length>
	} {
		got := render(decl)
		if len(got) != len(base) {
			t.Errorf("%s: %d items, want %d (identical to no declaration)", decl, len(got), len(base))
			continue
		}
		for i := range got {
			if got[i].Kind != base[i].Kind {
				t.Errorf("%s: item[%d] kind = %v, want %v", decl, i, got[i].Kind, base[i].Kind)
				break
			}
		}
	}
}

// TestFilterBalancedAcrossPageBreak is THE test this task exists for. A filtered box
// split across a page break must produce a BALANCED push/pop pair on EVERY page it
// lands on — never a push on one page and its pop on the next.
//
// Mutation-verify: move the bracket emission out of AppendItems into a per-page
// wrapper that only brackets the first page, and page 1's depth check fails.
func TestFilterBalancedAcrossPageBreak(t *testing.T) {
	const w = 300
	// A filtered block far taller than the page, made of many short lines so the
	// line splitter can genuinely fragment it (rather than overflowing whole).
	var lines strings.Builder
	for i := 0; i < 60; i++ {
		lines.WriteString("<p style=\"margin:0;height:12px\">line</p>")
	}
	src := `<html><body style="margin:0">` +
		`<div style="filter:grayscale(1);background-color:rgb(9,9,9)">` + lines.String() + `</div>` +
		`</body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 100)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	// 60 * 12pt = 720pt of content in 100pt pages: the box must split MORE THAN ONCE.
	if len(pages.Pages) < 3 {
		t.Fatalf("expected the box to split across at least 3 pages, got %d", len(pages.Pages))
	}
	assertBalanced(t, pages.Pages)

	// Every page carrying part of the box must actually carry a bracket — a page-local
	// bracket model that silently drops filtering from continuation pages would still
	// be "balanced" (zero pushes, zero pops), so balance alone is not enough.
	withBrackets := 0
	for i := range pages.Pages {
		if countKind(pages.Pages[i].Items, layout.FilterPushKind) > 0 {
			withBrackets++
		}
	}
	if withBrackets != len(pages.Pages) {
		t.Errorf("%d of %d pages carry a filter bracket; every page's slice must be filtered",
			withBrackets, len(pages.Pages))
	}
}

// TestFilterNestedBalancedAcrossPageBreak: a filtered box inside a filtered box, both
// split by pagination, must nest correctly on every page.
func TestFilterNestedBalancedAcrossPageBreak(t *testing.T) {
	const w = 300
	var lines strings.Builder
	for i := 0; i < 40; i++ {
		lines.WriteString("<p style=\"margin:0;height:12px\">line</p>")
	}
	src := `<html><body style="margin:0">` +
		`<div style="filter:invert(1)">` +
		`<div style="filter:sepia(1)">` + lines.String() + `</div>` +
		`</div></body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 100)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) < 3 {
		t.Fatalf("expected at least 3 pages, got %d", len(pages.Pages))
	}
	assertBalanced(t, pages.Pages)
	for i := range pages.Pages {
		items := pages.Pages[i].Items
		pushes := countKind(items, layout.FilterPushKind)
		pops := countKind(items, layout.FilterPopKind)
		if pushes != pops {
			t.Errorf("page %d: %d pushes vs %d pops", i, pushes, pops)
		}
		if pushes < 2 {
			t.Errorf("page %d: %d pushes, want 2 (outer + inner filter both present)", i, pushes)
		}
	}
}

// TestFilterSpatialSplitLogs / the per-pixel counterpart pin the ONE approximation
// page-local bracketing carries: a blur cannot sample content that fell on the other
// page, so a split blur is logged — and a split grayscale, which is exact, is NOT
// (a warning on every grayscale() would be pure noise).
func TestFilterSpatialSplitLogs(t *testing.T) {
	splitLog := func(decl string) []string {
		var got []string
		logf := func(f string, a ...any) { got = append(got, fmt.Sprintf(f, a...)) }
		var lines strings.Builder
		for i := 0; i < 40; i++ {
			lines.WriteString("<p style=\"margin:0;height:12px\">line</p>")
		}
		src := `<html><body style="margin:0">` +
			`<div style="` + decl + `">` + lines.String() + `</div></body></html>`
		root := buildRoot(t, src, logf)
		if _, err := New(nil, nil, logf).LayoutPaged(context.Background(), root, 300, 100); err != nil {
			t.Fatalf("LayoutPaged: %v", err)
		}
		return got
	}
	hasSpatialWarning := func(msgs []string) bool {
		for _, m := range msgs {
			if strings.Contains(m, "spatial CSS filter") {
				return true
			}
		}
		return false
	}

	if !hasSpatialWarning(splitLog("filter:blur(2px)")) {
		t.Error("a split blur() did not log the page-local approximation")
	}
	if !hasSpatialWarning(splitLog("filter:drop-shadow(2px 2px 3px black)")) {
		t.Error("a split drop-shadow() did not log the page-local approximation")
	}
	if hasSpatialWarning(splitLog("filter:grayscale(1)")) {
		t.Error("a split grayscale() logged; a per-pixel filter splits EXACTLY and must stay silent")
	}
	if hasSpatialWarning(splitLog("filter:invert(1) saturate(2) hue-rotate(90deg)")) {
		t.Error("a split per-pixel chain logged; it splits exactly and must stay silent")
	}
	// The spatial warning is emitted once per document, not once per split.
	msgs := splitLog("filter:blur(2px)")
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, "spatial CSS filter") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("spatial split logged %d times, want exactly 1", n)
	}
}

// TestFilterEstablishesBFC: a filtered box must establish a block formatting context,
// which is what makes its whole rendering flatten through ONE AppendItems call and
// therefore sit inside one balanced bracket. Without it the box's background would be
// emitted in its ancestor's decoration phase and its text in the content phase, with
// no single range to bracket.
func TestFilterEstablishesBFC(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="height:30px;filter:sepia(1);background-color:rgb(4,5,6)">x</div>` +
		`</body></html>`
	frag := layoutTreeFor(t, src, 200, nil)
	var target *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if len(f.Filter) > 0 && target == nil {
			target = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(frag)
	if target == nil {
		t.Fatal("no filtered fragment found")
	}
	if !target.IsBFC {
		t.Error("a filtered box must establish a BFC (see establishesNewBFC)")
	}
}

// TestFilterRectFollowsRelativeOffset: a position:relative filtered box's paint-time
// offset must move the filter region with the content it filters, exactly as it moves
// a ClipPush rect. Mutation-verify: drop the FilterPushKind case in translateItems and
// the rect stays at the un-offset Y.
func TestFilterRectFollowsRelativeOffset(t *testing.T) {
	const w = 240
	src := `<html><body style="margin:0">` +
		`<div style="height:40px;position:relative;top:25px;filter:grayscale(1)">x</div>` +
		`</body></html>`
	frag := layoutTreeFor(t, src, w, nil)
	page := frag.Page(w, frag.Y+frag.H)
	var push *layout.Item
	for i := range page.Items {
		if page.Items[i].Kind == layout.FilterPushKind {
			push = &page.Items[i]
			break
		}
	}
	if push == nil {
		t.Fatal("no FilterPushKind emitted for the relative filtered box")
	}
	if push.Filter.YPt < 20 {
		t.Errorf("filter rect at Y=%.1f, want ~25 (the relative top offset)", push.Filter.YPt)
	}
}

// TestClipBracketsAlsoBalancedAcrossPageBreak records the answer to the question the
// filter design raised about the PRE-EXISTING clip pair: does ClipPushKind/ClipPopKind
// have the same latent page-break problem?
//
// It does NOT, and for the same structural reason the filter pair does not: pagination
// splits the FRAGMENT TREE, not the item list. assemblePages builds a per-page root
// whose body Children are that page's blocks and flattens it through its OWN
// AppendItems call, so a split overflow:hidden box becomes two fragments, each of
// which emits its own balanced pair. The clip pair was never exercised across a page
// break before (its only emission sites bracket leaf content), so this pins the
// property rather than assuming it.
func TestClipBracketsAlsoBalancedAcrossPageBreak(t *testing.T) {
	const w = 300
	var lines strings.Builder
	for i := 0; i < 40; i++ {
		lines.WriteString("<p style=\"margin:0;height:12px\">line</p>")
	}
	src := `<html><body style="margin:0">` +
		`<div style="overflow:hidden">` + lines.String() + `</div></body></html>`

	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPaged(context.Background(), root, w, 100)
	if err != nil {
		t.Fatalf("LayoutPaged: %v", err)
	}
	if len(pages.Pages) < 3 {
		t.Fatalf("expected at least 3 pages, got %d", len(pages.Pages))
	}
	for i := range pages.Pages {
		items := pages.Pages[i].Items
		depth := 0
		for j := range items {
			switch items[j].Kind {
			case layout.ClipPushKind:
				depth++
			case layout.ClipPopKind:
				depth--
				if depth < 0 {
					t.Fatalf("page %d: ClipPop with no matching ClipPush at item %d", i, j)
				}
			}
		}
		if depth != 0 {
			t.Errorf("page %d ends at clip-bracket depth %d, want 0", i, depth)
		}
		if countKind(items, layout.ClipPushKind) == 0 {
			t.Errorf("page %d carries no clip bracket; the split box's overflow clip was dropped", i)
		}
	}
}
