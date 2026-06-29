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

// buildSFNT reassembles a valid sfnt (TrueType/OpenType) byte stream from decoded
// tables: an offset table, a tag-sorted table directory with correct offsets and
// checksums, and the 4-byte-aligned table data. flavor is the sfnt version tag
// (0x00010000 for TrueType, "OTTO" for CFF). This is the common tail both the
// WOFF1 and WOFF2 decoders feed their decoded tables into.
func buildSFNT(flavor uint32, tables []sfntTable) []byte {
	sort.Slice(tables, func(i, j int) bool {
		return binary.BigEndian.Uint32(tables[i].tag[:]) < binary.BigEndian.Uint32(tables[j].tag[:])
	})
	n := len(tables)
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
	offsets := make([]int, n)
	for i := range tables {
		offsets[i] = offset
		offset += len(tables[i].data)
		offset = (offset + 3) &^ 3
	}
	total := offset

	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:], flavor)
	binary.BigEndian.PutUint16(buf[4:], uint16(n))
	binary.BigEndian.PutUint16(buf[6:], searchRange)
	binary.BigEndian.PutUint16(buf[8:], entrySelector)
	binary.BigEndian.PutUint16(buf[10:], rangeShift)
	for i, t := range tables {
		rec := 12 + 16*i
		copy(buf[rec:rec+4], t.tag[:])
		binary.BigEndian.PutUint32(buf[rec+4:], tableChecksum(t.data))
		binary.BigEndian.PutUint32(buf[rec+8:], uint32(offsets[i]))
		binary.BigEndian.PutUint32(buf[rec+12:], uint32(len(t.data)))
		copy(buf[offsets[i]:], t.data)
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
