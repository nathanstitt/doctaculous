package paint

import (
	"image/color"
	"math"

	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// paintGradientTile paints one background tile whose source is a CSS gradient,
// at page-space top-left (tx,ty) at size tw x th.
//
// It reuses the SAME shading seam SVG paint servers use — raster.NewAxialShader /
// NewRadialShader driven through render.Device.FillShading — rather than
// rasterizing the gradient into a bitmap and drawing that. Three reasons, in
// order of weight:
//
//   - Resolution independence. FillShading evaluates the ramp at each DEVICE
//     pixel, so a gradient stays smooth at any raster scale, and a vector
//     backend (pdfwrite) can recognize the describable shader and emit a native
//     /Shading dictionary instead of an image. Baking a bitmap here would throw
//     both away at the one place that decides.
//   - It is the seam's documented purpose. render.Shader exists precisely so
//     gradient geometry and colour maths stay out of the drawing code, and the
//     axial/radial evaluators already handle the spread modes CSS needs.
//   - No second implementation to keep in step with the first.
//
// The gradient's coordinates are in TILE space (origin at the tile's top-left),
// so the ctm handed to FillShading is tile-space → page-space → device.
func paintGradientTile(dev render.Device, g *layout.BackgroundGradient, tx, ty, tw, th float64, mat render.Matrix) {
	if g == nil || len(g.Stops) == 0 || tw <= 0 || th <= 0 {
		return
	}
	shader, local, ok := gradientShader(g)
	if !ok {
		return
	}
	// Tile space differs from page space only by the tile's origin; the gradient
	// was already resolved against the tile's own size, so there is no scale here.
	ctm := local.Mul(render.Translate(tx, ty)).Mul(mat)

	// Scope the shading to the tile's rectangle. FillShading with no clip fills
	// the WHOLE device, so the clip is what confines a tile to its own cell —
	// the same Save/PushClip/FillShading/Restore shape pkg/svg/draw's
	// fillGradient uses for a gradient-filled shape.
	dev.Save()
	clipRect(dev, mat, tx, ty, tx+tw, ty+th)
	dev.FillShading(shader, ctm, "")
	dev.Restore()
}

// gradientShader builds the render.Shader for a resolved background gradient,
// plus the matrix mapping the shader's own evaluation space into tile space.
//
// The matrix exists for the ELLIPSE case. The shared radial evaluator is
// circular (it solves |P − C(s)| = r(s), which has no independent per-axis
// radius), so an elliptical CSS gradient is rendered as a UNIT-RADIUS circle
// scaled by (rx, ry) about the centre — the standard reduction, and the same one
// SVG's gradientTransform performs for a stretched radialGradient. That keeps a
// single radial evaluator authoritative instead of adding an elliptical variant
// whose maths could drift from it.
//
// ok=false means the gradient is degenerate (a zero-length line, or a zero
// radius) and nothing should paint.
func gradientShader(g *layout.BackgroundGradient) (render.Shader, render.Matrix, bool) {
	ramp := &gradientRamp{stops: g.Stops}
	stops := make([]render.ShadingStop, len(g.Stops))
	for i, s := range g.Stops {
		stops[i] = render.ShadingStop{Offset: s.Pos, Color: s.Color}
	}
	spread := raster.SpreadPad
	if g.Repeating {
		spread = raster.SpreadRepeat
	}

	switch g.Kind {
	case layout.GradientRadial:
		if g.RX <= 0 || g.RY <= 0 {
			return nil, render.Identity, false
		}
		// Evaluate a unit circle at the origin, then scale it to (rx,ry) and
		// move it to the centre. Composed left-to-right per Matrix.Mul's
		// "m first, then n" semantics.
		local := render.Scale(g.RX, g.RY).Mul(render.Translate(g.CX, g.CY))
		return raster.NewRadialShader(0, 0, 0, 0, 0, 1, ramp, stops, spread), local, true

	default:
		if g.X0 == g.X1 && g.Y0 == g.Y1 {
			return nil, render.Identity, false // zero-length gradient line
		}
		return raster.NewAxialShader(g.X0, g.Y0, g.X1, g.Y1, ramp, stops, spread), render.Identity, true
	}
}

// gradientRamp is the piecewise-linear colour ramp for a CSS background
// gradient, implementing function.Func (pkg/pdf/function) with one input (the
// gradient parameter) and four outputs (straight R,G,B,A in [0,1]) — the exact
// shape raster.NewAxialShader/NewRadialShader consume, and the same contract
// pkg/svg's stopRamp satisfies for SVG paint servers.
//
// It is a SEPARATE type from svg's stopRamp, not a shared one, because the two
// differ in the one respect that matters here: INTERPOLATION SPACE. SVG 1.1
// interpolates stop colours in straight (non-premultiplied) sRGB, and this
// engine's SVG output is validated against that. CSS interpolates in
// PREMULTIPLIED alpha (CSS Images 3 §3.4.4), which is a genuinely different
// result the moment a stop is not opaque — see the premultiplication note on
// Eval. Sharing one ramp would force one of the two to be wrong; the shared
// piece is the SHADER, which is where the real geometry and per-pixel cost live.
type gradientRamp struct {
	stops []layout.GradientStop
}

// NumOutputs implements function.Func: a colour ramp outputs R, G, B, A.
func (r *gradientRamp) NumOutputs() int { return 4 }

// Eval implements function.Func, mapping the gradient parameter t to a straight
// RGBA colour in [0,1].
//
// Interpolation is done in PREMULTIPLIED alpha, then un-premultiplied on the way
// out. This is what browsers do and it is not a detail: interpolating straight
// RGBA from `red` to `transparent` walks the colour toward rgba(0,0,0,0), so the
// midpoint is a half-transparent BLACK — a visible grey/black band through what
// should be a clean fade. Premultiplying first keeps the midpoint a
// half-transparent RED, which is the fade an author expects and the one every
// browser paints.
//
// (`transparent` is the reachable case today: this engine's colour parser has no
// rgba()/8-digit-hex syntax yet, so `transparent` — which is defined as
// rgba(0,0,0,0) — is the only non-opaque colour a stop can name. The
// premultiplied path is exactly what makes THAT keyword behave, and it is
// already correct for the alpha syntaxes when they land.)
//
// Outside the first/last stop the ramp holds that endpoint's colour solid. The
// caller has already folded a repeating gradient's parameter back into range via
// the shader's spread mode, so this only sees out-of-range t for a pad gradient
// whose stops do not span [0,1] — where holding the endpoint is correct.
func (r *gradientRamp) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}
	n := len(r.stops)
	if n == 0 {
		// Unreachable (the painter refuses an empty stop list), but Eval must
		// stay total and never index out of range.
		return []float64{0, 0, 0, 0}
	}
	if n == 1 || t <= r.stops[0].Pos {
		return straightFloats(r.stops[0].Color)
	}
	last := r.stops[n-1]
	if t >= last.Pos {
		return straightFloats(last.Color)
	}

	// Find the segment [i-1, i] straddling t. Positions are non-decreasing, so a
	// linear scan is correct, and gradients have few stops so it stays cheap.
	for i := 1; i < n; i++ {
		a, b := r.stops[i-1], r.stops[i]
		if t > b.Pos {
			continue
		}
		span := b.Pos - a.Pos
		if span <= 0 {
			// Two stops at the SAME position: a hard colour break, and the later
			// stop wins from that point on. This is the mechanism CSS gives for
			// a stripe with a crisp edge, so it must not be smoothed over.
			return straightFloats(b.Color)
		}
		f := (t - a.Pos) / span
		return lerpPremul(a.Color, b.Color, f)
	}
	return straightFloats(last.Color)
}

