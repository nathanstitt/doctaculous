package font

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// TestLoadSFNTMalformedDoesNotPanic covers font programs that make the upstream
// parser index its own tables out of range.
//
// The panic is in the dependency, not here: textlayout's parseCmapFormat4
// computes a slice bound from the subtable's own length fields and slices at a
// NEGATIVE index when they disagree. Found by fuzzing, as
// `slice bounds out of range [-390:]`.
//
// It matters because a font program is untrusted document input — embedded in a
// PDF, or fetched as a web font — and this panic is raised during OPEN, before
// any per-page recover exists. The package already recovers around
// FontHExtents/FontVExtents for exactly this reason; the parse entry point
// simply lacked the same guard.
func TestLoadSFNTMalformedDoesNotPanic(t *testing.T) {
	for _, c := range malformedFonts() {
		t.Run(c.name, func(t *testing.T) {
			// A panic here fails the test rather than the process. Any outcome
			// is acceptable as long as LoadSFNT returns.
			face, err := LoadSFNT(c.data)
			if err != nil {
				return
			}
			if face == nil {
				t.Fatal("LoadSFNT returned nil face and nil error")
			}
			// Exercise the lazy paths too: much of the table work happens on
			// first use, not at load.
			_, _, _ = face.Glyph('A')
			_ = face.Outline(0)
			_ = face.GlyphAdvance(0)
		})
	}
}

// TestLoadSFNTRealFontsStillLoad guards the recover against hiding a real
// regression: the bundled faces must still parse and report glyphs.
func TestLoadSFNTRealFontsStillLoad(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"Roboto (TrueType/glyf)", gen.RobotoTTF()},
		{"Source Sans (OpenType/CFF)", gen.SourceSansOTF()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			face, err := LoadSFNT(c.data)
			if err != nil {
				t.Fatalf("LoadSFNT: %v", err)
			}
			if _, _, ok := face.Glyph('A'); !ok {
				t.Error("no glyph for 'A' in a real font")
			}
			if face.UnitsPerEm() <= 0 {
				t.Errorf("unitsPerEm = %v, want positive", face.UnitsPerEm())
			}
		})
	}
}

// malformedFonts returns font files whose headers are self-inconsistent: the
// counts and offsets disagree with the data, which is what drives the upstream
// parser off the end of its tables.
func malformedFonts() []struct {
	name string
	data []byte
} {
	type tc = struct {
		name string
		data []byte
	}
	var out []tc

	// A directory claiming far more tables than the file can hold.
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint32(hdr[0:], 0x00010000)
	binary.BigEndian.PutUint16(hdr[4:], 0xFFFF)
	out = append(out, tc{"numTables far past EOF", hdr})

	// An OpenType/CFF directory with the same lie.
	otto := make([]byte, 12)
	copy(otto[0:], "OTTO")
	binary.BigEndian.PutUint16(otto[4:], 0xFFFF)
	out = append(out, tc{"OTTO numTables far past EOF", otto})

	// A collection header claiming a huge font count.
	ttc := make([]byte, 16)
	copy(ttc[0:], "ttcf")
	binary.BigEndian.PutUint32(ttc[4:], 0x00010000)
	binary.BigEndian.PutUint32(ttc[8:], 0xFFFFFFFF)
	out = append(out, tc{"collection numFonts far past EOF", ttc})

	// A table entry whose offset and length point past the end.
	far := make([]byte, 12+16)
	binary.BigEndian.PutUint32(far[0:], 0x00010000)
	binary.BigEndian.PutUint16(far[4:], 1)
	copy(far[12:], "head")
	binary.BigEndian.PutUint32(far[12+8:], 0x7FFFFFFF)
	binary.BigEndian.PutUint32(far[12+12:], 0x7FFFFFFF)
	out = append(out, tc{"table offset past EOF", far})

	// A real font truncated at a series of points: each cut lands mid-table and
	// leaves the directory promising bytes that are no longer there.
	roboto := gen.RobotoTTF()
	for _, frac := range []int{2, 4, 8, 16, 64} {
		n := len(roboto) / frac
		cut := make([]byte, n)
		copy(cut, roboto[:n])
		out = append(out, tc{"Roboto truncated to 1/" + strconv.Itoa(frac), cut})
	}

	// A real font with its cmap table tag corrupted, so the parser looks for a
	// table that is not where the directory says.
	if i := bytes.Index(roboto, []byte("cmap")); i >= 0 {
		bad := make([]byte, len(roboto))
		copy(bad, roboto)
		copy(bad[i:], "cmaq")
		out = append(out, tc{"Roboto with a corrupted cmap tag", bad})
	}

	// Degenerates.
	out = append(out,
		tc{"empty", nil},
		tc{"one byte", []byte{0x00}},
		tc{"tag only", []byte("OTTO")},
		tc{"not a font", []byte(strings.Repeat("x", 4096))},
	)
	return out
}
