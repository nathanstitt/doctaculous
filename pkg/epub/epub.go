// Package epub is a read-only EPUB reader: it opens the container, walks
// META-INF/container.xml to the OPF package document, and extracts the spine
// documents' body markup in reading order plus their stylesheets — the pieces
// the HTML pipeline lays out (EPUB content IS XHTML, so the reflow engine does
// the real work). EPUB 2 and 3 both resolve through the spine; the NCX is not
// consulted (it duplicates the spine's order). The manifest's cover image is
// surfaced too (Book.CoverHref), under both the EPUB 3 and EPUB 2 conventions,
// since a cover is not otherwise reachable from the spine. DRM-protected books
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

// maxTotalPartBytes caps the decompressed bytes an entire book may occupy,
// mirroring the OOXML readers. The per-part cap does not bound a container that
// holds many parts; see the comment in OpenBytes.
const maxTotalPartBytes = 512 << 20

// totalPartBudget is the ceiling OpenBytes actually enforces. It exists only so
// a test can lower it: proving the cap at its real value means decompressing
// half a gigabyte, which under -race costs several more gigabytes of shadow
// memory. Production code reads [maxTotalPartBytes]; nothing but a test may
// write this.
var totalPartBudget = maxTotalPartBytes

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
	// CoverHref is the OPF manifest's cover-image href (content-dir-relative, so
	// it resolves through Resource like any chapter reference), or "" when the
	// book declares no cover. Both conventions are honored: the EPUB 3
	// properties="cover-image" manifest property, and the EPUB 2 de-facto
	// <meta name="cover" content="itemID"> metadata entry. The image may be any
	// format the renderer handles, SVG included.
	//
	// The href is reported even when the referenced part is missing from the
	// container; callers that need the bytes should go through Resource, which
	// reports whether it resolved.
	CoverHref string
	// CoverMediaType is the manifest media-type declared for CoverHref, or "".
	CoverMediaType string
	// CoverInSpine reports that the cover image is ALSO reachable from the
	// reading order — the manifest item is itself a spine document, or a spine
	// document references it. A renderer that prepends a cover page uses this to
	// avoid showing the same image twice.
	CoverInSpine bool

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
	// Every entry is decompressed into memory here, so the TOTAL is bounded as
	// well as each part. maxPartSize alone does not bound a book: a container can
	// hold arbitrarily many entries, and N of them each just under the per-part
	// cap multiply. Measured on the sibling OOXML reader, a 4 MB package holding
	// 20 compressible parts expanded to 4.2 GB with every individual part inside
	// its 256 MiB limit.
	//
	// Over-budget entries are skipped rather than aborting the open, matching how
	// an over-large single part is already handled: the book degrades to missing
	// images or chapters. container.xml is required below, so a truncation that
	// loses it still reports an honest error rather than a silently empty book.
	parts := map[string][]byte{}
	total := 0
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
		if total+len(b) > totalPartBudget {
			continue
		}
		total += len(b)
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
			// Metas carries the EPUB 2 de-facto cover convention,
			// <meta name="cover" content="itemID">. EPUB 3 replaced it with a
			// manifest property but did not forbid it, and real EPUB 3 files in
			// the wild still ship both, so both are read.
			Metas []struct {
				Name    string `xml:"name,attr"`
				Content string `xml:"content,attr"`
			} `xml:"meta"`
		} `xml:"metadata"`
		Manifest struct {
			Items []struct {
				ID        string `xml:"id,attr"`
				Href      string `xml:"href,attr"`
				MediaType string `xml:"media-type,attr"`
				// Properties is the EPUB 3 manifest-item property list; a
				// space-separated token set whose "cover-image" token names the
				// book's cover.
				Properties string `xml:"properties,attr"`
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

	// The cover-image item, by whichever convention the book uses. EPUB 3's
	// manifest property wins over the EPUB 2 <meta> when a book carries both,
	// because it is the normative one; a book carrying only the <meta> (every
	// EPUB 2, and plenty of EPUB 3) still resolves.
	coverID := ""
	for _, item := range opf.Manifest.Items {
		if hasProperty(item.Properties, "cover-image") {
			coverID = item.ID
			break
		}
	}
	if coverID == "" {
		for _, m := range opf.Metadata.Metas {
			if strings.EqualFold(strings.TrimSpace(m.Name), "cover") && m.Content != "" {
				coverID = m.Content
				break
			}
		}
	}
	if href, ok := hrefByID[coverID]; ok && coverID != "" {
		book.CoverHref = href
		for _, item := range opf.Manifest.Items {
			if item.ID == coverID {
				book.CoverMediaType = item.MediaType
				break
			}
		}
	}
	// A cover that is ITSELF a spine document would render twice if a caller also
	// prepended it; flag that here, where the spine is in hand. (The other
	// duplicate route — a spine chapter that <img>s the cover — is detected below,
	// against each chapter's markup.)
	for _, ref := range opf.Spine.ItemRefs {
		if ref.IDRef == coverID && coverID != "" {
			book.CoverInSpine = true
		}
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
		if book.CoverHref != "" && !book.CoverInSpine && chapterReferencesCover(body, href, book.CoverHref) {
			book.CoverInSpine = true
		}
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

// hasProperty reports whether a space-separated EPUB property list contains
// token, compared case-insensitively (the tokens are defined lowercase, but a
// producer that shouts them should not lose its cover). An empty list never
// matches.
func hasProperty(list, token string) bool {
	for _, f := range strings.Fields(list) {
		if strings.EqualFold(f, token) {
			return true
		}
	}
	return false
}

// chapterReferencesCover reports whether a spine document's body markup points
// any src/href at the cover image, so a caller prepending a cover page can skip
// doing so when the book already shows it. chapterHref is the chapter's own
// content-dir-relative href, needed because the chapter's references are
// CHAPTER-relative while coverHref is content-dir-relative.
//
// The scan is a plain attribute-value comparison over the raw markup: it does
// not parse, so a cover reference built by script or hidden behind a redirect is
// missed. That is the safe direction to be wrong in — a missed match prepends a
// cover page the book also shows inline, which is redundant but correct, whereas
// a false match would DROP a cover the reader should see.
func chapterReferencesCover(body, chapterHref, coverHref string) bool {
	want := path.Clean(coverHref)
	base := path.Dir(chapterHref)
	for _, m := range srcOrHrefRe.FindAllStringSubmatch(body, -1) {
		ref := m[2]
		if i := strings.IndexAny(ref, "#?"); i >= 0 {
			ref = ref[:i]
		}
		if ref == "" {
			continue
		}
		if path.Clean(path.Join(base, ref)) == want || path.Clean(ref) == want {
			return true
		}
	}
	return false
}

var (
	// srcOrHrefRe captures the value of any src= or href= attribute in raw markup.
	srcOrHrefRe = regexp.MustCompile(`(?is)\b(src|href)\s*=\s*["']([^"']*)["']`)

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
