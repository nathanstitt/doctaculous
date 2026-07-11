package doctaculous

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// mdOfCSV converts CSV bytes to Markdown.
func mdOfCSV(t *testing.T, data string) string {
	t.Helper()
	doc, err := OpenCSVBytes([]byte(data), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenCSVBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	return out.String()
}

func TestCSVToMarkdownTable(t *testing.T) {
	got := mdOfCSV(t, "Name,Qty\nWidgets,5\nGadgets,7\n")
	want := "| Name | Qty |\n| --- | --- |\n| Widgets | 5 |\n| Gadgets | 7 |\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestCSVParsingQuirks(t *testing.T) {
	// Quoted commas and embedded newlines, ragged rows (padded), a UTF-8 BOM,
	// CRLF line endings, and markup that must stay literal.
	src := "\uFEFFa,b,c\r\n\"x, y\",\"two\nlines\"\r\n<b>lit</b>\r\n"
	got := mdOfCSV(t, src)
	for _, want := range []string{
		"| a | b | c |",
		"x, y",       // quoted comma survives inside one cell
		"two lines",  // embedded newline collapses within the cell
		"<b>lit</b>", // markup stays literal (escaped into the table)
	} {
		if !strings.Contains(got, boxEscape(want)) && !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// The ragged rows padded to 3 columns: every row has 3 cells.
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.Count(line, "|") != 4 {
			t.Errorf("row not padded to 3 columns: %q", line)
		}
	}
}

// boxEscape mirrors the markdown writer's text escaping for assertion purposes.
func boxEscape(s string) string {
	r := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`)
	return r.Replace(s)
}

func TestTSVRoundTripsThroughCSV(t *testing.T) {
	ctx := context.Background()
	tsv := "Name\tQty\nWidgets\t5\n"

	// tsv -> csv
	var asCSV bytes.Buffer
	err := Convert(ctx, strings.NewReader(tsv), &asCSV, ConvertOptions{From: FormatTSV, To: FormatCSV, BundledFonts: true})
	if err != nil {
		t.Fatalf("tsv->csv: %v", err)
	}
	if asCSV.String() != "Name,Qty\nWidgets,5\n" {
		t.Errorf("tsv->csv = %q", asCSV.String())
	}
	// csv -> tsv closes the loop byte-identically.
	var back bytes.Buffer
	err = Convert(ctx, bytes.NewReader(asCSV.Bytes()), &back, ConvertOptions{From: FormatCSV, To: FormatTSV, BundledFonts: true})
	if err != nil {
		t.Fatalf("csv->tsv: %v", err)
	}
	if back.String() != tsv {
		t.Errorf("csv->tsv = %q, want %q", back.String(), tsv)
	}
}

func TestCSVEmptyInput(t *testing.T) {
	doc, err := OpenCSVBytes(nil, WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenCSVBytes(empty): %v", err)
	}
	if doc.Format() != FormatCSV {
		t.Errorf("Format() = %q", doc.Format())
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("empty csv produced output: %q", out.String())
	}
}

// TestPDFToCSVTableExtraction pins the standout path: a ruled table inside a
// PDF is recovered by the lattice detector and lands as CSV.
func TestPDFToCSVTableExtraction(t *testing.T) {
	const tableHTML = `<!DOCTYPE html><html><head><style>body{margin:0}
	table{border-collapse:collapse} td,th{border:1px solid black;padding:6px}</style></head><body>
	<h1>Report</h1>
	<p>An introductory paragraph.</p>
	<table>
		<tr><th>Item</th><th>Qty</th></tr>
		<tr><td>Widgets</td><td>5</td></tr>
	</table>
	</body></html>`
	var pdf bytes.Buffer
	err := convertHTMLToPDF(context.Background(), strings.NewReader(tableHTML), &pdf, PDFOptions{BundledFonts: true})
	if err != nil {
		t.Fatalf("build pdf: %v", err)
	}

	var logged []string
	var out bytes.Buffer
	err = Convert(context.Background(), bytes.NewReader(pdf.Bytes()), &out, ConvertOptions{
		From: FormatPDF, To: FormatCSV,
		Logf: func(f string, a ...any) { logged = append(logged, f) },
	})
	if err != nil {
		t.Fatalf("pdf->csv: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Item,Qty", "Widgets,5"} {
		if !strings.Contains(got, want) {
			t.Errorf("pdf->csv missing %q:\n%s", want, got)
		}
	}
	// The heading + paragraph were dropped — and that must have been logged.
	if strings.Contains(got, "Report") {
		t.Errorf("prose leaked into csv:\n%s", got)
	}
	if len(logged) == 0 {
		t.Errorf("dropped prose was not logged")
	}
}
