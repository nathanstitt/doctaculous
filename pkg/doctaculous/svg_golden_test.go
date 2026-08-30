package doctaculous

import (
	"context"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSVGResvgGolden renders every vendored resvg-test-suite file and compares
// against committed goldens (same tolerance as the other golden suites).
// Regenerate: go test ./pkg/doctaculous -run TestSVGResvgGolden -update
func TestSVGResvgGolden(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join("..", "..", "testdata", "svg", "resvg")
	goldenDir := filepath.Join("testdata", "golden", "svg-resvg")
	var files []string
	err := filepath.WalkDir(corpus, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".svg") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 50 {
		t.Fatalf("corpus too small: %d files (vendoring incomplete?)", len(files))
	}
	for _, f := range files {
		rel, _ := filepath.Rel(corpus, f)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := OpenSVGBytes(data)
			if err != nil {
				t.Fatalf("OpenSVGBytes: %v", err)
			}
			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
			if err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
			got := img.(*image.RGBA)
			path := filepath.Join(goldenDir, strings.TrimSuffix(rel, ".svg")+".png")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				writePNG(t, path, got)
				return
			}
			want := readPNG(t, path)
			if want == nil {
				t.Fatalf("missing golden %s; run with -update", path)
			}
			if diff, n := compareImages(want, got); diff {
				t.Errorf("differs from golden: %d pixels beyond tolerance", n)
			}
		})
	}
}
