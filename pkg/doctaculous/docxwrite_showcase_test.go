package doctaculous

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDOCXWriteShowcase drives the DOCX writer with the full htmldoc specimen —
// every HTML/CSS/image slice the engine supports, fetched over loopback HTTP —
// then reopens the produced package and pins two goldens: the reopened
// document's Markdown (htmldoc.docx.md — hermetic, no binary .docx committed)
// and a raster of its first page (docxout-htmldoc-p1.png). Together they lock
// the writer's structural coverage AND that its output lays out sanely. Run
// with -update to regenerate, then eyeball both in review.
func TestDOCXWriteShowcase(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(htmlDocDir)))
	defer srv.Close()

	doc, err := OpenURL(srv.URL+"/index.html", WithDefaultPaged(), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	var pkg bytes.Buffer
	if err := doc.WriteDOCX(context.Background(), &pkg, DOCXOptions{}); err != nil {
		t.Fatalf("WriteDOCX: %v", err)
	}
	reopened, err := OpenDOCXBytes(pkg.Bytes())
	if err != nil {
		t.Fatalf("OpenDOCXBytes rejects the showcase package: %v", err)
	}

	dir := filepath.Join("testdata", "golden")
	mdPath := filepath.Join(dir, "htmldoc.docx.md")
	var md bytes.Buffer
	if err := reopened.WriteMarkdown(context.Background(), &md, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}

	img, err := reopened.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}
	pngPath := filepath.Join(dir, "docxout-htmldoc-p1.png")

	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mdPath, md.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		writePNG(t, pngPath, got)
		t.Logf("updated %s and %s", mdPath, pngPath)
		return
	}

	wantMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestDOCXWriteShowcase -update", mdPath)
	}
	if !bytes.Equal(md.Bytes(), wantMD) {
		t.Errorf("showcase docx round-trip markdown differs from golden %s (len %d vs %d); regenerate with -update and eyeball the diff",
			mdPath, md.Len(), len(wantMD))
	}
	wantPNG := readPNG(t, pngPath)
	if wantPNG == nil {
		t.Fatalf("missing golden %s; run with -update", pngPath)
	}
	if diff, n := compareImages(wantPNG, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", pngPath, n)
	}
}
