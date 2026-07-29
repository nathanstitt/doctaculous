package crop

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden crop rectangles")

// loadPhoto decodes the real-photograph fixture. See testdata/README.md for
// provenance. Unlike the synthetic checkerboards in saliency_test.go it has
// natural gradients, noise and soft edges, plus a clearly off-centre subject —
// the conditions a directional assertion on a checkerboard cannot reproduce.
func loadPhoto(t *testing.T) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "hippo.jpg"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return img
}

// TestGoldenSaliencyOnPhoto pins the chosen rectangle for a real photograph.
// The synthetic tests pin the scorer's direction; this pins its behaviour on
// real image statistics, where a quality regression would otherwise be silent.
//
// A diff here is not automatically a failure — but it must be explained and the
// crop eyeballed before the golden is updated, exactly like the render goldens.
func TestGoldenSaliencyOnPhoto(t *testing.T) {
	img := loadPhoto(t)

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"square", Options{Strategy: StrategySaliency, Width: 1, Height: 1}},
		{"portrait-4x5", Options{Strategy: StrategySaliency, Width: 4, Height: 5}},
		{"wide-16x9", Options{Strategy: StrategySaliency, Width: 16, Height: 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Rect(img, tc.opts)
			if err != nil {
				t.Fatalf("Rect: %v", err)
			}
			if !got.In(img.Bounds()) {
				t.Fatalf("rect %v escapes bounds %v", got, img.Bounds())
			}
			path := filepath.Join("testdata", "golden-"+tc.name+".txt")
			if *update {
				writeGoldenRect(t, path, got)
				t.Logf("updated %s -> %v", path, got)
				return
			}
			if want := readGoldenRect(t, path); got != want {
				t.Errorf("Rect = %v, want %v\n"+
					"If this change is intentional, eyeball the crop before running -update.", got, want)
			}
		})
	}
}

// TestSaliencyBeatsCenterOnPhoto is the assertion that actually justifies the
// scorer: on a real photograph it must place the window on content rather than
// where a plain centre crop would land. A scorer that regressed to "always
// centre" would still satisfy the goldens above after an -update, but not this.
//
// The check is deliberately direction-agnostic. Which way the window moves is a
// property of the fixture, not of the algorithm, so asserting "shifts left"
// would silently overfit the test to one photograph and break the moment the
// fixture is swapped. What must hold for any subject-bearing photo is that the
// window moves off centre and that the region it lands on scores better.
func TestSaliencyBeatsCenterOnPhoto(t *testing.T) {
	img := loadPhoto(t)

	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"square", 1, 1},
		{"portrait-4x5", 4, 5},
		{"wide-16x9", 16, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Strategy: StrategySaliency, Width: tc.w, Height: tc.h}
			sal, err := Rect(img, opts)
			if err != nil {
				t.Fatalf("Rect(saliency): %v", err)
			}
			cen, err := Rect(img, Options{Strategy: StrategyCenter, Width: tc.w, Height: tc.h})
			if err != nil {
				t.Fatalf("Rect(center): %v", err)
			}
			if sal == cen {
				t.Fatalf("saliency chose the centre window %v; on a photograph with an "+
					"off-centre subject a content-aware crop should shift toward it", sal)
			}

			// The chosen window must actually score better than the centred one
			// under the same scorer — this is what "content-aware" means, and it
			// holds whichever direction the subject lies in.
			b := img.Bounds()
			sum := newIntegral(scoreMap(img, opts), b.Dx(), b.Dy())
			salScore := sum.mean(sal.Sub(b.Min))
			cenScore := sum.mean(cen.Sub(b.Min))
			if salScore <= cenScore {
				t.Errorf("saliency rect %v scores %.5f, centre rect %v scores %.5f; "+
					"the chosen window must score strictly better", sal, salScore, cen, cenScore)
			}
		})
	}
}

func writeGoldenRect(t *testing.T, path string, r image.Rectangle) {
	t.Helper()
	// Plain text, one rectangle per file, so a golden diff is readable in review.
	data := fmt.Sprintf("%d %d %d %d\n", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}

func readGoldenRect(t *testing.T, path string) image.Rectangle {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test ./pkg/crop -run TestGoldenSaliencyOnPhoto -update): %v", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only
	var r image.Rectangle
	s := bufio.NewScanner(f)
	if !s.Scan() {
		t.Fatalf("golden %s is empty", path)
	}
	if _, err := fmt.Sscanf(s.Text(), "%d %d %d %d", &r.Min.X, &r.Min.Y, &r.Max.X, &r.Max.Y); err != nil {
		t.Fatalf("golden %s is malformed (%q): %v", path, s.Text(), err)
	}
	return r
}
