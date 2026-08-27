package svg

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
)

// parseFilterDoc parses src and returns the first node carrying a filter,
// plus every log line emitted, so a test can assert on both the resolved
// graph and the degradation diagnostics.
func parseFilterDoc(t *testing.T, src string) (*Document, []string) {
	t.Helper()
	var logs []string
	doc, err := Parse([]byte(src), func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc, logs
}

// firstFilter walks the scene for the first Shape or Group carrying a
// resolved filter.
func firstFilter(doc *Document) *Filter {
	_, root := doc.Root()
	var found *Filter
	var walk func(n Node)
	walk = func(n Node) {
		if found != nil {
			return
		}
		switch k := n.(type) {
		case *Group:
			if k == nil {
				return
			}
			if k.Filter != nil {
				found = k.Filter
				return
			}
			for _, kid := range k.Kids {
				walk(kid)
			}
		case *Shape:
			if k != nil && k.Filter != nil {
				found = k.Filter
			}
		case *Text:
			if k != nil && k.Filter != nil {
				found = k.Filter
			}
		}
	}
	walk(root)
	return found
}

// countNodes reports how many paintable nodes the scene holds, for the
// "element is not rendered" assertions.
func countNodes(doc *Document) int {
	_, root := doc.Root()
	n := 0
	var walk func(x Node)
	walk = func(x Node) {
		switch k := x.(type) {
		case *Group:
			if k == nil {
				return
			}
			for _, kid := range k.Kids {
				walk(kid)
			}
		case *Shape:
			n++
		case *Text:
			n++
		}
	}
	walk(root)
	return n
}

// TestFilterRegionDefaults pins the -10%/120% default region, the single
// most consequential number in filter region math: resolving it as 0%/100%
// instead would clip every drop shadow in every document to the shape
// casting it.
func TestFilterRegionDefaults(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feFlood/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f == nil {
		t.Fatal("no filter resolved")
	}
	if f.Units != "objectBoundingBox" {
		t.Errorf("Units = %q, want objectBoundingBox (the default)", f.Units)
	}
	if f.PrimitiveUnits != "userSpaceOnUse" {
		t.Errorf("PrimitiveUnits = %q, want userSpaceOnUse (the OPPOSITE default from Units)", f.PrimitiveUnits)
	}
	if f.RegionX != -0.1 || f.RegionY != -0.1 {
		t.Errorf("region origin = (%v,%v), want (-0.1,-0.1)", f.RegionX, f.RegionY)
	}
	if f.RegionW != 1.2 || f.RegionH != 1.2 {
		t.Errorf("region size = (%v,%v), want (1.2,1.2)", f.RegionW, f.RegionH)
	}
}

// TestFilterRegionExplicitObjectBoundingBox confirms explicit percentages and
// fractions both resolve as bbox fractions in the default units.
func TestFilterRegionExplicitObjectBoundingBox(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f" x="0%" y="10%" width="100%" height="50%"><feFlood/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f.RegionX != 0 || f.RegionY != 0.1 || f.RegionW != 1 || f.RegionH != 0.5 {
		t.Errorf("region = (%v,%v,%v,%v), want (0,0.1,1,0.5)", f.RegionX, f.RegionY, f.RegionW, f.RegionH)
	}
}

// TestFilterRegionUserSpaceOnUse confirms userSpaceOnUse resolves to absolute
// user-unit lengths rather than bbox fractions.
func TestFilterRegionUserSpaceOnUse(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f" filterUnits="userSpaceOnUse" x="20" y="30" width="100" height="140"><feFlood/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f.Units != "userSpaceOnUse" {
		t.Fatalf("Units = %q", f.Units)
	}
	if f.RegionX != 20 || f.RegionY != 30 || f.RegionW != 100 || f.RegionH != 140 {
		t.Errorf("region = (%v,%v,%v,%v), want (20,30,100,140)", f.RegionX, f.RegionY, f.RegionW, f.RegionH)
	}
}

