package font

import (
	"os"
	"testing"
)

func TestDecodeWOFF2RejectsBadSignature(t *testing.T) {
	_, err := decodeWOFF2([]byte("wOF2\x00\x00")) // signature only
	if err == nil {
		t.Fatal("decodeWOFF2(truncated) = nil error, want a typed error")
	}
}

func TestUIntBase128(t *testing.T) {
	// Two base-128 groups: 0xFF 0x08 => (0x7F<<7)|0x08.
	got, n, err := readUIntBase128([]byte{0xFF, 0x08})
	if err != nil {
		t.Fatalf("readUIntBase128: %v", err)
	}
	if got != (0x7F<<7)|0x08 || n != 2 {
		t.Fatalf("readUIntBase128 = %d (n=%d), want %d (n=2)", got, n, (0x7F<<7)|0x08)
	}
}

func TestDecodeWOFF2RoundTrips(t *testing.T) {
	ttf, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read ttf: %v", err)
	}
	w2, err := os.ReadFile(fixturePath(t, "webfont.woff2"))
	if err != nil {
		t.Fatalf("read woff2: %v", err)
	}
	bare, err := LoadSFNT(ttf)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	got, err := LoadSFNT(w2)
	if err != nil {
		t.Fatalf("LoadSFNT(woff2): %v", err)
	}
	for _, r := range []rune{'A', 'a', 'g', 'W', 'm'} {
		assertSameGlyph(t, bare, got, r)
	}
}

func TestDecodeWOFF2CorruptTransformDegrades(t *testing.T) {
	w2, err := os.ReadFile(fixturePath(t, "webfont.woff2"))
	if err != nil {
		t.Fatalf("read woff2: %v", err)
	}
	corrupt := w2[:len(w2)-10] // truncate inside the compressed block
	if _, err := decodeWOFF2(corrupt); err == nil {
		t.Fatal("decodeWOFF2(truncated block) = nil error, want a typed error")
	}
}
