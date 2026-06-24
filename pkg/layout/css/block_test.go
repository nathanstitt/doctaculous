package css

import (
	"context"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// --- unit tests for the box-model helpers ---

func TestResolveLen(t *testing.T) {
	mk := func(v float64, u gcss.LengthUnit) gcss.Length { return gcss.Length{Value: v, Unit: u} }
	cases := []struct {
		name      string
		l         gcss.Length
		fs, basis float64
		wantVal   float64
		wantAuto  bool
	}{
		{"px 1:1 pt", mk(12, gcss.UnitPx), 16, 1000, 12, false},
		{"pt", mk(14, gcss.UnitPt), 16, 1000, 14, false},
		{"em scales by font size", mk(2, gcss.UnitEm), 16, 1000, 32, false},
		{"percent of basis", mk(50, gcss.UnitPercent), 16, 1000, 500, false},
		{"auto -> 0, isAuto", mk(0, gcss.UnitAuto), 16, 1000, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			val, isAuto := resolveLen(c.l, c.fs, c.basis)
			if val != c.wantVal || isAuto != c.wantAuto {
				t.Errorf("resolveLen = (%v,%v), want (%v,%v)", val, isAuto, c.wantVal, c.wantAuto)
			}
		})
	}
}

func TestMapBorderStyle(t *testing.T) {
	cases := map[string]layout.BorderStyle{
		"solid":  layout.BorderSolid,
		"dashed": layout.BorderDashed,
		"dotted": layout.BorderDotted,
		"double": layout.BorderDouble,
		"none":   layout.BorderNone,
		"":       layout.BorderNone,
		"groove": layout.BorderNone, // unsupported -> none
	}
	for in, want := range cases {
		if got := mapBorderStyle(in); got != want {
			t.Errorf("mapBorderStyle(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCollapseMargins(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"two equal positive -> that value", []float64{20, 20}, 20},
		{"positives -> max", []float64{10, 30, 20}, 30},
		{"mixed -> maxPos + minNeg", []float64{20, -10}, 10},
		{"mixed bigger negative", []float64{20, -30}, -10},
		{"only negatives -> most negative", []float64{-5, -10}, -10},
		{"empty -> 0", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collapseMargins(c.in...); got != c.want {
				t.Errorf("collapseMargins(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestBorderStyleNoneYieldsZeroUsedWidth(t *testing.T) {
	// A declared 5px border with style:none contributes no used width.
	frag := layoutOne(t, `<div style="border-top-width:5px;border-top-style:none">x</div>`, 1000)
	if frag.Border[layout.EdgeTop].Width != 0 {
		t.Errorf("border-style:none should give 0 used width, got %v", frag.Border[layout.EdgeTop].Width)
	}
}

// --- box-geometry fixtures ---
//
// NOTE: pkg/css does not yet expand CSS shorthands (border / padding / margin /
// background); applyDeclaration only recognizes longhand properties. These
// fixtures therefore author every box-model value as a longhand so the cascade
// actually records it. The block engine consumes the resolved ComputedStyle and is
// indifferent to how a value was authored, so this exercises it identically; when
// shorthand expansion lands in pkg/css the fixtures could be tightened.

// reset zeroes the default margins of every element via longhand margin-* (the
// shorthand `*{margin:0}` would be dropped by the cascade today). Several fixtures
// depend on body/html contributing no margin so border-box numbers are exact.
const reset = `<style>*{margin-top:0;margin-right:0;margin-bottom:0;margin-left:0}</style>`

// border4 expands a uniform solid border of the given width (points) to longhands.
func border4(width int) string {
	w := itoa(width)
	return "border-top-width:" + w + "px;border-right-width:" + w + "px;border-bottom-width:" + w + "px;border-left-width:" + w + "px;" +
		"border-top-style:solid;border-right-style:solid;border-bottom-style:solid;border-left-style:solid;" +
		"border-top-color:black;border-right-color:black;border-bottom-color:black;border-left-color:black;"
}

// pad4 expands uniform padding of the given width (points) to longhands.
func pad4(width int) string {
	w := itoa(width)
	return "padding-top:" + w + "px;padding-right:" + w + "px;padding-bottom:" + w + "px;padding-left:" + w + "px;"
}

// marginV expands a vertical margin (top+bottom) with zero horizontal margins.
func marginV(v int) string {
	s := itoa(v)
	return "margin-top:" + s + "px;margin-bottom:" + s + "px;margin-left:0;margin-right:0;"
}

func itoa(n int) string { return strconv.Itoa(n) }

// Fixture 1: auto width fills the containing block.
func TestAutoWidthFillsContainingBlock(t *testing.T) {
	// Zero body/html margins so the div fills the full viewport. Body has no UA
	// margin, but be explicit so the numbers are unambiguous.
	div := layoutOne(t, reset+`<div></div>`, 1000)
	if div.W != 1000 {
		t.Errorf("auto-width div border-box W = %v, want 1000", div.W)
	}
	if div.X != 0 {
		t.Errorf("div X = %v, want 0", div.X)
	}
}

// Fixture 2: padding + border grow the border box; content offsets by left
// border+padding.
func TestPaddingBorderGrowBorderBox(t *testing.T) {
	body := layoutBody(t, reset+`<div style="width:100px;`+pad4(10)+border4(5)+`"><p>hi</p></div>`, 1000)
	div := body.Children[0]
	// content-box default: width is the content box.
	if div.W != 130 { // 100 + 2*10 padding + 2*5 border
		t.Errorf("border-box W = %v, want 130", div.W)
	}
	// The inner p (block child) sits at content-box left = bodyContentX + mL(0) + bL(5) + pL(10).
	p := div.Children[0]
	if p.X != div.X+15 {
		t.Errorf("inner p X = %v, want div.X+15 (border 5 + padding 10) = %v", p.X, div.X+15)
	}
}

// Fixture 3: box-sizing:border-box makes width the border box.
func TestBoxSizingBorderBox(t *testing.T) {
	div := layoutOne(t, reset+`<div style="width:130px;box-sizing:border-box;`+pad4(10)+border4(5)+`"></div>`, 1000)
	if div.W != 130 {
		t.Errorf("border-box-sized W = %v, want 130", div.W)
	}
	// content width = 130 - 20 - 10 = 100; border-box left padding/border still 15.
	// Verify via a child's content origin would be div.X+15 (no child here), so check
	// the border edge widths reflect the box.
	if div.Border[layout.EdgeLeft].Width != 5 {
		t.Errorf("left border = %v, want 5", div.Border[layout.EdgeLeft].Width)
	}
}

// Fixture 4: percentage width resolves against the containing block width.
func TestPercentWidth(t *testing.T) {
	body := layoutBody(t, reset+`<div style="width:1000px"><div style="width:50%"></div></div>`, 1000)
	outer := body.Children[0]
	inner := outer.Children[0]
	if outer.W != 1000 {
		t.Fatalf("outer W = %v, want 1000", outer.W)
	}
	if inner.W != 500 {
		t.Errorf("50%% child border-box W = %v, want 500", inner.W)
	}
}

// Fixture 5: em padding scales by the element's font size.
func TestEmPadding(t *testing.T) {
	body := layoutBody(t, reset+`<div style="font-size:16px;padding-top:1em;padding-right:1em;padding-bottom:1em;padding-left:1em"><p>x</p></div>`, 1000)
	div := body.Children[0]
	p := div.Children[0]
	// 1em padding == 16pt; content-left offset by left padding (no border).
	if p.X != div.X+16 {
		t.Errorf("em padding: inner p X = %v, want div.X+16 = %v", p.X, div.X+16)
	}
}

// Fixture 6: min/max-width clamp the resolved width.
func TestMinMaxWidthClamp(t *testing.T) {
	// max clamps a 50% (=500) down to 300.
	maxed := layoutBody(t, reset+`<div style="width:1000px"><div style="width:50%;max-width:300px"></div></div>`, 1000)
	if w := maxed.Children[0].Children[0].W; w != 300 {
		t.Errorf("max-width clamp W = %v, want 300", w)
	}
	// min raises a 100px up to 400.
	mined := layoutOne(t, reset+`<div style="width:100px;min-width:400px"></div>`, 1000)
	if mined.W != 400 {
		t.Errorf("min-width clamp W = %v, want 400", mined.W)
	}
}

// Fixture 7: adjacent sibling margins collapse (two 20px -> 20 gap, not 40).
func TestAdjacentSiblingMarginCollapse(t *testing.T) {
	body := layoutBody(t, reset+`<div><div style="`+marginV(20)+`height:10px"></div><div style="`+marginV(20)+`height:10px"></div></div>`, 1000)
	wrap := body.Children[0]
	a := wrap.Children[0]
	b := wrap.Children[1]
	gap := b.Y - (a.Y + a.H)
	if gap != 20 {
		t.Errorf("sibling margin gap = %v, want 20 (collapsed, not 40)", gap)
	}
	if a.H != 10 || b.H != 10 {
		t.Errorf("fixed heights = %v,%v, want 10,10", a.H, b.H)
	}
}

// Fixture 8: parent/first-child top margins collapse through zero top
// border/padding (the child's margin passes through to move the parent's edge).
func TestParentFirstChildMarginCollapse(t *testing.T) {
	// The wrap has no top border/padding, so the child's 30px top margin collapses
	// with the wrap's (0) top margin. The child sits flush with the wrap's content
	// top -> child.Y == wrap.Y, and the wrap's margin edge moved down by 30.
	body := layoutBody(t, reset+`<div id="wrap"><div style="margin-top:30px;height:10px"></div></div>`, 1000)
	wrap := body.Children[0]
	child := wrap.Children[0]
	if child.Y != wrap.Y {
		t.Errorf("collapse-through: child.Y = %v, wrap.Y = %v; want equal (margin passed through)", child.Y, wrap.Y)
	}
	// The wrap's top margin edge is 30 below body's content top. body content top is
	// 0 (zero margins), so wrap.Y should be 30 (the collapsed 30 moved the wrap).
	if wrap.Y != 30 {
		t.Errorf("collapse-through: wrap.Y = %v, want 30 (single collapsed margin, not 0+30 stacked inside)", wrap.Y)
	}
}

// TestInlineBlockSuppressesMarginCollapse verifies an inline-block establishes a new
// block formatting context: its child's top margin does NOT collapse through it (the
// child's margin is interior), in contrast to the plain-block collapse-through of
// fixture 8. display:inline-block has zero top border/padding here, so without the
// BFC guard the child margin would pass through.
func TestInlineBlockSuppressesMarginCollapse(t *testing.T) {
	body := layoutBody(t, reset+`<div id="ib" style="display:inline-block;width:100px"><div style="margin-top:30px;height:10px"></div></div>`, 1000)
	ib := body.Children[0]
	child := ib.Children[0]
	// The child's 30px top margin is interior: it sits 30 below the inline-block's
	// content top (== border top, no border/padding), not flush with it.
	if child.Y != ib.Y+30 {
		t.Errorf("inline-block child.Y = %v, want ib.Y+30 = %v (margin not collapsed through new BFC)", child.Y, ib.Y+30)
	}
	// The inline-block's own border box height includes the interior 30 + child 10.
	if ib.H != 40 {
		t.Errorf("inline-block H = %v, want 40 (30 interior margin + 10 child)", ib.H)
	}
}

// Fixture 9: borders/background land on the fragment and flatten to items.
func TestBordersAndBackgroundOnFragment(t *testing.T) {
	gray := color.RGBA{128, 128, 128, 255}
	black := color.RGBA{0, 0, 0, 255}
	div := layoutOne(t, reset+`<div style="background-color:gray;`+border4(10)+`height:20px"></div>`, 1000)
	if div.Background != gray {
		t.Errorf("background = %v, want gray %v", div.Background, gray)
	}
	top := div.Border[layout.EdgeTop]
	if top.Width != 10 || top.Style != layout.BorderSolid || top.Color != black {
		t.Errorf("top border = %+v, want width 10 solid black", top)
	}
	// Flatten: 1 background + 4 border edges.
	items := div.AppendItems(nil)
	var bg, borders int
	for _, it := range items {
		switch it.Kind {
		case layout.BackgroundKind:
			bg++
		case layout.BorderKind:
			borders++
		}
	}
	if bg != 1 || borders != 4 {
		t.Errorf("flatten = %d bg + %d borders, want 1 + 4", bg, borders)
	}
}

// Fixture 10: flex/grid/table fall back to block normal flow (with a logged
// message), still stacking children vertically.
func TestUnsupportedFCFallsBackToBlock(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	logf := func(format string, args ...any) {
		mu.Lock()
		logs = append(logs, format)
		mu.Unlock()
	}
	body := layoutBodyWithLog(t, reset+`<div style="display:flex"><div style="height:10px"></div><div style="height:15px"></div></div>`, 1000, logf)
	flex := body.Children[0]
	if len(flex.Children) != 2 {
		t.Fatalf("flex fallback children = %d, want 2", len(flex.Children))
	}
	a, b := flex.Children[0], flex.Children[1]
	// Stacked vertically: second begins at first's bottom (margins are zero).
	if a.Y != flex.Y {
		t.Errorf("first child Y = %v, want flex content top %v", a.Y, flex.Y)
	}
	if b.Y != a.Y+a.H {
		t.Errorf("second child Y = %v, want stacked below first (%v)", b.Y, a.Y+a.H)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "not yet implemented; falling back to block normal flow") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a fallback log message, got %v", logs)
	}
}

// TestLayoutPageHeightIsContentBottom verifies the single-tall-page output sizes the
// page to the viewport width and the root's content bottom.
func TestLayoutPageHeightIsContentBottom(t *testing.T) {
	doc, err := html.Parse([]byte(reset + `<div style="height:50px"></div>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	pages, err := New(nil, nil, nil).Layout(context.Background(), root, 1000)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if len(pages.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages.Pages))
	}
	p := pages.Pages[0]
	if p.WidthPt != 1000 {
		t.Errorf("page width = %v, want 1000", p.WidthPt)
	}
	if p.HeightPt != 50 {
		t.Errorf("page height = %v, want 50 (content bottom)", p.HeightPt)
	}
}

// TestLayoutNilRootEmptyPage verifies a nil/degenerate root yields a single empty
// page rather than an error or panic.
func TestLayoutNilRootEmptyPage(t *testing.T) {
	pages, err := New(nil, nil, nil).Layout(context.Background(), nil, 800)
	if err != nil {
		t.Fatalf("Layout(nil): %v", err)
	}
	if len(pages.Pages) != 1 || pages.Pages[0].WidthPt != 800 || pages.Pages[0].HeightPt != 0 {
		t.Errorf("nil root pages = %+v, want one 800x0 page", pages.Pages)
	}
}

// TestLayoutHonorsCancellation verifies a cancelled context stops adding children.
func TestLayoutHonorsCancellation(t *testing.T) {
	doc, err := html.Parse([]byte(`<div></div><div></div>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Must not panic; returns a valid (possibly empty-bodied) page.
	if _, err := New(nil, nil, nil).Layout(ctx, root, 1000); err != nil {
		t.Fatalf("Layout under cancelled ctx: %v", err)
	}
}

// TestEndToEndBlockFlow runs a realistic document (headings, paragraphs, mixed
// inline+block content, a replaced img) through parse -> box-gen -> layout, proving
// the block engine produces a single sane page without panicking and stacks the
// body's block children in document order with non-decreasing top edges. Inline
// content has no height yet (the IFC is Task 6), so siblings separated only by
// inline content may share a Y; the assertion is monotonic, not strict.
func TestEndToEndBlockFlow(t *testing.T) {
	src := `<!doctype html><html><body>
		<h1>Title</h1>
		<p>Hello <em>world</em>, this is text.</p>
		<div>before<p>nested</p>after</div>
		<img src="pic.png" alt="pic">
	</body></html>`
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	frag := New(nil, nil, nil).layoutTree(context.Background(), root, 800)
	if frag == nil {
		t.Fatal("layoutTree returned nil")
	}
	body := bodyOf(t, frag)
	if len(body.Children) != 4 {
		t.Fatalf("body has %d block children, want 4 (h1, p, div, img-wrap)", len(body.Children))
	}
	// h1, p, div, and the anon-block wrapping <img> all fill the 800pt width (auto).
	for i, c := range body.Children {
		if c.W != 800 {
			t.Errorf("body child %d W = %v, want 800 (auto fills viewport)", i, c.W)
		}
	}
	// Block children stack top-to-bottom: each child's top edge is >= the previous
	// child's bottom edge (margins collapse but never reorder).
	for i := 1; i < len(body.Children); i++ {
		prev := body.Children[i-1]
		cur := body.Children[i]
		if cur.Y < prev.Y {
			t.Errorf("child %d Y = %v is above child %d Y = %v (out of flow order)", i, cur.Y, i-1, prev.Y)
		}
	}
	// The whole tree flattens without panicking and yields a page sized to content.
	pages, err := New(nil, nil, nil).Layout(context.Background(), root, 800)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if len(pages.Pages) != 1 || pages.Pages[0].WidthPt != 800 {
		t.Errorf("pages = %+v, want one 800-wide page", pages.Pages)
	}
}

// --- harness ---

// layoutTreeFor parses HTML, builds the box tree, and lays it out, returning the
// root fragment for geometry assertions.
func layoutTreeFor(t *testing.T, src string, viewportW float64, logf func(string, ...any)) *Fragment {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, logf)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	frag := New(nil, nil, logf).layoutTree(context.Background(), root, viewportW)
	if frag == nil {
		t.Fatalf("layoutTree returned nil for %q", src)
	}
	return frag
}

// layoutBody returns the <body> fragment (root is <html>, body is its sole child
// for these fixtures).
func layoutBody(t *testing.T, src string, viewportW float64) *Fragment {
	t.Helper()
	return bodyOf(t, layoutTreeFor(t, src, viewportW, nil))
}

// layoutBodyWithLog is layoutBody with a capturing logf.
func layoutBodyWithLog(t *testing.T, src string, viewportW float64, logf func(string, ...any)) *Fragment {
	t.Helper()
	return bodyOf(t, layoutTreeFor(t, src, viewportW, logf))
}

// layoutOne returns the first block fragment inside <body> — the single <div> the
// fixture places there.
func layoutOne(t *testing.T, src string, viewportW float64) *Fragment {
	t.Helper()
	body := bodyOf(t, layoutTreeFor(t, src, viewportW, nil))
	if len(body.Children) == 0 {
		t.Fatalf("body has no children for %q", src)
	}
	return body.Children[0]
}

// bodyOf returns the body fragment given the html-root fragment. x/net/html always
// synthesizes <html><body>; the body is the root's last block child (a leading
// <style> is display:none and not emitted, so body is the sole child).
func bodyOf(t *testing.T, root *Fragment) *Fragment {
	t.Helper()
	if len(root.Children) == 0 {
		t.Fatalf("html fragment has no children (expected body)")
	}
	return root.Children[len(root.Children)-1]
}