// lerpPremul interpolates from a to b at fraction f in premultiplied-alpha
// space, returning a STRAIGHT (non-premultiplied) RGBA in [0,1] — the form the
// shading seam expects.
//
// The round trip is: multiply each colour channel by its own alpha, lerp all
// four channels, then divide the result's colour channels by the interpolated
// alpha. A fully transparent result has no colour to recover (0/0), so its
// channels are left at zero, which is the only value that composites
// identically either way.
func lerpPremul(a, b color.RGBA, f float64) []float64 {
	ar, ag, ab, aa := chan01(a.R), chan01(a.G), chan01(a.B), chan01(a.A)
	br, bg, bb, ba := chan01(b.R), chan01(b.G), chan01(b.B), chan01(b.A)

	pr := lerp1(ar*aa, br*ba, f)
	pg := lerp1(ag*aa, bg*ba, f)
	pb := lerp1(ab*aa, bb*ba, f)
	pa := lerp1(aa, ba, f)

	if pa <= 0 {
		return []float64{0, 0, 0, 0}
	}
	return []float64{clampUnit(pr / pa), clampUnit(pg / pa), clampUnit(pb / pa), clampUnit(pa)}
}

// straightFloats converts a straight RGBA to the 4-element [0,1] slice
// function.Func.Eval returns.
func straightFloats(c color.RGBA) []float64 {
	return []float64{chan01(c.R), chan01(c.G), chan01(c.B), chan01(c.A)}
}

// chan01 converts a uint8 colour channel to [0,1].
func chan01(v uint8) float64 { return float64(v) / 255 }

// lerp1 linearly interpolates between a and b at fraction f.
func lerp1(a, b, f float64) float64 { return a + f*(b-a) }

// clampUnit clamps v to [0,1], guarding the un-premultiply division against
// floating-point overshoot at a near-zero alpha.
func clampUnit(v float64) float64 { return math.Max(0, math.Min(1, v)) }
