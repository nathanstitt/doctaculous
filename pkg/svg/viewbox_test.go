package svg

import (
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

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

// TestViewBoxMatrixDegenerateExtentReturnsIdentity is a direct unit test of
// viewBoxMatrix's defensive guard: a zero/negative/non-finite W or H must
// return render.Identity rather than dividing by it (which would produce an
// all-NaN or all-Inf matrix). The single current call site in svg.go already
// rejects these via parseViewBox, so this guard is unreachable from there
// today; it protects the additional viewBoxMatrix call sites nested <svg>,
// <symbol>, <pattern>, and <marker> support will add.
func TestViewBoxMatrixDegenerateExtentReturnsIdentity(t *testing.T) {
	cases := []struct {
		name string
		vb   viewBox
	}{
		{"zero-width", viewBox{0, 0, 0, 50}},
		{"zero-height", viewBox{0, 0, 100, 0}},
		{"negative-width", viewBox{0, 0, -100, 50}},
		{"negative-height", viewBox{0, 0, 100, -50}},
		{"nan-width", viewBox{0, 0, math.NaN(), 50}},
		{"inf-height", viewBox{0, 0, 100, math.Inf(1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := viewBoxMatrix(c.vb, 200, 200, "")
			if m != render.Identity {
				t.Errorf("viewBoxMatrix(%+v) = %+v, want render.Identity", c.vb, m)
			}
		})
	}
}
