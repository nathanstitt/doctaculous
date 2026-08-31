package css

import (
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// absChild lays out one absolutely-positioned child inside a relative container and
// returns its fragment rect.
func absChild(t *testing.T, parentStyle, childStyle string) (x, y, w, h float64) {
	t.Helper()
	root := layoutWithLoader(t,
		`<body><div style="position:relative;width:400px;height:120px;`+parentStyle+`">`+
			`<div style="position:absolute;`+childStyle+`"></div></div></body>`,
		600, resource.MapLoader{}, nil)
	var found *Fragment
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		if f.IsPositioned {
			found = f
		}
		for _, c := range f.Children {
			walk(c)
		}
		for _, p := range f.Positioned {
			walk(p)
		}
	}
	walk(root)
	if found == nil {
		t.Fatal("no positioned fragment found")
	}
	return found.X, found.Y, found.W, found.H
}

// GAP 10: `top` + `bottom` with height:auto sizes the box to the space between them
// (CSS 10.6.4). It used to collapse to the content height — zero for an empty box — so
// the ordinary way to span a container vertically painted nothing. The horizontal
// equivalent (left+right) already worked, which made the asymmetry surprising.
func TestAbsTopBottomSizesBox(t *testing.T) {
	_, y, _, h := absChild(t, "", "left:0;width:50px;top:0;bottom:20px")
	if d := y - 0; d > 0.5 || d < -0.5 {
		t.Errorf("top = %v, want 0", y)
	}
	if d := h - 100; d > 0.5 || d < -0.5 {
		t.Errorf("height = %v, want 100 (120 container - 20 bottom)", h)
	}
}

// The horizontal pair still works — the control that shows the two axes now agree.
func TestAbsLeftRightSizesBox(t *testing.T) {
	x, _, w, _ := absChild(t, "", "top:0;height:30px;left:0;right:40px")
	if d := x - 0; d > 0.5 || d < -0.5 {
		t.Errorf("left = %v, want 0", x)
	}
	if d := w - 360; d > 0.5 || d < -0.5 {
		t.Errorf("width = %v, want 360 (400 container - 40 right)", w)
	}
}

// An explicit height still wins over the offsets, so nothing that specified one moves.
func TestAbsExplicitHeightUnchanged(t *testing.T) {
	_, _, _, h := absChild(t, "", "left:0;width:50px;top:0;height:75px")
	if d := h - 75; d > 0.5 || d < -0.5 {
		t.Errorf("height = %v, want 75", h)
	}
}

// GAP 10b: an absolutely-positioned child of a FLEX container is not a flex item
// (CSS Flexbox §4.1) — it is out of flow and positioned against the container's
// padding box. It used to be laid out as an item and pinned to the container's edge,
// discarding `left` entirely.
func TestAbsChildOfFlexHonorsOffsets(t *testing.T) {
	blockX, _, _, _ := absChild(t, "", "left:300px;top:0;width:40px;height:40px")
	flexX, _, _, _ := absChild(t, "display:flex", "left:300px;top:0;width:40px;height:40px")
	if d := flexX - 300; d > 0.5 || d < -0.5 {
		t.Errorf("abs child of a flex container x = %v, want 300", flexX)
	}
	if d := flexX - blockX; d > 0.5 || d < -0.5 {
		t.Errorf("flex parent gave x=%v but a block parent gave x=%v; they must agree", flexX, blockX)
	}
}

// The same holds for a flex COLUMN, and for the vertical offset.
func TestAbsChildOfFlexColumnHonorsOffsets(t *testing.T) {
	x, y, _, _ := absChild(t, "display:flex;flex-direction:column", "left:200px;top:60px;width:40px;height:40px")
	if d := x - 200; d > 0.5 || d < -0.5 {
		t.Errorf("x = %v, want 200", x)
	}
	if d := y - 60; d > 0.5 || d < -0.5 {
		t.Errorf("y = %v, want 60", y)
	}
}

// GAP 11: an element whose height comes from FLEX layout must still resolve
// justify-content for its own children. Both routes to a flex-derived height — the
// parent's align-items:stretch and the item's own flex:1 — used to lay children out
// from the top, as if the height were auto.
func TestFlexDerivedHeightResolvesJustifyContent(t *testing.T) {
	childTop := func(outer, mid string) float64 {
		root := layoutWithLoader(t,
			`<body><div style="width:100px;`+outer+`"><div style="`+mid+`">`+
				`<div style="width:40px;height:20px" class="probe"></div></div></div></body>`,
			400, resource.MapLoader{}, nil)
		var deepest *Fragment
		var walk func(f *Fragment)
		walk = func(f *Fragment) {
			if f.W == 40 && f.H == 20 {
				deepest = f
			}
			for _, c := range f.Children {
				walk(c)
			}
		}
		walk(root)
		if deepest == nil {
			t.Fatal("probe child not found")
		}
		return deepest.Y
	}

	explicit := childTop("", "height:200px;display:flex;flex-direction:column;justify-content:center")
	stretch := childTop("display:flex;align-items:stretch;height:200px", "display:flex;flex-direction:column;justify-content:center")
	flexOne := childTop("display:flex;flex-direction:column;height:200px", "flex:1;display:flex;flex-direction:column;justify-content:center")

	if d := explicit - 90; d > 1 || d < -1 {
		t.Fatalf("explicit height centred at %v, want ~90 (the control is wrong)", explicit)
	}
	if d := stretch - explicit; d > 1 || d < -1 {
		t.Errorf("align-items:stretch height centred at %v, want the same as an explicit height (%v)", stretch, explicit)
	}
	if d := flexOne - explicit; d > 1 || d < -1 {
		t.Errorf("flex:1 height centred at %v, want the same as an explicit height (%v)", flexOne, explicit)
	}
}
