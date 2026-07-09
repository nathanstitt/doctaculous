package pdf

import (
	"bytes"
	"fmt"
	"strconv"
)

// rebuildXref is a fallback used when the cross-reference table is missing or
// broken: it scans the entire file for "N G obj" markers and the trailer. This
// makes the parser resilient to the many real-world PDFs with damaged xref data.
func (d *Document) rebuildXref() error {
	data := d.data
	// Scan for the "obj" keyword preceded by whitespace (the spec allows any
	// whitespace between the generation number and "obj", not just a space), and
	// back up to read the "N G" header.
	for i := 0; i+3 <= len(data); {
		idx := bytes.Index(data[i:], []byte("obj"))
		if idx < 0 {
			break
		}
		pos := i + idx // position of 'o' in "obj"
		// Require a whitespace byte immediately before "obj" so we don't match
		// "endobj" or identifiers that merely contain "obj".
		if pos == 0 || !isWhitespace(data[pos-1]) {
			i = pos + 3
			continue
		}
		// readObjHeaderBackward expects the offset of the byte just past the
		// header's trailing whitespace, i.e. the 'o' position works because it
		// first skips whitespace backward.
		num, gen, start, ok := readObjHeaderBackward(data, pos)
		if ok {
			d.xref[num] = xrefEntry{offset: int64(start)}
			_ = gen
		}
		i = pos + 3
	}

	// Find the trailer: prefer an explicit "trailer" dict, else look for a
	// catalog (/Type /Catalog) and synthesize a trailer pointing at it.
	if err := d.findTrailerForRebuild(); err != nil {
		return err
	}
	if len(d.xref) == 0 {
		return fmt.Errorf("pdf: rebuild found no objects")
	}
	return nil
}

func (d *Document) findTrailerForRebuild() error {
	data := d.data
	if idx := bytes.LastIndex(data, []byte("trailer")); idx >= 0 {
		p := newObjParser(data[idx+len("trailer"):])
		if obj, err := p.parseObject(); err == nil {
			if td, ok := obj.(Dict); ok && td["Root"] != nil {
				d.trailer = td
				return nil
			}
		}
	}
	// Synthesize: scan resolved objects for a Catalog.
	for num := range d.xref {
		obj := d.loadObject(num)
		if dict, ok := obj.(Dict); ok {
			if t, _ := dict["Type"].(Name); t == "Catalog" {
				d.trailer = Dict{"Root": Reference{Number: num, Generation: 0}}
				return nil
			}
		}
	}
	return fmt.Errorf("pdf: no trailer or catalog found during rebuild")
}

// readObjHeaderBackward reads "N G" immediately before the " obj" at objPos,
// also returning the byte offset where the object number starts.
func readObjHeaderBackward(data []byte, objPos int) (num, gen, start int, ok bool) {
	j := objPos
	// skip spaces before "obj"
	for j > 0 && isWhitespace(data[j-1]) {
		j--
	}
	genEnd := j
	for j > 0 && data[j-1] >= '0' && data[j-1] <= '9' {
		j--
	}
	genStart := j
	if genStart == genEnd {
		return 0, 0, 0, false
	}
	for j > 0 && isWhitespace(data[j-1]) {
		j--
	}
	numEnd := j
	for j > 0 && data[j-1] >= '0' && data[j-1] <= '9' {
		j--
	}
	numStart := j
	if numStart == numEnd {
		return 0, 0, 0, false
	}
	num, err1 := strconv.Atoi(string(data[numStart:numEnd]))
	gen, err2 := strconv.Atoi(string(data[genStart:genEnd]))
	if err1 != nil || err2 != nil {
		return 0, 0, 0, false
	}
	return num, gen, numStart, true
}
