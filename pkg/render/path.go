package render

// Point is a 2-D point in device space (pixels), origin at top-left, y down.
type Point struct {
	X, Y float64
}

// SegmentKind identifies a path segment type.
type SegmentKind int

const (
	// MoveTo starts a new subpath at P0.
	MoveTo SegmentKind = iota
	// LineTo draws a straight line to P0.
	LineTo
	// CubeTo draws a cubic Bézier with control points P0, P1 and endpoint P2.
	CubeTo
	// Close closes the current subpath.
	Close
)

// Segment is one element of a path. Which Point fields are meaningful depends on
// Kind: MoveTo/LineTo use P0; CubeTo uses P0,P1,P2; Close uses none.
type Segment struct {
	Kind       SegmentKind
	P0, P1, P2 Point
}

// Path is a sequence of segments already transformed into device space.
type Path struct {
	Segments []Segment
}

// MoveTo appends a move-to segment.
func (p *Path) MoveTo(x, y float64) {
	p.Segments = append(p.Segments, Segment{Kind: MoveTo, P0: Point{x, y}})
}

// LineTo appends a line-to segment.
func (p *Path) LineTo(x, y float64) {
	p.Segments = append(p.Segments, Segment{Kind: LineTo, P0: Point{x, y}})
}

// CubeTo appends a cubic Bézier segment.
func (p *Path) CubeTo(x0, y0, x1, y1, x2, y2 float64) {
	p.Segments = append(p.Segments, Segment{
		Kind: CubeTo,
		P0:   Point{x0, y0}, P1: Point{x1, y1}, P2: Point{x2, y2},
	})
}

// Close appends a close-subpath segment.
func (p *Path) Close() {
	p.Segments = append(p.Segments, Segment{Kind: Close})
}

// Empty reports whether the path has no segments.
func (p *Path) Empty() bool { return len(p.Segments) == 0 }

// Reset clears the path for reuse, retaining capacity.
func (p *Path) Reset() { p.Segments = p.Segments[:0] }

// Clone returns a deep copy of the path.
func (p *Path) Clone() *Path {
	if p == nil {
		return nil
	}
	segs := make([]Segment, len(p.Segments))
	copy(segs, p.Segments)
	return &Path{Segments: segs}
}