// TestFilterRegionUserSpaceDefaultsUseViewport confirms the -10%/120%
// defaults resolve against the VIEWPORT (not the bbox) in userSpaceOnUse.
func TestFilterRegionUserSpaceDefaultsUseViewport(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 100" xmlns="http://www.w3.org/2000/svg">
		<filter id="f" filterUnits="userSpaceOnUse"><feFlood/></filter>
		<rect x="20" y="20" width="60" height="60" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f.RegionX != -20 || f.RegionY != -10 {
		t.Errorf("origin = (%v,%v), want (-20,-10) — -10%% of the 200x100 viewport", f.RegionX, f.RegionY)
	}
	if f.RegionW != 240 || f.RegionH != 120 {
		t.Errorf("size = (%v,%v), want (240,120) — 120%% of the viewport", f.RegionW, f.RegionH)
	}
}

// TestFilterZeroAndNegativeRegionDisablesRendering pins the spec rule the
// resvg corpus confirms visually: a zero or negative filter region means the
// element is NOT RENDERED — not "rendered unfiltered".
func TestFilterZeroAndNegativeRegionDisablesRendering(t *testing.T) {
	for _, tc := range []struct{ name, attrs string }{
		{"zero width", `width="0"`},
		{"zero height", `height="0"`},
		{"negative width", `width="-0.5"`},
		{"negative height", `height="-0.5"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
				<filter id="f" `+tc.attrs+`><feFlood flood-color="red"/></filter>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
			</svg>`)
			f := firstFilter(doc)
			if f == nil {
				t.Fatal("no filter resolved")
			}
			if !f.RegionInvalid {
				t.Errorf("RegionInvalid = false for %s; the element must not render", tc.name)
			}
		})
	}
}

// TestFilterInvalidReferenceDropsElement pins the behavior that separates
// filter from clip-path/mask: an unresolvable reference makes the element
// disappear rather than degrading to "no filtering". Verified against the
// resvg invalid-FuncIRI fixture, whose reference PNG shows no rect at all.
func TestFilterInvalidReferenceDropsElement(t *testing.T) {
	for _, tc := range []struct{ name, ref string }{
		{"missing id", `url(#nope)`},
		{"not a filter", `url(#g1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
				<g id="g1"/>
				<rect x="20" y="20" width="160" height="160" fill="red" filter="`+tc.ref+`"/>
			</svg>`)
			if n := countNodes(doc); n != 0 {
				t.Errorf("%d paintable nodes survived an unresolvable filter reference; want 0", n)
			}
		})
	}
}

