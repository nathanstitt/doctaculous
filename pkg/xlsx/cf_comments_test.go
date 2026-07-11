package xlsx

import (
	"bytes"
	"strings"
	"testing"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// cfFixture carries a typed cellIs rule with a dxf, plus a dataBar rule the
// editor does not model (the opaque-passthrough case).
func cfFixture() []byte {
	sheet := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <sheetData><row r="1"><c r="A1"><v>5</v></c><c r="B1"><v>50</v></c></row></sheetData>
 <conditionalFormatting sqref="A1:A9">
  <cfRule type="cellIs" dxfId="0" priority="1" operator="greaterThan" stopIfTrue="1"><formula>3</formula></cfRule>
 </conditionalFormatting>
 <conditionalFormatting sqref="B1:B9">
  <cfRule type="dataBar" priority="2"><dataBar><cfvo type="min"/><cfvo type="max"/><color rgb="FF638EC6"/></dataBar></cfRule>
 </conditionalFormatting>
</worksheet>`
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <fonts count="1"><font/></fonts>
 <fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
 <borders count="1"><border/></borders>
 <cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellXfs>
 <dxfs count="1"><dxf><font><b/><color rgb="FF9C0006"/></font><fill><patternFill><bgColor rgb="FFFFC7CE"/></patternFill></fill></dxf></dxfs>
</styleSheet>`
	return genxlsx.New().AddSheet("CF", sheet).SetStyles(styles).Bytes()
}

// TestCFRead pins the reader view: typed fields, dxf resolution, and the
// byte-faithful Raw passthrough token.
func TestCFRead(t *testing.T) {
	wb, err := OpenBytes(cfFixture())
	if err != nil {
		t.Fatal(err)
	}
	cfs := wb.Sheets[0].CondFmts
	if len(cfs) != 2 {
		t.Fatalf("blocks = %d, want 2", len(cfs))
	}

	typed := cfs[0]
	if len(typed.Ranges) != 1 || typed.Ranges[0] != "A1:A9" {
		t.Errorf("ranges = %v", typed.Ranges)
	}
	rule := typed.Rules[0]
	if rule.Type != "cellIs" || rule.Operator != "greaterThan" || rule.Priority != 1 || !rule.StopIfTrue {
		t.Errorf("rule = %+v", rule)
	}
	if len(rule.Formulas) != 1 || rule.Formulas[0] != "3" {
		t.Errorf("formulas = %v", rule.Formulas)
	}
	if rule.Style == nil || !rule.Style.Font.Bold || rule.Style.Font.Color.RGB != "9C0006" || rule.Style.Fill.Bg.RGB != "FFC7CE" {
		t.Errorf("dxf = %+v", rule.Style)
	}
	if !bytes.Contains(rule.Raw, []byte(`operator="greaterThan"`)) {
		t.Errorf("raw = %s", rule.Raw)
	}

	opaque := cfs[1].Rules[0]
	if opaque.Type != "dataBar" || opaque.Style != nil {
		t.Errorf("dataBar rule = %+v", opaque)
	}
	for _, want := range []string{"<dataBar>", `<color rgb="FF638EC6"/>`, `<cfvo type="min"/>`} {
		if !bytes.Contains(opaque.Raw, []byte(want)) {
			t.Errorf("dataBar Raw missing %q:\n%s", want, opaque.Raw)
		}
	}

	// The editor sees the same view.
	f, err := Edit(cfFixture())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("CF")
	if err != nil {
		t.Fatal(err)
	}
	if got := sh.ConditionalFormats(); len(got) != 2 || got[0].Rules[0].Operator != "greaterThan" {
		t.Errorf("editor CF view = %+v", got)
	}
}

// TestCFSetAuthoritative pins the write path: an opaque rule re-emits
// verbatim (priority renumbered), a typed rule synthesizes with a minted dxf,
// and the whole set replaces what was there.
func TestCFSetAuthoritative(t *testing.T) {
	f, err := Edit(cfFixture())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("CF")
	if err != nil {
		t.Fatal(err)
	}
	existing := sh.ConditionalFormats()
	opaqueRaw := existing[1].Rules[0].Raw // the dataBar, carried opaquely

	next := []ConditionalFormatting{
		{Ranges: []string{"A1:A20"}, Rules: []CFRule{{
			Type: "containsText", Operator: "containsText", Text: "alert",
			Formulas: []string{`NOT(ISERROR(SEARCH("alert",A1)))`},
			Style:    &Style{Font: Font{Italic: true}, Fill: Fill{Pattern: "solid", Bg: Color{RGB: "FFEB9C"}}},
		}}},
		{Ranges: []string{"B1:B20"}, Rules: []CFRule{{Raw: opaqueRaw}}},
	}
	if err := sh.SetConditionalFormats(next); err != nil {
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
	cfs := wb.Sheets[0].CondFmts
	if len(cfs) != 2 {
		t.Fatalf("blocks after set = %d, want 2", len(cfs))
	}
	typed := cfs[0].Rules[0]
	if typed.Type != "containsText" || typed.Text != "alert" || typed.Priority != 1 {
		t.Errorf("typed rule = %+v", typed)
	}
	if typed.Style == nil || !typed.Style.Font.Italic || typed.Style.Fill.Bg.RGB != "FFEB9C" {
		t.Errorf("minted dxf = %+v", typed.Style)
	}
	opaque := cfs[1].Rules[0]
	if opaque.Type != "dataBar" || opaque.Priority != 2 {
		t.Errorf("opaque rule = %+v", opaque)
	}
	if !bytes.Contains(opaque.Raw, []byte(`<color rgb="FF638EC6"/>`)) {
		t.Errorf("opaque content lost:\n%s", opaque.Raw)
	}

	// Clearing removes every block.
	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	sh2, err := f2.Sheet("CF")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh2.SetConditionalFormats(nil); err != nil {
		t.Fatal(err)
	}
	wb2, err := OpenBytes(mustSave(t, f2))
	if err != nil {
		t.Fatal(err)
	}
	if len(wb2.Sheets[0].CondFmts) != 0 {
		t.Errorf("CF not cleared: %+v", wb2.Sheets[0].CondFmts)
	}
}

// TestCommentsRoundTrip pins notes end to end: set on a fresh workbook
// (creating the comments part, VML, rels, and content types), read back
// through both the editor and the Workbook reader, replace, and remove.
func TestCommentsRoundTrip(t *testing.T) {
	f := New()
	sh, err := f.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetString(2, 2, "flagged value"); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetComment(Comment{Row: 2, Col: 2, Author: "Reviewer", Text: "Reviewer (2026-07-10): check this"}); err != nil {
		t.Fatal(err)
	}
	if err := sh.SetComment(Comment{Row: 4, Col: 1, Author: "Other", Text: "second note"}); err != nil {
		t.Fatal(err)
	}
	out := mustSave(t, f)

	wb, err := OpenBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	notes := wb.Sheets[0].Comments
	if len(notes) != 2 {
		t.Fatalf("notes = %+v, want 2", notes)
	}
	if notes[0] != (Comment{Row: 2, Col: 2, Author: "Reviewer", Text: "Reviewer (2026-07-10): check this"}) {
		t.Errorf("note 0 = %+v", notes[0])
	}

	// The package wiring is complete: part, VML, rels, content types.
	_, parts := zipParts(t, out)
	if _, ok := parts["xl/comments1.xml"]; !ok {
		t.Error("comments part missing")
	}
	if _, ok := parts["xl/drawings/vmlDrawing1.vml"]; !ok {
		t.Error("VML drawing missing")
	}
	if !bytes.Contains(parts["[Content_Types].xml"], []byte(`Extension="vml"`)) {
		t.Error("vml content-type default missing")
	}
	if !bytes.Contains(parts["xl/worksheets/sheet1.xml"], []byte("legacyDrawing")) {
		t.Error("sheet legacyDrawing reference missing")
	}
	if !bytes.Contains(parts["xl/drawings/vmlDrawing1.vml"], []byte("<x:Row>1</x:Row>")) {
		t.Error("VML note anchor missing")
	}

	// Replace one note, remove the other.
	f2, err := Edit(out)
	if err != nil {
		t.Fatal(err)
	}
	sh2, err := f2.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if got := sh2.Comments(); len(got) != 2 {
		t.Fatalf("editor Comments = %+v", got)
	}
	if err := sh2.SetComment(Comment{Row: 2, Col: 2, Author: "Reviewer", Text: "updated"}); err != nil {
		t.Fatal(err)
	}
	if err := sh2.RemoveComment(4, 1); err != nil {
		t.Fatal(err)
	}
	wb2, err := OpenBytes(mustSave(t, f2))
	if err != nil {
		t.Fatal(err)
	}
	notes2 := wb2.Sheets[0].Comments
	if len(notes2) != 1 || notes2[0].Text != "updated" {
		t.Errorf("after replace/remove = %+v", notes2)
	}
}

// TestCommentsPreserveOtherParts pins that note edits stay surgical.
func TestCommentsPreserveOtherParts(t *testing.T) {
	src := editFixture()
	f, err := Edit(src)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.Sheet("Data")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetComment(Comment{Row: 1, Col: 1, Author: "A", Text: "note"}); err != nil {
		t.Fatal(err)
	}
	out := mustSave(t, f)
	_, want := zipParts(t, src)
	_, got := zipParts(t, out)
	// The OTHER sheet and the styles part stay byte-identical.
	for _, name := range []string{"xl/worksheets/sheet2.xml", "xl/styles.xml", "xl/workbook.xml"} {
		if !bytes.Equal(want[name], got[name]) {
			t.Errorf("untouched part %s changed", name)
		}
	}
	if !strings.Contains(string(got["xl/worksheets/sheet1.xml"]), "legacyDrawing") {
		t.Error("edited sheet lacks the legacyDrawing reference")
	}
}
