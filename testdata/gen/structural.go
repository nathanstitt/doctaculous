package gen

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// XRefStreamPDF returns a single-page text PDF whose cross-reference data is a
// cross-reference stream (/Type /XRef) rather than a classic xref table, with no
// fallback table. It exercises the parser's xref-stream path: a FlateDecode
// stream of fixed-width entries described by /W, with the trailer dictionary
// merged into the stream's own dictionary.
func XRefStreamPDF() []byte {
	x := newXrefStreamBuilder()
	font := x.addObject(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)
	content := []byte("BT /F1 24 Tf 72 700 Td (XRef stream PDF) Tj ET")
	contentNum := x.addStream(" /Length "+itoa(len(content)), content)

	pageNum := x.nextNum()
	pagesNum := pageNum + 1
	x.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, font, contentNum))
	x.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", pageNum))
	catalog := x.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	return x.finish(catalog)
}

// ObjStmPDF returns a single-page text PDF that stows several of its objects
// (font, page, pages, catalog) inside a compressed object stream (/Type /ObjStm)
// and uses a cross-reference stream to locate them by (stream, index). It
// exercises the object-stream decompression path that modern PDFs rely on.
func ObjStmPDF() []byte {
	x := newXrefStreamBuilder()

	// The content stream must be a regular indirect object (streams cannot live
	// inside an object stream).
	content := []byte("BT /F1 24 Tf 72 700 Td (Object stream PDF) Tj ET")
	contentNum := x.addStream(" /Length "+itoa(len(content)), content)

	// Reserve object numbers for the compressed objects so later objects (the
	// object stream itself, the xref stream) don't reuse them.
	fontNum := x.reserve()
	pageNum := x.reserve()
	pagesNum := x.reserve()
	catalogNum := x.reserve()

	compressed := []objStmEntry{
		{fontNum, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`},
		{pageNum, fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
			pagesNum, fontNum, contentNum)},
		{pagesNum, fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", pageNum)},
		{catalogNum, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum)},
	}
	x.addObjectStream(compressed)
	return x.finish(catalogNum)
}

// --- cross-reference-stream builder -----------------------------------------

// xrefEntry mirrors a row of a cross-reference stream: type 1 = uncompressed
// (field2 = byte offset), type 2 = compressed (field2 = object-stream number,
// field3 = index within it).
type xrefEntry struct {
	typ    int
	field2 int
	field3 int
}

type xrefStreamBuilder struct {
	buf     bytes.Buffer
	entries map[int]xrefEntry
	next    int // next object number to assign
}

func newXrefStreamBuilder() *xrefStreamBuilder {
	x := &xrefStreamBuilder{entries: map[int]xrefEntry{}, next: 1}
	x.buf.WriteString("%PDF-1.5\n")
	x.buf.WriteString("%\xE2\xE3\xCF\xD3\n")
	x.entries[0] = xrefEntry{typ: 0, field2: 0, field3: 65535} // free head
	return x
}

func (x *xrefStreamBuilder) nextNum() int { return x.next }

// reserve claims the next object number without writing anything, for objects
// (e.g. those packed into an object stream) whose bytes are written later.
func (x *xrefStreamBuilder) reserve() int {
	num := x.next
	x.next++
	return num
}

// addObject writes an uncompressed indirect object and records its offset.
func (x *xrefStreamBuilder) addObject(body string) int {
	num := x.next
	x.next++
	x.entries[num] = xrefEntry{typ: 1, field2: x.buf.Len()}
	fmt.Fprintf(&x.buf, "%d 0 obj\n%s\nendobj\n", num, body)
	return num
}

// addStream writes an uncompressed stream object (the dictExtra already carries
// /Length) and records its offset.
func (x *xrefStreamBuilder) addStream(dictExtra string, data []byte) int {
	num := x.next
	x.next++
	x.entries[num] = xrefEntry{typ: 1, field2: x.buf.Len()}
	fmt.Fprintf(&x.buf, "%d 0 obj\n<<%s >>\nstream\n", num, dictExtra)
	x.buf.Write(data)
	x.buf.WriteString("\nendstream\nendobj\n")
	return num
}

type objStmEntry struct {
	num  int
	body string
}

// addObjectStream packs the given objects into a compressed /ObjStm and records
// each as a type-2 (compressed) xref entry. It consumes one fresh object number
// for the stream itself.
func (x *xrefStreamBuilder) addObjectStream(objs []objStmEntry) int {
	var header, bodies bytes.Buffer
	for i, o := range objs {
		fmt.Fprintf(&header, "%d %d ", o.num, bodies.Len())
		bodies.WriteString(o.body)
		bodies.WriteByte('\n')
		x.entries[o.num] = xrefEntry{typ: 2, field2: 0 /* set below */, field3: i}
	}
	payload := append(header.Bytes(), bodies.Bytes()...)
	compressed := zlibCompress(payload)

	stmNum := x.next
	x.next++
	x.entries[stmNum] = xrefEntry{typ: 1, field2: x.buf.Len()}
	for _, o := range objs { // point compressed entries at this stream
		e := x.entries[o.num]
		e.field2 = stmNum
		x.entries[o.num] = e
	}
	dict := fmt.Sprintf(" /Type /ObjStm /N %d /First %d /Filter /FlateDecode /Length %d",
		len(objs), header.Len(), len(compressed))
	fmt.Fprintf(&x.buf, "%d 0 obj\n<<%s >>\nstream\n", stmNum, dict)
	x.buf.Write(compressed)
	x.buf.WriteString("\nendstream\nendobj\n")
	return stmNum
}

// finish writes the cross-reference stream itself and the startxref pointer.
// The xref stream is the last object; its own entry is added before encoding.
func (x *xrefStreamBuilder) finish(rootNum int) []byte {
	xrefNum := x.next
	x.next++
	xrefOff := x.buf.Len()
	x.entries[xrefNum] = xrefEntry{typ: 1, field2: xrefOff}

	size := x.next // highest object number + 1
	// Fixed field widths: 1 byte type, 4 bytes field2, 2 bytes field3.
	const w1, w2, w3 = 1, 4, 2
	var rows bytes.Buffer
	for i := range size {
		e, ok := x.entries[i]
		if !ok {
			e = xrefEntry{typ: 0} // unused slot → free entry
		}
		rows.WriteByte(byte(e.typ))
		var b4 [4]byte
		binary.BigEndian.PutUint32(b4[:], uint32(e.field2))
		rows.Write(b4[:])
		var b2 [2]byte
		binary.BigEndian.PutUint16(b2[:], uint16(e.field3))
		rows.Write(b2[:])
	}
	compressed := zlibCompress(rows.Bytes())

	dict := fmt.Sprintf(
		"<< /Type /XRef /Size %d /Root %d 0 R /W [ %d %d %d ] "+
			"/Filter /FlateDecode /Length %d >>",
		size, rootNum, w1, w2, w3, len(compressed))
	fmt.Fprintf(&x.buf, "%d 0 obj\n%s\nstream\n", xrefNum, dict)
	x.buf.Write(compressed)
	x.buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&x.buf, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return x.buf.Bytes()
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
