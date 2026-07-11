package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
)

const convertTestHTML = `<!DOCTYPE html><html><body>
<h1>Convert Title</h1>
<p>Body text for the convert command.</p>
</body></html>`

func writeConvertInput(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConvertCmdHTMLToPDF(t *testing.T) {
	in := writeConvertInput(t, "in.html", []byte(convertTestHTML))
	out := filepath.Join(t.TempDir(), "out.pdf")
	if err := convertCmd([]string{in, out, "--bundled-fonts"}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Errorf("output lacks a PDF header")
	}
}

func TestConvertCmdPDFToMarkdown(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, in, convertTestHTML)
	out := filepath.Join(dir, "out.md")
	if err := convertCmd([]string{in, out}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.Contains(string(data), "Convert Title") {
		t.Errorf("markdown output missing recovered text:\n%s", data)
	}
}

func TestConvertCmdDOCXToHTML(t *testing.T) {
	in := writeConvertInput(t, "in.docx", gendocx.Core[0].Bytes())
	out := filepath.Join(t.TempDir(), "out.html")
	if err := convertCmd([]string{in, out}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "<!DOCTYPE html>") || !strings.Contains(got, "quick brown fox") {
		t.Errorf("html output missing scaffold or content:\n%s", got)
	}
}

// TestConvertCmdToOverridesExtension verifies --to wins over the output
// extension.
func TestConvertCmdToOverridesExtension(t *testing.T) {
	in := writeConvertInput(t, "in.html", []byte(convertTestHTML))
	out := filepath.Join(t.TempDir(), "out.dat")
	if err := convertCmd([]string{in, out, "--to", "md"}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.Contains(string(data), "# Convert Title") {
		t.Errorf("markdown output missing heading:\n%s", data)
	}
}

// TestConvertCmdFromOverride verifies --from opens a mislabeled input as the
// named format.
func TestConvertCmdFromOverride(t *testing.T) {
	// A damaged-header PDF under a nonsense extension: detection cannot place it
	// (magic window misses, no extension signal), so --from is required.
	damaged := append(make([]byte, 1500), gen.TextPDF()...)
	in := writeConvertInput(t, "blob.dat", damaged)
	out := filepath.Join(t.TempDir(), "out.md")
	if err := convertCmd([]string{in, out}); err == nil {
		t.Fatalf("expected detection failure without --from")
	}
	if err := convertCmd([]string{in, out, "--from", "pdf"}); err != nil {
		t.Fatalf("convertCmd --from pdf: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

func TestConvertCmdRejectsSameFormat(t *testing.T) {
	in := writeConvertInput(t, "in.html", []byte(convertTestHTML))
	err := convertCmd([]string{in, filepath.Join(t.TempDir(), "out.html")})
	if err == nil || !strings.Contains(err.Error(), "same format") {
		t.Errorf("html->html: want a same-format error, got %v", err)
	}
}

func TestConvertCmdMultiPageImages(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, gen.MultiPagePDF(3), 0o600); err != nil {
		t.Fatal(err)
	}
	// Multi-page without %d is a clean error.
	err := convertCmd([]string{in, filepath.Join(dir, "out.png"), "--pages", "1-2"})
	if err == nil || !strings.Contains(err.Error(), "%d") {
		t.Errorf("want a %%d-placeholder error, got %v", err)
	}
	// With %d, one file per page appears.
	if err := convertCmd([]string{in, filepath.Join(dir, "page-%d.png"), "--pages", "1-2", "--dpi", "36"}); err != nil {
		t.Fatalf("convertCmd: %v", err)
	}
	for _, n := range []string{"page-1.png", "page-2.png"} {
		if fi, err := os.Stat(filepath.Join(dir, n)); err != nil || fi.Size() == 0 {
			t.Errorf("%s missing or empty (err=%v)", n, err)
		}
	}
}

func TestConvertCmdUnknownOutputExtension(t *testing.T) {
	in := writeConvertInput(t, "in.html", []byte(convertTestHTML))
	err := convertCmd([]string{in, filepath.Join(t.TempDir(), "out.xyz")})
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("want an error pointing at --to, got %v", err)
	}
	// Stdout output requires --to.
	err = convertCmd([]string{in, "-"})
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("stdout without --to: want an error pointing at --to, got %v", err)
	}
}

func TestRunDispatchesConvert(t *testing.T) {
	in := writeConvertInput(t, "in.html", []byte(convertTestHTML))
	out := filepath.Join(t.TempDir(), "out.md")
	if err := run([]string{"convert", in, out}); err != nil {
		t.Fatalf("run(convert ...): %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

func TestConvertCmdHelpExitsClean(t *testing.T) {
	if err := convertCmd([]string{"-h"}); err != nil {
		t.Errorf("-h should exit clean, got %v", err)
	}
}

// TestRasterizeCmdSniffsExtensionless pins the opener-unification behavior
// change: rasterize no longer assumes an unknown extension is a PDF — it
// detects the format from content, so an extension-less real PDF still works
// and garbage gets a format error instead of a PDF parse error.
func TestRasterizeCmdSniffsExtensionless(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "document") // no extension
	if err := os.WriteFile(in, gen.VectorPDF(), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.png")
	if err := rasterizeCmd([]string{in, "--out", out, "--dpi", "36"}); err != nil {
		t.Fatalf("rasterizeCmd on extension-less PDF: %v", err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Errorf("output missing or empty (err=%v)", err)
	}

	junk := filepath.Join(dir, "junk.rtf")
	if err := os.WriteFile(junk, []byte("not a document"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := rasterizeCmd([]string{junk, "--out", out})
	if err == nil || !strings.Contains(err.Error(), "format") {
		t.Errorf("garbage input: want a format-detection error, got %v", err)
	}
}
