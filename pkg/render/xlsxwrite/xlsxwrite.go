// Package xlsxwrite renders the TABLES of a cssbox tree (the shared box model
// produced by every frontend) as a SpreadsheetML (.xlsx) workbook: one
// worksheet per table, named from the table's caption when present. Like the
// CSV writer it is tables-only — a spreadsheet is a grid, so non-table content
// is dropped with a logged count, and a table-less document produces a
// workbook with one empty sheet (Excel requires at least one) plus a loud log.
// Merged ranges emit natively via mergeCells; header-row cells are bold; a cell
// whose text is a clean number emits as a numeric cell so spreadsheets can
// compute over it. Output is deterministic (fixed zip stamps and part order),
// and every mapping round-trips through this repo's own pkg/xlsx reader.
package xlsxwrite

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// Options configures XLSX rendering.
type Options struct {
	// Logf receives degradation diagnostics (dropped non-table content, a
	// document with no tables). nil -> no-op.
	Logf func(string, ...any)
}

// Write renders the tables of the cssbox tree rooted at root to w as a
// complete .xlsx package. A nil root writes a valid single-empty-sheet
// workbook.
func Write(root *cssbox.Box, w io.Writer, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	tables, dropped := boxwalk.CollectTables(root)
	if len(tables) == 0 {
		opts.Logf("xlsxwrite: document has no tables; writing a workbook with one empty sheet")
	} else if dropped > 0 {
		opts.Logf("xlsxwrite: dropped %d non-table block(s); XLSX carries tables only", dropped)
	}

	var sheets []sheetPart
	names := newSheetNamer()
	for i, table := range tables {
		name := names.pick(captionText(table), i)
		sheets = append(sheets, sheetPart{name: name, xml: sheetXML(table)})
	}
	if len(sheets) == 0 {
		sheets = append(sheets, sheetPart{name: "Sheet1", xml: emptySheetXML})
	}

	pkg, err := assemblePackage(sheets)
	if err != nil {
		return fmt.Errorf("xlsxwrite: %w", err)
	}
	if _, err := w.Write(pkg); err != nil {
		return fmt.Errorf("xlsxwrite: %w", err)
	}
	return nil
}

type sheetPart struct{ name, xml string }

// captionText extracts a table's caption as plain text ("" when none).
func captionText(table *cssbox.Box) string {
	for _, c := range table.Children {
		if c.Display == cssbox.DisplayTableCaption {
			return boxwalk.CellPlainText(c)
		}
	}
	return ""
}

// sheetNamer produces valid, unique Excel sheet names: the forbidden
// characters ([ ] : * ? / \) become spaces, names truncate to Excel's 31-char
// limit, and duplicates gain a numeric suffix.
type sheetNamer struct{ used map[string]bool }

func newSheetNamer() *sheetNamer { return &sheetNamer{used: map[string]bool{}} }

