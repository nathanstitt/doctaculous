package svg

import (
	"image/color"
	"math"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/filtereffects"
	svgfilter "github.com/nathanstitt/doctaculous/pkg/svg/filter"
)

// resolveFilterFunctions lowers a CSS `filter` function list into a single
// synthetic *Filter whose primitive graph is the concatenation of each
// function's equivalent chain.
//
// Each function's lowering is the Filter Effects spec's own — that is the
// whole point of doing it this way. blur() IS an feGaussianBlur, grayscale(a)
// IS an feColorMatrix type="saturate" with 1-a, drop-shadow() IS the
// feDropShadow chain. Implementing separate pixel code per function would
// double the surface for a divergence that no test would catch, since each
// function's output would be checked only against itself.
//
// ok=false means the value is invalid AS A WHOLE and the element renders
// UNFILTERED. Per CSS error handling an invalid declaration is ignored
// entirely, so a list with one bad function does not fall back to the
// functions that did parse — the corpus's one-invalid-function-in-list fixture
// (`grayscale() hue-rotate(random) opacity(0.5)`) renders the plain rect, not
// a greyed and half-transparent one.
//
// A url() entry that resolves to nothing is DROPPED while the rest of the list
// still applies (one-invalid-url-in-list keeps its grayscale and opacity),
// which is why an unresolvable reference here is not an error the way a bare
// `filter="url(#missing)"` is.
func (b *sceneBuilder) resolveFilterFunctions(value string, ctx *cascadeCtx, el *element, st Style, depth int) (*Filter, bool) {
	funcs, ok := filtereffects.Parse(value, filterLengthResolver(st))
	if !ok {
		b.warnOnceMsg("svg-filter-function-invalid",
			"svg: ignoring an invalid filter function list; the element was rendered unfiltered")
		return nil, false
	}
	if len(funcs) == 0 {
		return nil, true
	}

	// A CSS filter function has NO filter region — see Filter.Unbounded. The
	// region fields below are the <filter> default and act only as a fallback
	// if the renderer cannot determine the canvas; Unbounded is what actually
	// governs.
	out := &Filter{
		Units:          "objectBoundingBox",
		PrimitiveUnits: "userSpaceOnUse",
		RegionX:        -0.1, RegionY: -0.1, RegionW: 1.2, RegionH: 1.2,
		Unbounded: true,
	}

	// Each function consumes the previous function's output, so the list
	// composes in sequence. The first consumes SourceGraphic.
	prev := FilterInput{Kind: InputSourceGraphic}
	for _, fn := range funcs {
		if len(out.Primitives)+dropShadowChainLength > maxFilterPrimitives {
			b.warnOnceMsg("svg-filter-primitive-cap",
				"svg: <filter> exceeded the primitive cap; the rest were dropped")
			break
		}
		next, unsupported := b.lowerFilterFunction(fn, prev, ctx, el, out, depth)
		if unsupported != "" && out.Unsupported == "" {
			out.Unsupported = unsupported
		}
		prev = next
	}
	if len(out.Primitives) == 0 {
		// Every entry was a url() that resolved to nothing, or a no-op: no
		// filtering, which is valid rather than an error.
		return nil, true
	}
	return out, true
}

