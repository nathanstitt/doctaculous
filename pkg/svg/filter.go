package svg

import (
	"image/color"
	"strings"
)

// maxFilterChainDepth bounds a chain of filter references reached while
// resolving a filter (today only through a filtered element inside another
// filter's referenced content), mirroring maxMaskChainDepth's rationale: the
// cycle guard catches a filter that references itself, while this bounds a
// long ACYCLIC chain from costing unbounded recursion depth.
const maxFilterChainDepth = 64

// maxFilterPrimitives bounds how many primitives one <filter> may declare.
// Every primitive allocates a full filter-region pixel buffer while the graph
// runs (see pkg/svg/filter.Buffer), so an unbounded primitive count is
// unbounded memory AND time from a tiny input file — the same build-time
// amplification class of attack a prior PR found. Real filters use a handful
// of primitives; a drop-shadow chain, the longest common graph, uses five.
const maxFilterPrimitives = 64

// PrimitiveKind identifies which <fe*> element a FilterPrimitive came from.
// An unsupported-but-recognized primitive is carried as its own kind (rather
// than dropped at parse time) so the renderer can log precisely which feature
// caused it to degrade — see Filter.Unsupported.
type PrimitiveKind int

const (
	// PrimitiveFlood is <feFlood>: fill the subregion with flood-color.
	PrimitiveFlood PrimitiveKind = iota
	// PrimitiveOffset is <feOffset>: translate the input by dx/dy.
	PrimitiveOffset
	// PrimitiveGaussianBlur is <feGaussianBlur>: blur the input by
	// stdDeviation on each axis.
	PrimitiveGaussianBlur
	// PrimitiveComposite is <feComposite>: combine `in` and `in2` under a
	// Porter-Duff operator, or the arithmetic rule.
	PrimitiveComposite
	// PrimitiveBlend is <feBlend>: composite `in` over `in2` through a CSS
	// blend mode.
	PrimitiveBlend
	// PrimitiveColorMatrix is <feColorMatrix>: apply a 5x4 matrix to each
	// pixel's straight-alpha channels.
	PrimitiveColorMatrix
	// PrimitiveMerge is <feMerge>: composite its <feMergeNode> inputs in
	// document order with source-over.
	PrimitiveMerge
)

// InputKind identifies what a primitive's `in`/`in2` attribute names.
// Resolution happens at PARSE time (the document index is discarded when
// Parse returns), so the renderer never resolves a name.
type InputKind int

const (
	// InputResult refers to an earlier primitive's output by index; see
	// FilterInput.Index. This is what a resolved `result` name becomes.
	InputResult InputKind = iota
	// InputSourceGraphic is the implicit SourceGraphic: the filtered
	// element rendered normally.
	InputSourceGraphic
	// InputSourceAlpha is the implicit SourceAlpha: SourceGraphic's alpha
	// channel with the color channels zeroed.
	InputSourceAlpha
)

// FilterInput is one resolved primitive input.
type FilterInput struct {
	Kind InputKind
	// Index is the index into Filter.Primitives of the primitive whose
	// output this input refers to, valid only when Kind is InputResult. It
	// is ALWAYS less than the index of the primitive holding it — `in` may
	// only name an EARLIER `result` — which is what makes a cycle impossible
	// by construction rather than by a runtime guard, and lets the renderer
	// evaluate the graph as a simple forward pass.
	Index int
}

