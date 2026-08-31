package webp

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"

	xwebp "golang.org/x/image/webp"
)

// gradient builds a deterministic test image with a varied palette and, when
// alpha is set, a partly transparent region — enough structure that a
// transform or palette bug would show up as a pixel difference rather than
// hiding in a flat fill.
func gradient(w, h int, alpha bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(0xFF)
			if alpha && x < w/2 {
				a = uint8(0x40)
			}
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / max(w-1, 1)),
				G: uint8((y * 255) / max(h-1, 1)),
				B: uint8((x ^ y) & 0xFF),
				A: a,
			})
		}
	}
	return img
}

// TestEncodeRoundTripIsLossless is the claim that matters: WebP output is
// VP8L, so decoding it must return the ORIGINAL pixels exactly. It decodes
// with x/image/webp — a different implementation from the encoder — so this
// checks real interoperability, not the encoder agreeing with itself.
func TestEncodeRoundTripIsLossless(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		w, h  int
		alpha bool
	}{
		{"opaque", 64, 48, false},
		{"alpha", 64, 48, true},
		{"single pixel", 1, 1, false},
		{"tall thin", 3, 129, false},
		{"wide short", 129, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := gradient(tc.w, tc.h, tc.alpha)

			var buf bytes.Buffer
			if err := Encode(&buf, src); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := xwebp.Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("x/image/webp.Decode of our output: %v", err)
			}
			if b := got.Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
				t.Fatalf("decoded %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.w, tc.h)
			}

			// Compare through a common representation: the decoder returns
			// NRGBA/RGBA depending on alpha, and color.RGBA's RGBA() is
			// premultiplied, so normalize both sides the same way.
			want := image.NewNRGBA(src.Bounds())
			draw.Draw(want, want.Bounds(), src, src.Bounds().Min, draw.Src)
			have := image.NewNRGBA(got.Bounds())
			draw.Draw(have, have.Bounds(), got, got.Bounds().Min, draw.Src)

			for y := range tc.h {
				for x := range tc.w {
					w, h := want.NRGBAAt(x, y), have.NRGBAAt(x, y)
					// A fully transparent pixel's colour channels are not
					// meaningful and need not survive; everything else must.
					if w.A == 0 && h.A == 0 {
						continue
					}
					if w != h {
						t.Fatalf("pixel (%d,%d) = %v, want %v — VP8L is lossless, "+
							"so a difference means an encoder or transform bug", x, y, h, w)
					}
				}
			}
		})
	}
}

// TestEncodeRejectsUnrepresentable pins the honest failures: rather than
// writing a truncated or malformed file, Encode reports what it cannot do.
func TestEncodeRejectsUnrepresentable(t *testing.T) {
	t.Parallel()

	if err := Encode(&bytes.Buffer{}, nil); err == nil {
		t.Error("Encode(nil image): want an error")
	}
	if err := Encode(&bytes.Buffer{}, image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Error("Encode(empty image): want an error")
	}
	// MaxDimension is the format's 14-bit ceiling, not a policy choice: one
	// pixel over must fail. Allocating the full-size image would cost ~1 GiB,
	// so use a 1-pixel-tall strip.
	over := image.NewRGBA(image.Rect(0, 0, MaxDimension+1, 1))
	if err := Encode(&bytes.Buffer{}, over); err == nil {
		t.Errorf("Encode(%dx1): want an error, the format's limit is %d",
			MaxDimension+1, MaxDimension)
	}
	// ...and exactly at the limit must still work.
	atLimit := image.NewRGBA(image.Rect(0, 0, MaxDimension, 1))
	if err := Encode(&bytes.Buffer{}, atLimit); err != nil {
		t.Errorf("Encode(%dx1) = %v, want success at exactly the limit", MaxDimension, err)
	}
}

// errWriter fails every write, standing in for a full disk or a closed pipe.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// TestEncodeReportsWriteFailure guards the reason Encode buffers rather than
// handing the destination straight to the underlying encoder: nativewebp does
// not check the error from any of its writes, so passing w through would
// report a failed write as a successful encode. That is exactly the silent
// failure this repo's conventions forbid.
func TestEncodeReportsWriteFailure(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("disk full")
	err := Encode(errWriter{sentinel}, gradient(8, 8, false))
	if err == nil {
		t.Fatal("Encode to a failing writer returned nil; the write error was swallowed")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Encode error = %v, want it to wrap %v", err, sentinel)
	}
}

// TestEncodeDecodeSelfRoundTrip closes the loop through this package's own
// reader, the path a caller takes when converting webp→webp via the toolkit.
func TestEncodeDecodeSelfRoundTrip(t *testing.T) {
	t.Parallel()
	src := gradient(32, 24, true)
	var buf bytes.Buffer
	if err := Encode(&buf, src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Our own encoder must never produce something our animation check
	// mistakes for an animation.
	if IsAnimated(buf.Bytes()) {
		t.Error("IsAnimated(our own still output) = true")
	}
	img, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 24 {
		t.Errorf("bounds = %v, want 32x24", b)
	}
}
