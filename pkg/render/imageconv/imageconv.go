// Package imageconv re-encodes raster images into formats the office/EPUB
// output containers accept. Writers pass source image bytes through
// untouched when the container supports the format; for formats it cannot
// hold (HEIC and WebP today), TranscodeToPNG produces a PNG rendition to
// embed instead of degrading to alt text.
package imageconv

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	// Registered decoders for the sniffing Decode below. HEIC arrives via
	// pkg/heif's image.RegisterFormat, WebP via pkg/webp's.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "github.com/nathanstitt/doctaculous/pkg/heif"

	"github.com/nathanstitt/doctaculous/pkg/webp"
)

// TranscodeToPNG decodes image bytes in any registered format and re-encodes
// them as PNG, returning the PNG bytes and the decoded configuration.
func TranscodeToPNG(data []byte) ([]byte, image.Config, error) {
	// An animated WebP passes image.DecodeConfig (x/image/webp reads the VP8X
	// canvas size and ignores the animation flag) but fails image.Decode with a
	// bare "invalid format", indistinguishable from corrupt bytes. Name it here
	// so the writers' degradation logs say why a valid file was skipped.
	if webp.IsAnimated(data) {
		return nil, image.Config{}, fmt.Errorf("imageconv: %w", webp.ErrAnimated)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, image.Config{}, fmt.Errorf("imageconv: decode config: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, image.Config{}, fmt.Errorf("imageconv: decode: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, image.Config{}, fmt.Errorf("imageconv: encode png: %w", err)
	}
	return buf.Bytes(), cfg, nil
}
