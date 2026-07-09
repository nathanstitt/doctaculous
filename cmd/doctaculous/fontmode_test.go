package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
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

// TestTopdfBundledFontsFlag: topdf accepts --bundled-fonts and writes a PDF.
func TestTopdfBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body style="font-family:Helvetica"><p>hi</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := topdfCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("topdfCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestTohtmlBundledFontsFlag: tohtml accepts --bundled-fonts and writes output.
func TestTohtmlBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><h1>Title</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.html")
	if err := tohtmlCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("tohtmlCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestTomdBundledFontsFlag: tomd accepts --bundled-fonts.
func TestTomdBundledFontsFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte(`<html><body><p>hello</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.md")
	if err := tomdCmd([]string{in, "--out", out, "--bundled-fonts"}); err != nil {
		t.Fatalf("tomdCmd --bundled-fonts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}
