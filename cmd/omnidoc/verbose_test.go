package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brokenPDFSrc has a page whose /Contents points at an object that does not
// exist: it opens and rasterizes as blank, but its structure cannot be
// extracted. Converting it produces an empty Markdown file.
const brokenPDFSrc = `%PDF-1.7
1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj
3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 100 100]/Contents 9 0 R>> endobj
trailer <</Root 1 0 R/Size 4>>
`

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what was
// written. The CLI's -v logger writes there directly, which is the behaviour
// under test.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// TestConvertVerboseReportsDegradation is the CLI half of 0g. A conversion that
// silently drops a page's content used to report nothing at all: an empty output
// file and exit code 0, which reads as success. The conversion still succeeds
// (degrading rather than failing is the project rule), but -v now says what was
// dropped and why.
func TestConvertVerboseReportsDegradation(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(in, []byte(brokenPDFSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.md")

	stderr := captureStderr(t, func() {
		if err := convertCmd([]string{"-v", in, out}); err != nil {
			t.Errorf("convertCmd: %v", err)
		}
	})
	if !strings.Contains(stderr, "content unavailable") {
		t.Errorf("-v did not report the dropped page; stderr was:\n%s", stderr)
	}
}

// TestConvertQuietByDefault checks the flag is off by default: an ordinary run
// must not start emitting diagnostics to stderr, which would break scripts that
// treat any stderr output as a failure.
func TestConvertQuietByDefault(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(in, []byte(brokenPDFSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.md")

	stderr := captureStderr(t, func() {
		if err := convertCmd([]string{in, out}); err != nil {
			t.Errorf("convertCmd: %v", err)
		}
	})
	if stderr != "" {
		t.Errorf("a default run wrote to stderr:\n%s", stderr)
	}
}

// TestConvertVerboseOnGoodDocument guards against -v turning a healthy
// conversion into a wall of alarming text: a well-formed document must not
// report content as unavailable.
func TestConvertVerboseOnGoodDocument(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.pdf")
	writeTestPDF(t, in, convertTestHTML)
	out := filepath.Join(dir, "out.md")

	stderr := captureStderr(t, func() {
		if err := convertCmd([]string{"-v", in, out}); err != nil {
			t.Errorf("convertCmd: %v", err)
		}
	})
	if strings.Contains(stderr, "content unavailable") {
		t.Errorf("a well-formed document reported missing content:\n%s", stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.Contains(string(data), "Convert Title") {
		t.Errorf("-v changed the output; recovered text missing:\n%s", data)
	}
}
