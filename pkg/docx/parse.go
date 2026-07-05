package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"io"
	"path"
	"strconv"
	"strings"
)

// wNS is the WordprocessingML main namespace. encoding/xml resolves prefixes to
// namespaces, so we match on Space rather than the "w:" prefix (a producer may
// use a different prefix for the same namespace).
const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// parsePackage parses the main document and (if present) the styles part into a
// Document. Styles are resolved relative to the main document's relationships,
// falling back to the conventional word/styles.xml.
func parsePackage(pkg *pkgReader) (*Document, error) {
	mainName, err := pkg.mainDocumentPart()
	if err != nil {
		return nil, err
	}
	mainData, ok := pkg.part(mainName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingPart, mainName)
	}

	doc := &Document{Section: defaultSection()}
	if err := parseDocument(mainData, doc); err != nil {
		return nil, err
	}

	// Styles part: prefer the relationship target, fall back to the convention.
	stylesName := resolveStylesPart(pkg, mainName)
	if data, ok := pkg.part(stylesName); ok {
		styles, err := parseStyles(data)
		if err != nil {
			return nil, err
		}
		doc.Styles = styles
	}

	// Numbering part: prefer the relationship target, fall back to the convention.
	numName := resolveNumberingPart(pkg, mainName)
	if data, ok := pkg.part(numName); ok {
		num, err := parseNumbering(data)
		if err != nil {
			return nil, err
		}
		doc.Numbering = num
	}
	return doc, nil
}

// resolveStylesPart finds the styles part name via the main document's
// relationships, falling back to word/styles.xml.
func resolveStylesPart(pkg *pkgReader, mainName string) string {
	const stylesType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	rels := pkg.relsForByType(mainName, stylesType)
	if rels != "" {
		return rels
	}
	return "word/styles.xml"
}

// resolveNumberingPart finds the numbering part name via the main document's
// relationships, falling back to word/numbering.xml.
func resolveNumberingPart(pkg *pkgReader, mainName string) string {
	const numType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	rels := pkg.relsForByType(mainName, numType)
	if rels != "" {
		return rels
	}
	return "word/numbering.xml"
}

// relsForByType returns the first relationship target of the given type for a
// source part, or "" if none.
func (p *pkgReader) relsForByType(partName, relType string) string {
	partName = cleanPart(partName)
	dir, base := splitPart(partName)
	relsName := joinPart(dir, "_rels", base+".rels")
	data, ok := p.part(relsName)
	if !ok {
		return ""
	}
	var doc struct {
		Rels []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	for _, r := range doc.Rels {
		if r.Type == relType {
			return joinPart(dir, r.Target)
		}
	}
	return ""
}

// parseDocument walks word/document.xml, filling the body blocks and the
// body-level section properties.
func parseDocument(data []byte, doc *Document) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("%w: document: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space == wNS && se.Name.Local == "body" {
			if err := parseBody(dec, doc); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseBody consumes the children of w:body until its end element.
func parseBody(dec *xml.Decoder, doc *Document) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: body: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return fmt.Errorf("%w: body: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "sectPr":
				sect, err := parseSectPr(dec)
				if err != nil {
					return err
				}
				doc.Section = sect
			default:
				blk, sect, err := parseBlockChild(dec, t)
				if err != nil {
					return err
				}
				if blk != nil {
					doc.Body = append(doc.Body, *blk)
				}
				if sect != nil {
					doc.Section = *sect
				}
			}
		case xml.EndElement:
			if t.Name.Space == wNS && t.Name.Local == "body" {
				return nil
			}
		}
	}
}

// parseBlockChild dispatches a block-level start element (w:p or w:tbl) shared by
// the body and by table cells. It returns the parsed block (nil for an element it
// skips) and any sectPr found in a paragraph's pPr (a section boundary; nil in a
// cell context). start is the already-read start element.
func parseBlockChild(dec *xml.Decoder, start xml.StartElement) (*Block, *SectionProps, error) {
	switch start.Name.Local {
	case "p":
		p, sect, err := parseParagraph(dec)
		if err != nil {
			return nil, nil, err
		}
		return &Block{Paragraph: p}, sect, nil
	case "tbl":
		tb, err := parseTbl(dec)
		if err != nil {
			return nil, nil, err
		}
		return &Block{Table: tb}, nil, nil
	default:
		if err := dec.Skip(); err != nil {
			return nil, nil, fmt.Errorf("%w: block: %v", ErrMalformedXML, err)
		}
		return nil, nil, nil
	}
}

