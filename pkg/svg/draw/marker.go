package draw

import (
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// paintMarkers paints s's marker-start/-mid/-end at every vertex of s.Path,
// per SVG2 §11.6.7. sm is s's fully-accumulated device matrix (Shape.M
// composed with the caller's walk matrix — the SAME matrix dp (the
// already-transformed device-space path) was built from), so a marker's
// [0,Width]x[0,Height] viewport composes UNDER sm exactly like a Shape's own
// Path does: a marker painted on a rotated/skewed path inherits that
// rotation/skew, per spec (a marker's placement transform includes the
// referencing element's own user-space-to-device mapping, not just a
// translation to the vertex).
//
// strokeWidth is s's own resolved stroke-width in USER units (pre-sm), used
// when a referenced marker's markerUnits is the default "strokeWidth" — see
// resolveMarkerStrokeWidth for the exact fallback when the shape has no
// stroke at all.
func (r *Renderer) paintMarkers(dev render.Device, s *svg.Shape, sm render.Matrix, alpha float64, warned *warnFlags) {
	if s.MarkerStart == nil && s.MarkerMid == nil && s.MarkerEnd == nil {
		return
	}
	verts := render.Vertices(s.Path)
	if len(verts) == 0 {
		return
	}

	strokeWidth := resolveMarkerStrokeWidth(s.Style)
	last := len(verts) - 1

	for i, v := range verts {
		var m *svg.Marker
		switch {
		case i == 0:
			m = s.MarkerStart
		case i == last:
			m = s.MarkerEnd
		default:
			m = s.MarkerMid
		}
		if m == nil {
			continue
		}
		if warned.drawCalls >= maxDrawCalls {
			r.logDrawBudgetCapOnce(warned)
			return
		}
		angle := markerAngle(m, v, i == 0, i == last)
		r.paintOneMarker(dev, m, v.Pos, angle, strokeWidth, sm, alpha, warned)
	}
}

// resolveMarkerStrokeWidth returns the stroke-width (in user units) a
// markerUnits="strokeWidth" (the default) marker scales by: the shape's own
// resolved stroke-width PROPERTY VALUE, regardless of whether the shape
// actually paints a visible stroke. This must NOT gate on StrokePaint's
// ok=false (stroke="none", or simply no stroke color set at all): SVG2
// §11.6.7 scales by "the value of the stroke-width property" itself — see
// with-a-large-stroke.svg, which sets stroke-width="10" with NO stroke
// color and still expects markers scaled by 10. A shape with no stroke-width
// attribute at all resolves to the property's own default (1 user unit, per
// defaultStyle), which is exactly the fallback no-stroke-on-target.svg's
// <desc> describes ("marker will fallback to stroke-width=1") — that
// fallback falls out of StrokeWidthValue's ordinary default, not a special
// case here.
func resolveMarkerStrokeWidth(st svg.Style) float64 {
	return st.StrokeWidthValue()
}

// markerAngle resolves the rotation (radians) a marker paints at, given the
// vertex's tangents and whether this vertex is the path's overall start/end
// (not just "an endpoint of A segment" — see render.Vertices' doc comment on
// why marker-start/-end apply only at index 0/len-1 of the WHOLE path, never
// at an interior subpath boundary).
//
//   - A fixed orient="<angle>" (or the absent-attribute lacuna, 0) ignores
//     the path entirely.
//   - orient="auto" at the path's start/end vertex uses whichever of
//     out/in-tangent exists (a start vertex has no in-tangent unless its
//     subpath closed; an end vertex has no out-tangent) — falling back to 0
//     when NEITHER exists (a single-point subpath, e.g. zero-length-path-*.svg).
//   - orient="auto" at an INTERIOR vertex uses render.Bisector(in, out), per
//     the design's explicit callout that a mid vertex must bisect, not pick
//     one side arbitrarily.
//   - orient="auto-start-reverse" behaves exactly like "auto" everywhere
//     except the path's OWN start vertex (isFirst), where the result is
//     rotated by an additional 180° (SVG2: "this attribute value has no
//     effect on marker-mid or marker-end", and no effect at marker-start on
//     any subpath other than the very first — which render.Vertices already
//     ensures by construction, since isFirst is true only for index 0).
func markerAngle(m *svg.Marker, v render.Vertex, isFirst, isLast bool) float64 {
	if !m.Orient.Auto {
		return m.Orient.Angle
	}

	var dir render.Vector
	switch {
	case isFirst:
		dir = v.OutTangent
		if dir.IsZero() {
			dir = v.InTangent
		}
	case isLast:
		dir = v.InTangent
		if dir.IsZero() {
			dir = v.OutTangent
		}
	default:
		dir = render.Bisector(v.InTangent, v.OutTangent)
	}

	angle := 0.0
	if !dir.IsZero() {
		angle = dir.Angle()
	}
	if m.Orient.Reversed && isFirst {
		angle += math.Pi
	}
	return angle
}

// paintOneMarker paints one Marker instance at vertex position pos (already
// in the shape's own user space, i.e. pre-sm — sm maps it to device space
// below), rotated by angle radians, scaled per markerUnits.
func (r *Renderer) paintOneMarker(dev render.Device, m *svg.Marker, pos render.Point, angle, strokeWidth float64, sm render.Matrix, alpha float64, warned *warnFlags) {
	scale := 1.0
	if m.UnitsStrokeWidth {
		scale = strokeWidth
	}
	if scale <= 0 {
		// A zero/negative effective scale (stroke-width: 0 combined with
		// markerUnits="strokeWidth" — see zero-sized-stroke.svg) paints
		// nothing: there is no sensible non-degenerate marker size to fall
		// back to, and SVG's own rule for a zero stroke-width marker is "not
		// rendered".
		return
	}

	// placementM composes, innermost first: ViewBoxM (marker content ->
	// marker viewport), then translate(-RefX,-RefY) (the marker's reference
	// point lands at the origin), then rotate(angle), then scale(scale)
	// (markerUnits), then translate(pos) (the vertex, in the shape's own
	// user space), then sm (that user space -> device space, the same
	// matrix the shape's own Path composed under). SVG2 §11.6.7 defines
	// exactly this order: reference-point translation happens BEFORE
	// rotation and scaling, not after — a marker whose refX/refY isn't the
	// origin would otherwise orbit the vertex as it rotates instead of
	// pivoting in place.
	placementM := m.ViewBoxM.
		Mul(render.Translate(-m.RefX, -m.RefY)).
		Mul(render.Rotate(angle)).
		Mul(render.Scale(scale, scale)).
		Mul(render.Translate(pos.X, pos.Y)).
		Mul(sm)

	dev.Save()
	if m.ClipToViewport {
		// Markers clip to their viewport BY DEFAULT (SVG2: overflow:hidden
		// is the initial value for a marker, the opposite of most SVG
		// elements) — see default-clip.svg. The clip rect lives in the SAME
		// pre-ViewBoxM... no: the rect is [0,Width]x[0,Height], the marker's
		// OWN viewport, which content reaches AFTER ViewBoxM (viewBox maps
		// INTO [0,Width]x[0,Height], not out of it) — so the clip composes
		// under placementM MINUS the leading ViewBoxM term, i.e. under
		// everything from the ref-point translation onward. Composing
		// against placementM directly (which already has ViewBoxM baked in
		// front) would double-apply that mapping to the clip rect, clipping
		// the wrong region whenever a marker has a non-identity viewBox.
		clipM := render.Translate(-m.RefX, -m.RefY).
			Mul(render.Rotate(angle)).
			Mul(render.Scale(scale, scale)).
			Mul(render.Translate(pos.X, pos.Y)).
			Mul(sm)
		rect := viewportRectPathXY(m.Width, m.Height)
		dev.PushClip(render.TransformPath(rect, clipM), render.NonZero)
	}
	r.paint(dev, m.Kids, placementM, alpha, warned)
	dev.Restore()
}

// viewportRectPathXY builds the axis-aligned [0,w]x[0,h] rect path a
// <marker>'s default overflow:hidden clips its content to, mirroring
// viewportRectPath in pkg/svg's use.go (a <symbol>'s identical default) —
// duplicated rather than exported cross-package for four lines of pure
// geometry with no shared state.
func viewportRectPathXY(w, h float64) *render.Path {
	p := &render.Path{}
	p.MoveTo(0, 0)
	p.LineTo(w, 0)
	p.LineTo(w, h)
	p.LineTo(0, h)
	p.Close()
	return p
}
