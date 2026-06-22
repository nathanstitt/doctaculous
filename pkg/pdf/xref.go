package pdf

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/nathanstitt/doctaculous/pkg/pdf/filter"
)

// readXref locates the last startxref and reads the cross-reference chain
// (classic tables and/or xref streams), following /Prev and /XRefStm links.
// Entries from earlier sections do not overwrite ones already seen (later
// sections take precedence).
func (d *Document) readXref() error {
	off, err := d.findStartXref()
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for off >= 0 {
		if seen[off] {
			break // cycle guard
		}
		seen[off] = true

		prev, xrefStm, trailer, err := d.readXrefSection(off)
		if err != nil {
			return err
		}
		if d.trailer == nil && trailer != nil {
			d.trailer = trailer
		}
		// A hybrid-reference file points to an xref stream via /XRefStm.
		if xrefStm >= 0 && !seen[xrefStm] {
			if _, _, _, e := d.readXrefSection(xrefStm); e == nil {
				seen[xrefStm] = true
			}
		}
		off = prev
	}
	if len(d.xref) == 0 {
		return fmt.Errorf("pdf: empty cross-reference table")
	}
	return nil
}

// findStartXref scans the tail of the file for the last "startxref" keyword and
// returns the byte offset it points to.
func (d *Document) findStartXref() (int64, error) {
	const tailWindow = 2048
	start := max(len(d.data)-tailWindow, 0)
	tail := d.data[start:]
	idx := bytes.LastIndex(tail, []byte("startxref"))
	if idx < 0 {
		return -1, fmt.Errorf("pdf: startxref not found")
	}
	rest := tail[idx+len("startxref"):]
	// Skip whitespace, then read digits.
	i := 0
	for i < len(rest) && isWhitespace(rest[i]) {
		i++
	}
	j := i
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j == i {
		return -1, fmt.Errorf("pdf: malformed startxref")
	}
	off, err := strconv.ParseInt(string(rest[i:j]), 10, 64)
	if err != nil {
		return -1, fmt.Errorf("pdf: bad startxref offset: %w", err)
	}
	if off < 0 || off >= int64(len(d.data)) {
		return -1, fmt.Errorf("pdf: startxref offset %d out of range", off)
	}
	return off, nil
}

// readXrefSection reads one xref section at off. It returns the /Prev offset (or
// -1), an optional /XRefStm offset (or -1), and the section's trailer dict.
func (d *Document) readXrefSection(off int64) (prev, xrefStm int64, trailer Dict, err error) {
	if off < 0 || int(off) >= len(d.data) {
		return -1, -1, nil, fmt.Errorf("pdf: xref offset out of range")
	}
	// Classic tables begin with the keyword "xref"; otherwise it is an xref stream.
	rest := d.data[off:]
	probe := bytes.TrimLeft(rest, " \t\r\n")
	if bytes.HasPrefix(probe, []byte("xref")) {
		return d.readXrefTable(off)
	}
	return d.readXrefStream(off)
}

// readXrefTable parses a classic "xref ... trailer << >>" section.
func (d *Document) readXrefTable(off int64) (prev, xrefStm int64, trailer Dict, err error) {
	prev, xrefStm = -1, -1
	src := d.data[off:]
	// Move past "xref".
	pos := bytes.Index(src, []byte("xref"))
	if pos < 0 {
		return -1, -1, nil, fmt.Errorf("pdf: xref keyword missing")
	}
	pos += len("xref")

	lex := newLexer(src)
	lex.pos = pos
	for {
		lex.skipSpace()
		// A subsection header is "start count"; the trailer follows the keyword.
		save := lex.pos
		t1, e := lex.next()
		if e != nil {
			return -1, -1, nil, e
		}
		if t1.kind == tokKeyword && string(t1.val) == "trailer" {
			break
		}
		if t1.kind != tokInteger {
			// Unexpected; stop subsection scanning.
			lex.pos = save
			break
		}
		t2, e := lex.next()
		if e != nil || t2.kind != tokInteger {
			return -1, -1, nil, fmt.Errorf("pdf: malformed xref subsection header")
		}
		startObj := int(t1.num)
		count := int(t2.num)
		// Each entry is "offset(10) gen(5) type(1)". Spec-conformant files use a
		// 20-byte fixed stride, but real-world producers sometimes emit 19-byte
		// (single-EOL) entries or odd spacing, so we read the three fields
		// tokenwise rather than slicing a fixed width (which can panic on a
		// truncated final entry).
		for k := range count {
			offTok, e := lex.next()
			if e != nil || offTok.kind != tokInteger {
				return -1, -1, nil, fmt.Errorf("pdf: malformed xref entry offset")
			}
			genTok, e := lex.next()
			if e != nil || genTok.kind != tokInteger {
				return -1, -1, nil, fmt.Errorf("pdf: malformed xref entry generation")
			}
			typTok, e := lex.next()
			if e != nil || typTok.kind != tokKeyword {
				return -1, -1, nil, fmt.Errorf("pdf: malformed xref entry type")
			}
			objNum := startObj + k
			if len(typTok.val) > 0 && typTok.val[0] == 'n' {
				if _, exists := d.xref[objNum]; !exists {
					d.xref[objNum] = xrefEntry{offset: int64(offTok.num)}
				}
			}
			_ = genTok
		}
	}

	// Parse the trailer dictionary.
	p := newObjParser(src[lex.pos:])
	tobj, e := p.parseObject()
	if e != nil {
		return -1, -1, nil, fmt.Errorf("pdf: parsing trailer: %w", e)
	}
	tdict, ok := tobj.(Dict)
	if !ok {
		return -1, -1, nil, fmt.Errorf("pdf: trailer is not a dictionary")
	}
	trailer = tdict
	if pv, ok := IntValue(tdict["Prev"]); ok {
		prev = int64(pv)
	}
	if xs, ok := IntValue(tdict["XRefStm"]); ok {
		xrefStm = int64(xs)
	}
	return prev, xrefStm, trailer, nil
}

