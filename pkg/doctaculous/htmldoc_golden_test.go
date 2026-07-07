package doctaculous

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
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
const htmlDocPages = 15

// TestHTMLDocShowcase renders the multi-file "uber" specimen document over HTTP and
// compares every paginated page to a committed PNG (htmldoc-p<i>.png). It is the one
// golden that drives the whole HTML pipeline through OpenURL: multi-<link> cascade,
// @font-face WOFF2 download, PNG/JPEG/GIF decode, and fixed-height pagination, all
// from relative refs resolved against the document URL. Run with -update to
// regenerate, then eyeball every page PNG in review.
func TestHTMLDocShowcase(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(htmlDocDir)))
	defer srv.Close()

	// WithDefaultPaged drives pagination from the document's own @page rule (Letter
	// size, a bottom margin band, and a running page-counter footer) — exercising the
	// full paged-media path end to end, not just fixed-height slicing.
	doc, err := OpenURL(srv.URL+"/index.html", WithDefaultPaged())
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

// TestHTMLDocMarkdown exports the same multi-file showcase specimen to Markdown and
// plain text, comparing to committed htmldoc.md / htmldoc.txt goldens. It drives the
// conversion path end to end on a real, feature-dense document (headings, lists,
// tables, links, emphasis, images), the text-side counterpart to the raster showcase.
// Run with -update to regenerate, then eyeball the committed .md/.txt in review.
func TestHTMLDocMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(htmlDocDir)))
	defer srv.Close()

	doc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	dir := filepath.Join("testdata", "golden")
	for _, tc := range []struct {
		name string
		opts MarkdownOptions
	}{
		{"htmldoc.md", MarkdownOptions{}},
		{"htmldoc.txt", MarkdownOptions{Plain: true}},
	} {
		var out bytes.Buffer
		if err := doc.WriteMarkdown(context.Background(), &out, tc.opts); err != nil {
			t.Fatalf("WriteMarkdown(%s): %v", tc.name, err)
		}
		path := filepath.Join(dir, tc.name)
		if *update {
			if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("updated %s", path)
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestHTMLDocMarkdown -update", path)
		}
		if !bytes.Equal(want, out.Bytes()) {
			t.Errorf("%s differs from golden; run -update and eyeball the diff", tc.name)
		}
	}
}
