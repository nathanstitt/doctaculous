package raster

import (
	"bytes"
	"context"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// update regenerates the committed golden PNGs instead of comparing against
// them: go test ./pkg/render/raster -run TestGolden -update
var update = flag.Bool("update", false, "regenerate golden PNG fixtures")

// goldenDPI is deliberately low so goldens stay small and fast to diff in review.
const goldenDPI = 72

// pixelTolerance is the max per-channel absolute difference tolerated per pixel;
// it absorbs anti-aliasing jitter across platforms without masking real changes.
const pixelTolerance = 4

// maxDifferingFraction bounds how many pixels may differ (within tolerance) before
// a golden is considered a mismatch — a second guard against subtle drift.
const maxDifferingFraction = 0.002 // 0.2%

// TestGolden renders the first page of every core fixture and compares it to a
// committed PNG. Run with -update to (re)generate the goldens; reviewers eyeball
// the resulting diffs in the PR. The whole core corpus flows through one render
// path here, so a regression in any layer surfaces as a golden diff.
func TestGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range gen.Core {
		t.Run(f.Name, func(t *testing.T) {
			doc, err := pdf.Parse(f.Bytes())
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			pg, err := doc.Page(0)
			if err != nil {
				t.Fatalf("page: %v", err)
			}
			got, err := RenderPage(context.Background(), pg, Options{DPI: goldenDPI})
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			path := filepath.Join(dir, f.Name+".png")
			if *update {
				writePNG(t, path, got)
				t.Logf("updated %s", path)
				return
			}

			want := readPNG(t, path)
			if want == nil {
				t.Fatalf("missing golden %s; run: go test ./pkg/render/raster -run TestGolden -update", path)
			}
			if diff, n := compareImages(want, got); diff {
				t.Errorf("render differs from golden %s: %d pixels beyond tolerance (max %d)",
					path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
			}
		})
	}
}

// TestWeightedFontsGolden renders the weighted-substitute fixture (non-embedded
// Helvetica / Helvetica-Bold / Times-Italic) and compares to a committed PNG. It locks
// the standard-14 weight/slant substitution: the bold line must render heavier and the
// italic line slanted, rather than all three collapsing to a regular face. Run with
// -update to regenerate, then eyeball the PNG.
func TestWeightedFontsGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	doc, err := pdf.Parse(gen.WeightedFontsPDF())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	got, err := RenderPage(context.Background(), pg, Options{DPI: goldenDPI})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	path := filepath.Join(dir, "weighted-fonts.png")
	if *update {
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/render/raster -run TestWeightedFontsGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}

// compareImages returns whether the two images differ beyond tolerance, plus the
// count of pixels that exceeded the per-pixel tolerance.
func compareImages(want, got *image.RGBA) (differ bool, beyond int) {
	if want.Bounds() != got.Bounds() {
		return true, -1
	}
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wc := want.RGBAAt(x, y)
			gc := got.RGBAAt(x, y)
			if absU8(wc.R, gc.R) > pixelTolerance ||
				absU8(wc.G, gc.G) > pixelTolerance ||
				absU8(wc.B, gc.B) > pixelTolerance ||
				absU8(wc.A, gc.A) > pixelTolerance {
				beyond++
			}
		}
	}
	total := b.Dx() * b.Dy()
	return beyond > int(maxDifferingFraction*float64(total)), beyond
}

func absU8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readPNG loads a golden as *image.RGBA, or returns nil if the file is absent.
func readPNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
	}
	return rgba
}