// lowerFilterFunction appends one function's primitive chain to out and
// returns the input a following function should consume.
//
// unsupported is non-empty when the function referenced a <filter> containing
// a primitive this engine does not implement; it propagates so the whole list
// degrades to unfiltered with an accurate name, matching how a direct
// url(#filter) reference behaves.
func (b *sceneBuilder) lowerFilterFunction(fn filtereffects.Function, in FilterInput, ctx *cascadeCtx, el *element, out *Filter, depth int) (FilterInput, string) {
	appendPrim := func(p FilterPrimitive) FilterInput {
		out.Primitives = append(out.Primitives, p)
		return FilterInput{Kind: InputResult, Index: len(out.Primitives) - 1}
	}

	switch fn.Kind {
	case filtereffects.FuncURL:
		return b.lowerFilterURL(fn.Ref, in, out, depth)

	case filtereffects.FuncBlur:
		if fn.StdDeviation <= 0 {
			return in, "" // blur(0): the identity, so emit nothing
		}
		return appendPrim(FilterPrimitive{
			Kind: PrimitiveGaussianBlur, In: in,
			StdDevX: fn.StdDeviation, StdDevY: fn.StdDeviation,
			// The CSS filter functions are defined to operate in sRGB, NOT
			// the linearRGB that SVG's own primitives default to. Getting this
			// wrong makes every CSS blur() and drop-shadow() visibly lighter
			// than the reference — it is the same colour-space trap the
			// <filter> path carries, in the opposite direction.
			Space: FilterSRGB,
		}), ""

	case filtereffects.FuncDropShadow:
		return b.lowerDropShadowFunction(fn, in, ctx, el, out), ""

	case filtereffects.FuncHueRotate:
		return appendPrim(colorMatrixPrimitive(in, svgfilter.HueRotateMatrix(fn.Angle))), ""

	case filtereffects.FuncSaturate:
		return appendPrim(colorMatrixPrimitive(in, svgfilter.SaturateMatrix(float32(fn.Amount)))), ""

	case filtereffects.FuncGrayscale:
		// grayscale(a) is saturate(1-a): a full greyscale removes all
		// saturation. The amount is clamped to 1 HERE (unlike saturate's own
		// coefficient) because grayscale(2) must not over-rotate past
		// greyscale into inverted saturation — the corpus's
		// color-adjust-functions-2 fixture renders grayscale(2) identically to
		// grayscale(1).
		a := math.Min(fn.Amount, 1)
		return appendPrim(colorMatrixPrimitive(in, svgfilter.SaturateMatrix(float32(1-a)))), ""

	case filtereffects.FuncSepia:
		return appendPrim(colorMatrixPrimitive(in, sepiaMatrix(math.Min(fn.Amount, 1)))), ""

	case filtereffects.FuncInvert:
		return appendPrim(colorMatrixPrimitive(in, invertMatrix(math.Min(fn.Amount, 1)))), ""

	case filtereffects.FuncBrightness:
		return appendPrim(colorMatrixPrimitive(in, brightnessMatrix(fn.Amount))), ""

	case filtereffects.FuncContrast:
		return appendPrim(colorMatrixPrimitive(in, contrastMatrix(fn.Amount))), ""

	case filtereffects.FuncOpacity:
		return appendPrim(colorMatrixPrimitive(in, opacityMatrix(math.Min(fn.Amount, 1)))), ""
	}
	return in, ""
}

// lowerFilterURL splices a referenced <filter>'s primitives into the lowered
// graph, so `url(#a) url(#b)` chains b over a's output.
//
// The referenced graph's internal `in`/`result` wiring is index-based, so every
// InputResult index is SHIFTED by the offset at which the graph is spliced in,
// and its SourceGraphic inputs are rebound to whatever the previous function
// produced. Missing either half silently rewires the graph to a different
// primitive's output — which renders plausibly and wrongly.
func (b *sceneBuilder) lowerFilterURL(ref string, in FilterInput, out *Filter, depth int) (FilterInput, string) {
	f, ok := b.resolveFilterRefAt(ref, depth+1)
	if !ok || f == nil {
		// An unresolvable url() inside a LIST is dropped rather than dropping
		// the element — see resolveFilterFunctions's doc comment.
		return in, ""
	}
	if f.RegionInvalid || len(f.Primitives) == 0 {
		return in, ""
	}
	offset := len(out.Primitives)
	if offset+len(f.Primitives) > maxFilterPrimitives {
		return in, ""
	}
	for _, p := range f.Primitives {
		p.In = rebindInput(p.In, in, offset)
		p.In2 = rebindInput(p.In2, in, offset)
		if p.MergeInputs != nil {
			nodes := make([]FilterInput, len(p.MergeInputs))
			for i, n := range p.MergeInputs {
				nodes[i] = rebindInput(n, in, offset)
			}
			p.MergeInputs = nodes
		}
		out.Primitives = append(out.Primitives, p)
	}
	return FilterInput{Kind: InputResult, Index: len(out.Primitives) - 1}, f.Unsupported
}

