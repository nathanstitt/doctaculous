package svg

import (
	"image/color"
	"testing"
)

// stopEl builds a <stop> *element directly (not parsed from XML) with the
// given attrs, for tests that don't need a full document/cascade.
func stopEl(attrs map[string]string) *element {
	return &element{space: svgNS, local: "stop", attrs: attrs}
}

// evalRGB is a small test helper: it calls fn.Eval at t and returns the 4
// outputs (straight RGBA) rounded to uint8 channels, for terse assertions
// against expected colors. Every stopRamp is 4-output (R,G,B,A); alpha
// carries stop-opacity through to the caller instead of being folded into
// RGB, so wantColor below checks A whenever the expectation supplies one
// other than the implicit fully-opaque default.
func evalRGB(t *testing.T, fn interface {
	Eval(in []float64) []float64
	NumOutputs() int
}, at float64) color.RGBA {
	t.Helper()
	if fn.NumOutputs() != 4 {
		t.Fatalf("NumOutputs() = %d, want 4", fn.NumOutputs())
	}
	out := fn.Eval([]float64{at})
	if len(out) != 4 {
		t.Fatalf("Eval(%v) returned %d outputs, want 4", at, len(out))
	}
	toByte := func(v float64) uint8 {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return uint8(v*255 + 0.5)
	}
	return color.RGBA{R: toByte(out[0]), G: toByte(out[1]), B: toByte(out[2]), A: toByte(out[3])}
}

func wantColor(t *testing.T, got color.RGBA, want color.RGBA) {
	t.Helper()
	near := func(a, b uint8) bool {
		d := int(a) - int(b)
		if d < 0 {
			d = -d
		}
		return d <= 1
	}
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseStopsNormalRamp(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0", "stop-color": "red"}),
		stopEl(map[string]string{"offset": "1", "stop-color": "blue"}),
	}}

	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false for a 2-stop gradient")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{255, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 0.25), color.RGBA{191, 0, 64, 255})
	wantColor(t, evalRGB(t, ramp, 0.5), color.RGBA{128, 0, 128, 255})
	wantColor(t, evalRGB(t, ramp, 0.75), color.RGBA{64, 0, 191, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 255, 255})
}

func TestParseStopsPercentOffset(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0%", "stop-color": "black"}),
		stopEl(map[string]string{"offset": "50%", "stop-color": "red"}),
		stopEl(map[string]string{"offset": "100%", "stop-color": "white"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 0.5), color.RGBA{255, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{255, 255, 255, 255})
}

func TestParseStopsMissingAndInvalidOffsetDefaultsZero(t *testing.T) {
	// A stop with no offset attribute, and a stop with an unparseable one,
	// both default to offset 0.
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"stop-color": "lime"}),                     // missing offset
		stopEl(map[string]string{"offset": "banana", "stop-color": "lime"}), // unparseable
		stopEl(map[string]string{"offset": "1", "stop-color": "blue"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	// Both leading stops sit at offset 0; the ramp from 0 to 1 goes lime->blue.
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 255, 0, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 255, 255})
}

func TestParseStopsOffsetClampsToUnitRange(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "-5", "stop-color": "red"}),
		stopEl(map[string]string{"offset": "5", "stop-color": "blue"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	// Both offsets clamp into [0,1]: -5 -> 0, 5 -> 1. Ramp is red->blue over [0,1].
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{255, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 255, 255})
}

func TestParseStopsOutOfOrderOffsetClampsForward(t *testing.T) {
	// Second stop's offset (0.2) is less than the first's (0.5); per spec it
	// takes the previous stop's offset (0.5), NOT a sort. So the effective
	// ramp is: red@0.5, green@0.5, blue@1 -- meaning red is only seen at
	// exactly t=0 through t=0.5 (held, since two stops coincide at 0.5), then
	// a green->blue ramp from 0.5 to 1.
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0.5", "stop-color": "red"}),
		stopEl(map[string]string{"offset": "0.2", "stop-color": "lime"}),
		stopEl(map[string]string{"offset": "1", "stop-color": "blue"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	// Before 0.5: held at the first stop's color (red).
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{255, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 0.4), color.RGBA{255, 0, 0, 255})
	// At/after 0.5, the coincident red/lime pair means lime wins immediately,
	// then the ramp runs lime -> blue across [0.5, 1].
	wantColor(t, evalRGB(t, ramp, 0.5), color.RGBA{0, 255, 0, 255})
	wantColor(t, evalRGB(t, ramp, 0.75), color.RGBA{0, 128, 128, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 255, 255})
}

func TestParseStopsZeroStopsMeansNone(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient"}
	ramp, ok := parseStops(parent, nil)
	if ok {
		t.Fatalf("parseStops reported ok=true for zero stops, got %+v", ramp)
	}
	if ramp != nil {
		t.Fatalf("parseStops returned non-nil ramp for zero stops: %+v", ramp)
	}
}

func TestParseStopsOneStopIsSolid(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0.5", "stop-color": "lime"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false for a single stop")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 255, 0, 255})
	wantColor(t, evalRGB(t, ramp, 0.5), color.RGBA{0, 255, 0, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 255, 0, 255})
}

