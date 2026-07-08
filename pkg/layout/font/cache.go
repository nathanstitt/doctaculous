// Package font provides a concurrency-safe cache of resolved font faces for the
// reflow engine. Parsing a font program is the expensive step, so a document that
// uses the same family across many runs should parse each (family, style) once.
//
// The cache lives under pkg/layout (not a specific format package) because every
// reflowable frontend — DOCX, and later HTML/EPUB — resolves named families the
// same way; keeping the mutable cache here keeps pkg/font itself free of global
// state.
package font

import (
	"context"
	"strings"
	"sync"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// faceKey identifies a resolved face: a normalized family name plus its style.
type faceKey struct {
	family string
	style  pkgfont.Style
}

// cacheEntry memoizes one resolution, including the negative result (no bundled
// substitute) so a missing family is not re-looked-up on every run.
type cacheEntry struct {
	face *pkgfont.Face
	ok   bool
}

// FaceCache resolves named font families to *font.Face, caching each result. It
// is safe for concurrent use. The zero value is not usable; build one with
// NewFaceCache.
type FaceCache struct {
	mu    sync.Mutex
	faces map[faceKey]cacheEntry

	// Web-font resolution state (nil/empty for bundled-only caches, e.g. DOCX).
	fontFaces map[string][]gcss.FontFace // normalized family -> @font-face entries
	loader    resource.ResourceLoader
	sys       SystemFontProvider
	logf      func(string, ...any)
}

// NewFaceCache returns an empty cache ready for use.
func NewFaceCache() *FaceCache {
	return &FaceCache{faces: make(map[faceKey]cacheEntry)}
}

// NewFaceCacheWithFonts returns a cache that resolves @font-face families to
// downloaded faces before falling back to bundled substitutes. faces are the
// captured @font-face rules (grouped by family internally); loader fetches url()
// sources; sys resolves local() sources (nil → local() never matches); logf logs
// degradation (nil → no-op). It is safe for concurrent use.
func NewFaceCacheWithFonts(faces []gcss.FontFace, loader resource.ResourceLoader, sys SystemFontProvider, logf func(string, ...any)) *FaceCache {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	byFamily := make(map[string][]gcss.FontFace)
	for _, ff := range faces {
		key := normalizeFamily(ff.Family)
		byFamily[key] = append(byFamily[key], ff)
	}
	return &FaceCache{
		faces:     make(map[faceKey]cacheEntry),
		fontFaces: byFamily,
		loader:    loader,
		sys:       sys,
		logf:      logf,
	}
}

// normalizeFamily lowercases and trims a family name for case-insensitive lookup.
func normalizeFamily(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Resolve returns the face for family in the requested style, substituting a
// bundled look-alike via pkg/font. family may be a CSS font-family fallback list
// (comma-separated, as cleaned by the cascade); each candidate is tried in order —
// @font-face sources first, then a bundled substitute — and the first that
// resolves wins (a generic keyword like serif always resolves, so it acts as the
// terminal fallback). ok is false only when no candidate resolves (the caller
// skips affected runs). Results — including misses — are cached under the whole
// list string, so repeated calls for the same (list, style) are cheap.
func (c *FaceCache) Resolve(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	key := faceKey{family: normalizeFamily(family), style: style}

	c.mu.Lock()
	defer c.mu.Unlock()
	if e, found := c.faces[key]; found {
		return e.face, e.ok
	}
	face, ok := c.resolveList(family, style)
	c.faces[key] = cacheEntry{face: face, ok: ok}
	return face, ok
}

// resolveList tries each comma-separated candidate in the font-family list in
// order, returning the first face that resolves. For each candidate the resolution
// chain is: @font-face sources (so a downloaded face beats a bundled look-alike of
// the same name), then the injected Provider's style-aware lookup (system/disk fonts,
// including weighted real faces and families the bundle has no look-alike for), then
// the bundled substitute. Caller holds c.mu.
func (c *FaceCache) resolveList(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	for _, name := range splitFamilyList(family) {
		if face, ok := c.resolveFontFace(name, style); ok {
			return face, true
		}
		if face, ok := c.resolveProvider(name, style); ok {
			return face, true
		}
		if face, ok := pkgfont.LoadStandard(name, style); ok {
			return face, true
		}
	}
	return nil, false
}

// resolveProvider consults the injected Provider (when the configured sys also
// implements pkgfont.Provider) for a style-aware, non-@font-face face: the disk or
// system provider serves a weighted real face for the family, which beats the bundled
// look-alike. It decodes the returned bytes via pkgfont.LoadSFNT, so it handles
// TrueType/OpenType and WOFF1/WOFF2 program bytes; a provider that returns any other
// program format (e.g. a classic Type1 PFB) is logged and skipped, and resolution
// falls through to the bundled substitute — pkg/font exposes no general public loader
// for arbitrary program bytes, and the bundled DiskFontProvider only serves sfnt/WOFF
// files. Returns false when no Provider is configured or it has no match. Caller holds
// c.mu.
func (c *FaceCache) resolveProvider(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	prov, ok := c.sys.(pkgfont.Provider)
	if !ok || prov == nil {
		return nil, false
	}
	raw, ok := prov.LoadStyled(family, style.Bold, style.Italic)
	if !ok {
		return nil, false
	}
	face, err := pkgfont.LoadSFNT(raw)
	if err != nil {
		c.logf("font provider %q: decode failed (non-sfnt program?): %v", family, err)
		return nil, false
	}
	return face, true
}

// splitFamilyList splits a (already-cleaned) comma-separated font-family list into
// its candidate names, dropping empties. A single bare name yields one element, so
// callers need not special-case the non-list form.
func splitFamilyList(family string) []string {
	parts := strings.Split(family, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveFontFace walks the @font-face entries for family (best style match first),
// trying each source in order: local() via the system provider, url() via the
// loader. The first that decodes wins. Returns false when there is no @font-face
// for the family or none of its sources resolve. Caller holds c.mu.
func (c *FaceCache) resolveFontFace(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	entries := c.fontFaces[normalizeFamily(family)]
	if len(entries) == 0 {
		return nil, false
	}
	for _, ff := range bestFirst(entries, style) {
		for _, src := range ff.Sources {
			var raw []byte
			switch {
			case src.Local != "":
				if c.sys == nil {
					continue
				}
				b, ok := c.sys.LoadLocal(src.Local)
				if !ok {
					continue
				}
				raw = b
			case src.URL != "":
				if c.loader == nil {
					continue
				}
				b, _, err := c.loader.Load(context.Background(), src.URL)
				if err != nil {
					c.logf("@font-face %q: fetch %q failed: %v", family, src.URL, err)
					continue
				}
				raw = b
			default:
				continue
			}
			face, err := pkgfont.LoadSFNT(raw)
			if err != nil {
				c.logf("@font-face %q: decode failed: %v", family, err)
				continue
			}
			return face, true
		}
	}
	return nil, false
}

// bestFirst orders @font-face entries so the one best matching style comes first:
// exact weight+style, then a regular/unspecified entry, then the rest in source
// order. This is a coarse match — full font-weight numeric matching is a deferral.
func bestFirst(entries []gcss.FontFace, style pkgfont.Style) []gcss.FontFace {
	wantBold := style.Bold
	wantItalic := style.Italic
	score := func(ff gcss.FontFace) int {
		ffBold := ff.Weight == "bold" || ff.Weight == "700"
		ffItalic := ff.Style == "italic" || ff.Style == "oblique"
		s := 0
		if ffBold == wantBold {
			s += 2
		}
		if ffItalic == wantItalic {
			s++
		}
		return s
	}
	out := make([]gcss.FontFace, len(entries))
	copy(out, entries)
	// Insertion sort by DESCENDING score, stable (keeps source order within equal
	// scores). Avoids sort.Slice/modernize friction; entry counts are tiny.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && score(out[j]) > score(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
