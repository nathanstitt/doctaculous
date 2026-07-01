package pdfwrite

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// subsetTrueType returns an SFNT containing only the glyph programs for keep (plus
// composite dependencies and .notdef), with glyph indices preserved so a Type0
// font's Identity CIDToGIDMap stays valid. Unused glyphs become zero-length in the
// rewritten glyf/loca; every other table is copied verbatim. It returns an error if
// data is not a parseable glyf-flavored SFNT (the caller then embeds the whole
// program).
func subsetTrueType(data []byte, keep []uint16) ([]byte, error) {
	tables, flavor, err := parseSFNTTables(data)
	if err != nil {
		return nil, err
	}
	head, ok := tables["head"]
	if !ok {
		return nil, fmt.Errorf("pdfwrite: subset: no head table")
	}
	glyf, ok := tables["glyf"]
	if !ok {
		return nil, fmt.Errorf("pdfwrite: subset: no glyf table (not glyf-flavored)")
	}
	locaBytes, ok := tables["loca"]
	if !ok {
		return nil, fmt.Errorf("pdfwrite: subset: no loca table")
	}
	if len(head) < 52 {
		return nil, fmt.Errorf("pdfwrite: subset: short head table")
	}
	longLoca := binary.BigEndian.Uint16(head[50:52]) == 1

	loca, err := parseLoca(locaBytes, longLoca)
	if err != nil {
		return nil, err
	}
	numGlyphs := len(loca) - 1
	if numGlyphs <= 0 {
		return nil, fmt.Errorf("pdfwrite: subset: empty loca")
	}

	// Compute the retained set: requested GIDs ∪ composite components (transitively)
	// ∪ .notdef.
	retained := map[int]bool{0: true}
	var stack []int
	add := func(g int) {
		if g >= 0 && g < numGlyphs && !retained[g] {
			retained[g] = true
			stack = append(stack, g)
		}
	}
	for _, g := range keep {
		add(int(g))
	}
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, comp := range compositeComponents(glyf, loca, g) {
			add(comp)
		}
	}

	// Rewrite glyf keeping only retained glyph programs; build a new loca.
	newGlyf := make([]byte, 0, len(glyf))
	newLoca := make([]uint32, numGlyphs+1)
	for g := 0; g < numGlyphs; g++ {
		newLoca[g] = uint32(len(newGlyf))
		if retained[g] {
			start, end := loca[g], loca[g+1]
			if end > start && int(end) <= len(glyf) {
				newGlyf = append(newGlyf, glyf[start:end]...)
			}
		}
		// unused glyphs contribute nothing (zero-length entry)
	}
	newLoca[numGlyphs] = uint32(len(newGlyf))

	tables["glyf"] = newGlyf
	tables["loca"] = encodeLoca(newLoca, longLoca)

	return buildSFNTBytes(flavor, tables), nil
}

// parseSFNTTables reads the offset table and table directory into a tag->bytes map.
func parseSFNTTables(data []byte) (map[string][]byte, uint32, error) {
	if len(data) < 12 {
		return nil, 0, fmt.Errorf("pdfwrite: subset: short sfnt")
	}
	flavor := binary.BigEndian.Uint32(data[0:4])
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	tables := make(map[string][]byte, numTables)
	for i := 0; i < numTables; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil, 0, fmt.Errorf("pdfwrite: subset: truncated table directory")
		}
		tag := string(data[rec : rec+4])
		off := int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		length := int(binary.BigEndian.Uint32(data[rec+12 : rec+16]))
		if off < 0 || length < 0 || off+length > len(data) {
			return nil, 0, fmt.Errorf("pdfwrite: subset: table %q out of range", tag)
		}
		tables[tag] = data[off : off+length]
	}
	return tables, flavor, nil
}

// parseLoca decodes the loca table into numGlyphs+1 glyf offsets.
func parseLoca(b []byte, long bool) ([]uint32, error) {
	if long {
		if len(b)%4 != 0 {
			return nil, fmt.Errorf("pdfwrite: subset: long loca not 4-aligned")
		}
		n := len(b) / 4
		out := make([]uint32, n)
		for i := 0; i < n; i++ {
			out[i] = binary.BigEndian.Uint32(b[i*4:])
		}
		return out, nil
	}
	if len(b)%2 != 0 {
		return nil, fmt.Errorf("pdfwrite: subset: short loca not 2-aligned")
	}
	n := len(b) / 2
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = uint32(binary.BigEndian.Uint16(b[i*2:])) * 2 // short loca stores half-offsets
	}
	return out, nil
}

// encodeLoca serializes glyf offsets back to a loca table in the same format.
func encodeLoca(loca []uint32, long bool) []byte {
	if long {
		b := make([]byte, len(loca)*4)
		for i, v := range loca {
			binary.BigEndian.PutUint32(b[i*4:], v)
		}
		return b
	}
	b := make([]byte, len(loca)*2)
	for i, v := range loca {
		binary.BigEndian.PutUint16(b[i*2:], uint16(v/2))
	}
	return b
}

// compositeComponents returns the component GIDs referenced by glyph g, or nil for a
// simple/empty glyph. A composite glyph has a negative numberOfContours header.
func compositeComponents(glyf []byte, loca []uint32, g int) []int {
	if g+1 >= len(loca) {
		return nil
	}
	start, end := loca[g], loca[g+1]
	if end <= start || int(end) > len(glyf) || end-start < 10 {
		return nil
	}
	gd := glyf[start:end]
	if int16(binary.BigEndian.Uint16(gd[0:2])) >= 0 {
		return nil // simple glyph
	}
	var comps []int
	pos := 10 // skip numberOfContours + bbox
	const (
		argsAreWords   = 0x0001
		weHaveScale    = 0x0008
		moreComponents = 0x0020
		xyScale        = 0x0040
		twoByTwo       = 0x0080
	)
	for pos+4 <= len(gd) {
		flags := binary.BigEndian.Uint16(gd[pos:])
		glyphIndex := int(binary.BigEndian.Uint16(gd[pos+2:]))
		comps = append(comps, glyphIndex)
		pos += 4
		if flags&argsAreWords != 0 {
			pos += 4
		} else {
			pos += 2
		}
		switch {
		case flags&weHaveScale != 0:
			pos += 2
		case flags&xyScale != 0:
			pos += 4
		case flags&twoByTwo != 0:
			pos += 8
		}
		if flags&moreComponents == 0 {
			break
		}
	}
	return comps
}

// buildSFNTBytes reassembles an sfnt from a tag->bytes table map: offset table,
// tag-sorted directory with offsets/checksums, and 4-byte-aligned data. It mirrors
// pkg/font's internal builder (kept local so the subsetter is self-contained).
func buildSFNTBytes(flavor uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	n := len(tags)
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
		binary.BigEndian.PutUint32(buf[rec+4:], sfntTableChecksum(tables[tag]))
		binary.BigEndian.PutUint32(buf[rec+8:], uint32(offsets[tag]))
		binary.BigEndian.PutUint32(buf[rec+12:], uint32(len(tables[tag])))
		copy(buf[offsets[tag]:], tables[tag])
	}
	return buf
}

// sfntTableChecksum is the OpenType per-table checksum (sum of big-endian 32-bit
// words, zero-padded to 4 bytes).
func sfntTableChecksum(b []byte) uint32 {
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
