package crop

import (
	"image"
	"testing"
)

func TestRectCenterSquareFromLandscape(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	got, err := Rect(img, Options{Strategy: StrategyCenter, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	// A 1:1 target against 400x300 takes the full height and centers on x.
	want := image.Rect(50, 0, 350, 300)
	if got != want {
		t.Errorf("Rect = %v, want %v", got, want)
	}
}

func TestRectCenterSquareFromPortrait(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 400))
	got, err := Rect(img, Options{Strategy: StrategyCenter, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	want := image.Rect(0, 50, 300, 350)
	if got != want {
		t.Errorf("Rect = %v, want %v", got, want)
	}
}

func TestRectNorthAnchorsTop(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 400))
	got, err := Rect(img, Options{Strategy: StrategyNorth, Width: 1, Height: 1})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	want := image.Rect(0, 0, 300, 300)
	if got != want {
		t.Errorf("Rect = %v, want %v", got, want)
	}
}

func TestRectExplicitRectIsClampedToBounds(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	got, err := Rect(img, Options{
		Strategy: StrategyRect,
		Width:    10, Height: 10,
		Rect: image.Rect(50, 50, 500, 500),
	})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	want := image.Rect(50, 50, 100, 100)
	if got != want {
		t.Errorf("Rect = %v, want %v", got, want)
	}
}

func TestRectRejectsNonPositiveSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if _, err := Rect(img, Options{Strategy: StrategyCenter, Width: 0, Height: 5}); err == nil {
		t.Fatal("expected an error for zero width, got nil")
	}
}

func TestRectHonoursNonZeroOrigin(t *testing.T) {
	// image.Image bounds need not start at (0,0); sub-images do not.
	img := image.NewRGBA(image.Rect(20, 10, 420, 310))
	got, err := Rect(img, Options{Strategy: StrategyCenter, Width: 100, Height: 100})
	if err != nil {
		t.Fatalf("Rect: %v", err)
	}
	want := image.Rect(70, 10, 370, 310)
	if got != want {
		t.Errorf("Rect = %v, want %v", got, want)
	}
}
