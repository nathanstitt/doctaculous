// Package pdfwrite implements a render.Device that emits a PDF document instead of
// pixels. This file holds the write-only PDF object model and serializer; it is
// deliberately separate from the parse-oriented pkg/pdf.
package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// object is any value that can appear in the PDF body.
type object interface{ writeTo(w *bytes.Buffer) }

// The PDF value types. Name is a name (/Foo); Int, Real, Bool, String are scalars;
// Ref is an indirect reference; Dict and Array are composites; stream is a stream
// object.
type (
	// Name is a PDF name object (written /Name, with #xx escaping).
	Name string
	// Int is a PDF integer.
	Int int64
	// Real is a PDF real number.
	Real float64
	// Bool is a PDF boolean.
	Bool bool
	// String is a PDF literal string, written (escaped) in parentheses.
	String string
	// Ref is a 1-based indirect object id; the zero value means "null" / absent.
	Ref int
	// Dict is a PDF dictionary; keys are stored WITHOUT a leading slash.
	Dict map[string]object
	// Array is a PDF array.
	Array []object
)

func (n Name) writeTo(b *bytes.Buffer) { b.WriteByte('/'); b.WriteString(escapeName(string(n))) }
func (i Int) writeTo(b *bytes.Buffer)  { b.WriteString(strconv.FormatInt(int64(i), 10)) }
func (r Real) writeTo(b *bytes.Buffer) { b.WriteString(formatReal(float64(r))) }
func (x Bool) writeTo(b *bytes.Buffer) {
	if x {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
}

func (s String) writeTo(b *bytes.Buffer) {
	b.WriteByte('(')
	b.WriteString(escapeString(string(s)))
	b.WriteByte(')')
}

func (r Ref) writeTo(b *bytes.Buffer) {
	if r == 0 {
		b.WriteString("null")
		return
	}
	fmt.Fprintf(b, "%d 0 R", int(r))
}

func (d Dict) writeTo(b *bytes.Buffer) {
	b.WriteString("<<")
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic output for reproducible tests
	for _, k := range keys {
		b.WriteByte('/')
		b.WriteString(escapeName(k))
		b.WriteByte(' ')
		d[k].writeTo(b)
		b.WriteByte(' ')
	}
	b.WriteString(">>")
}

func (a Array) writeTo(b *bytes.Buffer) {
	b.WriteByte('[')
	for i, e := range a {
		if i > 0 {
			b.WriteByte(' ')
		}
		e.writeTo(b)
	}
	b.WriteByte(']')
}

// stream is a dict plus already-encoded body bytes; /Length is set at addStream.
type stream struct {
	dict Dict
	data []byte
}

func (s stream) writeTo(b *bytes.Buffer) {
	s.dict.writeTo(b)
	b.WriteString("\nstream\n")
	b.Write(s.data)
	b.WriteString("\nendstream")
}

// writer accumulates indirect objects and serializes a complete PDF file.
type writer struct {
	objs []object // index i holds object id i+1; nil = allocated-but-unfilled
	root Ref
	info Ref
}

func newWriter() *writer { return &writer{} }

// alloc reserves a new indirect object id (1-based).
func (w *writer) alloc() Ref {
	w.objs = append(w.objs, nil)
	return Ref(len(w.objs))
}

// put stores obj at id (from alloc).
func (w *writer) put(id Ref, obj object) { w.objs[int(id)-1] = obj }

// setRoot records the document catalog reference written into the trailer.
func (w *writer) setRoot(id Ref) { w.root = id }

// setInfo records the /Info dictionary reference for the trailer (optional). It is
// used by the document assembler to attach /Title metadata.
//
//nolint:unused // wired up by the page assembler (page.go).
func (w *writer) setInfo(id Ref) { w.info = id }

// addStream allocates a stream object, flate-compresses data, sets /Filter and
// /Length, stores it, and returns its id.
func (w *writer) addStream(dict Dict, data []byte) Ref {
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	if dict == nil {
		dict = Dict{}
	}
	dict["Filter"] = Name("FlateDecode")
	dict["Length"] = Int(int64(zbuf.Len()))
	id := w.alloc()
	w.put(id, stream{dict: dict, data: zbuf.Bytes()})
	return id
}

// serialize writes the full PDF (header, body, xref table, trailer) to out.
func (w *writer) serialize(out io.Writer) error {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n") // binary marker for robustness

	offsets := make([]int, len(w.objs)+1) // offsets[id]
	for i, obj := range w.objs {
		id := i + 1
		if obj == nil {
			return fmt.Errorf("pdfwrite: object %d allocated but never put", id)
		}
		offsets[id] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", id)
		obj.writeTo(&b)
		b.WriteString("\nendobj\n")
	}

	xrefStart := b.Len()
	n := len(w.objs) + 1
	fmt.Fprintf(&b, "xref\n0 %d\n", n)
	b.WriteString("0000000000 65535 f \n")
	for id := 1; id < n; id++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[id])
	}

	trailer := Dict{"Size": Int(int64(n)), "Root": w.root}
	if w.info != 0 {
		trailer["Info"] = w.info
	}
	b.WriteString("trailer\n")
	trailer.writeTo(&b)
	fmt.Fprintf(&b, "\nstartxref\n%d\n%%%%EOF\n", xrefStart)

	if _, err := out.Write(b.Bytes()); err != nil {
		return fmt.Errorf("pdfwrite: write: %w", err)
	}
	return nil
}

// formatReal formats a float for PDF output: fixed-point with up to 4 decimals and
// no trailing zeros or "-0", so numbers stay compact and deterministic.
func formatReal(f float64) string {
	s := strconv.FormatFloat(f, 'f', 4, 64)
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// escapeName escapes characters not allowed bare in a PDF name (#xx hex).
func escapeName(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '!' || c > '~' || c == '#' || c == '/' || c == '(' || c == ')' ||
			c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '%' {
			fmt.Fprintf(&sb, "#%02X", c)
		} else {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// escapeString escapes a PDF literal string body.
func escapeString(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '\n':
			sb.WriteString("\\n")
		case '\r':
			sb.WriteString("\\r")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
