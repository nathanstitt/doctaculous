package font

import "image/color"

// COLR v1 paint graph.
//
// v1 replaces v0's flat layer list with a graph of "paint" records: a base glyph points
// at a paint, and paints nest — layer lists, solid fills, gradients, transforms,
// composites. Full v1 is a large surface.
//
// This walk flattens the subset the engine can express as ordered solid-filled
// outlines: PaintColrLayers (a layer list), PaintGlyph (an outline), PaintSolid (a
// palette colour), and PaintColrGlyph (a reference to another colour glyph). A
// gradient or composite paint is reported as UNSUPPORTED, and the caller then draws the
// glyph monochrome rather than filling it with an arbitrary flat colour — a wrong
// colour is harder to notice than no colour.
//
// Every offset read is bounds-checked and the walk is depth- and node-limited: the
// graph is untrusted input and may be cyclic.

// Paint record formats (OpenType COLR v1, "Paint tables"). The numbering is dense and
// pairs each non-variable format with a variable one (PaintSolid 2 / PaintVarSolid 3,
// and so on), which is why the supported set below skips odd values.
//
// These were checked against real records in the Noto fixture rather than transcribed
// alone: an early draft had 12 as PaintColrGlyph, but a format-12 record there carries
// a u24 child-paint offset and resolves to a nested PaintGlyph, i.e. it is a transform.
// A wrong format number does not crash — it silently paints the wrong thing.
const (
	paintColrLayers   = 1
	paintSolid        = 2
	paintVarSolid     = 3
	paintLinearGrad   = 4
	paintVarLinear    = 5
	paintRadialGrad   = 6
	paintVarRadial    = 7
	paintSweepGrad    = 8
	paintVarSweep     = 9
	paintGlyph        = 10
	paintColrGlyph    = 11
	paintTransform    = 12
	paintVarTransform = 13
	paintTranslate    = 14
	paintVarTranslate = 15
	paintScale        = 16
	paintComposite    = 32
)

// colrV1MaxNodes bounds a single glyph's walk. A malformed or hostile table can
// describe a graph that expands combinatorially; a real emoji uses tens of paints.
const colrV1MaxNodes = 512

// colrV1MaxDepth bounds nesting depth independently of node count, so a deep chain
// cannot exhaust the goroutine stack before the node budget is spent.
const colrV1MaxDepth = 64

// colrV1Layers resolves a v1 base glyph to flat colour layers. ok=false means the
// glyph is not a v1 colour glyph, or its paint graph uses features this engine cannot
// express — both degrade to the monochrome outline.
func (t *colrTable) colrV1Layers(gid uint16, pal *cpalTable) ([]ColorLayer, bool) {
	off, ok := t.v1PaintFor(gid)
	if !ok {
		return nil, false
	}
	w := &colrWalk{t: t, pal: pal, m: identityAffine()}
	if !w.paint(off, 0) || len(w.out) == 0 {
		return nil, false
	}
	return w.out, true
}

// v1PaintFor binary-searches the v1 BaseGlyphList for gid, returning the absolute
// offset of its root paint.
func (t *colrTable) v1PaintFor(gid uint16) (uint32, bool) {
	base := t.baseListV1
	if base == 0 || !within(t.raw, base, 4) {
		return 0, false
	}
	n := int(be32(t.raw, int(base)))
	if !within(t.raw, base+4, n*6) {
		return 0, false
	}
	lo, hi := 0, n-1
	for lo <= hi {
		mid := (lo + hi) / 2
		rec := int(base) + 4 + mid*6
		g := be16(t.raw, rec)
		switch {
		case g < gid:
			lo = mid + 1
		case g > gid:
			hi = mid - 1
		default:
			// paintOffset is relative to the start of the BaseGlyphList.
			return base + be32(t.raw, rec+2), true
		}
	}
	return 0, false
}

// colrWalk accumulates the flattened layers of one glyph's paint graph.
type colrWalk struct {
	t     *colrTable
	pal   *cpalTable
	out   []ColorLayer
	nodes int
	// fill is the colour the enclosing PaintGlyph should use, set by a PaintSolid
	// found beneath it. Recorded rather than emitted directly because the graph
	// nests colour UNDER the shape it fills.
	fill       color.RGBA
	grad       *ColorGradient
	haveFill   bool
	foreground bool
	// m is the accumulated affine of the enclosing transform paints, composed in the
	// graph's own order (outer applied to inner).
	m affine
}