// TestFilterNoneRendersNormally confirms filter="none" is not an error: the
// element renders, unfiltered.
func TestFilterNoneRendersNormally(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<rect x="20" y="20" width="160" height="160" fill="green" filter="none"/>
	</svg>`)
	if n := countNodes(doc); n != 1 {
		t.Fatalf("%d nodes, want 1", n)
	}
	if f := firstFilter(doc); f != nil {
		t.Error("filter=\"none\" resolved to a filter; want nil")
	}
}

// TestFilterEmptyGraphIsNotPassThrough pins the resvg no-children fixture: a
// <filter> with no primitives outputs transparent black, so the element
// disappears. Treating an empty graph as "no filtering" — the tempting
// simplification — renders the element instead, which is wrong.
func TestFilterEmptyGraphIsNotPassThrough(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"/>
		<rect x="20" y="20" width="160" height="160" fill="green" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f == nil {
		t.Fatal("an empty <filter> did not resolve; it is valid, just empty")
	}
	if len(f.Primitives) != 0 {
		t.Errorf("%d primitives, want 0", len(f.Primitives))
	}
	if f.RegionInvalid {
		t.Error("an empty filter's region is valid; only its OUTPUT is empty")
	}
}

// TestFilterGraphImplicitSourceGraphic confirms the first primitive with no
// `in` defaults to SourceGraphic.
func TestFilterGraphImplicitSourceGraphic(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feOffset dx="1"/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if got := f.Primitives[0].In.Kind; got != InputSourceGraphic {
		t.Errorf("first primitive input = %v, want InputSourceGraphic", got)
	}
}

// TestFilterGraphResultChaining confirms a named result wires to the
// primitive that declared it, by INDEX, resolved at parse time.
func TestFilterGraphResultChaining(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f">
			<feFlood flood-color="red" result="a"/>
			<feOffset dx="1" result="b"/>
			<feOffset dx="2" in="a"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if len(f.Primitives) != 3 {
		t.Fatalf("%d primitives, want 3", len(f.Primitives))
	}
	third := f.Primitives[2].In
	if third.Kind != InputResult || third.Index != 0 {
		t.Errorf("in=\"a\" resolved to %v index %d, want InputResult index 0", third.Kind, third.Index)
	}
}

// TestFilterGraphUndefinedResultFallsBackToPrevious pins the spec's error
// handling, which the resvg in-to-invalid fixtures assert: an `in` naming a
// result that does not exist uses the PREVIOUS primitive's output, not an
// empty image and not SourceGraphic.
func TestFilterGraphUndefinedResultFallsBackToPrevious(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f">
			<feFlood flood-color="red"/>
			<feOffset dx="1" in="doesNotExist"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	in := f.Primitives[1].In
	if in.Kind != InputResult || in.Index != 0 {
		t.Errorf("an undefined `in` resolved to %v index %d, want the previous primitive (InputResult index 0)", in.Kind, in.Index)
	}
}

// TestFilterGraphForwardReferenceCannotCycle is the structural guarantee the
// design relies on instead of a runtime cycle check: `in` may only name an
// EARLIER result, so naming a LATER one falls back to the positional default
// and no cycle can be built.
func TestFilterGraphForwardReferenceCannotCycle(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f">
			<feFlood flood-color="red" in="later"/>
			<feOffset dx="1" result="later"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if in := f.Primitives[0].In; in.Kind != InputSourceGraphic {
		t.Errorf("a forward reference resolved to %v; want the SourceGraphic default", in.Kind)
	}
	// Every resolved result index must point strictly BACKWARD, which is
	// what makes the renderer's single forward pass sound.
	for i, p := range f.Primitives {
		if p.In.Kind == InputResult && p.In.Index >= i {
			t.Errorf("primitive %d references index %d, which is not strictly earlier", i, p.In.Index)
		}
	}
}

// TestFilterGraphSelfResultReferencesPrevious confirms result="x" in="x" on
// the SAME primitive refers to the earlier x, never to itself — the result
// name is recorded only after its own input resolves.
func TestFilterGraphSelfResultReferencesPrevious(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f">
			<feFlood flood-color="red" result="x"/>
			<feOffset dx="1" in="x" result="x"/>
			<feOffset dx="2" in="x"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if in := f.Primitives[1].In; in.Kind != InputResult || in.Index != 0 {
		t.Errorf("second primitive in=\"x\" -> %v index %d, want index 0", in.Kind, in.Index)
	}
	if in := f.Primitives[2].In; in.Kind != InputResult || in.Index != 1 {
		t.Errorf("third primitive in=\"x\" -> index %d, want the REDEFINED x at index 1", in.Index)
	}
}

// TestFilterGraphSourceAlpha confirms the SourceAlpha implicit input is
// recognized by name.
func TestFilterGraphSourceAlpha(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feOffset dx="1" in="SourceAlpha"/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if in := f.Primitives[0].In; in.Kind != InputSourceAlpha {
		t.Errorf("in=\"SourceAlpha\" resolved to %v", in.Kind)
	}
}

// TestFilterPrimitiveCountIsBounded confirms the parse-time cap holds, so a
// tiny file cannot ask for an unbounded number of full-region pixel buffers.
func TestFilterPrimitiveCountIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg"><filter id="f">`)
	for i := 0; i < maxFilterPrimitives*3; i++ {
		b.WriteString(`<feOffset dx="1"/>`)
	}
	b.WriteString(`</filter><rect x="20" y="20" width="160" height="160" filter="url(#f)"/></svg>`)

	doc, _ := parseFilterDoc(t, b.String())
	f := firstFilter(doc)
	if len(f.Primitives) > maxFilterPrimitives {
		t.Errorf("%d primitives survived, want at most %d", len(f.Primitives), maxFilterPrimitives)
	}
}

