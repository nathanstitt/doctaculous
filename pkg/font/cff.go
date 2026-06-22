package font

import (
	"encoding/binary"
	"errors"
)

// wrapBareCFF wraps a bare CFF table (a PDF FontFile3 with /Subtype Type1C or
// CIDFontType0C) in a minimal OpenType ("OTTO") container so that
// golang.org/x/image/font/sfnt can parse it. sfnt only accepts SFNT containers
// and locates a font's glyph data, metrics, and character map through SFNT
// tables; a bare CFF carries none of those, so we synthesize the smallest set
// sfnt requires: head, maxp, hhea, hmtx, post, cmap, plus the CFF table itself.
//
// Glyph access for CFF-based PDF fonts is by glyph index (Type0/Identity or a
// simple font's code→GID mapping resolved elsewhere), so the synthesized cmap
// need only be well-formed, not meaningful.
func wrapBareCFF(cff []byte) ([]byte, error) {
	numGlyphs, err := cffNumGlyphs(cff)
	if err != nil {
		return nil, err
	}

	head := synthHead()
	maxp := synthMaxp(numGlyphs)
	hhea := synthHhea()
	hmtx := synthHmtx()
	post := synthPost()
	cmap := synthCmap()

	// Table records must be sorted ascending by 4-byte tag (sfnt enforces this).
	tables := []otTable{
		{tag: "CFF ", data: cff},
		{tag: "cmap", data: cmap},
		{tag: "head", data: head},
		{tag: "hhea", data: hhea},
		{tag: "hmtx", data: hmtx},
		{tag: "maxp", data: maxp},
		{tag: "post", data: post},
	}
	return assembleOTTO(tables), nil
}

// otTable is one table to place in the synthesized container.
type otTable struct {
	tag  string // exactly 4 bytes
	data []byte
}

// assembleOTTO builds an OTTO SFNT container from the given tables. Tables must
// already be sorted ascending by tag. Each table is padded to a 4-byte boundary
// (sfnt requires every table to begin on a four-byte boundary); checksums are
// left zero, which sfnt ignores.
func assembleOTTO(tables []otTable) []byte {
	const offsetTableSize = 12
	const recordSize = 16
	n := len(tables)

	// Compute each table's offset, accounting for 4-byte alignment.
	offset := offsetTableSize + recordSize*n
	offsets := make([]int, n)
	for i, t := range tables {
		offsets[i] = offset
		offset += align4(len(t.data))
	}
	total := offset

	out := make([]byte, total)
	// Offset table: sfntVersion "OTTO", numTables, and the binary-search fields
	// (searchRange, entrySelector, rangeShift) which sfnt does not validate.
	copy(out[0:4], "OTTO")
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	sr, es, rs := searchParams(n)
	binary.BigEndian.PutUint16(out[6:], sr)
	binary.BigEndian.PutUint16(out[8:], es)
	binary.BigEndian.PutUint16(out[10:], rs)

	// Table records.
	for i, t := range tables {
		rec := out[offsetTableSize+recordSize*i:]
		copy(rec[0:4], t.tag)
		// rec[4:8] checksum left zero (ignored by sfnt).
		binary.BigEndian.PutUint32(rec[8:], uint32(offsets[i]))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(t.data)))
		copy(out[offsets[i]:], t.data)
	}
	return out
}

func align4(n int) int { return (n + 3) &^ 3 }

// searchParams returns the searchRange/entrySelector/rangeShift offset-table
// fields for numTables. sfnt ignores them, but well-formed values are cheap.
func searchParams(numTables int) (searchRange, entrySelector, rangeShift uint16) {
	es := 0
	pow := 1
	for pow*2 <= numTables {
		pow *= 2
		es++
	}
	searchRange = uint16(pow * 16)
	entrySelector = uint16(es)
	rangeShift = uint16(numTables*16) - searchRange
	return
}

