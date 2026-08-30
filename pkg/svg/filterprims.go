package svg

import (
	"strings"

	svgfilter "github.com/nathanstitt/omnidoc/pkg/svg/filter"
)

// CompositeOperator selects which Porter-Duff (or arithmetic) rule an
// feComposite applies. The values mirror pkg/svg/filter's own enum one for one;
// the scene keeps its own so pkg/svg's public API does not leak the pixel-math
// package's types into every consumer.
type CompositeOperator int

const (
	// CompositeOver is the default operator: A over B.
	CompositeOver CompositeOperator = iota
	// CompositeIn keeps A only where B is opaque.
	CompositeIn
	// CompositeOut keeps A only where B is transparent.
	CompositeOut
	// CompositeAtop keeps A where B is opaque and B elsewhere.
	CompositeAtop
	// CompositeXor keeps each input only where the other is transparent.
	CompositeXor
	// CompositeArithmetic applies k1·i1·i2 + k2·i1 + k3·i2 + k4 per channel.
	CompositeArithmetic
)

// parseCompositeOperator resolves feComposite's `operator`, defaulting to
// "over".
//
// An UNRECOGNIZED operator falls back to the default rather than disabling the
// primitive — the corpus's invalid-operator fixture (operator="z") renders as
// a plain over-composite, so treating it as an error would blank a fixture the
// reference renders normally.
func parseCompositeOperator(v string) CompositeOperator {
	switch strings.TrimSpace(v) {
	case "in":
		return CompositeIn
	case "out":
		return CompositeOut
	case "atop":
		return CompositeAtop
	case "xor":
		return CompositeXor
	case "arithmetic":
		return CompositeArithmetic
	default:
		return CompositeOver
	}
}

// readGaussianBlur resolves feGaussianBlur's stdDeviation into per-axis
// deviations.
//
// stdDeviation is one or two <number>s. The corpus pins three error cases, all
// of which resolve to ZERO (the primitive becomes the identity and the element
// renders UNBLURRED rather than disappearing):
//
//   - absent or empty: the attribute's default of 0
//   - negative on either axis: "an error" per SVG, and negative-stdDeviation
//     renders the plain rect
//   - more than two values: stdDeviation-with-multiple-values ("5 10 15 20")
//     likewise renders the plain rect
//
// A single value applies to BOTH axes; two values are x then y, and either may
// legitimately be zero on its own (stdDeviation="0 5" blurs vertically only).
func readGaussianBlur(p *FilterPrimitive, el *element) {
	v, ok := el.attrs["stdDeviation"]
	if !ok {
		return
	}
	nums := parseNumberList(v)
	if len(nums) == 0 || len(nums) > 2 {
		return
	}
	x := nums[0]
	y := x
	if len(nums) == 2 {
		y = nums[1]
	}
	if x < 0 || y < 0 {
		return
	}
	p.StdDevX, p.StdDevY = x, y
}

// readComposite resolves feComposite's operator and arithmetic coefficients.
//
// k1..k4 default to 0 and are NOT range-checked. The corpus's
// operator=arithmetic-and-invalid-k1-4 fixture carries k4="100" and renders
// opaque white, proving the reference feeds the coefficients through verbatim
// and clamps only the per-channel RESULT — despite that fixture's own <desc>
// claiming the values "must be in 0..1 range".
func readComposite(p *FilterPrimitive, el *element) {
	p.Operator = parseCompositeOperator(el.attrs["operator"])
	p.K1 = plainNumberAttr(el, "k1", 0)
	p.K2 = plainNumberAttr(el, "k2", 0)
	p.K3 = plainNumberAttr(el, "k3", 0)
	p.K4 = plainNumberAttr(el, "k4", 0)
}

// readBlend resolves feBlend's `mode` into the canonical PDF /BM name the
// renderer's shared blend table is keyed by, leaving "" for normal and for any
// unrecognized mode (both of which composite source-over per SVG's
// fall-back-to-the-initial-value error handling).
func readBlend(p *FilterPrimitive, el *element) {
	mode := strings.TrimSpace(el.attrs["mode"])
	if name, ok := svgfilter.BlendModeName(mode); ok {
		p.BlendMode = name
	}
}

// readColorMatrix resolves feColorMatrix's type and values into a single 5x4
// matrix.
//
// Every shorthand type is EXPANDED here rather than carried as a type plus a
// coefficient, so the renderer implements one operation and the spec's three
// shorthand formulas are testable on their own. The error handling the corpus
// pins:
//
//   - no type, or an unrecognized type: treated as type="matrix", so
//     invalid-type (type="qwe" with a full values list) applies those values
//     rather than falling back to the identity
//   - type="matrix" with a values list that is not exactly 20 numbers (absent,
//     empty, too few, too many): the identity
//   - saturate/hueRotate with no value: the spec's own default (1 and 0
//     respectively), each of which is the identity
//   - saturate's coefficient and hueRotate's angle are NOT clamped; the corpus
//     renders values="10" and values="-1" as the raw matrix evaluates
func readColorMatrix(p *FilterPrimitive, el *element) {
	p.Matrix = toFloat64Matrix(svgfilter.IdentityColorMatrix)

	raw, hasValues := el.attrs["values"]
	nums := parseNumberList(raw)
	numsOK := hasValues && len(nums) > 0

	switch strings.TrimSpace(el.attrs["type"]) {
	case "saturate":
		s := 1.0
		if numsOK && len(nums) == 1 {
			s = nums[0]
		}
		p.Matrix = toFloat64Matrix(svgfilter.SaturateMatrix(float32(s)))
	case "hueRotate":
		deg := 0.0
		if numsOK && len(nums) == 1 {
			deg = nums[0]
		}
		p.Matrix = toFloat64Matrix(svgfilter.HueRotateMatrix(deg))
	case "luminanceToAlpha":
		p.Matrix = toFloat64Matrix(svgfilter.LuminanceToAlphaMatrix)
	default:
		// "matrix", absent, and any unrecognized type.
		if numsOK && len(nums) == 20 {
			copy(p.Matrix[:], nums)
		}
	}
}

// toFloat64Matrix widens a pixel-math ColorMatrix into the scene's float64
// storage. The scene keeps float64 to match every other geometric value it
// holds; the narrowing back to float32 happens once, in the renderer.
func toFloat64Matrix(m svgfilter.ColorMatrix) [20]float64 {
	var out [20]float64
	for i, v := range m {
		out[i] = float64(v)
	}
	return out
}

// readMergeNodes resolves an feMerge's <feMergeNode> children into inputs, in
// document order (which is painting order — the first node is the bottom).
//
// A child that is not an feMergeNode is ignored per SVG's forgiving
// unknown-element handling. Each node's `in` resolves through the same
// resolveFilterInput the other primitives use, so an feMergeNode with no `in`
// takes the same positional default (SourceGraphic for the filter's first
// primitive, the previous primitive's output otherwise) rather than a rule of
// its own.
func readMergeNodes(p *FilterPrimitive, el *element, results map[string]int, index int) {
	// A non-nil empty slice distinguishes "an feMerge with no nodes" (which
	// produces transparent black) from a primitive that has no merge inputs
	// because it is not an feMerge at all.
	p.MergeInputs = []FilterInput{}
	for _, kid := range el.kids {
		if kid.space != svgNS || kid.local != "feMergeNode" {
			continue
		}
		if len(p.MergeInputs) >= maxFilterPrimitives {
			// The same amplification bound the primitive count carries: each
			// merge node is a full-region composite pass.
			break
		}
		p.MergeInputs = append(p.MergeInputs, resolveFilterInput(kid.attrs["in"], results, index))
	}
}
