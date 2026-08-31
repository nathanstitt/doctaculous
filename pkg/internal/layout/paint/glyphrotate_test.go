package paint

import (
	"math"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/font"
	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/render"
)

// matCaptureDev records the transform each glyph is drawn with, so a test can assert on
// the composed matrix rather than on rasterized pixels — the matrix is where the sign of
// the rotation actually lives. It embeds render.Device for the rest of the interface, as
// glyphRouteDev does.
type matCaptureDev struct {
	render.Device
	mats []render.Matrix
}

func (d *matCaptureDev) DrawGlyph(g render.GlyphRef) { d.mats = append(d.mats, g.Transform) }

// apply maps a point through m using the package's row-vector convention.
func apply(m render.Matrix, x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// paintOneGlyph paints a single glyph at the given rotation and returns its matrix.
func paintOneGlyph(t *testing.T, rotate float64) render.Matrix {
	t.Helper()
	face, ok := font.LoadStandard("Helvetica", font.Style{})
	if !ok {
		t.Fatal("no Helvetica")
	}
	gid, _ := face.GID('A')
	pg := &layout.Page{
		WidthPt: 100, HeightPt: 100,
		Items: []layout.Item{{
			Kind: layout.GlyphKind,
			Glyph: layout.GlyphItem{
				Outline: face.Outline(gid), XPt: 0, YPt: 0, SizePt: 10,
				Face: face, GID: gid, Runes: []rune{'A'}, Rotate: rotate,
			},
		}},
	}
	dev := &matCaptureDev{}
	PaintPage(dev, pg, render.Scale(1, 1))
	if len(dev.mats) != 1 {
		t.Fatalf("drew %d glyphs, want 1", len(dev.mats))
	}
	return dev.mats[0]
}

// The rotation's SIGN is the trap in this path, and it fails silently: a glyph rotated
// the wrong way still paints, at the right place, at the right size — the text simply
// reads bottom-to-top with mirrored letters. Nothing errors.
//
// It is asserted through the composed matrix rather than by eye because the matrix is
// where the sign lives. Two directions pin it:
//
//   - the em-space ADVANCE direction (1,0) must map to page +Y, so successive glyphs
//     march DOWN the page;
//   - em-space UP (0,1) must map to page +X, so the glyph tops face right — the
//     orientation you tilt your head clockwise to read, which is what CSS specifies for
//     a vertical writing mode.
//
// Getting only the first right still allows a 180-degree error, which is why both are
// checked. During development this composed with a compensating negation for the em
// scale's Y flip, which reversed both and rendered exactly that way.
func TestGlyphRotationTurnsClockwiseInPageSpace(t *testing.T) {
	m := paintOneGlyph(t, math.Pi/2)

	// The origin must not move: the glyph turns about its own pen origin.
	if ox, oy := apply(m, 0, 0); ox > 1e-9 || ox < -1e-9 || oy > 1e-9 || oy < -1e-9 {
		t.Errorf("origin moved to (%v,%v); the rotation must be about the glyph's own origin", ox, oy)
	}

	advX, advY := apply(m, 1, 0)
	if advY <= 0 {
		t.Errorf("em advance (1,0) maps to page (%v,%v); want page +Y so glyphs march DOWN", advX, advY)
	}
	if advX > 1e-6 || advX < -1e-6 {
		t.Errorf("em advance (1,0) has page X = %v, want ~0 (a quarter turn, not a partial one)", advX)
	}

	upX, upY := apply(m, 0, 1)
	if upX <= 0 {
		t.Errorf("em up (0,1) maps to page (%v,%v); want page +X so glyph tops face RIGHT", upX, upY)
	}
}

// Zero rotation must compose to exactly the matrix a glyph got before this field
// existed — the guarantee that every horizontal document is byte-identical. Asserted
// against a rotation that is skipped rather than multiplied by an identity, since a
// float multiply through cos(0)/sin(0) is not guaranteed to be bit-exact.
func TestZeroRotationLeavesTheMatrixUntouched(t *testing.T) {
	m := paintOneGlyph(t, 0)
	want := render.Scale(10, -10).Mul(render.Translate(0, 0)).Mul(render.Scale(1, 1))
	if m != want {
		t.Errorf("unrotated glyph matrix = %+v, want %+v (bit-identical)", m, want)
	}
}

// A rotated glyph keeps its SIZE: the turn must be a rotation, not a scale-and-rotate.
// A basis vector's length is the em scale in both directions.
func TestRotationPreservesScale(t *testing.T) {
	m := paintOneGlyph(t, math.Pi/2)
	for _, tc := range []struct {
		name string
		x, y float64
	}{{"advance", 1, 0}, {"up", 0, 1}} {
		px, py := apply(m, tc.x, tc.y)
		if got := math.Hypot(px, py); math.Abs(got-10) > 1e-6 {
			t.Errorf("%s basis length = %v, want 10 (the em scale)", tc.name, got)
		}
	}
}
