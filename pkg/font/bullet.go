package font

import "github.com/nathanstitt/doctaculous/pkg/render"

// Marker bullet geometry, in em units (Y up, baseline at 0) — chosen to sit like a
// browser's list bullet: a small mark vertically centered a little above the
// baseline (around the lowercase x-height middle) and an advance that leaves a gap
// before the item text.
const (
	bulletDiameter  = 0.35 // mark size as a fraction of the em
	bulletCenterY   = 0.30 // vertical center above the baseline (~x-height middle)
	bulletAdvanceEm = 0.55 // horizontal advance the mark consumes (mark + gap)
	bulletRingInset = 0.07 // ring wall thickness for the hollow circle (em)
	// kappa is the cubic-Bézier constant for a quarter circle: 4/3·(√2−1).
	kappa = 0.5522847498307936
)

// syntheticBullet returns a synthesized outline (em units, Y up) and advance for a
// list-marker bullet rune, so bullets render even when the bundled face has no glyph
// for that code point (the TeX Gyre substitutes lack ▪ U+25AA, for example). This
// mirrors how browsers paint list markers as geometry rather than font glyphs, which
// makes the marker font-independent and robust.
//
// It handles the three CSS bullet styles and their common Unicode spellings:
//   - disc   "•" (U+2022) and "●" (U+25CF)            → a filled circle
//   - circle "◦" (U+25E6) and "○" (U+25CB)            → a hollow ring
//   - square "▪" (U+25AA), "◾" (U+25FE), "■" (U+25A0) → a filled square
//
// ok is false for any other rune (the caller then falls back to the real font path).
func syntheticBullet(r rune) (outline *render.Path, advanceEm float64, ok bool) {
	switch r {
	case '•', '●':
		return discPath(), bulletAdvanceEm, true
	case '◦', '○':
		return ringPath(), bulletAdvanceEm, true
	case '▪', '◾', '■':
		return squarePath(), bulletAdvanceEm, true
	}
	return nil, 0, false
}

// CirclePath synthesizes a circle outline centered at (cx, cy): a filled disc
// when innerR <= 0, otherwise a ring whose hole has radius innerR (the inner
// circle is wound opposite so the nonzero fill subtracts it). Coordinates follow
// the glyph-outline convention (em-style units, Y up); callers scale/position via
// the glyph transform. Exported for painters that need font-independent circular
// marks (list bullets here; radio-button chrome in the layout engine).
func CirclePath(cx, cy, outerR, innerR float64) *render.Path {
	p := &render.Path{}
	addCircle(p, cx, cy, outerR, false)
	if innerR > 0 && innerR < outerR {
		addCircle(p, cx, cy, innerR, true)
	}
	return p
}

// discPath is a filled circle centered at (radius, bulletCenterY).
func discPath() *render.Path {
	r := bulletDiameter / 2
	cx := r
	p := &render.Path{}
	addCircle(p, cx, bulletCenterY, r, false)
	return p
}

// ringPath is a hollow circle: an outer circle and a smaller inner circle wound the
// opposite way so the nonzero fill (used for glyph outlines) cancels the interior.
func ringPath() *render.Path {
	rOuter := bulletDiameter / 2
	rInner := rOuter - bulletRingInset
	cx := rOuter
	p := &render.Path{}
	addCircle(p, cx, bulletCenterY, rOuter, false) // outer, CCW
	if rInner > 0 {
		addCircle(p, cx, bulletCenterY, rInner, true) // inner, CW → hole
	}
	return p
}

// squarePath is a filled square centered at (side/2, bulletCenterY). It is drawn a
// touch smaller than the disc/ring diameter so its visual weight matches them.
func squarePath() *render.Path {
	side := bulletDiameter * 0.9
	x0, y0 := 0.0, bulletCenterY-side/2
	x1, y1 := side, bulletCenterY+side/2
	p := &render.Path{}
	p.MoveTo(x0, y0)
	p.LineTo(x1, y0)
	p.LineTo(x1, y1)
	p.LineTo(x0, y1)
	p.Close()
	return p
}

// addCircle appends a circle of radius r centered at (cx, cy) to p as four cubic
// Bézier quarter-arcs. cw selects clockwise winding (used for a ring's inner hole so
// it subtracts under the nonzero rule); cw=false is counter-clockwise.
func addCircle(p *render.Path, cx, cy, r float64, cw bool) {
	k := r * kappa
	if cw {
		// Clockwise from the right (cx+r, cy): right → bottom → left → top.
		p.MoveTo(cx+r, cy)
		p.CubeTo(cx+r, cy-k, cx+k, cy-r, cx, cy-r)
		p.CubeTo(cx-k, cy-r, cx-r, cy-k, cx-r, cy)
		p.CubeTo(cx-r, cy+k, cx-k, cy+r, cx, cy+r)
		p.CubeTo(cx+k, cy+r, cx+r, cy+k, cx+r, cy)
	} else {
		// Counter-clockwise from the right (cx+r, cy): right → top → left → bottom.
		p.MoveTo(cx+r, cy)
		p.CubeTo(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
		p.CubeTo(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
		p.CubeTo(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
		p.CubeTo(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	}
	p.Close()
}
