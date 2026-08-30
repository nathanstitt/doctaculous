package font

import (
	"testing"
)

// A CBDT/CBLC face reports bitmap strikes and decodes a real image for every fixture
// codepoint. Apple Color Emoji and the original Noto Color Emoji both ship colour
// glyphs this way — with no COLR table at all — so without this path an emoji from
// either renders nothing.
func TestColorBitmapsDecodeForEveryFixtureGlyph(t *testing.T) {
	face := colorFontFixture(t, "NotoColorEmoji-CBDT.ttf")
	if !face.HasColorBitmaps() {
		t.Fatal("HasColorBitmaps = false for a CBDT/CBLC font")
	}
	for _, r := range fixtureEmoji {
		gid, ok := face.GID(r)
		if !ok {
			t.Errorf("%U: no glyph id", r)
			continue
		}
		bm, ok := face.ColorBitmapFor(gid, 32)
		if !ok {
			t.Errorf("%U (gid %d): no bitmap", r, gid)
			continue
		}
		if bm.Img == nil {
			t.Errorf("%U: bitmap has a nil image", r)
			continue
		}
		if b := bm.Img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
			t.Errorf("%U: bitmap is %dx%d", r, b.Dx(), b.Dy())
		}
		if bm.PPEM <= 0 {
			t.Errorf("%U: bitmap reports ppem %v", r, bm.PPEM)
		}
	}
}

// A plain outline font has no strikes, so the bitmap probe is inert for ordinary text.
func TestPlainFaceHasNoColorBitmaps(t *testing.T) {
	face := colorFontFixture(t, "Roboto-Regular.ttf")
	if face.HasColorBitmaps() {
		t.Error("HasColorBitmaps = true for a plain outline font")
	}
	gid, _ := face.GID('A')
	if _, ok := face.ColorBitmapFor(gid, 32); ok {
		t.Error("ColorBitmapFor returned a bitmap for a plain outline glyph")
	}
}

// pickStrike prefers the nearest size, and on a tie the LARGER one: downscaling an
// image loses no detail while upscaling invents it.
func TestPickStrikePrefersNearestThenLarger(t *testing.T) {
	ppems := []float64{16, 32, 64, 128}
	at := func(i int) float64 { return ppems[i] }
	cases := []struct {
		size float64
		want float64
	}{
		{16, 16},
		{30, 32},
		{33, 32},
		{48, 64},   // equidistant between 32 and 64 -> the larger
		{200, 128}, // past the largest strike -> the largest
		{4, 16},    // below the smallest -> the smallest
	}
	for _, c := range cases {
		i, ok := pickStrike(len(ppems), c.size, at)
		if !ok {
			t.Fatalf("size %v: pickStrike reported no strike", c.size)
		}
		if got := ppems[i]; got != c.want {
			t.Errorf("size %v picked ppem %v, want %v", c.size, got, c.want)
		}
	}
}

// A malformed bitmap table degrades to "no bitmaps" rather than panicking.
func TestMalformedBitmapTablesDegrade(t *testing.T) {
	if _, ok := parseSbix(nil, 10); ok {
		t.Error("parseSbix accepted an empty table")
	}
	if _, ok := parseSbix([]byte{0, 1, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}, 10); ok {
		t.Error("parseSbix accepted a strike count past the table end")
	}
	if _, ok := parseCBLC(nil, nil); ok {
		t.Error("parseCBLC accepted empty tables")
	}
	if _, ok := parseCBLC([]byte{0, 3, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}, []byte{0, 0, 0, 0}); ok {
		t.Error("parseCBLC accepted a strike count past the table end")
	}
}
