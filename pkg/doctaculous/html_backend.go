package doctaculous

import (
	"context"
	"fmt"
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
	return OpenHTMLBytes(data, WithResourceLoader(resource.DirLoader{Base: filepath.Dir(path)}))
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
	root, err := layoutcss.Build(ctx, doc, cfg.loader, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: build html boxes: %w", err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCache(), cfg.loader, cfg.logf)
	pages, err := engine.Layout(ctx, root, cfg.viewportPt)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: layout html: %w", err)
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
