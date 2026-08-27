package render

import (
	"math"
	"testing"
)

func approxVec(t *testing.T, name string, got Vector, wantX, wantY float64) {
	t.Helper()
	const eps = 1e-9
	if math.Abs(got.X-wantX) > eps || math.Abs(got.Y-wantY) > eps {
		t.Errorf("%s = (%v, %v), want (%v, %v)", name, got.X, got.Y, wantX, wantY)
	}
}

func TestVerticesNil(t *testing.T) {
	if got := Vertices(nil); got != nil {
		t.Fatalf("Vertices(nil) = %v, want nil", got)
	}
	if got := Vertices(&Path{}); got != nil {
		t.Fatalf("Vertices(empty) = %v, want nil", got)
	}
}

func TestVerticesSimpleLine(t *testing.T) {
	p := &Path{}
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	p.LineTo(10, 10)
	v := Vertices(p)
	if len(v) != 3 {
		t.Fatalf("len(v) = %d, want 3", len(v))
	}
	// Vertex 0: M start, no in-tangent, out toward (10,0).
	if !v[0].InTangent.IsZero() {
		t.Errorf("v[0].InTangent = %v, want zero", v[0].InTangent)
	}
	approxVec(t, "v[0].OutTangent", v[0].OutTangent, 10, 0)
	if !v[0].IsSubpathStart {
		t.Error("v[0].IsSubpathStart = false, want true")
	}

	// Vertex 1: mid, in from (0,0)->(10,0), out toward (10,10).
	approxVec(t, "v[1].InTangent", v[1].InTangent, 10, 0)
	approxVec(t, "v[1].OutTangent", v[1].OutTangent, 0, 10)

	// Vertex 2: end, in from (10,0)->(10,10), no out.
	approxVec(t, "v[2].InTangent", v[2].InTangent, 0, 10)
	if !v[2].OutTangent.IsZero() {
		t.Errorf("v[2].OutTangent = %v, want zero", v[2].OutTangent)
	}
}

// TestVerticesZeroLengthPath covers zero-length-path-1/2.svg: a path whose
// only drawn segment is a LineTo back to the same point. Both endpoints must
// report a zero tangent rather than a NaN/garbage direction, since marker
// placement (a later layer) needs to detect "no direction" and fall back to
// something sane (e.g. angle 0) instead of dividing by a zero-length vector.
func TestVerticesZeroLengthPath(t *testing.T) {
	p := &Path{}
	p.MoveTo(100, 100)
	p.LineTo(100, 100)
	v := Vertices(p)
	if len(v) != 2 {
		t.Fatalf("len(v) = %d, want 2", len(v))
	}
	if !v[0].OutTangent.IsZero() {
		t.Errorf("v[0].OutTangent = %v, want zero (zero-length LineTo)", v[0].OutTangent)
	}
	if !v[1].InTangent.IsZero() {
		t.Errorf("v[1].InTangent = %v, want zero (zero-length LineTo)", v[1].InTangent)
	}
}

