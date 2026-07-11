// Package epubwrite renders a cssbox tree (the shared box model produced by
// the HTML, DOCX, Markdown, and PDF-extraction frontends) to an EPUB 3
// package. It is a STRUCTURE writer built on the htmlwrite serializer (EPUB
// content documents ARE XHTML): the document splits into chapters at each
// <h1> (content before the first — or a document with no <h1> at all — is a
// single untitled chapter), a nav.xhtml table of contents is built from the
// chapter titles, and images referenced through the resource loader embed as
// manifest items (data: URIs stay inline — they round-trip verbatim).
//
// Output is deterministic: fixed zip timestamps and part order, the
// spec-required stored (uncompressed) "mimetype" as the first entry, and a
// fixed dcterms:modified stamp.
package epubwrite

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"strings"
	"time"

	// The image decoders embedImage's DecodeConfig relies on, registered here
	// so the writer is self-sufficient.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/htmlwrite"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// Options configures EPUB output.
type Options struct {
	// Title is the book's dc:title; the first <h1>'s text when empty, else
	// "Document".
	Title string
	// Loader resolves image refs (an <img> src) to bytes for embedding as
	// manifest items. data: URIs stay inline without a loader; for the rest,
	// nil means the reference is kept as-is (logged — it will not resolve
	// inside the container).
	Loader resource.ResourceLoader
	// Logf receives degradation diagnostics. nil -> no-op.
	Logf func(string, ...any)
}

