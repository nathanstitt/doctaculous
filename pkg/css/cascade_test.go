package css

import (
	"image/color"
	"testing"
)

func TestInitialComputedStyle(t *testing.T) {
	cs := initialStyle()
	if cs.Display != "inline" { // CSS initial value of display is inline
		t.Fatalf("initial display = %q, want inline", cs.Display)
	}
	if cs.Color != (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("initial color = %v, want black", cs.Color)
	}
	if cs.FontSizePt != 16 { // 16px default medium, expressed in px-as-pt placeholder
		t.Fatalf("initial font-size = %v, want 16", cs.FontSizePt)
	}
}
