package pdfwrite

import (
	"encoding/binary"
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/font"
)

// subsetTrueType returns an SFNT containing only the glyph programs for keep (plus
// composite dependencies and .notdef), with glyph indices preserved so a Type0
// font's Identity CIDToGIDMap stays valid. Unused glyphs become zero-length in the
// rewritten glyf/loca; every other table is copied verbatim. It returns an error if
// data is not a parseable glyf-flavored SFNT (the caller then embeds the whole
// program).
func subsetTrueType(data []byte, keep []uint16) ([]byte, error) {
	tables, flavor, err := font.ParseSFNTTables(data)
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

	return font.BuildSFNT(flavor, tables), nil
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