// FilterPrimitive is one resolved <fe*> element in a filter's graph.
type FilterPrimitive struct {
	Kind PrimitiveKind

	// In is the primitive's first input.
	In FilterInput

	// In2 is the second input, used by feComposite and feBlend. It resolves
	// by exactly the same rules as In (see resolveFilterInput), including the
	// "an undefined name falls back to the previous primitive's output"
	// fallback, so a graph never has to special-case it.
	In2 FilterInput

	// MergeInputs are feMerge's <feMergeNode> inputs in DOCUMENT ORDER, which
	// is PAINTING order: the first node is the bottom of the stack. An feMerge
	// with no nodes has an empty slice and produces transparent black, which
	// is distinct from a merge of one node.
	MergeInputs []FilterInput

	// HasSubregion reports whether any of X/Y/W/H was specified. A
	// primitive with no subregion of its own defaults to the filter region
	// (for a primitive with no input, like feFlood) or the union of its
	// inputs' subregions — see the renderer.
	HasSubregion           bool
	HasX, HasY, HasW, HasH bool
	X, Y, W, H             float64

	// SubregionInvalid records a specified-but-negative width or height on
	// this primitive. Per SVG this is an ERROR that disables the element's
	// rendering entirely (verified against the resvg invalid-subregion
	// fixture, where the target renders as nothing at all) rather than
	// merely skipping the primitive, so it is carried to the renderer
	// rather than resolved away here.
	SubregionInvalid bool

	// FloodColor is feFlood's flood-color composed with flood-opacity into
	// the alpha channel, as a straight (non-premultiplied) sRGB RGBA —
	// exactly resolveStopColor's shape for a gradient <stop>.
	FloodColor color.RGBA

	// Dx, Dy are feOffset's translation, in the units PrimitiveUnits
	// selects.
	Dx, Dy float64

	// StdDevX, StdDevY are feGaussianBlur's per-axis standard deviations, in
	// the units PrimitiveUnits selects. Zero on an axis means no blur there.
	//
	// A NEGATIVE or otherwise invalid stdDeviation is resolved to zero rather
	// than recorded: per SVG a negative value is an error that disables the
	// PRIMITIVE, and the corpus's negative-stdDeviation fixture confirms the
	// element still renders (unblurred) rather than disappearing. Carrying the
	// negative through to the renderer would invite it to be read as a blur
	// radius.
	StdDevX, StdDevY float64

	// Operator is feComposite's operator.
	Operator CompositeOperator
	// K1..K4 are feComposite's arithmetic coefficients, meaningful only when
	// Operator is CompositeArithmetic.
	K1, K2, K3, K4 float64

	// BlendMode is feBlend's mode as a PDF /BM name (see
	// pkg/svg/filter.BlendModeName), or "" for normal/source-over. Storing the
	// canonical name rather than the SVG spelling keeps the renderer free of
	// the vocabulary mapping.
	BlendMode string

	// Matrix is feColorMatrix's resolved 5x4 matrix, row-major. Every
	// shorthand type (saturate, hueRotate, luminanceToAlpha) is expanded into
	// a matrix HERE, at parse time, so the renderer implements exactly one
	// operation and the shorthand formulas are unit-testable without a
	// renderer.
	Matrix [20]float64

	// Space is the color-interpolation-filters value in effect at THIS
	// primitive. It is per-primitive, not per-filter: the property is an
	// ordinary inherited presentation property, so a primitive may override
	// what the <filter> element sets.
	Space FilterColorSpace
}

// FilterColorSpace is the color-interpolation-filters value, which selects
// the space a primitive's pixel math runs in.
type FilterColorSpace int

const (
	// FilterLinearRGB is the SVG DEFAULT — unlike every other color
	// operation in this engine, which works in sRGB. See
	// pkg/svg/filter.LinearRGB.
	FilterLinearRGB FilterColorSpace = iota
	// FilterSRGB is the color-interpolation-filters:sRGB opt-out.
	FilterSRGB
)

