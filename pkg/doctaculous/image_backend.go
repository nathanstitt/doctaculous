package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// ImageOptions controls image-encoded output (WriteImage, and Convert to
// FormatPNG/FormatJPEG).
type ImageOptions struct {
	// Format selects the encoding: FormatPNG (the default when zero) or
	// FormatJPEG. Any other format is an error.
	Format Format
	// Quality is the JPEG quality, 1..100; default 90 when zero. Ignored for PNG.
	Quality int
	// Page is the zero-based page Convert encodes when the target is an image —
	// an io.Writer holds exactly one encoded image, so multi-page image output is
	// a batch concern (RasterizePages plus one EncodeImage per page, as the CLI
	// does with a %d output pattern). WriteImage takes its page index explicitly
	// and ignores this field.
	Page int
	// Raster controls the rasterization that produces the image (DPI, background,
	// fonts, ...).
	Raster RasterOptions
}

// format resolves the target encoding, defaulting to PNG.
func (o ImageOptions) format() Format {
	if o.Format == FormatUnknown {
		return FormatPNG
	}
	return o.Format
}

// quality resolves the JPEG quality, defaulting to 90 and clamping to [1,100].
func (o ImageOptions) quality() int {
	switch {
	case o.Quality <= 0:
		return 90
	case o.Quality > 100:
		return 100
	}
	return o.Quality
}

// WriteImage rasterizes page index (zero-based) and encodes it to out in
// opts.Format. It works on every document format.
func (d *Document) WriteImage(ctx context.Context, out io.Writer, index int, opts ImageOptions) error {
	img, err := d.RasterizePage(ctx, index, opts.Raster)
	if err != nil {
		return err
	}
	return EncodeImage(out, img, opts)
}

// EncodeImage encodes an already-rasterized image to out in opts.Format — the
// encode half of WriteImage, exported so batch callers (e.g. a RasterizePages
// fan-out writing one file per page) reuse the library's encoding.
func EncodeImage(out io.Writer, img image.Image, opts ImageOptions) error {
	switch f := opts.format(); f {
	case FormatPNG:
		if err := png.Encode(out, img); err != nil {
			return fmt.Errorf("doctaculous: encode png: %w", err)
		}
	case FormatJPEG:
		if err := jpeg.Encode(out, img, &jpeg.Options{Quality: opts.quality()}); err != nil {
			return fmt.Errorf("doctaculous: encode jpeg: %w", err)
		}
	default:
		return fmt.Errorf("doctaculous: encode image: %s is not an image format: %w", f, ErrUnsupportedFormat)
	}
	return nil
}
