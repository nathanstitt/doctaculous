package raster

import (
	"fmt"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/function"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// FillShading paints the active clip region (or the whole image if unclipped) by
// evaluating shader at each pixel. Each pixel center is mapped from device space
// back into shading (user) space via the inverse of ctm, then handed to
// shader.ColorAt; a pixel where ColorAt reports paint=false is left untouched.
// Painted colors composite through the clip coverage and the named blend mode,
// reusing the same blend/over path as solid fills, so shadings honor /BM exactly
// like other paint operators.
func (d *Device) FillShading(shader render.Shader, ctm render.Matrix, blendMode string) {
	if shader == nil {
		return
	}
	inv, ok := invert(ctm)
	if !ok {
		return // singular CTM: nothing maps to shading space
	}
	clip := d.activeClip()
	b := d.img.Bounds()
	if clip != nil {
		// Only the clip's bounding box can have non-zero coverage; iterate just it.
		b = b.Intersect(clip.Bounds())
	}
	if b.Empty() {
		return
	}
	sep, isSep := separableBlends[blendMode]
	nonsep, isNonsep := nonSeparableBlends[blendMode]

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cov := uint8(255)
			if clip != nil {
				if cov = clip.AlphaAt(x, y).A; cov == 0 {
					continue
				}
			}
			ux, uy := inv.Apply(float64(x)+0.5, float64(y)+0.5)
			c, paint := shader.ColorAt(ux, uy)
			if !paint {
				continue
			}
			a := mulU8(c.A, cov)
			if a == 0 {
				continue
			}
			src := c
			if isSep || isNonsep {
				dst := d.img.RGBAAt(x, y)
				src = blendSource(dst, c, sep, nonsep, isSep)
			}
			over(d.img, x, y, src, a)
		}
	}
}

// shading evaluates a PDF shading dictionary (ISO 32000-1 §8.7.4.5): given a
// point in shading space it returns the color to paint there. Axial (Type 2),
// radial (Type 3), and function-based (Type 1) shadings are supported; mesh
// shadings (Types 4–7) are handled separately in shading_mesh.go.
//
// A shading is built once per `sh` operator (or per shading-pattern fill) and
// sampled per device pixel via ColorAt (the render.Shader method), so
// construction does the costly work (function parse, color-space classification)
// up front and ColorAt stays a cheap arithmetic + Function.Eval call.
type shading struct {
	shadingType int
	csKind      csKind        // how Function outputs (or /Background) map to RGB
	fn          function.Func // /Function: parametric value(s) → color components
	domain      [2]float64    // /Domain [t0 t1] for axial/radial (default [0 1])

	// Axial (Type 2) geometry.
	axis [4]float64 // x0 y0 x1 y1

	// Radial (Type 3) geometry.
	circles [6]float64 // x0 y0 r0 x1 y1 r1

	// Function-based (Type 1) geometry.
	fnDomain [4]float64 // x0 x1 y0 y1
	fnInv    render.Matrix
	fnHasInv bool

	extend [2]bool // /Extend [b0 b1] for axial/radial

	// background is the color painted for points that fall outside the shading
	// (used when the shading is a pattern fill). hasBackground reports presence.
	background    color.RGBA
	hasBackground bool
}

// newShader parses a shading dictionary into a render.Shader. Axial (Type 2),
// radial (Type 3), and function-based (Type 1) shadings build a *shading; mesh
// shadings (Types 4–7) route through newMeshShader (shading_mesh.go). It returns
// an error for unsupported types or malformed geometry, so the caller degrades
// gracefully with a log rather than rendering garbage.
func newShader(doc *pdf.Document, dict pdf.Dict) (render.Shader, error) {
	st, ok := doc.GetInt(dict["ShadingType"])
	if !ok {
		return nil, fmt.Errorf("shading: missing /ShadingType")
	}
	if st >= 4 && st <= 7 {
		return newMeshShader(doc, dict)
	}
	s := &shading{shadingType: st}

	// /ColorSpace classifies how Function outputs map to RGB. Reuse the image
	// color-space resolver (component-count based); bpc is irrelevant here so we
	// pass 8.
	cs, err := resolveImageCS(doc, dict["ColorSpace"], 8, nil)
	if err != nil {
		return nil, fmt.Errorf("shading: color space: %w", err)
	}
	s.csKind = cs.kind

	// /Background (optional): a component array in the shading's color space.
	if bg := doc.GetArray(dict["Background"]); len(bg) > 0 {
		comps := make([]float64, len(bg))
		for i, e := range bg {
			comps[i], _ = pdf.Number(doc.Resolve(e))
		}
		s.background = s.toRGBA(comps)
		s.hasBackground = true
	}

	switch st {
	case 1:
		return s, s.initFunctionBased(doc, dict)
	case 2:
		return s, s.initAxial(doc, dict)
	case 3:
		return s, s.initRadial(doc, dict)
	default:
		return nil, fmt.Errorf("shading: unsupported /ShadingType %d", st)
	}
}