// parseParagraph consumes a w:p, returning the paragraph and any sectPr found in
// its pPr (which marks a section boundary).
func parseParagraph(dec *xml.Decoder) (*Paragraph, *SectionProps, error) {
	p := &Paragraph{}
	var sect *SectionProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: p: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: p: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "pPr":
				props, s, err := parsePPr(dec)
				if err != nil {
					return nil, nil, err
				}
				p.Props = props
				sect = s
			case "r":
				runs, err := parseRun(dec)
				if err != nil {
					return nil, nil, err
				}
				for i := range runs {
					// Copy into a fresh local: &runs[i] would alias parseRun's slice.
					r := runs[i]
					p.Content = append(p.Content, ParaChild{Run: &r})
				}
			default:
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: p: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				return p, sect, nil
			}
		}
	}
}

// parsePPr consumes a w:pPr, returning paragraph properties and any nested
// sectPr.
func parsePPr(dec *xml.Decoder) (ParagraphProps, *SectionProps, error) {
	var props ParagraphProps
	var sect *SectionProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, nil, fmt.Errorf("%w: pPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				applyPPrChild(&props, t)
				switch t.Name.Local {
				case "sectPr":
					s, err := parseSectPr(dec)
					if err != nil {
						return props, nil, err
					}
					sect = &s
					continue
				case "numPr":
					applyNumPr(&props, dec)
					continue
				}
			}
			if err := dec.Skip(); err != nil {
				return props, nil, fmt.Errorf("%w: pPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "pPr" {
				return props, sect, nil
			}
		}
	}
}

// applyPPrchild applies a single direct paragraph-property element. Elements with
// further children (sectPr) are handled by the caller; the rest are self-closing
// and fully described by their attributes.
func applyPPrChild(props *ParagraphProps, e xml.StartElement) {
	switch e.Name.Local {
	case "pStyle":
		props.StyleID = wVal(e)
	case "jc":
		props.Justify = parseJustify(wVal(e))
		props.HasJustify = true
	case "pageBreakBefore":
		props.PageBreakBefore = parseOnOff(wVal(e))
	case "spacing":
		applySpacing(props, e)
	case "ind":
		applyIndent(props, e)
	}
}

// applyNumPr reads a w:numPr's w:ilvl and w:numId children into the paragraph's
// list membership. A numPr with a numId (even without an explicit ilvl, which
// defaults to 0) marks the paragraph as a list item.
func applyNumPr(props *ParagraphProps, dec *xml.Decoder) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "ilvl":
					if v, ok := wAttrInt(t, "val"); ok {
						props.ILvl = v
					}
				case "numId":
					if v, ok := wAttrInt(t, "val"); ok {
						props.NumID = v
						props.HasNum = true
					}
				}
			}
			_ = dec.Skip()
		case xml.EndElement:
			if t.Name.Local == "numPr" {
				return
			}
		}
	}
}

// applySpacing reads w:spacing before/after/line/lineRule.
func applySpacing(props *ParagraphProps, e xml.StartElement) {
	if v, ok := wAttrInt(e, "before"); ok {
		props.SpacingBefore = Twips(v)
		props.HasSpacingBefore = true
	}
	if v, ok := wAttrInt(e, "after"); ok {
		props.SpacingAfter = Twips(v)
		props.HasSpacingAfter = true
	}
	if v, ok := wAttrInt(e, "line"); ok {
		props.Line = Twips(v)
		props.HasLine = true
		props.LineRule = LineRuleAuto
	}
	if rule, ok := wAttr(e, "lineRule"); ok {
		switch rule {
		case "exact":
			props.LineRule = LineRuleExact
		case "atLeast":
			props.LineRule = LineRuleAtLeast
		default:
			props.LineRule = LineRuleAuto
		}
	}
}

