package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// writeFixture writes a generated PDF to a temp file and returns its path.
func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRasterizeCmdEndToEnd(t *testing.T) {
	in := writeFixture(t, gen.VectorPDF())
	out := filepath.Join(t.TempDir(), "out.png")
	if err := rasterizeCmd([]string{in, "--out", out, "--dpi", "72"}); err != nil {
		t.Fatalf("rasterizeCmd: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestRasterizeCmdMultiPage(t *testing.T) {
	in := writeFixture(t, gen.MultiPagePDF(3))
	dir := t.TempDir()
	pattern := filepath.Join(dir, "page-%d.png")
	if err := rasterizeCmd([]string{in, "--pages", "1-3", "--out", pattern, "--dpi", "72"}); err != nil {
		t.Fatalf("rasterizeCmd: %v", err)
	}
	for _, name := range []string{"page-1.png", "page-2.png", "page-3.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestRasterizeCmdMultiPageRequiresPattern(t *testing.T) {
	in := writeFixture(t, gen.MultiPagePDF(2))
	out := filepath.Join(t.TempDir(), "out.png") // no %d
	if err := rasterizeCmd([]string{in, "--pages", "1-2", "--out", out}); err == nil {
		t.Fatal("expected error when multi-page output lacks a numbering placeholder")
	}
}

func TestRasterizeCmdHelpExitsClean(t *testing.T) {
	// -h must not be reported as an error (it should exit 0 at the main level).
	if err := rasterizeCmd([]string{"-h"}); err != nil {
		t.Errorf("rasterize -h returned error: %v", err)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestRunVersion(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Errorf("version: %v", err)
	}
}
