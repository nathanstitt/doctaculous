package raster

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	"github.com/nathanstitt/omnidoc/pkg/pdf"
)

// constShaderRGBA paints one solid color everywhere, for FillShading tests.
type constShaderRGBA color.RGBA

func (s constShaderRGBA) ColorAt(float64, float64) (color.RGBA, bool) {
	return color.RGBA(s), true
}

// linRamp is a 1-in/3-out linear ramp Func used to verify axial/radial parametric
// mapping without depending on the function package's parser: at t it returns
// lerp(c0,c1,t) per channel.
type linRamp struct{ c0, c1 [3]float64 }

func (f linRamp) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}
	return []float64{
		f.c0[0] + t*(f.c1[0]-f.c0[0]),
		f.c0[1] + t*(f.c1[1]-f.c0[1]),
		f.c0[2] + t*(f.c1[2]-f.c0[2]),
	}
}
func (f linRamp) NumOutputs() int { return 3 }

// linRampAlpha is a 1-in/4-out linear ramp Func (straight RGBA), modeling
// the shape pkg/svg/stops.go's stopRamp produces: at t it returns
// lerp(c0,c1,t) per channel including alpha.
type linRampAlpha struct{ c0, c1 [4]float64 }

func (f linRampAlpha) Eval(in []float64) []float64 {
	t := 0.0
	if len(in) > 0 {
		t = in[0]
	}
	out := make([]float64, 4)
	for i := range out {
		out[i] = f.c0[i] + t*(f.c1[i]-f.c0[i])
	}
	return out
}
func (f linRampAlpha) NumOutputs() int { return 4 }

func near(a, b uint8, tol int) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func wantRGB(t *testing.T, got color.RGBA, ok bool, r, g, b uint8) {
	t.Helper()
	if !ok {
		t.Fatalf("ColorAt reported !ok, want color {%d %d %d}", r, g, b)
	}
	if !near(got.R, r, 1) || !near(got.G, g, 1) || !near(got.B, b, 1) || got.A != 255 {
		t.Fatalf("ColorAt = {%d %d %d %d}, want {%d %d %d 255}", got.R, got.G, got.B, got.A, r, g, b)
	}
}

func TestAxialShadingProjection(t *testing.T) {
	// Axis from (0,0) to (10,0); red→blue; no extend.
	s := &shading{
		shadingType: 2,
		csKind:      csRGB,
		fn:          linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}},
		domain:      [2]float64{0, 1},
		axis:        [4]float64{0, 0, 10, 0},
	}
	// Endpoint at axis start → red.
	c, ok := s.ColorAt(0, 0)
	wantRGB(t, c, ok, 255, 0, 0)
	// Endpoint at axis end → blue.
	c, ok = s.ColorAt(10, 0)
	wantRGB(t, c, ok, 0, 0, 255)
	// Midpoint → halfway (purple).
	c, ok = s.ColorAt(5, 0)
	wantRGB(t, c, ok, 128, 0, 128)
	// Off-axis but same projection (s depends only on the axis component).
	c, ok = s.ColorAt(5, 99)
	wantRGB(t, c, ok, 128, 0, 128)
	// Before the start with no extend → not painted.
	if _, ok := s.ColorAt(-1, 0); ok {
		t.Fatalf("ColorAt(-1,0) painted, want !ok (no extend)")
	}
	// After the end with no extend → not painted.
	if _, ok := s.ColorAt(11, 0); ok {
		t.Fatalf("ColorAt(11,0) painted, want !ok (no extend)")
	}
}

func TestAxialShadingExtend(t *testing.T) {
	s := &shading{
		shadingType: 2,
		csKind:      csRGB,
		fn:          linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}},
		domain:      [2]float64{0, 1},
		axis:        [4]float64{0, 0, 10, 0},
		extend:      [2]bool{true, true},
	}
	// Before start clamps to red; after end clamps to blue.
	c, ok := s.ColorAt(-5, 0)
	wantRGB(t, c, ok, 255, 0, 0)
	c, ok = s.ColorAt(50, 0)
	wantRGB(t, c, ok, 0, 0, 255)
}

