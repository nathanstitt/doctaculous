package xlsx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// externalWorkbooks pins each committed real-world fixture's shape as read by
// this package — sheet count and the feature surfaces the calc adoption
// depends on (conditional formats, notes, pivots) — verified at download time.
// Producers span Excel, Mac Excel, LibreOffice 5.4/25.2, and an ISO-strict
// workbook; see testdata/external/xlsx/README.md for provenance and licensing.
var externalWorkbooks = []struct {
	file                      string
	sheets, cf, notes, pivots int
}{
	{"50784-font_theme_colours.xlsx", 3, 0, 0, 0},
	{"55406_Conditional_formatting_sample.xlsx", 1, 2, 0, 0},
	{"ExcelPivotTableSample.xlsx", 3, 0, 0, 2},
	{"NewStyleConditionalFormattings.xlsx", 1, 18, 0, 0},
	{"SimpleWithComments.xlsx", 3, 0, 3, 0},
	{"Themes.xlsx", 1, 0, 0, 0},
	{"WithConditionalFormatting.xlsx", 3, 3, 0, 0},
	{"WithVariousData.xlsx", 3, 0, 2, 0},
	{"autofilter.xlsx", 1, 0, 0, 0},
	{"cell-note.xlsx", 1, 0, 1, 0},
	{"filter_type.xlsx", 3, 0, 0, 0}, // ISO-strict namespaces
}

// TestExternalCorpusPreservation enforces the editor's preservation contract
// against real Excel- and LibreOffice-authored workbooks: the reader opens
// each file and sees its pinned features, a no-op Edit+Save is part-for-part
// BYTE-IDENTICAL, and a real cell edit still reopens through the reader.
// Skips if the corpus is absent (a sparse checkout).
func TestExternalCorpusPreservation(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "external", "xlsx")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("external xlsx corpus not present")
	}
	for _, tc := range externalWorkbooks {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Skipf("fixture missing: %v", err)
			}

			// The reader sees the pinned shape.
			wb, err := OpenBytes(data)
			if err != nil {
				t.Fatalf("OpenBytes: %v", err)
			}
			if len(wb.Sheets) != tc.sheets {
				t.Errorf("sheets = %d, want %d", len(wb.Sheets), tc.sheets)
			}
			cf, notes := 0, 0
			for _, s := range wb.Sheets {
				cf += len(s.CondFmts)
				notes += len(s.Comments)
			}
			if cf != tc.cf {
				t.Errorf("conditional formats = %d, want %d", cf, tc.cf)
			}
			if notes != tc.notes {
				t.Errorf("notes = %d, want %d", notes, tc.notes)
			}

			// The pivot read path agrees on the editor view.
			f, err := Edit(data)
			if err != nil {
				t.Fatalf("Edit: %v", err)
			}
			if got := len(f.PivotTables()); got != tc.pivots {
				t.Errorf("pivots = %d, want %d", got, tc.pivots)
			}

			// The preservation contract: a no-op Edit+Save is part-for-part
			// byte-identical (reads never dirty).
			out, err := f.Save()
			if err != nil {
				t.Fatalf("no-op Save: %v", err)
			}
			wantNames, wantParts := zipParts(t, data)
			gotNames, gotParts := zipParts(t, out)
			if len(wantNames) != len(gotNames) {
				t.Fatalf("part count changed: %d -> %d", len(wantNames), len(gotNames))
			}
			for _, name := range wantNames {
				if !bytes.Equal(wantParts[name], gotParts[name]) {
					t.Errorf("no-op save changed part %s", name)
				}
			}

			// A real edit still reopens through the reader.
			f2, err := Edit(data)
			if err != nil {
				t.Fatalf("Edit: %v", err)
			}
			sh, err := f2.Sheet(f2.SheetNames()[0])
			if err != nil {
				t.Fatalf("Sheet: %v", err)
			}
			if err := sh.SetString(1, 1, "edited by test"); err != nil {
				t.Fatalf("SetString: %v", err)
			}
			edited, err := f2.Save()
			if err != nil {
				t.Fatalf("Save after edit: %v", err)
			}
			if _, err := OpenBytes(edited); err != nil {
				t.Errorf("edited workbook does not reopen: %v", err)
			}
		})
	}
}
