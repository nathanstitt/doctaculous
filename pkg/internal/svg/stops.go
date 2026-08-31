package svg

import (
	"image/color"
	"strings"

	"github.com/nathanstitt/omnidoc/pkg/render"
)

// stop is one resolved gradient stop: a position in [0,1] and a straight
// (non-premultiplied) color, whose alpha carries stop-opacity (see
// resolveStopColor). Positions are non-decreasing across a parsed list —
// see parseStops.
type stop struct {
	offset float64
	c      color.RGBA
}

// stopRamp is a piecewise-linear color ramp built from a gradient element's
// <stop> children, implementing function.Func (pkg/pdf/function) with one
// input (a parametric position, typically clamped to [0,1] by the caller)
// and four outputs (straight R, G, B, A in [0,1]). pkg/render/raster's
// NewAxialShader/NewRadialShader read the 4th output as alpha only for a
// shading they construct themselves (gated by an explicit flag, not by
// output count), so this ramp's alpha reaches the device instead of being
// forced opaque. It never has zero stops — parseStops returns ok=false for
// that case instead.
type stopRamp struct {
	stops []stop
}

// NumOutputs implements function.Func: a color ramp outputs R, G, B, A.
func (r *stopRamp) NumOutputs() int { return 4 }

// Eval implements function.Func. t is the input parameter (in[0]); a missing
// input is treated as 0. t is clamped to the ramp's own [first, last] offset
// range before searching (offsets are already clamped to [0,1] individually,
// but the first stop need not be exactly 0 nor the last exactly 1). Below the
// first stop's offset, or above the last, the ramp holds that endpoint's
// color (and opacity) solid. Between two stops it lerps their RGBA
// components linearly in straight (non-premultiplied) sRGB, per-channel
// including alpha.
func (r *stopRamp) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}

	n := len(r.stops)
	if n == 0 {
		// Unreachable via parseStops (which never returns a zero-stop ramp),
		// but Eval must stay total and never panic/index out of range.
		return []float64{0, 0, 0, 0}
	}
	if n == 1 || t < r.stops[0].offset {
		return rgbaFloats(r.stops[0].c)
	}
	last := r.stops[n-1]
	if t > last.offset {
		return rgbaFloats(last.c)
	}

	// Find the segment [i-1, i] straddling t. Offsets are non-decreasing, so
	// a linear scan is correct; gradients have few stops so this stays cheap.
	for i := 1; i < n; i++ {
		a, b := r.stops[i-1], r.stops[i]
		if t > b.offset {
			continue
		}
		span := b.offset - a.offset
		if span <= 0 {
			// Coincident offsets (a run of stops clamped to the same
			// position): the later stop wins outright, matching SVG's
			// "later stop paints over" behavior for a zero-length segment.
			return rgbaFloats(b.c)
		}
		frac := (t - a.offset) / span
		return []float64{
			lerp(colorChannel(a.c.R), colorChannel(b.c.R), frac),
			lerp(colorChannel(a.c.G), colorChannel(b.c.G), frac),
			lerp(colorChannel(a.c.B), colorChannel(b.c.B), frac),
			lerp(colorChannel(a.c.A), colorChannel(b.c.A), frac),
		}
	}
	return rgbaFloats(last.c)
}

// shadingStops converts r's stops into render.ShadingStop form, the shape
// raster.NewAxialShader/NewRadialShader retain for render.ShadingDescriber.
// This is a pure reformatting of the same offsets/colors Eval already uses —
// it does not affect sampling (Eval is untouched), only what a describable
// shading can report about itself.
func (r *stopRamp) shadingStops() []render.ShadingStop {
	out := make([]render.ShadingStop, len(r.stops))
	for i, s := range r.stops {
		out[i] = render.ShadingStop{Offset: s.offset, Color: s.c}
	}
	return out
}

// lerp linearly interpolates between a and b at fraction f.
func lerp(a, b, f float64) float64 { return a + f*(b-a) }

// colorChannel converts a uint8 color channel to [0,1].
func colorChannel(c uint8) float64 { return float64(c) / 255 }

// rgbaFloats converts c's R,G,B,A channels to a 4-element [0,1] slice, the
// shape function.Func.Eval returns.
func rgbaFloats(c color.RGBA) []float64 {
	return []float64{colorChannel(c.R), colorChannel(c.G), colorChannel(c.B), colorChannel(c.A)}
}