func TestRadialShadingCircles(t *testing.T) {
	// Concentric: center (0,0), r0=0 → r1=10; green→yellow; extend outer only.
	s := &shading{
		shadingType: 3,
		csKind:      csRGB,
		fn:          linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}},
		domain:      [2]float64{0, 1},
		circles:     [6]float64{0, 0, 0, 0, 0, 10},
		extend:      [2]bool{false, true},
	}
	// At the center the smallest circle (r=0, s=0) passes → green.
	c, ok := s.ColorAt(0, 0)
	wantRGB(t, c, ok, 0, 255, 0)
	// On the outer circle (radius 10) → s=1 → yellow.
	c, ok = s.ColorAt(10, 0)
	wantRGB(t, c, ok, 255, 255, 0)
	// Halfway out (radius 5) → s=0.5 → halfway green→yellow.
	c, ok = s.ColorAt(5, 0)
	wantRGB(t, c, ok, 128, 255, 0)
	// Same radius in another direction → identical (concentric).
	c, ok = s.ColorAt(0, 5)
	wantRGB(t, c, ok, 128, 255, 0)
	// Outside radius 10 with outer extend → clamps to s=1 (yellow).
	c, ok = s.ColorAt(20, 0)
	wantRGB(t, c, ok, 255, 255, 0)
}

func TestRadialShadingNoExtendOutside(t *testing.T) {
	s := &shading{
		shadingType: 3,
		csKind:      csRGB,
		fn:          linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}},
		domain:      [2]float64{0, 1},
		circles:     [6]float64{0, 0, 0, 0, 0, 10},
		extend:      [2]bool{false, false},
	}
	// Beyond the outer circle with no extend → not painted.
	if _, ok := s.ColorAt(20, 0); ok {
		t.Fatalf("ColorAt(20,0) painted, want !ok (no outer extend)")
	}
}

// TestNewShaderAxial exercises the full dict→shader build path for an axial
// shading and confirms the parsed geometry evaluates as expected.
func TestNewShaderAxial(t *testing.T) {
	dict := pdf.Dict{
		"ShadingType": pdf.Integer(2),
		"ColorSpace":  pdf.Name("DeviceRGB"),
		"Coords":      pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(10), pdf.Integer(0)},
		"Domain":      pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"Function": pdf.Dict{
			"FunctionType": pdf.Integer(2),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"C0":           pdf.Array{pdf.Integer(1), pdf.Integer(0), pdf.Integer(0)},
			"C1":           pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(1)},
			"N":            pdf.Integer(1),
		},
		"Extend": pdf.Array{pdf.Boolean(false), pdf.Boolean(false)},
	}
	sh, err := newShader(nil, dict)
	if err != nil {
		t.Fatalf("newShader: %v", err)
	}
	c, ok := sh.ColorAt(5, 0)
	wantRGB(t, c, ok, 128, 0, 128)
}

// TestFillShadingClipAndMap drives FillShading through a Device with a clip and a
// scaling CTM, confirming device→user inverse mapping and clip honoring.
func TestFillShadingClipAndMap(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	fillBackground(img, color.White)
	d := New(img)

	// CTM scales user→device by 2, so a device pixel (x,y) maps to user (x/2,y/2).
	ctm := render.Scale(2, 2)
	// Clip to the device rect [4,16)×[4,16).
	clip := image.NewAlpha(image.Rect(4, 4, 16, 16))
	for y := 4; y < 16; y++ {
		for x := 4; x < 16; x++ {
			clip.SetAlpha(x, y, color.Alpha{A: 255})
		}
	}
	d.clip = []*image.Alpha{clip}

	// A shader that paints solid blue everywhere it is asked.
	d.FillShading(constShaderRGBA(color.RGBA{0, 0, 255, 255}), ctm, "Normal")

	// Inside the clip → blue; outside → untouched white.
	if got := img.RGBAAt(10, 10); got.B < 250 {
		t.Fatalf("inside clip = %v, want blue", got)
	}
	if got := img.RGBAAt(1, 1); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("outside clip = %v, want white (untouched)", got)
	}
}

// TestNewAxialShaderPad confirms NewAxialShader(..., SpreadPad) matches the
// existing /Extend[true true] clamp behavior exactly: t beyond [0,1] clamps to
// the endpoint color rather than mirroring or wrapping.
func TestNewAxialShaderPad(t *testing.T) {
	fn := linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}}
	sh := NewAxialShader(0, 0, 10, 0, fn, nil, SpreadPad)
	c, ok := sh.ColorAt(-5, 0)
	wantRGB(t, c, ok, 255, 0, 0) // clamps to t=0 (red)
	c, ok = sh.ColorAt(15, 0)
	wantRGB(t, c, ok, 0, 0, 255) // clamps to t=1 (blue)
	c, ok = sh.ColorAt(2.5, 0)
	wantRGB(t, c, ok, 191, 0, 64) // t=0.25
}