// TestUnsupportedPrimitivesDegradeUnfiltered is the deferral-scaffolding
// test the task requires: EVERY primitive this engine does not implement
// must mark the filter Unsupported (which makes the renderer paint the
// element unfiltered and log), never silently produce an empty result.
func TestUnsupportedPrimitivesDegradeUnfiltered(t *testing.T) {
	deferred := []string{
		"feTurbulence", "feConvolveMatrix", "feDiffuseLighting", "feSpecularLighting",
		"feMorphology", "feImage", "feTile", "feComponentTransfer", "feDisplacementMap",
		// Shipping in a later task, recognized now so they degrade by name.
		"feGaussianBlur", "feBlend", "feComposite", "feColorMatrix", "feMerge", "feDropShadow",
	}
	for _, name := range deferred {
		t.Run(name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
				<filter id="f"><`+name+`/></filter>
				<rect x="20" y="20" width="160" height="160" fill="green" filter="url(#f)"/>
			</svg>`)
			f := firstFilter(doc)
			if f == nil {
				t.Fatal("filter did not resolve")
			}
			if f.Unsupported != name {
				t.Errorf("Unsupported = %q, want %q so the renderer can name the feature it dropped", f.Unsupported, name)
			}
			if n := countNodes(doc); n != 1 {
				t.Errorf("%d nodes; the element must still render (unfiltered), not vanish", n)
			}
		})
	}
}

// TestUnknownElementInFilterIsNotUnsupported confirms a non-primitive child
// (a <desc>, or a genuinely unknown element) is ignored per SVG's forgiving
// handling and does NOT trigger the unfiltered degradation — otherwise every
// filter carrying a <title> would silently stop filtering.
func TestUnknownElementInFilterIsNotUnsupported(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f">
			<desc>a description</desc>
			<somethingUnknown/>
			<feFlood flood-color="green"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f.Unsupported != "" {
		t.Errorf("Unsupported = %q; a non-primitive child must not disable filtering", f.Unsupported)
	}
	if len(f.Primitives) != 1 {
		t.Errorf("%d primitives, want just the feFlood", len(f.Primitives))
	}
}

// TestEnableBackgroundIsDropped confirms enable-background is treated as
// removed from the spec (like <tref>): it is not a primitive, so it neither
// resolves nor degrades the filter. The BackgroundImage/BackgroundAlpha
// inputs that only exist to feed it fall back like any unknown name.
func TestEnableBackgroundIsDropped(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg" enable-background="new">
		<filter id="f">
			<feFlood flood-color="green"/>
			<feOffset dx="1" in="BackgroundImage"/>
		</filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	f := firstFilter(doc)
	if f.Unsupported != "" {
		t.Errorf("Unsupported = %q; enable-background is dropped, not deferred", f.Unsupported)
	}
	if in := f.Primitives[1].In; in.Kind != InputResult || in.Index != 0 {
		t.Errorf("in=\"BackgroundImage\" -> %v index %d, want the previous-output fallback", in.Kind, in.Index)
	}
}

// TestFloodColorAndOpacity pins flood-color/flood-opacity resolution,
// including the SVG 2 percentage form and multiplication with a color that
// carries its own alpha.
func TestFloodColorAndOpacity(t *testing.T) {
	cases := []struct {
		name  string
		attrs string
		want  color.RGBA
	}{
		{"default is opaque black", ``, color.RGBA{R: 0, G: 0, B: 0, A: 255}},
		{"named color", `flood-color="seagreen"`, color.RGBA{R: 46, G: 139, B: 87, A: 255}},
		{"opacity number", `flood-color="red" flood-opacity="0.5"`, color.RGBA{R: 255, A: 128}},
		{"opacity percentage", `flood-color="red" flood-opacity="50%"`, color.RGBA{R: 255, A: 128}},
		// hsla's own alpha composes with flood-opacity multiplicatively.
		{"color alpha times opacity", `flood-color="rgba(0,255,0,0.5)" flood-opacity="0.5"`, color.RGBA{G: 255, A: 64}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
				<filter id="f"><feFlood `+c.attrs+`/></filter>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
			</svg>`)
			got := firstFilter(doc).Primitives[0].FloodColor
			if got.R != c.want.R || got.G != c.want.G || got.B != c.want.B {
				t.Errorf("color = %v, want %v", got, c.want)
			}
			if d := int(got.A) - int(c.want.A); d > 1 || d < -1 {
				t.Errorf("alpha = %d, want %d", got.A, c.want.A)
			}
		})
	}
}

