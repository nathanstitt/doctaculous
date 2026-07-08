package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

// writeTestPDF renders html to a PDF on disk at path via ConvertHTMLToPDF. It is the
// shared helper the tohtml/tomd CLI tests use to produce a real .pdf input.
func writeTestPDF(t *testing.T, path, html string) {
	t.Helper()
	var buf bytes.Buffer
	if err := doctaculous.ConvertHTMLToPDF(context.Background(), strings.NewReader(html), &buf, doctaculous.PDFOptions{}); err != nil {
		t.Fatalf("ConvertHTMLToPDF: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("produced empty PDF")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tableHTML is a ruled table with collapsed borders so the PDF lattice detector finds it
// on the round trip.
const tableHTML = `<!DOCTYPE html><html><head><style>body{margin:0}
	table{border-collapse:collapse} td,th{border:1px solid black;padding:6px}</style></head><body>
	<h1>Report</h1>
	<p>An introductory paragraph of body text.</p>
	<table>
		<tr><th>A</th><th>B</th></tr>
		<tr><td>1</td><td>2</td></tr>
	</table>
</body></html>`

// TestTohtmlCmdPDFEndToEnd converts a generated PDF to HTML and asserts the output carries
// the document scaffold, the recovered table, and the cell text.
func TestTohtmlCmdPDFEndToEnd(t *testing.T) {
	dir := t.TempDir()
	inPDF := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, inPDF, tableHTML)

	outHTML := filepath.Join(dir, "out.html")
	if err := tohtmlCmd([]string{inPDF, "--out", outHTML}); err != nil {
		t.Fatalf("tohtmlCmd: %v", err)
	}
	data, err := os.ReadFile(outHTML)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "<!DOCTYPE html>") {
		t.Errorf("output missing doctype\n%s", got)
	}
	if !strings.Contains(got, "<table>") {
		t.Errorf("output missing <table>\n%s", got)
	}
	if !strings.Contains(got, "<td>") && !strings.Contains(got, "<th>") {
		t.Errorf("output missing table cells\n%s", got)
	}
	for _, want := range []string{"A", "B", "1", "2"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing cell text %q\n%s", want, got)
		}
	}
}

// TestTohtmlCmdFragment asserts --fragment omits the <!DOCTYPE html> wrapper.
func TestTohtmlCmdFragment(t *testing.T) {
	dir := t.TempDir()
	inPDF := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, inPDF, `<html><body><p>fragment body text</p></body></html>`)

	outHTML := filepath.Join(dir, "out.html")
	if err := tohtmlCmd([]string{inPDF, "--out", outHTML, "--fragment"}); err != nil {
		t.Fatalf("tohtmlCmd --fragment: %v", err)
	}
	data, _ := os.ReadFile(outHTML)
	got := string(data)
	if strings.Contains(got, "<!DOCTYPE html>") {
		t.Errorf("fragment output should not carry a doctype:\n%s", got)
	}
	if !strings.Contains(got, "fragment body text") {
		t.Errorf("fragment output missing body text:\n%s", got)
	}
}

// TestTohtmlCmdHelpExitsClean asserts -h is not an error.
func TestTohtmlCmdHelpExitsClean(t *testing.T) {
	if err := tohtmlCmd([]string{"-h"}); err != nil {
		t.Errorf("tohtml -h returned error: %v", err)
	}
}

// TestInferCommandHTML asserts a .html/.htm output infers the tohtml command.
func TestInferCommandHTML(t *testing.T) {
	for _, out := range []string{"a.html", "a.htm"} {
		got, err := inferCommand([]string{"--in", "a.pdf", "--out", out})
		if err != nil || got != "tohtml" {
			t.Errorf("inferCommand(--out %s) = %q, %v; want tohtml", out, got, err)
		}
	}
}
