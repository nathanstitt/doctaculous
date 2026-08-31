package svgwrite

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// FillShading fills the active clip by evaluating shader.
//
// A Shader that can describe its own geometry (render.ShadingDescriber) is
// emitted as a native <linearGradient>/<radialGradient>, which stays
// resolution-independent and is a few hundred bytes. Anything else — a mesh
// shading, or a describer declining this instance — is sampled per pixel and
// embedded as an <image>, because dropping it would lose content.
//
// SVG accepts strictly more gradients here than PDF does: pdfwrite must
// decline non-opaque stops and any spread but pad, whereas SVG expresses both
// natively via stop-opacity and spreadMethod. So gradients that pdfwrite has
// to rasterize stay vector in this backend.
func (d *Device) FillShading(shader render.Shader, ctm render.Matrix, blendMode string) {
	if shader == nil {
		return
	}
	if desc, ok := describeShading(shader); ok {
		if d.fillNativeGradient(desc, ctm, blendMode) {
			return
		}
	}
	d.rasterizeShading(shader, ctm, blendMode)
}

// describeShading asks a Shader to describe itself.
//
// Note that a wrapper around a Shader (e.g. one folding in a constant alpha)
// must implement ShadingDescriber and delegate, or the description is lost and
// the gradient silently rasterizes — see render.ShadingDescriber's doc comment.
func describeShading(s render.Shader) (render.ShadingDesc, bool) {
	sd, ok := s.(render.ShadingDescriber)
	if !ok {
		return render.ShadingDesc{}, false
	}
	return sd.DescribeShading()
}

// fillNativeGradient emits desc as a gradient and paints the clip region with
// it, reporting false if the geometry is degenerate and should be sampled.
func (d *Device) fillNativeGradient(desc render.ShadingDesc, ctm render.Matrix, blendMode string) bool {
	if len(desc.Stops) == 0 {
		return false
	}
	// The region to cover: FillShading paints the whole active clip, and the
	// <clipPath> elements already emitted restrict it to the exact shape.
	b := d.clipRect
	if b == nil {
		b = &clipBounds{0, 0, float64(d.wPx), float64(d.hPx)}
	}
	if b.maxX <= b.minX || b.maxY <= b.minY {
		return true // clipped to nothing: correctly paints nothing
	}

	id := d.id("grad")
	switch desc.Kind {
	case render.ShadingAxial:
		x0, y0, x1, y1 := desc.Coords[0], desc.Coords[1], desc.Coords[2], desc.Coords[3]
		if x0 == x1 && y0 == y1 {
			return false // zero-length axis has no native meaning
		}
		fmt.Fprintf(&d.defs,
			`<linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s"`,
			id, formatCoord(x0), formatCoord(y0), formatCoord(x1), formatCoord(y1))
	case render.ShadingRadial:
		fx, fy, fr := desc.Coords[0], desc.Coords[1], desc.Coords[2]
		cx, cy, cr := desc.Coords[3], desc.Coords[4], desc.Coords[5]
		if cr <= 0 {
			return false // an empty outer circle paints nothing native
		}
		// SVG's <radialGradient> has no focal RADIUS: fr must be 0 for the
		// native form to match. A nonzero focal radius is a PDF/SVG2 feature
		// this element cannot express, so sample it instead of emitting a
		// gradient that would render visibly wrong.
		if fr != 0 {
			return false
		}
		fmt.Fprintf(&d.defs,
			`<radialGradient id="%s" gradientUnits="userSpaceOnUse" cx="%s" cy="%s" r="%s" fx="%s" fy="%s"`,
			id, formatCoord(cx), formatCoord(cy), formatCoord(cr), formatCoord(fx), formatCoord(fy))
	default:
		return false
	}
	if s := spreadAttr(desc.Spread); s != "" {
		fmt.Fprintf(&d.defs, " spreadMethod=%q", s)
	}
	// The shading's geometry is in user space; ctm maps that to device space.
	if !isIdentity(ctm) {
		fmt.Fprintf(&d.defs, " gradientTransform=%q", matrixAttr(ctm))
	}
	d.defs.WriteString(">")
	writeStops(&d.defs, desc.Stops)
	if desc.Kind == render.ShadingAxial {
		d.defs.WriteString("</linearGradient>\n")
	} else {
		d.defs.WriteString("</radialGradient>\n")
	}

	fmt.Fprintf(d.buf, `<rect x="%s" y="%s" width="%s" height="%s" fill="url(#%s)"`,
		formatCoord(b.minX), formatCoord(b.minY),
		formatCoord(b.maxX-b.minX), formatCoord(b.maxY-b.minY), id)
	d.writeBlend(blendMode)
	d.buf.WriteString("/>\n")
	return true
}

