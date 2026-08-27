package css

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/resource"
	"github.com/nathanstitt/doctaculous/pkg/svg"
	svgdraw "github.com/nathanstitt/doctaculous/pkg/svg/draw"
)

// svgContentType is the IANA media type for SVG. A resource whose loader reports
// it takes the VECTOR path (parsed into an svg.Document and painted through
// layout.VectorItem); everything else goes to imageCache and is rasterized.
const svgContentType = "image/svg+xml"

// parsedSVG is a memoized svg.Parse result for one source ref: the parsed
// document plus ok=false for a ref that is not SVG at all, could not be fetched,
// or could not be parsed. Mirrors decodedImage, including caching misses so a
// broken ref is not re-fetched on every reference.
type parsedSVG struct {
	doc *svg.Document
	ok  bool
	// wasSVG records that the ref's content type WAS image/svg+xml even though
	// the parse failed. It is what lets the caller tell "this <img> is a PNG,
	// hand it to the raster path" (wasSVG false) from "this <img> is a broken
	// SVG, degrade to an empty box" (wasSVG true) — without it, a malformed SVG
	// would fall through to image.Decode and be reported as an image failure.
	wasSVG bool
}

// svgCache resolves a source ref to a parsedSVG through a ResourceLoader,
// caching each result (including misses). It is the VECTOR counterpart to
// imageCache: SVG must never travel as an image.Image, because that would force
// a bitmap round trip and lose the resolution independence that is the whole
// point of the vector path. Safe for concurrent use. Build with newSVGCache.
type svgCache struct {
	loader resource.ResourceLoader
	logf   func(string, ...any)

	mu    sync.Mutex
	byRef map[string]parsedSVG
}

// newSVGCache returns an empty cache backed by loader (which may be nil, in which
// case every lookup misses) and logging degraded parses through logf (a nil logf
// is a no-op).
func newSVGCache(loader resource.ResourceLoader, logf func(string, ...any)) *svgCache {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &svgCache{loader: loader, logf: logf, byRef: make(map[string]parsedSVG)}
}

// get returns the parsed SVG document for ref, loading and parsing it on first
// use and caching the result (including a miss). A ref that resolves to a
// non-SVG content type is a miss WITHOUT a log — that is the ordinary case of an
// <img> pointing at a PNG, which the raster path then handles. A miss from a
// genuine failure (unfetchable, unparseable) is logged and the caller degrades to
// a sized placeholder.
//
// Like imageCache.get, a miss caused by a TRANSIENT context cancellation is not
// cached, so a later render with a live context can still succeed.
func (c *svgCache) get(ctx context.Context, ref string) parsedSVG {
	if ref == "" {
		return parsedSVG{}
	}

	c.mu.Lock()
	if e, found := c.byRef[ref]; found {
		c.mu.Unlock()
		return e
	}
	c.mu.Unlock()

	e, transient := c.parse(ctx, ref)
	if transient {
		return e // do not poison the cache with a transient (cancellation) miss
	}

	c.mu.Lock()
	c.byRef[ref] = e
	c.mu.Unlock()
	return e
}

// parse fetches ref and parses it as SVG, returning a miss on any failure.
// transient reports whether the miss came from a context cancellation/deadline.
//
// The SVG discrimination is by CONTENT TYPE only, never by sniffing the bytes: a
// loader that cannot name the type (an unknown extension) yields an empty content
// type, and treating that as maybe-SVG would run the XML parser over every
// unrecognized binary blob. The raster path's image.Decode sniffing already
// covers the unknown-type case for the formats it knows.
func (c *svgCache) parse(ctx context.Context, ref string) (p parsedSVG, transient bool) {
	var (
		data        []byte
		contentType string
		err         error
	)
	switch {
	case strings.HasPrefix(ref, "data:"):
		// A data: URI carries its own bytes and its own type — decode regardless
		// of loader, matching imageCache.decode.
		data, contentType, err = resource.LoadDataURL(ref)
	case c.loader == nil:
		return parsedSVG{}, false
	default:
		data, contentType, err = c.loader.Load(ctx, ref)
	}
	if err != nil {
		// Not logged: imageCache.decode logs the same load failure for the same
		// ref, and logging twice for one broken <img> is noise.
		return parsedSVG{}, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	}
	if normalizeContentType(contentType) != svgContentType {
		return parsedSVG{}, false // not SVG: the raster path owns this ref
	}
	doc, err := parseSVGBytes(data, ref, c.logf)
	if err != nil {
		return parsedSVG{wasSVG: true}, false
	}
	return parsedSVG{doc: doc, ok: true, wasSVG: true}, false
}