// Filter is a resolved <filter> element: the filter region, the units that
// map it, and the primitive graph, all resolved at parse time so the
// renderer never touches the document index.
//
// A <filter> element's OWN transform attribute has no effect (the filter
// applies in the FILTERED element's user space), matching Mask's identical
// note.
type Filter struct {
	// Units is filterUnits: "objectBoundingBox" (the default) or
	// "userSpaceOnUse", mapping RegionX/Y/W/H into the filtered element's
	// user space.
	Units string

	// PrimitiveUnits is primitiveUnits: "userSpaceOnUse" (the default) or
	// "objectBoundingBox", mapping each primitive's own lengths (a
	// subregion, feOffset's dx/dy). Note this default is the OPPOSITE of
	// Units's, the same trap maskUnits/maskContentUnits carries.
	PrimitiveUnits string

	// RegionX, RegionY, RegionW, RegionH are the filter region rect,
	// defaulting to -10%, -10%, 120%, 120% per SVG. That default is not
	// cosmetic: it is what gives a blur or a drop shadow room to bleed past
	// the source's own bounding box, and a filter region computed without
	// it clips every shadow in the document.
	RegionX, RegionY, RegionW, RegionH float64

	// RegionInvalid records a specified-but-non-positive region width or
	// height. Per SVG the element is then NOT RENDERED AT ALL (verified
	// against the resvg corpus), which is distinct from "render unfiltered".
	RegionInvalid bool

	// Primitives is the resolved graph in document order. Empty means the
	// <filter> declared no primitives, which per SVG makes the filter output
	// transparent black — the element disappears (verified against the
	// resvg no-children fixture). This is NOT the same as an absent filter,
	// so a caller must never treat an empty graph as "no filtering".
	Primitives []FilterPrimitive

	// Unbounded marks a filter that has NO filter region: the CSS
	// filter-function shorthand (`filter="blur(4)"`, `drop-shadow(...)`).
	//
	// This is not a nicety. A <filter> element's region defaults to
	// -10%/120% of the bounding box, but a filter FUNCTION has no <filter>
	// element and therefore no region at all — the corpus states it outright
	// in drop-shadow-function-filter-region's <desc> ("Filter function doesn't
	// have a filter region, like the `filter` element") and shows a
	// stdDeviation=30 shadow spreading across the whole canvas. Applying the
	// element region's default here clips every CSS drop-shadow to a box
	// barely larger than the element, which looks like a too-small blur rather
	// than like a clip and is easy to misdiagnose.
	//
	// The renderer substitutes the visible canvas, which is both unbounded in
	// the sense that matters and still bounded for allocation.
	Unbounded bool

	// Unsupported names the first primitive this engine does not implement,
	// or "" when every primitive in the graph is supported. The renderer
	// paints the element UNFILTERED and logs this name: a visible
	// approximation beats a blank, and naming the primitive tells a user
	// exactly which feature they did not get.
	Unsupported string
}

// unsupportedPrimitives are the <fe*> elements this engine recognizes as real
// filter primitives but does not implement. A filter containing any of them
// degrades to rendering the element unfiltered, with a log naming the
// element — never to a silently empty result, which would make the element
// vanish and give the user nothing to act on.
//
// enable-background is deliberately absent: it is an ATTRIBUTE, not a
// primitive, and it was REMOVED from the SVG spec with no browser ever
// implementing it, so it is dropped outright rather than deferred (the same
// treatment <tref> got). The two BackgroundImage/BackgroundAlpha inputs that
// only exist to feed it resolve like any other unknown input name.
var unsupportedPrimitives = map[string]bool{
	"feTurbulence":        true,
	"feConvolveMatrix":    true,
	"feDiffuseLighting":   true,
	"feSpecularLighting":  true,
	"feMorphology":        true,
	"feImage":             true,
	"feTile":              true,
	"feComponentTransfer": true,
	"feDisplacementMap":   true,
}

