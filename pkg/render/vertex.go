package render

import "math"

// Vertex is one point along a Path where a marker can be placed: its
// position, plus the direction the path arrives from (InTangent) and departs
// toward (OutTangent). Either tangent may be the zero Vector when there is no
// segment on that side (the very first vertex of the whole path has no
// in-tangent unless its subpath closes) or every candidate direction is
// degenerate (see Vertices' doc comment on the fallback chain).
//
// A tangent is a direction, not a unit vector: callers that need an angle
// should use Vector.Angle, which is well-defined for any non-zero vector
// regardless of magnitude.
type Vertex struct {
	Pos Point

	// InTangent points in the direction of travel arriving at Pos (i.e. from
	// the previous point toward Pos). The zero Vector means "no incoming
	// direction" — either this is the first vertex of the path and its
	// subpath never closes, or the incoming segment was degenerate at every
	// fallback.
	InTangent Vector

	// OutTangent points in the direction of travel leaving Pos (i.e. from Pos
	// toward the next point). The zero Vector means "no outgoing direction" —
	// either this is the last vertex of its subpath (with no closing Z), or
	// the outgoing segment was degenerate at every fallback.
	OutTangent Vector

	// IsSubpathStart marks a vertex produced by a MoveTo: the first vertex of
	// a new subpath. Per SVG's marker placement rule, only the very first
	// such vertex of the WHOLE path is the path's marker-start vertex; every
	// other one (an interior M) is a marker-mid vertex despite also starting
	// a subpath. Vertices itself does not resolve that distinction (it has
	// no notion of marker-start/mid/end); callers combine IsSubpathStart with
	// "index == 0" / "index == len-1" as needed.
	IsSubpathStart bool
}

// Vector is a 2-D direction (not necessarily unit length).
type Vector struct {
	X, Y float64
}

// IsZero reports whether v is the zero vector (no meaningful direction).
func (v Vector) IsZero() bool {
	return v.X == 0 && v.Y == 0
}

// Angle returns v's direction in radians, using the same axis convention as
// the rest of this package (device space: x right, y down). The zero vector
// has no defined angle; callers must check IsZero first.
func (v Vector) Angle() float64 {
	return math.Atan2(v.Y, v.X)
}

// normalize returns v scaled to unit length, or the zero vector (with ok
// false) when v is already zero or too small to normalize reliably.
func (v Vector) normalize() (Vector, bool) {
	l := math.Hypot(v.X, v.Y)
	if l < 1e-9 {
		return Vector{}, false
	}
	return Vector{v.X / l, v.Y / l}, true
}

// sub returns a-b as a Vector.
func sub(a, b Point) Vector {
	return Vector{a.X - b.X, a.Y - b.Y}
}

// Bisector returns the direction bisecting in and out — the vector sum of
// their unit forms — for orienting a marker at an interior vertex where the
// path's incoming and outgoing directions differ (SVG's orient="auto" rule
// for marker-mid: "the bisector of the angle between the two segments").
// When in and out point in exactly opposite directions (a perfect U-turn),
// their unit vectors cancel and the bisector is otherwise undefined; this
// returns the direction perpendicular to in, rotated toward out's side,
// matching the limiting behavior as the turn angle approaches 180°. When one
// side is the zero vector, the other side's direction is returned outright
// (nothing to bisect against). Two zero vectors return the zero vector.
func Bisector(in, out Vector) Vector {
	inU, inOK := in.normalize()
	outU, outOK := out.normalize()
	switch {
	case !inOK && !outOK:
		return Vector{}
	case !inOK:
		return outU
	case !outOK:
		return inU
	}
	sum := Vector{inU.X + outU.X, inU.Y + outU.Y}
	if _, ok := sum.normalize(); ok {
		return sum
	}
	// inU and outU are opposite (a cusp/U-turn): fall back to the direction
	// perpendicular to in, rotated 90° toward the side out points to, which
	// is the limit of the bisector as the angle between in and -in
	// approaches from either side. Perpendiculars to inU are (-inU.Y, inU.X)
	// and (inU.Y, -inU.X); pick whichever has a non-negative dot product
	// with out (the un-normalized original), so the choice is stable even
	// when out is small.
	perp := Vector{-inU.Y, inU.X}
	if perp.X*out.X+perp.Y*out.Y < 0 {
		perp = Vector{inU.Y, -inU.X}
	}
	return perp
}