// parseStops reads el's direct <stop> children (SVG-namespace only; any
// other child, including a foreign-namespace <stop>, is ignored) into a
// stopRamp. ctx supplies the CSS cascade that stop-color/stop-opacity
// resolve through — a nil ctx falls back to presentation attributes alone,
// same as Style.apply. It never panics on a nil el, a nil attrs map, or a
// <stop> with no attributes at all.
//
// Per the SVG spec: zero stops means the paint server paints nothing (ok is
// false, ramp is nil); one stop is a solid color across the whole ramp
// (returned as a length-1 stopRamp, which Eval treats as constant).
func parseStops(el *element, ctx *cascadeCtx) (*stopRamp, bool) {
	if el == nil {
		return nil, false
	}

	var stops []stop
	prevOffset := 0.0
	for _, kid := range el.kids {
		if kid == nil || kid.space != svgNS || kid.local != "stop" {
			continue
		}
		off := parseStopOffset(kid)
		if off < prevOffset {
			off = prevOffset // spec: non-decreasing, clamp forward (not a sort)
		}
		prevOffset = off

		c := resolveStopColor(kid, ctx)
		stops = append(stops, stop{offset: off, c: c})
	}

	if len(stops) == 0 {
		return nil, false
	}
	return &stopRamp{stops: stops}, true
}

// parseStopOffset resolves a <stop>'s offset attribute: a percentage
// ("50%") or a bare number ("0.5") — both are a FRACTION of [0,1], not a
// user-unit length, so this uses parseNumber (after stripping a trailing
// "%" and dividing by 100), not parseLength. A missing or unparseable
// offset defaults to 0. The result is clamped to [0,1].
func parseStopOffset(el *element) float64 {
	val, ok := el.attrs["offset"]
	if !ok {
		return 0
	}
	val = strings.TrimSpace(val)
	isPct := strings.HasSuffix(val, "%")
	val = strings.TrimSuffix(val, "%")
	v, ok := parseNumber(val)
	if !ok {
		return 0
	}
	if isPct {
		v /= 100
	}
	return clamp(v, 0, 1)
}

// resolveStopColor resolves a <stop>'s effective color: stop-color
// (default black, "currentColor" resolves against the stop's own 'color'
// property) composed with stop-opacity (default 1) into the alpha channel,
// as a straight (non-premultiplied) RGBA. Both properties are read through
// ctx's cascade, exactly like Style.apply's applyPaint/applyOpacityProp — a
// stop can be styled by a stylesheet rule targeting it, not just its own
// attributes.
//
// The returned alpha reaches the device: stopRamp is a 4-output
// function.Func (rgbaFloats carries R,G,B,A through), and
// NewAxialShader/NewRadialShader read that 4th component as real alpha, so
// e.g. a stop-opacity:0 stop fades to transparent over whatever is behind
// the shape, not to black.
func resolveStopColor(el *element, ctx *cascadeCtx) color.RGBA {
	attr := ctx.resolve(el)

	// 'color' backs currentColor, exactly like Style.apply's applyColorProp,
	// but a <stop> has no inherited Style of its own, so this only ever
	// looks at the stop's own resolved 'color' (defaulting to black).
	cur := color.RGBA{0, 0, 0, 255}
	if v, ok := attr("color"); ok {
		if c, ok := parseColorValue(strings.TrimSpace(v)); ok {
			cur = c
		}
	}

	c := color.RGBA{0, 0, 0, 255} // stop-color initial value: black
	if v, ok := attr("stop-color"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "currentColor":
			c = cur
		case "inherit", "":
			// "inherit" has no meaningful parent for a <stop>; keep default.
		default:
			if parsed, ok := parseColorValue(v); ok {
				c = parsed
			}
		}
	}

	opacity := 1.0
	if v, ok := attr("stop-opacity"); ok {
		v = strings.TrimSpace(v)
		pct := strings.HasSuffix(v, "%")
		num, ok := parseNumber(strings.TrimSuffix(v, "%"))
		if ok {
			if pct {
				num /= 100
			}
			opacity = clamp(num, 0, 1)
		}
	}

	c.A = uint8(clamp(opacity*255, 0, 255))
	return c
}
