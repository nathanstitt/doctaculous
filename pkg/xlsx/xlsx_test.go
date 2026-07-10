package xlsx

import (
	"errors"
	"testing"

	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

func openFixture(t *testing.T, name string) *Workbook {
	t.Helper()
	for _, f := range genxlsx.Core {
		if f.Name == name {
			wb, err := OpenBytes(f.Bytes())
			if err != nil {
				t.Fatalf("OpenBytes(%s): %v", name, err)
			}
			return wb
		}
	}
	t.Fatalf("no fixture %q", name)
	return nil
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		ref      string
		row, col int
	}{
		{"A1", 0, 0},
		{"B2", 1, 1},
		{"Z1", 0, 25},
		{"AA1", 0, 26},
		{"AB10", 9, 27},
		{"BA1", 0, 52},
		{"", -1, -1},
		{"12", -1, -1},
		{"A", -1, -1},
		{"A0", -1, -1},
	}
	for _, c := range cases {
		row, col := parseRef(c.ref)
		if row != c.row || col != c.col {
			t.Errorf("parseRef(%q) = (%d,%d), want (%d,%d)", c.ref, row, col, c.row, c.col)
		}
	}
}

func TestValuesFixture(t *testing.T) {
	wb := openFixture(t, "values")
	if len(wb.Sheets) != 1 {
		t.Fatalf("sheets = %d", len(wb.Sheets))
	}
	g := wb.Sheets[0].Cells
	checks := []struct {
		r, c int
		text string
	}{
		{0, 0, "Name"},          // shared string
		{0, 1, "Qty"},           // shared string
		{1, 0, "rich text"},     // rich-text shared string (runs concatenated)
		{1, 1, "42.5"},          // number, General
		{2, 0, "inline text"},   // inlineStr
		{2, 1, "TRUE"},          // boolean
		{3, 0, "cached result"}, // formula: the cached string, not the formula
		{3, 1, "#DIV/0!"},       // error cell
	}
	for _, c := range checks {
		if got := g[c.r][c.c].Text; got != c.text {
			t.Errorf("cell (%d,%d) = %q, want %q", c.r, c.c, got, c.text)
		}
	}
}

func TestDatesFixture(t *testing.T) {
	wb := openFixture(t, "dates")
	g := wb.Sheets[0].Cells
	checks := []struct {
		r, c int
		text string
	}{
		{0, 0, "2023-03-15"},       // builtin 14
		{0, 1, "12:00:00"},         // builtin 21 on a pure time fraction
		{1, 0, "2023-03-15"},       // custom yyyy-mm-dd
		{1, 1, "2023-03-15 18:00"}, // builtin 22 date-time
		{2, 0, "25%"},              // builtin percent
		{2, 1, "1234.5"},           // General
	}
	for _, c := range checks {
		if got := g[c.r][c.c].Text; got != c.text {
			t.Errorf("cell (%d,%d) = %q, want %q", c.r, c.c, got, c.text)
		}
	}
}

func TestDate1904Epoch(t *testing.T) {
	// Serial 100 in the 1904 system = 1904-04-10; in 1900 = 1900-04-09.
	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" s="1"><v>100</v></c></row></sheetData></worksheet>`
	styles := `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<cellXfs count="2"><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`

	wb, err := OpenBytes(genxlsx.New().AddSheet("S", sheet).SetStyles(styles).SetDate1904().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := wb.Sheets[0].Cells[0][0].Text; got != "1904-04-10" {
		t.Errorf("1904-system serial 100 = %q, want 1904-04-10", got)
	}
	wb, err = OpenBytes(genxlsx.New().AddSheet("S", sheet).SetStyles(styles).Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := wb.Sheets[0].Cells[0][0].Text; got != "1900-04-09" {
		t.Errorf("1900-system serial 100 = %q, want 1900-04-09", got)
	}
}

func TestStyledFixture(t *testing.T) {
	wb := openFixture(t, "styled")
	g := wb.Sheets[0].Cells
	if !g[0][0].Bold {
		t.Errorf("A1 not bold: %+v", g[0][0])
	}
	if !g[1][0].Italic || g[1][0].FillRGB != "FFEECC" {
		t.Errorf("A2 style wrong: %+v", g[1][0])
	}
	if g[2][0].Align != "right" {
		t.Errorf("A3 align = %q, want right", g[2][0].Align)
	}
}

func TestMergedFixture(t *testing.T) {
	wb := openFixture(t, "merged")
	s := wb.Sheets[0]
	if len(s.Merges) != 2 {
		t.Fatalf("merges = %d, want 2", len(s.Merges))
	}
	if m := s.Merges[0]; m != (Merge{Row: 0, Col: 0, RowSpan: 1, ColSpan: 2}) {
		t.Errorf("merge A1:B1 = %+v", m)
	}
	if m := s.Merges[1]; m != (Merge{Row: 1, Col: 0, RowSpan: 2, ColSpan: 1}) {
		t.Errorf("merge A2:A3 = %+v", m)
	}
}

func TestMultisheetFixture(t *testing.T) {
	wb := openFixture(t, "multisheet")
	if len(wb.Sheets) != 3 {
		t.Fatalf("sheets = %d, want 3", len(wb.Sheets))
	}
	if wb.Sheets[0].Name != "First" || wb.Sheets[0].Hidden {
		t.Errorf("sheet 0 = %q hidden=%v", wb.Sheets[0].Name, wb.Sheets[0].Hidden)
	}
	if !wb.Sheets[2].Hidden {
		t.Errorf("sheet 2 should be hidden")
	}
}

func TestNotXLSX(t *testing.T) {
	if _, err := OpenBytes([]byte("not a zip")); !errors.Is(err, ErrNotXLSX) {
		t.Errorf("want ErrNotXLSX, got %v", err)
	}
}

func TestGeneralNumberFormatting(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"42", "42"},
		{"42.5", "42.5"},
		{"-3", "-3"},
		{"0.1", "0.1"},
		{"1e+20", "1e+20"},
	}
	for _, c := range cases {
		if got := formatNumber(c.raw, "", false); got != c.want {
			t.Errorf("formatNumber(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestIsDateCode(t *testing.T) {
	dates := []string{"mm-dd-yy", "yyyy\\-mm\\-dd", "h:mm:ss", "[h]:mm", "d-mmm"}
	for _, c := range dates {
		if !isDateCode(c) {
			t.Errorf("isDateCode(%q) = false, want true", c)
		}
	}
	notDates := []string{"", "0.00", "#,##0", "0%", `[Magenta]0.00`, `0.00" mph"`, `[>=100]0`}
	for _, c := range notDates {
		if isDateCode(c) {
			t.Errorf("isDateCode(%q) = true, want false", c)
		}
	}
}