func TestParseStopsDefaultsBlackOpaque(t *testing.T) {
	// No stop-color or stop-opacity at all: default black, opacity 1.
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0"}),
		stopEl(map[string]string{"offset": "1"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 0, 0, 255})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 0, 255})
}

func TestParseStopsCurrentColor(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0", "stop-color": "currentColor", "color": "lime"}),
		stopEl(map[string]string{"offset": "1", "stop-color": "blue"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 255, 0, 255})
}

func TestParseStopsNonSVGOrNonStopChildrenIgnored(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		{space: "http://example.com/foreign", local: "stop", attrs: map[string]string{"offset": "0", "stop-color": "red"}},
		{space: svgNS, local: "desc", attrs: map[string]string{"offset": "0", "stop-color": "red"}},
		stopEl(map[string]string{"offset": "0", "stop-color": "lime"}),
		stopEl(map[string]string{"offset": "1", "stop-color": "blue"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	// The foreign-namespace and non-<stop> children must not count as stops.
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 255, 0, 255})
}

func TestParseStopsNilElementAndNilAttrs(t *testing.T) {
	if ramp, ok := parseStops(nil, nil); ok || ramp != nil {
		t.Fatalf("parseStops(nil) = %+v, %v; want nil, false", ramp, ok)
	}
	// A <stop> with an entirely nil attrs map must not panic.
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		{space: svgNS, local: "stop"},
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false for a single attributeless stop")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 0, 0, 255})
}

// TestParseStopsOpacity verifies stop-opacity affects the ramp's output as
// real (straight) alpha, not a darkened RGB: a half-opacity white stop
// yields white with A≈128, not mid-gray at A=255.
func TestParseStopsOpacity(t *testing.T) {
	parent := &element{space: svgNS, local: "linearGradient", kids: []*element{
		stopEl(map[string]string{"offset": "0", "stop-color": "white", "stop-opacity": "0.5"}),
		stopEl(map[string]string{"offset": "1", "stop-color": "white", "stop-opacity": "1"}),
	}}
	ramp, ok := parseStops(parent, nil)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{255, 255, 255, 128})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{255, 255, 255, 255})
}

// TestParseStopsThroughCascade verifies stop-color/stop-opacity come through
// the CSS cascade (a <style> rule), not just raw presentation attributes.
func TestParseStopsThroughCascade(t *testing.T) {
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + ns + `>
		<style>.s1{stop-color:lime;stop-opacity:0.5} .s2{stop-color:blue}</style>
		<linearGradient id="g">
			<stop class="s1" offset="0"/>
			<stop class="s2" offset="1"/>
		</linearGradient>
	</svg>`
	root, err := parseXML([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	idx := buildIndex(root, func(string, string) {})
	ctx := &cascadeCtx{idx: idx}

	grad, ok := idx.ids["g"]
	if !ok {
		t.Fatal("gradient element not indexed")
	}
	ramp, ok := parseStops(grad, ctx)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	// lime at 50% opacity: straight RGB unchanged, alpha carries the opacity.
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 255, 0, 128})
	wantColor(t, evalRGB(t, ramp, 1), color.RGBA{0, 0, 255, 255})
}

// TestParseStopsCascadeBeatsPresentationAttr verifies a stylesheet rule
// overrides a stop-color presentation attribute on the same element, per
// normal cascade ordering (sheet rules outrank presentation hints).
func TestParseStopsCascadeBeatsPresentationAttr(t *testing.T) {
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + ns + `>
		<style>stop{stop-color:blue}</style>
		<linearGradient id="g">
			<stop offset="0" stop-color="red"/>
			<stop offset="1" stop-color="red"/>
		</linearGradient>
	</svg>`
	root, err := parseXML([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	idx := buildIndex(root, func(string, string) {})
	ctx := &cascadeCtx{idx: idx}
	grad := idx.ids["g"]

	ramp, ok := parseStops(grad, ctx)
	if !ok {
		t.Fatal("parseStops reported ok=false")
	}
	wantColor(t, evalRGB(t, ramp, 0), color.RGBA{0, 0, 255, 255})
}
