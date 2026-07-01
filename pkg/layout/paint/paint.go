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

// imageDest is a destination rectangle in page space (points, Y-down) into which a
// replaced image's full pixel grid is drawn. For object-fit modes that scale
// uniformly (contain/cover/none/scale-down) it may be larger or smaller than, and
// offset from, the content box; the caller clips to the content box when it
// overflows.
type imageDest struct {
	x, y, w, h float64
}

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
		case layout.ImageKind:
			paintImage(dev, &it.Image, mat)
		case layout.BackgroundImageKind:
			paintBackgroundImage(dev, &it.BgImage, mat)
		case layout.ClipPushKind:
			// Save the clip state, then intersect the active clip with the rect (mapped
			// through the page matrix). A degenerate rect makes clipRect a no-op push, but
			// Save/Restore still balance, so the stream stays well-formed.
			dev.Save()
			clipRect(dev, mat, it.Rule.XPt, it.Rule.YPt, it.Rule.XPt+it.Rule.WPt, it.Rule.YPt+it.Rule.HPt)
		case layout.ClipPopKind:
			dev.Restore()
		}
	}
}

// paintGlyph fills one glyph. The outline is in em units (Y up); compose the
// transform em → page points → device:
//
//	scale(size, -size)  — em to points, flipping Y so the font's up becomes page down
//	translate(X, Y)     — move to the glyph's baseline origin in page space
//	mat                 — page points to device pixels
// paintGlyph draws one glyph. When the glyph carries font identity (Face+GID), it
// uses DrawGlyph so text-emitting backends (PDF) can embed real text; otherwise it
// falls back to filling the raw outline. The em -> device transform is the same in
// both cases: Scale(size,-size) · Translate(X,Y) · mat.
func paintGlyph(dev render.Device, g *layout.GlyphItem, mat render.Matrix) {
	m := render.Scale(g.SizePt, -g.SizePt).
		Mul(render.Translate(g.XPt, g.YPt)).
		Mul(mat)
	if g.Face != nil {
		dev.DrawGlyph(render.GlyphRef{
			Face:      g.Face,
			GID:       g.GID,
			Runes:     g.Runes,
			Transform: m,
			Color:     render.FillColor{R: g.Color.R, G: g.Color.G, B: g.Color.B, A: g.Color.A},
		})
		return
	}
	if g.Outline == nil || g.Outline.Empty() {
		return
	}
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

	case layout.BorderOutset, layout.BorderInset:
		// 3D edge: fill the whole strip with the light or dark shade chosen by side.
		// outset = top/left light, bottom/right dark; inset is the inverse.
		light := b.Style == layout.BorderOutset
		fillRect(dev, mat, x0, y0, x1, y1, edge3DColor(b.Color, b.Side, light))

	case layout.BorderRidge, layout.BorderGroove:
		// 3D ridge/groove: split the strip across its thickness into an outer and inner
		// half, painting them with opposite light/dark shades. ridge = outer behaves
		// like outset; groove is the inverse.
		outerLight := b.Style == layout.BorderRidge
		outer := edge3DColor(b.Color, b.Side, outerLight)
		inner := edge3DColor(b.Color, b.Side, !outerLight)
		if horizontal {
			mid := (y0 + y1) / 2
			fillRect(dev, mat, x0, y0, x1, mid, outer)
			fillRect(dev, mat, x0, mid, x1, y1, inner)
		} else {
			mid := (x0 + x1) / 2
			fillRect(dev, mat, x0, y0, mid, y1, outer)
			fillRect(dev, mat, mid, y0, x1, y1, inner)
		}
	}
}

// edge3DColor returns the shade for a 3D border edge: the "light" side of the bevel
// (top/left when raised) keeps the base color, the "dark" side is darkened to ~half.
// light selects whether THIS edge is on the lit side (caller derives it from the style
// and side). The top and left edges are lit when light is true; bottom and right edges
// are the opposite, so the caller passes the already-resolved light flag and this only
// flips it for bottom/right. (Matches browser bevels: a raised box lights top+left.)
func edge3DColor(c color.RGBA, side layout.EdgeSide, light bool) color.RGBA {
	// Bottom/right edges are the opposite face of the bevel from top/left.
	if side == layout.EdgeBottom || side == layout.EdgeRight {
		light = !light
	}
	if light {
		return c
	}
	return color.RGBA{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: c.A}
}

