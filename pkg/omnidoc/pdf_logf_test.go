package omnidoc

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// brokenPDF has one page whose /Contents points at an object that does not
// exist. It parses (so Open succeeds) and rasterizes as a blank page, but its
// structure cannot be extracted -- the shape that used to convert to an empty
// file with a nil error and no diagnostic whatsoever.
const brokenPDF = `%PDF-1.7
1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj
3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 100 100]/Contents 9 0 R>> endobj
trailer <</Root 1 0 R/Size 4>>
`

// TestPDFExtractionReportsThroughLogf is the regression guard for 0g: every
// PDF-to-anything conversion routes through cssboxRoot, which passed nil for the
// extractor's logger and discarded the error it returned. The result was an
// empty output file, a nil error, and silence -- indistinguishable from a
// document that genuinely had no text.
//
// The conversion still SUCCEEDS (degrading rather than failing is the project
// rule); what changed is that it now says what it dropped.
func TestPDFExtractionReportsThroughLogf(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	doc, err := OpenBytes([]byte(brokenPDF), WithLogf(func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(f, a...))
	}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}

	var buf bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &buf, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) == 0 {
		t.Fatal("no diagnostic was logged: an unreadable page converted silently")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "content unavailable") {
		t.Errorf("logs do not explain what was dropped:\n%s", joined)
	}
}

// TestPDFExtractionWithoutLogfStillWorks checks the nil-logger path: a caller
// that supplies no logger must still get a working (if empty) conversion rather
// than a panic on the nil func.
func TestPDFExtractionWithoutLogfStillWorks(t *testing.T) {
	doc, err := OpenBytes([]byte(brokenPDF))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var buf bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &buf, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
}

// TestPDFGoodDocumentLogsNothingAlarming guards against the logger becoming
// noise: a well-formed document must not report content as unavailable.
func TestPDFGoodDocumentLogsNothingAlarming(t *testing.T) {
	// A minimal but complete one-page PDF.
	good := `%PDF-1.7
1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj
3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Contents 4 0 R>> endobj
4 0 obj <</Length 12>> stream
0 0 m 5 5 l
endstream endobj
trailer <</Root 1 0 R/Size 5>>
`
	var mu sync.Mutex
	var logs []string
	doc, err := OpenBytes([]byte(good), WithLogf(func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(f, a...))
	}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	var buf bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &buf, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range logs {
		if strings.Contains(l, "content unavailable") || strings.Contains(l, "extraction failed") {
			t.Errorf("a well-formed document reported a failure: %s", l)
		}
	}
}
