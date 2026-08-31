package filter

import (
	"bytes"
	"compress/zlib"
	"testing"
)

func TestFlateDecode(t *testing.T) {
	original := []byte("the quick brown fox jumps over the lazy dog")
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(buf.Bytes(), []Stage{{Name: "FlateDecode"}})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("flate round-trip = %q, want %q", got, original)
	}
}

func TestASCIIHexDecode(t *testing.T) {
	cases := map[string]string{
		"48656C6C6F>":     "Hello",
		"48 65 6C 6C 6F>": "Hello",
		"4>":              "@", // odd digit -> 0x40
	}
	for in, want := range cases {
		got, err := Decode([]byte(in), []Stage{{Name: "ASCIIHexDecode"}})
		if err != nil {
			t.Fatalf("ASCIIHexDecode(%q): %v", in, err)
		}
		if string(got) != want {
			t.Errorf("ASCIIHexDecode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestASCII85Decode(t *testing.T) {
	// "<~...~>" wrapper is stripped by the PDF lexer; here we feed the inner data.
	// Encoding of "Man " is "9jqo^" in ascii85.
	got, err := Decode([]byte("9jqo^~>"), []Stage{{Name: "ASCII85Decode"}})
	if err != nil {
		t.Fatalf("ASCII85Decode: %v", err)
	}
	if string(got) != "Man " {
		t.Errorf("ASCII85Decode = %q, want %q", got, "Man ")
	}
}

func TestASCII85ZeroShortcut(t *testing.T) {
	got, err := Decode([]byte("z~>"), []Stage{{Name: "ASCII85Decode"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("ascii85 'z' = %v, want four zero bytes", got)
	}
}

func TestRunLengthDecode(t *testing.T) {
	// length 2 -> copy 3 literal bytes "ABC"; length 254 (257-254=3) -> 3x 'Z'; 128 EOD.
	in := []byte{2, 'A', 'B', 'C', 254, 'Z', 128}
	got, err := Decode(in, []Stage{{Name: "RunLengthDecode"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ABCZZZ" {
		t.Errorf("RunLengthDecode = %q, want %q", got, "ABCZZZ")
	}
}

func TestFilterChain(t *testing.T) {
	// ASCIIHex then... just ASCIIHex; verify multi-stage plumbing with two hex passes
	// is not meaningful, so test Flate+predictor path instead via a known PNG-up row.
	// Predictor 12 (PNG Up), 1 column, 1 color, 8 bpc: each row prefixed by filter tag.
	// Row0 tag=0 value 10; Row1 tag=2(Up) delta 5 -> 15.
	rawRows := []byte{0, 10, 2, 5}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(rawRows)
	_ = zw.Close()
	got, err := Decode(buf.Bytes(), []Stage{{
		Name:   "FlateDecode",
		Params: Params{Predictor: 12, Columns: 1, Colors: 1, BitsPerComponent: 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{10, 15}
	if !bytes.Equal(got, want) {
		t.Errorf("flate+png-predictor = %v, want %v", got, want)
	}
}

func TestUnsupportedImageFilter(t *testing.T) {
	_, err := Decode([]byte("data"), []Stage{{Name: "DCTDecode"}})
	if err == nil {
		t.Fatal("expected error for DCTDecode")
	}
	if !IsImageFilter("DCTDecode") {
		t.Error("DCTDecode should be reported as an image filter")
	}
}

func TestLZWRoundTripKnownVector(t *testing.T) {
	// Classic LZW example from the TIFF spec input "-----A---B" is complex;
	// instead verify a trivial all-literal stream decodes (no compression gains).
	// Encode "AB" manually: clear(256) A(65) B(66) EOD(257) at 9-bit MSB-first.
	bits := newBitWriter()
	bits.write(256, 9)
	bits.write(65, 9)
	bits.write(66, 9)
	bits.write(257, 9)
	got, err := Decode(bits.bytes(), []Stage{{Name: "LZWDecode"}})
	if err != nil {
		t.Fatalf("LZWDecode: %v", err)
	}
	if string(got) != "AB" {
		t.Errorf("LZWDecode = %q, want %q", got, "AB")
	}
}

func TestLZWEarlyChangeDistinct(t *testing.T) {
	// A short all-literal stream stays within 9-bit codes, so EarlyChange does not
	// affect the output here — but we assert both an explicit 0 and an explicit 1
	// decode identically and that "unspecified" (nil) matches the default (1).
	bits := newBitWriter()
	bits.write(256, 9) // clear
	bits.write(65, 9)  // A
	bits.write(66, 9)  // B
	bits.write(257, 9) // EOD
	data := bits.bytes()

	zero := 0
	one := 1
	cases := map[string]*int{"unspecified": nil, "explicit-0": &zero, "explicit-1": &one}
	for name, ec := range cases {
		got, err := Decode(data, []Stage{{Name: "LZWDecode", Params: Params{EarlyChange: ec}}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != "AB" {
			t.Errorf("%s: LZWDecode = %q, want AB", name, got)
		}
	}
}

// bitWriter writes MSB-first bit groups, mirroring the decoder's bitReader.
type bitWriter struct {
	out   []byte
	cur   byte
	nBits int
}

func newBitWriter() *bitWriter { return &bitWriter{} }

func (w *bitWriter) write(val, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((val >> i) & 1)
		w.cur = (w.cur << 1) | bit
		w.nBits++
		if w.nBits == 8 {
			w.out = append(w.out, w.cur)
			w.cur = 0
			w.nBits = 0
		}
	}
}

func (w *bitWriter) bytes() []byte {
	if w.nBits > 0 {
		w.out = append(w.out, w.cur<<(8-w.nBits))
		w.cur = 0
		w.nBits = 0
	}
	return w.out
}
