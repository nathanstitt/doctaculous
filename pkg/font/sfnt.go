package font

import (
	"encoding/binary"
	"fmt"
)

// sfnt version tags / WOFF signatures (the first 4 bytes of the container).
const (
	sigTrueType = 0x00010000
	sigTrue     = 0x74727565 // "true"
	sigOTTO     = 0x4F54544F // "OTTO"
	sigTTCF     = 0x74746366 // "ttcf" (TrueType Collection)
	sigWOFF     = 0x774F4646 // "wOFF"
	sigWOFF2    = 0x774F4632 // "wOF2"
)

// LoadSFNT builds a reflow Face from a font file's bytes, transparently unwrapping
// a WOFF1 or WOFF2 container to its sfnt tables first. Raw sfnt (TrueType/OpenType)
// is parsed directly. It returns a typed error for an unrecognized or malformed
// container so the caller (the face cache) falls back to a bundled substitute.
func LoadSFNT(data []byte) (*Face, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: too short", ErrInvalidWOFF)
	}
	sig := binary.BigEndian.Uint32(data[:4])
	var sfnt []byte
	switch sig {
	case sigTrueType, sigTrue, sigOTTO, sigTTCF:
		sfnt = data
	case sigWOFF:
		b, err := decodeWOFF1(data)
		if err != nil {
			return nil, err
		}
		sfnt = b
	case sigWOFF2:
		b, err := decodeWOFF2(data)
		if err != nil {
			return nil, err
		}
		sfnt = b
	default:
		return nil, fmt.Errorf("%w: unrecognized signature 0x%08x", ErrUnsupportedFontProgram, sig)
	}
	prog, err := parseProgram(sfnt, progTrueType)
	if err != nil {
		return nil, err
	}
	// Retain the (decompressed) sfnt bytes for PDF embedding. A CFF-flavored sfnt
	// (an OTTO container with a "CFF " table) embeds as /FontFile3 OpenType, a
	// glyf-flavored one as /FontFile2; sniff the table directory to tell them apart.
	kind := ProgramKindTrueType
	if sfntHasTable(sfnt, "CFF ") {
		kind = ProgramKindCFF
	}
	return &Face{prog: prog, names: prog.nameToGID(), progData: sfnt, progKind: kind}, nil
}

// sfntHasTable reports whether the sfnt table directory in data declares a table
// with the 4-byte tag. It is tolerant of a short/malformed directory (returns
// false rather than panicking).
func sfntHasTable(data []byte, tag string) bool {
	if len(data) < 12 || len(tag) != 4 {
		return false
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	const dirStart = 12
	const recSize = 16 // tag(4) + checksum(4) + offset(4) + length(4)
	for i := 0; i < numTables; i++ {
		off := dirStart + i*recSize
		if off+4 > len(data) {
			return false
		}
		if string(data[off:off+4]) == tag {
			return true
		}
	}
	return false
}
