package doctaculous

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/epub"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// OpenEPUB reads and renders an EPUB book: the spine documents concatenate in
// reading order (each chapter starting a new page when paginated), with the
// book's stylesheets, images, and fonts resolving from the container. For
// additional options use OpenEPUBFile.
func OpenEPUB(path string) (*Document, error) {
	return OpenEPUBFile(path)
}

// OpenEPUBFile reads and renders an .epub file at path, applying any options.
func OpenEPUBFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open epub %q: %w", path, err)
	}
	return OpenEPUBBytes(data, opts...)
}

// OpenEPUBBytes renders an in-memory book, applying any options, and returns
// a Document ready to rasterize or convert. EPUB content is XHTML, so the
// whole HTML pipeline applies — package CSS included — with the container
// backing resource resolution (a caller's own WithResourceLoader overrides
// it and loses the container's media). DRM-protected books are refused
// (epub.ErrEncrypted).
func OpenEPUBBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	book, err := epub.OpenBytes(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: %w", err)
	}
	all := append([]HTMLOption{WithResourceLoader(epubLoader{book: book})}, opts...)
	doc, err := OpenHTMLBytes([]byte(bookToHTML(book)), all...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatEPUB
	return doc, nil
}

// bookToHTML assembles the merged document: the collected styling in the
// head, each chapter's body markup wrapped in a page-breaking section.
func bookToHTML(b *epub.Book) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n")
	if b.Title != "" {
		sb.WriteString("<title>" + htmlEscaper.Replace(b.Title) + "</title>\n")
	}
	for _, ref := range b.StylesheetRefs {
		sb.WriteString(`<link rel="stylesheet" href="` + htmlEscaper.Replace(ref) + `">` + "\n")
	}
	for _, css := range b.InlineCSS {
		sb.WriteString("<style>\n" + css + "\n</style>\n")
	}
	sb.WriteString("</head>\n<body>\n")
	for i, chapter := range b.Chapters {
		style := ""
		if i > 0 {
			style = ` style="break-before: page"`
		}
		sb.WriteString("<section" + style + ">\n" + chapter + "\n</section>\n")
	}
	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

// epubLoader adapts the book's container to the resource loader seam.
type epubLoader struct {
	book *epub.Book
}

// Load resolves a chapter-relative reference from the container.
func (l epubLoader) Load(_ context.Context, ref string) ([]byte, string, error) {
	data, ok := l.book.Resource(ref)
	if !ok {
		return nil, "", fmt.Errorf("epub resource %q: %w", ref, resource.ErrNotFound)
	}
	return data, epubContentType(ref), nil
}

// epubContentType maps a resource extension to its content type ("" lets the
// engine sniff).
func epubContentType(ref string) string {
	switch strings.ToLower(path.Ext(ref)) {
	case ".css":
		return "text/css"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}
