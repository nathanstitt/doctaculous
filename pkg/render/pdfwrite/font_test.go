package pdfwrite

import (
	"bytes"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

// serializeWithFont emits fe's face tree, keeps it reachable, and serializes.
func serializeWithFont(t *testing.T, w *writer, fontRef Ref) []byte {
	t.Helper()
	holder := w.alloc()
	w.put(holder, Dict{"X": fontRef})
	root := w.alloc()
	w.put(root, Dict{"Type": Name("Catalog")})
	w.setRoot(root)
	var buf bytes.Buffer
	if err := w.serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return buf.Bytes()
}

// TestEmbedFontProducesType0AndToUnicode checks the TrueType embed path emits a
// Type0/Identity-H CID font tree with a FontFile2 program and a ToUnicode CMap.
func TestEmbedFontProducesType0AndToUnicode(t *testing.T) {
	face, ok := font.LoadStandard("Courier", font.Style{}) // Courier -> Inconsolata (TrueType)
	if !ok {
		t.Fatal("no Courier")
	}
	if _, kind := face.ProgramBytes(); kind != font.ProgramKindTrueType {
		t.Fatalf("expected TrueType face, got kind %v", kind)
	}
	w := newWriter()
	fe := newFontEmbedder()
	for _, r := range []rune{'A', 'B'} {
		gid, _ := face.GID(r)
		code, embedded := fe.use(face, gid, []rune{r})
		if !embedded {
			t.Fatalf("TrueType glyph %d not embeddable", gid)
		}
		if uint16(code) != gid {
			t.Errorf("TrueType code = %d; want GID %d", code, gid)
		}
	}
	fontRef := fe.emit(w, face)
	if fontRef == 0 {
		t.Fatal("emit returned 0 for a TrueType face")
	}
	out := serializeWithFont(t, w, fontRef)
	for _, want := range []string{"/Type0", "/Identity-H", "/CIDFontType2", "/ToUnicode", "/FontFile2"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestEmbedType1ProducesSimpleFontFile checks the classic Type1 embed path (the
// default Helvetica/Times substitutes are Type1 PFB) emits a simple /Type1 font
// with a /FontFile carrying Length1/Length2/Length3, an /Encoding /Differences, and
// a /ToUnicode, so default-font text is real, searchable text.
func TestEmbedType1ProducesSimpleFontFile(t *testing.T) {
	face, ok := font.LoadStandard("Helvetica", font.Style{}) // -> TeX Gyre Heros (Type1)
	if !ok {
		t.Fatal("no Helvetica")
	}
	if _, kind := face.ProgramBytes(); kind != font.ProgramKindType1 {
		t.Fatalf("expected Type1 face, got kind %v", kind)
	}
	w := newWriter()
	fe := newFontEmbedder()
	for _, r := range []rune{'H', 'i'} {
		gid, _ := face.GID(r)
		code, embedded := fe.use(face, gid, []rune{r})
		if !embedded {
			t.Fatalf("Type1 glyph %d not embeddable", gid)
		}
		if code > 255 {
			t.Errorf("Type1 code = %d; want a single byte", code)
		}
	}
	fontRef := fe.emit(w, face)
	if fontRef == 0 {
		t.Fatal("emit returned 0 for a Type1 face")
	}
	out := serializeWithFont(t, w, fontRef)
	for _, want := range []string{"/Subtype /Type1", "/ToUnicode", "/FontFile ", "/Length1", "/Length2", "/Length3", "/Differences", "/Widths"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestType1OverflowFallsBack asserts that beyond 256 glyphs a Type1 face reports
// the glyph as not embeddable (the device then paints an outline fill).
func TestType1Overflow(t *testing.T) {
	face, _ := font.LoadStandard("Helvetica", font.Style{})
	fe := newFontEmbedder()
	// Assign 256 distinct fake GIDs; the 257th must overflow.
	for gid := uint16(1); gid <= 256; gid++ {
		if _, ok := fe.use(face, gid, []rune{rune(gid)}); !ok {
			t.Fatalf("glyph %d unexpectedly not embeddable within the 256 cap", gid)
		}
	}
	if _, ok := fe.use(face, 9999, []rune{'z'}); ok {
		t.Fatal("257th distinct Type1 glyph should overflow (not embeddable)")
	}
}

// TestSubsetRetainsUsedGlyphsOnly checks the TrueType subsetter shrinks the program
// while keeping it a loadable SFNT (glyph indices preserved via glyf zeroing).
func TestSubsetRetainsUsedGlyphsOnly(t *testing.T) {
	face, _ := font.LoadStandard("Courier", font.Style{}) // Inconsolata (TrueType)
	data, kind := face.ProgramBytes()
	if kind != font.ProgramKindTrueType {
		t.Skip("bundled monospace is not TrueType")
	}
	gidA, _ := face.GID('A')
	gidB, _ := face.GID('B')

	sub, err := subsetTrueType(data, []uint16{gidA, gidB})
	if err != nil {
		t.Fatalf("subsetTrueType: %v", err)
	}
	if len(sub) == 0 || len(sub) >= len(data) {
		t.Fatalf("subset size %d not smaller than original %d", len(sub), len(data))
	}
	// The subset must still be a parseable SFNT the font package can load, and 'A'
	// must still resolve to its outline (indices preserved).
	f2, err := font.LoadSFNT(sub)
	if err != nil {
		t.Fatalf("subset not loadable: %v", err)
	}
	if f2.Outline(gidA) == nil {
		t.Fatal("subset dropped the retained glyph 'A'")
	}
}

// TestPFBToType1 checks the PFB decoder splits the bundled substitute into the
// three Type1 segments with sane lengths.
func TestPFBToType1(t *testing.T) {
	face, _ := font.LoadStandard("Helvetica", font.Style{})
	data, _ := face.ProgramBytes()
	prog, l1, l2, l3, err := pfbToType1(data)
	if err != nil {
		t.Fatalf("pfbToType1: %v", err)
	}
	if l1 <= 0 || l2 <= 0 || l3 <= 0 {
		t.Fatalf("segment lengths: L1=%d L2=%d L3=%d; all should be > 0", l1, l2, l3)
	}
	if len(prog) != l1+l2+l3 {
		t.Fatalf("program length %d != L1+L2+L3 (%d)", len(prog), l1+l2+l3)
	}
	if !bytes.HasPrefix(prog, []byte("%!")) {
		t.Fatalf("Type1 program should start with %%! header")
	}
}
