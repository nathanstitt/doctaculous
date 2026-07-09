package filter

import (
	"bytes"
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf/filter/jbig2"
)

// DecodeJBIG2 decodes a PDF JBIG2Decode image stream to a row-major, MSB-first,
// 1-bit-per-pixel buffer sized to width×height — the byte layout decodeRawImage consumes
// for a 1-bpc DeviceGray image. data is the raw JBIG2 stream; globals is the
// /JBIG2Globals stream bytes (nil when absent).
//
// JBIG2 uses set-bit = black; a PDF 1-bpc DeviceGray image uses sample 0 = black. This
// repacks with that inversion (a JBIG2 black pixel becomes bit 0), so downstream sees the
// conventional PDF bilevel image.
//
// It never panics: the vendored (third-party) decoder is wrapped in a recover, and any
// panic or error is returned as an error so the caller can skip the image and log.
func DecodeJBIG2(data, globals []byte, width, height int) (out []byte, err error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("jbig2: bad dimensions %dx%d", width, height)
	}
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("jbig2: decoder panicked: %v", r)
		}
	}()

	dec, derr := jbig2.NewDecoderWithGlobals(bytes.NewReader(data), globals)
	if derr != nil {
		return nil, fmt.Errorf("jbig2: new decoder: %w", derr)
	}
	img, derr := dec.Decode()
	if derr != nil {
		return nil, fmt.Errorf("jbig2: decode: %w", derr)
	}

	// Repack the decoded bilevel image into MSB-first 1-bpp rows (byte-aligned per row),
	// inverting polarity (JBIG2 black=1 → PDF DeviceGray sample 0 = black).
	rowBytes := (width + 7) / 8
	out = make([]byte, rowBytes*height)
	b := img.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			black := false
			if x < b.Dx() && y < b.Dy() {
				r16, g16, bl16, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if (r16+g16+bl16)/3 < 0x8000 {
					black = true
				}
			}
			// PDF sample: 0 = black. Set the bit to 1 for a WHITE pixel, leave 0 for black.
			if !black {
				out[y*rowBytes+x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return out, nil
}
