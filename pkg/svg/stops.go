package svg

import (
	"image/color"
	"strings"
)

// stop is one resolved gradient stop: a position in [0,1] and a color whose
// alpha has already been folded in (see resolveStopColor). Positions are
// non-decreasing across a parsed list — see parseStops.
type stop struct {
	offset float64
	c      color.RGBA
}

// stopRamp is a piecewise-linear color ramp built from a gradient element's
// <stop> children, implementing function.Func (pkg/pdf/function) with one
// input (a parametric position, typically clamped to [0,1] by the caller)
// and three outputs (R, G, B in [0,1]), matching the shape
// NewAxialShader/NewRadialShader (pkg/render/raster/shading.go) expect. It
// never has zero stops — parseStops returns ok=false for that case instead.
type stopRamp struct {
	stops []stop
}

// NumOutputs implements function.Func: a color ramp always outputs R, G, B.
func (r *stopRamp) NumOutputs() int { return 3 }

// Eval implements function.Func. t is the input parameter (in[0]); a missing
// input is treated as 0. t is clamped to the ramp's own [first, last] offset
// range before searching (offsets are already clamped to [0,1] individually,
// but the first stop need not be exactly 0 nor the last exactly 1). Below the
// first stop's offset, or above the last, the ramp holds that endpoint's
// color solid. Between two stops it lerps their colors linearly in straight
// (non-premultiplied) sRGB.
func (r *stopRamp) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}

	n := len(r.stops)
	if n == 0 {
		// Unreachable via parseStops (which never returns a zero-stop ramp),
		// but Eval must stay total and never panic/index out of range.
		return []float64{0, 0, 0}
	}
	if n == 1 || t < r.stops[0].offset {
		return rgbFloats(r.stops[0].c)
	}
	last := r.stops[n-1]
	if t > last.offset {
		return rgbFloats(last.c)
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
			return rgbFloats(b.c)
		}
		frac := (t - a.offset) / span
		return []float64{
			lerp(colorChannel(a.c.R), colorChannel(b.c.R), frac),
			lerp(colorChannel(a.c.G), colorChannel(b.c.G), frac),
			lerp(colorChannel(a.c.B), colorChannel(b.c.B), frac),
		}
	}
	return rgbFloats(last.c)
}

// lerp linearly interpolates between a and b at fraction f.
func lerp(a, b, f float64) float64 { return a + f*(b-a) }

// colorChannel converts a uint8 color channel to [0,1].
func colorChannel(c uint8) float64 { return float64(c) / 255 }

// rgbFloats converts c's R,G,B channels to a 3-element [0,1] slice, the
// shape function.Func.Eval returns.
func rgbFloats(c color.RGBA) []float64 {
	return []float64{colorChannel(c.R), colorChannel(c.G), colorChannel(c.B)}
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
// property) composed with stop-opacity (default 1) into the alpha channel.
// Both properties are read through ctx's cascade, exactly like
// Style.apply's applyPaint/applyOpacityProp — a stop can be styled by a
// stylesheet rule targeting it, not just its own attributes.
//
// The returned color's alpha is then folded back into the RGB channels by
// stopRamp (via rgbFloats, which only ever reads R/G/B): a stopRamp is a
// function.Func with exactly 3 outputs (no alpha channel), so stop-opacity
// is represented as if composited over a black backdrop (color * opacity)
// rather than as true alpha. This is the same approximation the wider
// shading pipeline already makes (pkg/render/raster/shading.go's toRGBA
// hard-codes alpha to opaque) — a real alpha-aware gradient composite is
// tracked as a follow-up, not this task's scope.
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

	return premultiplyOverBlack(c, opacity)
}

// premultiplyOverBlack scales c's R/G/B channels by opacity, approximating
// stop-opacity as if the (non-premultiplied-alpha-capable) 3-channel ramp
// were composited over a black backdrop: at opacity 0 the stop contributes
// black, at opacity 1 it contributes c unchanged. Alpha is left at c.A
// (always opaque going in) since stopRamp's outputs never read it.
func premultiplyOverBlack(c color.RGBA, opacity float64) color.RGBA {
	opacity = clamp(opacity, 0, 1)
	scale := func(v uint8) uint8 {
		return uint8(clamp(float64(v)*opacity, 0, 255))
	}
	return color.RGBA{R: scale(c.R), G: scale(c.G), B: scale(c.B), A: c.A}
}