// TestVerticesCubicCoincidentControlPoints exercises the fallback chain the
// orient=auto-on-M-C-C-*.svg fixtures target: a cubic whose first control
// point sits exactly on the current point must still report a direction from
// its later control points, not a zero out-tangent.
func TestVerticesCubicCoincidentControlPoints(t *testing.T) {
	t.Run("P0 coincides with cur, falls back to P1", func(t *testing.T) {
		p := &Path{}
		p.MoveTo(0, 0)
		p.CubeTo(0, 0, 5, 10, 10, 10) // P0==cur; P1-cur = (5,10)
		v := Vertices(p)
		approxVec(t, "out", v[0].OutTangent, 5, 10)
	})
	t.Run("P0 and P1 coincide with cur, falls back to P2", func(t *testing.T) {
		p := &Path{}
		p.MoveTo(0, 0)
		p.CubeTo(0, 0, 0, 0, 10, 10) // P0==P1==cur; P2-cur = (10,10)
		v := Vertices(p)
		approxVec(t, "out", v[0].OutTangent, 10, 10)
	})
	t.Run("all four points coincide: zero out-tangent", func(t *testing.T) {
		p := &Path{}
		p.MoveTo(5, 5)
		p.CubeTo(5, 5, 5, 5, 5, 5)
		v := Vertices(p)
		if !v[0].OutTangent.IsZero() {
			t.Errorf("out = %v, want zero", v[0].OutTangent)
		}
		if !v[1].InTangent.IsZero() {
			t.Errorf("in = %v, want zero", v[1].InTangent)
		}
	})
	t.Run("P2 coincides with P1, falls back to P2-P0", func(t *testing.T) {
		p := &Path{}
		p.MoveTo(0, 0)
		p.CubeTo(1, 1, 10, 10, 10, 10) // P2==P1; P2-P0 = (10,10)-(0,0)... wait use distinct P0
		v := Vertices(p)
		approxVec(t, "in", v[1].InTangent, 9, 9) // P2-P0 = (10,10)-(1,1)
	})
	t.Run("P2 coincides with both P1 and P0, falls back to P2-cur", func(t *testing.T) {
		p := &Path{}
		p.MoveTo(0, 0)
		p.CubeTo(10, 10, 10, 10, 10, 10) // P2==P1==P0; P2-cur = (10,10)
		v := Vertices(p)
		approxVec(t, "in", v[1].InTangent, 10, 10)
	})
}

// TestVerticesClose covers orient=auto-on-M-L-Z.svg: a Close contributes the
// implicit line back to the subpath start, and that line's direction becomes
// the start vertex's in-tangent.
func TestVerticesClose(t *testing.T) {
	p := &Path{}
	p.MoveTo(30, 100)
	p.LineTo(170, 100)
	p.Close()
	v := Vertices(p)
	if len(v) != 2 {
		t.Fatalf("len(v) = %d, want 2", len(v))
	}
	// v[0] = (30,100): out toward (170,100); in from the closing line
	// (30,100)-(170,100), i.e. direction (-140,0).
	approxVec(t, "v[0].OutTangent", v[0].OutTangent, 140, 0)
	approxVec(t, "v[0].InTangent", v[0].InTangent, -140, 0)
	// v[1] = (170,100): in from (30,100)->(170,100) = (140,0); out along the
	// closing line toward (30,100) = (-140,0).
	approxVec(t, "v[1].InTangent", v[1].InTangent, 140, 0)
	approxVec(t, "v[1].OutTangent", v[1].OutTangent, -140, 0)
}

// TestVerticesRepeatedClose covers orient=auto-on-M-L-L-Z-Z-Z.svg: repeated Z
// after the pen is already back at the subpath start must be a harmless
// no-op, not overwrite the first Close's already-computed in-tangent with a
// zero-length "direction".
func TestVerticesRepeatedClose(t *testing.T) {
	p := &Path{}
	p.MoveTo(50, 160)
	p.LineTo(100, 50)
	p.LineTo(150, 160)
	p.Close()
	p.Close()
	p.Close()
	v := Vertices(p)
	if len(v) != 3 {
		t.Fatalf("len(v) = %d, want 3", len(v))
	}
	// v[0]'s in-tangent must be the first Close's direction: (50,160)-(150,160) = (-100,0).
	approxVec(t, "v[0].InTangent", v[0].InTangent, -100, 0)
	// v[2]'s out-tangent must likewise still be the first Close's direction
	// (subsequent Z's are zero-length and must not clobber it).
	approxVec(t, "v[2].OutTangent", v[2].OutTangent, -100, 0)
}

// TestVerticesBareMoveZ covers a subpath with no drawing segment before its
// Close ("M Z"): there is no implicit line to draw, so the start vertex's
// in-tangent stays zero rather than becoming some degenerate direction.
func TestVerticesBareMoveZ(t *testing.T) {
	p := &Path{}
	p.MoveTo(50, 50)
	p.Close()
	v := Vertices(p)
	if len(v) != 1 {
		t.Fatalf("len(v) = %d, want 1", len(v))
	}
	if !v[0].InTangent.IsZero() {
		t.Errorf("InTangent = %v, want zero", v[0].InTangent)
	}
	if !v[0].OutTangent.IsZero() {
		t.Errorf("OutTangent = %v, want zero", v[0].OutTangent)
	}
}