// resolveFilterRef resolves a filter property's raw value (as recorded by
// Style.FilterRef) into a *Filter.
//
// ok=false means the reference is present but UNRESOLVABLE (a malformed
// url(), or an id naming no <filter> element). Per SVG that is an error which
// makes the referencing element NOT RENDER AT ALL — verified against the
// resvg invalid-FuncIRI fixture, where the target rect disappears entirely.
// This is the exact OPPOSITE of clip-path/mask, where an unresolvable
// reference degrades to "no restriction", so the two must not be modeled the
// same way: returning (nil, false) here means "drop the element", while
// (nil, true) means "no filter applies".
// el and st are the referencing element and its resolved style, needed only
// for the CSS filter-FUNCTION path (`filter="blur(4)"`), where a length may be
// font-relative and drop-shadow()'s omitted colour comes from the element's
// own `color`. A bare url() reference ignores both.
func (b *sceneBuilder) resolveFilterRef(ref string, el *element, st Style) (*Filter, bool) {
	if isSVGFilterFunctionList(ref) {
		// A CSS filter function list (`blur(4)`, `grayscale() opacity(.5)`,
		// or a mix of functions and url()s) rather than a single reference.
		// It lowers into a synthetic <filter> graph; see
		// resolveFilterFunctions for why an invalid list renders the element
		// UNFILTERED rather than dropping it, unlike an invalid url().
		f, ok := b.resolveFilterFunctions(ref, &cascadeCtx{idx: b.idx, logf: b.logf}, el, st, 0)
		if !ok {
			return nil, true // invalid list: no filter, element still renders
		}
		return f, true
	}
	return b.resolveFilterRefAt(ref, 0)
}

// resolveFilterRefAt resolves a bare url(#id) filter reference with an
// explicit chain depth.
func (b *sceneBuilder) resolveFilterRefAt(ref string, depth int) (*Filter, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(strings.ToLower(ref), "url(") {
		// Not a url() reference and not a recognized function list: per the
		// spec's error handling this makes the element not render, but
		// degrading to "unfiltered" is far friendlier, so this reports "no
		// filter" and logs.
		b.warnOnceMsg("svg-filter-not-funciri", "svg: ignoring filter: only url(#id) references and CSS filter functions are supported")
		return nil, true
	}
	id, _, ok := parsePaintServerRef(ref)
	if !ok {
		b.warnOnceMsg("svg-filter-bad-funciri", "svg: element not rendered: unparseable filter url() reference")
		return nil, false
	}
	fragID, ok := fragmentID(id)
	if !ok {
		return nil, false
	}
	return b.resolveFilter(fragID, depth)
}

// resolveFilter resolves id against the document index into a *Filter,
// memoizing by id and guarding against a self-referencing or cyclic chain via
// buildingFilter (mirrors buildingMask exactly). depth bounds an acyclic
// chain via maxFilterChainDepth.
//
// ok=false (the element does not render) when id names nothing, names a
// non-<filter> element, or a cycle/excessive depth is detected.
func (b *sceneBuilder) resolveFilter(id string, depth int) (*Filter, bool) {
	if f, ok := b.filterMemo[id]; ok {
		return f, f != nil
	}
	if depth >= maxFilterChainDepth || b.buildingFilter[id] {
		return nil, false
	}
	el, ok := b.idx.ids[id]
	if !ok || el.space != svgNS || el.local != "filter" {
		b.warnOnceMsg("svg-filter-unresolved", "svg: element not rendered: filter reference does not resolve to a <filter>")
		return nil, false
	}

	b.buildingFilter[id] = true
	defer delete(b.buildingFilter, id)

	ctx := &cascadeCtx{idx: b.idx, logf: b.logf}
	f := &Filter{
		Units:          filterUnits(el),
		PrimitiveUnits: primitiveUnits(el),
	}
	f.RegionX, f.RegionY, f.RegionW, f.RegionH = filterRegion(el, f.Units, b.vp)
	if f.RegionW <= 0 || f.RegionH <= 0 {
		// Per SVG: "A negative or zero value disables the effect of the
		// given filter element (i.e., the element is not rendered)."
		f.RegionInvalid = true
		b.filterMemo[id] = f
		return f, true
	}

	f.Primitives, f.Unsupported = b.buildFilterPrimitives(el, ctx, f.PrimitiveUnits)
	b.filterMemo[id] = f
	return f, true
}

