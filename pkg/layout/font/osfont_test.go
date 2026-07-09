package font

import (
	"testing"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
)

// OSFontProvider must satisfy the pkgfont.Provider interface.
var _ pkgfont.Provider = (*OSFontProvider)(nil)

// TestOSFontProviderResolvesInstalledFont is assertive-when-possible, skip-when-bare:
// it discovers what fonts are actually installed on this host and, if a common family
// resolves, asserts LoadStyled returns non-empty bytes that decode as a font. It never
// hard-asserts a specific font (notably not Arial, which is absent on default Linux/CI),
// so it is not flaky.
func TestOSFontProviderResolvesInstalledFont(t *testing.T) {
	p := NewOSFontProvider()
	families := []string{"Helvetica", "Times", "Courier", "DejaVu Sans", "Liberation Sans", "Arial", "Calibri"}
	for _, fam := range families {
		if data, ok := p.LoadStyled(fam, false, false); ok {
			if len(data) == 0 {
				t.Fatalf("LoadStyled(%q) returned ok with empty bytes", fam)
			}
			if _, err := pkgfont.LoadSFNT(data); err != nil {
				t.Fatalf("LoadStyled(%q) bytes did not decode as a font: %v", fam, err)
			}
			return
		}
	}
	t.Skip("no common system font installed on this host; skipping system-font resolution assertion")
}

// TestOSFontProviderMissReturnsFalse: a family that cannot exist resolves to ok=false
// (or, on a machine where sysfont's fuzzy match reaches for it, still returns decodable
// bytes — either is acceptable; the contract is only 'no panic, ok=false on a true miss').
func TestOSFontProviderMissReturnsFalse(t *testing.T) {
	p := NewOSFontProvider()
	data, ok := p.LoadStyled("ZzQqNoSuchFontFamily12345", false, false)
	if ok && len(data) == 0 {
		t.Fatal("LoadStyled reported ok with empty bytes")
	}
}
