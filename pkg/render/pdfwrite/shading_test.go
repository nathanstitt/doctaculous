package pdfwrite

import (
	"bytes"
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/render"
	"github.com/nathanstitt/omnidoc/pkg/render/raster"
)

// opaqueStops is a 2-stop opaque red->blue ramp, the simplest describable,
// emittable shading.
func opaqueStops() []render.ShadingStop {
	return []render.ShadingStop{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 1, Color: color.RGBA{B: 255, A: 255}},
	}
}

// TestCanEmitShadingAcceptsOpaquePad proves the common case (2+ opaque stops,
// SpreadPad) is accepted.
func TestCanEmitShadingAcceptsOpaquePad(t *testing.T) {
	desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: opaqueStops(), Spread: render.SpreadPad}
	if _, ok := canEmitShading(desc); !ok {
		t.Fatal("opaque pad-spread 2-stop gradient should be emittable")
	}
}

// TestCanEmitShadingAllowsAlpha proves a stop with alpha < 255 no longer
// forces a fallback: with luminosity soft masks available (group.go/
// softmask.go), the color ramp still emits as a native /Shading dictionary;
// FillShading pairs it with a parallel /DeviceGray alpha shading under an
// /SMask instead of rasterizing (see TestFillShadingAlphaEmitsShadingUnderSoftMask).
// needsAlphaMask, not canEmitShading, is what decides whether the alpha-mask
// wrapping happens.
func TestCanEmitShadingAllowsAlpha(t *testing.T) {
	stops := opaqueStops()
	stops[0].Color.A = 128
	desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: stops, Spread: render.SpreadPad}
	if _, ok := canEmitShading(desc); !ok {
		t.Fatal("a stop with alpha < 255 should still be emittable as a native shading (paired with a soft mask)")
	}
	if !needsAlphaMask(desc) {
		t.Error("needsAlphaMask should report true for a stop with alpha < 255")
	}
}

// TestNeedsAlphaMaskFalseWhenOpaque proves needsAlphaMask reports false for
// an all-opaque stop list, so an ordinary opaque gradient's /Shading emits
// with no soft-mask wrapping at all (byte-identical to before this alpha
// support existed).
func TestNeedsAlphaMaskFalseWhenOpaque(t *testing.T) {
	desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: opaqueStops(), Spread: render.SpreadPad}
	if needsAlphaMask(desc) {
		t.Error("needsAlphaMask should report false when every stop is opaque")
	}
}

// TestCanEmitShadingRejectsNonPadSpread proves reflect/repeat spreads (no
// native PDF /Extend equivalent) force a fallback.
func TestCanEmitShadingRejectsNonPadSpread(t *testing.T) {
	for _, spread := range []render.SpreadMode{render.SpreadReflect, render.SpreadRepeat} {
		desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: opaqueStops(), Spread: spread}
		if _, ok := canEmitShading(desc); ok {
			t.Fatalf("spread %v must not be emittable as a native shading", spread)
		}
	}
}

// TestCanEmitShadingRejectsFewerThanTwoStops proves a description with 0 or 1
// stops (e.g. NewAxialShader called with a nil/empty stop list, as the
// existing rasterization tests in device_test.go do) is not emittable — there
// is no ramp to build a /Function from.
func TestCanEmitShadingRejectsFewerThanTwoStops(t *testing.T) {
	for _, stops := range [][]render.ShadingStop{nil, {{Offset: 0, Color: color.RGBA{A: 255}}}} {
		desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: stops, Spread: render.SpreadPad}
		if _, ok := canEmitShading(desc); ok {
			t.Fatalf("%d stop(s) must not be emittable", len(stops))
		}
	}
}

// TestBuildShadingDictAxial proves an axial description becomes a
// /ShadingType 2 dict with /Coords holding exactly x0,y0,x1,y1 (radial's
// trailing two coords must not leak in).
func TestBuildShadingDictAxial(t *testing.T) {
	desc := render.ShadingDesc{
		Kind:   render.ShadingAxial,
		Coords: [6]float64{1, 2, 3, 4, 99, 99},
		Stops:  opaqueStops(),
		Spread: render.SpreadPad,
	}
	dict := buildShadingDict(desc)
	if dict["ShadingType"] != Int(2) {
		t.Errorf("ShadingType = %v, want 2", dict["ShadingType"])
	}
	coords, ok := dict["Coords"].(Array)
	if !ok || len(coords) != 4 {
		t.Fatalf("Coords = %v, want a 4-element array", dict["Coords"])
	}
	want := Array{Real(1), Real(2), Real(3), Real(4)}
	for i := range want {
		if coords[i] != want[i] {
			t.Errorf("Coords[%d] = %v, want %v", i, coords[i], want[i])
		}
	}
	if dict["ColorSpace"] != Name("DeviceRGB") {
		t.Errorf("ColorSpace = %v, want DeviceRGB", dict["ColorSpace"])
	}
	if extend, ok := dict["Extend"].(Array); !ok || len(extend) != 2 || extend[0] != Bool(true) || extend[1] != Bool(true) {
		t.Errorf("Extend = %v, want [true true]", dict["Extend"])
	}
}

