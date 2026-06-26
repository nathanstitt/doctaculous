package font

import (
	"path/filepath"
	"testing"
)

func TestDiskFontProviderLoadsByName(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "fonts")
	p := DiskFontProvider{Dir: dir}
	// Case-insensitive, extension-agnostic match on the base name "webfont".
	data, ok := p.LoadLocal("webfont")
	if !ok || len(data) == 0 {
		t.Fatalf("LoadLocal(webfont) ok=%v len=%d, want a hit", ok, len(data))
	}
}

func TestDiskFontProviderMissReturnsFalse(t *testing.T) {
	p := DiskFontProvider{Dir: filepath.Join("..", "..", "..", "testdata", "fonts")}
	if _, ok := p.LoadLocal("no-such-font"); ok {
		t.Fatal("LoadLocal(no-such-font) ok=true, want false")
	}
}

func TestDiskFontProviderEmptyDir(t *testing.T) {
	var p DiskFontProvider // zero value: empty Dir -> never matches, no panic
	if _, ok := p.LoadLocal("webfont"); ok {
		t.Fatal("zero DiskFontProvider matched, want miss")
	}
}
