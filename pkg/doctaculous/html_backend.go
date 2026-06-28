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
	viewportPt float64
	loader     resource.ResourceLoader
	sys        layoutfont.SystemFontProvider
	logf       func(string, ...any)
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
// the file's directory.
func OpenHTML(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open html %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	return OpenHTMLBytes(data,
		WithResourceLoader(resource.DirLoader{Base: dir}),
		WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: dir}),
	)
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
	root, fontFaces, err := layoutcss.BuildWithFonts(ctx, doc, cfg.loader, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: build html boxes: %w", err)
	}
	faces := layoutfont.NewFaceCacheWithFonts(fontFaces, cfg.loader, cfg.sys, cfg.logf)
	engine := layoutcss.New(faces, cfg.loader, cfg.logf)
	pages, err := engine.Layout(ctx, root, cfg.viewportPt)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: layout html: %w", err)
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