// readXrefStream parses a cross-reference stream (PDF 1.5+).
func (d *Document) readXrefStream(off int64) (prev, xrefStm int64, trailer Dict, err error) {
	prev, xrefStm = -1, -1
	obj := d.parseObjectAt(off, -1)
	s, ok := obj.(*Stream)
	if !ok {
		return -1, -1, nil, fmt.Errorf("pdf: expected xref stream at offset %d", off)
	}
	dict := s.Dict
	if d.trailer == nil {
		trailer = dict
	}

	w := d.GetArray(dict["W"])
	if len(w) != 3 {
		return -1, -1, nil, fmt.Errorf("pdf: xref stream missing /W")
	}
	w0, _ := IntValue(w[0])
	w1, _ := IntValue(w[1])
	w2, _ := IntValue(w[2])
	rowLen := w0 + w1 + w2
	if rowLen <= 0 {
		return -1, -1, nil, fmt.Errorf("pdf: bad /W in xref stream")
	}

	size, _ := IntValue(dict["Size"])
	index := d.xrefStreamIndex(dict, size)

	stages, imgF := d.filterStages(dict)
	if imgF != "" {
		return -1, -1, nil, fmt.Errorf("pdf: xref stream uses image filter %s", imgF)
	}
	data, derr := filter.Decode(s.Raw, stages)
	if derr != nil {
		return -1, -1, nil, fmt.Errorf("pdf: decoding xref stream: %w", derr)
	}

	d.parseXrefStreamData(data, index, w0, w1, w2, rowLen)

	if pv, ok := IntValue(dict["Prev"]); ok {
		prev = int64(pv)
	}
	if trailer != nil {
		return prev, xrefStm, trailer, nil
	}
	return prev, xrefStm, nil, nil
}

// xrefStreamIndex returns the /Index pairs (objStart, count), defaulting to the
// whole table [0 Size] when absent.
func (d *Document) xrefStreamIndex(dict Dict, size int) [][2]int {
	idx := d.GetArray(dict["Index"])
	if len(idx) == 0 {
		return [][2]int{{0, size}}
	}
	var out [][2]int
	for i := 0; i+1 < len(idx); i += 2 {
		start, _ := IntValue(idx[i])
		count, _ := IntValue(idx[i+1])
		out = append(out, [2]int{start, count})
	}
	return out
}

func (d *Document) parseXrefStreamData(data []byte, index [][2]int, w0, w1, w2, rowLen int) {
	pos := 0
	readField := func(width int) int64 {
		if width == 0 {
			return 0
		}
		var v int64
		for i := 0; i < width && pos < len(data); i++ {
			v = v<<8 | int64(data[pos])
			pos++
		}
		return v
	}
	for _, pair := range index {
		objStart, count := pair[0], pair[1]
		for k := range count {
			if pos+rowLen > len(data) {
				return
			}
			var typ int64 = 1 // default type when w0 == 0
			if w0 > 0 {
				typ = readField(w0)
			}
			f2 := readField(w1)
			f3 := readField(w2)
			objNum := objStart + k
			if _, exists := d.xref[objNum]; exists {
				continue
			}
			switch typ {
			case 1: // uncompressed: f2 = offset
				d.xref[objNum] = xrefEntry{offset: f2}
			case 2: // compressed: f2 = stream obj number, f3 = index
				d.xref[objNum] = xrefEntry{inStream: true, streamObj: int(f2), indexInStrm: int(f3)}
			default: // type 0 (free) or unknown: skip
			}
		}
	}
}