// paint walks one paint record. It returns false when the record is malformed or uses
// an unsupported feature, which aborts the whole glyph.
func (w *colrWalk) paint(off uint32, depth int) bool {
	if depth > colrV1MaxDepth {
		return false
	}
	w.nodes++
	if w.nodes > colrV1MaxNodes {
		return false
	}
	b := w.t.raw
	if int(off) >= len(b) {
		return false
	}
	switch b[off] {
	case paintColrLayers:
		return w.colrLayers(off, depth)

	case paintSolid, paintVarSolid:
		idx := be16(b, int(off)+1)
		if idx == 0xFFFF {
			w.foreground, w.haveFill = true, true
			return true
		}
		c, ok := w.pal.colorAt(idx)
		if !ok {
			w.foreground, w.haveFill = true, true
			return true
		}
		// alpha is F2Dot14 in [0,1]; fold it into the colour's own alpha.
		if a := f2dot14(be16(b, int(off)+3)); a < 1 {
			if a < 0 {
				a = 0
			}
			c.A = uint8(float64(c.A)*a + 0.5)
		}
		w.fill, w.foreground, w.haveFill, w.grad = c, false, true, nil
		return true

	case paintGlyph:
		// PaintGlyph: fmt(1) paintOffset(u24) glyphID(u16). The nested paint supplies
		// the colour; the glyph id supplies the shape.
		child, ok := offsetFrom(off, u24(b, int(off)+1), len(b))
		if !ok {
			return false
		}
		g := be16(b, int(off)+4)
		saveFill, saveFG, saveHave, saveGrad := w.fill, w.foreground, w.haveFill, w.grad
		w.haveFill, w.grad = false, nil
		if !w.paint(child, depth+1) {
			return false
		}
		if !w.haveFill {
			// A shape with no resolvable colour beneath it: treat the whole glyph as
			// unsupported rather than guessing a fill.
			return false
		}
		w.out = append(w.out, ColorLayer{
			GID: g, Color: w.fill, Foreground: w.foreground, Gradient: w.grad,
			XX: w.m.xx, YX: w.m.yx, XY: w.m.xy, YY: w.m.yy, DX: w.m.dx, DY: w.m.dy,
		})
		w.fill, w.foreground, w.haveFill, w.grad = saveFill, saveFG, saveHave, saveGrad
		return true

	case paintColrGlyph:
		// A reference to another colour glyph: splice its layers in.
		g := be16(b, int(off)+1)
		sub, ok := w.t.layersFor(g, w.pal)
		if !ok {
			return false
		}
		w.nodes += len(sub)
		if w.nodes > colrV1MaxNodes {
			return false
		}
		w.out = append(w.out, sub...)
		return true

	case paintTransform, paintVarTransform:
		// PaintTransform: fmt, paintOffset u24, transformOffset u24 -> Affine2x3.
		// A PURE TRANSLATION composes with the flat layer model (each layer carries an
		// offset), so it is applied. Anything else — rotation, scale, skew — cannot be
		// expressed per layer and refuses the glyph rather than painting it in the
		// wrong shape. Real emoji fonts use translation here: every transform in the
		// Noto fixture is an identity matrix with an offset.
		to, ok := offsetFrom(off, u24(b, int(off)+4), len(b))
		if !ok || !within(b, to, 24) {
			return false
		}
		// The FULL affine is carried, not just a translation or flip: emoji use
		// rotation and scale for real (a party popper's streamers), and modelling less
		// meant refusing those glyphs entirely.
		child, ok := offsetFrom(off, u24(b, int(off)+1), len(b))
		if !ok {
			return false
		}
		return w.transformed(affine{
			xx: fixed1616(be32(b, int(to))), yx: fixed1616(be32(b, int(to)+4)),
			xy: fixed1616(be32(b, int(to)+8)), yy: fixed1616(be32(b, int(to)+12)),
			dx: fixed1616(be32(b, int(to)+16)), dy: fixed1616(be32(b, int(to)+20)),
		}, child, depth)

	case paintTranslate, paintVarTranslate:
		// PaintTranslate: fmt, paintOffset u24, dx int16, dy int16.
		child, ok := offsetFrom(off, u24(b, int(off)+1), len(b))
		if !ok {
			return false
		}
		return w.transformed(affine{
			xx: 1, yy: 1,
			dx: float64(int16(be16(b, int(off)+4))), dy: float64(int16(be16(b, int(off)+6))),
		}, child, depth)

	case paintScale:
		// PaintScale: fmt, paintOffset u24, scaleX F2Dot14, scaleY F2Dot14.
		child, ok := offsetFrom(off, u24(b, int(off)+1), len(b))
		if !ok {
			return false
		}
		return w.transformed(affine{
			xx: f2dot14(be16(b, int(off)+4)), yy: f2dot14(be16(b, int(off)+6)),
		}, child, depth)

	case paintLinearGrad, paintVarLinear:
		g, ok := w.linearGradient(off)
		if !ok {
			return false
		}
		w.grad, w.haveFill, w.foreground = g, true, false
		return true

	case paintRadialGrad, paintVarRadial:
		g, ok := w.radialGradient(off)
		if !ok {
			return false
		}
		w.grad, w.haveFill, w.foreground = g, true, false
		return true

	case paintSweepGrad, paintVarSweep, paintComposite:
		// A sweep (conic) gradient has no analogue in the render layer's shading
		// vocabulary, and a composite needs group compositing per layer. Both refuse
		// the glyph rather than approximating it.
		return false
	}
	return false
}

