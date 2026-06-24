// Package paint draws a laid-out page (pkg/layout) onto a render.Device. It is
// format-neutral: it consumes only positioned glyphs and rules in page space and
// knows nothing about DOCX/HTML/EPUB. Together with pkg/render/raster this turns
// the engine's output into pixels.
package paint

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// PaintPage draws every item of page onto dev. mat maps page space (points,
// Y-down, origin at the page's top-left) into device space (pixels); for a simple
// rasterization it is a uniform scale of dpi/72.
func PaintPage(dev render.Device, page *layout.Page, mat render.Matrix) {
	for i := range page.Items {
		it := &page.Items[i]
		switch it.Kind {
		case layout.GlyphKind:
			paintGlyph(dev, &it.Glyph, mat)
		case layout.RuleKind:
			paintRule(dev, &it.Rule, mat)
		case layout.BackgroundKind:
			// A background is just a filled rectangle behind content; reuse the rule
			// path (Item.Rule carries its geometry and color).
			paintRule(dev, &it.Rule, mat)
		case layout.BorderKind:
			paintBorder(dev, &it.Border, mat)
		}
	}
}

// paintGlyph fills one glyph. The outline is in em units (Y up); compose the
// transform em → page points → device:
//
//	scale(size, -size)  — em to points, flipping Y so the font's up becomes page down
//	translate(X, Y)     — move to the glyph's baseline origin in page space
//	mat                 — page points to device pixels
func paintGlyph(dev render.Device, g *layout.GlyphItem, mat render.Matrix) {
	if g.Outline == nil || g.Outline.Empty() {
		return
	}
	m := render.Scale(g.SizePt, -g.SizePt).
		Mul(render.Translate(g.XPt, g.YPt)).
		Mul(mat)
	dev.FillGlyph(transformPath(g.Outline, m), render.FillColor{
		R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A,
	}, "")
}

// paintRule fills an axis-aligned rectangle (underline/background) in page space.
func paintRule(dev render.Device, r *layout.RuleItem, mat render.Matrix) {
	fillRect(dev, mat, r.XPt, r.YPt, r.XPt+r.WPt, r.YPt+r.HPt, r.Color)
}

// fillRect fills the axis-aligned page-space rectangle [x0,x1]×[y0,y1] with c,
// mapping its corners through mat. A degenerate (zero/negative-area) rect draws
// nothing, matching the painter's never-panic contract for degenerate input.
func fillRect(dev render.Device, mat render.Matrix, x0, y0, x1, y1 float64, c color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	p := &render.Path{}
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
	dev.Fill(p, render.FillPaint{
		Color: c,
		Rule:  render.NonZero,
	})
}

// paintBorder draws one styled border edge. The edge is a full-length strip whose
// rectangle the caller (the CSS layout engine) computed; corner mitering between
// adjacent strips is out of scope, so dashes/dots simply run the strip's length.
//
//	solid  — fill the whole strip.
//	double — fill the outer and inner thirds across the strip's thickness, leaving
//	         the middle third empty.
//	dashed — tile filled rects along the strip's length, dash ≈ gap ≈ 3×thickness.
//	dotted — like dashed but dash = gap = thickness (square dots).
//
// The thickness axis and length axis depend on Side: top/bottom strips are
// horizontal (thickness along Y, length along X); left/right strips are vertical
// (thickness along X, length along Y).
func paintBorder(dev render.Device, b *layout.BorderItem, mat render.Matrix) {
	if b.Style == layout.BorderNone || b.WPt <= 0 || b.HPt <= 0 {
		return
	}
	x0, y0 := b.XPt, b.YPt
	x1, y1 := b.XPt+b.WPt, b.YPt+b.HPt
	horizontal := b.Side == layout.EdgeTop || b.Side == layout.EdgeBottom

	switch b.Style {
	case layout.BorderSolid:
		fillRect(dev, mat, x0, y0, x1, y1, b.Color)

	case layout.BorderDouble:
		// Split across the thickness axis into thirds; fill the outer and inner band.
		if horizontal {
			t := b.HPt / 3
			fillRect(dev, mat, x0, y0, x1, y0+t, b.Color)
			fillRect(dev, mat, x0, y1-t, x1, y1, b.Color)
		} else {
			t := b.WPt / 3
			fillRect(dev, mat, x0, y0, x0+t, y1, b.Color)
			fillRect(dev, mat, x1-t, y0, x1, y1, b.Color)
		}

	case layout.BorderDashed, layout.BorderDotted:
		// thick is the strip's thickness; dash/gap are measured along its length.
		thick := b.HPt
		if !horizontal {
			thick = b.WPt
		}
		dash := 3 * thick
		if b.Style == layout.BorderDotted {
			dash = thick
		}
		gap := dash
		step := dash + gap
		if step <= 0 {
			return
		}
		if horizontal {
			for x := x0; x < x1; x += step {
				end := x + dash
				if end > x1 {
					end = x1 // clamp the final dash to the strip
				}
				fillRect(dev, mat, x, y0, end, y1, b.Color)
			}
		} else {
			for y := y0; y < y1; y += step {
				end := y + dash
				if end > y1 {
					end = y1 // clamp the final dash to the strip
				}
				fillRect(dev, mat, x0, y, x1, end, b.Color)
			}
		}
	}
}

// transformPath returns a copy of src with every point mapped through m.
func transformPath(src *render.Path, m render.Matrix) *render.Path {
	out := &render.Path{Segments: make([]render.Segment, len(src.Segments))}
	for i, s := range src.Segments {
		ns := render.Segment{Kind: s.Kind}
		switch s.Kind {
		case render.MoveTo, render.LineTo:
			ns.P0 = applyPoint(m, s.P0)
		case render.CubeTo:
			ns.P0 = applyPoint(m, s.P0)
			ns.P1 = applyPoint(m, s.P1)
			ns.P2 = applyPoint(m, s.P2)
		case render.Close:
			// no points
		}
		out.Segments[i] = ns
	}
	return out
}

func applyPoint(m render.Matrix, p render.Point) render.Point {
	x, y := m.Apply(p.X, p.Y)
	return render.Point{X: x, Y: y}
}

func moveTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.MoveTo(dx, dy)
}

func lineTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.LineTo(dx, dy)
}
