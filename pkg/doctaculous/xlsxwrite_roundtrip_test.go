package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// xlsxOf converts an opened document to .xlsx bytes.
func xlsxOf(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.WriteXLSX(context.Background(), &buf, XLSXOptions{}); err != nil {
		t.Fatalf("WriteXLSX: %v", err)
	}
	return buf.Bytes()
}

// TestXLSXWriteRoundTripParity is the writer's core guarantee for single-table
// documents: HTML → WriteXLSX → reopen (via pkg/xlsx) → WriteMarkdown produces
// the same Markdown as converting the HTML directly — spans, header rows, and
// values all survive the workbook.
func TestXLSXWriteRoundTripParity(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"simple", `<html><body><table>
			<tr><th>Item</th><th>Qty</th></tr>
			<tr><td>Widgets</td><td>5</td></tr>
			<tr><td>Gadgets</td><td>7.25</td></tr>
			</table></body></html>`},
		{"colspan", `<html><body><table>
			<tr><th colspan="2">Wide Header</th></tr>
			<tr><td>a</td><td>b</td></tr>
			</table></body></html>`},
		{"rowspan", `<html><body><table>
			<tr><th>K</th><th>V</th></tr>
			<tr><td rowspan="2">tall</td><td>r1</td></tr>
			<tr><td>r2</td></tr>
			</table></body></html>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := OpenHTMLBytes([]byte(c.html), WithBundledFonts())
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			want := markdownOf(t, src)

			reopened, err := OpenXLSXBytes(xlsxOf(t, src))
			if err != nil {
				t.Fatalf("OpenXLSXBytes: %v", err)
			}
			got := markdownOf(t, reopened)
			if got != want {
				t.Errorf("round trip diverged:\n--- direct ---\n%s\n--- via xlsx ---\n%s", want, got)
			}
		})
	}
}

// TestXLSXWriteMultiTableSheets verifies each table becomes its own worksheet,
// named from its caption (sanitized) or "Table N".
func TestXLSXWriteMultiTableSheets(t *testing.T) {
	src, err := OpenHTMLBytes([]byte(`<html><body>
	<p>prose to drop</p>
	<table><caption>Q1: Sales/Totals</caption><tr><td>alpha</td></tr></table>
	<table><tr><td>beta</td></tr></table>
	</body></html>`), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	reopened, err := OpenXLSXBytes(xlsxOf(t, src))
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	got := markdownOf(t, reopened)
	// Both sheets appear with their names as headings (forbidden chars became
	// spaces in the caption-derived name; the heading's whitespace collapses on
	// the way back through the HTML pipeline).
	for _, want := range []string{"## Q1 Sales Totals", "## Table 2", "alpha", "beta"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "prose to drop") {
		t.Errorf("prose leaked into the workbook:\n%s", got)
	}
}

// TestCSVToXLSXAndBack pins the spreadsheet-family loop: csv → xlsx → csv is
// byte-identical (numbers stay numbers, the header row survives).
func TestCSVToXLSXAndBack(t *testing.T) {
	ctx := context.Background()
	csvSrc := "Name,Qty,Price\nWidgets,5,9.99\nGadgets,12,0.5\n"

	var wb bytes.Buffer
	err := Convert(ctx, strings.NewReader(csvSrc), &wb, ConvertOptions{From: FormatCSV, To: FormatXLSX, BundledFonts: true})
	if err != nil {
		t.Fatalf("csv->xlsx: %v", err)
	}
	var back bytes.Buffer
	err = Convert(ctx, bytes.NewReader(wb.Bytes()), &back, ConvertOptions{To: FormatCSV, BundledFonts: true})
	if err != nil {
		t.Fatalf("xlsx->csv: %v", err)
	}
	if back.String() != csvSrc {
		t.Errorf("csv->xlsx->csv diverged:\n--- got ---\n%s--- want ---\n%s", back.String(), csvSrc)
	}
}

// TestPDFToXLSX proves the extraction path: a ruled table inside a PDF lands as
// a workbook sheet.
func TestPDFToXLSX(t *testing.T) {
	const tableHTML = `<!DOCTYPE html><html><head><style>body{margin:0}
	table{border-collapse:collapse} td,th{border:1px solid black;padding:6px}</style></head><body>
	<table><tr><th>Item</th><th>Qty</th></tr><tr><td>Widgets</td><td>5</td></tr></table>
	</body></html>`
	var pdf bytes.Buffer
	err := ConvertHTMLToPDF(context.Background(), strings.NewReader(tableHTML), &pdf, PDFOptions{BundledFonts: true})
	if err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	var wb bytes.Buffer
	err = Convert(context.Background(), bytes.NewReader(pdf.Bytes()), &wb, ConvertOptions{To: FormatXLSX})
	if err != nil {
		t.Fatalf("pdf->xlsx: %v", err)
	}
	reopened, err := OpenXLSXBytes(wb.Bytes())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	got := markdownOf(t, reopened)
	for _, want := range []string{"Item", "Widgets", "5"} {
		if !strings.Contains(got, want) {
			t.Errorf("pdf->xlsx lost %q:\n%s", want, got)
		}
	}
}

// TestXLSXWriteDeterministic pins byte-identical output for identical input.
func TestXLSXWriteDeterministic(t *testing.T) {
	src, err := OpenHTMLBytes([]byte(`<html><body><table><caption>C</caption>
	<tr><th>A</th></tr><tr><td>1</td></tr></table></body></html>`), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	if !bytes.Equal(xlsxOf(t, src), xlsxOf(t, src)) {
		t.Errorf("two writes of the same tree differ")
	}
}

// TestXLSXWriteTablelessDocument verifies the empty-workbook contract: one
// empty sheet (Excel requires at least one) and a loud log.
func TestXLSXWriteTablelessDocument(t *testing.T) {
	src, err := OpenHTMLBytes([]byte(`<html><body><p>prose only</p></body></html>`), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	var logged bool
	var buf bytes.Buffer
	err = src.WriteXLSX(context.Background(), &buf, XLSXOptions{Logf: func(string, ...any) { logged = true }})
	if err != nil {
		t.Fatalf("WriteXLSX: %v", err)
	}
	if !logged {
		t.Errorf("no-tables condition not logged")
	}
	reopened, err := OpenXLSXBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("empty workbook does not reopen: %v", err)
	}
	if got := markdownOf(t, reopened); strings.TrimSpace(got) != "" {
		t.Errorf("empty workbook produced content: %q", got)
	}
}