// writeStops emits the gradient ramp.
func writeStops(sb *strings.Builder, stops []render.ShadingStop) {
	for _, s := range stops {
		hex, alpha := colorAttr(s.Color)
		fmt.Fprintf(sb, `<stop offset="%s" stop-color=%q`, formatCoord(s.Offset), hex)
		// stop-opacity is why SVG can express gradients PDF cannot: a
		// non-opaque stop forces pdfwrite to rasterize, but is native here.
		if alpha < 1 {
			fmt.Fprintf(sb, " stop-opacity=%q", formatCoord(alpha))
		}
		sb.WriteString("/>")
	}
}

// spreadAttr maps a spread mode to SVG's spreadMethod. Pad is the initial
// value and needs no attribute.
func spreadAttr(s render.SpreadMode) string {
	switch s {
	case render.SpreadReflect:
		return "reflect"
	case render.SpreadRepeat:
		return "repeat"
	default:
		return ""
	}
}

// rasterizeShading samples shader over the clip region and embeds the result.
//
// This is the honest fallback for a gradient with no native SVG form (a mesh
// shading, a radial with a focal radius). Sampling matches the raster backend's
// convention — pixel centers mapped back through the inverse CTM into shading
// space — so the sampled result agrees with a rasterized render of the page.
func (d *Device) rasterizeShading(shader render.Shader, ctm render.Matrix, blendMode string) {
	inv, ok := invertMatrix(ctm)
	if !ok {
		d.warnOnce("shading-singular", "svgwrite: shading has a singular transform; skipped")
		return
	}
	page := &clipBounds{0, 0, float64(d.wPx), float64(d.hPx)}
	b := intersectClip(d.clipRect, page)
	if b == nil {
		b = page
	}
	minX, minY := int(math.Floor(b.minX)), int(math.Floor(b.minY))
	maxX, maxY := int(math.Ceil(b.maxX)), int(math.Ceil(b.maxY))
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		return
	}
	d.warnOnce("shading-raster", "svgwrite: shading has no native SVG form; embedding a sampled image")

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	any := false
	for y := range h {
		for x := range w {
			px, py := float64(minX+x)+0.5, float64(minY+y)+0.5
			ux, uy := inv.Apply(px, py)
			c, paint := shader.ColorAt(ux, uy)
			if !paint {
				continue
			}
			img.SetRGBA(x, y, c)
			any = true
		}
	}
	if !any {
		return
	}
	// DrawImage maps the unit square through the matrix AND flips v, because
	// its contract is PDF image space (top row at v=1). This image was sampled
	// row 0 = top in device space, so the placement pre-flips to cancel that
	// out and land the rows where they were sampled from.
	place := render.Matrix{A: 1, D: -1, F: 1}.
		Mul(render.Scale(float64(w), float64(h))).
		Mul(render.Translate(float64(minX), float64(minY)))
	d.DrawImage(img, place, 1, blendMode)
}

// invertMatrix returns the inverse of an affine matrix, ok=false if singular.
func invertMatrix(m render.Matrix) (render.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det > -1e-12 && det < 1e-12 {
		return render.Matrix{}, false
	}
	id := 1 / det
	return render.Matrix{
		A: m.D * id, B: -m.B * id, C: -m.C * id, D: m.A * id,
		E: (m.C*m.F - m.D*m.E) * id, F: (m.B*m.E - m.A*m.F) * id,
	}, true
}
