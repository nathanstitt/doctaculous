package font

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// woff2KnownTags is the WOFF2 known-table tag list, indexed by the 6-bit flag
// value. Index 63 (0x3f) means a 4-byte custom tag follows instead. Order is
// normative — copy exactly from the W3C WOFF2 spec "Known Table Tags".
var woff2KnownTags = [...][4]byte{
	{'c', 'm', 'a', 'p'}, {'h', 'e', 'a', 'd'}, {'h', 'h', 'e', 'a'}, {'h', 'm', 't', 'x'},
	{'m', 'a', 'x', 'p'}, {'n', 'a', 'm', 'e'}, {'O', 'S', '/', '2'}, {'p', 'o', 's', 't'},
	{'c', 'v', 't', ' '}, {'f', 'p', 'g', 'm'}, {'g', 'l', 'y', 'f'}, {'l', 'o', 'c', 'a'},
	{'p', 'r', 'e', 'p'}, {'C', 'F', 'F', ' '}, {'V', 'O', 'R', 'G'}, {'E', 'B', 'D', 'T'},
	{'E', 'B', 'L', 'C'}, {'g', 'a', 's', 'p'}, {'h', 'd', 'm', 'x'}, {'k', 'e', 'r', 'n'},
	{'L', 'T', 'S', 'H'}, {'P', 'C', 'L', 'T'}, {'V', 'D', 'M', 'X'}, {'v', 'h', 'e', 'a'},
	{'v', 'm', 't', 'x'}, {'B', 'A', 'S', 'E'}, {'G', 'D', 'E', 'F'}, {'G', 'P', 'O', 'S'},
	{'G', 'S', 'U', 'B'}, {'E', 'B', 'S', 'C'}, {'J', 'S', 'T', 'F'}, {'M', 'A', 'T', 'H'},
	{'C', 'B', 'D', 'T'}, {'C', 'B', 'L', 'C'}, {'C', 'O', 'L', 'R'}, {'C', 'P', 'A', 'L'},
	{'S', 'V', 'G', ' '}, {'s', 'b', 'i', 'x'}, {'a', 'c', 'n', 't'}, {'a', 'v', 'a', 'r'},
	{'b', 'd', 'a', 't'}, {'b', 'l', 'o', 'c'}, {'b', 's', 'l', 'n'}, {'c', 'v', 'a', 'r'},
	{'f', 'd', 's', 'c'}, {'f', 'e', 'a', 't'}, {'f', 'm', 't', 'x'}, {'f', 'v', 'a', 'r'},
	{'g', 'v', 'a', 'r'}, {'h', 's', 't', 'y'}, {'j', 'u', 's', 't'}, {'l', 'c', 'a', 'r'},
	{'m', 'o', 'r', 't'}, {'m', 'o', 'r', 'x'}, {'o', 'p', 'b', 'd'}, {'p', 'r', 'o', 'p'},
	{'t', 'r', 'a', 'k'}, {'Z', 'a', 'p', 'f'}, {'S', 'i', 'l', 'f'}, {'G', 'l', 'a', 't'},
	{'G', 'l', 'o', 'c'}, {'F', 'e', 'a', 't'}, {'S', 'i', 'l', 'l'},
}

// woff2Entry is one parsed WOFF2 directory entry before its data is sliced from
// the decompressed block.
type woff2Entry struct {
	tag         [4]byte
	transformed bool
	origLength  uint32
	transLength uint32 // valid only when transformed
}

