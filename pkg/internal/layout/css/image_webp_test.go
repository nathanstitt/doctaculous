package css

import (
	"os"
	"path/filepath"
	"testing"
)

func webpFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "webp", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestDecodeImageBytesWebP covers both routes a <img src="...webp"> can take
// into the decoder: the content-type fast path, when a loader reports
// image/webp, and the sniffing fallback, when it reports nothing useful (a
// DirLoader whose extension map misses, or a data: URI without a type).
func TestDecodeImageBytesWebP(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"still-lossy.webp", "still-lossless.webp", "still-lossy-alpha.webp"} {
		data := webpFixture(t, name)
		for _, ct := range []string{"image/webp", "image/webp; charset=binary", ""} {
			img, err := decodeImageBytes(data, ct)
			if err != nil {
				t.Fatalf("decodeImageBytes(%s, %q) = %v, want success", name, ct, err)
			}
			if b := img.Bounds(); b.Dx() <= 0 || b.Dy() <= 0 {
				t.Errorf("decodeImageBytes(%s, %q) bounds = %v, want a real image", name, ct, b)
			}
		}
	}
}

// TestDecodeImageBytesWebPAnimated pins the degradation: an animated WebP is
// refused, so the cache records a miss and the caller draws a placeholder
// rather than rendering a broken image. Both routes must refuse — the
// content-type path calls pkg/webp directly, while the sniffing path reaches
// x/image through image.Decode.
func TestDecodeImageBytesWebPAnimated(t *testing.T) {
	t.Parallel()
	data := webpFixture(t, "animated.webp")
	for _, ct := range []string{"image/webp", ""} {
		if _, err := decodeImageBytes(data, ct); err == nil {
			t.Errorf("decodeImageBytes(animated, %q) succeeded, want an error", ct)
		}
	}
}
