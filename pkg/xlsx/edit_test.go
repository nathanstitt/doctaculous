package xlsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// editFixture is a package with content the editor does NOT model — prefixed
// extension elements, a theme part, unknown sheet XML — plus ordinary data.
func editFixture() []byte {
	return genxlsx.New().
		AddSheet("Data", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac">
 <sheetData>
  <row r="1" x14ac:dyDescent="0.3"><c r="A1" t="inlineStr"><is><t>name</t></is></c><c r="B1"><v>10</v></c></row>
  <row r="2"><c r="A2" t="inlineStr"><is><t>x</t></is></c><c r="B2" s="0"><v>20</v></c></row>
 </sheetData>
 <extLst><ext uri="{KEEP}"><x14:sparklineGroups xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main"><x14:opaque attr="yes">payload</x14:opaque></x14:sparklineGroups></ext></extLst>
</worksheet>`).
		AddSheet("Other", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData></worksheet>`).
		SetStyles(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font/></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellXfs></styleSheet>`).
		Bytes()
}

// zipParts explodes a package into ordered names and per-part bytes.
func zipParts(t *testing.T, data []byte) ([]string, map[string][]byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	var names []string
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, f.Name)
		parts[f.Name] = b
	}
	return names, parts
}

// TestEditNoOpByteIdentical pins the strongest preservation guarantee: Edit +
// Save with no mutations — including after arbitrary READS — reproduces every
// part byte-identically, in the original order.
func TestEditNoOpByteIdentical(t *testing.T) {
	src := editFixture()
	f, err := Edit(src)
	if err != nil {
		t.Fatal(err)
	}
	// Reads must not dirty anything.
	_ = f.SheetNames()
	_ = f.Date1904()
	sh, err := f.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	_ = sh.Merges()
	_, _ = sh.Frozen()
	sh.Cells(func(int, int, CellData) bool { return true })
	_ = sh.Cell(1, 2)

	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	wantNames, wantParts := zipParts(t, src)
	gotNames, gotParts := zipParts(t, out)
	if len(wantNames) != len(gotNames) {
		t.Fatalf("part count changed: %v -> %v", wantNames, gotNames)
	}
	for i := range wantNames {
		if wantNames[i] != gotNames[i] {
			t.Errorf("part order changed at %d: %s -> %s", i, wantNames[i], gotNames[i])
		}
	}
	for name, want := range wantParts {
		if !bytes.Equal(want, gotParts[name]) {
			t.Errorf("%s changed on a no-op save", name)
		}
	}
}

// TestEditSurgicalPreservation pins the dirty-tracking contract: editing one
// cell leaves every OTHER part byte-identical, and the unmodeled extension
// content inside the edited part survives verbatim.
func TestEditSurgicalPreservation(t *testing.T) {
	src := editFixture()
	f, err := Edit(src)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetNumber(2, 2, 99); err != nil {
		t.Fatal(err)
	}
	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}

	_, wantParts := zipParts(t, src)
	_, gotParts := zipParts(t, out)
	for name, want := range wantParts {
		if name == "xl/worksheets/sheet1.xml" {
			continue // the edited part
		}
		if !bytes.Equal(want, gotParts[name]) {
			t.Errorf("untouched part %s changed", name)
		}
	}
	edited := string(gotParts["xl/worksheets/sheet1.xml"])
	for _, want := range []string{
		"<x14:opaque attr=\"yes\">payload</x14:opaque>", // unmodeled extension XML survives
		`x14ac:dyDescent="0.3"`,                         // unknown row attribute survives
		"<v>99</v>",                                     // the edit landed
	} {
		if !bytes.Contains([]byte(edited), []byte(want)) {
			t.Errorf("edited part missing %q:\n%s", want, edited)
		}
	}

	// The edit reads back through the enriched reader.
	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := wb.Sheets[0].Cells[1][1].Value; got.Kind != KindNumber || got.F != 99 {
		t.Errorf("edited cell = %+v", got)
	}
}

// TestEditTypedCellWrites round-trips every typed write through the reader.
func TestEditTypedCellWrites(t *testing.T) {
	f := New()
	sh, err := f.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetString(1, 1, " padded "); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetNumber(1, 2, 3.14); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetBool(1, 3, true); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	if err := sh.SetDate(2, 1, date); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 7, 10, 14, 30, 0, 0, time.UTC)
	if err := sh.SetDate(2, 2, stamp); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetFormula(3, 1, "B1*2", Value{Kind: KindNumber, F: 6.28}); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetFormula(3, 2, `CONCAT("a","b")`, Value{Kind: KindString, S: "ab"}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}

	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	cells := wb.Sheets[0].Cells
	if v := cells[0][0].Value; v.Kind != KindString || v.S != " padded " {
		t.Errorf("string = %+v", v)
	}
	if v := cells[0][1].Value; v.Kind != KindNumber || v.F != 3.14 {
		t.Errorf("number = %+v", v)
	}
	if v := cells[0][2].Value; v.Kind != KindBool || !v.B {
		t.Errorf("bool = %+v", v)
	}
	if v := cells[1][0].Value; v.Kind != KindDate || !v.T.Equal(date) {
		t.Errorf("date = %+v (want %v)", v, date)
	}
	if v := cells[1][1].Value; v.Kind != KindDate || !v.T.Equal(stamp) {
		t.Errorf("datetime = %+v (want %v)", v, stamp)
	}
	if c := cells[2][0]; c.Formula != "B1*2" || c.Value.F != 6.28 {
		t.Errorf("formula cell = %+v", c)
	}
	if c := cells[2][1]; c.Formula != `CONCAT("a","b")` || c.Value.S != "ab" {
		t.Errorf("string formula cell = %+v", c)
	}

	// ClearCell drops content but keeps the (date) style.
	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	sh2, err := f2.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	before := sh2.Cell(2, 1).StyleID
	sh2.ClearCell(2, 1)
	if got := sh2.Cell(2, 1); got.Value.Kind != KindEmpty || got.Formula != "" || got.StyleID != before {
		t.Errorf("cleared cell = %+v (style was %d)", got, before)
	}
}

// TestEditSheetOps covers add/delete/move/rename/visibility/tab color.
func TestEditSheetOps(t *testing.T) {
	f, err := Edit(editFixture())
	if err != nil {
		t.Fatal(err)
	}
	added, err := f.AddSheet("Summary")
	if err != nil {
		t.Fatal(err)
	}
	if err := added.SetString(1, 1, "hello"); err != nil {
		t.Fatal(err)
	}
	added.SetTabColor("00AA55")
	if err := f.MoveSheet("Summary", 0); err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("Other")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetVisibility(SheetHidden); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetName("Archive"); err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteSheet("Data"); err != nil {
		t.Fatal(err)
	}

	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(wb.Sheets) != 2 || wb.Sheets[0].Name != "Summary" || wb.Sheets[1].Name != "Archive" {
		names := []string{}
		for _, s := range wb.Sheets {
			names = append(names, s.Name)
		}
		t.Fatalf("sheets = %v, want [Summary Archive]", names)
	}
	if wb.Sheets[0].TabColorRGB != "00AA55" {
		t.Errorf("tab color = %q", wb.Sheets[0].TabColorRGB)
	}
	if wb.Sheets[0].Cells[0][0].Text != "hello" {
		t.Errorf("added sheet cell = %q", wb.Sheets[0].Cells[0][0].Text)
	}
	if wb.Sheets[1].Visibility != SheetHidden {
		t.Errorf("visibility = %v", wb.Sheets[1].Visibility)
	}

	// Guards: the last visible sheet can be neither hidden nor deleted.
	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := f2.DeleteSheet("Summary"); !errors.Is(err, ErrLastVisibleSheet) {
		t.Errorf("deleting the last visible sheet: %v", err)
	}
	last, err := f2.Sheet("Summary")
	if err != nil {
		t.Fatal(err)
	}
	if err := last.SetVisibility(SheetHidden); !errors.Is(err, ErrLastVisibleSheet) {
		t.Errorf("hiding the last visible sheet: %v", err)
	}
	if _, err := f2.Sheet("Nope"); !errors.Is(err, ErrSheetNotFound) {
		t.Errorf("missing sheet: %v", err)
	}
}

// TestEditGeometry covers merges, panes, dimensions, and row/col sizing.
func TestEditGeometry(t *testing.T) {
	f, err := Edit(editFixture())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	sh.SetMerges([]Range{{StartRow: 1, StartCol: 1, EndRow: 1, EndCol: 2}})
	sh.SetFrozen(1, 0)
	sh.SetDimension(Range{StartRow: 1, StartCol: 1, EndRow: 4, EndCol: 3})
	sh.SetRowHeight(1, 30)
	sh.SetColWidth(2, 18.5)

	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	s := wb.Sheets[0]
	if len(s.Merges) != 1 || s.Merges[0] != (Merge{Row: 0, Col: 0, RowSpan: 1, ColSpan: 2}) {
		t.Errorf("merges = %+v", s.Merges)
	}
	if s.FrozenRows != 1 || s.FrozenCols != 0 {
		t.Errorf("frozen = %d/%d", s.FrozenRows, s.FrozenCols)
	}
	if s.RowHeights[0] != 30 {
		t.Errorf("row height = %+v", s.RowHeights)
	}
	if s.ColWidths[1] != 18.5 {
		t.Errorf("col width = %+v", s.ColWidths)
	}

	// Editor-side reads agree; clearing restores the unset state.
	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	sh2, err := f2.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	if got := sh2.Merges(); len(got) != 1 || got[0].String() != "A1:B1" {
		t.Errorf("Merges() = %+v", got)
	}
	if r, c := sh2.Frozen(); r != 1 || c != 0 {
		t.Errorf("Frozen() = %d/%d", r, c)
	}
	if h, ok := sh2.RowHeight(1); !ok || h != 30 {
		t.Errorf("RowHeight = %g/%v", h, ok)
	}
	if w, ok := sh2.ColWidth(2); !ok || w != 18.5 {
		t.Errorf("ColWidth = %g/%v", w, ok)
	}
	if d, ok := sh2.Dimension(); !ok || d.String() != "A1:C4" {
		t.Errorf("Dimension = %+v/%v", d, ok)
	}
	sh2.SetMerges(nil)
	sh2.SetFrozen(0, 0)
	sh2.ClearRowHeight(1)
	sh2.ClearColWidth(2)
	out2, err := f2.Save()
	if err != nil {
		t.Fatal(err)
	}
	wb2, err := OpenBytes(out2)
	if err != nil {
		t.Fatal(err)
	}
	s2 := wb2.Sheets[0]
	if len(s2.Merges) != 0 || s2.FrozenRows != 0 || len(s2.RowHeights) != 0 || len(s2.ColWidths) != 0 {
		t.Errorf("cleared state = %+v", s2)
	}
}

// TestEditColRangeSplit pins that setting one column's width inside an
// existing <col min..max> range splits the range, keeping neighbors.
func TestEditColRangeSplit(t *testing.T) {
	src := genxlsx.New().AddSheet("S", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <cols><col min="1" max="5" width="20" customWidth="1"/></cols>
 <sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData>
</worksheet>`).Bytes()
	f, err := Edit(src)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	sh.SetColWidth(3, 5)
	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	w := wb.Sheets[0].ColWidths
	want := map[int]float64{0: 20, 1: 20, 2: 5, 3: 20, 4: 20}
	for col, wantW := range want {
		if w[col] != wantW {
			t.Errorf("col %d width = %g, want %g (%+v)", col, w[col], wantW, w)
		}
	}
}