// transformed walks child with an additional flip and translation applied to every
// layer it produces, restoring the previous transform afterwards so siblings are
// unaffected. The composition is outer-then-inner: the inner offset is scaled by the
// outer flip, matching how the paint graph nests.
func (w *colrWalk) transformed(t affine, child uint32, depth int) bool {
	saved := w.m
	w.m = saved.mul(t)
	ok := w.paint(child, depth+1)
	w.m = saved
	return ok
}

// affine is a 2x3 matrix in font units: [xx yx; xy yy] with translation (dx, dy).
type affine struct{ xx, yx, xy, yy, dx, dy float64 }

func identityAffine() affine { return affine{xx: 1, yy: 1} }

// mul returns a followed by... strictly, the composition that applies `in` first and
// then `a`, matching how a nested paint sits INSIDE its enclosing transform.
func (a affine) mul(in affine) affine {
	return affine{
		xx: in.xx*a.xx + in.yx*a.xy,
		yx: in.xx*a.yx + in.yx*a.yy,
		xy: in.xy*a.xx + in.yy*a.xy,
		yy: in.xy*a.yx + in.yy*a.yy,
		dx: in.dx*a.xx + in.dy*a.xy + a.dx,
		dy: in.dx*a.yx + in.dy*a.yy + a.dy,
	}
}

// fixed1616 converts an OpenType Fixed (16.16) to float64.
func fixed1616(v uint32) float64 { return float64(int32(v)) / 65536.0 }

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// colrLayers walks a PaintColrLayers record: a slice of the LayerList, painted in
// order (first layer at the bottom).
func (w *colrWalk) colrLayers(off uint32, depth int) bool {
	b := w.t.raw
	n := int(b[off+1])
	first := be32(b, int(off)+2)
	list := w.t.layerListV1
	if list == 0 || !within(b, list, 4) {
		return false
	}
	count := int(be32(b, int(list)))
	if int(first)+n > count {
		return false
	}
	for i := 0; i < n; i++ {
		rec := int(list) + 4 + (int(first)+i)*4
		if rec+4 > len(b) {
			return false
		}
		if !w.paint(list+be32(b, rec), depth+1) {
			return false
		}
	}
	return true
}

