package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// TestTopdfCmdHTMLEndToEnd converts an HTML file to a PDF and asserts the output is
// a valid, non-empty PDF with embedded (searchable) text.
func TestTopdfCmdHTMLEndToEnd(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	html := `<!DOCTYPE html><html><head><style>body{margin:0}</style></head>` +
		`<body><p>Hello from the CLI</p></body></html>`
	if err := os.WriteFile(in, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := topdfCmd([]string{in, "--out", out}); err != nil {
		t.Fatalf("topdfCmd: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("output PDF is empty")
	}
	doc, err := pdf.Parse(data)
	if err != nil {
		t.Fatalf("output is not a valid PDF: %v", err)
	}
	if doc.PageCount() < 1 {
		t.Fatalf("page count = %d; want >= 1", doc.PageCount())
	}
}

// TestTopdfCmdRejectsPDFInput asserts a PDF (or other non-reflow) input is rejected.
func TestTopdfCmdRejectsPDFInput(t *testing.T) {
	in := filepath.Join(t.TempDir(), "x.pdf")
	if err := os.WriteFile(in, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.pdf")
	if err := topdfCmd([]string{in, "--out", out}); err == nil {
		t.Fatal("expected topdf to reject a .pdf input")
	}
}

// TestTopdfCmdRequiresOut asserts --out is required.
func TestTopdfCmdRequiresOut(t *testing.T) {
	in := filepath.Join(t.TempDir(), "in.html")
	if err := os.WriteFile(in, []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := topdfCmd([]string{in}); err == nil {
		t.Fatal("expected an error when --out is omitted")
	}
}

// TestTopdfCmdHelpExitsClean asserts -h is not reported as an error.
func TestTopdfCmdHelpExitsClean(t *testing.T) {
	if err := topdfCmd([]string{"-h"}); err != nil {
		t.Errorf("topdf -h returned error: %v", err)
	}
}

// TestTopdfCmdInFlag asserts the --in flag is an alternative to the positional input.
func TestTopdfCmdInFlag(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte("<p>via --in</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := topdfCmd([]string{"--in", in, "--out", out}); err != nil {
		t.Fatalf("topdfCmd --in: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not written: %v", err)
	}
}

// TestTopdfCmdRejectsInAndPositional asserts giving both --in and a positional arg errors.
func TestTopdfCmdRejectsInAndPositional(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := topdfCmd([]string{in, "--in", in, "--out", out}); err == nil {
		t.Fatal("expected an error when both --in and a positional input are given")
	}
}

// TestRunInfersCommand asserts the top-level run() picks the subcommand from the
// --out extension when no subcommand is named.
func TestRunInfersCommand(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.html")
	if err := os.WriteFile(in, []byte("<p>infer me</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.pdf")
	if err := run([]string{"--in", in, "--out", out}); err != nil {
		t.Fatalf("run (inferred topdf): %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("inferred topdf produced no output: %v", err)
	}
}

// TestInferCommand covers the extension-based command inference.
func TestInferCommand(t *testing.T) {
	cases := []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"--in", "a.html", "--out", "a.pdf"}, "topdf", true},
		{[]string{"--in", "a.pdf", "--out", "a.png"}, "rasterize", true},
		{[]string{"--in", "https://x.com", "--out", "x.pdf"}, "topdf", true},
		{[]string{"--in", "a.docx", "--out", "out"}, "topdf", true},    // fall back to input ext
		{[]string{"--in", "a.pdf", "--out", "out"}, "rasterize", true}, // fall back to input ext
		{[]string{"--in", "mystery", "--out", "mystery"}, "", false},   // inconclusive
	}
	for _, tc := range cases {
		got, err := inferCommand(tc.args)
		if tc.ok && err != nil {
			t.Errorf("inferCommand(%v) errored: %v", tc.args, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("inferCommand(%v) = %q; want an error", tc.args, got)
		}
		if got != tc.want {
			t.Errorf("inferCommand(%v) = %q; want %q", tc.args, got, tc.want)
		}
	}
}
