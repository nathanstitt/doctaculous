package omnidoc

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/nathanstitt/omnidoc/pkg/xlsx"
)

// TestErrSheetNotFoundInteroperates covers the duplicate-sentinel bug.
//
// Until this change these were two distinct values whose messages differed by a
// single colon -- "xlsx sheet not found" here, "xlsx: sheet not found" in
// pkg/xlsx -- so errors.Is between them returned false in BOTH directions. A
// caller using both packages (which is the normal case: open a workbook with
// pkg/xlsx, render it with this one) would write the wrong check, and it would
// compile and pass review while never matching.
func TestErrSheetNotFoundInteroperates(t *testing.T) {
	if !errors.Is(ErrSheetNotFound, xlsx.ErrSheetNotFound) {
		t.Error("errors.Is(omnidoc.ErrSheetNotFound, xlsx.ErrSheetNotFound) = false")
	}
	// The reverse direction holds by identity: aliasing makes these the same
	// value, which is stronger than a one-way errors.Is match.
	if ErrSheetNotFound != xlsx.ErrSheetNotFound { //nolint:err113 // identity IS the property under test
		t.Error("omnidoc.ErrSheetNotFound is not the same value as xlsx.ErrSheetNotFound")
	}
	// A real error from each package must match the other package's sentinel:
	// that is what a caller actually writes.
	_, err := OpenXLSXBytes(fixtureBytes(t, "multisheet"), WithBundledFonts(), WithSheets("Nope"))
	if err == nil {
		t.Fatal("opening with an unknown sheet name succeeded")
	}
	if !errors.Is(err, xlsx.ErrSheetNotFound) {
		t.Errorf("an omnidoc error does not match xlsx.ErrSheetNotFound: %v", err)
	}
	if !errors.Is(err, ErrSheetNotFound) {
		t.Errorf("an omnidoc error does not match omnidoc.ErrSheetNotFound: %v", err)
	}
}

// TestErrNoStructureIsMatchable covers the sentinel for "this document carries
// no box tree to walk".
//
// It is a branchable condition -- the natural response is to fall back to
// rasterizing -- but before this change the nine writers returned bare
// fmt.Errorf, and did not even agree on the wording: seven said "document has no
// convertible structure" and two said "document is not a reflow document". A
// caller had to string-match, against two different strings.
func TestErrNoStructureIsMatchable(t *testing.T) {
	// An SVG document is the structureless case: it lays out to pages but keeps
	// no box tree, so cssboxRoot returns nil even though the renderer satisfies
	// reflowTree. Before this change the writers walked that nil tree, wrote
	// nothing, and returned nil -- an empty file reported as success.
	doc, err := OpenBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg" ` +
		`width="10" height="10"><rect width="5" height="5"/></svg>`))
	if err != nil {
		t.Fatalf("OpenBytes(svg): %v", err)
	}

	writers := []struct {
		name string
		call func(io *bytes.Buffer) error
	}{
		{"WriteMarkdown", func(b *bytes.Buffer) error {
			return doc.WriteMarkdown(context.Background(), b, MarkdownOptions{})
		}},
		{"WriteHTML", func(b *bytes.Buffer) error {
			return doc.WriteHTML(context.Background(), b, HTMLWriteOptions{})
		}},
		{"WriteDOCX", func(b *bytes.Buffer) error {
			return doc.WriteDOCX(context.Background(), b, DOCXOptions{})
		}},
		{"WriteCSV", func(b *bytes.Buffer) error {
			return doc.WriteCSV(context.Background(), b, CSVOptions{})
		}},
	}
	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := w.call(&buf)
			if err == nil {
				t.Fatalf("%s on a structureless document succeeded", w.name)
			}
			if !errors.Is(err, ErrNoStructure) {
				t.Errorf("%s error does not match ErrNoStructure: %v", w.name, err)
			}
		})
	}
}

// TestErrPageOutOfRangeIsMatchable covers the other sentinel: the one failure a
// caller can fix by asking for a different page.
func TestErrPageOutOfRangeIsMatchable(t *testing.T) {
	doc, err := OpenHTMLBytes([]byte("<p>one page</p>"))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}
	for _, idx := range []int{-1, doc.PageCount(), doc.PageCount() + 100} {
		_, err := doc.RasterizePage(context.Background(), idx, RasterOptions{})
		if err == nil {
			t.Errorf("RasterizePage(%d) succeeded on a %d-page document", idx, doc.PageCount())
			continue
		}
		if !errors.Is(err, ErrPageOutOfRange) {
			t.Errorf("RasterizePage(%d) error does not match ErrPageOutOfRange: %v", idx, err)
		}
	}
	// A valid index must still work: the sentinel must not swallow the happy path.
	if _, err := doc.RasterizePage(context.Background(), 0, RasterOptions{}); err != nil {
		t.Errorf("RasterizePage(0): %v", err)
	}
}