// TestNewAxialShaderReflect confirms SpreadReflect mirrors the ramp beyond
// [0,1]: sval=1.25 folds to 0.75, sval=2.25 folds to 0.25, sval=-0.25 folds to
// 0.25 (mirrored around 0).
func TestNewAxialShaderReflect(t *testing.T) {
	fn := linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}}
	sh := NewAxialShader(0, 0, 10, 0, fn, nil, SpreadReflect)

	want075, ok := sh.ColorAt(7.5, 0) // sval=0.75, within [0,1] unaffected by fold
	if !ok {
		t.Fatalf("ColorAt(7.5,0) reported !ok")
	}
	got, ok := sh.ColorAt(12.5, 0) // sval=1.25 -> reflects to 0.75
	wantRGB(t, got, ok, want075.R, want075.G, want075.B)

	want025, ok := sh.ColorAt(2.5, 0) // sval=0.25
	if !ok {
		t.Fatalf("ColorAt(2.5,0) reported !ok")
	}
	got, ok = sh.ColorAt(22.5, 0) // sval=2.25 -> reflects to 0.25
	wantRGB(t, got, ok, want025.R, want025.G, want025.B)

	got, ok = sh.ColorAt(-2.5, 0) // sval=-0.25 -> reflects to 0.25
	wantRGB(t, got, ok, want025.R, want025.G, want025.B)
}

// TestNewAxialShaderRepeat confirms SpreadRepeat wraps the ramp modulo 1:
// sval=1.25 wraps to 0.25, sval=-0.25 wraps to 0.75.
func TestNewAxialShaderRepeat(t *testing.T) {
	fn := linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}}
	sh := NewAxialShader(0, 0, 10, 0, fn, nil, SpreadRepeat)

	want025, ok := sh.ColorAt(2.5, 0) // sval=0.25
	if !ok {
		t.Fatalf("ColorAt(2.5,0) reported !ok")
	}
	got, ok := sh.ColorAt(12.5, 0) // sval=1.25 -> wraps to 0.25
	wantRGB(t, got, ok, want025.R, want025.G, want025.B)

	want075, ok := sh.ColorAt(7.5, 0) // sval=0.75
	if !ok {
		t.Fatalf("ColorAt(7.5,0) reported !ok")
	}
	got, ok = sh.ColorAt(-2.5, 0) // sval=-0.25 -> wraps to 0.75
	wantRGB(t, got, ok, want075.R, want075.G, want075.B)
}

// TestNewRadialShaderPad, TestNewRadialShaderReflect, and
// TestNewRadialShaderRepeat exercise the radial (focal-circle) form using
// SVG-style parameters (fx,fy,fr,cx,cy,cr) mapped onto circles[0..2]/[3..5].
func TestNewRadialShaderPad(t *testing.T) {
	fn := linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}}
	sh := NewRadialShader(0, 0, 0, 0, 0, 10, fn, nil, SpreadPad)
	c, ok := sh.ColorAt(0, 0)
	wantRGB(t, c, ok, 0, 255, 0)
	c, ok = sh.ColorAt(10, 0)
	wantRGB(t, c, ok, 255, 255, 0)
	c, ok = sh.ColorAt(20, 0) // beyond outer radius -> clamps to s=1
	wantRGB(t, c, ok, 255, 255, 0)
}

func TestNewRadialShaderReflect(t *testing.T) {
	fn := linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}}
	sh := NewRadialShader(0, 0, 0, 0, 0, 10, fn, nil, SpreadReflect)

	want, ok := sh.ColorAt(5, 0) // s=0.5, within [0,1]
	if !ok {
		t.Fatalf("ColorAt(5,0) reported !ok")
	}
	// s=1.5 (radius 15) reflects to 0.5.
	got, ok := sh.ColorAt(15, 0)
	wantRGB(t, got, ok, want.R, want.G, want.B)
}