// TestBuildShadingDictRadial proves a radial description becomes a
// /ShadingType 3 dict with all six /Coords (fx,fy,fr,cx,cy,cr).
func TestBuildShadingDictRadial(t *testing.T) {
	desc := render.ShadingDesc{
		Kind:   render.ShadingRadial,
		Coords: [6]float64{1, 2, 3, 4, 5, 6},
		Stops:  opaqueStops(),
		Spread: render.SpreadPad,
	}
	dict := buildShadingDict(desc)
	if dict["ShadingType"] != Int(3) {
		t.Errorf("ShadingType = %v, want 3", dict["ShadingType"])
	}
	coords, ok := dict["Coords"].(Array)
	if !ok || len(coords) != 6 {
		t.Fatalf("Coords = %v, want a 6-element array", dict["Coords"])
	}
	for i, want := range []Real{1, 2, 3, 4, 5, 6} {
		if coords[i] != want {
			t.Errorf("Coords[%d] = %v, want %v", i, coords[i], want)
		}
	}
}

// TestBuildRampFunctionTwoStops proves exactly two stops collapse to a single
// FunctionType 2 (exponential, N=1) rather than a stitching function.
func TestBuildRampFunctionTwoStops(t *testing.T) {
	fn := buildRampFunction(opaqueStops())
	if fn["FunctionType"] != Int(2) {
		t.Fatalf("FunctionType = %v, want 2 for a 2-stop ramp", fn["FunctionType"])
	}
	if fn["N"] != Int(1) {
		t.Errorf("N = %v, want 1 (linear)", fn["N"])
	}
	c0, _ := fn["C0"].(Array)
	c1, _ := fn["C1"].(Array)
	if len(c0) != 3 || c0[0] != Real(1) || c0[1] != Real(0) || c0[2] != Real(0) {
		t.Errorf("C0 = %v, want [1 0 0] (red)", c0)
	}
	if len(c1) != 3 || c1[0] != Real(0) || c1[1] != Real(0) || c1[2] != Real(1) {
		t.Errorf("C1 = %v, want [0 0 1] (blue)", c1)
	}
}

// TestBuildRampFunctionThreeStops proves more than two stops become a
// FunctionType 3 stitching function: /Bounds carries the interior offset(s),
// /Encode is [0 1 0 1 ...], and /Functions holds one Type 2 per segment.
func TestBuildRampFunctionThreeStops(t *testing.T) {
	stops := []render.ShadingStop{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 0.5, Color: color.RGBA{G: 255, A: 255}},
		{Offset: 1, Color: color.RGBA{B: 255, A: 255}},
	}
	fn := buildRampFunction(stops)
	if fn["FunctionType"] != Int(3) {
		t.Fatalf("FunctionType = %v, want 3 for a 3-stop ramp", fn["FunctionType"])
	}
	bounds, ok := fn["Bounds"].(Array)
	if !ok || len(bounds) != 1 || bounds[0] != Real(0.5) {
		t.Errorf("Bounds = %v, want [0.5]", fn["Bounds"])
	}
	encode, ok := fn["Encode"].(Array)
	if !ok || len(encode) != 4 {
		t.Fatalf("Encode = %v, want 4 values", fn["Encode"])
	}
	want := Array{Real(0), Real(1), Real(0), Real(1)}
	for i := range want {
		if encode[i] != want[i] {
			t.Errorf("Encode[%d] = %v, want %v", i, encode[i], want[i])
		}
	}
	functions, ok := fn["Functions"].(Array)
	if !ok || len(functions) != 2 {
		t.Fatalf("Functions = %v, want 2 subfunctions", fn["Functions"])
	}
	for i, sub := range functions {
		d, ok := sub.(Dict)
		if !ok || d["FunctionType"] != Int(2) {
			t.Errorf("Functions[%d] = %v, want a FunctionType 2 dict", i, sub)
		}
	}
}

