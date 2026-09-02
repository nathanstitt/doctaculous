package omnidoc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"strconv"

	"github.com/nathanstitt/omnidoc/pkg/internal/webp"
)

// ErrAnimatedImage reports a well-formed animated WebP offered as image input.
// The toolkit reads still images only, so the file is refused at open rather
// than decoded as its first frame. Callers branch on it with errors.Is.
var ErrAnimatedImage = webp.ErrAnimated

// OpenImageFile reads a PNG, JPEG, WebP, or HEIC image at path as a
// single-page document, applying any options: the page is exactly the image's
// pixel size (1 px = 1 pt), so image→PDF yields a page the image fills edge to
// edge, and every other conversion follows (the structure writers carry the
// image; the tables-only writers degrade to their documented empty-output
// story).
func OpenImageFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: open image %q: %w", path, err)
	}
	return OpenImageBytes(data, opts...)
}

// OpenImageBytes opens an in-memory PNG or JPEG as a document, applying any
// options, and returns a Document ready to rasterize or convert. The format
// stamps from the actual encoding.
func OpenImageBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("omnidoc: decode image: %w", err)
	}
	var format Format
	var mime string
	switch kind {
	case "png":
		format, mime = FormatPNG, "image/png"
	case "jpeg":
		format, mime = FormatJPEG, "image/jpeg"
	case "heif":
		format, mime = FormatHEIC, "image/heic"
	case webp.FormatName:
		// image.DecodeConfig reports an animated WebP's canvas size without
		// error (x/image/webp parses VP8X but ignores the animation flag), so
		// without this check a page would be built for an image that cannot
		// decode. The toolkit reads still images only.
		if webp.IsAnimated(data) {
			return nil, fmt.Errorf("omnidoc: open image: %w", ErrAnimatedImage)
		}
		format, mime = FormatWebP, "image/webp"
	default:
		return nil, fmt.Errorf("omnidoc: image format %q: %w", kind, ErrUnsupportedFormat)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("omnidoc: degenerate image size %dx%d", cfg.Width, cfg.Height)
	}

	w, h := strconv.Itoa(cfg.Width), strconv.Itoa(cfg.Height)
	var sb bytes.Buffer
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	sb.WriteString("body { margin: 0 }\n")
	sb.WriteString("@page { size: " + w + "px " + h + "px; margin: 0 }\n")
	sb.WriteString("img { display: block }\n")
	sb.WriteString("</style>\n</head>\n<body>\n")
	sb.WriteString(`<img src="data:` + mime + `;base64,` + base64.StdEncoding.EncodeToString(data) +
		`" width="` + w + `" height="` + h + `" alt="">` + "\n")
	sb.WriteString("</body>\n</html>\n")

	// The page is exactly the image; a caller's own WithPageSize wins.
	all := append([]HTMLOption{WithPageSize(float64(cfg.Width), float64(cfg.Height))}, opts...)
	doc, err := OpenHTMLBytes(sb.Bytes(), all...)
	if err != nil {
		return nil, err
	}
	doc.format = format
	return doc, nil
}
