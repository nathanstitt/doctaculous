package xlsx

import (
	"bytes"
	"strings"
	"testing"
)

// pivotSource builds a workbook with a small sales table plus an empty target
// sheet, returning the file open for editing.
func pivotSource(t *testing.T) *File {
	t.Helper()
	f := New()
	sh, err := f.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	rows := [][]any{
		{"Region", "Product", "Sales"},
		{"East", "Widget", 100.0},
		{"West", "Widget", 150.0},
		{"East", "Gadget", 75.0},
	}
	for r, row := range rows {
		for c, v := range row {
			var err error
			switch v := v.(type) {
			case string:
				err = sh.SetString(r+1, c+1, v)
			case float64:
				err = sh.SetNumber(r+1, c+1, v)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.AddSheet("Pivot"); err != nil {
		t.Fatal(err)
	}
	return f
}

func testPivot() PivotTable {
	return PivotTable{
		Name:           "SalesPivot",
		SourceSheet:    "Sheet1",
		SourceRange:    "A1:C4",
		TargetSheet:    "Pivot",
		Location:       "A3",
		Rows:           []string{"Region"},
		Cols:           []string{"Product"},
		Values:         []PivotValueField{{Field: "Sales", Aggregation: "sum"}},
		RowGrandTotals: true,
		ColGrandTotals: false,
	}
}

// TestPivotRoundTrip pins AddPivotTable's package wiring and that our own
// reader recovers the full definition (the round-trip oracle).
func TestPivotRoundTrip(t *testing.T) {
	f := pivotSource(t)
	if err := f.AddPivotTable(testPivot()); err != nil {
		t.Fatal(err)
	}
	out := mustSave(t, f)

	// The wiring is complete: parts, rels, content types, workbook caches.
	_, parts := zipParts(t, out)
	for _, name := range []string{
		"xl/pivotCache/pivotCacheDefinition1.xml",
		"xl/pivotCache/pivotCacheRecords1.xml",
		"xl/pivotCache/_rels/pivotCacheDefinition1.xml.rels",
		"xl/pivotTables/pivotTable1.xml",
		"xl/pivotTables/_rels/pivotTable1.xml.rels",
	} {
		if _, ok := parts[name]; !ok {
			t.Errorf("part %s missing", name)
		}
	}
	if !bytes.Contains(parts["xl/workbook.xml"], []byte("<pivotCaches>")) {
		t.Error("workbook pivotCaches missing")
	}
	if !bytes.Contains(parts["[Content_Types].xml"], []byte("pivotTable+xml")) {
		t.Error("pivotTable content type missing")
	}
	if !bytes.Contains(parts["xl/pivotCache/pivotCacheDefinition1.xml"], []byte(`refreshOnLoad="1"`)) {
		t.Error("cache must refresh on load (values recompute, not round-trip)")
	}

	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	pts := f2.PivotTables()
	if len(pts) != 1 {
		t.Fatalf("pivot tables = %d, want 1", len(pts))
	}
	got := pts[0]
	want := testPivot()
	want.Location = "A3" // as written
	if got.Name != want.Name || got.SourceSheet != want.SourceSheet ||
		got.SourceRange != want.SourceRange || got.TargetSheet != want.TargetSheet ||
		got.Location != want.Location {
		t.Errorf("identity = %+v", got)
	}
	if len(got.Rows) != 1 || got.Rows[0] != "Region" {
		t.Errorf("rows = %v", got.Rows)
	}
	if len(got.Cols) != 1 || got.Cols[0] != "Product" {
		t.Errorf("cols = %v", got.Cols)
	}
	if len(got.Values) != 1 || got.Values[0].Field != "Sales" ||
		got.Values[0].Aggregation != "sum" || got.Values[0].DisplayName != "Sum of Sales" {
		t.Errorf("values = %+v", got.Values)
	}
	if !got.RowGrandTotals || got.ColGrandTotals {
		t.Errorf("grand totals = row %v col %v", got.RowGrandTotals, got.ColGrandTotals)
	}
	if got.StyleName != defaultPivotStyle {
		t.Errorf("style = %q", got.StyleName)
	}
}

// TestPivotRemoveThenAdd pins calc's save shape: RemovePivotTables clears
// every part and wire, and a fresh AddPivotTable after it does not duplicate.
func TestPivotRemoveThenAdd(t *testing.T) {
	f := pivotSource(t)
	if err := f.AddPivotTable(testPivot()); err != nil {
		t.Fatal(err)
	}
	withPivot := mustSave(t, f)

	f2, err := Edit(withPivot)
	if err != nil {
		t.Fatal(err)
	}
	if err := f2.RemovePivotTables(); err != nil {
		t.Fatal(err)
	}
	cleared := mustSave(t, f2)
	_, parts := zipParts(t, cleared)
	for name := range parts {
		if strings.HasPrefix(name, "xl/pivotCache/") || strings.HasPrefix(name, "xl/pivotTables/") {
			t.Errorf("pivot part %s survived removal", name)
		}
	}
	if bytes.Contains(parts["xl/workbook.xml"], []byte("pivotCache")) {
		t.Error("workbook pivotCaches survived removal")
	}
	if bytes.Contains(parts["[Content_Types].xml"], []byte("pivotTable+xml")) {
		t.Error("pivot content type survived removal")
	}
	if bytes.Contains(parts[sheetRelsName("xl/worksheets/sheet2.xml")], []byte("pivotTable")) {
		t.Error("sheet pivot relationship survived removal")
	}

	// Remove-then-add in ONE session: exactly one pivot afterwards.
	f3, err := Edit(withPivot)
	if err != nil {
		t.Fatal(err)
	}
	if err := f3.RemovePivotTables(); err != nil {
		t.Fatal(err)
	}
	pt := testPivot()
	pt.Values[0].Aggregation = "average"
	pt.Values[0].DisplayName = "Avg Sales"
	if err := f3.AddPivotTable(pt); err != nil {
		t.Fatal(err)
	}
	f4, err := Edit(mustSave(t, f3))
	if err != nil {
		t.Fatal(err)
	}
	pts := f4.PivotTables()
	if len(pts) != 1 {
		t.Fatalf("pivots after remove+add = %d, want 1", len(pts))
	}
	if pts[0].Values[0].Aggregation != "average" || pts[0].Values[0].DisplayName != "Avg Sales" {
		t.Errorf("values = %+v", pts[0].Values)
	}
}

// TestPivotBadField pins the error path: axis/value fields must name source
// header columns.
func TestPivotBadField(t *testing.T) {
	f := pivotSource(t)
	pt := testPivot()
	pt.Rows = []string{"Nonexistent"}
	if err := f.AddPivotTable(pt); err == nil || !strings.Contains(err.Error(), "Nonexistent") {
		t.Errorf("err = %v, want unknown-field error", err)
	}
}

// TestDefinedNamesRoundTrip pins SetDefinedNames against both the editor and
// Workbook reader views, including scope and hidden flags.
func TestDefinedNamesRoundTrip(t *testing.T) {
	f := New()
	one := 0
	names := []DefinedName{
		{Name: "SalesData", RefersTo: "Sheet1!$A$1:$C$4"},
		{Name: "LocalName", RefersTo: "Sheet1!$B$2", LocalSheet: &one, Hidden: true},
	}
	if err := f.SetDefinedNames(names); err != nil {
		t.Fatal(err)
	}
	out := mustSave(t, f)

	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(wb.DefinedNames) != 2 {
		t.Fatalf("reader names = %+v", wb.DefinedNames)
	}
	if wb.DefinedNames[0].Name != "SalesData" || wb.DefinedNames[0].RefersTo != "Sheet1!$A$1:$C$4" {
		t.Errorf("name 0 = %+v", wb.DefinedNames[0])
	}
	local := wb.DefinedNames[1]
	if local.LocalSheet == nil || *local.LocalSheet != 0 || !local.Hidden {
		t.Errorf("name 1 = %+v", local)
	}

	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := f2.DefinedNames(); len(got) != 2 || got[1].Name != "LocalName" {
		t.Errorf("editor names = %+v", got)
	}

	// Replacing and clearing are authoritative.
	if err := f2.SetDefinedNames(nil); err != nil {
		t.Fatal(err)
	}
	wb2, err := OpenBytes(mustSave(t, f2))
	if err != nil {
		t.Fatal(err)
	}
	if len(wb2.DefinedNames) != 0 {
		t.Errorf("names not cleared: %+v", wb2.DefinedNames)
	}
}