// TestBuildRampFunctionCoincidentOffsets proves two stops at (or nearly at)
// the same offset — a hard color break, e.g. a sharp two-tone split — do not
// produce a zero-width /Bounds interval, which PDF's Type 3 stitching function
// requires be strictly increasing (a reader's behavior on a degenerate bound
// is undefined). The nudge must still preserve strictly-increasing /Bounds
// across more than one coincident pair. This checks the in-memory Real
// values; TestBoundsSurviveSerialization is the byte-level counterpart that
// actually caught the round-1 regression (a nudge too small to survive
// formatReal's 4-decimal rounding).
func TestBuildRampFunctionCoincidentOffsets(t *testing.T) {
	stops := []render.ShadingStop{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 0.5, Color: color.RGBA{R: 255, A: 255}}, // hard break: same color up to 0.5
		{Offset: 0.5, Color: color.RGBA{B: 255, A: 255}}, // ...then blue starts here
		{Offset: 1, Color: color.RGBA{B: 255, A: 255}},
	}
	fn := buildRampFunction(stops)
	bounds, ok := fn["Bounds"].(Array)
	if !ok || len(bounds) != 2 {
		t.Fatalf("Bounds = %v, want 2 values for a 4-stop ramp", fn["Bounds"])
	}
	b0, _ := bounds[0].(Real)
	b1, _ := bounds[1].(Real)
	if !(float64(b0) < float64(b1)) {
		t.Errorf("Bounds = [%v %v], want strictly increasing (coincident offsets must be nudged apart)", b0, b1)
	}
	if float64(b0) < 0.5 || float64(b0) > 0.5+2e-3 {
		t.Errorf("Bounds[0] = %v, want ~0.5 (the hard break, only nudged an imperceptible amount)", b0)
	}
}

// dictBoundsBytes serializes dict (a /FunctionType 3 dict, as buildRampFunction
// returns for >2 stops) through the SAME writeTo/formatReal code path
// production uses (writer.put1 -> serialize), then extracts and parses the
// /Bounds array back out of the raw PDF bytes with a regex — proving what a
// real reader would actually see, not what the in-memory Real values were
// before formatReal's 4-decimal rounding. This is the check the round-1
// reviewer specifically asked for: the 1e-6 nudge regression was invisible to
// any test that only inspected pre-serialization floats.
var boundsRe = regexp.MustCompile(`/Bounds\s*\[([^\]]*)\]`)

func dictBoundsBytes(t *testing.T, dict Dict) []float64 {
	t.Helper()
	var buf bytes.Buffer
	dict.writeTo(&buf)
	raw := buf.String()
	m := boundsRe.FindStringSubmatch(raw)
	if m == nil {
		t.Fatalf("no /Bounds array found in emitted dict:\n%s", raw)
	}
	fields := strings.Fields(m[1])
	out := make([]float64, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("/Bounds entry %q did not parse as a number: %v", f, err)
		}
		out[i] = v
	}
	return out
}

// assertBoundsWellFormed asserts, on values parsed back out of emitted PDF
// bytes, that /Bounds is strictly increasing and every entry lies strictly
// inside /Domain [0 1] (ISO 32000-1 §7.10.4: Domain₀ < Bounds₀ < … <
// Bounds_{n-2} < Domain₁). A reader (Acrobat, Ghostscript) need not tolerate a
// violation even though this project's own lenient parser does.
func assertBoundsWellFormed(t *testing.T, bounds []float64) {
	t.Helper()
	for i, b := range bounds {
		if b <= 0 || b >= 1 {
			t.Errorf("Bounds[%d] = %v, want strictly inside (0,1)", i, b)
		}
		if i > 0 && !(b > bounds[i-1]) {
			t.Errorf("Bounds[%d] = %v <= Bounds[%d] = %v, want strictly increasing", i, b, i-1, bounds[i-1])
		}
	}
}