// buildFilterPrimitives walks a <filter>'s children into the resolved
// primitive graph, wiring `in`/`result` names as it goes.
//
// The result-name table is built INCREMENTALLY (a name becomes visible only
// after the primitive declaring it is appended), which is what enforces the
// spec's "in may only reference an earlier result" rule structurally: a
// forward reference simply is not in the table yet and falls back like any
// other undefined name, so a cycle can never be constructed. Verify this
// invariant holds before adding a primitive that wires inputs differently.
func (b *sceneBuilder) buildFilterPrimitives(el *element, ctx *cascadeCtx, primitiveUnits string) ([]FilterPrimitive, string) {
	var prims []FilterPrimitive
	results := map[string]int{}
	unsupported := ""

	for _, kid := range el.kids {
		if kid.space != svgNS {
			continue
		}
		if unsupportedPrimitives[kid.local] {
			if unsupported == "" {
				unsupported = kid.local
			}
			continue
		}
		if kid.local == "feDropShadow" {
			// feDropShadow is the SVG 2 SHORTHAND for a five-primitive chain,
			// and it is expanded into that chain here rather than implemented
			// as its own primitive. Doing it this way is what makes it
			// inherit every behavior the chain already has — the blur's
			// premultiplication, the offset's fractional resampling, the
			// flood's colour-space conversion, the composite's Porter-Duff —
			// instead of being a second, subtly-different implementation of
			// all four.
			if len(prims)+dropShadowChainLength > maxFilterPrimitives {
				b.warnOnceMsg("svg-filter-primitive-cap",
					"svg: <filter> exceeded the primitive cap; the rest were dropped")
				break
			}
			in := resolveFilterInput(kid.attrs["in"], results, len(prims))
			space := primitiveColorSpace(kid, ctx)
			var sub FilterPrimitive
			b.readPrimitiveSubregion(&sub, kid, primitiveUnits)
			if sub.SubregionInvalid {
				// The subregion error is a property of the SHORTHAND element,
				// so it must reach the renderer on a primitive rather than
				// being lost in the expansion.
				prims = append(prims, FilterPrimitive{Kind: PrimitiveOffset, In: in, SubregionInvalid: true})
				continue
			}
			expanded := expandDropShadow(kid, ctx, in, space, sub, len(prims))
			if name := strings.TrimSpace(kid.attrs["result"]); name != "" {
				// The shorthand's `result` names the LAST primitive of the
				// expansion (the merge), which is the shorthand's own output.
				results[name] = len(prims) + len(expanded) - 1
			}
			prims = append(prims, expanded...)
			continue
		}

		var kind PrimitiveKind
		switch kid.local {
		case "feFlood":
			kind = PrimitiveFlood
		case "feOffset":
			kind = PrimitiveOffset
		case "feGaussianBlur":
			kind = PrimitiveGaussianBlur
		case "feComposite":
			kind = PrimitiveComposite
		case "feBlend":
			kind = PrimitiveBlend
		case "feColorMatrix":
			kind = PrimitiveColorMatrix
		case "feMerge":
			kind = PrimitiveMerge
		default:
			// Not a filter primitive at all (a <desc>, a comment element, or
			// a genuinely unknown name): ignored, per SVG's forgiving
			// unknown-element handling. Not an "unsupported primitive", so
			// it must not trigger the unfiltered degradation.
			continue
		}
		if len(prims) >= maxFilterPrimitives {
			b.warnOnceMsg("svg-filter-primitive-cap",
				"svg: <filter> exceeded the primitive cap; the rest were dropped")
			break
		}

		p := FilterPrimitive{Kind: kind}
		p.In = resolveFilterInput(kid.attrs["in"], results, len(prims))
		p.Space = primitiveColorSpace(kid, ctx)
		b.readPrimitiveSubregion(&p, kid, primitiveUnits)

		switch kind {
		case PrimitiveFlood:
			p.FloodColor = resolveFloodColor(kid, ctx)
		case PrimitiveOffset:
			p.Dx = plainNumberAttr(kid, "dx", 0)
			p.Dy = plainNumberAttr(kid, "dy", 0)
		case PrimitiveGaussianBlur:
			readGaussianBlur(&p, kid)
		case PrimitiveComposite:
			p.In2 = resolveFilterInput(kid.attrs["in2"], results, len(prims))
			readComposite(&p, kid)
		case PrimitiveBlend:
			p.In2 = resolveFilterInput(kid.attrs["in2"], results, len(prims))
			readBlend(&p, kid)
		case PrimitiveColorMatrix:
			readColorMatrix(&p, kid)
		case PrimitiveMerge:
			readMergeNodes(&p, kid, results, len(prims))
		}

		if name := strings.TrimSpace(kid.attrs["result"]); name != "" {
			// Recorded AFTER this primitive's own `in` resolved, so
			// result="x" in="x" refers to the PREVIOUS x, never itself.
			results[name] = len(prims)
		}
		prims = append(prims, p)
	}
	return prims, unsupported
}

