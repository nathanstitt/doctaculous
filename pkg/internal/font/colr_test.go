package font

import (
	"os"
	"path/filepath"
	"testing"
)

// colorFontFixture loads a committed colour-font fixture.
func colorFontFixture(t *testing.T, name string) *Face {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "fonts", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	face, err := LoadSFNT(b)
	if err != nil {
		t.Fatalf("LoadSFNT(%s): %v", name, err)
	}
	return face
}

// The five codepoints the fixtures were subset to. Between them they exercise flat
// solid layers, a linear gradient, a radial gradient, nested colour-glyph references,
// and rotated transforms — see testdata/gen/fonts/tools/README.md.
var fixtureEmoji = []rune{'\U0001F600', '\U0001F389', '❤', '\U0001F44D', '\U0001F31F'}

// A COLR/CPAL face reports colour glyphs and resolves every fixture codepoint to a
// non-empty layer list. Before this the tables were not read at all, so an emoji
// painted as its (empty) base outline: nothing.
func TestColorLayersResolveForEveryFixtureGlyph(t *testing.T) {
	face := colorFontFixture(t, "NotoColorEmoji-COLRv1.ttf")
	if !face.HasColorGlyphs() {
		t.Fatal("HasColorGlyphs = false for a COLR/CPAL font")
	}
	for _, r := range fixtureEmoji {
		gid, ok := face.GID(r)
		if !ok {
			t.Errorf("%U: no glyph id", r)
			continue
		}
		layers, ok := face.ColorLayers(gid)
		if !ok || len(layers) == 0 {
			t.Errorf("%U (gid %d): no colour layers", r, gid)
		}
	}
}

// The layers carry real palette colours, not zero values. A parser that read the
// wrong offsets would still return the right NUMBER of layers while every colour came
// out black or transparent, which a count-only assertion would not catch.
func TestColorLayersCarryPaletteColors(t *testing.T) {
	face := colorFontFixture(t, "NotoColorEmoji-COLRv1.ttf")
	gid, _ := face.GID('❤')
	layers, ok := face.ColorLayers(gid)
	if !ok || len(layers) == 0 {
		t.Fatal("no layers for the heart")
	}
	// A heart is red: its first layer must be a saturated red, not black or blank.
	c := layers[0].Color
	if c.A == 0 {
		t.Fatal("first layer is fully transparent")
	}
	if c.R <= 200 || c.G >= 120 || c.B >= 120 {
		t.Errorf("first layer colour = %v, want a saturated red", c)
	}
}

// A non-colour face reports no colour glyphs and no layers, so the colour path costs
// one check and never fires for ordinary text.
func TestPlainFaceHasNoColorGlyphs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "gen", "fonts", "Roboto-Regular.ttf"))
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	face, err := LoadSFNT(b)
	if err != nil {
		t.Fatalf("LoadSFNT: %v", err)
	}
	if face.HasColorGlyphs() {
		t.Error("HasColorGlyphs = true for a plain outline font")
	}
	gid, _ := face.GID('A')
	if layers, ok := face.ColorLayers(gid); ok || len(layers) != 0 {
		t.Errorf("ColorLayers on a plain face returned %d layers", len(layers))
	}
}

// COLR v1 gradients decode with their geometry and stops. The party popper carries a
// linear gradient and the grinning face a radial one; an implementation that refused
// gradients (an earlier one did) returned no layers at all for these glyphs.
func TestColorLayersDecodeGradients(t *testing.T) {
	face := colorFontFixture(t, "NotoColorEmoji-COLRv1.ttf")
	found := map[bool]int{} // radial -> count
	for _, r := range fixtureEmoji {
		gid, _ := face.GID(r)
		layers, _ := face.ColorLayers(gid)
		for _, l := range layers {
			if l.Gradient == nil {
				continue
			}
			g := l.Gradient
			found[g.Radial]++
			if len(g.Stops) < 2 {
				t.Errorf("%U: gradient has %d stops, want >= 2", r, len(g.Stops))
			}
			for i := 1; i < len(g.Stops); i++ {
				if g.Stops[i].Offset < g.Stops[i-1].Offset {
					t.Errorf("%U: gradient stops are not sorted by offset", r)
					break
				}
			}
		}
	}
	if found[false] == 0 {
		t.Error("no linear gradient decoded; the fixture contains one")
	}
	if found[true] == 0 {
		t.Error("no radial gradient decoded; the fixture contains one")
	}
}

// Layer transforms decode as full affines. Modelling only translation+flip — an
// earlier version did — forced the party popper (whose streamers are rotated) to be
// refused entirely, so this asserts a genuinely rotated layer survives.
func TestColorLayerTransformsIncludeRotation(t *testing.T) {
	face := colorFontFixture(t, "NotoColorEmoji-COLRv1.ttf")
	gid, _ := face.GID('\U0001F389')
	layers, ok := face.ColorLayers(gid)
	if !ok || len(layers) == 0 {
		t.Fatal("no layers for the party popper")
	}
	rotated := 0
	for _, l := range layers {
		if _, yx, xy, _, _, _ := l.Transform(); yx != 0 || xy != 0 {
			rotated++
		}
	}
	if rotated == 0 {
		t.Error("no layer carries an off-diagonal (rotating) transform, but the glyph has them")
	}
}

// An identity layer reports the identity, so the paint path can skip the matrix build.
func TestColorLayerIdentityDefault(t *testing.T) {
	var l ColorLayer
	if !l.IsIdentity() {
		t.Error("the zero ColorLayer is not reported as identity")
	}
	xx, yx, xy, yy, dx, dy := l.Transform()
	if xx != 1 || yy != 1 || yx != 0 || xy != 0 || dx != 0 || dy != 0 {
		t.Errorf("zero transform = (%v,%v,%v,%v,%v,%v), want identity", xx, yx, xy, yy, dx, dy)
	}
}

// A malformed COLR table degrades to "no colour glyphs" rather than panicking or
// making an otherwise-usable font fail to load. Colour tables are untrusted document
// input reached through @font-face.
func TestMalformedColorTablesDegrade(t *testing.T) {
	cases := []struct {
		name string
		colr []byte
	}{
		{"empty", nil},
		{"truncated header", []byte{0, 1, 0}},
		{"offsets past the end", []byte{
			0, 0, // version 0
			0, 1, // numBaseGlyphRecords
			0xFF, 0xFF, 0xFF, 0xFF, // baseGlyphRecordsOffset
			0xFF, 0xFF, 0xFF, 0xFF, // layerRecordsOffset
			0, 1, // numLayerRecords
		}},
	}
	for _, c := range cases {
		if _, ok := parseCOLR(c.colr); ok {
			t.Errorf("%s: parseCOLR accepted a malformed table", c.name)
		}
	}
}

// CPAL rejects a palette whose colour records do not fit, rather than reading past
// the table.
func TestMalformedCPALDegrades(t *testing.T) {
	bad := []byte{
		0, 0, // version
		0, 4, // numPaletteEntries
		0, 1, // numPalettes
		0, 1, // numColorRecords (fewer than the palette needs)
		0, 0, 0, 12, // colorRecordsArrayOffset
		0, 0, // colorRecordIndices[0]
	}
	if _, ok := parseCPAL(bad); ok {
		t.Error("parseCPAL accepted a palette whose entries exceed its records")
	}
}
