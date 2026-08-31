package imageconv

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/internal/webp"
)

// fixture reads a sibling package's testdata. The two it needs sit at different
// levels since the internal/ move: webp is internal like this package, while
// heif stayed public.
func fixture(t *testing.T, pkg, name string) []byte {
	t.Helper()
	rel := filepath.Join("..", pkg, "testdata", name)
	if pkg == "heif" {
		rel = filepath.Join("..", "..", pkg, "testdata", name)
	}
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read fixture %s/%s: %v", pkg, name, err)
	}
	return data
}

// TestTranscodeWebP covers the writers' path for a format the office/EPUB
// containers cannot hold: a still WebP becomes an embeddable PNG rather than
// degrading to alt text.
func TestTranscodeWebP(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"still-lossy.webp", "still-lossless.webp", "still-lossy-alpha.webp"} {
		data := fixture(t, "webp", name)
		png, cfg, err := TranscodeToPNG(data)
		if err != nil {
			t.Fatalf("TranscodeToPNG(%s): %v", name, err)
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			t.Errorf("%s: config = %dx%d, want a real size", name, cfg.Width, cfg.Height)
		}
		// The result must actually be a PNG of the same dimensions — the point
		// of the transcode is that a container that only accepts PNG can embed it.
		got, kind, err := image.DecodeConfig(bytes.NewReader(png))
		if err != nil {
			t.Fatalf("%s: re-decode transcoded bytes: %v", name, err)
		}
		if kind != "png" {
			t.Errorf("%s: transcoded to %q, want png", name, kind)
		}
		if got.Width != cfg.Width || got.Height != cfg.Height {
			t.Errorf("%s: transcoded to %dx%d, want %dx%d",
				name, got.Width, got.Height, cfg.Width, cfg.Height)
		}
	}
}

// TestTranscodeWebPAnimated pins the degradation contract for the writers.
// Without the explicit check this call would reach image.Decode and fail with
// a bare "webp: invalid format" — indistinguishable from corrupt bytes — so
// the writer's log would blame the file instead of naming the real reason.
func TestTranscodeWebPAnimated(t *testing.T) {
	t.Parallel()
	data := fixture(t, "webp", "animated.webp")
	if _, _, err := TranscodeToPNG(data); !errors.Is(err, webp.ErrAnimated) {
		t.Errorf("TranscodeToPNG(animated webp) = %v, want webp.ErrAnimated", err)
	}
}

// TestTranscodeHEIC guards the case the package was originally written for.
func TestTranscodeHEIC(t *testing.T) {
	t.Parallel()
	data := fixture(t, "heif", "sips-quad-64x48.heic")
	png, cfg, err := TranscodeToPNG(data)
	if err != nil {
		t.Fatalf("TranscodeToPNG(heic): %v", err)
	}
	if cfg.Width != 64 || cfg.Height != 48 {
		t.Errorf("config = %dx%d, want 64x48", cfg.Width, cfg.Height)
	}
	if _, kind, err := image.DecodeConfig(bytes.NewReader(png)); err != nil || kind != "png" {
		t.Errorf("transcoded to %q (err %v), want png", kind, err)
	}
}

func TestTranscodeGarbage(t *testing.T) {
	t.Parallel()
	if _, _, err := TranscodeToPNG([]byte("not an image")); err == nil {
		t.Error("TranscodeToPNG(garbage): want an error")
	}
}
