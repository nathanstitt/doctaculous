package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/doctaculous"
)

func TestTodocxCmdHTMLInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><h1>Doc</h1><p>Body text.</p></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.docx")
	if err := todocxCmd([]string{in, "--out", out}); err != nil {
		t.Fatalf("todocxCmd: %v", err)
	}
	// The output must reopen through the library and carry the content.
	doc, err := doctaculous.OpenDOCX(out)
	if err != nil {
		t.Fatalf("produced docx does not open: %v", err)
	}
	var sb strings.Builder
	if err := doc.WriteText(t.Context(), &sb, doctaculous.MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(sb.String(), "Body text.") {
		t.Errorf("content lost:\n%s", sb.String())
	}
}

func TestTodocxCmdPDFInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, in, `<html><body><h1>Extracted</h1><p>Recovered body.</p></body></html>`)
	out := filepath.Join(dir, "out.docx")
	if err := todocxCmd([]string{in, "--out", out}); err != nil {
		t.Fatalf("todocxCmd: %v", err)
	}
	doc, err := doctaculous.OpenDOCX(out)
	if err != nil {
		t.Fatalf("produced docx does not open: %v", err)
	}
	var sb strings.Builder
	if err := doc.WriteText(t.Context(), &sb, doctaculous.MarkdownOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(sb.String(), "Recovered body") {
		t.Errorf("extracted content lost:\n%s", sb.String())
	}
}

func TestTodocxCmdRejectsDOCXInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><p>x</p></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	mid := filepath.Join(dir, "mid.docx")
	if err := todocxCmd([]string{in, "--out", mid}); err != nil {
		t.Fatal(err)
	}
	err := todocxCmd([]string{mid, "--out", filepath.Join(dir, "again.docx")})
	if err == nil || !strings.Contains(err.Error(), "same format") {
		t.Errorf("docx->docx: want a same-format error, got %v", err)
	}
}

func TestConvertCmdDocxOutput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("# MD to Word\n\nSome **bold** text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.docx")
	if err := convertCmd([]string{in, out}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	doc, err := doctaculous.OpenDOCX(out)
	if err != nil {
		t.Fatalf("produced docx does not open: %v", err)
	}
	var sb strings.Builder
	if err := doc.WriteMarkdown(t.Context(), &sb, doctaculous.MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	if !strings.Contains(sb.String(), "# MD to Word") || !strings.Contains(sb.String(), "**bold**") {
		t.Errorf("md->docx->md lost content:\n%s", sb.String())
	}
}

func TestTodocxCmdHelpExitsClean(t *testing.T) {
	if err := todocxCmd([]string{"-h"}); err != nil {
		t.Errorf("-h should exit clean, got %v", err)
	}
}
