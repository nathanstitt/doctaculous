package doctaculous

import (
	"context"
	"image"
	"path/filepath"
	"strconv"
	"testing"
)

// pagedMediaGoldens are fixtures rendered with WithDefaultPaged so the document's @page
// rule drives page size, margins, and running headers/footers. Each page is compared to
// a committed PNG (html-<name>-p<i>.png). Eyeball every page on review.
var pagedMediaGoldens = []struct {
	name    string
	wantPgs int
	html    string
}{
	{
		// @page size + margins + a bottom-center page counter. Eyeball: content is inset
		// by the 40px margins on every page; a "1 / 2" / "2 / 2" footer is centered in the
		// bottom margin band. Two 220px blocks on a 300px-tall page (content height 220 =
		// 300 - 2*40) ⇒ each block fills a page ⇒ 2 pages.
		name:    "page-margins",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  @page {
    size: 360px 300px;
    margin: 40px;
    @bottom-center { content: counter(page) " / " counter(pages); color: #444444 }
  }
  body { margin: 0 }
  div { color: #ffffff }
</style></head><body>
  <div style="height:220px;background:#993333">Page one content</div>
  <div style="height:220px;background:#339933">Page two content</div>
</body></html>`,
	},
	{
		// Widows/orphans: a multi-line paragraph straddles a page boundary with
		// widows:3 orphans:3, so neither page bottom nor the next page top shows fewer
		// than 3 lines of the split paragraph. A spacer pushes the paragraph down so the
		// break lands inside it. Eyeball: the paragraph splits ≥3 lines per side.
		name:    "widows-orphans",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 400px 300px; margin: 20px }
  body { margin: 0; widows: 3; orphans: 3 }
  .spacer { height: 150px; background: #eeeeee }
  p { margin: 0; font-size: 16px; line-height: 22px; width: 200px; background: #ffeedd }
</style></head><body>
  <div class="spacer">spacer</div>
  <p>word word word word word word word word word word word word word word word word word word word word word word word word word word word word</p>
</body></html>`,
	},
	{
		name:    "page-three-headers",
		wantPgs: 1,
		html: `<!DOCTYPE html><html><head><style>
  @page {
    size: 400px 240px; margin: 40px;
    @top-left { content: "L"; color:#333 }
    @top-center { content: "CENTER"; color:#333 }
    @top-right { content: "R"; color:#333 }
  }
  body { margin: 0 }
</style></head><body><div style="height:160px;background:#cccccc">x</div></body></html>`,
	},
	{
		// CSS GCPM running header via string()/string-set: each page's @top-left header
		// shows the most recent <h2> heading (string-set sect content()). Each <h2>+<.blk>
		// pair (~21px heading + 150px block ≈ 171px) fits the 188px content height
		// (260 - 2*36), but two pairs (~342px) do not ⇒ the second pair breaks to page 2 ⇒
		// 2 pages. Eyeball: page 0's top-left header reads "Alpha", page 1's reads "Beta".
		name:    "running-header",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
    @page { size: 400px 260px; margin: 36px 20px; @top-left { content: string(sect); color:#555; font-size:11px } }
    body { margin: 0 }
    h2 { font-size:18px; margin:0 }
    h2.s { string-set: sect content() }
    .blk { height: 150px }
  </style></head><body>
    <h2 class="s">Alpha</h2><div class="blk" style="background:#fdd">one</div>
    <h2 class="s">Beta</h2><div class="blk" style="background:#dfd">two</div>
  </body></html>`,
	},
	{
		// @page bleed + crop marks: the page bitmap is the media box (trim 300x220 + 16px
		// bleed on each side => 332x252); content sits inside the trim box; thin black crop
		// marks point at the four trim corners in the bleed band, and cross marks straddle
		// the edge midpoints. Eyeball: 8 corner marks + 4 edge crosses, content inset, a
		// white bleed margin around a single page.
		name:    "page-crop-marks",
		wantPgs: 1,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 300px 220px; margin: 20px; bleed: 16px; marks: crop cross }
  body { margin: 0 }
</style></head><body><div style="height:180px;background:#bcd6f0">trim box content</div></body></html>`,
	},
}

// TestHTMLPagedMediaGolden renders @page-driven paginated documents end to end (via
// WithDefaultPaged) and compares each page to a committed PNG. Run with -update to
// regenerate, then eyeball every page PNG in review — the margin band insets, the
// centered running footer, and the widows/orphans line splits are all visual.
func TestHTMLPagedMediaGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	for _, f := range pagedMediaGoldens {
		t.Run(f.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes([]byte(f.html), WithDefaultPaged())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			if doc.PageCount() != f.wantPgs {
				t.Fatalf("PageCount = %d, want %d", doc.PageCount(), f.wantPgs)
			}
			for i := 0; i < doc.PageCount(); i++ {
				img, err := doc.RasterizePage(context.Background(), i, RasterOptions{DPI: goldenDPI})
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
					t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestHTMLPagedMediaGolden -update", path)
				}
				if diff, n := compareImages(want, got); diff {
					t.Errorf("page %d differs from golden %s: %d pixels beyond tolerance (max %d)",
						i, path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
				}
			}
		})
	}
}
