package font

import (
	"math"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// The bundled sans-serif face lacks the square bullet (and others); Glyph must still
// return an outline + advance for every CSS bullet style so list markers render.
func TestSyntheticBulletGlyphs(t *testing.T) {
	face, ok := LoadStandard("sans-serif", Style{})
	if !ok {
		t.Fatal("LoadStandard sans-serif failed")
	}
	for _, r := range []rune{'•', '◦', '▪', '●', '○', '◾', '■'} {
		outline, adv, ok := face.Glyph(r)
		if !ok {
			t.Errorf("Glyph(%q) ok=false, want a marker outline", r)
			continue
		}
		if adv <= 0 {
			t.Errorf("Glyph(%q) advance=%v, want > 0", r, adv)
		}
		if outline == nil || outline.Empty() {
			t.Errorf("Glyph(%q) outline empty, want geometry", r)
		}
	}
}

// syntheticBullet returns false for non-bullet runes so the real font path is used.
func TestSyntheticBulletRejectsNonBullets(t *testing.T) {
	for _, r := range []rune{'a', '1', '-', '.', ' ', '*', '·'} {
		if _, _, ok := syntheticBullet(r); ok {
			t.Errorf("syntheticBullet(%q) ok=true, want false", r)
		}
	}
}

// The hollow ring must actually be hollow: its inner contour winds opposite the
// outer one so the nonzero fill cancels the interior (otherwise it paints as a disc).
func TestRingIsHollow(t *testing.T) {
	p := ringPath()
	// Signed area per subpath via the shoelace formula on the segment endpoints; the
	// two contours must have opposite signs (one CCW, one CW).
	signs := subpathAreaSigns(p)
	if len(signs) != 2 {
		t.Fatalf("ring has %d subpaths, want 2", len(signs))
	}
	if signs[0]*signs[1] >= 0 {
		t.Errorf("ring contours wind the same way (signs %v); interior would fill", signs)
	}
}

// The disc and square are single filled contours.
func TestDiscAndSquareSingleContour(t *testing.T) {
	if got := len(subpathAreaSigns(discPath())); got != 1 {
		t.Errorf("disc has %d subpaths, want 1", got)
	}
	if got := len(subpathAreaSigns(squarePath())); got != 1 {
		t.Errorf("square has %d subpaths, want 1", got)
	}
}

// subpathAreaSigns returns the sign of each subpath's signed area, sampling Bézier
// endpoints (control points are ignored — enough to detect winding direction here).
func subpathAreaSigns(p *render.Path) []float64 {
	var signs []float64
	var area, lx, ly, sx, sy float64
	flush := func() {
		if area != 0 {
			signs = append(signs, math.Copysign(1, area))
		}
		area = 0
	}
	acc := func(x, y float64) {
		area += lx*y - x*ly
		lx, ly = x, y
	}
	for _, s := range p.Segments {
		switch s.Kind {
		case render.MoveTo:
			flush()
			sx, sy = s.P0.X, s.P0.Y
			lx, ly = sx, sy
		case render.LineTo:
			acc(s.P0.X, s.P0.Y)
		case render.CubeTo:
			acc(s.P2.X, s.P2.Y)
		case render.Close:
			acc(sx, sy)
		}
	}
	flush()
	return signs
}
