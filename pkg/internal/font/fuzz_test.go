package font

import (
	"encoding/binary"
	"testing"

	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// FuzzLoadSFNT exercises the font loader: the table directory, then whichever
// per-table parsers the face needs (cmap, head, hhea, hmtx, loca, glyf, CFF,
// COLR/CPAL, and the collection header for a .ttc).
//
// A font file is a binary format full of offsets and counts that index other
// tables, which is the classic shape for out-of-bounds reads and
// allocation-from-a-file-number. The property under test is that LoadSFNT
// returns: any error is fine, a panic is not.
//
// The face is exercised after loading, not just parsed, because much of the
// table work is lazy -- a defect that only fires when a glyph is actually read
// would otherwise hide behind a successful load.
func FuzzLoadSFNT(f *testing.F) {
	// Real fonts give the mutator valid structure to corrupt: a TrueType
	// (glyf/loca) and an OpenType (CFF) exercise different halves of the loader.
	f.Add(gen.RobotoTTF())
	f.Add(gen.SourceSansOTF())
	for _, s := range sfntSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		face, err := LoadSFNT(data)
		if err != nil || face == nil {
			return
		}
		// Touch the lazy paths. Glyph indices come from the file's own tables,
		// so a small sweep covers the in-range and out-of-range cases both.
		for _, r := range []rune{'A', 'g', '0', ' ', 0x4E2D, 0x1F600} {
			_, _, _ = face.Glyph(r)
			_, _ = face.GID(r)
		}
		for gid := range 64 {
			_ = face.Outline(uint16(gid))
			_ = face.GlyphAdvance(uint16(gid))
			_ = face.GlyphName(uint16(gid))
			_, _ = face.ColorLayers(uint16(gid))
			_, _ = face.ColorBitmapFor(uint16(gid), 12)
		}
		_, _ = face.FamilyName()
		_ = face.UnitsPerEm()
		_, _ = face.ProgramBytes()
	})
}

// sfntSeeds returns hand-built font files aimed at the header and directory,
// which a mutator reaches slowly from a valid font because every offset is
// checked against a length it also has to keep consistent.
func sfntSeeds() [][]byte {
	var out [][]byte

	// A directory claiming far more tables than the data can hold: numTables is
	// a count taken straight from the file and used to size the read.
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint32(hdr[0:], 0x00010000) // TrueType sfnt version
	binary.BigEndian.PutUint16(hdr[4:], 0xFFFF)     // numTables
	out = append(out, hdr)

	// The same, with an OpenType/CFF tag.
	otto := make([]byte, 12)
	copy(otto[0:], "OTTO")
	binary.BigEndian.PutUint16(otto[4:], 0xFFFF)
	out = append(out, otto)

	// A TrueType collection header claiming a huge font count.
	ttc := make([]byte, 16)
	copy(ttc[0:], "ttcf")
	binary.BigEndian.PutUint32(ttc[4:], 0x00010000)
	binary.BigEndian.PutUint32(ttc[8:], 0xFFFFFFFF) // numFonts
	out = append(out, ttc)

	// One well-formed table entry whose offset and length point far past EOF.
	far := make([]byte, 12+16)
	binary.BigEndian.PutUint32(far[0:], 0x00010000)
	binary.BigEndian.PutUint16(far[4:], 1)
	copy(far[12:], "head")
	binary.BigEndian.PutUint32(far[12+8:], 0x7FFFFFFF)  // offset
	binary.BigEndian.PutUint32(far[12+12:], 0x7FFFFFFF) // length
	out = append(out, far)

	// Degenerates: too short to hold a directory at all.
	out = append(out,
		[]byte{},
		[]byte{0x00},
		[]byte("OTTO"),
		[]byte("not a font"),
	)
	return out
}