// inlineSVGCache memoizes svg.Parse over INLINE <svg> markup, keyed by the markup
// itself. It is separate from svgCache because there is no ref and no loader: box
// generation already re-serialized the subtree, so the source bytes are in hand.
// Memoizing still matters — replacedUsedSize and replacedFragment each resolve the
// same box, and intrinsic-width measurement can resolve it several more times, so
// without a cache one inline <svg> is parsed repeatedly per layout.
type inlineSVGCache struct {
	mu       sync.Mutex
	byMarkup map[string]parsedSVG
}

// newInlineSVGCache returns an empty inline-markup cache.
func newInlineSVGCache() *inlineSVGCache {
	return &inlineSVGCache{byMarkup: make(map[string]parsedSVG)}
}

// get returns the parsed document for inline SVG markup, parsing on first use and
// caching the result (including a failed parse, so malformed inline markup is not
// re-parsed on every reference). ok is false for a failed parse; the caller
// reserves the box and paints nothing.
func (c *inlineSVGCache) get(markup string, logf func(string, ...any)) parsedSVG {
	if markup == "" {
		return parsedSVG{wasSVG: true}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, found := c.byMarkup[markup]; found {
		return e
	}
	doc, err := parseSVGBytes([]byte(markup), "", logf)
	e := parsedSVG{doc: doc, ok: err == nil && doc != nil, wasSVG: true}
	c.byMarkup[markup] = e
	return e
}

// maxSVGBytes caps the size of an SVG referenced from a document. An <img src>
// is untrusted input, and svg.Parse's own budgets (the <use> instantiation
// budget, the <text> character budget) bound EXPANSION, not the size of the
// source itself; a multi-hundred-megabyte SVG would still be parsed into a scene
// tree before any of them applied. A real-world embedded SVG is orders of
// magnitude below this.
const maxSVGBytes = 32 << 20

// parseSVGBytes parses SVG source into a Document, refusing input over
// maxSVGBytes and never panicking: svg.Parse is total on malformed XML (it
// recovers and returns a partial scene), but a recover here keeps a future parser
// bug from taking the whole layout down, since this runs on untrusted input.
// ref names the source in diagnostics; pass "" for inline markup.
func parseSVGBytes(data []byte, ref string, logf func(string, ...any)) (doc *svg.Document, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	where := "inline <svg>"
	if ref != "" {
		where = "svg " + ref
	}
	if len(data) > maxSVGBytes {
		logf("css layout: %s is %d bytes, over the %d-byte limit; skipped", where, len(data), maxSVGBytes)
		return nil, errSVGTooLarge
	}
	defer func() {
		if r := recover(); r != nil {
			logf("css layout: parse %s panicked: %v; skipped", where, r)
			doc, err = nil, errSVGParse
		}
	}()
	doc, err = svg.Parse(data, logf)
	if err != nil {
		logf("css layout: parse %s failed: %v", where, err)
		return nil, err
	}
	return doc, nil
}

var (
	errSVGTooLarge = errors.New("svg source exceeds the size limit")
	errSVGParse    = errors.New("svg parse failed")
)

// svgIntrinsic reports doc's un-defaulted intrinsic sizing contribution in the
// form replacedUsedSize consumes: an absolute size when the SVG states one, else
// an aspect ratio, else nothing (so the caller applies the CSS 300x150 default).
//
// When only ONE axis is stated and a viewBox ratio exists, the other axis is
// derived from the ratio, which yields a definite size with the right ratio —
// the same answer resolveSize's rule 4 gives, reached without the 300x150 default
// ever entering.
func svgIntrinsic(doc *svg.Document) (iw, ih float64, ok bool) {
	in := doc.Intrinsic()
	switch {
	case in.HasWidth && in.HasHeight:
		return in.Width, in.Height, true
	case in.HasWidth && in.HasRatio:
		return in.Width, in.Width * in.RatioH / in.RatioW, true
	case in.HasHeight && in.HasRatio:
		return in.Height * in.RatioW / in.RatioH, in.Height, true
	case in.HasRatio:
		// Ratio only: report the viewBox extent. It carries the correct ratio, so
		// replacedUsedSize derives the other axis correctly when CSS supplies one,
		// and uses it as the intrinsic size when CSS supplies neither — matching
		// what a browser does with a viewBox-only SVG.
		return in.RatioW, in.RatioH, true
	case in.HasWidth:
		return in.Width, 0, false // a width with no ratio gives no usable ratio
	case in.HasHeight:
		return 0, in.Height, false
	default:
		return 0, 0, false // no size, no ratio: the caller defaults to 300x150
	}
}

// svgDefaultW, svgDefaultH are the CSS replaced-element defaults applied when an
// embedded SVG states neither a size nor a ratio (CSS Images 3 §5.1). They match
// pkg/svg's own defaults; duplicated rather than exported from there because they
// are a CSS rule the HOST applies, not an SVG fact.
const (
	svgDefaultW = 300.0
	svgDefaultH = 150.0
)

// newSVGScene wraps a parsed document as a layout.VectorScene ready for a
// VectorItem. It is the one place pkg/layout/css names pkg/svg/draw, so the
// vector seam stays a single, visible connection.
func newSVGScene(doc *svg.Document) layout.VectorScene { return svgdraw.New(doc) }

// scaledScene adapts a scene authored at (srcW, srcH) points to a viewport of a
// different size, by pre-multiplying a scale into the ctm.
//
// An svg.Renderer draws in the document's OWN viewport coordinates
// (Document.WidthPt/HeightPt), and paint.paintVector hands it only a translate to
// the item's box — the standalone SVG frontend makes the box exactly the document
// size, so no scale is needed there. An EMBEDDED SVG's used size comes from the
// host's CSS and routinely differs (`<img src="icon.svg" width="24">` on a 512pt
// icon), so the scene must be scaled into its box. Doing it in the ctm keeps the
// output vector: it is a coordinate transform, not a resampling.
//
// The scale is per-axis and non-uniform on purpose. The SVG's own
// preserveAspectRatio already mapped its viewBox into its viewport; the host's
// CSS then sizes that viewport, and CSS replaced-element sizing is what preserves
// (or does not preserve) the ratio at this level.
type scaledScene struct {
	inner      layout.VectorScene
	srcW, srcH float64
}

// DrawVector implements layout.VectorScene, drawing the inner scene scaled from
// its authored size into the item's box. ctm already maps the box's top-left to
// device space, so the scale composes on the LEFT (applied first).
func (s scaledScene) DrawVector(dev render.Device, ctm render.Matrix) {
	s.inner.DrawVector(dev, render.Scale(s.srcW, s.srcH).Mul(ctm))
}

// fitSceneTo returns a scene that draws doc into a boxW x boxH viewport. When the
// box already matches the document's own viewport (the overwhelmingly common case
// of an unsized <img>, and every standalone SVG), the renderer is returned
// unwrapped so the drawn output is bit-for-bit what the standalone path produces.
func fitSceneTo(doc *svg.Document, boxW, boxH float64) layout.VectorScene {
	scene := newSVGScene(doc)
	if doc.WidthPt <= 0 || doc.HeightPt <= 0 {
		return scene
	}
	sx, sy := boxW/doc.WidthPt, boxH/doc.HeightPt
	const tol = 1e-9
	if math.Abs(sx-1) < tol && math.Abs(sy-1) < tol {
		return scene
	}
	return scaledScene{inner: scene, srcW: sx, srcH: sy}
}
