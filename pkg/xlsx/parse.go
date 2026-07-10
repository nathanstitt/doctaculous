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

	// Workbook: sheet list, the date system, and defined names.
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
		DefinedNames struct {
			DefinedName []struct {
				Name         string `xml:"name,attr"`
				LocalSheetID *int   `xml:"localSheetId,attr"`
				Hidden       string `xml:"hidden,attr"`
				RefersTo     string `xml:",chardata"`
			} `xml:"definedName"`
		} `xml:"definedNames"`
	}
	if err := xml.Unmarshal(wbData, &wbDoc); err != nil {
		return nil, fmt.Errorf("%w: workbook: %v", ErrNotXLSX, err)
	}

	rels := pkg.relsOf(wbPart)
	shared := parseSharedStrings(partOfType(pkg, wbPart, relSharedStrings))
	styles := parseStyles(partOfType(pkg, wbPart, relStyles))
	date1904 := onOff(wbDoc.Pr.Date1904)

	wb := &Workbook{Date1904: date1904}
	for _, dn := range wbDoc.DefinedNames.DefinedName {
		wb.DefinedNames = append(wb.DefinedNames, DefinedName{
			Name:       dn.Name,
			RefersTo:   strings.TrimSpace(dn.RefersTo),
			LocalSheet: dn.LocalSheetID,
			Hidden:     onOff(dn.Hidden),
		})
	}
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
		switch s.State {
		case "hidden":
			sheet.Visibility = SheetHidden
		case "veryHidden":
			sheet.Visibility = SheetVeryHidden
		}
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
// reader exposes: number format, bold/italic, fill, alignment — plus the full
// resolved Style per xf for structured consumers.
type styleTable struct {
	numFmtByXf []int          // xf index -> numFmtId
	customFmt  map[int]string // custom numFmtId -> format code
	fontByXf   []int          // xf index -> font index
	fillByXf   []int          // xf index -> fill index
	alignByXf  []string       // xf index -> horizontal alignment ("" default)
	fonts      []struct{ b, i bool }
	fills      []string // fill index -> "RRGGBB" or ""
	// resolved is the full Style per xf index, shared (read-only) between
	// every cell referencing the xf.
	resolved []*Style
}

// xmlColor is a styles.xml color element in any of its schemes.
type xmlColor struct {
	RGB     string  `xml:"rgb,attr"`
	Indexed *int    `xml:"indexed,attr"`
	Theme   *int    `xml:"theme,attr"`
	Tint    float64 `xml:"tint,attr"`
	Auto    string  `xml:"auto,attr"`
}

// toColor converts a parsed color element (nil = absent) to the model Color.
func toColor(c *xmlColor) Color {
	if c == nil {
		return Color{}
	}
	return Color{
		RGB:     normalizeRGB(c.RGB),
		Indexed: c.Indexed,
		Theme:   c.Theme,
		Tint:    c.Tint,
		Auto:    onOff(c.Auto),
	}
}

// xmlEdge is one border edge element.
type xmlEdge struct {
	Style string    `xml:"style,attr"`
	Color *xmlColor `xml:"color"`
}

