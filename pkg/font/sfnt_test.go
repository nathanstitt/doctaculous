package font

import (
	"os"
	"path/filepath"
	"testing"
)

// fixturePath resolves a testdata/fonts/* fixture from the repo root (two levels
// up from pkg/font).
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fonts", name)
}

func TestLoadSFNTRawTTF(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "webfont.ttf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	face, err := LoadSFNT(data)
	if err != nil {
		t.Fatalf("LoadSFNT(ttf): %v", err)
	}
	// A letter present in the subset must resolve to a non-empty advance.
	out, adv, ok := face.Glyph('A')
	if !ok || adv <= 0 {
		t.Fatalf("Glyph('A') ok=%v adv=%v, want a real glyph", ok, adv)
	}
	_ = out // outline presence is not asserted here; advance + ok is the contract
}

func TestLoadSFNTRejectsGarbage(t *testing.T) {
	_, err := LoadSFNT([]byte("not a font at all"))
	if err == nil {
		t.Fatal("LoadSFNT(garbage) = nil error, want a typed error")
	}
}
