package doctaculous

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/epub"
	genepub "github.com/nathanstitt/doctaculous/testdata/gen/epub"
)

// epubSpecimen builds the golden book: two chapters exercising package CSS,
// inline styles, an image resolved from the container, and structure the
// conversion writers carry.
func epubSpecimen() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 28, 16))
	for y := range 16 {
		for x := range 28 {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x88, B: 0xCC, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}

	ch1 := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head>
<link rel="stylesheet" type="text/css" href="styles.css"/>
<style>.note { color: #C82020 }</style>
<title>Chapter One</title></head>
<body>
<h1>Chapter One</h1>
<p>Opening prose with <em>emphasis</em> and <strong>weight</strong>, styled by the package sheet.</p>
<p class="note">An inline-styled red note.</p>
<ul><li>First point</li><li>Second point</li></ul>
<p><img src="images/figure1.png" alt="a figure"/></p>
</body></html>`
	ch2 := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter Two</title></head>
<body>
<h1>Chapter Two</h1>
<p>The second chapter starts on a fresh page when paginated, and its
<a href="https://example.com/">reference link</a> survives conversion.</p>
<table><tr><th>Term</th><th>Meaning</th></tr><tr><td>OCF</td><td>the container format</td></tr></table>
</body></html>`

	return genepub.New().
		SetTitle("Specimen Book").
		SetCSS("h1 { color: #2E74B5 } p { margin: 0 0 8px 0 }").
		AddChapter("ch1.xhtml", ch1).
		AddChapter("ch2.xhtml", ch2).
		AddMedia("images/figure1.png", buf.Bytes()).
		Bytes()
}

// TestEPUBGolden renders the specimen's first page end to end — the EPUB
// visual entry. Run with -update, then eyeball.
func TestEPUBGolden(t *testing.T) {
	doc, err := OpenEPUBBytes(epubSpecimen(), WithViewportWidth(460), WithPageSize(460, 600), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenEPUBBytes: %v", err)
	}
	if doc.Format() != FormatEPUB {
		t.Errorf("Format() = %q, want epub", doc.Format())
	}
	if doc.PageCount() < 2 {
		t.Errorf("PageCount = %d, want >= 2 (chapter two breaks to a new page)", doc.PageCount())
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "epub-specimen.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestEPUBGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}

// TestEPUBDetectionAndConvert pins the unified-conversion wiring, structure
// conversion, and the DRM refusal.
func TestEPUBDetectionAndConvert(t *testing.T) {
	book := epubSpecimen()
	doc, err := OpenBytes(book)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if doc.Format() != FormatEPUB {
		t.Errorf("detected format = %q, want epub", doc.Format())
	}

	var md bytes.Buffer
	if err := Convert(context.Background(), bytes.NewReader(book), &md, ConvertOptions{To: FormatMarkdown}); err != nil {
		t.Fatalf("Convert epub→md: %v", err)
	}
	got := md.String()
	for _, want := range []string{
		"# Chapter One",
		"_emphasis_",
		"- First point",
		"# Chapter Two",
		"[reference link](https://example.com/)",
		"| Term | Meaning |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, got)
		}
	}

	// A DRM-protected container is refused with the typed error.
	locked := withEncryptionXML(t, book)
	if _, err := OpenBytes(locked); !errors.Is(err, epub.ErrEncrypted) {
		t.Errorf("DRM epub: want epub.ErrEncrypted, got %v", err)
	}
}

// withEncryptionXML appends a META-INF/encryption.xml to a container.
func withEncryptionXML(t *testing.T, src []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	w, err := zw.Create("META-INF/encryption.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container"/>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