// Vertices walks p and returns one Vertex per MoveTo/LineTo/CubeTo endpoint,
// in path order, across all subpaths. It never panics and never hangs: a nil
// or empty Path yields nil.
//
// Tangent fallback chain (documented once here, applied identically to
// LineTo and CubeTo):
//
//   - LineTo: the tangent is simply P0-cur (out, at the segment's start
//     vertex) or the same direction (in, at the segment's end vertex). A
//     zero-length LineTo (P0 == cur) yields the zero Vector on both ends —
//     there is no direction to report, and the caller (marker placement)
//     must treat that as "no tangent contribution" rather than dividing by
//     zero.
//   - CubeTo: out-tangent at the segment's start vertex is P0-cur, falling
//     back to P1-cur then P2-cur when earlier control points coincide with
//     cur (a common authoring pattern — a cubic whose first control point
//     sits exactly on the current point still has a well-defined tangent
//     from its LATER control points, per how real path-editing tools emit
//     "sharp corner into a smooth curve" segments). In-tangent at the
//     segment's end vertex is P2-P1, falling back to P2-P0 then P2-cur when
//     later control points coincide with the endpoint. If every fallback in
//     the chain is degenerate (all four points coincide), the tangent is the
//     zero Vector.
//
// Close contributes the implicit line from the current point back to the
// subpath's start point, exactly like an explicit LineTo to that point
// (including the same zero-length fallback when the subpath is already
// there) — and that implicit segment's tangent updates the SUBPATH START
// vertex's in-tangent (it has none from the initial MoveTo alone). A
// subpath with no drawing segments before its Close (bare "M Z") produces no
// implicit line (nothing to close) and leaves the start vertex's in-tangent
// zero. A repeated Close (Z Z Z, or Z immediately after another Z) after the
// first is a no-op: the pen is already at the subpath start, so each
// additional Close is a zero-length implicit line contributing nothing
// further.
//
// Multiple subpaths: every MoveTo (including the first) produces a Vertex
// with IsSubpathStart set. Per SVG, marker-start applies only to the very
// FIRST vertex of the WHOLE path (index 0 in the returned slice); every
// other MoveTo is an interior vertex for marker-mid purposes despite also
// starting a new subpath — callers select on the returned slice's index, not
// on IsSubpathStart alone.
func Vertices(p *Path) []Vertex {
	if p == nil || len(p.Segments) == 0 {
		return nil
	}

	var verts []Vertex
	// subpathStart indexes, into verts, the vertex that began the subpath
	// currently in progress; -1 when no subpath is open yet.
	subpathStart := -1
	var cur Point

	// setOut sets verts[idx].OutTangent, called exactly once per vertex (the
	// vertex that STARTS a drawn segment).
	setOut := func(idx int, v Vector) {
		verts[idx].OutTangent = v
	}
	// setIn sets verts[idx].InTangent, called exactly once per vertex except
	// the very first (the vertex that ENDS a drawn segment).
	setIn := func(idx int, v Vector) {
		verts[idx].InTangent = v
	}

	for _, s := range p.Segments {
		switch s.Kind {
		case MoveTo:
			verts = append(verts, Vertex{Pos: s.P0, IsSubpathStart: true})
			subpathStart = len(verts) - 1
			cur = s.P0

		case LineTo:
			if subpathStart < 0 {
				// Malformed path (LineTo with no preceding MoveTo): treat cur
				// as an implicit start rather than panic/index out of range.
				verts = append(verts, Vertex{Pos: cur, IsSubpathStart: true})
				subpathStart = len(verts) - 1
			}
			startIdx := len(verts) - 1
			dir := sub(s.P0, cur)
			setOut(startIdx, dir)
			verts = append(verts, Vertex{Pos: s.P0})
			setIn(len(verts)-1, dir)
			cur = s.P0

		case CubeTo:
			if subpathStart < 0 {
				verts = append(verts, Vertex{Pos: cur, IsSubpathStart: true})
				subpathStart = len(verts) - 1
			}
			startIdx := len(verts) - 1
			outDir := cubicOutTangent(cur, s.P0, s.P1, s.P2)
			setOut(startIdx, outDir)
			verts = append(verts, Vertex{Pos: s.P2})
			inDir := cubicInTangent(cur, s.P0, s.P1, s.P2)
			setIn(len(verts)-1, inDir)
			cur = s.P2

		case Close:
			if subpathStart < 0 || len(verts) == 0 {
				continue
			}
			start := verts[subpathStart]
			startIdx := len(verts) - 1
			dir := sub(start.Pos, cur)
			// The implicit close line's out-tangent belongs to the CURRENT
			// pen position (the last vertex so far in this subpath); its
			// in-tangent belongs to the subpath's start vertex. When cur is
			// already exactly the subpath's start (a bare "M Z", or a
			// repeated Z), dir is the zero Vector and both assignments are
			// harmless no-ops (they don't overwrite a previously-computed
			// non-zero in-tangent on the start vertex with garbage, since
			// zero here genuinely means "no additional direction").
			if !dir.IsZero() {
				setOut(startIdx, dir)
				setIn(subpathStart, dir)
			}
			cur = start.Pos
			// subpathStart stays as-is: a Z does not end the subpath for
			// vertex-walking purposes (SVG permits drawing more segments
			// after a Z, continuing from the subpath's start point; those
			// would need a preceding MoveTo in well-formed data, but nothing
			// here assumes that).
		}
	}

	return verts
}

// cubicOutTangent returns the out-tangent at a cubic segment's START vertex
// (cur): P0-cur, falling back to P1-cur then P2-cur when earlier points
// coincide with cur, per Vertices' documented fallback chain.
func cubicOutTangent(cur, p0, p1, p2 Point) Vector {
	if v := sub(p0, cur); !v.IsZero() {
		return v
	}
	if v := sub(p1, cur); !v.IsZero() {
		return v
	}
	return sub(p2, cur)
}

// cubicInTangent returns the in-tangent at a cubic segment's END vertex (p2):
// P2-P1, falling back to P2-P0 then P2-cur when later points coincide with
// p2, per Vertices' documented fallback chain.
func cubicInTangent(cur, p0, p1, p2 Point) Vector {
	if v := sub(p2, p1); !v.IsZero() {
		return v
	}
	if v := sub(p2, p0); !v.IsZero() {
		return v
	}
	return sub(p2, cur)
}