// synthHead builds a 54-byte head table. Only unitsPerEm (offset 18),
// indexToLocFormat (offset 50), and the glyph bounds (offset 36) are read by
// sfnt; the rest is left zero.
func synthHead() []byte {
	b := make([]byte, 54)
	binary.BigEndian.PutUint32(b[0:], 0x00010000) // version 1.0
	binary.BigEndian.PutUint16(b[18:], 1000)      // unitsPerEm
	// glyph bounds (xMin,yMin,xMax,yMax) at 36..44 left zero; loca format at 50 zero.
	return b
}

// synthMaxp builds a 6-byte (PostScript/CFF) maxp table declaring numGlyphs.
// This must equal the CFF's CharStrings count or sfnt rejects the font.
func synthMaxp(numGlyphs int) []byte {
	b := make([]byte, 6)
	binary.BigEndian.PutUint32(b[0:], 0x00005000) // version 0.5
	binary.BigEndian.PutUint16(b[4:], uint16(numGlyphs))
	return b
}

// synthHhea builds a 36-byte hhea table with numberOfHMetrics = 1 (offset 34),
// matching the single record in the synthesized hmtx.
func synthHhea() []byte {
	b := make([]byte, 36)
	binary.BigEndian.PutUint32(b[0:], 0x00010000) // version 1.0
	binary.BigEndian.PutUint16(b[34:], 1)         // numberOfHMetrics
	return b
}

// synthHmtx builds a 4-byte hmtx table: one longHorMetric (advanceWidth,
// leftSideBearing). sfnt only validates the length against numberOfHMetrics.
func synthHmtx() []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:], 1000) // advanceWidth (unused; PDF widths win)
	return b
}

// synthPost builds a 32-byte version 3.0 post table (no glyph-name data).
func synthPost() []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint32(b[0:], 0x00030000) // version 3.0
	return b
}

// synthCmap builds a minimal cmap with a single (3,1) format-4 subtable that
// maps no characters (one terminating segment). It exists only so sfnt's
// mandatory cmap parse finds a supported subtable; CFF glyphs are reached by
// index, not by this map.
func synthCmap() []byte {
	// Format 4 subtable with segCountX2 = 2 (one segment: the required final
	// 0xFFFF..0xFFFF terminator). Layout: header(14) + endCode(2) + reservedPad(2)
	// + startCode(2) + idDelta(2) + idRangeOffset(2) = 24 bytes.
	const subLen = 24
	sub := make([]byte, subLen)
	binary.BigEndian.PutUint16(sub[0:], 4)       // format
	binary.BigEndian.PutUint16(sub[2:], subLen)  // length
	binary.BigEndian.PutUint16(sub[4:], 0)       // language
	binary.BigEndian.PutUint16(sub[6:], 2)       // segCountX2 = 2*1
	binary.BigEndian.PutUint16(sub[8:], 2)       // searchRange
	binary.BigEndian.PutUint16(sub[10:], 0)      // entrySelector
	binary.BigEndian.PutUint16(sub[12:], 0)      // rangeShift
	binary.BigEndian.PutUint16(sub[14:], 0xFFFF) // endCode[0]
	binary.BigEndian.PutUint16(sub[16:], 0)      // reservedPad
	binary.BigEndian.PutUint16(sub[18:], 0xFFFF) // startCode[0]
	binary.BigEndian.PutUint16(sub[20:], 1)      // idDelta[0]
	binary.BigEndian.PutUint16(sub[22:], 0)      // idRangeOffset[0]

	// cmap header: version(2) + numTables(2) + one encoding record(8).
	const hdr = 4 + 8
	out := make([]byte, hdr+subLen)
	binary.BigEndian.PutUint16(out[0:], 0) // version
	binary.BigEndian.PutUint16(out[2:], 1) // numTables
	binary.BigEndian.PutUint16(out[4:], 3) // platformID = Windows
	binary.BigEndian.PutUint16(out[6:], 1) // encodingID = Unicode BMP
	binary.BigEndian.PutUint32(out[8:], hdr)
	copy(out[hdr:], sub)
	return out
}

// errInvalidCFF is returned when a bare CFF table cannot be parsed. The CFF
// INDEX/DICT parsing itself lives in cff_charset.go.
var errInvalidCFF = errors.New("font: invalid CFF program")