// resolveFilterInput resolves one `in` attribute against the results
// declared by EARLIER primitives.
//
// The fallbacks are the spec's, and they differ for the first primitive:
// an absent/unrecognized `in` on the FIRST primitive means SourceGraphic,
// while on any later primitive it means "the previous primitive's output".
// An `in` naming an undefined result takes that same previous-output
// fallback rather than erroring, which is what the corpus's in-to-invalid
// fixtures assert.
func resolveFilterInput(raw string, results map[string]int, index int) FilterInput {
	name := strings.TrimSpace(raw)
	switch name {
	case "SourceGraphic":
		return FilterInput{Kind: InputSourceGraphic}
	case "SourceAlpha":
		return FilterInput{Kind: InputSourceAlpha}
	}
	if name != "" {
		if i, ok := results[name]; ok {
			return FilterInput{Kind: InputResult, Index: i}
		}
		// An undefined name (including BackgroundImage/BackgroundAlpha,
		// whose enable-background backing was removed from the spec) falls
		// through to the positional default below.
	}
	if index == 0 {
		return FilterInput{Kind: InputSourceGraphic}
	}
	return FilterInput{Kind: InputResult, Index: index - 1}
}

// readPrimitiveSubregion reads a primitive's own x/y/width/height, recording
// which were specified (an unspecified one inherits a default the renderer
// computes from the inputs) and flagging a negative width/height as the
// rendering-disabling error SVG defines it to be.
func (b *sceneBuilder) readPrimitiveSubregion(p *FilterPrimitive, el *element, units string) {
	// A PERCENTAGE means different things under the two primitiveUnits
	// values, and the difference is a factor of the viewport:
	//
	//   userSpaceOnUse:     50% of the viewport, an absolute user-unit length
	//                       the caller's matrix then maps as-is.
	//   objectBoundingBox:  the fraction 0.5, which the caller's units matrix
	//                       multiplies by the target's bounding box.
	//
	// Resolving objectBoundingBox percentages against the viewport (as an
	// earlier revision did) makes width="50%" mean 100 BBOX WIDTHS rather than
	// half of one — the subregion then covers everything and clips nothing,
	// which the subregion-and-primitiveUnits=objectBoundingBox-2 fixture shows
	// as a missing clip rather than an obviously wrong number.
	userSpace := units != "objectBoundingBox"
	if v, ok := el.attrs["x"]; ok {
		if n, ok := parseFilterLength(v, b.vp.w, userSpace); ok {
			p.X, p.HasX, p.HasSubregion = n, true, true
		}
	}
	if v, ok := el.attrs["y"]; ok {
		if n, ok := parseFilterLength(v, b.vp.h, userSpace); ok {
			p.Y, p.HasY, p.HasSubregion = n, true, true
		}
	}
	if v, ok := el.attrs["width"]; ok {
		if n, ok := parseFilterLength(v, b.vp.w, userSpace); ok {
			if n < 0 {
				p.SubregionInvalid = true
			}
			p.W, p.HasW, p.HasSubregion = n, true, true
		}
	}
	if v, ok := el.attrs["height"]; ok {
		if n, ok := parseFilterLength(v, b.vp.h, userSpace); ok {
			if n < 0 {
				p.SubregionInvalid = true
			}
			p.H, p.HasH, p.HasSubregion = n, true, true
		}
	}
}