func TestNewRadialShaderRepeat(t *testing.T) {
	fn := linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}}
	sh := NewRadialShader(0, 0, 0, 0, 0, 10, fn, nil, SpreadRepeat)

	want025, ok := sh.ColorAt(2.5, 0) // s=0.25
	if !ok {
		t.Fatalf("ColorAt(2.5,0) reported !ok")
	}
	// s=1.25 (radius 12.5) wraps to 0.25.
	got, ok := sh.ColorAt(12.5, 0)
	wantRGB(t, got, ok, want025.R, want025.G, want025.B)
}

// TestNewAxialShaderDegenerate confirms a zero-length axis never panics and
// paints the domain-start color under every spread mode.
func TestNewAxialShaderDegenerate(t *testing.T) {
	fn := linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}}
	for _, sp := range []Spread{SpreadPad, SpreadReflect, SpreadRepeat} {
		sh := NewAxialShader(5, 5, 5, 5, fn, nil, sp)
		c, ok := sh.ColorAt(100, -100)
		wantRGB(t, c, ok, 255, 0, 0)
	}
}

// TestNewRadialShaderZeroRadius confirms a zero-radius radial shader never
// panics under any spread mode.
func TestNewRadialShaderZeroRadius(t *testing.T) {
	fn := linRamp{c0: [3]float64{0, 1, 0}, c1: [3]float64{1, 1, 0}}
	for _, sp := range []Spread{SpreadPad, SpreadReflect, SpreadRepeat} {
		sh := NewRadialShader(0, 0, 0, 0, 0, 0, fn, nil, sp)
		sh.ColorAt(1, 1)
		sh.ColorAt(0, 0)
	}
}

// TestNewAxialShaderAlphaFromFn confirms a shader built via NewAxialShader
// with a 4-output Func (straight RGBA, the shape pkg/svg/stops.go's
// stopRamp produces) carries the 4th component through as real alpha rather
// than forcing opaque: a stop fading from opaque red to fully transparent
// blue must read back A≈255 at t=0, A≈128 at t=0.5, and A≈0 at t=1.
func TestNewAxialShaderAlphaFromFn(t *testing.T) {
	fn := linRampAlpha{c0: [4]float64{1, 0, 0, 1}, c1: [4]float64{0, 0, 1, 0}}
	sh := NewAxialShader(0, 0, 10, 0, fn, nil, SpreadPad)

	c, ok := sh.ColorAt(0, 0)
	if !ok {
		t.Fatalf("ColorAt(0,0) reported !ok")
	}
	if !near(c.A, 255, 1) {
		t.Fatalf("t=0: A = %d, want ~255", c.A)
	}
	c, ok = sh.ColorAt(5, 0)
	if !ok {
		t.Fatalf("ColorAt(5,0) reported !ok")
	}
	if !near(c.A, 128, 1) {
		t.Fatalf("t=0.5: A = %d, want ~128", c.A)
	}
	c, ok = sh.ColorAt(10, 0)
	if !ok {
		t.Fatalf("ColorAt(10,0) reported !ok")
	}
	if !near(c.A, 0, 1) {
		t.Fatalf("t=1: A = %d, want ~0", c.A)
	}
}

// TestPDFShadingCMYKStaysOpaque confirms a PDF-constructed shading (built via
// newShader, never via NewAxialShader/NewRadialShader) keeps A=0xFF even when
// its /Function returns 4 components for a CMYK /ColorSpace — the
// alphaFromFn flag (not component count) gates alpha interpretation, so a
// CMYK shading's K component is never misread as alpha.
func TestPDFShadingCMYKStaysOpaque(t *testing.T) {
	dict := pdf.Dict{
		"ShadingType": pdf.Integer(2),
		"ColorSpace":  pdf.Name("DeviceCMYK"),
		"Coords":      pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(10), pdf.Integer(0)},
		"Domain":      pdf.Array{pdf.Integer(0), pdf.Integer(1)},
		"Function": pdf.Dict{
			"FunctionType": pdf.Integer(2),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			// C0/C1 are 4-component CMYK: full black (K=1) at t=0, no ink at t=1.
			"C0": pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(0), pdf.Integer(1)},
			"C1": pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(0), pdf.Integer(0)},
			"N":  pdf.Integer(1),
		},
		"Extend": pdf.Array{pdf.Boolean(true), pdf.Boolean(true)},
	}
	sh, err := newShader(nil, dict)
	if err != nil {
		t.Fatalf("newShader: %v", err)
	}
	for _, x := range []float64{0, 5, 10} {
		c, ok := sh.ColorAt(x, 0)
		if !ok {
			t.Fatalf("ColorAt(%v,0) reported !ok", x)
		}
		if c.A != 0xFF {
			t.Fatalf("ColorAt(%v,0).A = %d, want 0xFF (CMYK K component must not be read as alpha)", x, c.A)
		}
	}
}

