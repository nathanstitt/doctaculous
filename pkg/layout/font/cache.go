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
	"sync"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
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
}

// NewFaceCache returns an empty cache ready for use.
func NewFaceCache() *FaceCache {
	return &FaceCache{faces: make(map[faceKey]cacheEntry)}
}

// Resolve returns the face for family in the requested style, substituting a
// bundled look-alike via pkg/font. ok is false when no substitute is bundled for
// the family (the caller skips affected runs). Results — including misses — are
// cached, so repeated calls for the same (family, style) are cheap.
func (c *FaceCache) Resolve(family string, style pkgfont.Style) (*pkgfont.Face, bool) {
	key := faceKey{family: family, style: style}

	c.mu.Lock()
	defer c.mu.Unlock()
	if e, found := c.faces[key]; found {
		return e.face, e.ok
	}
	face, ok := pkgfont.LoadStandard(family, style)
	c.faces[key] = cacheEntry{face: face, ok: ok}
	return face, ok
}
