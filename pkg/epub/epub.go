// Package epub is a read-only EPUB reader: it opens the container, walks
// META-INF/container.xml to the OPF package document, and extracts the spine
// documents' body markup in reading order plus their stylesheets — the pieces
// the HTML pipeline lays out (EPUB content IS XHTML, so the reflow engine does
// the real work). EPUB 2 and 3 both resolve through the spine; the NCX is not
// consulted (it duplicates the spine's order). DRM-protected books
// (META-INF/encryption.xml) are refused with ErrEncrypted.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// ErrNotEPUB reports input that is not an EPUB container.
var ErrNotEPUB = errors.New("epub: not an epub file")

// ErrEncrypted reports a DRM-protected book the reader cannot open.
var ErrEncrypted = errors.New("epub: encrypted (DRM-protected) books are not supported")

// maxPartSize caps any single decompressed part, mirroring the OOXML readers.
const maxPartSize = 256 << 20

// Book is a parsed EPUB: its metadata title, the spine documents' body markup
// in reading order, and the collected styling, with the container retained for
// resource resolution (images, fonts, linked CSS).
type Book struct {
	// Title is the OPF dc:title, or "".
	Title string
	// Chapters holds each spine document's body inner markup, reading order.
	Chapters []string
	// StylesheetRefs are the hrefs of the chapters' <link rel=stylesheet>
	// references, resolved to container part names, deduplicated in first-use
	// order.
	StylesheetRefs []string
	// InlineCSS holds the chapters' <style> block contents in order.
	InlineCSS []string

	parts      map[string][]byte
	contentDir string
}

// Open reads and parses the book at path.
func Open(pathName string) (*Book, error) {
	data, err := os.ReadFile(pathName)
	if err != nil {
		return nil, err
	}
	return OpenBytes(data)
}

// OpenBytes parses a book from an in-memory byte slice.
func OpenBytes(data []byte) (*Book, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEPUB, err)
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxPartSize+1))
		_ = rc.Close()
		if err != nil || len(b) > maxPartSize {
			continue
		}
		parts[strings.TrimPrefix(f.Name, "/")] = b
	}
	if _, ok := parts["META-INF/container.xml"]; !ok {
		return nil, fmt.Errorf("%w: missing META-INF/container.xml", ErrNotEPUB)
	}
	if _, ok := parts["META-INF/encryption.xml"]; ok {
		return nil, ErrEncrypted
	}
	return parseBook(parts)
}

// Resource resolves a chapter-relative reference (an image, font, or linked
// stylesheet) to its bytes. Refs resolve against the OPF's directory — the
// layout every real-world EPUB uses (all content under one OPS/OEBPS
// directory); fragment and query suffixes are stripped.
func (b *Book) Resource(ref string) ([]byte, bool) {
	if i := strings.IndexAny(ref, "#?"); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" {
		return nil, false
	}
	name := path.Clean(path.Join(b.contentDir, ref))
	if data, ok := b.parts[name]; ok {
		return data, true
	}
	// A root-relative or already-absolute part name.
	if data, ok := b.parts[strings.TrimPrefix(path.Clean(ref), "/")]; ok {
		return data, true
	}
	return nil, false
}

// parseBook walks container.xml → OPF → spine.
func parseBook(parts map[string][]byte) (*Book, error) {
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(parts["META-INF/container.xml"], &container); err != nil || len(container.Rootfiles) == 0 {
		return nil, fmt.Errorf("%w: malformed container.xml", ErrNotEPUB)
	}
	opfName := strings.TrimPrefix(container.Rootfiles[0].FullPath, "/")
	opfData, ok := parts[opfName]
	if !ok {
		return nil, fmt.Errorf("%w: missing package document %s", ErrNotEPUB, opfName)
	}

	var opf struct {
		Metadata struct {
			Title []string `xml:"title"`
		} `xml:"metadata"`
		Manifest struct {
			Items []struct {
				ID        string `xml:"id,attr"`
				Href      string `xml:"href,attr"`
				MediaType string `xml:"media-type,attr"`
			} `xml:"item"`
		} `xml:"manifest"`
		Spine struct {
			ItemRefs []struct {
				IDRef  string `xml:"idref,attr"`
				Linear string `xml:"linear,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if err := xml.Unmarshal(opfData, &opf); err != nil {
		return nil, fmt.Errorf("%w: malformed package document: %v", ErrNotEPUB, err)
	}

	book := &Book{parts: parts, contentDir: path.Dir(opfName)}
	if book.contentDir == "." {
		book.contentDir = ""
	}
	if len(opf.Metadata.Title) > 0 {
		book.Title = strings.TrimSpace(opf.Metadata.Title[0])
	}

	hrefByID := map[string]string{}
	for _, item := range opf.Manifest.Items {
		hrefByID[item.ID] = item.Href
	}
	seenCSS := map[string]bool{}
	for _, ref := range opf.Spine.ItemRefs {
		if ref.Linear == "no" {
			continue // auxiliary content outside the reading order
		}
		href, ok := hrefByID[ref.IDRef]
		if !ok {
			continue
		}
		data, ok := book.Resource(href)
		if !ok {
			continue
		}
		body, links, styles := extractChapter(string(data))
		book.Chapters = append(book.Chapters, body)
		for _, l := range links {
			// Stylesheet hrefs are chapter-relative; chapters live under the
			// content dir, so resolve against it like every other resource.
			if i := strings.IndexAny(l, "#?"); i >= 0 {
				l = l[:i]
			}
			if l == "" || seenCSS[l] {
				continue
			}
			seenCSS[l] = true
			book.StylesheetRefs = append(book.StylesheetRefs, l)
		}
		book.InlineCSS = append(book.InlineCSS, styles...)
	}
	if len(book.Chapters) == 0 {
		return nil, fmt.Errorf("%w: the spine references no readable documents", ErrNotEPUB)
	}
	return book, nil
}

var (
	bodyOpenRe  = regexp.MustCompile(`(?is)<body[^>]*>`)
	bodyCloseRe = regexp.MustCompile(`(?is)</body\s*>`)
	linkRe      = regexp.MustCompile(`(?is)<link[^>]*>`)
	hrefRe      = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']+)["']`)
	relStyleRe  = regexp.MustCompile(`(?is)rel\s*=\s*["']?stylesheet`)
	styleRe     = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
)

// extractChapter pulls a chapter's body inner markup, stylesheet link hrefs,
// and inline style blocks. XHTML is markup the lenient HTML parser downstream
// accepts verbatim, so a tag-level scan suffices (no re-serialization pass
// that could perturb content).
func extractChapter(src string) (body string, links []string, styles []string) {
	head := src
	if open := bodyOpenRe.FindStringIndex(src); open != nil {
		head = src[:open[0]]
		rest := src[open[1]:]
		if end := bodyCloseRe.FindStringIndex(rest); end != nil {
			body = rest[:end[0]]
		} else {
			body = rest
		}
	} else {
		// A headless fragment: the whole document is content.
		body = src
	}
	for _, link := range linkRe.FindAllString(head, -1) {
		if !relStyleRe.MatchString(link) {
			continue
		}
		if m := hrefRe.FindStringSubmatch(link); m != nil {
			links = append(links, m[1])
		}
	}
	for _, m := range styleRe.FindAllStringSubmatch(head, -1) {
		styles = append(styles, m[1])
	}
	return body, links, styles
}