// u24 reads a 24-bit big-endian offset as a SIGNED value.
//
// COLR v1 paint offsets are Offset24, and real fonts use NEGATIVE ones: a paint that
// several layers share sits before them in the table, so the later references point
// backwards. Reading them as unsigned turns -5 into 16777211 and the walk lands far
// outside the table — which is exactly how this first showed up, as an out-of-bounds
// read on the third layer of an otherwise ordinary glyph.
func u24(b []byte, o int) int32 {
	if o < 0 || o+3 > len(b) {
		return 0
	}
	v := int32(b[o])<<16 | int32(b[o+1])<<8 | int32(b[o+2])
	if v&0x800000 != 0 {
		v -= 1 << 24 // sign-extend from 24 bits
	}
	return v
}

// offsetFrom applies a signed paint offset to a base, reporting ok=false when the
// result falls outside the table.
func offsetFrom(base uint32, delta int32, size int) (uint32, bool) {
	v := int64(base) + int64(delta)
	if v < 0 || v >= int64(size) {
		return 0, false
	}
	return uint32(v), true
}

// f2dot14 converts an F2Dot14 fixed-point value to float64.
func f2dot14(v uint16) float64 { return float64(int16(v)) / 16384.0 }

// linearGradient decodes PaintLinearGradient: fmt, colorLineOffset u24, then six
// int16 coordinates (x0,y0, x1,y1, x2,y2).
func (w *colrWalk) linearGradient(off uint32) (*ColorGradient, bool) {
	b := w.t.raw
	stops, extend, ok := w.colorLine(off)
	if !ok {
		return nil, false
	}
	return &ColorGradient{
		X0: float64(int16(be16(b, int(off)+4))), Y0: float64(int16(be16(b, int(off)+6))),
		X1: float64(int16(be16(b, int(off)+8))), Y1: float64(int16(be16(b, int(off)+10))),
		X2: float64(int16(be16(b, int(off)+12))), Y2: float64(int16(be16(b, int(off)+14))),
		Stops: stops, Extend: extend,
	}, true
}

// radialGradient decodes PaintRadialGradient: fmt, colorLineOffset u24, then
// x0,y0,r0, x1,y1,r1 (int16 / uint16 radii).
func (w *colrWalk) radialGradient(off uint32) (*ColorGradient, bool) {
	b := w.t.raw
	stops, extend, ok := w.colorLine(off)
	if !ok {
		return nil, false
	}
	return &ColorGradient{
		Radial: true,
		X0:     float64(int16(be16(b, int(off)+4))), Y0: float64(int16(be16(b, int(off)+6))),
		R0: float64(be16(b, int(off)+8)),
		X1: float64(int16(be16(b, int(off)+10))), Y1: float64(int16(be16(b, int(off)+12))),
		R1:    float64(be16(b, int(off)+14)),
		Stops: stops, Extend: extend,
	}, true
}

// colorLine decodes the ColorLine a gradient paint points at: an extend mode plus a
// run of (offset, palette index, alpha) stops.
func (w *colrWalk) colorLine(off uint32) ([]GradientStop, string, bool) {
	b := w.t.raw
	clo, ok := offsetFrom(off, u24(b, int(off)+1), len(b))
	if !ok || !within(b, clo, 3) {
		return nil, "", false
	}
	extend := "pad"
	switch b[clo] {
	case 1:
		extend = "repeat"
	case 2:
		extend = "reflect"
	}
	n := int(be16(b, int(clo)+1))
	if n == 0 || !within(b, clo+3, n*6) {
		return nil, "", false
	}
	stops := make([]GradientStop, 0, n)
	for i := 0; i < n; i++ {
		r := int(clo) + 3 + i*6
		pos := f2dot14(be16(b, r))
		idx := be16(b, r+2)
		var c color.RGBA
		if idx == 0xFFFF {
			// A foreground stop inside a gradient has no text colour available here;
			// refuse rather than guess, so the glyph falls back to its outline.
			return nil, "", false
		}
		got, ok := w.pal.colorAt(idx)
		if !ok {
			return nil, "", false
		}
		c = got
		if a := f2dot14(be16(b, r+4)); a < 1 {
			if a < 0 {
				a = 0
			}
			c.A = uint8(float64(c.A)*a + 0.5)
		}
		stops = append(stops, GradientStop{Offset: pos, Color: c})
	}
	return stops, extend, true
}
