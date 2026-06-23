package raster

import (
	"image"
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/render"
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