// toRGBA converts shading color components to an opaque RGBA via the shading's
// color-space kind (reusing the image converter).
func (s *shading) toRGBA(comps []float64) color.RGBA {
	c := componentsToRGBA(s.csKind, comps)
	c.A = 0xFF
	return c
}

// colorAt maps a parametric value t through /Function to an RGBA color. When no
// function is present the components are taken directly (rare for shadings, but
// tolerated).
func (s *shading) colorAt(t float64) color.RGBA {
	if s.fn == nil {
		return s.toRGBA([]float64{t})
	}
	return s.toRGBA(s.fn.Eval([]float64{t}))
}

func (s *shading) initAxial(doc *pdf.Document, dict pdf.Dict) error {
	coords := doc.GetArray(dict["Coords"])
	if len(coords) < 4 {
		return fmt.Errorf("shading: axial /Coords needs 4 numbers, got %d", len(coords))
	}
	for i := 0; i < 4; i++ {
		s.axis[i], _ = pdf.Number(doc.Resolve(coords[i]))
	}
	s.domain = readDomain2(doc, dict["Domain"])
	s.extend = readExtend(doc, dict["Extend"])
	fn, err := function.Parse(doc, dict["Function"])
	if err != nil {
		return fmt.Errorf("shading: axial /Function: %w", err)
	}
	s.fn = fn
	return nil
}

func (s *shading) initRadial(doc *pdf.Document, dict pdf.Dict) error {
	coords := doc.GetArray(dict["Coords"])
	if len(coords) < 6 {
		return fmt.Errorf("shading: radial /Coords needs 6 numbers, got %d", len(coords))
	}
	for i := 0; i < 6; i++ {
		s.circles[i], _ = pdf.Number(doc.Resolve(coords[i]))
	}
	s.domain = readDomain2(doc, dict["Domain"])
	s.extend = readExtend(doc, dict["Extend"])
	fn, err := function.Parse(doc, dict["Function"])
	if err != nil {
		return fmt.Errorf("shading: radial /Function: %w", err)
	}
	s.fn = fn
	return nil
}

func (s *shading) initFunctionBased(doc *pdf.Document, dict pdf.Dict) error {
	// /Domain [x0 x1 y0 y1], default [0 1 0 1].
	s.fnDomain = [4]float64{0, 1, 0, 1}
	if d := doc.GetArray(dict["Domain"]); len(d) >= 4 {
		for i := 0; i < 4; i++ {
			s.fnDomain[i], _ = pdf.Number(doc.Resolve(d[i]))
		}
	}
	// /Matrix maps shading space to the function's domain; we invert it to take
	// device-derived shading-space points back into domain coords.
	m := render.Identity
	if mm := doc.GetArray(dict["Matrix"]); len(mm) == 6 {
		var v [6]float64
		for i, e := range mm {
			v[i], _ = pdf.Number(doc.Resolve(e))
		}
		m = render.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
	}
	if inv, ok := invert(m); ok {
		s.fnInv, s.fnHasInv = inv, true
	}
	fn, err := function.Parse(doc, dict["Function"])
	if err != nil {
		return fmt.Errorf("shading: function-based /Function: %w", err)
	}
	s.fn = fn
	return nil
}

// ColorAt returns the color at shading-space point (x,y), implementing
// render.Shader. ok=false means the point lies outside the shading and the
// shading is not extended there, so the caller must leave the backdrop untouched
// (or paint /Background when used as a pattern fill — that choice is the caller's,
// via background/hasBackground).
func (s *shading) ColorAt(x, y float64) (color.RGBA, bool) {
	switch s.shadingType {
	case 1:
		return s.atFunctionBased(x, y)
	case 2:
		return s.atAxial(x, y)
	case 3:
		return s.atRadial(x, y)
	default:
		return color.RGBA{}, false
	}
}

