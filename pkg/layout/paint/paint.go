// Package paint draws a laid-out page (pkg/layout) onto a render.Device. It is
// format-neutral: it consumes only positioned glyphs and rules in page space and
// knows nothing about DOCX/HTML/EPUB. Together with pkg/render/raster this turns
// the engine's output into pixels.
package paint

import (
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

// paintRule fills an axis-aligned rectangle (underline/border) in page space.
func paintRule(dev render.Device, r *layout.RuleItem, mat render.Matrix) {
	if r.WPt <= 0 || r.HPt <= 0 {
		return
	}
	p := &render.Path{}
	x0, y0 := r.XPt, r.YPt
	x1, y1 := r.XPt+r.WPt, r.YPt+r.HPt
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
	dev.Fill(p, render.FillPaint{
		Color: r.Color,
		Rule:  render.NonZero,
	})
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
