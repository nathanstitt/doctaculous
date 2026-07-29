package crop

import (
	"image"
	"image/color"
	"testing"
)

func TestScaleProducesExactTargetSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	fillRect(img, img.Bounds(), color.RGBA{10, 120, 200, 255})

	out, err := Scale(img, Options{Strategy: StrategyCenter, Width: 720, Height: 720})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if got := out.Bounds().Dx(); got != 720 {
		t.Errorf("width = %d, want 720", got)
	}
	if got := out.Bounds().Dy(); got != 720 {
		t.Errorf("height = %d, want 720", got)
	}
}

func TestScaleUpscalesSmallSource(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 40))
	fillRect(img, img.Bounds(), color.RGBA{200, 200, 200, 255})

	out, err := Scale(img, Options{Strategy: StrategyCenter, Width: 300, Height: 300})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if out.Bounds().Dx() != 300 || out.Bounds().Dy() != 300 {
		t.Errorf("bounds = %v, want 300x300", out.Bounds())
	}
}

func TestScalePreservesContentOfCrop(t *testing.T) {
	// Left half red, right half blue. A west crop must come back all red.
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	fillRect(img, image.Rect(0, 0, 100, 100), color.RGBA{255, 0, 0, 255})
	fillRect(img, image.Rect(100, 0, 200, 100), color.RGBA{0, 0, 255, 255})

	out, err := Scale(img, Options{Strategy: StrategyWest, Width: 50, Height: 50})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	r, g, b, _ := out.At(25, 25).RGBA()
	if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
		t.Errorf("centre pixel = (%d,%d,%d), want predominantly red", r>>8, g>>8, b>>8)
	}
}

func TestScaleRejectsInvalidSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if _, err := Scale(img, Options{Strategy: StrategyCenter, Width: -1, Height: 10}); err == nil {
		t.Fatal("expected an error for negative width, got nil")
	}
}

// TestScaleExactSizeAcrossAspectRatios pins the generalisation the API promises:
// 720x720 is the immediate use case, but any positive target must come back at
// exactly the requested size, from any source aspect. Both fitWindow branches
// and the clamp are exercised here.
func TestScaleExactSizeAcrossAspectRatios(t *testing.T) {
	// Sizes are kept small deliberately: this test is about the exact-size
	// contract and the fitWindow branches, both of which are pure geometry and
	// scale-invariant. Large sources only add CatmullRom and scoreMap time
	// (~6s across the sweep) without covering anything extra —
	// TestScaleProducesExactTargetSize keeps one genuinely large case.
	sources := []image.Rectangle{
		image.Rect(0, 0, 400, 300),   // landscape 4:3
		image.Rect(0, 0, 300, 400),   // portrait 3:4
		image.Rect(0, 0, 200, 200),   // square
		image.Rect(0, 0, 384, 216),   // wide 16:9
		image.Rect(0, 0, 64, 48),     // tiny, forces upscale
		image.Rect(20, 10, 420, 310), // non-zero origin
	}
	targets := []struct{ w, h int }{
		{72, 72},   // square (the 720x720 shape, scaled down)
		{108, 135}, // portrait 4:5
		{192, 108}, // wide 16:9
		{1, 1},     // degenerate minimum
		{7, 3},     // odd ratio, exercises integer truncation
	}
	for _, src := range sources {
		for _, tg := range targets {
			for _, s := range []Strategy{StrategyCenter, StrategySaliency, StrategyNorth} {
				img := image.NewRGBA(src)
				fillRect(img, img.Bounds(), color.RGBA{70, 110, 90, 255})
				out, err := Scale(img, Options{Strategy: s, Width: tg.w, Height: tg.h})
				if err != nil {
					t.Fatalf("Scale(src=%v, %dx%d, strategy=%d): %v", src, tg.w, tg.h, s, err)
				}
				if out.Bounds().Dx() != tg.w || out.Bounds().Dy() != tg.h {
					t.Errorf("src=%v strategy=%d: bounds = %v, want %dx%d",
						src, s, out.Bounds(), tg.w, tg.h)
				}
			}
		}
	}
}

func TestScaleRectReportsTheRectangleUsed(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	fillRect(img, img.Bounds(), color.RGBA{120, 120, 120, 255})

	out, got, err := ScaleRect(img, Options{Strategy: StrategyCenter, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("ScaleRect: %v", err)
	}
	// A 1:1 target against 400x300 takes the full height, centred on x.
	if want := image.Rect(50, 0, 350, 300); got != want {
		t.Errorf("rect = %v, want %v", got, want)
	}
	if out.Bounds().Dx() != 100 || out.Bounds().Dy() != 100 {
		t.Errorf("bounds = %v, want 100x100", out.Bounds())
	}
	// Scale must agree with ScaleRect on the image itself.
	plain, err := Scale(img, Options{Strategy: StrategyCenter, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if plain.Bounds() != out.Bounds() {
		t.Errorf("Scale bounds %v != ScaleRect bounds %v", plain.Bounds(), out.Bounds())
	}
}

func TestScaleRectRoundTripsThroughStrategyRect(t *testing.T) {
	// A reported saliency rectangle, fed back as Options.Rect, must reproduce
	// the same crop — this is what lets a caller persist a smart crop.
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	fillRect(img, img.Bounds(), color.RGBA{128, 128, 128, 255})
	checkerRect(img, image.Rect(10, 10, 190, 190))

	_, first, err := ScaleRect(img, Options{Strategy: StrategySaliency, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("ScaleRect(saliency): %v", err)
	}
	_, again, err := ScaleRect(img, Options{Strategy: StrategyRect, Rect: first, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("ScaleRect(rect): %v", err)
	}
	if again != first {
		t.Errorf("replayed rect = %v, want %v", again, first)
	}
}
