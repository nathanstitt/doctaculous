package doctaculous

import (
	"fmt"
	"os"
	"path/filepath"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	mdfront "github.com/nathanstitt/doctaculous/pkg/markdown"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// OpenMarkdown reads and renders a Markdown (.md) file, laying it out at the
// default viewport width into a single tall page. For additional options (e.g.
// WithPageSize) use OpenMarkdownFile.
func OpenMarkdown(path string) (*Document, error) {
	return OpenMarkdownFile(path)
}

// OpenMarkdownFile reads and renders a Markdown file at path, applying any
// options. Like OpenHTMLFile it roots a DirLoader and a DiskFontProvider at
// the file's directory, so relative image refs in the Markdown resolve from
// disk; the caller's opts are applied after those defaults and win.
func OpenMarkdownFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open markdown %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	all := append([]HTMLOption{
		WithResourceLoader(resource.DirLoader{Base: dir}),
		WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: dir}),
	}, opts...)
	return OpenMarkdownBytes(data, all...)
}

// OpenMarkdownBytes parses and renders in-memory Markdown (CommonMark + GFM:
// tables, strikethrough, task lists, autolinks), applying any options, and
// returns a Document ready to rasterize or convert. The source is converted to
// HTML (with an embedded GitHub-flavored default stylesheet) and flows through
// the HTML pipeline, so every HTMLOption applies.
func OpenMarkdownBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	htmlData, err := mdfront.ToHTML(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: %w", err)
	}
	doc, err := OpenHTMLBytes(htmlData, opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatMarkdown
	return doc, nil
}