// parseFilterLength parses one filter length: a plain number, or a
// percentage resolved against ref. In objectBoundingBox primitiveUnits a
// bare number is already a bbox fraction, so ref is unused there — the
// caller's units matrix does that mapping.
func parseFilterLength(v string, ref float64, userSpace bool) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if strings.HasSuffix(v, "%") {
		n, ok := parseNumber(strings.TrimSuffix(v, "%"))
		if !ok {
			return 0, false
		}
		if userSpace {
			return n / 100 * ref, true
		}
		return n / 100, true
	}
	return parseLength(v, ref)
}

// plainNumberAttr reads a filter attribute typed <number> in the SVG grammar,
// falling back to def when absent or unparseable.
//
// It deliberately rejects a PERCENTAGE (and any unit suffix): feOffset's
// dx/dy are <number>, not <length>, so "20%" is an invalid value that per
// SVG's error handling falls back to the attribute's default of 0 rather than
// resolving against the viewport. The resvg percentage-values fixture pins
// this — it renders the element UNSHIFTED — and an earlier revision of this
// code that accepted percentages moved it instead.
func plainNumberAttr(el *element, name string, def float64) float64 {
	v, ok := el.attrs[name]
	if !ok {
		return def
	}
	n, ok := parseNumber(strings.TrimSpace(v))
	if !ok {
		return def
	}
	return n
}

// filterUnits resolves filterUnits, defaulting to objectBoundingBox.
// An unrecognized value falls back to the default rather than erroring,
// matching the resvg invalid-filterUnits fixture.
func filterUnits(el *element) string {
	if el.attrs["filterUnits"] == "userSpaceOnUse" {
		return "userSpaceOnUse"
	}
	return "objectBoundingBox"
}

// primitiveUnits resolves primitiveUnits, defaulting to userSpaceOnUse —
// the OPPOSITE default from filterUnits.
func primitiveUnits(el *element) string {
	if el.attrs["primitiveUnits"] == "objectBoundingBox" {
		return "objectBoundingBox"
	}
	return "userSpaceOnUse"
}

// filterRegion resolves a <filter>'s x/y/width/height against SVG's defaults
// of -10%, -10%, 120%, 120%, exactly like maskRegion does for a <mask>.
//
// The -10%/120% default is what gives a filter room to bleed past its
// source's bounding box; resolving it as 0%/100% would clip every drop
// shadow in the document to the shape that casts it.
func filterRegion(el *element, units string, vp viewport) (x, y, w, h float64) {
	userSpace := units == "userSpaceOnUse"
	if userSpace {
		x = gradientCoord(el.attrs, "x", -0.1*vp.w, true, vp.w)
		y = gradientCoord(el.attrs, "y", -0.1*vp.h, true, vp.h)
		w = gradientCoord(el.attrs, "width", 1.2*vp.w, true, vp.w)
		h = gradientCoord(el.attrs, "height", 1.2*vp.h, true, vp.h)
		return x, y, w, h
	}
	x = gradientCoord(el.attrs, "x", -0.1, false, 0)
	y = gradientCoord(el.attrs, "y", -0.1, false, 0)
	w = gradientCoord(el.attrs, "width", 1.2, false, 0)
	h = gradientCoord(el.attrs, "height", 1.2, false, 0)
	return x, y, w, h
}

