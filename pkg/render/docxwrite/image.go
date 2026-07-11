package docxwrite

import (
	"bytes"
	"fmt"
	"image"
	"strconv"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// EMU conversion factors: 12700 EMU per point, 9525 EMU per CSS pixel (96dpi).
const (
	emuPerPt = 12700
	emuPerPx = 9525
)

// imageRun is the CollectRuns image callback: it embeds the image as a media
// part plus an inline drawing and returns the complete run XML as the literal
// (emitParagraph writes literals raw). Without a resource loader — or when the
// fetch/decode fails — the image degrades to its alt text, logged.
func (w *writer) imageRun(rc *cssbox.ReplacedContent) string {
	if rc.Tag != "img" {
		return ""
	}
	src := rc.Attrs["src"]
	alt := rc.Attrs["alt"]
	fail := func(msg string, args ...any) string {
		w.opts.Logf("docxwrite: "+msg+"; keeping alt text", args...)
		if alt == "" {
			return ""
		}
		return `<w:r><w:t xml:space="preserve">` + escText.Replace(alt) + "</w:t></w:r>"
	}
	if src == "" {
		return fail("image with no src")
	}
	if w.opts.Loader == nil {
		return fail("no resource loader to fetch image %q", src)
	}

	m, ok := w.media[src]
	if !ok {
		data, _, err := w.opts.Loader.Load(w.ctx, src)
		if err != nil {
			return fail("fetching image %q: %v", src, err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fail("decoding image %q: %v", src, err)
		}
		ext, ok := mediaExt[format]
		if !ok {
			return fail("image %q has unsupported format %q", src, format)
		}
		n := len(w.mediaParts) + 1
		part := fmt.Sprintf("media/image%d.%s", n, ext)
		relID := fmt.Sprintf("rId%d", 3+len(w.rels))
		w.rels = append(w.rels, docRel{
			id:      relID,
			relType: "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image",
			target:  part,
		})
		m = mediaRef{relID: relID, pxW: cfg.Width, pxH: cfg.Height}
		w.mediaParts = append(w.mediaParts, mediaPart{name: "word/" + part, ext: ext, data: data})
		w.media[src] = m
	}

	// Extent: explicit width/height attributes (CSS px) win; else the intrinsic
	// pixel size.
	pxW, pxH := m.pxW, m.pxH
	if v, err := strconv.Atoi(rc.Attrs["width"]); err == nil && v > 0 {
		pxW = v
	}
	if v, err := strconv.Atoi(rc.Attrs["height"]); err == nil && v > 0 {
		pxH = v
	}
	if pxW <= 0 || pxH <= 0 {
		pxW, pxH = 1, 1
	}
	w.drawings++
	return inlineDrawingXML(w.drawings, m.relID, alt, int64(pxW)*emuPerPx, int64(pxH)*emuPerPx)
}

// mediaExt maps a Go image format name to the part extension (and content-type
// Default) the package declares. These are the formats the layout engine and
// the DOCX reader decode.
var mediaExt = map[string]string{
	"png":  "png",
	"jpeg": "jpeg",
	"gif":  "gif",
}

// inlineDrawingXML is the minimal complete wp:inline drawing both this repo's
// reader (which needs only extent + blip) and Word (which needs the docPr /
// nvPicPr / spPr scaffold) accept. Namespaces are declared on the elements so
// the document root stays minimal.
func inlineDrawingXML(id int, relID, alt string, cx, cy int64) string {
	name := escAttr.Replace(fmt.Sprintf("Picture %d", id))
	desc := escAttr.Replace(alt)
	return fmt.Sprintf(`<w:r><w:drawing>`+
		`<wp:inline distT="0" distB="0" distL="0" distR="0" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">`+
		`<wp:extent cx="%d" cy="%d"/>`+
		`<wp:docPr id="%d" name="%s" descr="%s"/>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, id, name, desc, id, name, escAttr.Replace(relID), cx, cy)
}
