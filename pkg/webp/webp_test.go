package webp

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// The still fixtures cover the three encodings a real WebP file uses: lossy
// VP8, lossless VP8L, and the extended VP8X container with an alpha plane
// (whose flags byte has a bit set adjacent to the animation bit, so it also
// guards against a sloppy mask in IsAnimated).
var stills = []string{"still-lossy.webp", "still-lossless.webp", "still-lossy-alpha.webp"}

func TestDecodeStills(t *testing.T) {
	t.Parallel()
	for _, name := range stills {
		t.Run(name, func(t *testing.T) {
			data := read(t, name)
			img, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Decode(%s) = %v, want success", name, err)
			}
			b := img.Bounds()
			if b.Dx() <= 0 || b.Dy() <= 0 {
				t.Errorf("Decode(%s) bounds = %v, want a non-empty image", name, b)
			}

			cfg, err := DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("DecodeConfig(%s) = %v, want success", name, err)
			}
			// The config must agree with the decoded image; a mismatch means a
			// caller sizing a page from the config would lay out the wrong box.
			if cfg.Width != b.Dx() || cfg.Height != b.Dy() {
				t.Errorf("DecodeConfig(%s) = %dx%d, decoded image is %dx%d",
					name, cfg.Width, cfg.Height, b.Dx(), b.Dy())
			}
		})
	}
}

// TestIsAnimated pins the flag read that x/image/webp skips.
func TestIsAnimated(t *testing.T) {
	t.Parallel()
	if !IsAnimated(read(t, "animated.webp")) {
		t.Error("IsAnimated(animated.webp) = false, want true")
	}
	for _, name := range stills {
		if IsAnimated(read(t, name)) {
			t.Errorf("IsAnimated(%s) = true, want false", name)
		}
	}
	// Non-WebP and truncated input are not animated, and must not panic on a
	// short buffer — IsAnimated indexes into a fixed header offset.
	for _, junk := range [][]byte{
		nil,
		[]byte("RIFF"),
		[]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), // header present, flags byte missing
		[]byte("\x89PNG\r\n\x1a\n"),
		bytes.Repeat([]byte{0}, 64),
	} {
		if IsAnimated(junk) {
			t.Errorf("IsAnimated(%q) = true, want false", junk)
		}
	}
}

// TestAnimatedRejected is the degradation contract: an animated WebP is a
// deliberate refusal naming itself, not a generic decode failure. Both entry
// points must refuse, because a caller that sniffs a config before decoding
// would otherwise be told an image it cannot read is 386x395.
func TestAnimatedRejected(t *testing.T) {
	t.Parallel()
	data := read(t, "animated.webp")

	if _, err := Decode(bytes.NewReader(data)); !errors.Is(err, ErrAnimated) {
		t.Errorf("Decode(animated) = %v, want ErrAnimated", err)
	}
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if !errors.Is(err, ErrAnimated) {
		t.Errorf("DecodeConfig(animated) = %v, want ErrAnimated", err)
	}
	if cfg.Width != 0 || cfg.Height != 0 {
		t.Errorf("DecodeConfig(animated) returned %dx%d, want a zero config", cfg.Width, cfg.Height)
	}
}

// TestRegistration checks that importing this package makes image.Decode
// handle WebP toolkit-wide, and reports it under FormatName.
func TestRegistration(t *testing.T) {
	t.Parallel()
	for _, name := range stills {
		data := read(t, name)
		img, kind, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("image.Decode(%s) = %v, want success", name, err)
		}
		if kind != FormatName {
			t.Errorf("image.Decode(%s) kind = %q, want %q", name, kind, FormatName)
		}
		if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
			t.Errorf("image.Decode(%s) bounds = %v, want a non-empty image", name, b)
		}
	}
}

// TestSniffingPathCannotRejectAnimation pins the limitation that shapes this
// package's API, so it is a documented property rather than a latent surprise.
//
// image.sniff returns the first registered format whose magic matches and
// iterates in registration order; an importing package's init runs after the
// package it imports, so this package cannot install an entry that outranks
// x/image/webp's. The consequence is that image.DecodeConfig reports an
// animated file's canvas size with no error. Callers that must not be fooled
// check IsAnimated on the bytes — TranscodeToPNG and OpenImageBytes do.
//
// If a future x/image starts rejecting animation itself, this test fails and
// those explicit checks can be reconsidered.
func TestSniffingPathCannotRejectAnimation(t *testing.T) {
	t.Parallel()
	data := read(t, "animated.webp")

	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig(animated) = %v; upstream now rejects animation, "+
			"so the explicit IsAnimated checks in imageconv/image_frontend can be revisited", err)
	}
	if kind != FormatName {
		t.Errorf("image.DecodeConfig(animated) kind = %q, want %q", kind, FormatName)
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		t.Errorf("image.DecodeConfig(animated) = %dx%d, want the (misleading) canvas size",
			cfg.Width, cfg.Height)
	}
	// ...while the matching Decode fails, and not in a way a caller can attribute.
	if _, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		t.Error("image.Decode(animated) succeeded; upstream may now support animation")
	}
}
