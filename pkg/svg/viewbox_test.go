package svg

import "testing"

func TestViewBoxMatrix(t *testing.T) {
	vb := viewBox{0, 0, 100, 50}
	// Default xMidYMid meet into a 200x200 viewport: uniform scale 2, centered
	// vertically: (0,0)->(0,50), (100,50)->(200,150).
	m := viewBoxMatrix(vb, 200, 200, "")
	if x, y := m.Apply(0, 0); x != 0 || y != 50 {
		t.Errorf("meet origin = (%g,%g), want (0,50)", x, y)
	}
	if x, y := m.Apply(100, 50); x != 200 || y != 150 {
		t.Errorf("meet corner = (%g,%g), want (200,150)", x, y)
	}
	// slice: scale = max = 4, centered horizontally: (0,0) -> (-100, 0).
	m = viewBoxMatrix(vb, 200, 200, "xMidYMid slice")
	if x, y := m.Apply(0, 0); x != -100 || y != 0 {
		t.Errorf("slice origin = (%g,%g), want (-100,0)", x, y)
	}
	// none: non-uniform 2x4.
	m = viewBoxMatrix(vb, 200, 200, "none")
	if x, y := m.Apply(100, 50); x != 200 || y != 200 {
		t.Errorf("none corner = (%g,%g)", x, y)
	}
	// xMinYMax meet: flush left, flush bottom.
	m = viewBoxMatrix(vb, 200, 200, "xMinYMax meet")
	if x, y := m.Apply(0, 50); x != 0 || y != 200 {
		t.Errorf("xMinYMax = (%g,%g), want (0,200)", x, y)
	}
	// min offsets: viewBox="10 20 100 50" maps (10,20)->aligned origin.
	m = viewBoxMatrix(viewBox{10, 20, 100, 50}, 200, 100, "xMinYMin meet")
	if x, y := m.Apply(10, 20); x != 0 || y != 0 {
		t.Errorf("min offset = (%g,%g), want (0,0)", x, y)
	}
	if _, ok := parseViewBox("0 0 -5 10"); ok {
		t.Error("negative width accepted")
	}
	if vb, ok := parseViewBox(" 0,0 100 50 "); !ok || vb.W != 100 {
		t.Errorf("comma form = %+v,%v", vb, ok)
	}
}
