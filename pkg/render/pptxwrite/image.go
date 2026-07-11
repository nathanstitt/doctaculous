package pptxwrite

import (
	"bytes"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// mediaExt maps a Go image format name to the media part extension the
// package declares.
var mediaExt = map[string]string{
	"png":  "png",
	"jpeg": "jpeg",
	"gif":  "gif",
}

// imageRun is the CollectRuns image callback: it embeds the image as a deduped
// media part and QUEUES a picture shape (PPTX text runs cannot hold images —
// the picture follows the text shape that referenced it). The returned string
// is plain alt text (or "") — inlineRuns treats it as text, not markup.
// data: URIs decode without a loader; otherwise, without a resource loader —
// or when the fetch/decode fails — the image degrades to its alt text, logged.
func (w *writer) imageRun(rc *cssbox.ReplacedContent) string {
	if rc.Tag != "img" {
		return ""
	}
	src := rc.Attrs["src"]
	alt := rc.Attrs["alt"]
	fail := func(msg string, args ...any) string {
		w.opts.Logf("pptxwrite: "+msg+"; keeping alt text", args...)
		return alt
	}
	if src == "" {
		return fail("image with no src")
	}

	idx, ok := w.media[src]
	var pxW, pxH int
	if !ok {
		var data []byte
		var err error
		if strings.HasPrefix(src, "data:") {
			data, _, err = resource.LoadDataURL(src)
		} else if w.opts.Loader != nil {
			data, _, err = w.opts.Loader.Load(w.ctx, src)
		} else {
			return fail("no resource loader to fetch image %q", src)
		}
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
		idx = len(w.mediaParts)
		w.mediaParts = append(w.mediaParts, mediaPart{
			name: fmt.Sprintf("image%d.%s", idx+1, ext),
			data: data,
		})
		w.media[src] = idx
		w.mediaSizes = append(w.mediaSizes, [2]int{cfg.Width, cfg.Height})
	}
	pxW, pxH = w.mediaSizes[idx][0], w.mediaSizes[idx][1]

	// Explicit width/height attributes (CSS px) win over the intrinsic size.
	if v, err := strconv.Atoi(rc.Attrs["width"]); err == nil && v > 0 {
		pxW = v
	}
	if v, err := strconv.Atoi(rc.Attrs["height"]); err == nil && v > 0 {
		pxH = v
	}
	if pxW <= 0 || pxH <= 0 {
		pxW, pxH = 1, 1
	}
	w.pendingPics = append(w.pendingPics, picShape{mediaIdx: idx, alt: alt, pxW: pxW, pxH: pxH})
	return ""
}
