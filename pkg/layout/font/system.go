package font

import (
	"os"
	"path/filepath"
	"strings"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
)

// DiskFontProvider implements pkgfont.Provider (the injectable font-resolution layer).
var _ pkgfont.Provider = DiskFontProvider{}

// SystemFontProvider resolves an @font-face local() name to font bytes (raw sfnt
// or a WOFF container — the caller unwraps via font.LoadSFNT). A nil provider, or
// one with no match, means local() does not resolve and the caller tries the next
// src entry.
type SystemFontProvider interface {
	// LoadLocal returns the raw font bytes for a named local face. ok is false when
	// the provider has no such font.
	LoadLocal(name string) (data []byte, ok bool)
}

// fontExts are the file extensions DiskFontProvider recognizes, in preference order.
var fontExts = []string{".ttf", ".otf", ".woff2", ".woff"}

// DiskFontProvider serves local() fonts from a directory, matching name against
// file base names case-insensitively (extension-agnostic). It is the hermetic
// default for tests (point Dir at testdata/) and a simple local resolver. A zero
// value (empty Dir) never matches.
type DiskFontProvider struct {
	Dir string
}

// LoadStyled implements pkgfont.Provider: it resolves family + weight/slant to font
// bytes by probing conventional style-suffixed base names in preference order
// ("Family-BoldItalic", "Family-Bold", "Family", ...) against the recognized
// extensions, so a directory laid out like "Arial-Bold.ttf" is matched. It reuses
// LoadLocal's exact + scan matching per candidate. A family with only a regular file
// still resolves (the bare-family candidate), giving the caller the upright face when a
// weighted file is absent.
func (d DiskFontProvider) LoadStyled(family string, bold, italic bool) ([]byte, bool) {
	for _, cand := range styleCandidates(family, bold, italic) {
		if b, ok := d.LoadLocal(cand); ok {
			return b, true
		}
	}
	return nil, false
}

// styleCandidates lists base-name candidates for a family+style, most-specific first,
// so LoadStyled prefers an exact weighted file but degrades to a less-specific one.
func styleCandidates(family string, bold, italic bool) []string {
	f := strings.TrimSpace(family)
	var out []string
	switch {
	case bold && italic:
		out = append(out, f+"-BoldItalic", f+"-BoldOblique", f+"-Bold", f+"-Italic")
	case bold:
		out = append(out, f+"-Bold")
	case italic:
		out = append(out, f+"-Italic", f+"-Oblique")
	}
	return append(out, f) // the bare family (regular) is always the final fallback
}

// LoadLocal implements SystemFontProvider.
func (d DiskFontProvider) LoadLocal(name string) ([]byte, bool) {
	if d.Dir == "" || name == "" {
		return nil, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, ext := range fontExts {
		path := filepath.Join(d.Dir, want+ext)
		if b, err := os.ReadFile(path); err == nil {
			return b, true
		}
	}
	// Fallback: scan the directory for a base-name match (handles a name whose file
	// uses different casing or a space the exact-path probe missed).
	ents, err := os.ReadDir(d.Dir)
	if err != nil {
		return nil, false
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if base == want {
			if b, err := os.ReadFile(filepath.Join(d.Dir, e.Name())); err == nil {
				return b, true
			}
		}
	}
	return nil, false
}
