package doctaculous

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	for _, f := range genxlsx.Core {
		if f.Name == name {
			return f.Bytes()
		}
	}
	t.Fatalf("no fixture %q", name)
	return nil
}

func TestXLSXToMarkdownTable(t *testing.T) {
	doc, err := OpenXLSXBytes(fixtureBytes(t, "values"), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	if doc.Format() != FormatXLSX {
		t.Errorf("Format() = %q", doc.Format())
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Name", "Qty", "rich text", "42.5", "inline text", "TRUE", "cached result", "#DIV/0!"} {
		if !strings.Contains(got, want) {
			t.Errorf("xlsx->md missing %q:\n%s", want, got)
		}
	}
	// A single visible sheet gets no sheet-name heading.
	if strings.Contains(got, "# Values") || strings.Contains(got, "## Values") {
		t.Errorf("single-sheet workbook should not emit a sheet heading:\n%s", got)
	}
}

func TestXLSXMergedCellsBecomeSpans(t *testing.T) {
	doc, err := OpenXLSXBytes(fixtureBytes(t, "merged"), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteHTML(context.Background(), &out, HTMLWriteOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `colspan="2"`) {
		t.Errorf("colspan lost:\n%s", got)
	}
	if !strings.Contains(got, `rowspan="2"`) {
		t.Errorf("rowspan lost:\n%s", got)
	}
}

func TestXLSXMultisheet(t *testing.T) {
	doc, err := OpenXLSXBytes(fixtureBytes(t, "multisheet"), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	got := out.String()
	// Visible sheets appear with their names as headings; the hidden sheet is gone.
	for _, want := range []string{"## First", "## Second", "first sheet cell", "second sheet cell"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "hidden cell") || strings.Contains(got, "Secrets") {
		t.Errorf("hidden sheet leaked:\n%s", got)
	}
}

// multisheetMarkdown opens the multisheet fixture with the given options and
// returns its Markdown rendering.
func multisheetMarkdown(t *testing.T, opts ...OpenOption) string {
	t.Helper()
	doc, err := OpenXLSXBytes(fixtureBytes(t, "multisheet"), opts...)
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	return out.String()
}

// WithSheets selecting one sheet renders only that sheet, and — because it is
// then the single rendered sheet — emits no sheet-name heading.
func TestXLSXWithSheetsSingle(t *testing.T) {
	got := multisheetMarkdown(t, WithBundledFonts(), WithSheets("Second"))
	if !strings.Contains(got, "second sheet cell") {
		t.Errorf("selected sheet content missing:\n%s", got)
	}
	if strings.Contains(got, "first sheet cell") || strings.Contains(got, "hidden cell") {
		t.Errorf("unselected sheet leaked:\n%s", got)
	}
	if strings.Contains(got, "## Second") || strings.Contains(got, "## First") {
		t.Errorf("single selected sheet should emit no heading:\n%s", got)
	}
}

// WithSheets selecting several sheets renders exactly those, in the requested
// order (which may differ from file order), each with its heading.
func TestXLSXWithSheetsOrder(t *testing.T) {
	got := multisheetMarkdown(t, WithBundledFonts(), WithSheets("Second", "First"))
	first := strings.Index(got, "## Second")
	second := strings.Index(got, "## First")
	if first < 0 || second < 0 {
		t.Fatalf("both selected headings should appear:\n%s", got)
	}
	if first > second {
		t.Errorf("sheets not in requested order (Second before First):\n%s", got)
	}
	if strings.Contains(got, "hidden cell") {
		t.Errorf("hidden sheet leaked:\n%s", got)
	}
}

// WithSheets can name a hidden sheet explicitly; unlike the default render it is
// then included.
func TestXLSXWithSheetsHiddenExplicit(t *testing.T) {
	got := multisheetMarkdown(t, WithBundledFonts(), WithSheets("Secrets"))
	if !strings.Contains(got, "hidden cell") {
		t.Errorf("explicitly named hidden sheet should render:\n%s", got)
	}
}

// A name no sheet carries fails with ErrSheetNotFound, naming the missing sheet;
// no document is produced.
func TestXLSXWithSheetsNotFound(t *testing.T) {
	_, err := OpenXLSXBytes(fixtureBytes(t, "multisheet"), WithBundledFonts(), WithSheets("First", "Nope"))
	if !errors.Is(err, ErrSheetNotFound) {
		t.Fatalf("want ErrSheetNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "Nope") {
		t.Errorf("error should name the missing sheet %q: %v", "Nope", err)
	}
}

func TestXLSXStyledCells(t *testing.T) {
	doc, err := OpenXLSXBytes(fixtureBytes(t, "styled"), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	var out bytes.Buffer
	if err := doc.WriteMarkdown(context.Background(), &out, MarkdownOptions{}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	// A bold first row IS the table's header (the header-detection design):
	// it renders as the GFM header row, whose implicit bold suppresses the
	// explicit markers.
	if !strings.HasPrefix(out.String(), "| bold header |\n| --- |") {
		t.Errorf("bold first row did not become the header row:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "_italic filled_") {
		t.Errorf("italic cell lost its slant:\n%s", out.String())
	}
}

// TestXLSXToCSV pins the spreadsheet-to-spreadsheet path: sheet values land as
// CSV rows.
func TestXLSXToCSV(t *testing.T) {
	var out bytes.Buffer
	err := Convert(context.Background(), bytes.NewReader(fixtureBytes(t, "values")), &out,
		ConvertOptions{To: FormatCSV, BundledFonts: true}) // From auto-detected via zip magic
	if err != nil {
		t.Fatalf("xlsx->csv: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Name,Qty") || !strings.Contains(got, "rich text,42.5") {
		t.Errorf("xlsx->csv rows wrong:\n%s", got)
	}
}