// TestBoundsSurviveSerialization re-runs the round-1 reviewer's exact repro
// (validated independently against Poppler, which rejects a non-strictly-
// increasing /Bounds where this project's own lenient parser would not) on
// the bytes buildRampFunction's dict actually serializes to, for all four
// reported cases: a normal 3-stop ramp, a hard break (two stops at the same
// offset), three stops at the same offset, and a cluster sitting AT the top
// domain endpoint (1.0) — the case that additionally requires the upper-bound
// clamp (Fix B), since naive forward-only nudging pushes it to or past 1.
func TestBoundsSurviveSerialization(t *testing.T) {
	rgba := func(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }
	cases := map[string][]render.ShadingStop{
		"normal 3-stop": {
			{Offset: 0, Color: rgba(255, 0, 0)},
			{Offset: 0.5, Color: rgba(0, 255, 0)},
			{Offset: 1, Color: rgba(0, 0, 255)},
		},
		"hard break (two at 0.5)": {
			{Offset: 0, Color: rgba(255, 0, 0)},
			{Offset: 0.5, Color: rgba(255, 0, 0)},
			{Offset: 0.5, Color: rgba(0, 0, 255)},
			{Offset: 1, Color: rgba(0, 0, 255)},
		},
		"three at 0.5": {
			{Offset: 0, Color: rgba(255, 0, 0)},
			{Offset: 0.5, Color: rgba(255, 0, 0)},
			{Offset: 0.5, Color: rgba(0, 255, 0)},
			{Offset: 0.5, Color: rgba(0, 0, 255)},
			{Offset: 1, Color: rgba(0, 0, 255)},
		},
		"cluster at 1.0": {
			{Offset: 0, Color: rgba(255, 0, 0)},
			{Offset: 1, Color: rgba(0, 255, 0)},
			{Offset: 1, Color: rgba(0, 0, 255)},
			{Offset: 1, Color: rgba(255, 255, 0)},
		},
	}
	for name, stops := range cases {
		t.Run(name, func(t *testing.T) {
			fn := buildRampFunction(stops)
			bounds := dictBoundsBytes(t, fn)
			t.Logf("%s: /Bounds %v", name, bounds)
			assertBoundsWellFormed(t, bounds)
		})
	}
}

// TestSpreadOffsetsClampsToDomainBoundary is TestBoundsSurviveSerialization's
// unit-level counterpart, directly on spreadOffsets: a cluster at exactly 1.0,
// a cluster at exactly 0.0, and a long run of coincident stops (long enough
// that naive upward-only spreading would overflow past 1) must all still
// yield strictly increasing offsets with every interior (would-be /Bounds)
// value strictly inside (0,1).
func TestSpreadOffsetsClampsToDomainBoundary(t *testing.T) {
	mk := func(offsets ...float64) []render.ShadingStop {
		stops := make([]render.ShadingStop, len(offsets))
		for i, o := range offsets {
			stops[i] = render.ShadingStop{Offset: o, Color: color.RGBA{A: 255}}
		}
		return stops
	}
	cases := map[string][]render.ShadingStop{
		"cluster at 1.0":               mk(0, 1, 1, 1),
		"cluster at 0.0":               mk(0, 0, 0, 1),
		"long run at 1.0 (50 stops)":   append(mk(0), mkOnes(49)...),
		"long run at 0.0 (50 stops)":   append(mkZeros(49), render.ShadingStop{Offset: 1, Color: color.RGBA{A: 255}}),
		"2000 coincident stops at 0.5": mkAllSame(2000, 0.5),
	}
	for name, stops := range cases {
		t.Run(name, func(t *testing.T) {
			out := spreadOffsets(stops)
			n := len(out)
			for i := 1; i < n; i++ {
				if !(out[i] > out[i-1]) {
					t.Fatalf("offsets[%d]=%v <= offsets[%d]=%v, want strictly increasing", i, out[i], i-1, out[i-1])
				}
			}
			// Only interior offsets (index 1..n-2) become /Bounds entries and
			// must be strictly inside (0,1); the first/last offsets are the
			// ramp's own endpoints and may legitimately sit at 0 or 1.
			for i := 1; i < n-1; i++ {
				if out[i] <= 0 || out[i] >= 1 {
					t.Errorf("interior offsets[%d] = %v, want strictly inside (0,1)", i, out[i])
				}
			}
		})
	}
}

func mkOnes(n int) []render.ShadingStop {
	out := make([]render.ShadingStop, n)
	for i := range out {
		out[i] = render.ShadingStop{Offset: 1, Color: color.RGBA{A: 255}}
	}
	return out
}

func mkZeros(n int) []render.ShadingStop {
	out := make([]render.ShadingStop, n)
	for i := range out {
		out[i] = render.ShadingStop{Offset: 0, Color: color.RGBA{A: 255}}
	}
	return out
}

func mkAllSame(n int, offset float64) []render.ShadingStop {
	out := make([]render.ShadingStop, n)
	for i := range out {
		out[i] = render.ShadingStop{Offset: offset, Color: color.RGBA{A: 255}}
	}
	return out
}

