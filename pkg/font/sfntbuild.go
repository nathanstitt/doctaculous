package font

import (
	"encoding/binary"
	"sort"
)

// sfntTable is one decoded table: its 4-byte tag and raw (uncompressed,
// untransformed) bytes.
type sfntTable struct {
	tag  [4]byte
	data []byte
}

// buildSFNT reassembles an sfnt from decoded tables; the common tail both the
// WOFF1 and WOFF2 decoders feed their decoded tables into.
func buildSFNT(flavor uint32, tables []sfntTable) []byte {
	// Duplicate tags in a malformed container collapse here (last wins); the old
	// slice-based builder emitted both directory entries.
	m := make(map[string][]byte, len(tables))
	for _, t := range tables {
		m[string(t.tag[:])] = t.data
	}
	return BuildSFNT(flavor, m)
}

// BuildSFNT reassembles a valid sfnt (TrueType/OpenType) byte stream from a
// tag->bytes table map: an offset table, a tag-sorted table directory with
// correct offsets and checksums, and the 4-byte-aligned table data. flavor is
// the sfnt version tag (0x00010000 for TrueType, "OTTO" for CFF). This is the
// one SFNT assembler used by the WOFF1/WOFF2 decoders and the PDF glyf
// subsetter.
func BuildSFNT(flavor uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	n := len(tags)
	// searchRange = (largest power of 2 <= n) * 16; entrySelector = log2 of that
	// power; rangeShift = n*16 - searchRange. (OpenType offset-table fields.)
	pow2, exp := 1, 0
	for pow2*2 <= n {
		pow2 *= 2
		exp++
	}
	searchRange := uint16(pow2 * 16)
	entrySelector := uint16(exp)
	rangeShift := uint16(n*16) - searchRange

	headerLen := 12 + 16*n
	offset := headerLen
	// 4-byte-align each table's start; record padded offsets.
	offsets := make(map[string]int, n)
	for _, tag := range tags {
		offsets[tag] = offset
		offset += len(tables[tag])
		offset = (offset + 3) &^ 3
	}
	total := offset

	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:], flavor)
	binary.BigEndian.PutUint16(buf[4:], uint16(n))
	binary.BigEndian.PutUint16(buf[6:], searchRange)
	binary.BigEndian.PutUint16(buf[8:], entrySelector)
	binary.BigEndian.PutUint16(buf[10:], rangeShift)
	for i, tag := range tags {
		rec := 12 + 16*i
		copy(buf[rec:rec+4], tag)
		binary.BigEndian.PutUint32(buf[rec+4:], tableChecksum(tables[tag]))
		binary.BigEndian.PutUint32(buf[rec+8:], uint32(offsets[tag]))
		binary.BigEndian.PutUint32(buf[rec+12:], uint32(len(tables[tag])))
		copy(buf[offsets[tag]:], tables[tag])
	}
	return buf
}

// tableChecksum is the sum of the table's 32-bit big-endian words, zero-padded to
// a 4-byte boundary (OpenType table checksum). The parser does not validate these,
// but a well-formed directory carries them.
func tableChecksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(b); i += 4 {
		sum += binary.BigEndian.Uint32(b[i:])
	}
	if rem := len(b) % 4; rem != 0 {
		var tail [4]byte
		copy(tail[:], b[len(b)-rem:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}