// TestAxialShaderDescribeShadingRoundTrips confirms a shading built via
// NewAxialShader implements render.ShadingDescriber and reports back exactly
// the coords/stops/spread it was constructed with.
func TestAxialShaderDescribeShadingRoundTrips(t *testing.T) {
	fn := linRampAlpha{c0: [4]float64{1, 0, 0, 1}, c1: [4]float64{0, 0, 1, 1}}
	stops := []render.ShadingStop{
		{Offset: 0, Color: color.RGBA{255, 0, 0, 255}},
		{Offset: 1, Color: color.RGBA{0, 0, 255, 255}},
	}
	sh := NewAxialShader(1, 2, 3, 4, fn, stops, SpreadReflect)

	describer, ok := sh.(render.ShadingDescriber)
	if !ok {
		t.Fatalf("axial shading does not implement render.ShadingDescriber")
	}
	desc, ok := describer.DescribeShading()
	if !ok {
		t.Fatalf("DescribeShading reported !ok for a describable axial shading")
	}
	if desc.Kind != render.ShadingAxial {
		t.Errorf("Kind = %v, want ShadingAxial", desc.Kind)
	}
	wantCoords := [6]float64{1, 2, 3, 4, 0, 0}
	if desc.Coords != wantCoords {
		t.Errorf("Coords = %v, want %v", desc.Coords, wantCoords)
	}
	if desc.Spread != SpreadReflect {
		t.Errorf("Spread = %v, want SpreadReflect", desc.Spread)
	}
	if len(desc.Stops) != len(stops) {
		t.Fatalf("Stops = %v, want %v", desc.Stops, stops)
	}
	for i := range stops {
		if desc.Stops[i] != stops[i] {
			t.Errorf("Stops[%d] = %v, want %v", i, desc.Stops[i], stops[i])
		}
	}
}

// TestRadialShaderDescribeShadingRoundTrips is TestAxialShaderDescribeShadingRoundTrips'
// radial counterpart: Coords must carry fx,fy,fr,cx,cy,cr in that order.
func TestRadialShaderDescribeShadingRoundTrips(t *testing.T) {
	fn := linRampAlpha{c0: [4]float64{0, 1, 0, 1}, c1: [4]float64{1, 1, 0, 1}}
	stops := []render.ShadingStop{
		{Offset: 0, Color: color.RGBA{0, 255, 0, 255}},
		{Offset: 0.5, Color: color.RGBA{128, 255, 0, 200}},
		{Offset: 1, Color: color.RGBA{255, 255, 0, 255}},
	}
	sh := NewRadialShader(1, 2, 3, 4, 5, 6, fn, stops, SpreadRepeat)

	describer, ok := sh.(render.ShadingDescriber)
	if !ok {
		t.Fatalf("radial shading does not implement render.ShadingDescriber")
	}
	desc, ok := describer.DescribeShading()
	if !ok {
		t.Fatalf("DescribeShading reported !ok for a describable radial shading")
	}
	if desc.Kind != render.ShadingRadial {
		t.Errorf("Kind = %v, want ShadingRadial", desc.Kind)
	}
	wantCoords := [6]float64{1, 2, 3, 4, 5, 6}
	if desc.Coords != wantCoords {
		t.Errorf("Coords = %v, want %v", desc.Coords, wantCoords)
	}
	if desc.Spread != SpreadRepeat {
		t.Errorf("Spread = %v, want SpreadRepeat", desc.Spread)
	}
	if len(desc.Stops) != len(stops) {
		t.Fatalf("Stops = %v, want %v", desc.Stops, stops)
	}
	for i := range stops {
		if desc.Stops[i] != stops[i] {
			t.Errorf("Stops[%d] = %v, want %v", i, desc.Stops[i], stops[i])
		}
	}
}

