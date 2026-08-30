package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// flexChildRect lays out one sized child inside a container and returns its fragment
// rect, so a test can assert where a margin actually put it.
func flexChildRect(t *testing.T, parentStyle, childStyle string) (x, y, w, h float64) {
	t.Helper()
	root := layoutWithLoader(t,
		`<body><div style="`+parentStyle+`"><div style="width:30px;height:20px;`+childStyle+`"></div></div></body>`,
		400, resource.MapLoader{}, nil)
	f := root
	for len(f.Children) > 0 {
		f = f.Children[len(f.Children)-1]
	}
	return f.X, f.Y, f.W, f.H
}

// A margin on a flex child is honoured, in both axes. Flex layout was previously
// margin-blind entirely — an item's size and position came from its border box — so
// `margin-top` on a child of a flex column did nothing while the identical rule on a
// block child worked. That reads as "the rule did not apply".
func TestFlexChildMarginsApply(t *testing.T) {
	// Column: margin-top is on the MAIN axis.
	_, y, _, _ := flexChildRect(t, "padding-top:1px;display:flex;flex-direction:column", "margin-top:40px")
	if d := y - 41; d > 0.5 || d < -0.5 {
		t.Errorf("flex column child y = %v, want 41 (1px padding + 40px margin)", y)
	}
	// Row: margin-left is on the MAIN axis.
	x, _, _, _ := flexChildRect(t, "display:flex", "margin-left:60px")
	if d := x - 60; d > 0.5 || d < -0.5 {
		t.Errorf("flex row child x = %v, want 60", x)
	}
	// Column: margin-left is on the CROSS axis.
	x, _, _, _ = flexChildRect(t, "display:flex;flex-direction:column", "margin-left:25px")
	if d := x - 25; d > 0.5 || d < -0.5 {
		t.Errorf("flex column cross margin x = %v, want 25", x)
	}
}

// A block child with the same rule lands in the same place, which is the control that
// makes the assertions above meaningful: the bug was flex-specific, and block layout
// must not change.
func TestBlockChildMarginUnchanged(t *testing.T) {
	_, blockY, _, _ := flexChildRect(t, "padding-top:1px", "margin-top:40px")
	_, flexY, _, _ := flexChildRect(t, "padding-top:1px;display:flex;flex-direction:column", "margin-top:40px")
	if d := blockY - flexY; d > 0.5 || d < -0.5 {
		t.Errorf("block child at y=%v but flex child at y=%v; they should agree", blockY, flexY)
	}
}

// `margin: auto` on the main axis absorbs free space (CSS Flexbox §8.1) — the
// idiomatic way to push one item to the end of a row.
func TestFlexAutoMarginAbsorbsFreeSpace(t *testing.T) {
	x, _, _, _ := flexChildRect(t, "display:flex;width:200px", "margin-left:auto")
	if d := x - 170; d > 0.5 || d < -0.5 {
		t.Errorf("auto-margin child x = %v, want 170 (200 container - 30 item)", x)
	}
	// Both auto margins centre the item.
	x, _, _, _ = flexChildRect(t, "display:flex;width:200px", "margin-left:auto;margin-right:auto")
	if d := x - 85; d > 0.5 || d < -0.5 {
		t.Errorf("two auto margins x = %v, want 85 (centred)", x)
	}
}

// A margin consumes main-axis space, so it must be counted when packing lines and
// distributing free space — not merely applied as an offset at paint time.
func TestFlexMarginConsumesMainSpace(t *testing.T) {
	// justify-content: flex-end must place the item's MARGIN box against the end.
	root := layoutWithLoader(t,
		`<body><div style="display:flex;width:200px;justify-content:flex-end"><div style="width:30px;height:20px;margin-right:50px"></div></div></body>`,
		400, resource.MapLoader{}, nil)
	f := root
	for len(f.Children) > 0 {
		f = f.Children[len(f.Children)-1]
	}
	// 200 container - 50 margin - 30 item = 120.
	if d := f.X - 120; d > 0.5 || d < -0.5 {
		t.Errorf("justify-content:flex-end with a trailing margin put the item at x=%v, want 120", f.X)
	}
}

// flexContainerHeight lays out a flex container holding three 20px children, the middle
// one carrying childStyle, and returns the CONTAINER's used height.
func flexContainerHeight(t *testing.T, parentStyle, childStyle string) float64 {
	t.Helper()
	root := layoutWithLoader(t,
		`<body><div id="c" style="`+parentStyle+`">`+
			`<div style="height:20px"></div>`+
			`<div style="height:20px;`+childStyle+`"></div>`+
			`<div style="height:20px"></div></div></body>`,
		400, resource.MapLoader{}, nil)
	var container *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if len(f.Children) == 3 && container == nil {
			container = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if container == nil {
		t.Fatal("flex container fragment not found")
	}
	return container.H
}

// An auto-height flex column must be tall enough to contain its children's MARGIN
// boxes. Line packing, free-space distribution, and cross sizing were all converted to
// outer sizes when margins landed, but the indefinite-main branch that content-sizes the
// line kept summing the border box. The margins were still applied at placement, so the
// children were positioned correctly and simply overflowed the container that was
// supposed to have grown for them.
//
// Measured before the fix: margin-bottom:40px on the middle child gave a container
// height of 80 where CSS requires 100. margin-top happened to read as correct because
// the leading margin shifts every subsequent child, so the overflow landed past the last
// one rather than between them — which is why this needs both directions asserted.
func TestFlexColumnContentHeightIncludesChildMargins(t *testing.T) {
	const col = "display:flex;flex-direction:column"

	if h := flexContainerHeight(t, col, ""); h != 60 {
		t.Fatalf("baseline column height = %v, want 60 (3 x 20px); the fixture is wrong", h)
	}
	for _, tc := range []struct {
		name, child string
		want        float64
	}{
		{"leading margin", "margin-top:40px", 100},
		{"trailing margin", "margin-bottom:40px", 100},
		{"both margins", "margin-top:15px;margin-bottom:25px", 100},
	} {
		if h := flexContainerHeight(t, col, tc.child); h != tc.want {
			t.Errorf("column with %s: height = %v, want %v", tc.name, h, tc.want)
		}
	}

	// A CROSS-axis margin must NOT grow a column's height — it is horizontal there.
	if h := flexContainerHeight(t, col, "margin-left:40px"); h != 60 {
		t.Errorf("column with a cross-axis margin: height = %v, want 60 (unchanged)", h)
	}
}

// The same content-sizing rule for a definite-height container must not change: an
// explicit height wins over the content extent, margins or not. This is the control that
// keeps the fix scoped to the indefinite-main branch.
func TestFlexDefiniteHeightUnaffectedByChildMargins(t *testing.T) {
	h := flexContainerHeight(t, "display:flex;flex-direction:column;height:60px", "margin-bottom:40px")
	if h != 60 {
		t.Errorf("explicit height:60px with a child margin = %v, want 60", h)
	}
}

// `gap` still works and does not double-count with margins — it worked before this
// change and must not regress.
func TestFlexGapStillWorks(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><div style="display:flex;gap:60px"><div style="width:10px;height:20px"></div><div style="width:30px;height:20px"></div></div></body>`,
		400, resource.MapLoader{}, nil)
	var second *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.W == 30 && f.H == 20 {
			second = f
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if second == nil {
		t.Fatal("second flex item not found")
	}
	if d := second.X - 70; d > 0.5 || d < -0.5 {
		t.Errorf("second item x = %v, want 70 (10px item + 60px gap)", second.X)
	}
}