// applyIndent reads w:ind left/right/firstLine/hanging.
func applyIndent(props *ParagraphProps, e xml.StartElement) {
	if v, ok := wAttrInt(e, "left"); ok {
		props.IndentLeft = Twips(v)
		props.HasIndentLeft = true
	}
	if v, ok := wAttrInt(e, "right"); ok {
		props.IndentRight = Twips(v)
		props.HasIndentRight = true
	}
	if v, ok := wAttrInt(e, "firstLine"); ok {
		props.FirstLine = Twips(v)
		props.HasFirstLine = true
	}
	if v, ok := wAttrInt(e, "hanging"); ok {
		// A hanging indent pulls the first line left of the block indent.
		props.FirstLine = Twips(-v)
		props.HasFirstLine = true
	}
}

// parseRun consumes a w:r. A run may yield more than one logical run when it
// contains a break: the text before/around the break and the break itself are
// modeled as runs sharing the same properties so the layout engine sees them in
// order.
func parseRun(dec *xml.Decoder) ([]Run, error) {
	var props RunProps
	var sb strings.Builder
	var out []Run
	flushText := func() {
		if sb.Len() > 0 {
			out = append(out, Run{Props: props, Text: sb.String()})
			sb.Reset()
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "rPr":
				props, err = parseRPr(dec)
				if err != nil {
					return nil, err
				}
			case "t":
				text, err := parseText(dec, t)
				if err != nil {
					return nil, err
				}
				sb.WriteString(text)
			case "tab":
				sb.WriteByte('\t')
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			case "br":
				flushText()
				out = append(out, Run{Props: props, Break: parseBreak(t)})
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			default:
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "r" {
				flushText()
				return out, nil
			}
		}
	}
}

// parseText reads the character data of a w:t verbatim. We deliberately do not
// trim regardless of xml:space: that attribute is Word's signal that whitespace
// is significant, but even without it a <w:t>'s character data is the run's
// content (producers do not indent inside <w:t>), so trimming here would drop
// spaces that separate adjacent runs. The attribute is therefore not consulted.
func parseText(dec *xml.Decoder, _ xml.StartElement) (string, error) {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("%w: t: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.WriteString(string(t))
		case xml.EndElement:
			if t.Name.Local == "t" {
				return sb.String(), nil
			}
		}
	}
}

// parseRPr consumes a w:rPr into RunProps.
func parseRPr(dec *xml.Decoder) (RunProps, error) {
	var props RunProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: rPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				applyRPrChild(&props, t)
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: rPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "rPr" {
				return props, nil
			}
		}
	}
}

// applyRPrChild applies one direct run-property element.
func applyRPrChild(props *RunProps, e xml.StartElement) {
	switch e.Name.Local {
	case "b":
		props.Bold = parseOnOff(wVal(e))
		props.HasBold = true
	case "i":
		props.Italic = parseOnOff(wVal(e))
		props.HasItalic = true
	case "u":
		// Any underline val other than "none" turns underline on.
		props.Underline = wVal(e) != "none" && wVal(e) != ""
		if wVal(e) == "" {
			// <w:u/> with no val still means underline on for some producers; but the
			// schema requires w:val, so treat empty as off to avoid false positives.
			props.Underline = false
		}
		props.HasUnderline = true
	case "sz":
		if v, ok := wAttrInt(e, "val"); ok {
			props.SizeHalfPts = v
			props.HasSize = true
		}
	case "color":
		if c, ok := parseColor(wVal(e)); ok {
			props.Color = c
			props.HasColor = true
		}
	case "rFonts":
		if v, ok := wAttr(e, "ascii"); ok && v != "" {
			props.Family = v
		} else if v, ok := wAttr(e, "hAnsi"); ok && v != "" {
			props.Family = v
		}
	}
}

