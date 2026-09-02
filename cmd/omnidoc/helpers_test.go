package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/omnidoc"
)

// writeTestPDF renders html to a PDF on disk at path via the generic Convert,
// giving the CLI tests a real .pdf input. It pins bundled-font mode so the
// generated PDF embeds the bundled substitutes (stable ToUnicode), making the
// round-tripped text reliably extractable regardless of the host's installed
// fonts.
func writeTestPDF(t *testing.T, path, html string) {
	t.Helper()
	var buf bytes.Buffer
	if err := omnidoc.Convert(context.Background(), strings.NewReader(html), &buf, omnidoc.ConvertOptions{
		From:         omnidoc.FormatHTML,
		To:           omnidoc.FormatPDF,
		BundledFonts: true,
	}); err != nil {
		t.Fatalf("Convert html to pdf: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("produced empty PDF")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
