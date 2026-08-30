package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTomdCmdHTMLEndToEnd converts an HTML file to Markdown and asserts the output
// carries the expected constructs.
func TestTomdCmdHTMLEndToEnd(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	html := `<!DOCTYPE html><html><body><h1>Title</h1>` +
		`<p>Body with <strong>bold</strong> and <a href="https://x.test">a link</a>.</p>` +
		`<table><tr><th>K</th><th>V</th></tr><tr><td>a</td><td>1</td></tr></table></body></html>`
	if err := os.WriteFile(in, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.md")
	if err := tomdCmd([]string{in, "--out", out}); err != nil {
		t.Fatalf("tomdCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	got := string(data)
	for _, want := range []string{"# Title", "**bold**", "[a link](https://x.test)", "| K | V |", "| a | 1 |"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n%s", want, got)
		}
	}
}

// TestTomdCmdPlain asserts --plain suppresses Markdown syntax.
func TestTomdCmdPlain(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><h1>Hi</h1><p><strong>b</strong></p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.txt")
	if err := tomdCmd([]string{in, "--out", out, "--plain"}); err != nil {
		t.Fatalf("tomdCmd --plain: %v", err)
	}
	data, _ := os.ReadFile(out)
	got := string(data)
	if strings.Contains(got, "#") || strings.Contains(got, "**") {
		t.Errorf("plain output has markdown syntax:\n%s", got)
	}
}

// TestTomdCmdPDFInput asserts a real PDF input is converted to Markdown (structure
// recovered by extraction), carrying the expected text.
func TestTomdCmdPDFInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, in, `<!DOCTYPE html><html><head><style>body{margin:0}</style></head><body>
		<h1>Extract Me</h1>
		<p>A paragraph of body text to recover.</p>
	</body></html>`)
	out := filepath.Join(dir, "out.md")
	if err := tomdCmd([]string{in, "--out", out}); err != nil {
		t.Fatalf("tomdCmd on PDF: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	got := string(data)
	for _, want := range []string{"Extract Me", "body text to recover"} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown from PDF missing %q\n%s", want, got)
		}
	}
}

// TestTomdCmdRejectsUnknownInput asserts an unrecognized extension is rejected.
func TestTomdCmdRejectsUnknownInput(t *testing.T) {
	in := filepath.Join(t.TempDir(), "x.rtf")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tomdCmd([]string{in, "--out", filepath.Join(t.TempDir(), "o.md")}); err == nil {
		t.Fatal("expected tomd to reject a .rtf input")
	}
}

// TestInferCommandTomdPDFInput asserts a .pdf input with a .md output infers tomd.
func TestInferCommandTomdPDFInput(t *testing.T) {
	got, err := inferCommand([]string{"--in", "a.pdf", "--out", "a.md"})
	if err != nil || got != "tomd" {
		t.Errorf("inferCommand(--in a.pdf --out a.md) = %q, %v; want tomd", got, err)
	}
}

// TestTomdCmdHelpExitsClean asserts -h is not an error.
func TestTomdCmdHelpExitsClean(t *testing.T) {
	if err := tomdCmd([]string{"-h"}); err != nil {
		t.Errorf("tomd -h returned error: %v", err)
	}
}

// TestInferCommandMarkdown asserts a .md/.txt output infers the tomd command.
func TestInferCommandMarkdown(t *testing.T) {
	for _, ext := range []string{"a.md", "a.markdown", "a.txt"} {
		got, err := inferCommand([]string{"--in", "a.html", "--out", ext})
		if err != nil || got != "tomd" {
			t.Errorf("inferCommand(--out %s) = %q, %v; want tomd", ext, got, err)
		}
	}
}
