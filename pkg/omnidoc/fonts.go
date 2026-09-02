package omnidoc

import (
	layoutfont "github.com/nathanstitt/omnidoc/pkg/internal/layout/font"
)

// FontProvider resolves a font family and style to raw font-program bytes
// (sfnt/TrueType/OpenType, WOFF, bare CFF, or classic Type1/PFB — the toolkit
// detects the format). It is the injectable resolution layer both the PDF
// rasterizer and the reflow engine consult for a non-embedded font before
// falling back to the bundled substitutes, so a caller can point the toolkit
// at system fonts, a fonts directory, or exact-metric real faces — including
// families the bundle has no look-alike for (Symbol, ZapfDingbats).
//
// ok is false when the provider has no match; the caller then proceeds to the
// bundled fallback. A provider must be safe for concurrent use: a parsed
// document is shared read-only across the page fan-out.
//
// DirFontProvider is the ready-made implementation for a directory of font
// files. Set one on RasterOptions.FontProvider or SVGOptions.FontProvider.
type FontProvider interface {
	// LoadStyled returns the raw program bytes for family in the given
	// weight/slant.
	LoadStyled(family string, bold, italic bool) (data []byte, ok bool)
}

// SystemFontProvider resolves an @font-face local() name to font bytes (raw
// sfnt or a WOFF container). It is consulted by the HTML pipeline when a
// stylesheet's @font-face src lists a local() source; a nil provider, or one
// with no match, means local() does not resolve and the next src entry is
// tried. Install one with WithSystemFontProvider.
//
// DirFontProvider implements this too, matching the local() name against the
// file base names in its directory.
type SystemFontProvider interface {
	// LoadLocal returns the raw font bytes for a named local face. ok is false
	// when the provider has no such font.
	LoadLocal(name string) (data []byte, ok bool)
}

// DirFontProvider serves fonts from a directory of font files, and satisfies
// both FontProvider and SystemFontProvider:
//
//   - LoadLocal matches an @font-face local() name against the files' base
//     names case-insensitively and extension-agnostically, so local("Roboto")
//     finds Roboto.ttf, roboto.otf, or Roboto.woff2.
//   - LoadStyled probes conventional style-suffixed base names in preference
//     order ("Family-BoldItalic", "Family-Bold", "Family", ...) so a directory
//     laid out like Arial-Bold.ttf resolves a bold request, and a family with
//     only a regular file still resolves to its upright face.
//
// OpenHTMLFile, OpenMarkdownFile, and the other file-based openers install one
// rooted at the document's directory by default, so fonts beside the document
// resolve without configuration. A zero value (empty Dir) never matches.
type DirFontProvider struct {
	// Dir is the directory scanned for font files.
	Dir string
}

// LoadStyled implements FontProvider.
func (d DirFontProvider) LoadStyled(family string, bold, italic bool) ([]byte, bool) {
	return layoutfont.DiskFontProvider{Dir: d.Dir}.LoadStyled(family, bold, italic)
}

// LoadLocal implements SystemFontProvider.
func (d DirFontProvider) LoadLocal(name string) ([]byte, bool) {
	return layoutfont.DiskFontProvider{Dir: d.Dir}.LoadLocal(name)
}
