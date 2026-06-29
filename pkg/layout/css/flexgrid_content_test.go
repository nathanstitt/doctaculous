package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
)

// layoutHTML parses src, builds the cssbox tree, lays it out at width w, and returns
// the root fragment. A small helper for the flex/grid content regression tests.
func layoutHTML(t *testing.T, src string, w float64) *Fragment {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root, err := Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return New(nil, nil, nil).layoutTree(context.Background(), root, w)
}

// contentBottom returns the lowest Y+H over fragments that have painted content
// (text lines or a control), i.e. the visible content height of the tree.
func contentBottom(f *Fragment) float64 {
	var max float64
	var walk func(g *Fragment)
	walk = func(g *Fragment) {
		if g == nil {
			return
		}
		if (len(g.Lines) > 0 || g.Control != nil) && g.Y+g.H > max {
			max = g.Y + g.H
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk(f)
	return max
}

// TestInlineTextInFlexGridItemRenders is a regression test for two bugs that made a
// flex/grid item wrapping inline content (text or a <span>) render at zero size:
// (1) the anonymous flex/grid item was created with BlockFC so its inline text never
// laid out, and (2) the anonymous item carried a zero-value Style, whose zero Width
// read as "width:0" (it was not recognized by isAnonymous). Both are fixed; text in a
// flex/grid item must now produce real height.
func TestInlineTextInFlexGridItemRenders(t *testing.T) {
	cases := map[string]string{
		"flex": `<body style="margin:0"><div style="display:flex">` +
			`<span>Hello</span><span>World</span></div></body>`,
		"grid": `<body style="margin:0"><div style="display:grid;grid-template-columns:1fr 1fr">` +
			`<span>Hello</span><span>World</span></div></body>`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if h := contentBottom(layoutHTML(t, src, 400)); h <= 0 {
				t.Errorf("%s text item height = %.1f, want > 0 (item collapsed)", name, h)
			}
		})
	}
}

// TestGridFormRowsPlaceInColumns is a regression test for the normalize fix: a
// flex/grid container must NOT run the generic block-level inline-run wrap on its
// children (that coalesced a row's label <span> AND its adjacent control into one
// anonymous block, corrupting the item-to-cell mapping). Here a 2-column grid of
// alternating label/control rows must place each label in column 1 and each control
// in column 2 (distinct X), with a grid-column:1/3 spanning item below them.
func TestGridFormRowsPlaceInColumns(t *testing.T) {
	src := `<head><style>
	.g{display:grid;grid-template-columns:110px 1fr;gap:14px}
	.a{grid-column:1 / 3}
	</style></head><body style="margin:0"><div class="g">` +
		`<span>L1</span><input type="text" value="x">` +
		`<span>L2</span><input type="text" value="y">` +
		`<div class="a">ACTIONS</div>` +
		`</div></body>`
	frag := layoutHTML(t, src, 400)
	// Expect 5 placed content items: 2 labels (col1, X=0), 2 controls (col2, X>=110),
	// 1 spanning ACTIONS (col1, X=0, full width).
	var labelXs, ctrlXs []float64
	var walk func(g *Fragment)
	walk = func(g *Fragment) {
		if g == nil {
			return
		}
		if g.Control != nil {
			ctrlXs = append(ctrlXs, g.X)
		} else if len(g.Lines) > 0 {
			labelXs = append(labelXs, g.X)
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk(frag)
	if len(ctrlXs) != 2 {
		t.Fatalf("got %d controls, want 2 (the inputs were coalesced/dropped): %v", len(ctrlXs), ctrlXs)
	}
	// Both controls must be in column 2 (X well past the 110px label column), proving
	// they are their own grid items in the field column — not nested in a label item.
	for _, x := range ctrlXs {
		if x < 110 {
			t.Errorf("control X = %.1f, want >= 110 (in the field column, not column 1)", x)
		}
	}
	// Labels must be in column 1 (X 0).
	for _, x := range labelXs {
		if x > 1 {
			t.Errorf("label X = %.1f, want ~0 (in the label column)", x)
		}
	}
}

// TestGridFlowAxisLockedSpanPlacement is a regression test for the §8.5 flow-axis-
// locked placement fix: an item with a definite line on the FLOW axis but auto on the
// cross axis (grid-column: 1 / 3 with an auto row, in row flow) is pinned to its
// locked flow line and placed on the next free cross line — not auto-flowed freely.
// The spanning item must land BELOW the auto-placed row, full width, at X 0.
func TestGridFlowAxisLockedSpanPlacement(t *testing.T) {
	src := `<head><style>
	.g{display:grid;grid-template-columns:100px 100px;gap:0}
	.span{grid-column:1 / 3}
	</style></head><body style="margin:0"><div class="g">` +
		`<div style="height:20px;background:#111">a</div>` +
		`<div style="height:20px;background:#222">b</div>` +
		`<div class="span" style="height:20px;background:#333">wide</div>` +
		`</div></body>`
	frag := layoutHTML(t, src, 400)
	// The spanning div must be at Y >= 20 (below the first row) and span both columns
	// (W == 200, the two 100px tracks).
	var span *Fragment
	var walk func(g *Fragment)
	walk = func(g *Fragment) {
		if g == nil {
			return
		}
		if g.W == 200 && g.Y >= 20 {
			span = g
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk(frag)
	if span == nil {
		t.Fatalf("spanning item not found at the second row spanning both columns (it was mis-placed)")
	}
	if span.X != 0 {
		t.Errorf("spanning item X = %.1f, want 0 (full width from column 1)", span.X)
	}
}
