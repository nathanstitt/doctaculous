package css

import (
	"bytes"
	"context"
	"errors"
	"image"
	"strings"
	"sync"

	// Blank-imported for their decoders' side-effect registration with the
	// image package, so image.Decode can sniff PNG/JPEG/GIF and the
	// content-type fast paths below can call the format decoders directly. All
	// three are standard library and pure Go (the PDF raster backend already uses
	// image/jpeg for DCTDecode), so no new dependency is introduced.
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// decodedImage is a memoized image-decode result for one source ref. It records
// the decoded image and its intrinsic pixel size, plus ok=false for a failed
// decode (missing ref, unsupported format, malformed bytes) so a broken src is
// not re-fetched and re-decoded on every reference. It mirrors the face cache's
// cacheEntry, which likewise caches misses.
type decodedImage struct {
	img  image.Image
	w, h float64 // intrinsic size in pixels (treated 1:1 as points), 0 when !ok
	ok   bool
}

// imageCache resolves a source ref to a decodedImage through a ResourceLoader,
// caching each result (including misses). It is safe for concurrent use, so the
// engine's render fan-out can share one cache without locks of its own. A nil
// loader resolves everything to a miss (no ref can be fetched). Build with
// newImageCache.
type imageCache struct {
	loader resource.ResourceLoader
	logf   func(string, ...any)

	mu    sync.Mutex
	byRef map[string]decodedImage
}

// newImageCache returns an empty cache backed by loader (which may be nil, in
// which case every lookup misses) and logging degraded decodes through logf (a
// nil logf is a no-op).
func newImageCache(loader resource.ResourceLoader, logf func(string, ...any)) *imageCache {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &imageCache{loader: loader, logf: logf, byRef: make(map[string]decodedImage)}
}

// get returns the decoded image for ref, loading and decoding it on first use
// and caching the result (including a miss). An empty ref, a nil loader, a
// not-found ref, an unsupported format, or malformed bytes all resolve to a miss
// (ok=false) and are logged once (when first encountered); the caller degrades to
// a placeholder. get honors ctx cancellation via the loader.
//
// A miss caused by a TRANSIENT context cancellation/deadline is not cached, so a
// later render of the same ref with a live context can still succeed; permanent
// misses (not-found, undecodable, unsupported) are cached so a broken ref is not
// re-fetched on every reference.
func (c *imageCache) get(ctx context.Context, ref string) decodedImage {
	if ref == "" {
		return decodedImage{}
	}

	c.mu.Lock()
	if e, found := c.byRef[ref]; found {
		c.mu.Unlock()
		return e
	}
	c.mu.Unlock()

	e, transient := c.decode(ctx, ref)
	if transient {
		return e // do not poison the cache with a transient (cancellation) miss
	}

	c.mu.Lock()
	c.byRef[ref] = e
	c.mu.Unlock()
	return e
}

// decode fetches ref through the loader and decodes its bytes into an image,
// returning a miss (and logging) on any failure. The content type chooses the
// decoder; an empty/unknown content type falls back to image.Decode, which
// sniffs the format from the bytes (the blank imports register PNG/JPEG/GIF).
// transient reports whether the miss was caused by a context cancellation/deadline
// (so the caller can avoid caching it); permanent failures report false.
func (c *imageCache) decode(ctx context.Context, ref string) (d decodedImage, transient bool) {
	if c.loader == nil {
		c.logf("css layout: no resource loader; cannot decode image %q", ref)
		return decodedImage{}, false
	}
	data, contentType, err := c.loader.Load(ctx, ref)
	if err != nil {
		c.logf("css layout: load image %q failed: %v", ref, err)
		return decodedImage{}, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}
	img, err := decodeImageBytes(data, contentType)
	if err != nil {
		c.logf("css layout: decode image %q (%s) failed: %v", ref, contentType, err)
		return decodedImage{}, false
	}
	b := img.Bounds()
	return decodedImage{img: img, w: float64(b.Dx()), h: float64(b.Dy()), ok: true}, false
}

// decodeImageBytes decodes image bytes using the decoder named by contentType,
// falling back to image.Decode (format sniffing) when the content type is empty
// or not one of the recognized raster types. Returns an error for an
// undecodable/unsupported format; the caller turns that into a graceful miss.
func decodeImageBytes(data []byte, contentType string) (image.Image, error) {
	switch normalizeContentType(contentType) {
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/gif":
		return gif.Decode(bytes.NewReader(data))
	default:
		// Unknown/empty content type (e.g. a DirLoader extension it doesn't map):
		// sniff the format from the bytes. image.Decode uses the formats registered
		// by the blank imports above, so PNG/JPEG/GIF still decode; anything else
		// (SVG, WebP, ...) returns image.ErrFormat, which the caller degrades.
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
}

// normalizeContentType lower-cases a content type and strips any parameters
// (e.g. "image/png; charset=binary" -> "image/png") so the switch matches the
// bare MIME type.
func normalizeContentType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}
