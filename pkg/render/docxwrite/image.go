package docxwrite

import (
	"bytes"
	"fmt"
	"image"
	"strconv"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// emuPerPx is 9525 EMU per CSS pixel (96dpi; 12700 EMU per point).
const emuPerPx = 9525

// pendingMarker is the literal the image callback returns for each queued
// model child; paraChildren pops one pending entry per marker run.
const pendingMarker = "\x00"

// imageRun is the CollectRuns image callback: it embeds the image as a media
// part (docx.AddImage) plus an inline Drawing queued as a pending model child,
// and returns the queue marker as the literal. Without a resource loader — or
// when the fetch/decode fails — the image degrades to its alt text, logged.
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
		w.pending = append(w.pending, docx.ParaChild{Run: &docx.Run{Text: alt}})
		return pendingMarker
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
		name := fmt.Sprintf("image%d.%s", len(w.media)+1, ext)
		m = mediaRef{relID: w.doc.AddImage(name, data), pxW: cfg.Width, pxH: cfg.Height}
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
	w.pending = append(w.pending, docx.ParaChild{Drawing: &docx.Drawing{
		RelID:       m.relID,
		WidthEMU:    int64(pxW) * emuPerPx,
		HeightEMU:   int64(pxH) * emuPerPx,
		Description: alt,
	}})
	return pendingMarker
}

// mediaExt maps a Go image format name to the part extension (and content-type
// Default) the package declares. These are the formats the layout engine and
// the DOCX reader decode.
var mediaExt = map[string]string{
	"png":  "png",
	"jpeg": "jpeg",
	"gif":  "gif",
}