func toEdge(e xmlEdge) Edge { return Edge{Style: e.Style, Color: toColor(e.Color)} }

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
				B      *struct{} `xml:"b"`
				I      *struct{} `xml:"i"`
				Strike *struct{} `xml:"strike"`
				U      *struct {
					Val string `xml:"val,attr"`
				} `xml:"u"`
				Sz *struct {
					Val float64 `xml:"val,attr"`
				} `xml:"sz"`
				Name *struct {
					Val string `xml:"val,attr"`
				} `xml:"name"`
				Color *xmlColor `xml:"color"`
			} `xml:"font"`
		} `xml:"fonts"`
		Fills struct {
			Fill []struct {
				Pattern struct {
					Type    string    `xml:"patternType,attr"`
					FgColor *xmlColor `xml:"fgColor"`
					BgColor *xmlColor `xml:"bgColor"`
				} `xml:"patternFill"`
			} `xml:"fill"`
		} `xml:"fills"`
		Borders struct {
			Border []struct {
				DiagonalUp   string  `xml:"diagonalUp,attr"`
				DiagonalDown string  `xml:"diagonalDown,attr"`
				Left         xmlEdge `xml:"left"`
				Right        xmlEdge `xml:"right"`
				Top          xmlEdge `xml:"top"`
				Bottom       xmlEdge `xml:"bottom"`
				Diagonal     xmlEdge `xml:"diagonal"`
			} `xml:"border"`
		} `xml:"borders"`
		CellXfs struct {
			Xf []struct {
				NumFmtID  int `xml:"numFmtId,attr"`
				FontID    int `xml:"fontId,attr"`
				FillID    int `xml:"fillId,attr"`
				BorderID  int `xml:"borderId,attr"`
				Alignment *struct {
					Horizontal   string `xml:"horizontal,attr"`
					Vertical     string `xml:"vertical,attr"`
					WrapText     string `xml:"wrapText,attr"`
					Indent       int    `xml:"indent,attr"`
					TextRotation int    `xml:"textRotation,attr"`
					ShrinkToFit  string `xml:"shrinkToFit,attr"`
				} `xml:"alignment"`
				Protection *struct {
					Locked *string `xml:"locked,attr"`
					Hidden *string `xml:"hidden,attr"`
				} `xml:"protection"`
			} `xml:"xf"`
		} `xml:"cellXfs"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return st
	}
	for _, nf := range doc.NumFmts.NumFmt {
		st.customFmt[nf.ID] = nf.Code
	}

	fullFonts := make([]Font, len(doc.Fonts.Font))
	for i, f := range doc.Fonts.Font {
		st.fonts = append(st.fonts, struct{ b, i bool }{f.B != nil, f.I != nil})
		ft := Font{Bold: f.B != nil, Italic: f.I != nil, Strike: f.Strike != nil, Color: toColor(f.Color)}
		if f.U != nil {
			ft.Underline = f.U.Val
			if ft.Underline == "" {
				ft.Underline = "single"
			}
		}
		if f.Sz != nil {
			ft.Size = f.Sz.Val
		}
		if f.Name != nil {
			ft.Name = f.Name.Val
		}
		fullFonts[i] = ft
	}

	fullFills := make([]Fill, len(doc.Fills.Fill))
	for i, f := range doc.Fills.Fill {
		rgb := ""
		if f.Pattern.Type == "solid" && f.Pattern.FgColor != nil {
			rgb = normalizeRGB(f.Pattern.FgColor.RGB)
		}
		st.fills = append(st.fills, rgb)
		fullFills[i] = Fill{Pattern: f.Pattern.Type, Fg: toColor(f.Pattern.FgColor), Bg: toColor(f.Pattern.BgColor)}
	}

	fullBorders := make([]Border, len(doc.Borders.Border))
	for i, b := range doc.Borders.Border {
		fullBorders[i] = Border{
			Top: toEdge(b.Top), Right: toEdge(b.Right), Bottom: toEdge(b.Bottom),
			Left: toEdge(b.Left), Diagonal: toEdge(b.Diagonal),
			DiagonalUp: onOff(b.DiagonalUp), DiagonalDown: onOff(b.DiagonalDown),
		}
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

		s := &Style{NumFmtID: xf.NumFmtID, NumFmt: resolveNumFmt(xf.NumFmtID, st.customFmt)}
		if xf.FontID >= 0 && xf.FontID < len(fullFonts) {
			s.Font = fullFonts[xf.FontID]
		}
		if xf.FillID >= 0 && xf.FillID < len(fullFills) {
			s.Fill = fullFills[xf.FillID]
		}
		if xf.BorderID >= 0 && xf.BorderID < len(fullBorders) {
			s.Border = fullBorders[xf.BorderID]
		}
		if xf.Alignment != nil {
			s.Alignment = Alignment{
				Horizontal:   xf.Alignment.Horizontal,
				Vertical:     xf.Alignment.Vertical,
				WrapText:     onOff(xf.Alignment.WrapText),
				Indent:       xf.Alignment.Indent,
				TextRotation: xf.Alignment.TextRotation,
				ShrinkToFit:  onOff(xf.Alignment.ShrinkToFit),
			}
		}
		if xf.Protection != nil {
			p := &Protection{Locked: true} // the OOXML default
			if xf.Protection.Locked != nil {
				p.Locked = onOff(*xf.Protection.Locked)
			}
			if xf.Protection.Hidden != nil {
				p.Hidden = onOff(*xf.Protection.Hidden)
			}
			s.Protection = p
		}
		st.resolved = append(st.resolved, s)
	}
	return st
}

// styleFor returns the resolved Style for an xf index, or nil when out of
// range (no styles part, or a malformed index).
func (st styleTable) styleFor(xf int) *Style {
	if xf < 0 || xf >= len(st.resolved) {
		return nil
	}
	return st.resolved[xf]
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
	// formula is the f element's text (the master text for a shared formula's
	// first cell; "" for shared members, filled by expansion).
	formula string
	// sharedSI is the f element's si group index for t="shared", else -1.
	sharedSI int
}

// sharedMaster records a shared-formula group's defining cell.
type sharedMaster struct {
	row, col int
	src      string
}

// parseSheet streams one worksheet part into a dense Sheet grid, collecting
// the sheet-level facts (merges, panes, dimensions, tab color) along the way.
func parseSheet(data []byte, shared []string, styles styleTable, date1904 bool) Sheet {
	var sheet Sheet
	var cells []rawCell
	maxRow, maxCol := -1, -1
	sharedMasters := map[int]sharedMaster{}

	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur *rawCell
	var inV, inT, inF bool
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch e := tok.(type) {
		case xml.StartElement:
			switch e.Name.Local {
			case "c":
				c := rawCell{row: -1, col: -1, sharedSI: -1}
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
			case "f":
				if cur != nil {
					inF = true
					for _, a := range e.Attr {
						if a.Name.Local == "si" {
							if si, err := strconv.Atoi(a.Value); err == nil {
								cur.sharedSI = si
							}
						}
					}
				}
			case "row":
				parseRowElement(e, &sheet, styles)
			case "col":
				parseColElement(e, &sheet)
			case "pane":
				parsePaneElement(e, &sheet)
			case "sheetFormatPr":
				for _, a := range e.Attr {
					switch a.Name.Local {
					case "defaultRowHeight":
						sheet.DefaultRowHeight, _ = strconv.ParseFloat(a.Value, 64)
					case "defaultColWidth":
						sheet.DefaultColWidth, _ = strconv.ParseFloat(a.Value, 64)
					}
				}
			case "tabColor":
				for _, a := range e.Attr {
					if a.Name.Local == "rgb" {
						sheet.TabColorRGB = normalizeRGB(a.Value)
					}
				}
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
			if cur != nil {
				switch {
				case inF:
					cur.formula += string(e)
				case inV || inT:
					cur.value += string(e)
				}
			}
		case xml.EndElement:
			switch e.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "f":
				inF = false
			case "c":
				if cur != nil && cur.row >= 0 && cur.col >= 0 {
					if cur.sharedSI >= 0 && cur.formula != "" {
						// The group's defining cell carries the master text.
						if _, seen := sharedMasters[cur.sharedSI]; !seen {
							sharedMasters[cur.sharedSI] = sharedMaster{row: cur.row, col: cur.col, src: cur.formula}
						}
					}
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
		// A shared-formula member carries no text of its own: expand the
		// group master with its relative references shifted to this cell.
		if c.formula == "" && c.sharedSI >= 0 {
			if m, ok := sharedMasters[c.sharedSI]; ok {
				c.formula = shiftFormula(m.src, c.row-m.row, c.col-m.col)
			}
		}
		bold, italic, fill, align := styles.cellStyle(c.styleIdx)
		grid[c.row][c.col] = Cell{
			Text:    displayValue(c, shared, styles, date1904),
			Bold:    bold,
			Italic:  italic,
			FillRGB: fill,
			Align:   align,
			Value:   typedValue(c, shared, styles, date1904),
			Formula: c.formula,
			StyleID: c.styleIdx,
			Style:   styles.styleFor(c.styleIdx),
		}
	}
	sheet.Cells = grid
	return sheet
}

// parseRowElement reads a row's explicit height and row-level style.
func parseRowElement(e xml.StartElement, sheet *Sheet, styles styleTable) {
	rowNum := -1
	var ht float64
	hasHt := false
	styleIdx := -1
	customFormat := false
	for _, a := range e.Attr {
		switch a.Name.Local {
		case "r":
			if n, err := strconv.Atoi(a.Value); err == nil {
				rowNum = n - 1
			}
		case "ht":
			if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
				ht, hasHt = f, true
			}
		case "s":
			styleIdx, _ = strconv.Atoi(a.Value)
		case "customFormat":
			customFormat = onOff(a.Value)
		}
	}
	if rowNum < 0 {
		return
	}
	if hasHt {
		if sheet.RowHeights == nil {
			sheet.RowHeights = map[int]float64{}
		}
		sheet.RowHeights[rowNum] = ht
	}
	if customFormat && styleIdx >= 0 {
		if s := styles.styleFor(styleIdx); s != nil {
			if sheet.RowStyles == nil {
				sheet.RowStyles = map[int]*Style{}
			}
			sheet.RowStyles[rowNum] = s
		}
	}
}

// parseColElement expands a <col min..max> width declaration per column.
func parseColElement(e xml.StartElement, sheet *Sheet) {
	minCol, maxCol := -1, -1
	var width float64
	hasWidth := false
	for _, a := range e.Attr {
		switch a.Name.Local {
		case "min":
			if n, err := strconv.Atoi(a.Value); err == nil {
				minCol = n - 1
			}
		case "max":
			if n, err := strconv.Atoi(a.Value); err == nil {
				maxCol = n - 1
			}
		case "width":
			if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
				width, hasWidth = f, true
			}
		}
	}
	if !hasWidth || minCol < 0 || maxCol < minCol {
		return
	}
	// Guard against a min..max covering the full 16384-column sheet: only the
	// used range matters to consumers, but the map must stay bounded.
	if maxCol-minCol > 16383 {
		maxCol = minCol + 16383
	}
	if sheet.ColWidths == nil {
		sheet.ColWidths = map[int]float64{}
	}
	for c := minCol; c <= maxCol; c++ {
		sheet.ColWidths[c] = width
	}
}

// parsePaneElement reads a frozen-pane split.
func parsePaneElement(e xml.StartElement, sheet *Sheet) {
	var x, y int
	frozen := false
	for _, a := range e.Attr {
		switch a.Name.Local {
		case "xSplit":
			if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
				x = int(f)
			}
		case "ySplit":
			if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
				y = int(f)
			}
		case "state":
			frozen = a.Value == "frozen" || a.Value == "frozenSplit"
		}
	}
	if frozen {
		sheet.FrozenCols, sheet.FrozenRows = x, y
	}
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
