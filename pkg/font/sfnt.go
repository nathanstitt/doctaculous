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
	return &Face{prog: prog, names: prog.nameToGID()}, nil
}

// decodeWOFF1 is replaced by woff1.go in Task 4. It will decode the WOFF1 table
// directory into []sfntTable and feed them to buildSFNT; until then it is a stub
// that returns a typed error. (The buildSFNT reference keeps the shared reassembly
// seam wired in for Tasks 4/6.)
func decodeWOFF1(data []byte) ([]byte, error) {
	_ = buildSFNT
	return nil, fmt.Errorf("%w: WOFF1 decode not yet implemented", ErrInvalidWOFF)
}

// decodeWOFF2 is replaced by woff2.go in Task 6. It will decode (and de-transform)
// the WOFF2 tables into []sfntTable and feed them to buildSFNT; until then it is a
// stub that returns a typed error. (The buildSFNT reference keeps the shared
// reassembly seam wired in for Tasks 4/6.)
func decodeWOFF2(data []byte) ([]byte, error) {
	_ = buildSFNT
	return nil, fmt.Errorf("%w: WOFF2 decode not yet implemented", ErrInvalidWOFF)
}
