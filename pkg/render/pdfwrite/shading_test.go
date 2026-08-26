package pdfwrite

import (
	"bytes"
	"fmt"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
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

// TestCanEmitShadingRejectsAlpha proves any stop with alpha < 255 forces a
// fallback: PDF /Shading has no alpha channel and a native shading would
// silently drop the transparency.
func TestCanEmitShadingRejectsAlpha(t *testing.T) {
	stops := opaqueStops()
	stops[0].Color.A = 128
	desc := render.ShadingDesc{Kind: render.ShadingAxial, Stops: stops, Spread: render.SpreadPad}
	if _, ok := canEmitShading(desc); ok {
		t.Fatal("a stop with alpha < 255 must not be emittable as a native shading")
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
// across more than one coincident pair.
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
	if float64(b0) < 0.5 || float64(b0) > 0.5+1e-3 {
		t.Errorf("Bounds[0] = %v, want ~0.5 (the hard break, only nudged an imperceptible amount)", b0)
	}
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

// TestFillShadingFallsBackAndLogsReason proves a shader whose description is
// not emittable (here: a stop carries alpha) still rasterizes into an image
// (the pre-existing fallback behavior) and logs exactly one fidelity note
// explaining why, so a WithLogf caller can see the decision.
func TestFillShadingFallsBackAndLogsReason(t *testing.T) {
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
	stops[0].Color.A = 100 // transparent stop: must force the raster fallback
	shader := raster.NewAxialShader(0, 0, 100, 0, rampFunc{}, stops, raster.SpreadPad)
	dev.FillShading(shader, render.Matrix{A: 1, D: 1}, "")
	dev.Restore()

	if len(dev.shadings) != 0 {
		t.Errorf("shadings recorded = %d, want 0 (should have fallen back)", len(dev.shadings))
	}
	if len(dev.images) == 0 {
		t.Fatal("fallback did not rasterize into an image")
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want exactly one fallback note", logs)
	}
	if !bytes.Contains([]byte(logs[0]), []byte("alpha")) {
		t.Errorf("log message = %q, want it to mention alpha as the reason", logs[0])
	}
}
