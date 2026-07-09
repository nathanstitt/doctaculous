package doctaculous

import (
	"context"
	"image"
	"path/filepath"
	"strconv"
	"testing"
)

// paginatedGoldens are fixtures rendered with WithPageSize so they span multiple pages;
// each page is compared to a committed PNG (html-<name>-p<i>.png). Distinct per-block
// background colors make each page visually obvious on review.
var paginatedGoldens = []struct {
	name    string
	pageW   float64
	pageH   float64
	wantPgs int
	html    string
}{
	{
		// A forced page break exercises the break-before path alongside the
		// height-overflow path.
		name:    "paginate",
		pageW:   240,
		pageH:   200,
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  body { margin: 0; }
  div { height: 80px; color: #ffffff; }
</style></head><body>
  <div style="background:#993333">Block one</div>
  <div style="background:#339933">Block two</div>
  <div style="background:#333399;break-before:page">Block three (forced new page)</div>
  <div style="background:#999933">Block four</div>
</body></html>`,
	},
	{
		// C4: the body border FRAGMENTS across pages. Eyeball: page 0 has the blue TOP
		// border + both sides (and the first block sits below the top border); the middle
		// page has only the LEFT/RIGHT sides (no spurious top edge); the last page has the
		// BOTTOM border + both sides. Three 200pt blocks on 250pt pages => 3 pages.
		name:    "paginate-body-border",
		pageW:   400,
		pageH:   250,
		wantPgs: 3,
		html: `<!DOCTYPE html><html><body style="border:10px solid #2222cc;margin:0">
  <div style="height:200px;margin:0;background:#ffdddd">one</div>
  <div style="height:200px;margin:0;background:#ddffdd">two</div>
  <div style="height:200px;margin:0;background:#ddddff">three</div>
</body></html>`,
	},
}

// TestHTMLPaginatedGolden renders a paginated document end to end and compares each
// page to a committed PNG, mirroring TestHTMLGolden but looping over pages. Run with
// -update to regenerate, then eyeball every page PNG in review. The four 80pt blocks
// (Ys 0/80/160/240) on 200pt pages plus one forced break-before produce two pages:
// page 0 = blocks 1+2 (both fit: bottom 160 <= 200), page 1 = blocks 3+4 (block 3 is
// forced onto a fresh page by break-before, and block 4 fits below it). This locks in
// BOTH the between-block overflow split would-be (blocks stack two-per-page) AND the
// forced break-before.
func TestHTMLPaginatedGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	for _, f := range paginatedGoldens {
		t.Run(f.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes([]byte(f.html), WithPageSize(f.pageW, f.pageH), WithBundledFonts())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			if doc.PageCount() != f.wantPgs {
				t.Fatalf("PageCount = %d, want %d", doc.PageCount(), f.wantPgs)
			}

			for i := 0; i < doc.PageCount(); i++ {
				img, err := doc.RasterizePage(context.Background(), i, RasterOptions{DPI: goldenDPI, BundledFonts: true})
				if err != nil {
					t.Fatalf("RasterizePage(%d): %v", i, err)
				}
				got, ok := img.(*image.RGBA)
				if !ok {
					t.Fatalf("rasterized image is %T, want *image.RGBA", img)
				}

				path := filepath.Join(dir, "html-"+f.name+"-p"+strconv.Itoa(i)+".png")
				if *update {
					writePNG(t, path, got)
					t.Logf("updated %s", path)
					continue
				}
				want := readPNG(t, path)
				if want == nil {
					t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestHTMLPaginatedGolden -update", path)
				}
				if diff, n := compareImages(want, got); diff {
					t.Errorf("page %d differs from golden %s: %d pixels beyond tolerance (max %d)",
						i, path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
				}
			}
		})
	}
}
