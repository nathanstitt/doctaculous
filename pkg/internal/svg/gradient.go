package svg

import (
	"math"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// paintServer is a fully-resolved gradient, ready to paint: a render.Shader
// evaluated in the gradient's own local coordinate space, plus the matrix
// that maps that local space into the referencing shape's user space (the
// space Shape.Path is defined in, pre-Shape.M). A Shape carries this instead
// of a paint-server id because docIndex (and the *resolvedServer it resolves
// through) is discarded when Parse returns, and Document must stay
// side-table-free so it can be shared lock-free across the engine's parallel
// page-render fan-out — see the package doc on sceneBuilder.idx.
type paintServer struct {
	shader render.Shader
	m      render.Matrix // local (gradient) space -> shape's user space
}

// Shader returns the resolved gradient's render.Shader, evaluated in the
// gradient's own local coordinate space (see Matrix for how to map into
// device space). Exported so pkg/svg/draw (a separate package) can drive
// Device.FillShading directly, without exporting the paintServer type
// itself — the same pattern as Style's FillPaint/StrokePaint accessors.
func (ps *paintServer) Shader() render.Shader { return ps.shader }

// Matrix returns the matrix mapping the gradient's local coordinate space
// into the referencing Shape's own user space (pre-Shape.M). The caller
// composes this with Shape.M and the accumulated scene-walk matrix to get
// the full local-space-to-device CTM FillShading needs.
func (ps *paintServer) Matrix() render.Matrix { return ps.m }

// viewport carries the current SVG viewport's size in user units, the
// reference frame a userSpaceOnUse gradient's percentage coordinates resolve
// against (SVG's "Units" section: x-like percentages against width, y-like
// against height, and a mixed-axis value like r against
// sqrt(w^2+h^2)/sqrt(2)). This engine has exactly one viewport in play at any
// time — nested <svg> and <use> (which would each establish their own) are
// not yet implemented — so a single value threaded from Parse down to
// buildShape is sufficient; see sceneBuilder.vp.
type viewport struct {
	w, h float64
}

// diag returns the SVG-spec-defined reference length for a mixed-axis
// percentage (e.g. a radial gradient's r/fr): sqrt(w^2+h^2)/sqrt(2).
func (v viewport) diag() float64 {
	return math.Sqrt(v.w*v.w+v.h*v.h) / math.Sqrt2
}

// resolveGradient resolves a fill/stroke paint-server reference (the "#id"
// fragment FillServer/StrokeServer report) against the document, building a
// paintServer for it. path is the shape's own PRE-TRANSFORM geometry, used
// to compute the objectBoundingBox per SVG's definition of that unit space.
// vp is the current viewport, used only to resolve a userSpaceOnUse
// percentage coordinate (rare in practice, but spec-correct). The returned
// paintServer's matrix carries only the gradient's own space (bbox-or-user
// mapping, then gradientTransform) up to the shape's user space — Shape.M
// and the accumulated scene-walk matrix are composed on top of it later, at
// paint time, since only the walk knows them.
//
// ok is false when: id does not resolve to a paint server at all (unknown
// id, or a non-gradient/non-pattern target — resolver.resolve already
// reports that), the resolved server has no stops (paints nothing per SVG),
// the server is a <pattern> (buildShape routes those to resolvePattern
// instead — not this function's job), or the gradient is
// objectBoundingBox-relative over a shape with a degenerate bounding box
// (zero width or height): per the SVG spec, a gradient cannot be established
// in that case and the element must not be painted with it.
func resolveGradient(id string, resolver *paintServerResolver, path *render.Path, vp viewport, logf func(string, ...any)) (paintServer, bool) {
	ps, ok := resolver.resolve(id)
	if !ok || ps.kind == "pattern" {
		return paintServer{}, false
	}
	if ps.stops == nil {
		// Zero stops: SVG says the paint server paints nothing (distinct from
		// "fell back to solid color" — the caller already knows to treat
		// FillServer/StrokeServer as authoritative here).
		return paintServer{}, false
	}

	userSpace := ps.attrs["gradientUnits"] == "userSpaceOnUse"

	bboxM := render.Identity
	if !userSpace {
		minX, minY, maxX, maxY, hasBounds := path.Bounds()
		w, h := maxX-minX, maxY-minY
		if !hasBounds || w == 0 || h == 0 {
			// Degenerate bounding box: SVG2 §13.2.2 (and the equivalent
			// wording for radialGradient) requires the element not be
			// painted with this gradient at all when objectBoundingBox
			// geometry can't be established. Make that explicit here rather
			// than let a zero-scale matrix invert to garbage downstream.
			return paintServer{}, false
		}
		bboxM = render.Scale(w, h).Mul(render.Translate(minX, minY))
	}

	gradM := render.Identity
	if s, ok := ps.attrs["gradientTransform"]; ok {
		m, ok := parseTransform(s)
		if !ok {
			logf("svg: ignoring gradientTransform=%q on paint server %q: unparseable", s, id)
		} else {
			gradM = m
		}
	}

	// gradientTransform first, then bboxM (or Identity for userSpaceOnUse):
	// gradientTransform is defined to apply WITHIN the already-established
	// gradient coordinate system (objectBoundingBox or userSpaceOnUse) — i.e.
	// it transforms gradient-local coordinates, and the result is then mapped
	// into user space by the bbox (or identity) mapping. Per Matrix.Mul(m, n)
	// = "m first, then n" semantics, that composition is gradM.Mul(bboxM).
	localToUser := gradM.Mul(bboxM)

	spread := parseSpreadMethod(ps.attrs["spreadMethod"])

	shadingStops := ps.stops.shadingStops()

	var shader render.Shader
	switch ps.kind {
	case "linearGradient":
		x1, y1, x2, y2 := linearGradientCoords(ps.attrs, userSpace, vp)
		shader = raster.NewAxialShader(x1, y1, x2, y2, ps.stops, shadingStops, spread)
	case "radialGradient":
		cx, cy, r, fx, fy := radialGradientCoords(ps.attrs, userSpace, vp)
		shader = raster.NewRadialShader(fx, fy, 0, cx, cy, r, ps.stops, shadingStops, spread)
	default:
		return paintServer{}, false
	}

	return paintServer{shader: shader, m: localToUser}, true
}

// parseSpreadMethod resolves the spreadMethod attribute (pad|reflect|repeat),
// defaulting to pad for an absent or unrecognized value.
func parseSpreadMethod(v string) raster.Spread {
	switch v {
	case "reflect":
		return raster.SpreadReflect
	case "repeat":
		return raster.SpreadRepeat
	default:
		return raster.SpreadPad
	}
}

// gradientCoord resolves one x/y/r-like gradient coordinate attribute. def is
// the value to use when the attribute is absent or unparseable, already
// expressed in whichever unit space userSpace selects (a [0,1] fraction for
// objectBoundingBox, or a user-unit length for userSpaceOnUse — callers
// compute the right default per space, since "100%" means different things
// in each). ref is the reference length a userSpaceOnUse PERCENTAGE value
// resolves against (vp.w, vp.h, or vp.diag(), chosen by the caller per
// axis); it is unused in objectBoundingBox space.
//
// In objectBoundingBox space, a bare number OR a percentage is a FRACTION of
// the unit bounding box (0.5 == 50%), parsed exactly like parseStopOffset's
// offset grammar but without clamping to [0,1] — a coordinate like x1 may
// legitimately fall outside that range (e.g. x1="-0.5"). In userSpaceOnUse
// space it is an ordinary length: a bare number is a user-unit length, and a
// percentage resolves against ref, via parseLength.
func gradientCoord(attrs map[string]string, name string, def float64, userSpace bool, ref float64) float64 {
	val, ok := attrs[name]
	if !ok {
		return def
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return def
	}
	if userSpace {
		v, ok := parseLength(val, ref)
		if !ok {
			return def
		}
		return v
	}
	isPct := strings.HasSuffix(val, "%")
	v, ok := parseNumber(strings.TrimSuffix(val, "%"))
	if !ok {
		return def
	}
	if isPct {
		v /= 100
	}
	return v
}

// linearGradientCoords resolves x1/y1/x2/y2, applying SVG's defaults
// (x1=0%, y1=0%, x2=100%, y2=0%) in whichever unit space is active. In
// objectBoundingBox space the defaults are plain fractions (0, 0, 1, 0); in
// userSpaceOnUse space a bare default fraction would be wrong (x2's default
// must mean "100% of the viewport width", not the literal user-unit value
// 1), so the userSpaceOnUse defaults are computed directly against vp
// instead of being run back through the objectBoundingBox fraction.
func linearGradientCoords(attrs map[string]string, userSpace bool, vp viewport) (x1, y1, x2, y2 float64) {
	if userSpace {
		x1 = gradientCoord(attrs, "x1", 0, true, vp.w)
		y1 = gradientCoord(attrs, "y1", 0, true, vp.h)
		x2 = gradientCoord(attrs, "x2", vp.w, true, vp.w)
		y2 = gradientCoord(attrs, "y2", 0, true, vp.h)
		return x1, y1, x2, y2
	}
	x1 = gradientCoord(attrs, "x1", 0, false, 0)
	y1 = gradientCoord(attrs, "y1", 0, false, 0)
	x2 = gradientCoord(attrs, "x2", 1, false, 0)
	y2 = gradientCoord(attrs, "y2", 0, false, 0)
	return x1, y1, x2, y2
}

// radialGradientCoords resolves cx/cy/r (default 50%/50%/50%) and fx/fy
// (default cx/cy, per SVG), applying SVG's unit-space rules identically to
// linearGradientCoords: r's default and any percentage value for it resolve
// against the mixed-axis diagonal reference (vp.diag()), not vp.w or vp.h
// alone, per the SVG "Units" section.
func radialGradientCoords(attrs map[string]string, userSpace bool, vp viewport) (cx, cy, r, fx, fy float64) {
	if userSpace {
		cx = gradientCoord(attrs, "cx", vp.w/2, true, vp.w)
		cy = gradientCoord(attrs, "cy", vp.h/2, true, vp.h)
		r = gradientCoord(attrs, "r", vp.diag()/2, true, vp.diag())
		fx = cx
		if _, ok := attrs["fx"]; ok {
			fx = gradientCoord(attrs, "fx", cx, true, vp.w)
		}
		fy = cy
		if _, ok := attrs["fy"]; ok {
			fy = gradientCoord(attrs, "fy", cy, true, vp.h)
		}
		return cx, cy, r, fx, fy
	}

	cx = gradientCoord(attrs, "cx", 0.5, false, 0)
	cy = gradientCoord(attrs, "cy", 0.5, false, 0)
	r = gradientCoord(attrs, "r", 0.5, false, 0)
	fx = cx
	if _, ok := attrs["fx"]; ok {
		fx = gradientCoord(attrs, "fx", cx, false, 0)
	}
	fy = cy
	if _, ok := attrs["fy"]; ok {
		fy = gradientCoord(attrs, "fy", cy, false, 0)
	}
	return cx, cy, r, fx, fy
}
