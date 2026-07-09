package font

import (
	"os"
	"testing"
)

// TestLoadSFNTCollection verifies LoadSFNT decodes a TrueType Collection (.ttc) by
// extracting its first face. It uses a macOS system .ttc when present (the common real
// case that motivated the fix); on hosts without one it skips (never flaky).
func TestLoadSFNTCollection(t *testing.T) {
	candidates := []string{
		"/System/Library/Fonts/Helvetica.ttc",
		"/System/Library/Fonts/Times.ttc",
		"/System/Library/Fonts/Courier.ttc",
	}
	var data []byte
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		t.Skip("no system .ttc collection available on this host")
	}
	face, err := LoadSFNT(data)
	if err != nil {
		t.Fatalf("LoadSFNT(.ttc): %v", err)
	}
	if _, _, ok := face.Glyph('A'); !ok {
		t.Fatal("collection face has no glyph for 'A'")
	}
}
