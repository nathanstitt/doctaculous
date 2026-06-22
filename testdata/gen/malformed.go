package gen

import "bytes"

// The malformed fixtures below intentionally violate the spec in one specific
// way each, so a parser failure localizes to a single defect. The contract they
// test is graceful degradation: the parser must return an error (or recover and
// rebuild) WITHOUT panicking. Tests assert "no panic", not a particular error.

// TruncatedPDF returns a valid text PDF with its trailer and xref table lopped
// off, simulating a file cut short mid-write. A robust parser should attempt an
// object scan / xref rebuild rather than crash.
func TruncatedPDF() []byte {
	full := TextPDF()
	if i := bytes.LastIndex(full, []byte("xref")); i > 0 {
		return full[:i]
	}
	return full[:len(full)/2]
}

// BadXrefOffsetPDF returns a PDF whose startxref points at a bogus byte offset.
// The classic table can't be read there, forcing the parser onto its rebuild
// path (scan for "N G obj" markers).
func BadXrefOffsetPDF() []byte {
	full := TextPDF()
	// Replace the real startxref offset with an absurd one.
	idx := bytes.LastIndex(full, []byte("startxref"))
	if idx < 0 {
		return full
	}
	head := full[:idx]
	return append(head, []byte("startxref\n999999\n%%EOF\n")...)
}

// MissingEndobjPDF returns a PDF where one object omits its "endobj" keyword,
// exercising tolerant object-boundary handling.
func MissingEndobjPDF() []byte {
	full := TextPDF()
	return bytes.Replace(full, []byte("\nendobj\n"), []byte("\n"), 1)
}

// NoHeaderPDF returns bytes that lack the "%PDF-" header entirely. The parser
// should reject this with an error, never a panic.
func NoHeaderPDF() []byte {
	full := TextPDF()
	return bytes.TrimPrefix(full, []byte("%PDF-1.7\n"))
}

// BadStreamLengthPDF returns a PDF whose content stream declares a /Length far
// larger than the actual stream bytes, so length-trusting code would read past
// the stream. The parser should clamp to the "endstream" keyword.
func BadStreamLengthPDF() []byte {
	b := newBuilder()
	font := b.addObject(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)
	content := []byte("BT /F1 24 Tf 72 700 Td (Bad length) Tj ET")

	// Hand-write a stream object with a wildly wrong /Length.
	num := len(b.offsets)
	b.offsets = append(b.offsets, b.buf.Len())
	b.buf.WriteString(itoa(num))
	b.buf.WriteString(" 0 obj\n<< /Length 999999 >>\nstream\n")
	b.buf.Write(content)
	b.buf.WriteString("\nendstream\nendobj\n")
	contentNum := num

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	b.addObject(sprintfPage(pagesNum, font, contentNum))
	b.addObject("<< /Type /Pages /Kids [ " + itoa(pageNum) + " 0 R ] /Count 1 >>")
	catalog := b.addObject("<< /Type /Catalog /Pages " + itoa(pagesNum) + " 0 R >>")
	return b.finish(catalog)
}

func sprintfPage(pagesNum, font, contentNum int) string {
	return "<< /Type /Page /Parent " + itoa(pagesNum) +
		" 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 " + itoa(font) +
		" 0 R >> >> /Contents " + itoa(contentNum) + " 0 R >>"
}
