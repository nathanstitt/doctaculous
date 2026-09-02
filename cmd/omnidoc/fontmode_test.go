package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/omnidoc/testdata/gen"
)

// TestRasterizeBundledFontsFlag: rasterize accepts --bundled-fonts and writes a PNG.
func TestRasterizeBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, gen.WeightedFontsPDF(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.png")
	if err := rasterizeCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("rasterizeCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestConvertToPDFBundledFontsFlag: convert to PDF accepts --bundled-fonts and writes a PDF.
func TestConvertToPDFBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body style="font-family:Helvetica"><p>hi</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := convertCmd([]string{in, out, "--bundled-fonts"}); err != nil {
		t.Fatalf("convertCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestConvertToHTMLBundledFontsFlag: convert to HTML accepts --bundled-fonts and writes output.
func TestConvertToHTMLBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("# Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.html")
	if err := convertCmd([]string{in, out, "--bundled-fonts"}); err != nil {
		t.Fatalf("convertCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestConvertToMarkdownBundledFontsFlag: convert to Markdown accepts --bundled-fonts.
func TestConvertToMarkdownBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><p>hello</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.md")
	if err := convertCmd([]string{in, out, "--bundled-fonts"}); err != nil {
		t.Fatalf("convertCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}