// TestFloodColorIsNotInherited pins the resvg flood-color inheritance
// fixtures: the property does not inherit, and an explicit "inherit" reads
// the DIRECT parent only — green on the <filter> reaches the feFlood, green
// on a <g> wrapping the <filter> does not.
func TestFloodColorIsNotInherited(t *testing.T) {
	green := color.RGBA{R: 0, G: 128, B: 0, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	cases := []struct {
		name string
		src  string
		want color.RGBA
	}{
		{
			// inheritance-1: on the <filter>, no explicit inherit -> not inherited.
			name: "filter attribute does not reach the primitive",
			src: `<filter id="f" flood-color="green"><feFlood/></filter>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>`,
			want: black,
		},
		{
			// inheritance-2: on an ancestor <g> -> not inherited.
			name: "ancestor group does not reach the primitive",
			src: `<g flood-color="green"><filter id="f"><feFlood/></filter></g>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>`,
			want: black,
		},
		{
			// inheritance-3: explicit inherit, parent <filter> HAS a value.
			name: "explicit inherit reads the direct parent",
			src: `<filter id="f" flood-color="green"><feFlood flood-color="inherit"/></filter>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>`,
			want: green,
		},
		{
			// inheritance-4: explicit inherit, but the value is on the
			// GRANDparent — the <filter> itself has none, so the initial
			// value wins rather than walking further up.
			name: "explicit inherit does not walk past the direct parent",
			src: `<g flood-color="green"><filter id="f"><feFlood flood-color="inherit"/></filter></g>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>`,
			want: black,
		},
		{
			// inheritance-5: nothing to inherit at all.
			name: "explicit inherit with no parent value",
			src: `<filter id="f"><feFlood flood-color="inherit"/></filter>
				<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>`,
			want: black,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">`+c.src+`</svg>`)
			got := firstFilter(doc).Primitives[0].FloodColor
			if got.R != c.want.R || got.G != c.want.G || got.B != c.want.B {
				t.Errorf("flood color = %v, want %v", got, c.want)
			}
		})
	}
}

// TestColorInterpolationFiltersDefaultAndOptOut pins the property that makes
// filters unlike everything else in this engine: it defaults to linearRGB,
// and only an explicit sRGB opts out. It is INHERITED, so a value on the
// <filter> reaches every primitive inside.
func TestColorInterpolationFiltersDefaultAndOptOut(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want FilterColorSpace
	}{
		{"default is linearRGB", `<filter id="f"><feFlood/></filter>`, FilterLinearRGB},
		{"explicit sRGB on the primitive", `<filter id="f"><feFlood color-interpolation-filters="sRGB"/></filter>`, FilterSRGB},
		{"sRGB on the filter inherits down", `<filter id="f" color-interpolation-filters="sRGB"><feFlood/></filter>`, FilterSRGB},
		{"explicit linearRGB", `<filter id="f"><feFlood color-interpolation-filters="linearRGB"/></filter>`, FilterLinearRGB},
		{"auto resolves to linearRGB", `<filter id="f"><feFlood color-interpolation-filters="auto"/></filter>`, FilterLinearRGB},
		{"primitive overrides the filter", `<filter id="f" color-interpolation-filters="sRGB"><feFlood color-interpolation-filters="linearRGB"/></filter>`, FilterLinearRGB},
		{"a style rule reaches it", `<style>feFlood { color-interpolation-filters: sRGB; }</style><filter id="f"><feFlood/></filter>`, FilterSRGB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">`+c.src+
				`<rect x="20" y="20" width="160" height="160" filter="url(#f)"/></svg>`)
			if got := firstFilter(doc).Primitives[0].Space; got != c.want {
				t.Errorf("space = %v, want %v", got, c.want)
			}
		})
	}
}