// TestVerticesMultipleSubpaths covers target-with-subpaths-1/2.svg: marker
// placement applies marker-start to the FIRST vertex of the WHOLE path and
// marker-mid to every other vertex including interior MoveTos — Vertices
// itself just reports IsSubpathStart per vertex; this test checks the
// flags land where the design says a caller should read them (index 0 vs.
// interior MoveTos vs. the last index).
func TestVerticesMultipleSubpaths(t *testing.T) {
	p := &Path{}
	p.MoveTo(0, 0)
	p.LineTo(10, 0)
	p.MoveTo(20, 20)
	p.LineTo(30, 20)
	v := Vertices(p)
	if len(v) != 4 {
		t.Fatalf("len(v) = %d, want 4", len(v))
	}
	if !v[0].IsSubpathStart {
		t.Error("v[0].IsSubpathStart = false, want true (first M)")
	}
	if v[1].IsSubpathStart {
		t.Error("v[1].IsSubpathStart = true, want false")
	}
	if !v[2].IsSubpathStart {
		t.Error("v[2].IsSubpathStart = false, want true (interior M)")
	}
	// The interior M (index 2) is NOT the whole path's marker-start (index 0
	// is), even though IsSubpathStart is true for both — callers must use
	// index, not this flag alone, to pick marker-start vs marker-mid.
	if !v[2].InTangent.IsZero() {
		t.Errorf("v[2].InTangent = %v, want zero (interior M, no close)", v[2].InTangent)
	}
	// Last vertex (index 3) is the whole path's marker-end vertex.
	approxVec(t, "v[3].InTangent", v[3].InTangent, 10, 0)
}

// TestBisectorSimpleTurn covers the bisector at a mid vertex where in/out
// differ (a right-angle turn): the bisector of (1,0) arriving and (0,1)
// leaving should point along (1,1) normalized.
func TestBisectorSimpleTurn(t *testing.T) {
	b := Bisector(Vector{1, 0}, Vector{0, 1})
	u, ok := b.normalize()
	if !ok {
		t.Fatal("bisector normalized to zero")
	}
	want := 1 / math.Sqrt2
	approxVec(t, "bisector", u, want, want)
}

// TestBisectorStraightLine covers the common "no turn" case: in and out point
// the same direction, so the bisector is that same direction.
func TestBisectorStraightLine(t *testing.T) {
	b := Bisector(Vector{1, 0}, Vector{1, 0})
	u, ok := b.normalize()
	if !ok {
		t.Fatal("bisector normalized to zero")
	}
	approxVec(t, "bisector", u, 1, 0)
}

// TestBisectorUTurn covers the degenerate cusp case: in and out point exactly
// opposite directions, so their unit sum cancels and the bisector falls back
// to a perpendicular. Only the fallback's well-definedness (non-zero, and
// perpendicular to in) is asserted, not a specific one of the two
// perpendicular choices, since SVG itself leaves this case's exact angle
// undefined (a 180° cusp has no canonical bisector).
func TestBisectorUTurn(t *testing.T) {
	b := Bisector(Vector{1, 0}, Vector{-1, 0})
	if b.IsZero() {
		t.Fatal("bisector is zero for a U-turn, want a defined perpendicular fallback")
	}
	// Must be perpendicular to (1,0): b.X ~= 0.
	if math.Abs(b.X) > 1e-9 {
		t.Errorf("bisector = %v, want perpendicular to (1,0) (X~=0)", b)
	}
}