// primitiveColorSpace resolves color-interpolation-filters at one primitive,
// defaulting to linearRGB per SVG.
//
// The property is INHERITED, so a primitive with no value of its own picks up
// whatever the <filter> element (or an ancestor of it) set — which is how the
// corpus's filter-level color-interpolation-filters="sRGB" reaches every
// primitive inside. It goes through the cascade rather than a raw attribute
// read so a stylesheet rule and a style="" declaration both work.
func primitiveColorSpace(el *element, ctx *cascadeCtx) FilterColorSpace {
	attr := ctx.resolve(el)
	v, ok := attr("color-interpolation-filters")
	if !ok {
		// Not set on the primitive itself: inherit from the <filter>.
		if el.parent != nil {
			return primitiveColorSpace(el.parent, ctx)
		}
		return FilterLinearRGB
	}
	switch strings.TrimSpace(v) {
	case "sRGB":
		return FilterSRGB
	case "inherit":
		if el.parent != nil {
			return primitiveColorSpace(el.parent, ctx)
		}
		return FilterLinearRGB
	default:
		// "linearRGB", "auto" (which SVG defines as linearRGB for filters),
		// and any unrecognized value all resolve to the default.
		return FilterLinearRGB
	}
}

// resolveFloodColor resolves feFlood's flood-color composed with
// flood-opacity into the alpha channel, as a straight sRGB RGBA — the same
// shape (and the same currentColor/percentage handling) resolveStopColor
// produces for a gradient <stop>.
//
// flood-color is NOT an inherited property, so "inherit" resolves against the
// DIRECT parent's own value only, and a value set on a grandparent does not
// reach here. Both halves are asserted by the corpus's flood-color
// inheritance-1..5 fixtures, where green on the <filter> reaches an
// feFlood[flood-color=inherit] but green on a <g> wrapping the <filter> does
// not.
func resolveFloodColor(el *element, ctx *cascadeCtx) color.RGBA {
	attr := ctx.resolve(el)

	cur := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	if v, ok := attr("color"); ok {
		if c, ok := parseColorValue(strings.TrimSpace(v)); ok {
			cur = c
		}
	}

	c := color.RGBA{R: 0, G: 0, B: 0, A: 255} // flood-color initial value: black
	if v, ok := attr("flood-color"); ok {
		v = strings.TrimSpace(v)
		switch v {
		case "currentColor":
			c = cur
		case "inherit":
			c = inheritedFloodColor(el.parent, ctx)
		case "":
		default:
			if parsed, ok := parseColorValue(v); ok {
				c = parsed
			}
		}
	}

	opacity := resolveFloodOpacity(el, ctx)
	// flood-color may itself carry alpha (e.g. hsla(...)), which composes
	// multiplicatively with flood-opacity rather than being replaced by it.
	c.A = uint8(clamp(float64(c.A)*opacity, 0, 255))
	return c
}

// inheritedFloodColor reads flood-color from el (a primitive's parent, i.e.
// normally the <filter> element) for an explicit "inherit", falling back to
// the initial value (black) when the parent does not set it either.
//
// It deliberately does NOT walk further up the tree: flood-color is not an
// inherited property, so an unset parent contributes the INITIAL value, not
// its own parent's — see resolveFloodColor's doc comment for the corpus
// fixtures that pin this distinction.
func inheritedFloodColor(parent *element, ctx *cascadeCtx) color.RGBA {
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	if parent == nil {
		return black
	}
	v, ok := ctx.resolve(parent)("flood-color")
	if !ok {
		return black
	}
	if c, ok := parseColorValue(strings.TrimSpace(v)); ok {
		return c
	}
	return black
}

// resolveFloodOpacity resolves flood-opacity (default 1), accepting both a
// number and a percentage per SVG 2 — the corpus's 50percent fixture uses
// the percentage form.
func resolveFloodOpacity(el *element, ctx *cascadeCtx) float64 {
	v, ok := ctx.resolve(el)("flood-opacity")
	if !ok {
		return 1
	}
	v = strings.TrimSpace(v)
	pct := strings.HasSuffix(v, "%")
	n, ok := parseNumber(strings.TrimSuffix(v, "%"))
	if !ok {
		return 1
	}
	if pct {
		n /= 100
	}
	return clamp(n, 0, 1)
}
