package font

import (
	"os"
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

// TestDiskFontProviderLoadStyled verifies the family+style resolver probes the
// conventional style-suffixed base names and degrades to the bare family.
func TestDiskFontProviderLoadStyled(t *testing.T) {
	dir := t.TempDir()
	write := func(base, content string) {
		if err := os.WriteFile(filepath.Join(dir, base+".ttf"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Acme", "regular")
	write("Acme-Bold", "bold")
	write("Acme-Italic", "italic")
	// No Acme-BoldItalic on disk: bold+italic should fall back to Bold, then Italic.
	p := DiskFontProvider{Dir: dir}

	cases := []struct {
		bold, italic bool
		want         string
	}{
		{false, false, "regular"},
		{true, false, "bold"},
		{false, true, "italic"},
		{true, true, "bold"}, // BoldItalic absent -> Bold is the next candidate
	}
	for _, c := range cases {
		data, ok := p.LoadStyled("Acme", c.bold, c.italic)
		if !ok {
			t.Errorf("LoadStyled(Acme,%v,%v) miss", c.bold, c.italic)
			continue
		}
		if string(data) != c.want {
			t.Errorf("LoadStyled(Acme,%v,%v) = %q, want %q", c.bold, c.italic, data, c.want)
		}
	}

	// A family with no file at all misses.
	if _, ok := p.LoadStyled("Nonexistent", true, false); ok {
		t.Error("LoadStyled(Nonexistent) matched, want miss")
	}
}