// Write renders the cssbox tree rooted at root to w as a complete .epub
// package. root is expected to be a finalized tree (post box-generation
// fixups). A nil root writes a valid one-empty-chapter book.
func Write(ctx context.Context, root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	wr := &writer{opts: opts, ctx: ctx, media: map[string]string{}}

	var blocks []*cssbox.Box
	if root != nil {
		collectBlocks(root, &blocks)
	}
	chapters := splitChapters(blocks)
	if len(chapters) == 0 {
		chapters = []chapter{{}}
	}

	title := opts.Title
	for i := range chapters {
		if t := headingText(chapters[i].heading); t != "" {
			chapters[i].title = t
			if title == "" {
				title = t
			}
		} else {
			chapters[i].title = fmt.Sprintf("Chapter %d", i+1)
		}
	}
	if title == "" {
		title = "Document"
	}

	// Serialize each chapter through htmlwrite's XHTML mode; the ImageSrc hook
	// embeds fetched images as it goes.
	var bodies []string
	for _, ch := range chapters {
		container := &cssbox.Box{Children: ch.blocks}
		var buf bytes.Buffer
		err := htmlwrite.Write(container, &buf, htmlwrite.Options{
			Fragment: true,
			XHTML:    true,
			ImageSrc: wr.imageSrc,
			Logf:     opts.Logf,
		})
		if err != nil {
			return fmt.Errorf("epubwrite: %w", err)
		}
		bodies = append(bodies, strings.TrimRight(buf.String(), "\n"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	pkg, err := wr.assemble(title, chapters, bodies)
	if err != nil {
		return fmt.Errorf("epubwrite: %w", err)
	}
	if _, err := w.Write(pkg); err != nil {
		return fmt.Errorf("epubwrite: %w", err)
	}
	return nil
}

// writer holds the media accumulation the ImageSrc hook feeds.
type writer struct {
	opts Options
	ctx  context.Context

	// media dedupes embedded images: src ref -> manifest href.
	media      map[string]string
	mediaParts []mediaPart
}

// mediaPart is one embedded image part.
type mediaPart struct {
	href, mediaType string // href relative to the OPF dir
	data            []byte
}

// chapter is one spine document: its heading box (nil for an untitled
// preamble) and the blocks it contains.
type chapter struct {
	heading *cssbox.Box
	title   string
	blocks  []*cssbox.Box
}

// collectBlocks flattens the tree's block sequence, descending transparent
// wrappers (html/body/div) exactly like the structure writers' walk, so the
// chapter split sees the same top-level blocks htmlwrite will serialize.
func collectBlocks(b *cssbox.Box, out *[]*cssbox.Box) {
	switch {
	case b.HeadingLvl >= 1 && b.HeadingLvl <= 6,
		b.SemTag == "blockquote", b.SemTag == "pre", b.SemTag == "hr",
		b.Display == cssbox.DisplayTable,
		boxwalk.IsListContainer(b),
		b.Display == cssbox.DisplayListItem:
		*out = append(*out, b)
	case boxwalk.IsBlockContainer(b):
		if boxwalk.HasInlineContent(b) {
			*out = append(*out, b)
			return
		}
		for _, c := range b.Children {
			collectBlocks(c, out)
		}
	default:
		for _, c := range b.Children {
			collectBlocks(c, out)
		}
	}
}

// splitChapters cuts the block sequence at each <h1>. Content before the
// first <h1> becomes an untitled leading chapter.
func splitChapters(blocks []*cssbox.Box) []chapter {
	var out []chapter
	for _, b := range blocks {
		if b.HeadingLvl == 1 || len(out) == 0 {
			if b.HeadingLvl == 1 {
				out = append(out, chapter{heading: b, blocks: []*cssbox.Box{b}})
				continue
			}
			out = append(out, chapter{})
		}
		cur := &out[len(out)-1]
		cur.blocks = append(cur.blocks, b)
	}
	return out
}

// headingText renders a heading box's plain text.
func headingText(h *cssbox.Box) string {
	if h == nil {
		return ""
	}
	var runs []boxwalk.StyledRun
	boxwalk.CollectRuns(h, boxwalk.InlineState{}, func(*cssbox.ReplacedContent) string { return "" }, &runs)
	var sb strings.Builder
	for _, r := range runs {
		sb.WriteString(r.Text)
	}
	return boxwalk.CollapseSpaces(sb.String())
}

// imageSrc is the htmlwrite hook: a data: URI stays inline (it round-trips
// verbatim); any other reference fetches through the loader and embeds as a
// deduped manifest item, its src rewritten to the item's href. Without a
// loader — or on a failed fetch/decode — the reference is kept as-is, logged.
func (w *writer) imageSrc(src string) string {
	if strings.HasPrefix(src, "data:") {
		return src
	}
	if href, ok := w.media[src]; ok {
		return href
	}
	keep := func(msg string, args ...any) string {
		w.opts.Logf("epubwrite: "+msg+"; keeping the reference (it will not resolve inside the container)", args...)
		return src
	}
	if w.opts.Loader == nil {
		return keep("no resource loader to fetch image %q", src)
	}
	data, _, err := w.opts.Loader.Load(w.ctx, src)
	if err != nil {
		return keep("fetching image %q: %v", src, err)
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return keep("decoding image %q: %v", src, err)
	}
	var ext, mt string
	switch format {
	case "png":
		ext, mt = "png", "image/png"
	case "jpeg":
		ext, mt = "jpeg", "image/jpeg"
	case "gif":
		ext, mt = "gif", "image/gif"
	default:
		return keep("image %q has unsupported format %q", src, format)
	}
	href := fmt.Sprintf("images/img%d.%s", len(w.mediaParts)+1, ext)
	w.mediaParts = append(w.mediaParts, mediaPart{href: href, mediaType: mt, data: data})
	w.media[src] = href
	return href
}

// assemble builds the deterministic EPUB 3 container.
func (w *writer) assemble(title string, chapters []chapter, bodies []string) ([]byte, error) {
	type part struct {
		name string
		data string
	}
	var parts []part

	parts = append(parts, part{"META-INF/container.xml", xmlDecl + `<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
`})

	// content.opf
	var opf strings.Builder
	opf.WriteString(xmlDecl)
	opf.WriteString(`<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">` + "\n")
	opf.WriteString(`<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n")
	opf.WriteString(`<dc:identifier id="pub-id">urn:doctaculous:generated</dc:identifier>` + "\n")
	opf.WriteString(`<dc:title>` + escXML.Replace(title) + `</dc:title>` + "\n")
	opf.WriteString(`<dc:language>en</dc:language>` + "\n")
	opf.WriteString(`<meta property="dcterms:modified">2000-01-01T00:00:00Z</meta>` + "\n")
	opf.WriteString(`</metadata>` + "\n<manifest>\n")
	opf.WriteString(`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	for i := range chapters {
		fmt.Fprintf(&opf, `<item id="chap%d" href="chap%d.xhtml" media-type="application/xhtml+xml"/>`+"\n", i+1, i+1)
	}
	for i, m := range w.mediaParts {
		fmt.Fprintf(&opf, `<item id="img%d" href="%s" media-type="%s"/>`+"\n", i+1, m.href, m.mediaType)
	}
	opf.WriteString("</manifest>\n<spine>\n")
	for i := range chapters {
		fmt.Fprintf(&opf, `<itemref idref="chap%d"/>`+"\n", i+1)
	}
	opf.WriteString("</spine>\n</package>\n")
	parts = append(parts, part{"OEBPS/content.opf", opf.String()})

	// nav.xhtml — the EPUB 3 table of contents from the chapter titles.
	var nav strings.Builder
	nav.WriteString(xmlDecl + `<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>` + escXML.Replace(title) + `</title></head>
<body><nav epub:type="toc"><ol>
`)
	for i, ch := range chapters {
		fmt.Fprintf(&nav, `<li><a href="chap%d.xhtml">%s</a></li>`+"\n", i+1, escXML.Replace(ch.title))
	}
	nav.WriteString("</ol></nav></body>\n</html>\n")
	parts = append(parts, part{"OEBPS/nav.xhtml", nav.String()})

	for i, body := range bodies {
		var doc strings.Builder
		doc.WriteString(xmlDecl + `<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>` + escXML.Replace(chapters[i].title) + `</title></head>
<body>
`)
		if body != "" {
			doc.WriteString(body)
			doc.WriteByte('\n')
		}
		doc.WriteString("</body>\n</html>\n")
		parts = append(parts, part{fmt.Sprintf("OEBPS/chap%d.xhtml", i+1), doc.String()})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	// The spec-required first entry: "mimetype", stored (uncompressed).
	mf, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store, Modified: stamp})
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write([]byte("application/epub+zip")); err != nil {
		return nil, err
	}
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(p.data)); err != nil {
			return nil, err
		}
	}
	for _, m := range w.mediaParts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: "OEBPS/" + m.href, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(m.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// xmlDecl is the standard part prolog.
const xmlDecl = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// escXML escapes XML text/attribute metacharacters.
var escXML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
