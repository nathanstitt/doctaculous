package xlsx

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

// buildSheet wraps one <sheetData> body in the minimum OPC structure OpenBytes
// accepts, so a test can aim a single hostile attribute at the parser. It takes
// a testing.TB rather than a *testing.T so the fuzz seed corpus can build its
// inputs with it too.
func buildSheet(tb testing.TB, sheetData string) []byte {
	tb.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		tb.Helper()
		w, err := z.Create(name)
		if err != nil {
			tb.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			tb.Fatalf("zip write %s: %v", name, err)
		}
	}
	add("[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/></Types>`)
	add("_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="`+relOfficeDocument+`" Target="xl/workbook.xml"/></Relationships>`)
	add("xl/workbook.xml", `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<sheets><sheet name="S1" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" `+
		`Target="worksheets/sheet1.xml"/></Relationships>`)
	add("xl/worksheets/sheet1.xml", `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
		sheetData+`</worksheet>`)
	if err := z.Close(); err != nil {
		tb.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestOpenBytesMalformedRefs covers cell references that size the dense grid in
// parseSheet. Each of these took the process down before the bounds in parseRef:
// the column loop overflowed int (14 letters wrapped negative, panicking make
// with "len out of range" straight out of the public entry point), and an
// unbounded row number allocated until the kernel killed the process -- which
// no recover() can catch, so the per-page recover guarantee did not hold.
//
// The parser must treat an out-of-sheet address as malformed and keep going.
func TestOpenBytesMalformedRefs(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"column overflows to negative", "ZZZZZZZZZZZZZZ1"},
		{"column past sheet width", "ZZZZZZZZZ1"},
		{"column just past XFD", "XFE1"},
		{"row past sheet height", "A999999999999"},
		{"row just past maximum", "A1048577"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildSheet(t, `<sheetData><row><c r="`+c.ref+`" t="str"><v>x</v></c></row></sheetData>`)
			// Before the fix this panicked out of OpenBytes with "makeslice:
			// len out of range" (columns) or allocated until the process was
			// killed (rows). Either one fails the run here.
			wb, err := OpenBytes(context.Background(), data)
			if err != nil {
				return // rejecting the file outright is an acceptable outcome
			}
			if wb == nil {
				t.Fatal("OpenBytes returned nil workbook and nil error")
			}
			// The bad cell is dropped; the sheet must not carry a grid sized
			// from the bogus address.
			for _, sh := range wb.Sheets {
				if len(sh.Cells) > maxSheetRows {
					t.Errorf("sheet has %d rows, past the %d sheet limit", len(sh.Cells), maxSheetRows)
				}
				for i, row := range sh.Cells {
					if len(row) > maxSheetCols {
						t.Errorf("row %d has %d cols, past the %d sheet limit", i, len(row), maxSheetCols)
					}
				}
			}
		})
	}
}

// TestOpenBytesMalformedColElement aims the same class of value at the <col>
// width declaration, which fills a map keyed by column index.
func TestOpenBytesMalformedColElement(t *testing.T) {
	cases := []struct {
		name string
		attr string
	}{
		{"span covers an absurd range", `min="1" max="999999999"`},
		{"start past the sheet", `min="999999999" max="999999999"`},
		{"reversed range", `min="500" max="1"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildSheet(t, `<cols><col `+c.attr+` width="10"/></cols>`+
				`<sheetData><row><c r="A1" t="str"><v>x</v></c></row></sheetData>`)
			wb, err := OpenBytes(context.Background(), data)
			if err != nil {
				return
			}
			for _, sh := range wb.Sheets {
				if len(sh.ColWidths) > maxSheetCols {
					t.Errorf("ColWidths has %d entries, past the %d sheet limit",
						len(sh.ColWidths), maxSheetCols)
				}
				for col := range sh.ColWidths {
					if col < 0 || col >= maxSheetCols {
						t.Errorf("ColWidths holds out-of-sheet column %d", col)
					}
				}
			}
		})
	}
}

// FuzzOpenBytes exercises the parser against arbitrary input. The seeds are the
// shapes that historically crashed it; the property under test is only that
// OpenBytes returns rather than panicking or allocating without bound.
func FuzzOpenBytes(f *testing.F) {
	f.Add(buildSheet(f, `<sheetData><row><c r="A1" t="str"><v>x</v></c></row></sheetData>`))
	f.Add(buildSheet(f, `<sheetData><row><c r="ZZZZZZZZZZZZZZ1" t="str"><v>x</v></c></row></sheetData>`))
	f.Add(buildSheet(f, `<sheetData><row><c r="A999999999999" t="str"><v>x</v></c></row></sheetData>`))
	f.Add(buildSheet(f, `<cols><col min="1" max="999999999" width="10"/></cols><sheetData/>`))
	f.Add([]byte("PK\x03\x04not a zip"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		wb, err := OpenBytes(context.Background(), data)
		if err != nil {
			return
		}
		// Whatever came back has to respect the sheet bounds: those are what
		// keep the dense grid allocation finite.
		for _, sh := range wb.Sheets {
			if len(sh.Cells) > maxSheetRows {
				t.Fatalf("sheet has %d rows, past the %d sheet limit", len(sh.Cells), maxSheetRows)
			}
			for _, row := range sh.Cells {
				if len(row) > maxSheetCols {
					t.Fatalf("row has %d cols, past the %d sheet limit", len(row), maxSheetCols)
				}
			}
		}
	})
}