// rebindInput relocates one input of a spliced-in graph: an InputResult index
// shifts by offset, while SourceGraphic becomes the previous function's output
// (SourceAlpha stays the ORIGINAL element's alpha, which is what the spec's
// chaining means — each filter in the list still sees the source element).
func rebindInput(fi FilterInput, prev FilterInput, offset int) FilterInput {
	switch fi.Kind {
	case InputResult:
		return FilterInput{Kind: InputResult, Index: fi.Index + offset}
	case InputSourceGraphic:
		return prev
	default:
		return fi
	}
}

// lowerDropShadowFunction lowers drop-shadow() into the same five-primitive
// chain <feDropShadow> expands to, so the two spellings of the same effect
// cannot drift apart.
func (b *sceneBuilder) lowerDropShadowFunction(fn filtereffects.Function, in FilterInput, ctx *cascadeCtx, el *element, out *Filter) FilterInput {
	base := len(out.Primitives)
	shadow := dropShadowFunctionColor(fn.Color, el, ctx)

	// Like <feDropShadow>, and like blur(), the CSS function works in sRGB.
	space := FilterSRGB
	blurIdx, offsetIdx, floodIdx, compositeIdx := base, base+1, base+2, base+3

	out.Primitives = append(out.Primitives,
		FilterPrimitive{Kind: PrimitiveGaussianBlur, In: in, Space: space,
			StdDevX: fn.StdDeviation, StdDevY: fn.StdDeviation},
		FilterPrimitive{Kind: PrimitiveOffset, In: FilterInput{Kind: InputResult, Index: blurIdx},
			Space: space, Dx: fn.Dx, Dy: fn.Dy},
		FilterPrimitive{Kind: PrimitiveFlood, Space: space, FloodColor: shadow},
		FilterPrimitive{Kind: PrimitiveComposite,
			In:    FilterInput{Kind: InputResult, Index: floodIdx},
			In2:   FilterInput{Kind: InputResult, Index: offsetIdx},
			Space: space, Operator: CompositeIn},
		FilterPrimitive{Kind: PrimitiveMerge,
			In:    FilterInput{Kind: InputResult, Index: compositeIdx},
			Space: space,
			MergeInputs: []FilterInput{
				{Kind: InputResult, Index: compositeIdx},
				in,
			}},
	)
	return FilterInput{Kind: InputResult, Index: len(out.Primitives) - 1}
}

// dropShadowFunctionColor resolves drop-shadow()'s colour argument.
//
// An OMITTED colour means the element's own `color` property, not black: the
// corpus's drop-shadow-function-color-as-attribute fixture sets color="blue"
// on the element and writes drop-shadow(10 20) with no colour, and the shadow
// comes out blue. `currentColor` resolves to the same value explicitly.
func dropShadowFunctionColor(raw string, el *element, ctx *cascadeCtx) color.RGBA {
	cur := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	if v, ok := ctx.resolve(el)("color"); ok {
		if c, ok := parseColorValue(strings.TrimSpace(v)); ok {
			cur = c
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "currentcolor") {
		return cur
	}
	if c, ok := parseColorValue(raw); ok {
		return c
	}
	return cur
}

// colorMatrixPrimitive builds a colour-matrix primitive in the sRGB space the
// CSS filter functions are defined to use.
func colorMatrixPrimitive(in FilterInput, m svgfilter.ColorMatrix) FilterPrimitive {
	return FilterPrimitive{
		Kind:   PrimitiveColorMatrix,
		In:     in,
		Space:  FilterSRGB,
		Matrix: toFloat64Matrix(m),
	}
}

// brightnessMatrix returns the matrix for brightness(a): a linear scale of
// every colour channel, leaving alpha alone.
//
// The spec defines it as an feComponentTransfer with type="linear"
// slope="a" intercept="0" on R, G and B; a diagonal colour matrix is exactly
// that, which is what lets this ship without an feComponentTransfer.
func brightnessMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, 0,
		0, f, 0, 0, 0,
		0, 0, f, 0, 0,
		0, 0, 0, 1, 0,
	}
}

