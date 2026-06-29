package doctaculous

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
)

// htmlDocDir is the on-disk fixture tree for the end-to-end HTML showcase: an
// index.html that links css/reset.css + css/main.css, declares an @font-face served
// from fonts/, and references images under img/. It is served over loopback HTTP so
// the test exercises OpenURL + the HTTP ResourceLoader resolving relative refs
// across nested directories — the same path a real document on the web takes.
var htmlDocDir = filepath.Join("..", "..", "testdata", "htmldoc")

// htmlDocPages is the number of US-Letter pages the showcase fragments into. It is
// asserted so an accidental reflow that adds or drops a page fails loudly (not just
// a per-page pixel diff). Update it (and regenerate goldens) when the document
// intentionally changes length.
const htmlDocPages = 9

// TestHTMLDocShowcase renders the multi-file "uber" specimen document over HTTP and
// compares every paginated page to a committed PNG (htmldoc-p<i>.png). It is the one
// golden that drives the whole HTML pipeline through OpenURL: multi-<link> cascade,
// @font-face WOFF2 download, PNG/JPEG/GIF decode, and fixed-height pagination, all
// from relative refs resolved against the document URL. Run with -update to
// regenerate, then eyeball every page PNG in review.
func TestHTMLDocShowcase(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(htmlDocDir)))
	defer srv.Close()

	doc, err := OpenURL(srv.URL+"/index.html", WithPageSize(LetterWidthPt, LetterHeightPt))
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}

	if !*update && doc.PageCount() != htmlDocPages {
		t.Fatalf("PageCount = %d, want %d (the showcase reflowed; eyeball it, then update htmlDocPages and regenerate goldens)",
			doc.PageCount(), htmlDocPages)
	}

	dir := filepath.Join("testdata", "golden")
	for i := 0; i < doc.PageCount(); i++ {
		img, err := doc.RasterizePage(context.Background(), i, RasterOptions{DPI: goldenDPI})
		if err != nil {
			t.Fatalf("RasterizePage(%d): %v", i, err)
		}
		got, ok := img.(*image.RGBA)
		if !ok {
			t.Fatalf("rasterized image is %T, want *image.RGBA", img)
		}

		path := filepath.Join(dir, "htmldoc-p"+strconv.Itoa(i)+".png")
		if *update {
			writePNG(t, path, got)
			t.Logf("updated %s", path)
			continue
		}
		want := readPNG(t, path)
		if want == nil {
			t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestHTMLDocShowcase -update", path)
		}
		if diff, n := compareImages(want, got); diff {
			t.Errorf("page %d differs from golden %s: %d pixels beyond tolerance (max %d)",
				i, path, n, int(maxDifferingFraction*float64(got.Bounds().Dx()*got.Bounds().Dy())))
		}
	}
}
