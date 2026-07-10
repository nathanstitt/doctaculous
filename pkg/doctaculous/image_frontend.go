package doctaculous

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"strconv"
)

// OpenImage reads a PNG or JPEG image as a single-page document: the page is
// exactly the image's pixel size (1 px = 1 pt), so image→PDF yields a page
// the image fills edge to edge, and every other conversion follows (the
// structure writers carry the image; the tables-only writers degrade to their
// documented empty-output story). For options use OpenImageFile.
func OpenImage(path string) (*Document, error) {
	return OpenImageFile(path)
}

// OpenImageFile reads an image file at path, applying any options.
func OpenImageFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open image %q: %w", path, err)
	}
	return OpenImageBytes(data, opts...)
}

// OpenImageBytes opens an in-memory PNG or JPEG as a document, applying any
// options, and returns a Document ready to rasterize or convert. The format
// stamps from the actual encoding.
func OpenImageBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("doctaculous: decode image: %w", err)
	}
	var format Format
	var mime string
	switch kind {
	case "png":
		format, mime = FormatPNG, "image/png"
	case "jpeg":
		format, mime = FormatJPEG, "image/jpeg"
	default:
		return nil, fmt.Errorf("doctaculous: image format %q: %w", kind, ErrUnsupportedFormat)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("doctaculous: degenerate image size %dx%d", cfg.Width, cfg.Height)
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
