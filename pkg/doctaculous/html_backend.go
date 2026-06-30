package doctaculous

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// HTMLOption configures HTML layout via OpenHTML / OpenHTMLBytes.
type HTMLOption func(*htmlConfig)

// htmlConfig is the resolved HTML layout configuration.
type htmlConfig struct {
	viewportPt   float64
	pageHeightPt float64
	// paged requests pagination using the document's @page rules (set by
	// WithDefaultPaged or WithPageSize). explicitSize records that WithPageSize set an
	// explicit page size, which overrides any @page `size`.
	paged        bool
	explicitSize bool
	loader       resource.ResourceLoader
	sys          layoutfont.SystemFontProvider
	logf         func(string, ...any)
}

// defaultViewportPt is the default layout viewport width in points (px:pt 1:1).
// A fixed desktop width so a page lays out as a full-page capture (the single
// tall image model); content flows to whatever height it needs.
const defaultViewportPt = 1280

// defaultHTMLConfig returns the baseline configuration before options are
// applied: the default viewport width, no loader (links are skipped), and a
// no-op logger.
func defaultHTMLConfig() htmlConfig {
	return htmlConfig{viewportPt: defaultViewportPt, loader: nil, logf: nil}
}

// WithViewportWidth sets the layout viewport width in CSS pixels (treated 1:1 as
// points). Defaults to 1280. Values <= 0 are ignored.
func WithViewportWidth(px float64) HTMLOption {
	return func(c *htmlConfig) {
		if px > 0 {
			c.viewportPt = px
		}
	}
}

// LetterWidthPt / LetterHeightPt are US-Letter (8.5in × 11in) at 96dpi (px:pt 1:1),
// the conventional default page size for WithPageSize.
const (
	LetterWidthPt  = 816
	LetterHeightPt = 1056
)

// WithPageSize paginates output into fixed widthPt × heightPt (points) pages: the
// document lays out at widthPt and is sliced into heightPt-tall pages, breaking
// between top-level blocks and at forced page breaks (CSS break-before/after: page),
// honoring break-inside / widows / orphans. The explicit size overrides any @page
// `size`, but @page margins and margin boxes (running headers/footers) still apply.
// Without WithPageSize (or WithDefaultPaged) the document renders as a single tall
// page (the default). widthPt or heightPt <= 0 is ignored (no pagination).
func WithPageSize(widthPt, heightPt float64) HTMLOption {
	return func(c *htmlConfig) {
		if widthPt > 0 && heightPt > 0 {
			c.viewportPt = widthPt
			c.pageHeightPt = heightPt
			c.paged = true
			c.explicitSize = true
		}
	}
}

// WithDefaultPaged paginates output using the document's @page rules (size, margins,
// and margin boxes) when present, falling back to US-Letter (LetterWidthPt ×
// LetterHeightPt) for any dimension the document does not specify. Unlike WithPageSize
// it sets no explicit size, so an @page `size` is honored. Without it (and without
// WithPageSize) the document renders as a single tall page.
func WithDefaultPaged() HTMLOption {
	return func(c *htmlConfig) {
		c.paged = true
		c.explicitSize = false
		if c.pageHeightPt <= 0 {
			c.pageHeightPt = LetterHeightPt
		}
		c.viewportPt = LetterWidthPt
	}
}

// WithResourceLoader sets the loader used to resolve <link> stylesheet refs (and,
// later, images/fonts). Defaults to no loader (links are skipped). OpenHTML
// supplies a DirLoader rooted at the document's directory.
func WithResourceLoader(l resource.ResourceLoader) HTMLOption {
	return func(c *htmlConfig) { c.loader = l }
}

// WithSystemFontProvider sets the provider used to resolve @font-face local()
// sources. Defaults to nil (local() never matches; the next src is tried). OpenHTML
// supplies a DiskFontProvider rooted at the document's directory.
func WithSystemFontProvider(p layoutfont.SystemFontProvider) HTMLOption {
	return func(c *htmlConfig) { c.sys = p }
}

