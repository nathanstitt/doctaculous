package layout

import (
	"math"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// This file holds the resolved (absolute, points) form of CSS Backgrounds 3 §5
// border radii and the geometry built from them: the §5.1 overlap correction, the
// inner (padding-box) curve a border needs, and the rounded-rectangle path both the
// painter and any backend consume.
//
// It lives in pkg/layout rather than pkg/layout/paint because the radii travel ON
// the drawing items (RuleItem/BorderItem) — the painter is a consumer, not the
// owner — and because pkg/layout is the lowest layer both the CSS engine (which
// resolves the percentages) and the painter (which draws the arcs) already share.

// CornerRadii is a box's four corner radii resolved to absolute points, in the
// order CSS writes them: top-left, top-right, bottom-right, bottom-left. Each entry
// is [horizontal, vertical] — the two semi-axes of that corner's ellipse quadrant.
//
// A zero value means every corner is square, which is the overwhelmingly common
// case; Zero() lets every caller take its existing square-cornered fast path
// unchanged, which is what keeps output byte-identical for boxes with no radius.
type CornerRadii struct {
	TL, TR, BR, BL [2]float64
}

// Zero reports whether no corner is rounded at all. A corner with either semi-axis
// at zero is square (Backgrounds 3 §5.1), so the whole box is square only when
// every corner has a zero component.
func (r CornerRadii) Zero() bool {
	for _, c := range [4][2]float64{r.TL, r.TR, r.BR, r.BL} {
		if c[0] > 0 && c[1] > 0 {
			return false
		}
	}
	return true
}

// Correct applies the CSS Backgrounds 3 §5.1 overlap correction for a border box of
// w×h and returns the corrected radii.
//
// The rule: for each side, sum the two radii that meet along it; if that sum
// exceeds the side's length the radii overlap and the corners are geometrically
// impossible. The correction computes, per side, the ratio side/sum, takes f as the
// MINIMUM of those ratios (plus 1, so a box needing no correction is untouched),
// and scales ALL EIGHT components by that single f.
//
// The single shared factor is the part that is easy to get wrong and the reason
// this is a spec algorithm rather than a per-corner clamp: scaling each corner
// independently to fit its own side would distort the box's proportions and break
// the classic `border-radius: 100px` on an 80×80 box, which must yield a CIRCLE
// (every radius scaled to 40) rather than four separately-clamped arcs.
func (r CornerRadii) Correct(w, h float64) CornerRadii {
	if w <= 0 || h <= 0 {
		return CornerRadii{}
	}
	// Each side pairs the two corners that meet along it: top and bottom pair
	// horizontal components, left and right pair vertical ones.
	f := 1.0
	ratio := func(side, a, b float64) {
		if sum := a + b; sum > 0 {
			if q := side / sum; q < f {
				f = q
			}
		}
	}
	ratio(w, r.TL[0], r.TR[0]) // top
	ratio(w, r.BL[0], r.BR[0]) // bottom
	ratio(h, r.TL[1], r.BL[1]) // left
	ratio(h, r.TR[1], r.BR[1]) // right

	if f >= 1 {
		return r
	}
	scale := func(c [2]float64) [2]float64 { return [2]float64{c[0] * f, c[1] * f} }
	return CornerRadii{TL: scale(r.TL), TR: scale(r.TR), BR: scale(r.BR), BL: scale(r.BL)}
}

// Inset returns the radii of the curve one step INSIDE this box, given the four
// border widths — the padding-box (inner) curve of a rounded border.
//
// Per CSS Backgrounds 3 §5.2 the inner radius on an axis is the outer radius minus
// the border width that crosses it, floored at zero. Each corner's two components
// use the two DIFFERENT edges meeting at that corner: the top-left corner's
// horizontal semi-axis shrinks by the LEFT border's width and its vertical one by
// the TOP border's, because those are the widths the curve travels through on each
// axis.
//
// This is why a uniformly-thick rounded border is not the outer path stroked: a
// stroke would keep a constant radius, but the true inner curve's radius shrinks by
// the border width, and once it floors at zero the inner corner goes SQUARE while
// the outer stays round — visibly the shape browsers draw for a thick border on a
// small radius.
func (r CornerRadii) Inset(top, right, bottom, left float64) CornerRadii {
	sub := func(v, by float64) float64 { return math.Max(0, v-by) }
	return CornerRadii{
		TL: [2]float64{sub(r.TL[0], left), sub(r.TL[1], top)},
		TR: [2]float64{sub(r.TR[0], right), sub(r.TR[1], top)},
		BR: [2]float64{sub(r.BR[0], right), sub(r.BR[1], bottom)},
		BL: [2]float64{sub(r.BL[0], left), sub(r.BL[1], bottom)},
	}
}

// kappa is the control-point distance, as a fraction of the radius, that makes a
// cubic Bézier approximate a quarter ellipse: 4/3·tan(90°/4) = 4/3·(√2−1). The
// error is under 0.03% of the radius, far below a device pixel at any size this
// engine renders, and it is the same constant every 2D library uses for the job.
//
// The SVG frontend reaches the identical curve through its general endpoint-arc
// conversion (pkg/svg's arcSegments, SVG 1.1 F.6), which for a 90° axis-aligned
// slice reduces to exactly this. That routine is not reused here because it is
// unexported to pkg/svg and solves a much more general problem (arbitrary sweep,
// rotation, radius correction) than four axis-aligned quarter turns need; the
// constant below is the whole of what remains after that generality is removed.
const kappa = 4.0 / 3.0 * (math.Sqrt2 - 1)

// AppendRoundedRect appends to p the rounded rectangle [x,x+w]×[y,y+h] with radii
// (already corrected — call Correct first), mapping every emitted point through
// pt. A corner whose radii are zero (or negative) emits a sharp vertex, so a fully
// square CornerRadii produces exactly the four-line path the unrounded code emits.
//
// pt is a point mapper rather than a matrix so the caller can supply page-space →
// device-space mapping without this package importing the painter's conventions.
// The path is emitted clockwise in a Y-DOWN space (the page-space convention),
// starting after the top-left corner's arc, and is closed.
func AppendRoundedRect(p *render.Path, x, y, w, h float64, r CornerRadii, pt func(x, y float64) (float64, float64)) {
	if w <= 0 || h <= 0 {
		return
	}
	x1, y1 := x+w, y+h

	// clamp guards against a caller that skipped Correct, and against a radius that
	// individually exceeds the box: neither can produce a self-crossing path here.
	clamp := func(c [2]float64) [2]float64 {
		return [2]float64{math.Max(0, math.Min(c[0], w)), math.Max(0, math.Min(c[1], h))}
	}
	tl, tr, br, bl := clamp(r.TL), clamp(r.TR), clamp(r.BR), clamp(r.BL)

	moveTo := func(px, py float64) { p.MoveTo(pt(px, py)) }
	lineTo := func(px, py float64) { p.LineTo(pt(px, py)) }
	// corner emits the quarter ellipse from the current point to (ex,ey) around the
	// ellipse centre (cx,cy). The two control points sit kappa of the way along each
	// tangent, which is what makes the joins with the straight edges G1-continuous.
	corner := func(sx, sy, ex, ey, cx, cy float64) {
		c1x, c1y := sx+(cx-sx)*kappa, sy+(cy-sy)*kappa
		c2x, c2y := ex+(cx-ex)*kappa, ey+(cy-ey)*kappa
		ax, ay := pt(c1x, c1y)
		bx, by := pt(c2x, c2y)
		dx, dy := pt(ex, ey)
		p.CubeTo(ax, ay, bx, by, dx, dy)
	}

	// Walk clockwise from the top edge. Each corner's arc runs from the point where
	// the incoming straight edge ends to where the outgoing one begins; the "centre"
	// passed to corner is the rectangle's own corner vertex, which is where both
	// tangents meet.
	//
	// The final edge back up the left side is deliberately NOT emitted: Close()
	// already draws it. Emitting it would leave a redundant zero-length lineTo on a
	// fully square box, making the path six segments where the unrounded painter
	// emitted five — a difference every backend would serialize (pdfwrite would
	// write an extra `l` operator), breaking byte-identical output for documents
	// that use no radius at all.
	moveTo(x, y+tl[1])
	if tl[0] > 0 && tl[1] > 0 {
		corner(x, y+tl[1], x+tl[0], y, x, y)
	}
	lineTo(x1-tr[0], y)
	if tr[0] > 0 && tr[1] > 0 {
		corner(x1-tr[0], y, x1, y+tr[1], x1, y)
	}
	lineTo(x1, y1-br[1])
	if br[0] > 0 && br[1] > 0 {
		corner(x1, y1-br[1], x1-br[0], y1, x1, y1)
	}
	lineTo(x+bl[0], y1)
	if bl[0] > 0 && bl[1] > 0 {
		corner(x+bl[0], y1, x, y1-bl[1], x, y1)
	}
	p.Close()
}
