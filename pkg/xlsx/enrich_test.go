package xlsx

import (
	"testing"
	"time"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// enrichedWorkbook builds a fixture exercising the full enriched-read surface:
// typed values, formulas (plain + shared), the complete style vocabulary,
// row/col dimensions, frozen panes, tab color, and defined names.
func enrichedWorkbook(t *testing.T) *Workbook {
	t.Helper()
	sheetXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <sheetPr><tabColor rgb="FF00AA55"/></sheetPr>
 <sheetViews><sheetView workbookViewId="0"><pane xSplit="1" ySplit="2" state="frozen" topLeftCell="B3"/></sheetView></sheetViews>
 <sheetFormatPr defaultRowHeight="14.5" defaultColWidth="9.1"/>
 <cols><col min="1" max="1" width="22.5" customWidth="1"/><col min="2" max="3" width="12" customWidth="1"/></cols>
 <sheetData>
  <row r="1" ht="30" customHeight="1"><c r="A1" t="s" s="1"><v>0</v></c><c r="B1"><v>42</v></c><c r="C1"><v>0.5</v></c></row>
  <row r="2" s="2" customFormat="1"><c r="A2" t="b"><v>1</v></c><c r="B2" s="3"><v>45931</v></c><c r="C2" t="e"><v>#DIV/0!</v></c></row>
  <row r="3"><c r="A3" t="str"><f>CONCAT("a","b")</f><v>ab</v></c><c r="B3"><f t="shared" ref="B3:B4" si="0">A3*2</f><v>4</v></c><c r="C3" t="inlineStr"><is><t>inline</t></is></c></row>
  <row r="4"><c r="B4"><f t="shared" si="0"/><v>6</v></c><c r="C4"><f>$A$1+B4</f><v>7</v></c></row>
 </sheetData>
 <mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>
</worksheet>`
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <numFmts count="1"><numFmt numFmtId="164" formatCode="0.000&quot;kg&quot;"/></numFmts>
 <fonts count="4">
  <font><sz val="11"/><name val="Calibri"/></font>
  <font><b/><i/><strike/><u val="double"/><sz val="14"/><name val="Georgia"/><color rgb="FF336699"/></font>
  <font><u/><color theme="4" tint="-0.25"/></font>
  <font><color indexed="12"/></font>
 </fonts>
 <fills count="3">
  <fill><patternFill patternType="none"/></fill>
  <fill><patternFill patternType="gray125"/></fill>
  <fill><patternFill patternType="solid"><fgColor rgb="FFFFEE00"/><bgColor indexed="64"/></patternFill></fill>
 </fills>
 <borders count="2">
  <border><left/><right/><top/><bottom/><diagonal/></border>
  <border diagonalUp="1"><left style="thin"><color rgb="FF111111"/></left><right style="double"><color rgb="FF222222"/></right><top style="dashed"/><bottom style="medium"/><diagonal style="hair"/></border>
 </borders>
 <cellXfs count="4">
  <xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>
  <xf numFmtId="164" fontId="1" fillId="2" borderId="1"><alignment horizontal="center" vertical="top" wrapText="1" indent="2" textRotation="45" shrinkToFit="1"/><protection locked="0" hidden="1"/></xf>
  <xf numFmtId="0" fontId="2" fillId="1" borderId="0"/>
  <xf numFmtId="14" fontId="0" fillId="0" borderId="0"/>
 </cellXfs>
</styleSheet>`
	shared := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="1" uniqueCount="1"><si><t>Header</t></si></sst>`

	data := genxlsx.New().
		AddSheet("Enriched", sheetXML).
		SetStyles(styles).
		SetSharedStrings(shared).
		SetDefinedNames(`<definedName name="Total" hidden="1">Enriched!$B$1</definedName><definedName name="Local" localSheetId="0">Enriched!$A$1:$A$4</definedName>`).
		Bytes()
	wb, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	return wb
}

func TestEnrichedTypedValues(t *testing.T) {
	wb := enrichedWorkbook(t)
	s := wb.Sheets[0]

	cases := []struct {
		name     string
		row, col int
		want     Value
	}{
		{"shared string", 0, 0, Value{Kind: KindString, S: "Header"}},
		{"number", 0, 1, Value{Kind: KindNumber, F: 42}},
		{"fraction", 0, 2, Value{Kind: KindNumber, F: 0.5}},
		{"bool", 1, 0, Value{Kind: KindBool, B: true}},
		{"error", 1, 2, Value{Kind: KindError, S: "#DIV/0!"}},
		{"formula cached string", 2, 0, Value{Kind: KindString, S: "ab"}},
		{"inline string", 2, 2, Value{Kind: KindString, S: "inline"}},
		{"padding empty", 3, 0, Value{}},
	}
	for _, c := range cases {
		got := s.Cells[c.row][c.col].Value
		if got != c.want {
			t.Errorf("%s: Value = %+v, want %+v", c.name, got, c.want)
		}
	}

	// The date cell: builtin id 14 on a serial → KindDate with the converted time.
	date := s.Cells[1][1].Value
	if date.Kind != KindDate || date.F != 45931 {
		t.Fatalf("date value = %+v", date)
	}
	if want := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC); !date.T.Equal(want) {
		t.Errorf("date T = %v, want %v", date.T, want)
	}
}

func TestEnrichedFormulas(t *testing.T) {
	wb := enrichedWorkbook(t)
	s := wb.Sheets[0]

	if got := s.Cells[2][0].Formula; got != `CONCAT("a","b")` {
		t.Errorf("plain formula = %q", got)
	}
	// Shared group: the master keeps its text; the member gets the master
	// shifted by its offset (one row down: A3*2 → A4*2).
	if got := s.Cells[2][1].Formula; got != "A3*2" {
		t.Errorf("shared master = %q", got)
	}
	if got := s.Cells[3][1].Formula; got != "A4*2" {
		t.Errorf("shared member = %q, want A4*2", got)
	}
	// $-anchored references stay fixed.
	if got := s.Cells[3][2].Formula; got != "$A$1+B4" {
		t.Errorf("anchored formula = %q", got)
	}
	if got := s.Cells[0][1].Formula; got != "" {
		t.Errorf("plain value cell has formula %q", got)
	}
}

func TestEnrichedStyles(t *testing.T) {
	wb := enrichedWorkbook(t)
	s := wb.Sheets[0]

	// xf 1 (A1): the full vocabulary.
	st := s.Cells[0][0].Style
	if st == nil {
		t.Fatal("A1 has no resolved style")
	}
	if s.Cells[0][0].StyleID != 1 {
		t.Errorf("StyleID = %d, want 1", s.Cells[0][0].StyleID)
	}
	f := st.Font
	if !f.Bold || !f.Italic || !f.Strike || f.Underline != "double" || f.Size != 14 || f.Name != "Georgia" || f.Color.RGB != "336699" {
		t.Errorf("font = %+v", f)
	}
	if st.Fill.Pattern != "solid" || st.Fill.Fg.RGB != "FFEE00" || st.Fill.Bg.Indexed == nil || *st.Fill.Bg.Indexed != 64 {
		t.Errorf("fill = %+v", st.Fill)
	}
	a := st.Alignment
	if a.Horizontal != "center" || a.Vertical != "top" || !a.WrapText || a.Indent != 2 || a.TextRotation != 45 || !a.ShrinkToFit {
		t.Errorf("alignment = %+v", a)
	}
	b := st.Border
	if b.Left.Style != "thin" || b.Left.Color.RGB != "111111" || b.Right.Style != "double" ||
		b.Top.Style != "dashed" || b.Bottom.Style != "medium" || b.Diagonal.Style != "hair" || !b.DiagonalUp {
		t.Errorf("border = %+v", b)
	}
	if st.NumFmtID != 164 || st.NumFmt != `0.000"kg"` {
		t.Errorf("numFmt = %d %q", st.NumFmtID, st.NumFmt)
	}
	if st.Protection == nil || st.Protection.Locked || !st.Protection.Hidden {
		t.Errorf("protection = %+v", st.Protection)
	}

	// xf 2: an underline with no val is "single"; a theme color carries tint.
	st2 := s.Cells[1][0].Style // A2 inherits the ROW style? no — cell has no s attr → xf 0... row s=2 applies to the ROW, cells keep their own.
	_ = st2
	// Theme color + bare underline via the row style entry instead:
	rowStyle := s.RowStyles[1]
	if rowStyle == nil {
		t.Fatal("row 2 has no row style")
	}
	if rowStyle.Font.Underline != "single" {
		t.Errorf("bare <u/> = %q, want single", rowStyle.Font.Underline)
	}
	if rowStyle.Font.Color.Theme == nil || *rowStyle.Font.Color.Theme != 4 || rowStyle.Font.Color.Tint != -0.25 {
		t.Errorf("theme color = %+v", rowStyle.Font.Color)
	}
	if rowStyle.Fill.Pattern != "gray125" {
		t.Errorf("row fill pattern = %q", rowStyle.Fill.Pattern)
	}

	// The builtin table resolves ids without custom entries (xf 3 → id 14).
	if st3 := s.Cells[1][1].Style; st3 == nil || st3.NumFmt != "mm-dd-yy" {
		t.Errorf("builtin numFmt resolution = %+v", st3)
	}

	// The legacy display facts are untouched by the enrichment.
	if c := s.Cells[0][0]; !c.Bold || !c.Italic || c.FillRGB != "FFEE00" || c.Align != "center" {
		t.Errorf("legacy facts = %+v", c)
	}
}

func TestEnrichedSheetFacts(t *testing.T) {
	wb := enrichedWorkbook(t)
	s := wb.Sheets[0]

	if s.TabColorRGB != "00AA55" {
		t.Errorf("tab color = %q", s.TabColorRGB)
	}
	if s.FrozenCols != 1 || s.FrozenRows != 2 {
		t.Errorf("frozen = %d cols, %d rows", s.FrozenCols, s.FrozenRows)
	}
	if s.DefaultRowHeight != 14.5 || s.DefaultColWidth != 9.1 {
		t.Errorf("defaults = %g x %g", s.DefaultRowHeight, s.DefaultColWidth)
	}
	if got := s.RowHeights[0]; got != 30 {
		t.Errorf("row 1 height = %g, want 30", got)
	}
	if _, ok := s.RowHeights[2]; ok {
		t.Errorf("row 3 should have no explicit height")
	}
	if s.ColWidths[0] != 22.5 || s.ColWidths[1] != 12 || s.ColWidths[2] != 12 {
		t.Errorf("col widths = %+v", s.ColWidths)
	}
	if _, ok := s.ColWidths[3]; ok {
		t.Errorf("col D should have no explicit width")
	}

	if !wb.Date1904 == false { // fixture uses the 1900 system
		t.Errorf("Date1904 = %v", wb.Date1904)
	}
	if len(wb.DefinedNames) != 2 {
		t.Fatalf("defined names = %+v", wb.DefinedNames)
	}
	dn := wb.DefinedNames[0]
	if dn.Name != "Total" || dn.RefersTo != "Enriched!$B$1" || !dn.Hidden || dn.LocalSheet != nil {
		t.Errorf("defined name 0 = %+v", dn)
	}
	if dn := wb.DefinedNames[1]; dn.LocalSheet == nil || *dn.LocalSheet != 0 {
		t.Errorf("defined name 1 = %+v", dn)
	}
}

func TestCoordinateHelpers(t *testing.T) {
	cases := []struct {
		row, col int
		ref      string
	}{
		{1, 1, "A1"}, {7, 2, "B7"}, {1, 26, "Z1"}, {3, 27, "AA3"}, {100, 703, "AAA100"},
	}
	for _, c := range cases {
		if got := CellRef(c.row, c.col); got != c.ref {
			t.Errorf("CellRef(%d,%d) = %q, want %q", c.row, c.col, got, c.ref)
		}
		r, col, err := ParseCellRef(c.ref)
		if err != nil || r != c.row || col != c.col {
			t.Errorf("ParseCellRef(%q) = (%d,%d,%v), want (%d,%d)", c.ref, r, col, err, c.row, c.col)
		}
	}
	if _, _, err := ParseCellRef("1A"); err == nil {
		t.Error("ParseCellRef(1A) should fail")
	}
	if r, c, err := ParseCellRef("$B$7"); err != nil || r != 7 || c != 2 {
		t.Errorf("anchored ref = (%d,%d,%v)", r, c, err)
	}
	rng, err := ParseRange("B7:A1")
	if err != nil || rng != (Range{StartRow: 1, StartCol: 1, EndRow: 7, EndCol: 2}) {
		t.Errorf("ParseRange normalized = %+v (%v)", rng, err)
	}
	if rng.String() != "A1:B7" {
		t.Errorf("Range.String() = %q", rng.String())
	}
	single, err := ParseRange("C3")
	if err != nil || single.String() != "C3" {
		t.Errorf("single-cell range = %+v (%v)", single, err)
	}
}

func TestShiftFormula(t *testing.T) {
	cases := []struct {
		src        string
		dRow, dCol int
		want       string
	}{
		{"A1*2", 1, 0, "A2*2"},
		{"A1*2", 0, 1, "B1*2"},
		{"$A$1+B2", 3, 3, "$A$1+E5"},
		{"$A1+A$1", 1, 1, "$A2+B$1"},
		{`SUM(A1:B2)`, 2, 0, `SUM(A3:B4)`},
		{`"A1 stays"&B1`, 1, 0, `"A1 stays"&B2`},
		{`'A1'!B1+1`, 1, 0, `'A1'!B2+1`},
		{"LOG10(5)", 1, 1, "LOG10(5)"}, // a "(" makes it a function call, not a reference
		// Bare SUM2020 IS a valid cell reference (column SUM < XFD) — Excel
		// shifts it too, so the lexical pass matching Excel is correct.
		{"SUM2020", 1, 1, "SUN2021"},
		{"A1", -5, 0, "#REF!"},       // shifted off the sheet
		{"XFD1+A1", 0, 0, "XFD1+A1"}, // zero delta is identity
		{"IF(A1>0,B1,C$2)", 1, 2, "IF(C2>0,D2,E$2)"},
	}
	for _, c := range cases {
		if got := shiftFormula(c.src, c.dRow, c.dCol); got != c.want {
			t.Errorf("shiftFormula(%q, %d, %d) = %q, want %q", c.src, c.dRow, c.dCol, got, c.want)
		}
	}
}