// TestFillShadingEmitsNativeShadingForOpaqueAxial is the device-level
// end-to-end proof: an opaque, pad-spread axial shader built via
// raster.NewAxialShader (WITH a real stop list, unlike device_test.go's
// nil-stops fixtures) must produce an `sh` operator and a registered
// /Shading resource, and must NOT record an image XObject.
func TestFillShadingEmitsNativeShadingForOpaqueAxial(t *testing.T) {
	dev := newPageDevice(100, 50)

	clip := &render.Path{}
	clip.MoveTo(0, 0)
	clip.LineTo(100, 0)
	clip.LineTo(100, 50)
	clip.LineTo(0, 50)
	clip.Close()
	dev.Save()
	dev.PushClip(clip, render.NonZero)

	stops := opaqueStops()
	shader := raster.NewAxialShader(0, 0, 100, 0, rampFunc{}, stops, raster.SpreadPad)
	dev.FillShading(shader, render.Matrix{A: 1, D: 1}, "")
	dev.Restore()

	if len(dev.images) != 0 {
		t.Fatalf("native shading path recorded %d image(s), want 0", len(dev.images))
	}
	if len(dev.shadings) != 1 {
		t.Fatalf("shadings recorded = %d, want 1", len(dev.shadings))
	}
	if dev.shadings[0].dict["ShadingType"] != Int(2) {
		t.Errorf("ShadingType = %v, want 2 (axial)", dev.shadings[0].dict["ShadingType"])
	}
	content := decompress(t, dev.contentStream())
	wantOp := []byte("/" + dev.shadings[0].name + " sh\n")
	if !bytes.Contains(content, wantOp) {
		t.Errorf("content stream missing %q:\n%s", wantOp, content)
	}
}

// TestFillShadingAlphaEmitsShadingUnderSoftMask proves a shader whose
// description carries a transparent stop now emits VECTOR content: a native
// color /Shading (DeviceRGB), no image XObject, plus a Form XObject wrapping
// a parallel /DeviceGray alpha shading referenced by an /SMask on the
// ExtGState that scopes the "sh" operator — the alpha-gradient fallback lift
// (SVG groups/clip/mask design doc, decision 4). No fallback log fires: this
// is the vector path, not a degradation.
func TestFillShadingAlphaEmitsShadingUnderSoftMask(t *testing.T) {
	dev := newPageDevice(100, 50)
	var logs []string
	dev.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	clip := &render.Path{}
	clip.MoveTo(0, 0)
	clip.LineTo(100, 0)
	clip.LineTo(100, 50)
	clip.LineTo(0, 50)
	clip.Close()
	dev.Save()
	dev.PushClip(clip, render.NonZero)

	stops := opaqueStops()
	stops[0].Color.A = 100 // transparent stop: needs a soft mask, not a raster fallback
	shader := raster.NewAxialShader(0, 0, 100, 0, rampFunc{}, stops, raster.SpreadPad)
	dev.FillShading(shader, render.Matrix{A: 1, D: 1}, "")
	dev.Restore()

	if len(dev.images) != 0 {
		t.Errorf("alpha shading recorded %d image(s), want 0 (vector path, no raster fallback)", len(dev.images))
	}
	if len(dev.shadings) != 1 {
		t.Fatalf("shadings recorded = %d, want 1 (the color shading)", len(dev.shadings))
	}
	if dev.shadings[0].dict["ColorSpace"] != Name("DeviceRGB") {
		t.Errorf("color shading /ColorSpace = %v, want DeviceRGB", dev.shadings[0].dict["ColorSpace"])
	}
	if len(dev.forms) != 1 {
		t.Fatalf("forms recorded = %d, want 1 (the alpha-mask form)", len(dev.forms))
	}
	maskForm := dev.forms[0]
	if maskForm.colorSpace != "DeviceGray" {
		t.Errorf("mask form colorSpace = %q, want DeviceGray", maskForm.colorSpace)
	}
	if len(maskForm.shadings) != 1 || maskForm.shadings[0].dict["ColorSpace"] != Name("DeviceGray") {
		t.Fatalf("mask form shadings = %+v, want one DeviceGray shading", maskForm.shadings)
	}
	if len(dev.extGStates) != 1 || dev.extGStates[0].state.smaskFormName != maskForm.name {
		t.Fatalf("extGState smaskFormName = %+v, want it to reference %q", dev.extGStates, maskForm.name)
	}
	if len(logs) != 0 {
		t.Errorf("logs = %v, want none (this is the vector path, not a fallback)", logs)
	}
}
