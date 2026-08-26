package pdfwrite

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// pendingShading is a native /Shading dictionary referenced by a page's content
// stream, recorded so the document assembler can allocate it a real indirect
// object and register it in the page's /Shading resource sub-dictionary. This
// mirrors pendingImage's role for image XObjects.
type pendingShading struct {
	name string // resource name used in the content stream ("Sh0", "Sh1", ...)
	dict Dict   // the /ShadingType 2 or 3 dictionary, /Function already embedded
}

// describableShading is the subset of render.ShadingDesc this writer can turn
// into a native PDF /Shading dictionary. buildShadingDict returns ok=false (and
// never emits a dict) when the description falls outside that subset — the
// caller (FillShading) falls back to rasterizing in that case.
//
// canEmitShading applies the decision rule from the shader-describe design: a
// native shading is emitted only when doing so cannot silently misrender the
// gradient. PDF's /Shading has no alpha channel (real transparency needs a soft
// mask, out of scope here) and /Extend only models "pad" (reflect/repeat have
// no native equivalent), so either condition forces a fallback to rasterizing,
// which already handles both correctly.
func canEmitShading(desc render.ShadingDesc) (reason string, ok bool) {
	if len(desc.Stops) < 2 {
		return "fewer than two stops", false
	}
	for _, s := range desc.Stops {
		if s.Color.A != 0xFF {
			return "a stop carries alpha (needs a soft mask, not yet supported)", false
		}
	}
	if desc.Spread != render.SpreadPad {
		return "spread method has no native PDF /Extend equivalent", false
	}
	return "", true
}

// buildShadingDict converts a describable, opaque, pad-spread gradient
// description into a PDF /Shading dictionary (ISO 32000-1 §8.7.4.5): axial
// (ShadingType 2) or radial (ShadingType 3), DeviceRGB, both ends extended, with
// a /Function built from the stop ramp by buildRampFunction. Callers must first
// confirm canEmitShading(desc) returned ok=true.
func buildShadingDict(desc render.ShadingDesc) Dict {
	dict := Dict{
		"ColorSpace": Name("DeviceRGB"),
		"Extend":     Array{Bool(true), Bool(true)},
		"Function":   buildRampFunction(desc.Stops),
	}
	switch desc.Kind {
	case render.ShadingRadial:
		dict["ShadingType"] = Int(3)
		dict["Coords"] = Array{
			Real(desc.Coords[0]), Real(desc.Coords[1]), Real(desc.Coords[2]),
			Real(desc.Coords[3]), Real(desc.Coords[4]), Real(desc.Coords[5]),
		}
	default: // render.ShadingAxial
		dict["ShadingType"] = Int(2)
		dict["Coords"] = Array{
			Real(desc.Coords[0]), Real(desc.Coords[1]), Real(desc.Coords[2]), Real(desc.Coords[3]),
		}
	}
	return dict
}

// minStopSpan is the minimum offset gap this writer will treat as a distinct
// PDF /Bounds interval. Two (or more) stops at the same, or nearly the same,
// offset make a hard color break in SVG/CSS ("later stop wins" — see
// pkg/svg/stops.go's stopRamp.Eval); PDF's Type 3 stitching function instead
// requires /Bounds to be STRICTLY increasing, so a zero (or near-zero) width
// subdomain would be malformed and reader behavior on it is undefined. Nudging
// the later offset forward by this amount keeps /Bounds strictly increasing
// while the resulting subdomain is visually imperceptible (a fraction of a
// point at any realistic gradient size), preserving the hard break instead of
// smearing it into a visible ramp.
const minStopSpan = 1e-6

// buildRampFunction converts a non-decreasing stop list into a PDF /Function:
// a single FunctionType 2 (exponential, N=1 = linear) for exactly two stops, or
// a FunctionType 3 (stitching) over one Type 2 per segment for more than two.
// Coincident (or near-coincident) offsets are nudged apart by minStopSpan so
// /Bounds stays strictly increasing (PDF requires this; a zero-width interval
// is undefined behavior for a reader) while still rendering as a hard color
// break rather than a smeared ramp, since the nudge is imperceptibly small.
//
// Callers must pass at least two stops (canEmitShading enforces this before
// buildShadingDict is ever reached).
func buildRampFunction(stops []render.ShadingStop) Dict {
	offsets := spreadOffsets(stops)
	n := len(stops)
	if n == 2 {
		return exponentialFunc(stops[0], stops[1])
	}

	functions := make(Array, n-1)
	for i := 0; i < n-1; i++ {
		functions[i] = exponentialFunc(stops[i], stops[i+1])
	}
	bounds := make(Array, n-2)
	for i := 0; i < n-2; i++ {
		bounds[i] = Real(offsets[i+1])
	}
	encode := make(Array, 0, 2*(n-1))
	for i := 0; i < n-1; i++ {
		encode = append(encode, Real(0), Real(1))
	}
	return Dict{
		"FunctionType": Int(3),
		"Domain":       Array{Real(0), Real(1)},
		"Functions":    functions,
		"Bounds":       bounds,
		"Encode":       encode,
	}
}

// exponentialFunc builds a FunctionType 2 (exponential interpolation, N=1 so it
// is exactly linear) dictionary ramping from c0 to c1 across straight RGB.
// Alpha is not encoded — canEmitShading already required every stop be fully
// opaque before a caller reaches here.
func exponentialFunc(c0, c1 render.ShadingStop) Dict {
	return Dict{
		"FunctionType": Int(2),
		"Domain":       Array{Real(0), Real(1)},
		"C0":           rgbArray(c0.Color),
		"C1":           rgbArray(c1.Color),
		"N":            Int(1),
	}
}

// rgbArray converts c's straight R,G,B channels (alpha ignored) to a 3-element
// PDF array in [0,1], the shape /C0 and /C1 need for a DeviceRGB ramp.
func rgbArray(c color.RGBA) Array {
	return Array{
		Real(float64(c.R) / 255),
		Real(float64(c.G) / 255),
		Real(float64(c.B) / 255),
	}
}

// spreadOffsets returns stops' offsets with any coincident (or too-close) run
// nudged forward by minStopSpan so consecutive values are strictly increasing,
// matching PDF's requirement that /Bounds (built from the interior offsets) be
// strictly increasing. Only interior offsets (index 1..n-2) become /Bounds
// entries, but every offset is normalized here so the nudging is monotonic
// across the whole list rather than computed piecemeal.
func spreadOffsets(stops []render.ShadingStop) []float64 {
	out := make([]float64, len(stops))
	for i, s := range stops {
		v := s.Offset
		if i > 0 && v <= out[i-1]+minStopSpan {
			v = out[i-1] + minStopSpan
		}
		out[i] = v
	}
	return out
}