var sheetNameSanitizer = strings.NewReplacer("[", " ", "]", " ", ":", " ", "*", " ", "?", " ", "/", " ", `\`, " ")

func (n *sheetNamer) pick(caption string, index int) string {
	name := strings.TrimSpace(sheetNameSanitizer.Replace(caption))
	if name == "" {
		name = fmt.Sprintf("Table %d", index+1)
	}
	if len(name) > 31 {
		name = strings.TrimSpace(name[:31])
	}
	base := name
	for i := 2; n.used[strings.ToLower(name)]; i++ {
		suffix := fmt.Sprintf(" %d", i)
		trimmed := base
		if len(trimmed)+len(suffix) > 31 {
			trimmed = strings.TrimSpace(trimmed[:31-len(suffix)])
		}
		name = trimmed + suffix
	}
	n.used[strings.ToLower(name)] = true
	return name
}

const sheetOpen = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`

const emptySheetXML = sheetOpen + `<sheetData/></worksheet>`

// sheetXML emits one table as a worksheet: origin cells carry their value (a
// bold style for header-row cells, numeric type for clean numbers), covered
// slots stay empty, and every spanning cell adds a mergeCells ref.
func sheetXML(table *cssbox.Box) string {
	grid := boxwalk.BuildOccupancyGrid(table)
	var sb strings.Builder
	sb.WriteString(sheetOpen + "<sheetData>")
	var merges []string
	for r, row := range grid.Slots {
		fmt.Fprintf(&sb, `<row r="%d">`, r+1)
		for c := 0; c < grid.Cols; c++ {
			idx := row[c]
			if idx < 0 {
				continue // a gap from a short row: absent cell = empty
			}
			cell := grid.Cells[idx]
			if cell.Row != r || cell.Col != c {
				continue // covered by a span; the origin carries the value
			}
			if cell.RowSpan > 1 || cell.ColSpan > 1 {
				merges = append(merges, fmt.Sprintf("%s%d:%s%d",
					colName(c), r+1, colName(c+cell.ColSpan-1), r+cell.RowSpan))
			}
			writeCell(&sb, r, c, boxwalk.CellPlainText(cell.Box), cell.Header)
		}
		sb.WriteString("</row>")
	}
	sb.WriteString("</sheetData>")
	if len(merges) > 0 {
		fmt.Fprintf(&sb, `<mergeCells count="%d">`, len(merges))
		for _, m := range merges {
			sb.WriteString(`<mergeCell ref="` + m + `"/>`)
		}
		sb.WriteString("</mergeCells>")
	}
	sb.WriteString("</worksheet>")
	return sb.String()
}

// writeCell emits one c element. A clean number (its General rendering
// reproduces the text exactly, so "007" stays a string) becomes a numeric
// cell; everything else is an inline string. Header cells reference the bold
// style (xf index 1 in the fixed styles part).
func writeCell(sb *strings.Builder, r, c int, text string, header bool) {
	if text == "" && !header {
		return // absent cell = empty
	}
	style := ""
	if header {
		style = ` s="1"`
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		if strconv.FormatFloat(f, 'g', -1, 64) == text || strconv.FormatInt(int64(f), 10) == text {
			fmt.Fprintf(sb, `<c r="%s%d"%s><v>%s</v></c>`, colName(c), r+1, style, text)
			return
		}
	}
	fmt.Fprintf(sb, `<c r="%s%d"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
		colName(c), r+1, style, escText.Replace(text))
}

// escText escapes XML character data.
var escText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// colName converts a zero-based column index to its A1-style letters.
func colName(c int) string {
	name := ""
	for c >= 0 {
		name = string(rune('A'+c%26)) + name
		c = c/26 - 1
	}
	return name
}

// stylesXML is the fixed styles part: xf 0 default, xf 1 bold (header rows).
const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
<borders count="1"><border/></borders>
<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" applyFont="1"/></cellXfs>
</styleSheet>
`

// assemblePackage serializes the OPC container deterministically (fixed part
// order and timestamps — two writes of the same tree are byte-identical).
func assemblePackage(sheets []sheetPart) ([]byte, error) {
	var wbSheets, wbRels, ctSheets strings.Builder
	for i, s := range sheets {
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escAttr.Replace(s.name), i+1, i+1)
		fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`+"\n", i+1, i+1)
		fmt.Fprintf(&ctSheets, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`+"\n", i+1)
	}
	fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`+"\n", len(sheets)+1)

	type part struct{ name, data string }
	parts := []part{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
` + ctSheets.String() + `<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>
</Types>
`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>
`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			wbSheets.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
` + wbRels.String() + `</Relationships>`},
	}
	for i, s := range sheets {
		parts = append(parts, part{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s.xml})
	}
	parts = append(parts, part{"xl/styles.xml", stylesXML})

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, fmt.Errorf("create part %s: %w", p.name, err)
		}
		if _, err := f.Write([]byte(p.data)); err != nil {
			return nil, fmt.Errorf("write part %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close package: %w", err)
	}
	return buf.Bytes(), nil
}

// escAttr escapes attribute values.
var escAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
