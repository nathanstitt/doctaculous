package omnidoc

import (
	"fmt"
	"os"
	"path/filepath"

	mdfront "github.com/nathanstitt/omnidoc/pkg/internal/markdown"
	"github.com/nathanstitt/omnidoc/pkg/resource"
)

// OpenMarkdownFile reads and renders a Markdown file at path, applying any
// options. Like OpenHTMLFile it roots a DirLoader and a DirFontProvider at
// the file's directory, so relative image refs in the Markdown resolve from
// disk; the caller's opts are applied after those defaults and win.
func OpenMarkdownFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: open markdown %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	all := append([]HTMLOption{
		WithResourceLoader(resource.DirLoader{Base: dir}),
		WithSystemFontProvider(DirFontProvider{Dir: dir}),
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
	err = publicNestingErr(err)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: %w", err)
	}
	doc, err := OpenHTMLBytes(htmlData, opts...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatMarkdown
	return doc, nil
}