// TestBisectorOneSideZero covers a mid vertex reached via a zero-length
// segment on one side (e.g. adjacent to a zero-length Close): the bisector
// degrades to whichever side has a direction.
func TestBisectorOneSideZero(t *testing.T) {
	b := Bisector(Vector{}, Vector{3, 4})
	u, ok := b.normalize()
	if !ok {
		t.Fatal("bisector normalized to zero")
	}
	approxVec(t, "bisector", u, 0.6, 0.8)

	b2 := Bisector(Vector{3, 4}, Vector{})
	u2, ok := b2.normalize()
	if !ok {
		t.Fatal("bisector normalized to zero")
	}
	approxVec(t, "bisector", u2, 0.6, 0.8)
}

// TestBisectorBothZero covers a mid vertex with no direction on either side
// at all (both adjacent segments zero-length): the bisector is the zero
// Vector, and callers must treat that as "no orientation available" rather
// than crash on Angle().
func TestBisectorBothZero(t *testing.T) {
	b := Bisector(Vector{}, Vector{})
	if !b.IsZero() {
		t.Errorf("bisector = %v, want zero", b)
	}
}

// TestVerticesContinuationRun covers the on-ArcTo.svg fixture's shape: a
// single source command (an SVG elliptical arc) flattened into several
// CubeTo segments must produce exactly ONE vertex pair (start and end), not
// one per flattened cubic — a marker walking the result must never see a
// marker-mid vertex at an internal slice boundary that was never part of the
// original path data.
func TestVerticesContinuationRun(t *testing.T) {
	p := &Path{}
	p.MoveTo(0, 0)
	// Three-slice run simulating one arc flattened into three cubics: only
	// the FINAL CubeTo is a real command boundary.
	p.CubeToContinuation(1, 1, 2, 2, 3, 3)
	p.CubeToContinuation(4, 4, 5, 5, 6, 6)
	p.CubeTo(7, 7, 8, 8, 9, 9)
	v := Vertices(p)
	if len(v) != 2 {
		t.Fatalf("len(v) = %d, want 2 (one run must yield exactly one vertex pair)", len(v))
	}
	// Start vertex's out-tangent comes from the RUN'S FIRST slice (P0-cur =
	// (1,1)-(0,0) = (1,1)).
	approxVec(t, "v[0].OutTangent", v[0].OutTangent, 1, 1)
	// End vertex is the run's actual final endpoint (9,9), with its
	// in-tangent computed from the LAST slice (P2-P1 = (9,9)-(8,8) = (1,1)).
	if v[1].Pos != (Point{9, 9}) {
		t.Errorf("v[1].Pos = %v, want (9,9)", v[1].Pos)
	}
	approxVec(t, "v[1].InTangent", v[1].InTangent, 1, 1)
}

// TestVerticesContinuationRunThenMoreSegments checks that a Continuation run
// does not disturb ordinary vertex walking for whatever follows it in the
// same subpath (e.g. a LineTo after an arc).
func TestVerticesContinuationRunThenMoreSegments(t *testing.T) {
	p := &Path{}
	p.MoveTo(0, 0)
	p.CubeToContinuation(1, 0, 2, 0, 3, 0)
	p.CubeTo(4, 0, 5, 0, 6, 0)
	p.LineTo(10, 0)
	v := Vertices(p)
	if len(v) != 3 {
		t.Fatalf("len(v) = %d, want 3", len(v))
	}
	if v[1].Pos != (Point{6, 0}) {
		t.Errorf("v[1].Pos = %v, want (6,0)", v[1].Pos)
	}
	approxVec(t, "v[1].OutTangent", v[1].OutTangent, 4, 0)
	if v[2].Pos != (Point{10, 0}) {
		t.Errorf("v[2].Pos = %v, want (10,0)", v[2].Pos)
	}
}

func TestVectorAngle(t *testing.T) {
	tests := []struct {
		v    Vector
		want float64
	}{
		{Vector{1, 0}, 0},
		{Vector{0, 1}, math.Pi / 2},
		{Vector{-1, 0}, math.Pi},
		{Vector{0, -1}, -math.Pi / 2},
	}
	for _, tc := range tests {
		if got := tc.v.Angle(); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Angle(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
