// Package gen builds tiny, deterministic PDF files in memory for tests. It is
// intentionally minimal: it writes classic xref tables (and, where noted, xref
// streams / object streams) so the parser and renderer can be exercised without
// committing binary fixtures.
package gen

import (
	"bytes"
	"compress/zlib"
	"fmt"
)

// builder assembles a PDF with a classic cross-reference table.
type builder struct {
	buf     bytes.Buffer
	offsets []int // byte offset of each object; index 0 is the free object
}

func newBuilder() *builder {
	b := &builder{}
	b.buf.WriteString("%PDF-1.7\n")
	b.buf.WriteString("%\xE2\xE3\xCF\xD3\n") // binary marker
	b.offsets = []int{0}                     // object 0 is always free
	return b
}

// addObject appends an indirect object with the given body and returns its
// object number.
func (b *builder) addObject(body string) int {
	num := len(b.offsets)
	b.offsets = append(b.offsets, b.buf.Len())
	fmt.Fprintf(&b.buf, "%d 0 obj\n%s\nendobj\n", num, body)
	return num
}

// addStream appends a stream object (dict + bytes) and returns its number.
func (b *builder) addStream(dictExtra string, data []byte) int {
	num := len(b.offsets)
	b.offsets = append(b.offsets, b.buf.Len())
	fmt.Fprintf(&b.buf, "%d 0 obj\n<< /Length %d%s >>\nstream\n", num, len(data), dictExtra)
	b.buf.Write(data)
	b.buf.WriteString("\nendstream\nendobj\n")
	return num
}

// finish writes the xref table and trailer, pointing /Root at rootNum.
func (b *builder) finish(rootNum int) []byte {
	xrefOff := b.buf.Len()
	n := len(b.offsets)
	fmt.Fprintf(&b.buf, "xref\n0 %d\n", n)
	b.buf.WriteString("0000000000 65535 f \n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b.buf, "%010d 00000 n \n", b.offsets[i])
	}
	fmt.Fprintf(&b.buf, "trailer\n<< /Size %d /Root %d 0 R >>\n", n, rootNum)
	fmt.Fprintf(&b.buf, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return b.buf.Bytes()
}

func zlibCompress(data []byte) []byte {
	var out bytes.Buffer
	zw := zlib.NewWriter(&out)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return out.Bytes()
}