// parseTbl consumes a w:tbl into a Table: its grid, rows, and (Task 2.2) props.
func parseTbl(dec *xml.Decoder) (*Table, error) {
	tb := &Table{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "tblGrid":
				grid, err := parseTblGrid(dec)
				if err != nil {
					return nil, err
				}
				tb.Grid = grid
			case "tblPr":
				props, err := parseTblPr(dec)
				if err != nil {
					return nil, err
				}
				tb.Props = props
			case "tr":
				row, err := parseTr(dec)
				if err != nil {
					return nil, err
				}
				tb.Rows = append(tb.Rows, row)
			default:
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: tbl: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tbl" {
				return tb, nil
			}
		}
	}
}

// parseTblGrid reads the w:gridCol widths of a w:tblGrid.
func parseTblGrid(dec *xml.Decoder) ([]Twips, error) {
	var grid []Twips
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: tblGrid: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "gridCol" {
				if v, ok := wAttrInt(t, "w"); ok {
					grid = append(grid, Twips(v))
				}
			}
			if err := dec.Skip(); err != nil {
				return nil, fmt.Errorf("%w: tblGrid: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "tblGrid" {
				return grid, nil
			}
		}
	}
}

// parseTr consumes a w:tr into a TableRow.
func parseTr(dec *xml.Decoder) (TableRow, error) {
	var row TableRow
	for {
		tok, err := dec.Token()
		if err != nil {
			return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "trPr":
				props, err := parseTrPr(dec)
				if err != nil {
					return row, err
				}
				row.Props = props
			case "tc":
				cell, err := parseTc(dec)
				if err != nil {
					return row, err
				}
				row.Cells = append(row.Cells, cell)
			default:
				if err := dec.Skip(); err != nil {
					return row, fmt.Errorf("%w: tr: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tr" {
				return row, nil
			}
		}
	}
}

// parseTc consumes a w:tc into a TableCell, recursing into its block content.
func parseTc(dec *xml.Decoder) (TableCell, error) {
	cell := TableCell{GridSpan: 1}
	for {
		tok, err := dec.Token()
		if err != nil {
			return cell, fmt.Errorf("%w: tc: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return cell, fmt.Errorf("%w: tc: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "tcPr":
				props, span, vmerge, err := parseTcPr(dec)
				if err != nil {
					return cell, err
				}
				cell.Props = props
				if span > 0 {
					cell.GridSpan = span
				}
				cell.VMerge = vmerge
			default:
				blk, _, err := parseBlockChild(dec, t)
				if err != nil {
					return cell, err
				}
				if blk != nil {
					cell.Blocks = append(cell.Blocks, *blk)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "tc" {
				return cell, nil
			}
		}
	}
}

// parseTblPr reads w:tblPr: table width (w:tblW), alignment (w:jc), and borders
// (w:tblBorders) / shading (w:shd).
func parseTblPr(dec *xml.Decoder) (TableProps, error) {
	var props TableProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: tblPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "tblW":
					applyTblW(&props.WidthDxa, &props.WidthPct, t)
				case "jc":
					props.Justify = parseJustify(wVal(t))
				case "tblBorders":
					b, err := parseBorders(dec, "tblBorders")
					if err != nil {
						return props, err
					}
					props.Borders = b
					continue
				case "shd":
					props.Shading = parseShd(t)
				}
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: tblPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "tblPr" {
				return props, nil
			}
		}
	}
}

// parseTrPr reads w:trPr: the header-row flag (w:tblHeader) and row height
// (w:trHeight).
func parseTrPr(dec *xml.Decoder) (RowProps, error) {
	var props RowProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return props, fmt.Errorf("%w: trPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "tblHeader":
					props.IsHeader = parseOnOff(wVal(t))
				case "trHeight":
					if v, ok := wAttrInt(t, "val"); ok {
						props.HeightDxa = Twips(v)
					}
				}
			}
			if err := dec.Skip(); err != nil {
				return props, fmt.Errorf("%w: trPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "trPr" {
				return props, nil
			}
		}
	}
}

