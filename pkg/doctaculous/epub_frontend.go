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
	cover := coverSection(b)
	sb.WriteString(cover)
	for i, chapter := range b.Chapters {
		style := ""
		if i > 0 || cover != "" {
			style = ` style="break-before: page"`
		}
		sb.WriteString("<section" + style + ">\n" + chapter + "\n</section>\n")
	}
	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

// coverSection renders the book's cover image as a leading section, or "" when
// there is nothing to render.
//
// The cover goes FIRST, before the spine, because that is what a cover is — the
// front of the book — and because it is the only place it can go: an EPUB's
// cover-image manifest item is not part of the reading order, so it has no
// position within the spine to occupy. Making it a section of its own means the
// first chapter's break-before puts it alone on page 1 when paginated, which is
// the printed-book behavior, while an unpaginated (single tall page) render
// simply starts with the image.
//
// Two cases yield "": a book with no declared cover (nothing changes for it at
// all), and a book whose cover is ALSO reachable from the spine — either as a
// spine document or referenced by one — where prepending it would show the same
// image twice.
//
// The image is constrained to the page width rather than placed at its intrinsic
// size: a real cover is typically far larger than the page, and an unconstrained
// one would overflow and be clipped to a corner crop. max-width: 100% scales it
// down whole, preserving its aspect ratio through the replaced-element ratio
// constraint, and works identically for a raster cover and an SVG one (an SVG
// cover reaches the page through the same vector seam as any other
// <img src="*.svg">, so it stays vector and stays sharp).
//
// Only the WIDTH is constrained. A height bound would be the more faithful fit
// for a portrait cover on a short page, but the engine has no viewport-relative
// unit (no vh), and a percentage height on a replaced element has no basis in its
// single-axis model — both would be dropped, one of them silently. A cover taller
// than the page overflows onto the next page instead of being cropped, which is
// the better failure.
func coverSection(b *epub.Book) string {
	if b.CoverHref == "" || b.CoverInSpine {
		return ""
	}
	alt := "Cover"
	if b.Title != "" {
		alt = b.Title + " cover"
	}
	return `<section class="epub-cover" style="text-align: center">` + "\n" +
		`<img src="` + htmlEscaper.Replace(b.CoverHref) + `" alt="` + htmlEscaper.Replace(alt) + `"` +
		` style="max-width: 100%">` + "\n" +
		"</section>\n"
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
	case ".heic", ".heif":
		return "image/heic"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}
