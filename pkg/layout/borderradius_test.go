package layout

import (
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// uniform builds radii with the same circular radius on every corner.
func uniform(r float64) CornerRadii {
	c := [2]float64{r, r}
	return CornerRadii{TL: c, TR: c, BR: c, BL: c}
}

func TestCornerRadiiZero(t *testing.T) {
	tests := []struct {
		name string
		r    CornerRadii
		want bool
	}{
		{"zero value", CornerRadii{}, true},
		{"all rounded", uniform(5), false},
		{"one rounded corner", CornerRadii{TR: [2]float64{1, 1}}, false},
		{
			// A corner needs BOTH semi-axes to be non-zero to round; a corner with
			// one zero axis is square, so this whole box is square.
			"degenerate axes only", CornerRadii{TL: [2]float64{5, 0}, BR: [2]float64{0, 5}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.Zero(); got != tt.want {
				t.Errorf("Zero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCorrectOverlap covers the CSS Backgrounds 3 §5.1 overlap correction. The
// critical property is that ALL radii scale by ONE shared factor f = min over
// sides, not per-corner clamping — otherwise `border-radius:100px` on an 80x80 box
// yields four separately-clamped arcs instead of a circle.
func TestCorrectOverlap(t *testing.T) {
	tests := []struct {
		name    string
		r       CornerRadii
		w, h    float64
		want    CornerRadii
		comment string
	}{
		{
			name: "no correction needed", r: uniform(10), w: 100, h: 100,
			want:    uniform(10),
			comment: "sums (20) are well under both sides (100); f stays 1",
		},
		{
			name: "exactly fitting is untouched", r: uniform(40), w: 80, h: 80,
			want:    uniform(40),
			comment: "40+40 == 80 exactly: f == 1, no scaling",
		},
		{
			// THE headline case: 100px radii on an 80x80 box. Each side needs
			// 80/200 = 0.4, so f = 0.4 and every radius becomes 40 -- a circle.
			name: "over-large radii become a circle", r: uniform(100), w: 80, h: 80,
			want:    uniform(40),
			comment: "f = 80/(100+100) = 0.4; 100*0.4 = 40 on every corner",
		},
		{
			// The shared factor is the MINIMUM across sides, so the tightest side
			// governs every corner -- including corners that would have fit.
			name: "the tightest side governs every corner",
			r: CornerRadii{
				TL: [2]float64{10, 10}, TR: [2]float64{10, 10},
				BR: [2]float64{90, 90}, BL: [2]float64{90, 90},
			},
			w: 100, h: 100,
			// bottom side: 90+90 = 180 -> 100/180 = 0.5555...
			// right side:  10+90 = 100 -> 100/100 = 1
			// top side:    10+10 =  20 -> 5
			// left side:   10+90 = 100 -> 1
			// f = 0.5555..., applied to ALL eight components.
			want: CornerRadii{
				TL: [2]float64{10 * 100.0 / 180, 10 * 100.0 / 180},
				TR: [2]float64{10 * 100.0 / 180, 10 * 100.0 / 180},
				BR: [2]float64{90 * 100.0 / 180, 90 * 100.0 / 180},
				BL: [2]float64{90 * 100.0 / 180, 90 * 100.0 / 180},
			},
			comment: "f is the min over sides; the small corners shrink too",
		},
		{
			// Horizontal and vertical components are constrained by DIFFERENT
			// sides: horizontals by top/bottom (width), verticals by left/right
			// (height). A wide flat box is limited by its height.
			name: "elliptical radii on a flat box",
			r:    CornerRadii{TL: [2]float64{50, 50}, TR: [2]float64{50, 50}, BR: [2]float64{50, 50}, BL: [2]float64{50, 50}},
			w:    200, h: 40,
			// left/right sides: 50+50 = 100 vs height 40 -> 0.4
			// top/bottom:       50+50 = 100 vs width 200 -> 2
			// f = 0.4
			want:    uniform(20),
			comment: "the height-constrained sides give f = 40/100 = 0.4",
		},
		{
			name: "degenerate box yields square corners", r: uniform(10), w: 0, h: 50,
			want:    CornerRadii{},
			comment: "a zero-area box has no meaningful radii",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Correct(tt.w, tt.h)
			if !radiiNear(got, tt.want, 1e-9) {
				t.Errorf("Correct(%g, %g) = %v, want %v (%s)", tt.w, tt.h, got, tt.want, tt.comment)
			}
		})
	}
}

// TestCorrectIsIdempotent: correcting an already-corrected set must be a no-op,
// which is what lets the border ring re-correct its inner radii safely.
func TestCorrectIsIdempotent(t *testing.T) {
	once := uniform(100).Correct(80, 80)
	twice := once.Correct(80, 80)
	if !radiiNear(once, twice, 1e-9) {
		t.Errorf("Correct is not idempotent: %v then %v", once, twice)
	}
}

// TestInset covers the inner (padding-box) curve: inner radius = outer radius minus
// the border width crossing that axis, floored at zero (CSS Backgrounds 3 §5.2).
func TestInset(t *testing.T) {
	tests := []struct {
		name                     string
		r                        CornerRadii
		top, right, bottom, left float64
		want                     CornerRadii
		comment                  string
	}{
		{
			name: "uniform border shrinks every radius", r: uniform(20),
			top: 5, right: 5, bottom: 5, left: 5,
			want:    uniform(15),
			comment: "20-5 on both axes of all four corners",
		},
		{
			// Each corner's two components subtract DIFFERENT edges: the top-left
			// horizontal uses LEFT's width, its vertical uses TOP's.
			name: "each axis subtracts its own edge", r: uniform(20),
			top: 1, right: 2, bottom: 3, left: 4,
			want: CornerRadii{
				TL: [2]float64{20 - 4, 20 - 1}, // left, top
				TR: [2]float64{20 - 2, 20 - 1}, // right, top
				BR: [2]float64{20 - 2, 20 - 3}, // right, bottom
				BL: [2]float64{20 - 4, 20 - 3}, // left, bottom
			},
			comment: "horizontal uses left/right, vertical uses top/bottom",
		},
		{
			// A border thicker than the radius squares the INNER corner while the
			// outer stays round -- exactly what browsers draw, and the reason a
			// rounded border is not the outer path stroked.
			name: "a thick border squares the inner corner", r: uniform(5),
			top: 10, right: 10, bottom: 10, left: 10,
			want:    CornerRadii{},
			comment: "floored at zero, never negative",
		},
		{
			name: "no border leaves the radii alone", r: uniform(7),
			want:    uniform(7),
			comment: "all widths zero",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Inset(tt.top, tt.right, tt.bottom, tt.left)
			if !radiiNear(got, tt.want, 1e-9) {
				t.Errorf("Inset(%g,%g,%g,%g) = %v, want %v (%s)",
					tt.top, tt.right, tt.bottom, tt.left, got, tt.want, tt.comment)
			}
		})
	}
}

// TestAppendRoundedRectSquare proves the zero-radii path emits EXACTLY the plain
// four-line rectangle the unrounded painter emitted — same segment count, same
// order, same points. This is the guard for byte-identical output on every
// document that uses no radius: an extra zero-length segment here would be
// serialized by pdfwrite as a stray `l` operator in every content stream.
func TestAppendRoundedRectSquare(t *testing.T) {
	got := &render.Path{}
	AppendRoundedRect(got, 10, 20, 30, 40, CornerRadii{}, identity)

	want := &render.Path{}
	want.MoveTo(10, 20)
	want.LineTo(40, 20)
	want.LineTo(40, 60)
	want.LineTo(10, 60)
	want.Close()

	if len(got.Segments) != len(want.Segments) {
		t.Fatalf("square path has %d segments, want %d", len(got.Segments), len(want.Segments))
	}
	for i := range got.Segments {
		if got.Segments[i] != want.Segments[i] {
			t.Errorf("segment %d = %+v, want %+v", i, got.Segments[i], want.Segments[i])
		}
	}
}

// TestAppendRoundedRectGeometry checks the emitted path's shape: a rounded rect
// stays inside its box, touches all four edges, and uses a cubic per rounded
// corner.
func TestAppendRoundedRectGeometry(t *testing.T) {
	p := &render.Path{}
	AppendRoundedRect(p, 0, 0, 80, 80, uniform(40), identity)

	cubics := 0
	for _, s := range p.Segments {
		if s.Kind == render.CubeTo {
			cubics++
		}
	}
	if cubics != 4 {
		t.Errorf("a fully rounded rect should emit 4 cubic corners, got %d", cubics)
	}

	minX, minY, maxX, maxY, ok := p.Bounds()
	if !ok {
		t.Fatal("path reported no bounds")
	}
	// Bounds include control points, which for a quarter-arc lie inside the box's
	// corner, so the path must not exceed the box at all.
	const eps = 1e-9
	if minX < -eps || minY < -eps || maxX > 80+eps || maxY > 80+eps {
		t.Errorf("rounded rect escapes its box: got (%g,%g)-(%g,%g), want within (0,0)-(80,80)",
			minX, minY, maxX, maxY)
	}
	if math.Abs(maxX-80) > eps || math.Abs(maxY-80) > eps {
		t.Errorf("rounded rect should touch its far edges: got max (%g,%g), want (80,80)", maxX, maxY)
	}
}

// TestAppendRoundedRectDegenerate: the painter must never panic or emit anything
// for a zero/negative-area box.
func TestAppendRoundedRectDegenerate(t *testing.T) {
	for _, d := range []struct{ w, h float64 }{{0, 10}, {10, 0}, {-5, 10}, {10, -5}} {
		p := &render.Path{}
		AppendRoundedRect(p, 0, 0, d.w, d.h, uniform(3), identity)
		if !p.Empty() {
			t.Errorf("%gx%g box should emit nothing, got %d segments", d.w, d.h, len(p.Segments))
		}
	}
}

// TestAppendRoundedRectMapsPoints checks every emitted point goes through the
// mapper, so the painter's page->device matrix is actually applied.
func TestAppendRoundedRectMapsPoints(t *testing.T) {
	shift := func(x, y float64) (float64, float64) { return x + 100, y + 200 }
	p := &render.Path{}
	AppendRoundedRect(p, 0, 0, 10, 10, uniform(2), shift)
	minX, minY, _, _, ok := p.Bounds()
	if !ok {
		t.Fatal("path reported no bounds")
	}
	if minX < 100 || minY < 200 {
		t.Errorf("points were not mapped: min (%g,%g), want >= (100,200)", minX, minY)
	}
}

func identity(x, y float64) (float64, float64) { return x, y }

func radiiNear(a, b CornerRadii, tol float64) bool {
	for _, p := range [][2][2]float64{{a.TL, b.TL}, {a.TR, b.TR}, {a.BR, b.BR}, {a.BL, b.BL}} {
		if math.Abs(p[0][0]-p[1][0]) > tol || math.Abs(p[0][1]-p[1][1]) > tol {
			return false
		}
	}
	return true
}