// parseTcPr consumes a w:tcPr, returning cell props plus the gridSpan and vMerge
// state.
func parseTcPr(dec *xml.Decoder) (props CellProps, gridSpan int, vmerge VMergeKind, err error) {
	for {
		tok, terr := dec.Token()
		if terr != nil {
			return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, terr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "gridSpan":
					if v, ok := wAttrInt(t, "val"); ok {
						gridSpan = v
					}
				case "vMerge":
					vmerge = parseVMerge(t)
				case "tcW":
					// Cells carry only a dxa width in the model; a pct cell width is dropped.
					var pct int
					applyTblW(&props.WidthDxa, &pct, t)
				case "vAlign":
					props.VAlign = parseVAlign(wVal(t))
				case "tcBorders":
					b, berr := parseBorders(dec, "tcBorders")
					if berr != nil {
						return props, gridSpan, vmerge, berr
					}
					props.Borders = b
					continue
				case "shd":
					props.Shading = parseShd(t)
				}
			}
			if serr := dec.Skip(); serr != nil {
				return props, gridSpan, vmerge, fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, serr)
			}
		case xml.EndElement:
			if t.Name.Local == "tcPr" {
				return props, gridSpan, vmerge, nil
			}
		}
	}
}

// applyTblW reads a w:tblW / w:tcW measurement. type="dxa" is twips; type="pct"
// is fiftieths of a percent. Only one of dxa/pct is set.
func applyTblW(dxa *Twips, pct *int, e xml.StartElement) {
	typ, _ := wAttr(e, "type")
	v, ok := wAttrInt(e, "w")
	if !ok {
		return
	}
	switch typ {
	case "pct":
		*pct = v
	default: // "dxa" or unspecified
		*dxa = Twips(v)
	}
}

// parseVAlign maps a w:vAlign value to a CellVAlign.
func parseVAlign(val string) CellVAlign {
	switch val {
	case "center":
		return VAlignCenter
	case "bottom":
		return VAlignBottom
	default:
		return VAlignTop
	}
}

// parseShd reads a w:shd fill into a Shading. fill="auto" or absent yields no
// fill (HasFill false).
func parseShd(e xml.StartElement) Shading {
	fill, _ := wAttr(e, "fill")
	if c, ok := parseColor(fill); ok {
		return Shading{Fill: c, HasFill: true}
	}
	return Shading{}
}

// parseBorders reads a w:tblBorders / w:tcBorders element's four edges. name is
// the wrapping element's local name (so the loop knows its end tag).
func parseBorders(dec *xml.Decoder, name string) (BoxBorders, error) {
	var b BoxBorders
	for {
		tok, err := dec.Token()
		if err != nil {
			return b, fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				border := parseBorder(t)
				switch t.Name.Local {
				case "top":
					b.Top = border
				case "bottom":
					b.Bottom = border
				case "left", "start":
					b.Left = border
				case "right", "end":
					b.Right = border
				}
			}
			if err := dec.Skip(); err != nil {
				return b, fmt.Errorf("%w: %s: %v", ErrMalformedXML, name, err)
			}
		case xml.EndElement:
			if t.Name.Local == name {
				return b, nil
			}
		}
	}
}

// parseBorder reads one border edge element (w:sz eighths-of-a-point, w:color,
// w:val style). val="nil"/"none" marks the edge as no-border.
func parseBorder(e xml.StartElement) Border {
	var bd Border
	if v := wVal(e); v == "nil" || v == "none" {
		bd.None = true
	}
	if v, ok := wAttrInt(e, "sz"); ok {
		bd.SizeEighthPt = v
	}
	if c, ok := parseColor(wColor(e)); ok {
		bd.Color = c
		bd.HasColor = true
	}
	return bd
}

// wColor returns the w:color attribute value, or "" if absent.
func wColor(e xml.StartElement) string {
	v, _ := wAttr(e, "color")
	return v
}

// parseVMerge maps a w:vMerge element to a VMergeKind. A bare w:vMerge with no val
// (or val="restart") begins a merge; val="continue" continues it.
func parseVMerge(e xml.StartElement) VMergeKind {
	switch wVal(e) {
	case "continue":
		return VMergeContinue
	default: // "restart" or empty
		return VMergeRestart
	}
}

