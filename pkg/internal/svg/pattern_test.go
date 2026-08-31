package svg

import (
	"testing"
)

// findFirstShape returns the first *Shape found by a depth-first walk of n,
// or nil if none exists. Test helper for asserting on a parsed document's
// resolved paint servers without needing pkg/svg/draw's rendering machinery.
func findFirstShape(n Node) *Shape {
	switch v := n.(type) {
	case *Shape:
		return v
	case *Group:
		for _, kid := range v.Kids {
			if s := findFirstShape(kid); s != nil {
				return s
			}
		}
	}
	return nil
}

// TestPatternResolvesFillPattern verifies a plain <pattern> reference
// resolves into Shape.FillPattern (not FillGradient), with a non-nil tile.
func TestPatternResolvesFillPattern(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="10" height="10">
	    <rect width="10" height="10" fill="red"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("no Shape found in scene")
	}
	if s.FillGradient != nil {
		t.Error("FillGradient should be nil for a pattern reference")
	}
	if s.FillPattern == nil {
		t.Fatal("FillPattern is nil, want a resolved pattern")
	}
	if s.FillPattern.Tile() == nil || len(s.FillPattern.Tile().Kids) != 1 {
		t.Errorf("Tile().Kids = %v, want exactly 1 (the tile's <rect>)", s.FillPattern.Tile())
	}
}

// TestPatternHrefInheritsAttrsAndTileContent verifies a <pattern> with no
// width/height/children of its own inherits ALL of them (attrs
// first-defined-wins, tile content all-or-nothing) from its href target,
// exactly like a gradient's href chain (Task 4) — patterns share the same
// resolver.
func TestPatternHrefInheritsAttrsAndTileContent(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="40" height="40">
	  <pattern id="base" patternUnits="userSpaceOnUse" width="10" height="10">
	    <rect width="10" height="10" fill="red"/>
	  </pattern>
	  <pattern id="derived" href="#base"/>
	  <rect x="0" y="0" width="40" height="40" fill="url(#derived)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil || s.FillPattern == nil {
		t.Fatal("expected a resolved FillPattern via href inheritance")
	}
	if s.FillPattern.Tile() == nil || len(s.FillPattern.Tile().Kids) != 1 {
		t.Errorf("inherited Tile().Kids = %v, want exactly 1", s.FillPattern.Tile())
	}
	_, _, w, h := s.FillPattern.Cell()
	if w != 10 || h != 10 {
		t.Errorf("inherited cell size = (%v,%v), want (10,10)", w, h)
	}
}

// TestPatternContentUnitsObjectBoundingBox verifies patternContentUnits
// resolution does not panic and produces a non-identity content mapping
// scaled by the referencing shape's bounding box, distinguishing it from the
// (default) userSpaceOnUse identity mapping.
func TestPatternContentUnitsObjectBoundingBox(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" patternContentUnits="objectBoundingBox" width="10" height="10">
	    <rect x="0" y="0" width="0.5" height="0.5" fill="red"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil || s.FillPattern == nil {
		t.Fatal("expected a resolved FillPattern")
	}
	cm := s.FillPattern.ContentMatrix()
	// objectBoundingBox content units scale content coords by the 40x40
	// referencing shape's bbox: a content-space (0.5,0.5) point should map
	// to (20,20), not stay at (0.5,0.5) (which the identity/userSpaceOnUse
	// mapping would produce).
	x, y := cm.Apply(0.5, 0.5)
	if x != 20 || y != 20 {
		t.Errorf("ContentMatrix().Apply(0.5,0.5) = (%v,%v), want (20,20)", x, y)
	}
}

// TestPatternViewBoxMapsContent verifies a pattern's own viewBox establishes
// the content coordinate mapping, taking precedence over
// patternContentUnits, exactly like the root <svg>'s viewBox.
func TestPatternViewBoxMapsContent(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="40" height="40">
	  <pattern id="p" patternUnits="userSpaceOnUse" width="10" height="10" viewBox="0 0 100 100" preserveAspectRatio="none">
	    <rect x="0" y="0" width="50" height="50" fill="red"/>
	  </pattern>
	  <rect x="0" y="0" width="40" height="40" fill="url(#p)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil || s.FillPattern == nil {
		t.Fatal("expected a resolved FillPattern")
	}
	cm := s.FillPattern.ContentMatrix()
	// viewBox "0 0 100 100" mapped into a 10x10 cell with preserveAspectRatio
	// "none" scales content x/y by 10/100 = 0.1: content point (100,100)
	// (the viewBox's far corner) should map to (10,10) (the cell's far
	// corner), not stay at (100,100).
	x, y := cm.Apply(100, 100)
	if x != 10 || y != 10 {
		t.Errorf("ContentMatrix().Apply(100,100) = (%v,%v), want (10,10)", x, y)
	}
}

// TestPatternObjectBoundingBoxUnitsDegenerateBBoxPaintsNothing verifies a
// patternUnits="objectBoundingBox" (the default) pattern over a shape with a
// degenerate bounding box (a horizontal line: zero height, but shapePath
// still produces a path for it, unlike a zero-width rect which shapePath
// drops outright) resolves to no FillPattern at all — mirroring
// resolveGradient's identical rule for gradients — and does not panic.
func TestPatternObjectBoundingBoxUnitsDegenerateBBoxPaintsNothing(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="40" height="40">
	  <pattern id="p" width="0.5" height="0.5">
	    <rect width="10" height="10" fill="red"/>
	  </pattern>
	  <line x1="0" y1="20" x2="40" y2="20" stroke="url(#p)" stroke-width="4"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("expected a Shape for the <line> (shapePath does not drop it)")
	}
	if s.StrokePattern != nil {
		t.Errorf("StrokePattern = %#v, want nil (objectBoundingBox pattern over a degenerate bbox must not resolve)", s.StrokePattern)
	}
}