// decodeWOFF2 unwraps a WOFF2 container to sfnt bytes: parse the header + compact
// directory, Brotli-decompress the single table block, then for each table either
// pass it through or (glyf/loca) reverse the transform, and reassemble via
// buildSFNT. Layout per the W3C WOFF2 spec.
func decodeWOFF2(data []byte) ([]byte, error) {
	// WOFF2Header (48 bytes): signature(4) flavor(4) length(4) numTables(2)
	// reserved(2) totalSfntSize(4) totalCompressedSize(4) major(2) minor(2)
	// metaOffset(4) metaLength(4) metaOrigLength(4) privOffset(4) privLength(4).
	if len(data) < 48 {
		return nil, fmt.Errorf("%w: WOFF2 header truncated", ErrInvalidWOFF)
	}
	if binary.BigEndian.Uint32(data[0:]) != sigWOFF2 {
		return nil, fmt.Errorf("%w: bad WOFF2 signature", ErrInvalidWOFF)
	}
	flavor := binary.BigEndian.Uint32(data[4:])
	numTables := int(binary.BigEndian.Uint16(data[12:]))
	totalCompressed := binary.BigEndian.Uint32(data[20:])

	pos := 48
	entries := make([]woff2Entry, numTables)
	for i := 0; i < numTables; i++ {
		if pos >= len(data) {
			return nil, fmt.Errorf("%w: WOFF2 directory truncated", ErrInvalidWOFF)
		}
		flags := data[pos]
		pos++
		tagIdx := flags & 0x3f
		var tag [4]byte
		if tagIdx == 0x3f {
			if pos+4 > len(data) {
				return nil, fmt.Errorf("%w: WOFF2 custom tag truncated", ErrInvalidWOFF)
			}
			copy(tag[:], data[pos:pos+4])
			pos += 4
		} else {
			if int(tagIdx) >= len(woff2KnownTags) {
				return nil, fmt.Errorf("%w: WOFF2 bad known-tag index %d", ErrInvalidWOFF, tagIdx)
			}
			tag = woff2KnownTags[tagIdx]
		}
		transformVersion := (flags >> 6) & 0x3
		origLen, n, err := readUIntBase128(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
		e := woff2Entry{tag: tag, origLength: origLen}
		isGlyfOrLoca := tag == woff2KnownTags[10] || tag == woff2KnownTags[11] // glyf, loca
		if isGlyfOrLoca {
			e.transformed = transformVersion == 0
		} else {
			e.transformed = transformVersion != 0
		}
		if e.transformed {
			transLen, n, err := readUIntBase128(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += n
			e.transLength = transLen
		}
		entries[i] = e
	}

	if pos+int(totalCompressed) > len(data) {
		return nil, fmt.Errorf("%w: WOFF2 compressed block out of range", ErrInvalidWOFF)
	}
	br := brotli.NewReader(bytes.NewReader(data[pos : pos+int(totalCompressed)]))
	block, err := io.ReadAll(br)
	if err != nil {
		return nil, fmt.Errorf("%w: WOFF2 brotli: %v", ErrInvalidWOFF, err)
	}

	tables, err := reconstructWOFF2Tables(flavor, entries, block)
	if err != nil {
		return nil, err
	}
	return buildSFNT(flavor, tables), nil
}

// readUIntBase128 decodes a WOFF2 UIntBase128 value (1-5 big-endian base-128
// groups, high bit = continuation). Returns the value and bytes consumed.
func readUIntBase128(b []byte) (uint32, int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		if i >= len(b) {
			return 0, 0, fmt.Errorf("%w: UIntBase128 truncated", ErrInvalidWOFF)
		}
		c := b[i]
		if i == 0 && c == 0x80 {
			return 0, 0, fmt.Errorf("%w: UIntBase128 leading zero", ErrInvalidWOFF)
		}
		if v > (0xFFFFFFFF >> 7) {
			return 0, 0, fmt.Errorf("%w: UIntBase128 overflow", ErrInvalidWOFF)
		}
		v = (v << 7) | uint32(c&0x7f)
		if c&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("%w: UIntBase128 too long", ErrInvalidWOFF)
}

// reconstructWOFF2Tables slices each table from the decompressed block in
// directory order. (Task 6 replaces this with glyf/loca transform reconstruction.)
func reconstructWOFF2Tables(flavor uint32, entries []woff2Entry, block []byte) ([]sfntTable, error) {
	_ = flavor // flavor is used by the Task 6 transform-aware version
	tables := make([]sfntTable, 0, len(entries))
	off := 0
	for _, e := range entries {
		if e.transformed {
			return nil, fmt.Errorf("%w: WOFF2 transformed %q not yet supported", ErrInvalidWOFF, e.tag)
		}
		end := off + int(e.origLength)
		if end > len(block) {
			return nil, fmt.Errorf("%w: WOFF2 table %q out of range", ErrInvalidWOFF, e.tag)
		}
		tables = append(tables, sfntTable{tag: e.tag, data: append([]byte(nil), block[off:end]...)})
		off = end
	}
	return tables, nil
}
