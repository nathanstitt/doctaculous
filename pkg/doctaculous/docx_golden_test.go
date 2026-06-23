package doctaculous

import (
	"bytes"
	"context"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
)

// update regenerates the committed golden PNGs instead of comparing:
// go test ./pkg/doctaculous -run TestDOCXGolden -update
var update = flag.Bool("update", false, "regenerate DOCX golden PNG fixtures")

// goldenDPI keeps goldens small and quick to eyeball in review, matching the PDF
// golden DPI.
const goldenDPI = 72

const (
	pixelTolerance       = 4
	maxDifferingFraction = 0.002 // 0.2%
)

// TestDOCXGolden lays out and rasterizes the first page of every core DOCX
// fixture end to end (parse → cascade → lower → reflow → paint → raster) and
// compares it to a committed PNG. It also checks the fixture's declared page
// count. Run with -update to regenerate the goldens and eyeball the diffs.
func TestDOCXGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range gendocx.Core {
		t.Run(f.Name, func(t *testing.T) {
			doc, err := OpenDOCXBytes(f.Bytes())
			if err != nil {
				t.Fatalf("OpenDOCXBytes: %v", err)
			}
			if doc.PageCount() != f.Pages {
				t.Errorf("page count = %d, want %d", doc.PageCount(), f.Pages)
			}
			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
			if err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
			got, ok := img.(*image.RGBA)
			if !ok {
				t.Fatalf("rasterized image is %T, want *image.RGBA", img)
			}

			path := filepath.Join(dir, "docx-"+f.Name+".png")
			if *update {
				writePNG(t, path, got)
				t.Logf("updated %s", path)
				return
			}
			want := readPNG(t, path)
			if want == nil {
				t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestDOCXGolden -update", path)
			}
			if diff, n := compareImages(want, got); diff {
				t.Errorf("render differs from golden %s: %d pixels beyond tolerance (max %d)",
					path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
			}
		})
	}
}

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