// parseSectPr consumes a w:sectPr into SectionProps, starting from Letter
// defaults and overriding declared fields.
func parseSectPr(dec *xml.Decoder) (SectionProps, error) {
	sect := defaultSection()
	for {
		tok, err := dec.Token()
		if err != nil {
			return sect, fmt.Errorf("%w: sectPr: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "pgSz":
					if v, ok := wAttrInt(t, "w"); ok {
						sect.PageW = Twips(v)
					}
					if v, ok := wAttrInt(t, "h"); ok {
						sect.PageH = Twips(v)
					}
				case "pgMar":
					applyPgMar(&sect, t)
				}
			}
			if err := dec.Skip(); err != nil {
				return sect, fmt.Errorf("%w: sectPr: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "sectPr" {
				return sect, nil
			}
		}
	}
}

// applyPgMar reads w:pgMar margins.
func applyPgMar(sect *SectionProps, e xml.StartElement) {
	if v, ok := wAttrInt(e, "top"); ok {
		sect.MarginTop = Twips(v)
	}
	if v, ok := wAttrInt(e, "bottom"); ok {
		sect.MarginBottom = Twips(v)
	}
	if v, ok := wAttrInt(e, "left"); ok {
		sect.MarginLeft = Twips(v)
	}
	if v, ok := wAttrInt(e, "right"); ok {
		sect.MarginRight = Twips(v)
	}
	if v, ok := wAttrInt(e, "header"); ok {
		sect.Header = Twips(v)
	}
	if v, ok := wAttrInt(e, "footer"); ok {
		sect.Footer = Twips(v)
	}
	if v, ok := wAttrInt(e, "gutter"); ok {
		sect.Gutter = Twips(v)
	}
}

// --- small attribute helpers -------------------------------------------------

// wAttr returns the value of a w-namespaced attribute by local name. OOXML
// attributes are usually w-namespaced (w:val), but some producers emit them
// unqualified; match either.
func wAttr(e xml.StartElement, local string) (string, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == local && (a.Name.Space == wNS || a.Name.Space == "") {
			return a.Value, true
		}
	}
	return "", false
}

// wVal returns the w:val attribute, or "" if absent.
func wVal(e xml.StartElement) string {
	v, _ := wAttr(e, "val")
	return v
}

// wAttrInt returns an integer-valued w attribute.
func wAttrInt(e xml.StartElement, local string) (int, bool) {
	s, ok := wAttr(e, local)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseOnOff interprets a w toggle value. Per the schema, absent val means "on";
// "false"/"0"/"off"/"no" mean off.
func parseOnOff(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "", "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// parseJustify maps a w:jc value to a Justify.
func parseJustify(val string) Justify {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "center":
		return JustifyCenter
	case "right", "end":
		return JustifyRight
	case "both", "distribute":
		return JustifyBoth
	default:
		return JustifyLeft
	}
}

// parseColor parses an RRGGBB hex color. "auto" (or any unparseable value) yields
// ok=false so the cascade keeps the inherited color.
func parseColor(val string) (color.RGBA, bool) {
	val = strings.TrimSpace(val)
	if len(val) != 6 {
		return color.RGBA{}, false
	}
	n, err := strconv.ParseUint(val, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{
		R: uint8(n >> 16),
		G: uint8(n >> 8),
		B: uint8(n),
		A: 0xff,
	}, true
}

// parseBreak maps a w:br type to a BreakKind.
func parseBreak(e xml.StartElement) BreakKind {
	switch wAttrType(e) {
	case "page":
		return BreakPage
	case "column":
		return BreakColumn
	default:
		return BreakLine
	}
}

// wAttrType returns the w:type attribute of an element.
func wAttrType(e xml.StartElement) string {
	v, _ := wAttr(e, "type")
	return v
}

// splitPart splits a part name into directory (with trailing slash) and base.
func splitPart(name string) (dir, base string) {
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return "", name
	}
	return name[:i+1], name[i+1:]
}

// joinPart joins package path segments with "/" and cleans "." / ".." segments,
// relative to the package root. Package parts always use forward slashes.
func joinPart(elems ...string) string {
	return cleanPart(path.Clean(path.Join(elems...)))
}
