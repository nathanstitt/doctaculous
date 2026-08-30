package css

import (
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// lineBoxHeight lays out two identical rows and returns the vertical distance between
// their baselines — the used line height, measured rather than computed.
func lineBoxHeight(t *testing.T, style string) float64 {
	t.Helper()
	root := layoutWithLoader(t,
		`<body><div style="`+style+`">Xg</div><div style="`+style+`">Xg</div></body>`,
		400, resource.MapLoader{}, nil)
	var baselines []float64
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		for i := range f.Lines {
			baselines = append(baselines, f.Lines[i].BaselineY)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(baselines) < 2 {
		t.Fatalf("expected 2 line boxes, got %d", len(baselines))
	}
	return baselines[1] - baselines[0]
}

// A unitless line-height multiplies the font size. It used to be rejected by
// parseLength (correctly, for a length) and the declaration dropped, so every value
// produced the same font-metric height and the property appeared to do nothing.
func TestUnitlessLineHeightScalesWithMultiplier(t *testing.T) {
	h1 := lineBoxHeight(t, "font-size:20px;line-height:1")
	h2 := lineBoxHeight(t, "font-size:20px;line-height:2")
	h3 := lineBoxHeight(t, "font-size:20px;line-height:3")

	if h1 >= h2 || h2 >= h3 {
		t.Fatalf("line heights do not increase with the multiplier: 1=%v 2=%v 3=%v", h1, h2, h3)
	}
	// The multiplier is exact: 2 and 3 are 2x and 3x the font size.
	if d := h2 - 2*20; d > 0.5 || d < -0.5 {
		t.Errorf("line-height:2 at 20px = %v, want 40", h2)
	}
	if d := h3 - 3*20; d > 0.5 || d < -0.5 {
		t.Errorf("line-height:3 at 20px = %v, want 60", h3)
	}
}

// The three spellings that mean the same thing at one font size agree.
func TestLineHeightUnitsAgree(t *testing.T) {
	num := lineBoxHeight(t, "font-size:20px;line-height:2")
	em := lineBoxHeight(t, "font-size:20px;line-height:2em")
	pct := lineBoxHeight(t, "font-size:20px;line-height:200%")
	px := lineBoxHeight(t, "font-size:20px;line-height:40px")
	for _, c := range []struct {
		name string
		got  float64
	}{{"2em", em}, {"200%", pct}, {"40px", px}} {
		if d := c.got - num; d > 0.5 || d < -0.5 {
			t.Errorf("line-height:%s = %v, want %v (same as the unitless 2)", c.name, c.got, num)
		}
	}
}

// A NUMBER inherits as a number and re-multiplies against the descendant's own font
// size; an EM computes against the declaring element and inherits as a fixed length
// (CSS 2.1 §10.8.1). Getting this backwards is invisible until a nested font-size
// change appears, which is exactly when it matters.
func TestLineHeightInheritanceDependsOnUnit(t *testing.T) {
	numberH := nestedLineHeight(t, "font-size:10px;line-height:2", "font-size:40px")
	emH := nestedLineHeight(t, "font-size:10px;line-height:2em", "font-size:40px")

	// A number re-multiplies: 40px font * 2 = 80.
	if d := numberH - 80; d > 1 || d < -1 {
		t.Errorf("inherited unitless line-height = %v, want 80 (2 x the child's 40px)", numberH)
	}
	// An em is fixed at the parent's size: 10px * 2 = 20.
	if d := emH - 20; d > 1 || d < -1 {
		t.Errorf("inherited em line-height = %v, want 20 (computed against the parent's 10px)", emH)
	}
	if numberH <= emH {
		t.Error("the two inheritance modes produced the same result; the unit distinction was lost")
	}
}

// nestedLineHeight measures the used line height of a child that inherits line-height
// from its parent while declaring its own font size.
func nestedLineHeight(t *testing.T, parentStyle, childStyle string) float64 {
	t.Helper()
	root := layoutWithLoader(t,
		`<body style="`+parentStyle+`"><div style="`+childStyle+`">Xg</div><div style="`+childStyle+`">Xg</div></body>`,
		400, resource.MapLoader{}, nil)
	var baselines []float64
	var walk func(f *Fragment)
	walk = func(f *Fragment) {
		for i := range f.Lines {
			baselines = append(baselines, f.Lines[i].BaselineY)
		}
		for _, c := range f.Children {
			walk(c)
		}
	}
	walk(root)
	if len(baselines) < 2 {
		t.Fatalf("expected 2 line boxes, got %d", len(baselines))
	}
	return baselines[1] - baselines[0]
}

// "normal" keeps the font-metric height, so a document that never sets line-height is
// unaffected by any of the above.
func TestLineHeightNormalUnchanged(t *testing.T) {
	none := lineBoxHeight(t, "font-size:20px")
	normal := lineBoxHeight(t, "font-size:20px;line-height:normal")
	if d := none - normal; d > 0.5 || d < -0.5 {
		t.Errorf("line-height:normal = %v, want the same as no declaration (%v)", normal, none)
	}
}