// WithLogf sets a logger for layout/degradation diagnostics (may be called during
// Build and Layout). Defaults to a no-op.
func WithLogf(f func(string, ...any)) HTMLOption {
	return func(c *htmlConfig) { c.logf = f }
}

// OpenHTML reads and renders an HTML file at path, laying it out at the default
// viewport width into a single tall page, and returns a Document ready to
// rasterize. Relative <link> stylesheet refs resolve through a loader rooted at
// the file's directory. For additional options (e.g. WithPageSize), use
// OpenHTMLFile.
func OpenHTML(path string) (*Document, error) {
	return OpenHTMLFile(path)
}

// OpenHTMLFile reads and renders an HTML file at path, applying any options, and
// returns a Document ready to rasterize. Like OpenHTML it roots a DirLoader and a
// DiskFontProvider at the file's directory so relative <link>/<img>/@font-face refs
// resolve from disk; the caller's opts are applied AFTER those defaults, so e.g.
// WithPageSize(LetterWidthPt, LetterHeightPt) paginates the file, and a caller's own
// WithResourceLoader overrides the directory loader.
func OpenHTMLFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open html %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	all := append([]HTMLOption{
		WithResourceLoader(resource.DirLoader{Base: dir}),
		WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: dir}),
	}, opts...)
	return OpenHTMLBytes(data, all...)
}

// ErrUnsupportedScheme is returned (wrapped) by OpenURL when rawURL uses a scheme
// other than http or https, so callers can branch on it via errors.Is.
var ErrUnsupportedScheme = errors.New("unsupported URL scheme")

// OpenURL fetches the HTML document at rawURL over HTTP(S), lays it out at the
// default viewport width into a single tall page, and returns a Document ready to
// rasterize. Relative <link>/<img>/@font-face refs resolve against rawURL and are
// fetched over HTTP (data: sub-resource refs are decoded inline) through an
// HTTPLoader rooted at rawURL; rawURL itself must be http or https (a non-http(s)
// scheme returns ErrUnsupportedScheme). Options (e.g. WithViewportWidth, WithLogf,
// WithSystemFontProvider) may be supplied and take effect after the loader is set.
// Unlike OpenHTML, no system font provider is configured by default (a URL has no
// local font directory), so @font-face local() sources do not match unless one is
// supplied.
func OpenURL(rawURL string, opts ...HTMLOption) (*Document, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("doctaculous: open url: empty URL")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("doctaculous: open url %q: %w (%q)", rawURL, ErrUnsupportedScheme, u.Scheme)
	}
	loader := resource.HTTPLoader{Base: u}
	data, _, err := loader.Load(context.Background(), "")
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open url %q: %w", rawURL, err)
	}
	allOpts := append([]HTMLOption{WithResourceLoader(loader)}, opts...)
	return OpenHTMLBytes(data, allOpts...)
}

// OpenHTMLBytes parses and renders in-memory HTML, applying any options, and
// returns a Document ready to rasterize.
func OpenHTMLBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	cfg := defaultHTMLConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return htmlDocument(data, cfg)
}

// htmlDocument runs the HTML pipeline — parse → box generation → CSS layout —
// and wraps the resulting pages for rasterization, mirroring docxDocument. Layout
// runs once here (the document lays out into a single tall page); rasterization
// then proceeds over that page.
func htmlDocument(data []byte, cfg htmlConfig) (*Document, error) {
	doc, err := html.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: parse html: %w", err)
	}
	ctx := context.Background()
	root, fontFaces, pageRules, err := layoutcss.BuildWithFontsAndPages(ctx, doc, cfg.loader, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: build html boxes: %w", err)
	}
	faces := layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, cfg.sys, cfg.logf)
	engine := layoutcss.New(faces, cfg.loader, cfg.logf)
	pages, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged:        cfg.paged,
		FallbackW:    cfg.viewportPt,
		FallbackH:    cfg.pageHeightPt,
		ExplicitSize: cfg.explicitSize,
		Pages:        pageRules,
	})
	if err != nil {
		return nil, fmt.Errorf("doctaculous: layout html: %w", err)
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