// TestPDFShadingDoesNotDescribe confirms a PDF-constructed shading (built via
// newShader) does NOT implement a describable DescribeShading — either it
// doesn't satisfy render.ShadingDescriber at all, or DescribeShading reports
// ok=false — because a PDF input shading already has its own source
// dictionary and round-tripping it back to a description is out of scope.
func TestPDFShadingDoesNotDescribe(t *testing.T) {
	dict := pdf.Dict{
		"ShadingType": pdf.Integer(2),
		"ColorSpace":  pdf.Name("DeviceRGB"),
		"Coords":      pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(10), pdf.Integer(0)},
		"Function": pdf.Dict{
			"FunctionType": pdf.Integer(2),
			"Domain":       pdf.Array{pdf.Integer(0), pdf.Integer(1)},
			"C0":           pdf.Array{pdf.Integer(1), pdf.Integer(0), pdf.Integer(0)},
			"C1":           pdf.Array{pdf.Integer(0), pdf.Integer(0), pdf.Integer(1)},
			"N":            pdf.Integer(1),
		},
		"Extend": pdf.Array{pdf.Boolean(true), pdf.Boolean(true)},
	}
	sh, err := newShader(nil, dict)
	if err != nil {
		t.Fatalf("newShader: %v", err)
	}
	if describer, ok := sh.(render.ShadingDescriber); ok {
		if _, ok := describer.DescribeShading(); ok {
			t.Fatalf("PDF-constructed shading must not describe itself (ok=true), it already has a source dictionary")
		}
	}
}

// countingRamp is a linRamp that records how many times Eval was called, so a
// test can distinguish "evaluated per pixel" from "evaluated per table entry".
type countingRamp struct {
	inner linRamp
	calls *int
}

func (f countingRamp) Eval(in []float64) []float64 {
	*f.calls++
	return f.inner.Eval(in)
}
func (f countingRamp) NumOutputs() int { return f.inner.NumOutputs() }

// TestShadingRampMemoized is the guard on the colorAt ramp cache: sampling a
// shading far more times than the table has entries must not evaluate the
// /Function more than once per entry. Without the cache this count would track
// the number of SAMPLES (the per-pixel cost the cache exists to remove).
func TestShadingRampMemoized(t *testing.T) {
	calls := 0
	fn := countingRamp{inner: linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0, 1}}, calls: &calls}
	sh := NewAxialShader(0, 0, 100, 0, fn, nil, SpreadPad)

	const samples = 50000
	for i := 0; i < samples; i++ {
		sh.ColorAt(float64(i)*100/float64(samples), 0)
	}
	if calls > rampLUTSize {
		t.Errorf("Function evaluated %d times for %d samples; the ramp cache should cap it at %d (one per table entry)",
			calls, samples, rampLUTSize)
	}
	if calls == 0 {
		t.Fatal("Function never evaluated; the shader is not sampling the ramp at all")
	}
}

// TestShadingRampQuantizationBounded pins the fidelity cost of the ramp cache.
//
// The table quantizes the parametric value, so a cached lookup can differ from a
// direct evaluation. That error is what decides whether the goldens hold, so it
// is asserted here directly rather than left to be discovered as a golden diff:
// at rampLUTSize entries no channel may move by more than 1/255, which is inside
// the golden suites' own per-channel tolerance.
func TestShadingRampQuantizationBounded(t *testing.T) {
	fn := linRamp{c0: [3]float64{1, 0, 0}, c1: [3]float64{0, 0.5, 1}}
	cached := NewAxialShader(0, 0, 100, 0, fn, nil, SpreadPad)

	// An independent shading used only for direct (uncached) evaluation, so
	// filling one ramp cannot affect the other's measurement.
	direct := NewAxialShader(0, 0, 100, 0, fn, nil, SpreadPad).(*shading)

	worst := 0
	const samples = 20000
	for i := 0; i <= samples; i++ {
		x := float64(i) * 100 / float64(samples)
		got, ok := cached.ColorAt(x, 0)
		if !ok {
			t.Fatalf("ColorAt(%g,0) reported !ok", x)
		}
		// Recompute the parametric value the shading would use, then evaluate
		// it without the table.
		want := direct.evalColorAt(float64(i) / float64(samples))
		for _, d := range []int{
			absInt(int(got.R) - int(want.R)),
			absInt(int(got.G) - int(want.G)),
			absInt(int(got.B) - int(want.B)),
			absInt(int(got.A) - int(want.A)),
		} {
			if d > worst {
				worst = d
			}
		}
	}
	if worst > 1 {
		t.Errorf("ramp cache shifts a channel by %d/255; it must stay within 1/255 to hold the golden tolerances (rampLUTSize=%d too small?)", worst, rampLUTSize)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
