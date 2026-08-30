package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/crop"
	"github.com/nathanstitt/doctaculous/pkg/webp"
)

// ImageOptions controls image-encoded output (WriteImage, and Convert to
// FormatPNG/FormatJPEG/FormatWebP).
type ImageOptions struct {
	// Format selects the encoding: FormatPNG (the default when zero),
	// FormatJPEG, or FormatWebP. Any other format is an error.
	Format Format
	// Quality is the JPEG quality, 1..100; default 90 when zero. Ignored for
	// PNG and for WebP — both are lossless here, so there is nothing to trade
	// away. (WebP output is always lossless VP8L; the toolkit has no pure-Go
	// lossy VP8 encoder. See webp.Encode.)
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
	// Crop, when non-nil, crops and resizes the rasterized image to exactly
	// crop.Options.Width×Height before encoding. This is how a caller gets an
	// exact-size image: RasterOptions.MaxWidthPx/MaxHeightPx fit within a box
	// with aspect preserved, which cannot fill a square from a 4:3 source.
	// nil (the default) encodes the image as rasterized.
	Crop *crop.Options
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
	_, err := d.WriteImageRect(ctx, out, index, opts)
	return err
}

// WriteImageRect is WriteImage, and additionally reports the rectangle of the
// rasterized page that was cropped, in the rasterized image's pixel
// coordinates. With opts.Crop nil it reports the full rasterized bounds.
//
// Use this when opts.Crop selects a content-aware crop (crop.StrategySaliency)
// and the caller needs to know which region was chosen — the coordinates are
// scorer output, not a function of the options.
func (d *Document) WriteImageRect(ctx context.Context, out io.Writer, index int, opts ImageOptions) (image.Rectangle, error) {
	img, err := d.RasterizePage(ctx, index, opts.Raster)
	if err != nil {
		return image.Rectangle{}, err
	}
	return EncodeImageRect(out, img, opts)
}

// EncodeImage encodes an already-rasterized image to out in opts.Format — the
// encode half of WriteImage, exported so batch callers (e.g. a RasterizePages
// fan-out writing one file per page) reuse the library's encoding.
func EncodeImage(out io.Writer, img image.Image, opts ImageOptions) error {
	_, err := EncodeImageRect(out, img, opts)
	return err
}

// EncodeImageRect is EncodeImage, and additionally reports the source rectangle
// it cropped, in img's coordinate space. With opts.Crop nil it reports
// img.Bounds() and crops nothing.
//
// The rectangle matters when Crop.Strategy is crop.StrategySaliency: the scorer
// chooses it from image content, so it cannot be derived from the options. A
// caller that needs to know where a smart crop landed — to record it, show it,
// or reproduce the identical crop later by passing it back as Crop.Rect with
// crop.StrategyRect — takes it from here.
func EncodeImageRect(out io.Writer, img image.Image, opts ImageOptions) (image.Rectangle, error) {
	used := img.Bounds()
	if opts.Crop != nil {
		cropped, r, err := crop.ScaleRect(img, *opts.Crop)
		if err != nil {
			return image.Rectangle{}, fmt.Errorf("doctaculous: crop image: %w", err)
		}
		img, used = cropped, r
	}
	if err := encodeImageTo(out, img, opts); err != nil {
		return image.Rectangle{}, err
	}
	return used, nil
}

// encodeImageTo writes img to out in opts.Format, without cropping.
func encodeImageTo(out io.Writer, img image.Image, opts ImageOptions) error {
	switch f := opts.format(); f {
	case FormatPNG:
		if err := png.Encode(out, img); err != nil {
			return fmt.Errorf("doctaculous: encode png: %w", err)
		}
	case FormatJPEG:
		if err := jpeg.Encode(out, img, &jpeg.Options{Quality: opts.quality()}); err != nil {
			return fmt.Errorf("doctaculous: encode jpeg: %w", err)
		}
	case FormatWebP:
		// Lossless VP8L; opts.Quality does not apply. See webp.Encode.
		if err := webp.Encode(out, img); err != nil {
			return fmt.Errorf("doctaculous: encode webp: %w", err)
		}
	default:
		return fmt.Errorf("doctaculous: encode image: %s is not an image format: %w", f, ErrUnsupportedFormat)
	}
	return nil
}
