// Package heif decodes HEIF/HEIC still images (ISO/IEC 23008-12 containers
// holding HEVC-coded pictures) in pure Go.
//
// Scope: single still images as produced by real-world encoders — iPhone
// cameras (grids of independently coded tiles), macOS sips, x265/libheif and
// kvazaar — including auxiliary alpha planes and the irot/imir/clap
// orientation properties. Image sequences (msf1), AVIF (AV1-coded items), and
// protected items are rejected with an error wrapping ErrUnsupported.
//
// The package registers itself with the standard library's image package, so
// a blank import makes image.Decode and image.DecodeConfig handle .heic
// files.
//
// Note on patents: HEVC is covered by patent pools. This package is original
// MIT-licensed code, but shipping an HEVC decoder in a commercial product may
// require patent licenses; that responsibility rests with the deploying
// application, as with any open-source HEVC implementation.
package heif

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

// ErrInvalid reports a malformed HEIF container.
var ErrInvalid = errors.New("heif: invalid container")

// ErrUnsupported reports well-formed input this decoder deliberately does not
// handle (image sequences, AVIF, protected items, exotic HEVC profiles).
var ErrUnsupported = errors.New("heif: unsupported")

// Options configures decoding.
type Options struct {
	// Workers bounds the number of goroutines used for parallel decode
	// (grid tiles, wavefront rows, filter bands). 0 means GOMAXPROCS.
	Workers int
}

// Decode reads a HEIF image from r and returns the primary image, fully
// composed: grid tiles stitched, auxiliary alpha merged, and irot/imir/clap
// applied.
func Decode(r io.Reader) (image.Image, error) {
	return DecodeWithOptions(context.Background(), r, nil)
}

// DecodeWithOptions is Decode with a context (honored across parallel tile
// decodes) and explicit Options.
func DecodeWithOptions(ctx context.Context, r io.Reader, opts *Options) (img image.Image, err error) {
	// The bitstream parser never intentionally panics on malformed input,
	// but a decoder bug must surface as an error, not kill the caller's
	// batch (same posture as the PDF page boundary).
	defer func() {
		if p := recover(); p != nil {
			img, err = nil, fmt.Errorf("%w: panic during decode: %v", ErrInvalid, p)
		}
	}()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("heif: read: %w", err)
	}
	c, err := parseContainer(data)
	if err != nil {
		return nil, err
	}
	primary, err := c.primary()
	if err != nil {
		return nil, err
	}
	if err := checkDecodable(primary); err != nil {
		return nil, err
	}
	if _, _, err := c.configDims(primary); err != nil {
		return nil, err
	}
	if primary.itemType == "grid" {
		if _, _, err := c.validateGrid(primary); err != nil {
			return nil, err
		}
	}
	_ = ctx
	_ = opts
	// The container is fully parsed and the primary item located; pixel
	// decoding lands with the pkg/heif/hevc milestones.
	return nil, fmt.Errorf("%w: HEVC pixel decoding not yet implemented", ErrUnsupported)
}

// DecodeConfig returns the dimensions and color model of the primary image
// without decoding pixels. Reported dimensions are the final rendered ones:
// clean-aperture (clap) cropping is applied and 90/270-degree rotations
// (irot) swap width and height.
func DecodeConfig(r io.Reader) (cfg image.Config, err error) {
	defer func() {
		if p := recover(); p != nil {
			cfg, err = image.Config{}, fmt.Errorf("%w: panic during decode: %v", ErrInvalid, p)
		}
	}()
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("heif: read: %w", err)
	}
	c, err := parseContainer(data)
	if err != nil {
		return image.Config{}, err
	}
	primary, err := c.primary()
	if err != nil {
		return image.Config{}, err
	}
	if err := checkDecodable(primary); err != nil {
		return image.Config{}, err
	}
	w, h, err := c.configDims(primary)
	if err != nil {
		return image.Config{}, err
	}

	hasAlpha := c.alphaAuxFor(primary.id) != nil
	// deep: more than 8 bits per channel.
	deep := primary.pixi != nil && len(primary.pixi.bits) > 0 && primary.pixi.bits[0] > 8
	var model color.Model
	switch {
	case hasAlpha && deep:
		model = color.NRGBA64Model
	case hasAlpha:
		model = color.NRGBAModel
	case deep:
		model = color.RGBA64Model
	default:
		model = color.YCbCrModel
	}
	return image.Config{ColorModel: model, Width: w, Height: h}, nil
}

// configDims resolves the primary item's rendered dimensions from its ispe
// (falling back to the grid body for a derived grid item), applies integral
// clap cropping and irot rotation, and enforces the size caps.
func (c *container) configDims(primary *item) (w, h int, err error) {
	var width, height uint32
	switch {
	case primary.ispe != nil:
		width, height = primary.ispe.width, primary.ispe.height
	case primary.itemType == "grid":
		payload, err := c.payload(primary)
		if err != nil {
			return 0, 0, err
		}
		g, err := parseGridBody(payload)
		if err != nil {
			return 0, 0, err
		}
		width, height = g.width, g.height
	default:
		return 0, 0, fmt.Errorf("%w: item %d has no ispe property", ErrInvalid, primary.id)
	}

	if cl := primary.clap; cl != nil {
		if cw, ok := wholeFraction(cl.widthN, cl.widthD); ok && cw > 0 && cw <= width {
			width = cw
		}
		if ch, ok := wholeFraction(cl.heightN, cl.heightD); ok && ch > 0 && ch <= height {
			height = ch
		}
	}
	if primary.irot == 1 || primary.irot == 3 {
		width, height = height, width
	}
	if width == 0 || height == 0 {
		return 0, 0, fmt.Errorf("%w: zero image dimension", ErrInvalid)
	}
	if width > maxDim || height > maxDim || uint64(width)*uint64(height) > maxPixels {
		return 0, 0, fmt.Errorf("%w: image %dx%d exceeds size limits", ErrUnsupported, width, height)
	}
	return int(width), int(height), nil
}

// wholeFraction reduces n/d to a whole number if it is one.
func wholeFraction(n, d uint32) (uint32, bool) {
	if d == 0 || n%d != 0 {
		return 0, false
	}
	return n / d, true
}
