package font

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// loadFixtureFace parses a font file from testdata/gen/fonts for the tests that
// need a face whose .notdef is REAL — every bundled substitute ships a blank one,
// so the font-glyph branch of NotdefGlyph is unreachable through LoadStandard.
func loadFixtureFace(t *testing.T, name string) *Face {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "gen", "fonts", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture %s unavailable: %v", name, err)
	}
	face, err := LoadSFNT(raw)
	if err != nil {
		t.Fatalf("LoadSFNT(%s): %v", name, err)
	}
	return face
}

// TestNotdefGlyphUsesFontsOwnWhenPresent locks the browser-faithful first choice:
// a font that ships a drawn .notdef gets ITS mark, not our synthesized box.
// DejaVu Sans is the fixture precisely because it is one of the two faces on the
// hardware where the missing-glyph bug was found, and it draws a hollow box at
// glyph 0.
func TestNotdefGlyphUsesFontsOwnWhenPresent(t *testing.T) {
	face := loadFixtureFace(t, "DejaVuSans.ttf")

	own := face.Outline(0)
	if own == nil || own.Empty() {
		t.Skip("fixture's glyph 0 has no geometry; the font-glyph branch needs one that does")
	}

	got, adv := face.NotdefGlyph()
	if got == nil || got.Empty() {
		t.Fatal("NotdefGlyph returned an empty outline for a font with a real .notdef")
	}
	if len(got.Segments) != len(own.Segments) {
		t.Errorf("outline has %d segments, want the font's own %d — the synthesized box was used instead",
			len(got.Segments), len(own.Segments))
	}
	// The advance must be the FONT's, so the mark occupies the width its designer
	// intended rather than our generic one.
	if wantAdv := face.GlyphAdvance(0); adv != wantAdv {
		t.Errorf("advance = %v, want the font's own %v", adv, wantAdv)
	}
}

// TestNotdefGlyphSynthesizesWhenFontHasNone covers the branch that actually runs
// for this repo's bundled faces: every TeX Gyre substitute has a .notdef with a
// non-zero advance and ZERO contours, so stopping at the font's own glyph would
// still paint nothing. A synthesized box is required for the mark to be visible.
func TestNotdefGlyphSynthesizesWhenFontHasNone(t *testing.T) {
	for _, family := range []string{"sans-serif", "serif", "monospace"} {
		face, ok := LoadStandard(family, Style{})
		if !ok {
			t.Fatalf("LoadStandard(%q) failed", family)
		}
		if o := face.Outline(0); o != nil && !o.Empty() {
			t.Fatalf("%s: bundled substitute unexpectedly HAS a drawn .notdef; this test's premise (and the comment on NotdefGlyph) is stale", family)
		}

		got, adv := face.NotdefGlyph()
		if got == nil || got.Empty() {
			t.Errorf("%s: NotdefGlyph returned nothing; a missing rune would still render as empty space", family)
			continue
		}
		if adv <= 0 {
			t.Errorf("%s: advance = %v, want > 0 so the box occupies horizontal space", family, adv)
		}
	}
}

// TestNotdefBoxIsHollowAndOnTheBaseline checks the synthesized box's geometry
// rather than merely that it is non-empty: a filled blob or a box drifting off the
// line would both "pass" a non-empty check while looking nothing like tofu.
func TestNotdefBoxIsHollowAndOnTheBaseline(t *testing.T) {
	p := notdefBoxPath()

	minX, minY, maxX, maxY, ok := p.Bounds()
	if !ok {
		t.Fatal("synthesized box has no bounds")
	}
	if minY < 0 {
		t.Errorf("box descends below the baseline (minY=%v); it should sit on it", minY)
	}
	if maxY > 1 {
		t.Errorf("box top %v exceeds one em; it would collide with the line above", maxY)
	}
	if maxX > notdefAdvanceEm {
		t.Errorf("box right edge %v exceeds its advance %v; consecutive boxes would touch", maxX, notdefAdvanceEm)
	}
	if minX <= 0 {
		t.Errorf("box left edge %v has no side bearing; it would abut the previous glyph", minX)
	}

	// Hollow, not solid: two subpaths, so the nonzero fill cancels the interior.
	// Counting MoveTo ops is the direct way to assert that without re-rasterizing.
	moves := 0
	for _, s := range p.Segments {
		if s.Kind == render.MoveTo {
			moves++
		}
	}
	if moves != 2 {
		t.Errorf("box has %d subpaths, want 2 (outer wall + inner hole); a single one fills solid", moves)
	}
}

// TestNotdefGlyphReturnsFreshOutline guards the doc comment's promise that a
// caller may retain the returned path. Shape stores it on a Glyph that outlives
// the call, so a shared mutable path would let one glyph's later edit corrupt
// every other .notdef on the page.
func TestNotdefGlyphReturnsFreshOutline(t *testing.T) {
	face, ok := LoadStandard("sans-serif", Style{})
	if !ok {
		t.Fatal("LoadStandard failed")
	}
	a, _ := face.NotdefGlyph()
	b, _ := face.NotdefGlyph()
	if a == b {
		t.Fatal("NotdefGlyph returned the same *Path twice; callers retain it, so it must not be shared")
	}
	a.Reset()
	if b.Empty() {
		t.Error("mutating one returned outline emptied another")
	}
}