// TestEditDeterministic pins byte-identical saves for identical edit sequences.
func TestEditDeterministic(t *testing.T) {
	run := func() []byte {
		f, err := Edit(editFixture())
		if err != nil {
			t.Fatal(err)
		}
		sh, err := f.Sheet("Data")
		if err != nil {
			t.Fatal(err)
		}
		if err := sh.SetString(5, 1, "det"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.AddSheet("Extra"); err != nil {
			t.Fatal(err)
		}
		out, err := f.Save()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if !bytes.Equal(run(), run()) {
		t.Error("identical edit sequences produced different bytes")
	}
}

// TestEditCalcChainInvalidation pins that a value edit drops a stale
// xl/calcChain.xml along with its content-type and relationship entries.
func TestEditCalcChainInvalidation(t *testing.T) {
	// Assemble a package with a calcChain part by post-processing the fixture.
	base := editFixture()
	names, parts := zipParts(t, base)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		data := parts[name]
		switch name {
		case "[Content_Types].xml":
			data = bytes.Replace(data, []byte("</Types>"),
				[]byte(`<Override PartName="/xl/calcChain.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.calcChain+xml"/></Types>`), 1)
		case "xl/_rels/workbook.xml.rels":
			data = bytes.Replace(data, []byte("</Relationships>"),
				[]byte(`<Relationship Id="rId99" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/calcChain" Target="calcChain.xml"/></Relationships>`), 1)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	w, err := zw.Create("xl/calcChain.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`<calcChain xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><c r="B2" i="1"/></calcChain>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := Edit(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetNumber(2, 2, 21); err != nil {
		t.Fatal(err)
	}
	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	_, outParts := zipParts(t, out)
	if _, ok := outParts["xl/calcChain.xml"]; ok {
		t.Error("stale calcChain.xml survived a value edit")
	}
	if bytes.Contains(outParts["[Content_Types].xml"], []byte("calcChain")) {
		t.Error("calcChain content type survived")
	}
	if bytes.Contains(outParts["xl/_rels/workbook.xml.rels"], []byte("calcChain")) {
		t.Error("calcChain relationship survived")
	}
	// The output still opens.
	if _, err := OpenBytes(out); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

// TestNewBlankWorkbook pins the writer-first starting point.
func TestNewBlankWorkbook(t *testing.T) {
	f := New()
	if names := f.SheetNames(); len(names) != 1 || names[0] != "Sheet1" {
		t.Fatalf("SheetNames = %v", names)
	}
	out, err := f.Save()
	if err != nil {
		t.Fatal(err)
	}
	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatalf("a fresh blank workbook must open: %v", err)
	}
	if len(wb.Sheets) != 1 || wb.Sheets[0].Hidden {
		t.Errorf("blank workbook sheets = %+v", wb.Sheets)
	}
	// New is deterministic.
	if !bytes.Equal(blankWorkbook(), blankWorkbook()) {
		t.Error("blank workbook is not deterministic")
	}
}
