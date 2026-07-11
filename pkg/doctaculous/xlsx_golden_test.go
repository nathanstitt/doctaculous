package doctaculous

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// xlsxSpecimen builds the golden workbook: a styled sheet (bold header row,
// dates, percent, fill + alignment) and a merged-range sheet — the visual
// entry for the XLSX frontend.
func xlsxSpecimen() []byte {
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font/><font><b/></font></fonts>
<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>
<fill><patternFill patternType="solid"><fgColor rgb="FFFFF3C4"/></patternFill></fill></fills>
<cellXfs count="5">
<xf numFmtId="0" fontId="0" fillId="0"/>
<xf numFmtId="0" fontId="1" fillId="0"/>
<xf numFmtId="14" fontId="0" fillId="0"/>
<xf numFmtId="9" fontId="0" fillId="0" applyAlignment="1"><alignment horizontal="right"/></xf>
<xf numFmtId="0" fontId="0" fillId="2"/>
</cellXfs></styleSheet>`
	const so = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`
	inventory := so +
		`<row r="1"><c r="A1" s="1" t="inlineStr"><is><t>Item</t></is></c><c r="B1" s="1" t="inlineStr"><is><t>Restocked</t></is></c><c r="C1" s="1" t="inlineStr"><is><t>Margin</t></is></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>Widgets</t></is></c><c r="B2" s="2"><v>45000</v></c><c r="C2" s="3"><v>0.25</v></c></row>` +
		`<row r="3"><c r="A3" s="4" t="inlineStr"><is><t>Gadgets</t></is></c><c r="B3" s="2"><v>45100</v></c><c r="C3" s="3"><v>0.4</v></c></row>` +
		`</sheetData></worksheet>`
	merges := so +
		`<row r="1"><c r="A1" t="inlineStr"><is><t>spans two columns</t></is></c><c r="C1" t="inlineStr"><is><t>side</t></is></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>tall</t></is></c><c r="B2" t="inlineStr"><is><t>m1</t></is></c><c r="C2" t="inlineStr"><is><t>m2</t></is></c></row>` +
		`<row r="3"><c r="B3" t="inlineStr"><is><t>b1</t></is></c><c r="C3" t="inlineStr"><is><t>b2</t></is></c></row>` +
		`</sheetData><mergeCells count="2"><mergeCell ref="A1:B1"/><mergeCell ref="A2:A3"/></mergeCells></worksheet>`
	return genxlsx.New().
		AddSheet("Inventory", inventory).
		AddSheet("Merges", merges).
		SetStyles(styles).
		Bytes()
}

// TestXLSXGolden renders the specimen workbook end to end — the XLSX visual
// entry, mirroring TestMarkdownTextGolden. Run with -update, then eyeball.
func TestXLSXGolden(t *testing.T) {
	doc, err := OpenXLSXBytes(xlsxSpecimen(), WithViewportWidth(460), WithBundledFonts())
	if err != nil {
		t.Fatalf("OpenXLSXBytes: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Errorf("PageCount = %d, want 1", doc.PageCount())
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI, BundledFonts: true})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("rasterized image is %T, want *image.RGBA", img)
	}

	dir := filepath.Join("testdata", "golden")
	path := filepath.Join(dir, "xlsx-specimen.png")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePNG(t, path, got)
		t.Logf("updated %s", path)
		return
	}
	want := readPNG(t, path)
	if want == nil {
		t.Fatalf("missing golden %s; run: go test ./pkg/doctaculous -run TestXLSXGolden -update", path)
	}
	if diff, n := compareImages(want, got); diff {
		t.Errorf("render differs from golden %s: %d pixels beyond tolerance", path, n)
	}
}
