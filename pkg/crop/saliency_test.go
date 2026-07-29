package crop

import (
	"image"
	"image/color"
	"testing"
)

// fillRect paints a solid rectangle into img.
func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// checkerRect paints a high-frequency checkerboard, which has high edge energy.
func checkerRect(img *image.RGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if (x/2+y/2)%2 == 0 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
}

func TestSaliencyPrefersDetailOverFlat(t *testing.T) {
	// 400x200, flat grey everywhere, detail in the left third.
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	fillRect(img, img.Bounds(), color.RGBA{128, 128, 128, 255})
	checkerRect(img, image.Rect(10, 10, 190, 190))

	r, err := Rect(img, Options{Strategy: StrategySaliency, Width: 1, Height: 1})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	// The 200x200 window should sit over the detail, not the centre (100..300).
	if mid := (r.Min.X + r.Max.X) / 2; mid > 150 {
		t.Errorf("crop centre x = %d, want <=150 (over the detailed left third); rect=%v", mid, r)
	}
}

func TestSaliencyPrefersSaturatedOverGrey(t *testing.T) {
	// Equal detail both sides; only the right side is saturated.
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	fillRect(img, img.Bounds(), color.RGBA{128, 128, 128, 255})
	checkerRect(img, image.Rect(10, 10, 190, 190))
	for y := 10; y < 190; y++ {
		for x := 210; x < 390; x++ {
			c := color.RGBA{200, 20, 20, 255}
			if (x/2+y/2)%2 == 0 {
				c = color.RGBA{40, 40, 200, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	edge, sat, center := 1.0, 3.0, 0.0
	r, err := Rect(img, Options{
		Strategy: StrategySaliency, Width: 1, Height: 1,
		// Center is an explicit 0: the centre prior is genuinely off here, not
		// silently defaulted back to 0.3.
		Weights: Weights{Edge: &edge, Saturation: &sat, Center: &center},
	})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	if mid := (r.Min.X + r.Max.X) / 2; mid < 250 {
		t.Errorf("crop centre x = %d, want >=250 (over the saturated right); rect=%v", mid, r)
	}
}

// TestWeightsZeroIsHonoured pins the reason Weights holds pointers: an explicit
// zero must disable a term, not fall back to the default. With the centre prior
// off and no other signal, an all-flat image has a completely flat score map, so
// every candidate ties and the centred seed wins — the same rectangle a working
// centre prior would give. What distinguishes the two is the scored value, so
// assert on the score map rather than the rectangle.
func TestWeightsZeroIsHonoured(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	fillRect(img, img.Bounds(), color.RGBA{90, 90, 90, 255})

	zero := 0.0
	off := scoreMap(img, Options{Weights: Weights{Center: &zero}})
	on := scoreMap(img, Options{}) // Center nil -> default 0.3

	// Flat grey: no edges, no saturation, no skin. With the centre prior off
	// every score is 0; with it on the centre pixel is strictly positive.
	for i, v := range off {
		if v != 0 {
			t.Fatalf("Center=0: score[%d] = %v, want 0 (the prior should be off)", i, v)
		}
	}
	mid := (20/2)*40 + (40 / 2)
	if on[mid] <= 0 {
		t.Errorf("Center=nil: score[%d] = %v, want > 0 (the default prior should apply)", mid, on[mid])
	}
}

func TestDefaultWeightsMatchDocumentedDefaults(t *testing.T) {
	w := DefaultWeights()
	for _, tc := range []struct {
		name string
		got  *float64
		want float64
	}{
		{"Edge", w.Edge, defaultEdgeWeight},
		{"Saturation", w.Saturation, defaultSaturationWeight},
		{"Skin", w.Skin, defaultSkinWeight},
		{"Center", w.Center, defaultCenterWeight},
	} {
		if tc.got == nil {
			t.Errorf("DefaultWeights().%s is nil, want %v", tc.name, tc.want)
			continue
		}
		if *tc.got != tc.want {
			t.Errorf("DefaultWeights().%s = %v, want %v", tc.name, *tc.got, tc.want)
		}
	}
	// The bias guard: skin must never outweigh edge energy.
	if *w.Skin >= *w.Edge {
		t.Errorf("SkinWeight %v >= EdgeWeight %v; skin must nudge, not decide", *w.Skin, *w.Edge)
	}
}

func TestSaliencyOnFlatImageIsCentered(t *testing.T) {
	// With no signal anywhere, the centre prior decides.
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	fillRect(img, img.Bounds(), color.RGBA{90, 90, 90, 255})

	r, err := Rect(img, Options{Strategy: StrategySaliency, Width: 1, Height: 1})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	want := image.Rect(100, 0, 300, 200)
	if r != want {
		t.Errorf("Rect = %v, want %v (centered)", r, want)
	}
}

func TestSaliencyResultIsWithinBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 133, 71))
	checkerRect(img, img.Bounds())
	r, err := Rect(img, Options{Strategy: StrategySaliency, Width: 3, Height: 2})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	if !r.In(img.Bounds()) {
		t.Errorf("rect %v escapes bounds %v", r, img.Bounds())
	}
}

func TestSaliencyIsDeterministic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	fillRect(img, img.Bounds(), color.RGBA{50, 90, 140, 255})
	checkerRect(img, image.Rect(20, 180, 120, 280))
	opts := Options{Strategy: StrategySaliency, Width: 1, Height: 1}
	first, err := Rect(img, opts)
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	for i := 0; i < 8; i++ {
		got, err := Rect(img, opts)
		if err != nil {
			t.Fatalf("Rect: %v", err)
		}
		if got != first {
			t.Fatalf("run %d returned %v, first run returned %v", i, got, first)
		}
	}
}
