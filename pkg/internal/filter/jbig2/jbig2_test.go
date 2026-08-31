package jbig2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func loadPayload(t *testing.T) []byte {
	t.Helper()
	// From pkg/pdf/filter/jbig2/ up to repo root, then to the committed payload.
	p := filepath.Join("..", "..", "..", "..", "testdata", "gen", "jbig2", "generic.jb2")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read payload %s: %v", p, err)
	}
	return b
}

// TestVendoredDecodeSmoke confirms the vendored decoder builds and decodes the committed
// real JBIG2 payload to the expected page size. It guards against a future re-vendor
// breaking decoding.
func TestVendoredDecodeSmoke(t *testing.T) {
	dec, err := NewDecoder(bytes.NewReader(loadPayload(t)))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	img, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	b := img.Bounds()
	t.Logf("decoded first page: %dx%d", b.Dx(), b.Dy())
	if b.Dx() != 2550 || b.Dy() != 3305 {
		t.Fatalf("decoded size %dx%d, want 2550x3305", b.Dx(), b.Dy())
	}
}