// contrastMatrix returns the matrix for contrast(a): slope a about the 0.5
// midpoint, i.e. the spec's linear transfer with intercept (0.5 - a/2).
func contrastMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	c := float32(0.5 - a/2)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, c,
		0, f, 0, 0, c,
		0, 0, f, 0, c,
		0, 0, 0, 1, 0,
	}
}

// invertMatrix returns the matrix for invert(a), interpolating each channel
// between itself and its complement: v' = a·(1-v) + (1-a)·v = v·(1-2a) + a.
func invertMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(1 - 2*a)
	c := float32(a)
	return svgfilter.ColorMatrix{
		f, 0, 0, 0, c,
		0, f, 0, 0, c,
		0, 0, f, 0, c,
		0, 0, 0, 1, 0,
	}
}

// opacityMatrix returns the matrix for opacity(a): a linear scale of the ALPHA
// channel only, the spec's feComponentTransfer type="linear" on funcA.
func opacityMatrix(a float64) svgfilter.ColorMatrix {
	f := float32(a)
	return svgfilter.ColorMatrix{
		1, 0, 0, 0, 0,
		0, 1, 0, 0, 0,
		0, 0, 1, 0, 0,
		0, 0, 0, f, 0,
	}
}

// sepiaMatrix returns the matrix for sepia(a), interpolating between the
// identity (a=0) and the spec's fixed full-sepia matrix (a=1).
//
// The full-sepia coefficients are the Filter Effects spec's verbatim; they are
// NOT derivable from a saturation or hue formula, which is why they are
// written out rather than computed.
func sepiaMatrix(a float64) svgfilter.ColorMatrix {
	full := [9]float64{
		0.393, 0.769, 0.189,
		0.349, 0.686, 0.168,
		0.272, 0.534, 0.131,
	}
	identity := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	var c [9]float32
	for i := range c {
		c[i] = float32(identity[i] + a*(full[i]-identity[i]))
	}
	return svgfilter.ColorMatrix{
		c[0], c[1], c[2], 0, 0,
		c[3], c[4], c[5], 0, 0,
		c[6], c[7], c[8], 0, 0,
		0, 0, 0, 1, 0,
	}
}

// filterLengthResolver returns the LengthResolver filtereffects.Parse uses to
// turn a CSS length token into SVG user units.
//
// It REJECTS a percentage, which is what makes blur(50%) and
// drop-shadow(blue 3% 4% 5%) invalid: `blur()` takes a <length>, and a
// percentage is not one. The corpus renders both of those fixtures completely
// unfiltered, so accepting a percentage here (resolving it against the
// viewport, say) would render them blurred and miss both.
//
// em/ex resolve against the element's OWN resolved font-size, which is why the
// resolved Style is threaded down here rather than the raw element: the
// corpus's drop-shadow-function-em-values fixture depends on it.
func filterLengthResolver(st Style) filtereffects.LengthResolver {
	return func(token string) (float64, bool) {
		token = strings.TrimSpace(token)
		if token == "" || strings.HasSuffix(token, "%") {
			return 0, false
		}
		lower := strings.ToLower(token)
		switch {
		case strings.HasSuffix(lower, "em"):
			n, ok := parseNumber(lower[:len(lower)-2])
			if !ok {
				return 0, false
			}
			return n * st.FontSizePt(), true
		case strings.HasSuffix(lower, "ex"):
			n, ok := parseNumber(lower[:len(lower)-2])
			if !ok {
				return 0, false
			}
			// ex is half the font size, the same approximation applySpacing
			// and parseLength already use throughout this package.
			return n * st.FontSizePt() / 2, true
		}
		// parseLength handles the absolute units (px, pt, pc, mm, cm, in) and
		// a bare number. Its percentage branch is unreachable here: a
		// percentage was rejected above.
		return parseLength(token, 0)
	}
}
