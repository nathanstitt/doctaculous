package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// matrixHTML is the HTML source fixture for the conversion-matrix tests. The
// heading survives every structure output (Markdown/text/HTML) and, via the
// PDF writer's extractable bundled fonts, the PDF round trip too.
const matrixHTML = `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body>
<h1>Matrix Title</h1>
<p>An introductory paragraph of body text.</p>
</body></html>`

// matrixXLSX builds a one-sheet workbook carrying the matrix marker text.
func matrixXLSX() []byte {
	sheet := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>Name</t></is></c><c r="B1" t="inlineStr"><is><t>Qty</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>Matrix Title</t></is></c><c r="B2"><v>5</v></c></row>
</sheetData></worksheet>`
	return genxlsx.New().AddSheet("Data", sheet).Bytes()
}

// matrixPDF renders matrixHTML to PDF bytes with bundled fonts, so the text is
// reliably extractable regardless of the host's installed fonts (the same
// trick as the CLI tests' writeTestPDF).
func matrixPDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := ConvertHTMLToPDF(context.Background(), strings.NewReader(matrixHTML), &buf,
		PDFOptions{BundledFonts: true})
	if err != nil {
		t.Fatalf("build pdf fixture: %v", err)
	}
	return buf.Bytes()
}

// TestConvertMatrix iterates every (From, To) pair: pairs CanConvert allows
// must convert successfully with spot-checked output, and pairs it rejects
// must fail from Convert with the same sentinel error.
func TestConvertMatrix(t *testing.T) {
	ctx := context.Background()
	inputs := map[Format][]byte{
		FormatPDF:      matrixPDF(t),
		FormatDOCX:     gendocx.Core[0].Bytes(),
		FormatHTML:     []byte(matrixHTML),
		FormatMarkdown: []byte("# Matrix Title\n\nAn introductory paragraph of body text.\n"),
		FormatText:     []byte("Matrix Title\n\nplain body lines\n"),
		FormatCSV:      []byte("Name,Qty\nMatrix Title,5\n"),
		FormatTSV:      []byte("Name\tQty\nMatrix Title\t5\n"),
		FormatXLSX:     matrixXLSX(),
	}
	// A text fragment each input's content must carry into structure outputs.
	wantText := map[Format]string{
		FormatPDF:      "Matrix Title",
		FormatDOCX:     "quick brown fox",
		FormatHTML:     "Matrix Title",
		FormatMarkdown: "Matrix Title",
		FormatText:     "Matrix Title",
		FormatCSV:      "Matrix Title",
		FormatTSV:      "Matrix Title",
		FormatXLSX:     "Matrix Title",
	}
	// CSV/TSV output carries tables only; inputs whose fixture has no table
	// legitimately produce empty output there.
	tableless := map[Format]bool{
		FormatPDF:      true, // matrixPDF has a heading + paragraph, no ruled table
		FormatHTML:     true,
		FormatMarkdown: true,
		FormatText:     true,
		FormatDOCX:     true, // the paragraph fixture
	}
	sentinels := []error{ErrUnknownFormat, ErrUnsupportedFormat, ErrSameFormat}
	all := []Format{FormatPDF, FormatDOCX, FormatHTML, FormatMarkdown, FormatText, FormatCSV, FormatTSV, FormatXLSX, FormatPNG, FormatJPEG}

	for _, from := range all {
		for _, to := range all {
			name := string(from) + "->" + string(to)
			opts := ConvertOptions{
				From:         from,
				To:           to,
				BundledFonts: true,
				Image:        ImageOptions{Raster: RasterOptions{DPI: 72}},
			}

			if capErr := CanConvert(from, to); capErr != nil {
				err := Convert(ctx, bytes.NewReader(inputs[from]), io.Discard, opts)
				if err == nil {
					t.Errorf("%s: want error (%v), got nil", name, capErr)
					continue
				}
				for _, s := range sentinels {
					if errors.Is(capErr, s) != errors.Is(err, s) {
						t.Errorf("%s: Convert error %v does not match CanConvert class %v", name, err, capErr)
					}
				}
				continue
			}

			var out bytes.Buffer
			if err := Convert(ctx, bytes.NewReader(inputs[from]), &out, opts); err != nil {
				t.Errorf("%s: Convert: %v", name, err)
				continue
			}
			if (to == FormatCSV || to == FormatTSV) && tableless[from] {
				// Tables-only output from a table-less fixture: empty by design.
				continue
			}
			if out.Len() == 0 {
				t.Errorf("%s: empty output", name)
				continue
			}
			switch to {
			case FormatPDF:
				if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-")) {
					t.Errorf("%s: output lacks a PDF header", name)
				}
			case FormatDOCX:
				if !bytes.HasPrefix(out.Bytes(), []byte("PK\x03\x04")) {
					t.Errorf("%s: output is not a ZIP package", name)
				}
				// The produced package must reopen through our own reader and carry
				// the source text.
				doc, err := OpenDOCXBytes(out.Bytes())
				if err != nil {
					t.Errorf("%s: reopening produced docx: %v", name, err)
					break
				}
				var md bytes.Buffer
				if err := doc.WriteText(ctx, &md, MarkdownOptions{}); err != nil {
					t.Errorf("%s: reading produced docx: %v", name, err)
				} else if !strings.Contains(md.String(), wantText[from]) {
					t.Errorf("%s: reopened docx missing %q:\n%s", name, wantText[from], md.String())
				}
			case FormatPNG:
				if !bytes.HasPrefix(out.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
					t.Errorf("%s: output lacks a PNG signature", name)
				}
			case FormatJPEG:
				if !bytes.HasPrefix(out.Bytes(), []byte("\xFF\xD8\xFF")) {
					t.Errorf("%s: output lacks a JPEG signature", name)
				}
			case FormatMarkdown, FormatText, FormatHTML:
				if !strings.Contains(out.String(), wantText[from]) {
					t.Errorf("%s: output missing %q:\n%s", name, wantText[from], out.String())
				}
			case FormatCSV, FormatTSV:
				if !strings.Contains(out.String(), wantText[from]) {
					t.Errorf("%s: output missing %q:\n%s", name, wantText[from], out.String())
				}
			}
		}
	}
}

// TestConvertAutoDetectsFrom verifies Convert detects the input format from
// content when From is unset.
func TestConvertAutoDetectsFrom(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	err := Convert(ctx, bytes.NewReader(matrixPDF(t)), &out,
		ConvertOptions{To: FormatMarkdown})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(out.String(), "Matrix Title") {
		t.Errorf("output missing extracted text:\n%s", out.String())
	}
	// Undetectable input without From errors cleanly.
	err = Convert(ctx, strings.NewReader("plain prose, no format"), io.Discard,
		ConvertOptions{To: FormatMarkdown})
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("undetectable input: want ErrUnknownFormat, got %v", err)
	}
}

// TestConvertSelectsImagePage verifies Convert-to-image honors Image.Page.
func TestConvertSelectsImagePage(t *testing.T) {
	ctx := context.Background()
	// A document tall enough to paginate onto two US-Letter pages.
	html := `<html><body><div style="height: 1800px">first</div><p>second page</p></body></html>`
	opts := ConvertOptions{
		From:         FormatHTML,
		To:           FormatPNG,
		HTML:         []HTMLOption{WithPageSize(LetterWidthPt, LetterHeightPt)},
		BundledFonts: true,
		Image:        ImageOptions{Page: 1, Raster: RasterOptions{DPI: 36}},
	}
	var page1 bytes.Buffer
	if err := Convert(ctx, strings.NewReader(html), &page1, opts); err != nil {
		t.Fatalf("Convert page 1: %v", err)
	}
	opts.Image.Page = 0
	var page0 bytes.Buffer
	if err := Convert(ctx, strings.NewReader(html), &page0, opts); err != nil {
		t.Fatalf("Convert page 0: %v", err)
	}
	if bytes.Equal(page0.Bytes(), page1.Bytes()) {
		t.Errorf("pages 0 and 1 rendered identically; Image.Page not honored")
	}
	// An out-of-range page errors.
	opts.Image.Page = 99
	if err := Convert(ctx, strings.NewReader(html), io.Discard, opts); err == nil {
		t.Errorf("page 99: want error, got nil")
	}
}

// TestConvertFile verifies output-format inference from the extension and file
// creation.
func TestConvertFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.html")
	if err := os.WriteFile(inPath, []byte(matrixHTML), 0o600); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.md")
	if err := ConvertFile(ctx, inPath, outPath, ConvertOptions{BundledFonts: true}); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.Contains(string(data), "# Matrix Title") {
		t.Errorf("markdown output missing heading:\n%s", data)
	}

	// Unrecognizable output extension without To errors.
	err = ConvertFile(ctx, inPath, filepath.Join(dir, "out.xyz"), ConvertOptions{})
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("out.xyz: want ErrUnknownFormat, got %v", err)
	}
	// Explicit To overrides the extension.
	outAs := filepath.Join(dir, "out.dat")
	if err := ConvertFile(ctx, inPath, outAs, ConvertOptions{To: FormatText, BundledFonts: true}); err != nil {
		t.Fatalf("ConvertFile To=text: %v", err)
	}
	if data, _ := os.ReadFile(outAs); !strings.Contains(string(data), "Matrix Title") {
		t.Errorf("text output missing content:\n%s", data)
	}
	// Same-format via extension inference.
	err = ConvertFile(ctx, inPath, filepath.Join(dir, "out.html"), ConvertOptions{})
	if !errors.Is(err, ErrSameFormat) {
		t.Errorf("html->html: want ErrSameFormat, got %v", err)
	}
}

// TestConvertShimEquivalence pins that the legacy one-shot wrappers produce
// byte-identical output to the generic Convert call they now delegate to.
func TestConvertShimEquivalence(t *testing.T) {
	ctx := context.Background()

	var viaShim, viaGeneric bytes.Buffer
	if err := ConvertHTMLToMarkdown(ctx, strings.NewReader(matrixHTML), &viaShim, MarkdownOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToMarkdown: %v", err)
	}
	err := Convert(ctx, strings.NewReader(matrixHTML), &viaGeneric,
		ConvertOptions{From: FormatHTML, To: FormatMarkdown})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !bytes.Equal(viaShim.Bytes(), viaGeneric.Bytes()) {
		t.Errorf("ConvertHTMLToMarkdown diverges from the generic Convert")
	}

	pdfSrc := matrixPDF(t)
	viaShim.Reset()
	viaGeneric.Reset()
	if err := ConvertPDFToHTML(ctx, bytes.NewReader(pdfSrc), &viaShim, HTMLWriteOptions{}); err != nil {
		t.Fatalf("ConvertPDFToHTML: %v", err)
	}
	err = Convert(ctx, bytes.NewReader(pdfSrc), &viaGeneric,
		ConvertOptions{From: FormatPDF, To: FormatHTML})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !bytes.Equal(viaShim.Bytes(), viaGeneric.Bytes()) {
		t.Errorf("ConvertPDFToHTML diverges from the generic Convert")
	}
}

// TestDocumentWriteSameFormat verifies the generic Write rejects the
// document's own format while the format-specific writers stay unrestricted.
func TestDocumentWriteSameFormat(t *testing.T) {
	ctx := context.Background()
	doc, err := OpenHTMLBytes([]byte(matrixHTML))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	err = doc.Write(ctx, io.Discard, FormatHTML, ConvertOptions{})
	if !errors.Is(err, ErrSameFormat) {
		t.Errorf("Write(html doc, html): want ErrSameFormat, got %v", err)
	}
	// The format-specific method still works (deliberate policy asymmetry).
	var out bytes.Buffer
	if err := doc.WriteHTML(ctx, &out, HTMLWriteOptions{}); err != nil {
		t.Errorf("WriteHTML on an HTML doc must keep working: %v", err)
	}
	if out.Len() == 0 {
		t.Errorf("WriteHTML produced no output")
	}

	pdfDoc, err := OpenBytes(matrixPDF(t))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	err = pdfDoc.Write(ctx, io.Discard, FormatPDF, ConvertOptions{})
	if !errors.Is(err, ErrSameFormat) {
		t.Errorf("Write(pdf doc, pdf): want ErrSameFormat, got %v", err)
	}
}
