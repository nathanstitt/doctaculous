package rtfwrite

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// twipsPerPx converts CSS pixels (96dpi) to twips.
const twipsPerPx = 15

// imageRun is the CollectRuns image callback: it embeds the image as a \pict
// blob and returns the complete run as the literal (writeRuns emits literals
// raw). data: URIs decode without a loader; otherwise, without a resource
// loader — or when the fetch/decode fails — the image degrades to its alt
// text, logged. RTF carries png and jpeg; other formats degrade the same way.
func (w *writer) imageRun(rc *cssbox.ReplacedContent) string {
	if rc.Tag != "img" {
		return ""
	}
	src := rc.Attrs["src"]
	alt := rc.Attrs["alt"]
	fail := func(msg string, args ...any) string {
		w.opts.Logf("rtfwrite: "+msg+"; keeping alt text", args...)
		if alt == "" {
			return ""
		}
		return escapeRTF(alt)
	}
	if src == "" {
		return fail("image with no src")
	}

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
	var blip string
	switch format {
	case "png":
		blip = `\pngblip`
	case "jpeg":
		blip = `\jpegblip`
	default:
		return fail("image %q has format %q (RTF carries png/jpeg)", src, format)
	}

	// Extent: explicit width/height attributes (CSS px) win; else the
	// intrinsic pixel size.
	pxW, pxH := cfg.Width, cfg.Height
	if v, err := strconv.Atoi(rc.Attrs["width"]); err == nil && v > 0 {
		pxW = v
	}
	if v, err := strconv.Atoi(rc.Attrs["height"]); err == nil && v > 0 {
		pxH = v
	}
	if pxW <= 0 || pxH <= 0 {
		pxW, pxH = 1, 1
	}
	return fmt.Sprintf(`{\pict%s\picwgoal%d\pichgoal%d %s}`,
		blip, pxW*twipsPerPx, pxH*twipsPerPx, hex.EncodeToString(data))
}