// TestPrimitiveSubregionParsing confirms each edge is tracked independently
// (an unspecified edge must keep its computed default) and that a negative
// width or height is flagged as the rendering-disabling error SVG makes it.
func TestPrimitiveSubregionParsing(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feFlood x="10" width="50"/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	p := firstFilter(doc).Primitives[0]
	if !p.HasSubregion || !p.HasX || !p.HasW {
		t.Error("x/width were not recorded as specified")
	}
	if p.HasY || p.HasH {
		t.Error("y/height were recorded as specified though absent; each edge must default independently")
	}
	if p.X != 10 || p.W != 50 {
		t.Errorf("x=%v width=%v, want 10 and 50", p.X, p.W)
	}

	neg, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feFlood width="-50"/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	if !firstFilter(neg).Primitives[0].SubregionInvalid {
		t.Error("a negative subregion width was not flagged; the element must not render")
	}
}

// TestOffsetDxDyRejectsPercentages pins the resvg percentage-values fixture:
// feOffset's dx/dy are <number>, not <length>, so "20%" is invalid and falls
// back to 0 rather than resolving against the viewport.
func TestOffsetDxDyRejectsPercentages(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feOffset dx="20%" dy="40%"/></filter>
		<rect x="20" y="20" width="160" height="160" filter="url(#f)"/>
	</svg>`)
	p := firstFilter(doc).Primitives[0]
	if p.Dx != 0 || p.Dy != 0 {
		t.Errorf("dx=%v dy=%v, want 0 — a percentage is invalid for <number> and falls back to the default", p.Dx, p.Dy)
	}
}

// TestFilterMemoizedAcrossReferences confirms two elements naming the same
// filter share one resolved value, keeping Document allocation-light and
// side-table-free.
func TestFilterMemoizedAcrossReferences(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feFlood flood-color="green"/></filter>
		<rect id="a" x="0" y="0" width="50" height="50" filter="url(#f)"/>
		<rect id="b" x="60" y="0" width="50" height="50" filter="url(#f)"/>
	</svg>`)
	_, root := doc.Root()
	var filters []*Filter
	for _, kid := range root.Kids {
		if s, ok := kid.(*Shape); ok && s.Filter != nil {
			filters = append(filters, s.Filter)
		}
	}
	if len(filters) != 2 {
		t.Fatalf("%d filtered shapes, want 2", len(filters))
	}
	if filters[0] != filters[1] {
		t.Error("the same filter id resolved to two distinct values; memoization is not working")
	}
}

// TestFilterOnTextResolves confirms a filter reaches a <text> node, the seam
// that must use real placed-glyph bounds at render time.
func TestFilterOnTextResolves(t *testing.T) {
	doc, _ := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feOffset dx="5"/></filter>
		<text x="20" y="100" font-size="20" filter="url(#f)">Hi</text>
	</svg>`)
	if f := firstFilter(doc); f == nil {
		t.Fatal("no filter resolved on the <text> element")
	}
}

// TestFilterElementItselfNotRendered confirms a <filter>'s children never
// paint where they appear — the element is referenced, never drawn in place,
// exactly like <mask> and <clipPath>.
func TestFilterElementItselfNotRendered(t *testing.T) {
	doc, logs := parseFilterDoc(t, `<svg viewBox="0 0 200 200" xmlns="http://www.w3.org/2000/svg">
		<filter id="f"><feFlood flood-color="red"/></filter>
	</svg>`)
	if n := countNodes(doc); n != 0 {
		t.Errorf("%d nodes painted for a bare <filter>; want 0", n)
	}
	for _, l := range logs {
		if strings.Contains(l, "not yet supported") {
			t.Errorf("a <filter> element logged %q; filters are implemented and it belongs in skippedElements", l)
		}
	}
}
