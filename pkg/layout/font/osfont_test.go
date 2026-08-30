package font

import (
	"testing"

	pkgfont "github.com/nathanstitt/omnidoc/pkg/font"
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

func TestStyleQuery(t *testing.T) {
	cases := []struct {
		family       string
		bold, italic bool
		want         string
	}{
		{"Arial", false, false, "Arial"},
		{"Arial", true, false, "Arial Bold"},
		{"Times New Roman", false, true, "Times New Roman Italic"},
		{"Helvetica", true, true, "Helvetica Bold Italic"},
		{"  Georgia  ", false, false, "Georgia"},
	}
	for _, c := range cases {
		if got := styleQuery(c.family, c.bold, c.italic); got != c.want {
			t.Errorf("styleQuery(%q,%v,%v) = %q, want %q", c.family, c.bold, c.italic, got, c.want)
		}
	}
}

// familyMatches decides whether a font the OS matcher returned is actually the font
// that was asked for. The cases that must REJECT are drawn from real measurements on a
// stock macOS host, where sysfont answered a request for "DejaVu Sans" with Lucida
// Grande and answered "Roboto", "IBM Plex Mono" and a nonexistent family with the very
// same Arial Unicode MS bytes.
func TestFamilyMatches(t *testing.T) {
	cases := []struct {
		declared, want string
		accept         bool
		why            string
	}{
		{"Barlow Condensed", "Barlow Condensed", true, "exact"},
		{"barlow condensed", "Barlow Condensed", true, "case-insensitive"},
		{"IBMPlexMono", "IBM Plex Mono", true, "separator-insensitive"},
		{"Barlow Condensed SemiBold", "Barlow Condensed", true, "per-weight file extends family with a style word"},
		{"Roboto Bold Italic", "Roboto", true, "multiple style words"},
		{"Arial Black", "Arial", true, "Black is a weight, not a different family"},

		{"Lucida Grande", "DejaVu Sans", false, "measured: sysfont's answer for DejaVu Sans"},
		{"Arial Unicode MS", "Roboto", false, "measured: sysfont's answer for Roboto"},
		{"Gill Sans", "Liberation Sans", false, "measured: sysfont's answer for Liberation Sans"},
		{"Times New Roman", "Times", false, "a different family that merely shares a prefix"},
		{"Arial", "Arial Unicode MS", false, "declared is narrower than the request"},
		{"", "Arial", false, "no declared name"},
		{"Arial", "", false, "no request"},
	}
	for _, c := range cases {
		if got := familyMatches(c.declared, c.want); got != c.accept {
			t.Errorf("familyMatches(%q, %q) = %v, want %v (%s)", c.declared, c.want, got, c.accept, c.why)
		}
	}
}

func TestNormalizeFamilyName(t *testing.T) {
	cases := [][2]string{
		{"  IBM Plex Mono  ", "ibmplexmono"},
		{"Barlow-Condensed", "barlowcondensed"},
		{"Noto_Sans", "notosans"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeFamilyName(c[0]); got != c[1] {
			t.Errorf("normalizeFamilyName(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// declaresFamily ACCEPTS a program whose name cannot be read, rather than rejecting
// it: a valid face that declares no family (classic Type1, or an sfnt with no name
// table) must still be usable. The check exists to catch a confidently-wrong answer,
// not to demand proof of identity from every file.
func TestDeclaresFamilyAcceptsUnreadableName(t *testing.T) {
	p := NewOSFontProvider()
	if !p.declaresFamily([]byte("not a font at all"), "Anything", "/tmp/x.ttf") {
		t.Error("declaresFamily rejected an undecodable program; want accept (caller reports the real error)")
	}
}

// The whole point of the identity check: a family the host does not have must come
// back as a MISS, so the caller reaches its bundled fallback. Before the check,
// sysfont's fuzzy matcher answered every such request with some unrelated installed
// font, making the miss path unreachable.
func TestOSFontProviderRejectsWrongFamily(t *testing.T) {
	p := NewOSFontProvider()
	const bogus = "ZzQqNoSuchFontFamily12345"
	data, ok := p.LoadStyled(bogus, false, false)
	if ok {
		// Accepting is only defensible if the file really does declare this family,
		// which cannot happen for a name this improbable.
		face, err := pkgfont.LoadSFNT(data)
		if err != nil {
			t.Fatalf("LoadStyled(%q) returned ok with undecodable bytes: %v", bogus, err)
		}
		declared, _ := face.FamilyName()
		t.Fatalf("LoadStyled(%q) accepted a font declaring %q; want a miss so the bundled fallback runs", bogus, declared)
	}
}

// Falsifiability control for the test above: the identity check must not reject fonts
// that genuinely ARE installed. Skips on a bare host with none of these families.
func TestOSFontProviderStillAcceptsRealFamily(t *testing.T) {
	p := NewOSFontProvider()
	for _, fam := range []string{"Helvetica", "Times New Roman", "Courier New", "DejaVu Sans", "Liberation Sans", "Arial", "Georgia", "Verdana"} {
		data, ok := p.LoadStyled(fam, false, false)
		if !ok {
			continue
		}
		face, err := pkgfont.LoadSFNT(data)
		if err != nil {
			t.Fatalf("LoadStyled(%q) bytes did not decode: %v", fam, err)
		}
		declared, nameOK := face.FamilyName()
		if nameOK && !familyMatches(declared, fam) {
			t.Fatalf("LoadStyled(%q) returned a font declaring %q, which the check should have rejected", fam, declared)
		}
		return
	}
	t.Skip("no common system font installed on this host")
}