func (s *shading) atAxial(x, y float64) (color.RGBA, bool) {
	x0, y0, x1, y1 := s.axis[0], s.axis[1], s.axis[2], s.axis[3]
	dx, dy := x1-x0, y1-y0
	dd := dx*dx + dy*dy
	var sval float64
	if dd == 0 {
		// Degenerate axis (the two points coincide): the gradient is a single
		// color at t = domain start.
		sval = 0
	} else {
		sval = ((x-x0)*dx + (y-y0)*dy) / dd
	}
	switch {
	case sval < 0:
		if !s.extend[0] {
			return color.RGBA{}, false
		}
		sval = 0
	case sval > 1:
		if !s.extend[1] {
			return color.RGBA{}, false
		}
		sval = 1
	}
	t := s.domain[0] + sval*(s.domain[1]-s.domain[0])
	return s.colorAt(t), true
}

// atRadial finds the color at (x,y) for a radial (Type 3) shading. The shading is
// a family of circles parameterized by s∈[0,1]: center C(s)=lerp(C0,C1,s) and
// radius r(s)=lerp(r0,r1,s). A device point is colored by the LARGEST s for which
// the point lies on circle s (so nearer circles paint over farther ones), honoring
// /Extend beyond [0,1]. We solve |P − C(s)| = r(s) for s — a quadratic in s.
func (s *shading) atRadial(x, y float64) (color.RGBA, bool) {
	x0, y0, r0 := s.circles[0], s.circles[1], s.circles[2]
	x1, y1, r1 := s.circles[3], s.circles[4], s.circles[5]
	cdx, cdy, dr := x1-x0, y1-y0, r1-r0
	px, py := x-x0, y-y0

	// |P - C(s)|^2 = r(s)^2 expands to a*s^2 + b*s + c = 0.
	a := cdx*cdx + cdy*cdy - dr*dr
	b := -2 * (px*cdx + py*cdy + r0*dr)
	c := px*px + py*py - r0*r0

	// Collect candidate s values (largest preferred), keeping those with r(s) ≥ 0
	// and within the extended domain.
	best := math.Inf(-1)
	found := false
	consider := func(sv float64) {
		// Map s into [0,1] honoring Extend; reject if outside and not extended.
		cs := sv
		switch {
		case sv < 0:
			if !s.extend[0] {
				return
			}
			cs = 0
		case sv > 1:
			if !s.extend[1] {
				return
			}
			cs = 1
		}
		if r0+cs*dr < 0 { // radius must be non-negative on the painted circle
			return
		}
		if sv > best {
			best = sv
			found = true
		}
	}

	const eps = 1e-9
	if math.Abs(a) < eps {
		// Linear: b*s + c = 0.
		if math.Abs(b) > eps {
			consider(-c / b)
		}
	} else {
		disc := b*b - 4*a*c
		if disc >= 0 {
			sq := math.Sqrt(disc)
			consider((-b + sq) / (2 * a))
			consider((-b - sq) / (2 * a))
		}
	}
	if !found {
		return color.RGBA{}, false
	}
	cs := best
	if cs < 0 {
		cs = 0
	} else if cs > 1 {
		cs = 1
	}
	t := s.domain[0] + cs*(s.domain[1]-s.domain[0])
	return s.colorAt(t), true
}

func (s *shading) atFunctionBased(x, y float64) (color.RGBA, bool) {
	dx, dy := x, y
	if s.fnHasInv {
		dx, dy = s.fnInv.Apply(x, y)
	}
	// Points outside the function's /Domain are not painted.
	if dx < s.fnDomain[0] || dx > s.fnDomain[1] || dy < s.fnDomain[2] || dy > s.fnDomain[3] {
		return color.RGBA{}, false
	}
	if s.fn == nil {
		return color.RGBA{}, false
	}
	return s.toRGBA(s.fn.Eval([]float64{dx, dy})), true
}

// readDomain2 reads a 2-element /Domain [t0 t1], defaulting to [0 1].
func readDomain2(doc *pdf.Document, o pdf.Object) [2]float64 {
	d := [2]float64{0, 1}
	if arr := doc.GetArray(o); len(arr) >= 2 {
		d[0], _ = pdf.Number(doc.Resolve(arr[0]))
		d[1], _ = pdf.Number(doc.Resolve(arr[1]))
	}
	return d
}

// readExtend reads a 2-element /Extend [b0 b1] of booleans, defaulting to
// [false false].
func readExtend(doc *pdf.Document, o pdf.Object) [2]bool {
	var e [2]bool
	arr := doc.GetArray(o)
	for i := 0; i < 2 && i < len(arr); i++ {
		if b, ok := doc.Resolve(arr[i]).(pdf.Boolean); ok {
			e[i] = bool(b)
		}
	}
	return e
}
