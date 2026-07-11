package xlsx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// Relationship type URIs (OPC).
const (
	relOfficeDocument = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relStyles         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relSharedStrings  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"
)

// parseWorkbook locates and parses the workbook and its satellite parts.
func parseWorkbook(pkg *pkgReader) (*Workbook, error) {
	wbPart := "xl/workbook.xml"
	if rel, ok := pkg.firstRelOfType("/", relOfficeDocument); ok {
		if pkg.read(rel.Target) != nil {
			wbPart = rel.Target
		}
	}
	wbData := pkg.read(wbPart)
	if wbData == nil {
		return nil, fmt.Errorf("%w: missing workbook part", ErrNotXLSX)
	}

	// Workbook: sheet list + the date system.
	var wbDoc struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets struct {
			Sheet []struct {
				Name  string `xml:"name,attr"`
				State string `xml:"state,attr"`
				RID   string `xml:"id,attr"` // r:id resolves by local name
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	if err := xml.Unmarshal(wbData, &wbDoc); err != nil {
		return nil, fmt.Errorf("%w: workbook: %v", ErrNotXLSX, err)
	}

	rels := pkg.relsOf(wbPart)
	shared := parseSharedStrings(partOfType(pkg, wbPart, relSharedStrings))
	styles := parseStyles(partOfType(pkg, wbPart, relStyles))
	date1904 := onOff(wbDoc.Pr.Date1904)

	wb := &Workbook{}
	for _, s := range wbDoc.Sheets.Sheet {
		rel, ok := rels[s.RID]
		if !ok {
			continue // a sheet without a resolvable part is skipped
		}
		data := pkg.read(rel.Target)
		if data == nil {
			continue
		}
		sheet := parseSheet(data, shared, styles, date1904)
		sheet.Name = s.Name
		sheet.Hidden = s.State == "hidden" || s.State == "veryHidden"
		wb.Sheets = append(wb.Sheets, sheet)
	}
	return wb, nil
}

// partOfType reads the part behind the first relationship of relType on the
// workbook, falling back to the conventional location.
func partOfType(pkg *pkgReader, wbPart, relType string) []byte {
	if rel, ok := pkg.firstRelOfType(wbPart, relType); ok {
		if data := pkg.read(rel.Target); data != nil {
			return data
		}
	}
	switch relType {
	case relStyles:
		return pkg.read("xl/styles.xml")
	case relSharedStrings:
		return pkg.read("xl/sharedStrings.xml")
	}
	return nil
}

// onOff reads an OOXML boolean attribute (absent = false; "1"/"true" = true).
func onOff(v string) bool { return v == "1" || v == "true" }

// parseSharedStrings extracts the shared-string table: each si is either a
// plain t or rich-text r/t runs, concatenated.
func parseSharedStrings(data []byte) []string {
	if data == nil {
		return nil
	}
	var doc struct {
		SI []struct {
			T *string `xml:"t"`
			R []struct {
				T string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make([]string, len(doc.SI))
	for i, si := range doc.SI {
		if si.T != nil {
			out[i] = *si.T
			continue
		}
		var sb strings.Builder
		for _, r := range si.R {
			sb.WriteString(r.T)
		}
		out[i] = sb.String()
	}
	return out
}

// styleTable resolves a cell's style index (c/@s -> cellXfs) to the facts the
// reader exposes: number format, bold/italic, fill, alignment.
type styleTable struct {
	numFmtByXf []int          // xf index -> numFmtId
	customFmt  map[int]string // custom numFmtId -> format code
	fontByXf   []int          // xf index -> font index
	fillByXf   []int          // xf index -> fill index
	alignByXf  []string       // xf index -> horizontal alignment ("" default)
	fonts      []struct{ b, i bool }
	fills      []string // fill index -> "RRGGBB" or ""
}

// parseStyles reads xl/styles.xml. A nil/absent part yields a zero table
// (every lookup returns defaults).
func parseStyles(data []byte) styleTable {
	var st styleTable
	st.customFmt = map[int]string{}
	if data == nil {
		return st
	}
	var doc struct {
		NumFmts struct {
			NumFmt []struct {
				ID   int    `xml:"numFmtId,attr"`
				Code string `xml:"formatCode,attr"`
			} `xml:"numFmt"`
		} `xml:"numFmts"`
		Fonts struct {
			Font []struct {
				B *struct{} `xml:"b"`
				I *struct{} `xml:"i"`
			} `xml:"font"`
		} `xml:"fonts"`
		Fills struct {
			Fill []struct {
				Pattern struct {
					Type    string `xml:"patternType,attr"`
					FgColor struct {
						RGB string `xml:"rgb,attr"`
					} `xml:"fgColor"`
				} `xml:"patternFill"`
			} `xml:"fill"`
		} `xml:"fills"`
		CellXfs struct {
			Xf []struct {
				NumFmtID  int `xml:"numFmtId,attr"`
				FontID    int `xml:"fontId,attr"`
				FillID    int `xml:"fillId,attr"`
				Alignment *struct {
					Horizontal string `xml:"horizontal,attr"`
				} `xml:"alignment"`
			} `xml:"xf"`
		} `xml:"cellXfs"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return st
	}
	for _, nf := range doc.NumFmts.NumFmt {
		st.customFmt[nf.ID] = nf.Code
	}
	for _, f := range doc.Fonts.Font {
		st.fonts = append(st.fonts, struct{ b, i bool }{f.B != nil, f.I != nil})
	}
	for _, f := range doc.Fills.Fill {
		rgb := ""
		if f.Pattern.Type == "solid" {
			rgb = normalizeRGB(f.Pattern.FgColor.RGB)
		}
		st.fills = append(st.fills, rgb)
	}
	for _, xf := range doc.CellXfs.Xf {
		st.numFmtByXf = append(st.numFmtByXf, xf.NumFmtID)
		st.fontByXf = append(st.fontByXf, xf.FontID)
		st.fillByXf = append(st.fillByXf, xf.FillID)
		align := ""
		if xf.Alignment != nil {
			switch xf.Alignment.Horizontal {
			case "left", "center", "right":
				align = xf.Alignment.Horizontal
			}
		}
		st.alignByXf = append(st.alignByXf, align)
	}
	return st
}

// normalizeRGB reduces an ARGB/RGB hex to "RRGGBB" (uppercased), or "".
func normalizeRGB(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) == 8 { // ARGB
		v = v[2:]
	}
	if len(v) != 6 {
		return ""
	}
	return v
}

// numFmtID resolves a cell's style index to its number-format id (0 = General).
func (st styleTable) numFmtID(xf int) int {
	if xf < 0 || xf >= len(st.numFmtByXf) {
		return 0
	}
	return st.numFmtByXf[xf]
}

// cellStyle resolves the presentation facts for a style index.
func (st styleTable) cellStyle(xf int) (bold, italic bool, fill, align string) {
	if xf < 0 || xf >= len(st.fontByXf) {
		return false, false, "", ""
	}
	if fi := st.fontByXf[xf]; fi >= 0 && fi < len(st.fonts) {
		bold, italic = st.fonts[fi].b, st.fonts[fi].i
	}
	if fi := st.fillByXf[xf]; fi >= 0 && fi < len(st.fills) {
		fill = st.fills[fi]
	}
	return bold, italic, fill, st.alignByXf[xf]
}

// rawCell is one c element before value resolution.
type rawCell struct {
	row, col int
	typ      string // "" (numeric), s, str, inlineStr, b, e
	value    string
	styleIdx int
}

// parseSheet streams one worksheet part into a dense Sheet grid.
func parseSheet(data []byte, shared []string, styles styleTable, date1904 bool) Sheet {
	var sheet Sheet
	var cells []rawCell
	maxRow, maxCol := -1, -1

	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur *rawCell
	var inV, inT bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch e := tok.(type) {
		case xml.StartElement:
			switch e.Name.Local {
			case "c":
				c := rawCell{row: -1, col: -1}
				for _, a := range e.Attr {
					switch a.Name.Local {
					case "r":
						c.row, c.col = parseRef(a.Value)
					case "t":
						c.typ = a.Value
					case "s":
						c.styleIdx, _ = strconv.Atoi(a.Value)
					}
				}
				cur = &c
			case "v":
				inV = cur != nil
			case "t":
				// Text inside an inlineStr (c > is > t) or a rich run (c > is > r > t).
				inT = cur != nil && cur.typ == "inlineStr"
			case "mergeCell":
				for _, a := range e.Attr {
					if a.Name.Local == "ref" {
						if m, ok := parseMergeRef(a.Value); ok {
							sheet.Merges = append(sheet.Merges, m)
						}
					}
				}
			}
		case xml.CharData:
			if cur != nil && (inV || inT) {
				cur.value += string(e)
			}
		case xml.EndElement:
			switch e.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				if cur != nil && cur.row >= 0 && cur.col >= 0 {
					cells = append(cells, *cur)
					if cur.row > maxRow {
						maxRow = cur.row
					}
					if cur.col > maxCol {
						maxCol = cur.col
					}
				}
				cur = nil
			}
		}
	}

	if maxRow < 0 || maxCol < 0 {
		return sheet
	}
	grid := make([][]Cell, maxRow+1)
	for i := range grid {
		grid[i] = make([]Cell, maxCol+1)
	}
	for _, c := range cells {
		bold, italic, fill, align := styles.cellStyle(c.styleIdx)
		grid[c.row][c.col] = Cell{
			Text:    displayValue(c, shared, styles, date1904),
			Bold:    bold,
			Italic:  italic,
			FillRGB: fill,
			Align:   align,
		}
	}
	sheet.Cells = grid
	return sheet
}

// parseRef converts an A1-style reference to zero-based (row, col); (-1, -1)
// when malformed.
func parseRef(ref string) (row, col int) {
	i := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A'+1)
		i++
	}
	if i == 0 || i == len(ref) {
		return -1, -1
	}
	n, err := strconv.Atoi(ref[i:])
	if err != nil || n < 1 {
		return -1, -1
	}
	return n - 1, col - 1
}

// parseMergeRef converts an "A1:B2" range into a Merge.
func parseMergeRef(ref string) (Merge, bool) {
	colon := strings.IndexByte(ref, ':')
	if colon < 0 {
		return Merge{}, false
	}
	r1, c1 := parseRef(ref[:colon])
	r2, c2 := parseRef(ref[colon+1:])
	if r1 < 0 || r2 < r1 || c1 < 0 || c2 < c1 {
		return Merge{}, false
	}
	return Merge{Row: r1, Col: c1, RowSpan: r2 - r1 + 1, ColSpan: c2 - c1 + 1}, true
}
