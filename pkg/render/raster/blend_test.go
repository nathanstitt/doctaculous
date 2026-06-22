package raster

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// fillBlend fills a rect over an existing backdrop using a blend mode and returns
// the pixel at (px,py).
func fillBlend(t *testing.T, backdrop color.RGBA, src color.RGBA, mode string, px, py int) color.RGBA {
	t.Helper()
	dev, img := newTestDevice(50, 50)
	// Paint the whole canvas with the backdrop (Normal), then the source on top.
	dev.Fill(rectPath(0, 0, 50, 50), render.FillPaint{Color: backdrop})
	dev.Fill(rectPath(10, 10, 40, 40), render.FillPaint{Color: src, BlendMode: mode})
	return img.RGBAAt(px, py)
}

func TestBlendModesOverOpaque(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	near := func(got, want uint8) bool {
		d := int(got) - int(want)
		return d >= -1 && d <= 1
	}

	tests := []struct {
		mode                string
		wantR, wantG, wantB uint8
	}{
		// red backdrop, blue source.
		{"Multiply", 0, 0, 0},       // 1×0, 0×0, 0×1 = (0,0,0)
		{"Screen", 255, 0, 255},     // 1, 0, 1 = magenta
		{"Darken", 0, 0, 0},         // min per channel
		{"Lighten", 255, 0, 255},    // max per channel
		{"Difference", 255, 0, 255}, // |1-0|,|0-0|,|0-1|
		{"Exclusion", 255, 0, 255},  // a+b-2ab
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			c := fillBlend(t, red, blue, tc.mode, 25, 25)
			if !near(c.R, tc.wantR) || !near(c.G, tc.wantG) || !near(c.B, tc.wantB) {
				t.Errorf("%s red⊕blue = (%d,%d,%d), want (%d,%d,%d)",
					tc.mode, c.R, c.G, c.B, tc.wantR, tc.wantG, tc.wantB)
			}
		})
	}
}

func TestBlendNormalUnchanged(t *testing.T) {
	// A Normal (or unknown) blend over a backdrop is plain source-over: the opaque
	// blue source replaces the red backdrop.
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	for _, mode := range []string{"", "Normal", "Compatible", "BogusMode"} {
		c := fillBlend(t, red, blue, mode, 25, 25)
		if c != blue {
			t.Errorf("mode %q: got %v, want opaque blue (source-over)", mode, c)
		}
	}
}

func TestBlendLuminosityIsGray(t *testing.T) {
	// Luminosity takes the source's luminosity with the backdrop's hue/sat. A pure
	// backdrop gray with a colored source yields the source's luminance as gray.
	gray := color.RGBA{128, 128, 128, 255}
	red := color.RGBA{255, 0, 0, 255}
	c := fillBlend(t, gray, red, "Luminosity", 25, 25)
	// Backdrop is achromatic, so result is achromatic at red's luminosity (0.3*255≈76).
	if c.R != c.G || c.G != c.B {
		t.Errorf("Luminosity over gray = (%d,%d,%d), want achromatic", c.R, c.G, c.B)
	}
	if c.R < 60 || c.R > 90 {
		t.Errorf("Luminosity level = %d, want ~76 (0.3×255)", c.R)
	}
}

// TestSeparableBlendMath spot-checks the pure blend functions against the PDF
// formulas at representative inputs.
func TestSeparableBlendMath(t *testing.T) {
	approx := func(got, want float64) bool {
		d := got - want
		return d > -1e-9 && d < 1e-9
	}
	cases := []struct {
		mode   string
		cd, cs float64
		want   float64
	}{
		{"Multiply", 0.5, 0.5, 0.25},
		{"Screen", 0.5, 0.5, 0.75},
		{"Overlay", 0.25, 0.5, 0.25},   // dst<0.5 → 2·cd·cs = 2·.25·.5
		{"HardLight", 0.5, 0.25, 0.25}, // cs<0.5 → cd·2cs = .5·.5
		{"Difference", 0.2, 0.7, 0.5},
		{"Exclusion", 0.5, 0.5, 0.5},
	}
	for _, tc := range cases {
		f := separableBlends[tc.mode]
		if f == nil {
			t.Fatalf("no blend func for %q", tc.mode)
		}
		if got := f(tc.cd, tc.cs); !approx(got, tc.want) {
			t.Errorf("%s(%v,%v) = %v, want %v", tc.mode, tc.cd, tc.cs, got, tc.want)
		}
	}
}
