package xlsx

// CoreFixture is one canonical XLSX fixture, mirroring the DOCX generator's
// shape so tests range over Core uniformly.
type CoreFixture struct {
	Name  string
	Desc  string
	Build func() []byte
}

// Bytes returns the fixture's .xlsx bytes.
func (f CoreFixture) Bytes() []byte { return f.Build() }

// Core is the canonical fixture set: each locks one path through the reader.
var Core = []CoreFixture{
	{
		Name:  "values",
		Desc:  "shared + inline strings, numbers, booleans, and an error cell",
		Build: valuesXLSX,
	},
	{
		Name:  "dates",
		Desc:  "builtin + custom date/time formats in the 1900 system",
		Build: datesXLSX,
	},
	{
		Name:  "styled",
		Desc:  "bold/italic fonts, a solid fill, and explicit alignment",
		Build: styledXLSX,
	},
	{
		Name:  "merged",
		Desc:  "a colspan block and a rowspan block via mergeCells",
		Build: mergedXLSX,
	},
	{
		Name:  "multisheet",
		Desc:  "two visible sheets and one hidden sheet",
		Build: multisheetXLSX,
	},
}

const sheetOpen = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`
const sheetClose = `</sheetData></worksheet>`

func valuesXLSX() []byte {
	// A1 shared "Name", B1 shared "Qty"; A2 rich-text shared; B2 number;
	// A3 inline string; B3 bool; A4 formula with cached string; B4 error.
	sheet := sheetOpen +
		`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>` +
		`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>42.5</v></c></row>` +
		`<row r="3"><c r="A3" t="inlineStr"><is><t>inline text</t></is></c><c r="B3" t="b"><v>1</v></c></row>` +
		`<row r="4"><c r="A4" t="str"><f>CONCAT("a","b")</f><v>cached result</v></c><c r="B4" t="e"><v>#DIV/0!</v></c></row>` +
		sheetClose
	shared := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
<si><t>Name</t></si><si><t>Qty</t></si><si><r><t>rich </t></r><r><t>text</t></r></si></sst>`
	return New().AddSheet("Values", sheet).SetSharedStrings(shared).Bytes()
}

func datesXLSX() []byte {
	// A1: builtin date (numFmt 14); B1: builtin time (21); A2: custom yyyy-mm-dd
	// (id 164); B2: date-time (22); A3: percent (9); B3: General number.
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<numFmts count="1"><numFmt numFmtId="164" formatCode="yyyy\-mm\-dd"/></numFmts>
<fonts count="1"><font/></fonts>
<fills count="1"><fill><patternFill patternType="none"/></fill></fills>
<cellXfs count="6">
<xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="21"/><xf numFmtId="164"/><xf numFmtId="22"/><xf numFmtId="9"/>
</cellXfs></styleSheet>`
	// Serial 45000 = 2023-03-15; 0.5 = noon; 45000.75 = 18:00.
	sheet := sheetOpen +
		`<row r="1"><c r="A1" s="1"><v>45000</v></c><c r="B1" s="2"><v>0.5</v></c></row>` +
		`<row r="2"><c r="A2" s="3"><v>45000</v></c><c r="B2" s="4"><v>45000.75</v></c></row>` +
		`<row r="3"><c r="A3" s="5"><v>0.25</v></c><c r="B3"><v>1234.5</v></c></row>` +
		sheetClose
	return New().AddSheet("Dates", sheet).SetStyles(styles).Bytes()
}

func styledXLSX() []byte {
	styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="3"><font/><font><b/></font><font><i/></font></fonts>
<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>
<fill><patternFill patternType="solid"><fgColor rgb="FFFFEECC"/></patternFill></fill></fills>
<cellXfs count="4">
<xf numFmtId="0" fontId="0" fillId="0"/>
<xf numFmtId="0" fontId="1" fillId="0"/>
<xf numFmtId="0" fontId="2" fillId="2"/>
<xf numFmtId="0" fontId="0" fillId="0" applyAlignment="1"><alignment horizontal="right"/></xf>
</cellXfs></styleSheet>`
	sheet := sheetOpen +
		`<row r="1"><c r="A1" s="1" t="inlineStr"><is><t>bold header</t></is></c></row>` +
		`<row r="2"><c r="A2" s="2" t="inlineStr"><is><t>italic filled</t></is></c></row>` +
		`<row r="3"><c r="A3" s="3"><v>99</v></c></row>` +
		sheetClose
	return New().AddSheet("Styled", sheet).SetStyles(styles).Bytes()
}

func mergedXLSX() []byte {
	sheet := sheetOpen +
		`<row r="1"><c r="A1" t="inlineStr"><is><t>wide title</t></is></c><c r="C1" t="inlineStr"><is><t>side</t></is></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>tall</t></is></c><c r="B2" t="inlineStr"><is><t>m1</t></is></c><c r="C2" t="inlineStr"><is><t>m2</t></is></c></row>` +
		`<row r="3"><c r="B3" t="inlineStr"><is><t>b1</t></is></c><c r="C3" t="inlineStr"><is><t>b2</t></is></c></row>` +
		`</sheetData><mergeCells count="2"><mergeCell ref="A1:B1"/><mergeCell ref="A2:A3"/></mergeCells></worksheet>`
	return New().AddSheet("Merged", sheet).Bytes()
}

func multisheetXLSX() []byte {
	mk := func(text string) string {
		return sheetOpen + `<row r="1"><c r="A1" t="inlineStr"><is><t>` + text + `</t></is></c></row>` + sheetClose
	}
	return New().
		AddSheet("First", mk("first sheet cell")).
		AddSheet("Second", mk("second sheet cell")).
		AddHiddenSheet("Secrets", mk("hidden cell")).
		Bytes()
}