// paintImage draws a replaced-element image into its content box under the chosen
// object-fit mapping. it.XPt,YPt,WPt,HPt is the content box in page space (points,
// Y-down). The destination rectangle (after object-fit) maps the image's unit
// square upright into page space:
//
//	Mimg = scale(destW, -destH) · translate(destX, destY+destH)
//
// At image-bottom (v=0) y = destY+destH; at image-top (v=1) y = destY. This matches
// render.Device.DrawImage, which samples the source row from (1-v), so the image
// renders upright. Mimg is then composed with mat (page→device). When the
// destination overflows the content box (cover / oversized none), the content box is
// pushed as a clip so only the box-sized region is painted.
func paintImage(dev render.Device, it *layout.ImageItem, mat render.Matrix) {
	if it.Img == nil || it.WPt <= 0 || it.HPt <= 0 {
		return
	}
	b := it.Img.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	if iw <= 0 || ih <= 0 {
		return
	}

	d := fitDest(it.Fit, it.XPt, it.YPt, it.WPt, it.HPt, iw, ih, it.PosX, it.PosY)
	if d.w <= 0 || d.h <= 0 {
		return
	}

	// Clip to the content box when the fitted image can extend beyond it (cover, or
	// an oversized none/scale-down). fill/contain never overflow, so they skip the
	// clip (and its save/restore cost).
	clip := d.x < it.XPt-epsilon || d.y < it.YPt-epsilon ||
		d.x+d.w > it.XPt+it.WPt+epsilon || d.y+d.h > it.YPt+it.HPt+epsilon
	if clip {
		dev.Save()
		clipRect(dev, mat, it.XPt, it.YPt, it.XPt+it.WPt, it.YPt+it.HPt)
	}

	mImg := render.Scale(d.w, -d.h).Mul(render.Translate(d.x, d.y+d.h))
	dev.DrawImage(it.Img, mImg.Mul(mat), 1, "")

	if clip {
		dev.Restore()
	}
}

// epsilon guards the overflow comparison against float rounding so an
// exactly-fitting image isn't needlessly clipped.
const epsilon = 1e-6

// fitDest computes the destination rectangle (page space) the image's full pixel
// grid maps into, for content box (cx,cy,cw,ch) and intrinsic size (iw,ih), under
// fit. fill stretches to the box; contain/cover scale uniformly by the min/max axis
// ratio and center; none uses intrinsic size centered; scale-down picks the smaller
// of none and contain. The result may exceed the content box (cover, oversized
// none) — the caller clips.
func fitDest(fit layout.ObjectFit, cx, cy, cw, ch, iw, ih, posX, posY float64) imageDest {
	// positioned places a w×h image within the content box at the object-position
	// fractions (posX/posY of the free space cw-w / ch-h). posX=posY=0.5 centers it
	// (the default), reproducing the prior behavior exactly.
	positioned := func(w, h float64) imageDest {
		return imageDest{x: cx + (cw-w)*posX, y: cy + (ch-h)*posY, w: w, h: h}
	}
	switch fit {
	case layout.FitContain:
		s := scaleRatio(cw/iw, ch/ih, true) // fit inside: the smaller ratio
		return positioned(iw*s, ih*s)
	case layout.FitCover:
		s := scaleRatio(cw/iw, ch/ih, false) // cover: the larger ratio
		return positioned(iw*s, ih*s)
	case layout.FitNone:
		return positioned(iw, ih)
	case layout.FitScaleDown:
		// none unless it overflows the box, in which case contain (the smaller image).
		s := scaleRatio(cw/iw, ch/ih, true)
		if s >= 1 {
			return positioned(iw, ih) // intrinsic already fits: use none
		}
		return positioned(iw*s, ih*s)
	default: // FitFill
		return imageDest{x: cx, y: cy, w: cw, h: ch}
	}
}

// scaleRatio returns the smaller of a and b when min is true, else the larger —
// the uniform scale factor for contain (min) and cover (max).
func scaleRatio(a, b float64, min bool) float64 {
	if min {
		if a < b {
			return a
		}
		return b
	}
	if a > b {
		return a
	}
	return b
}

// clipRect intersects the device clip with the axis-aligned page-space rectangle
// [x0,x1]×[y0,y1], mapped through mat. Used to confine an object-fit:cover (or
// oversized) image to its content box.
func clipRect(dev render.Device, mat render.Matrix, x0, y0, x1, y1 float64) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	p := &render.Path{}
	moveTo(p, mat, x0, y0)
	lineTo(p, mat, x1, y0)
	lineTo(p, mat, x1, y1)
	lineTo(p, mat, x0, y1)
	p.Close()
	dev.PushClip(p, render.NonZero)
}

// transformPath returns a copy of src with every point mapped through m.
func transformPath(src *render.Path, m render.Matrix) *render.Path {
	return render.TransformPath(src, m)
}

func moveTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.MoveTo(dx, dy)
}

func lineTo(p *render.Path, m render.Matrix, x, y float64) {
	dx, dy := m.Apply(x, y)
	p.LineTo(dx, dy)
}
