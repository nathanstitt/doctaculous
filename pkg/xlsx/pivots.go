package xlsx

import (
	"encoding/xml"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/nathanstitt/doctaculous/pkg/xlsx/internal/xmlpart"
)

// PivotTable is one pivot table: its source data, placement, and field
// layout. The cache carries refreshOnLoad, so a consumer recomputes values on
// open — the definition, not computed results, is what round-trips.
type PivotTable struct {
	// Name is the pivot table's display name.
	Name string
	// SourceSheet/SourceRange locate the source data (the range's first row
	// is the field-name header row).
	SourceSheet string
	SourceRange string
	// TargetSheet/Location place the pivot (Location is the anchor range).
	TargetSheet string
	Location    string
	// Rows/Cols/Filters are the axis fields by source-column name.
	Rows, Cols, Filters []string
	// Values are the aggregated data fields.
	Values []PivotValueField
	// RowGrandTotals/ColGrandTotals toggle the grand-total rows/columns.
	RowGrandTotals, ColGrandTotals bool
	// StyleName is the pivot style ("PivotStyleLight16" when empty on write).
	StyleName string
}

// PivotValueField is one aggregated field.
type PivotValueField struct {
	// Field is the source-column name.
	Field string
	// Aggregation is the subtotal function ("sum", "count", "average", "max",
	// "min", "product", "countNums", "stdDev", "stdDevp", "var", "varp";
	// "sum" when empty).
	Aggregation string
	// DisplayName is the data field's shown name ("Sum of X" style; derived
	// when empty).
	DisplayName string
}

const (
	relPivotTable      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotTable"
	relPivotCacheDef   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheDefinition"
	relPivotCacheRecs  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheRecords"
	ctPivotTable       = "application/vnd.openxmlformats-officedocument.spreadsheetml.pivotTable+xml"
	ctPivotCacheDef    = "application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheDefinition+xml"
	ctPivotCacheRecs   = "application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheRecords+xml"
	defaultPivotStyle  = "PivotStyleLight16"
	spreadsheetMainXNS = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	relationshipsXNS   = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
)

// sheetRelTargets resolves EVERY relationship of relType on a sheet part.
func (f *File) sheetRelTargets(sheetPart, relType string) []string {
	data := f.rawPartCurrent(sheetRelsName(sheetPart))
	if data == nil {
		return nil
	}
	var doc struct {
		Rels []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []string
	for _, r := range doc.Rels {
		if r.Type == relType {
			out = append(out, path.Clean(path.Join(path.Dir(sheetPart), r.Target)))
		}
	}
	return out
}

// PivotTables reads every sheet's pivot definitions.
func (f *File) PivotTables() []PivotTable {
	var out []PivotTable
	for _, name := range f.SheetNames() {
		sh, err := f.Sheet(name)
		if err != nil {
			continue
		}
		for _, ptPart := range f.sheetRelTargets(sh.partName, relPivotTable) {
			if pt, ok := f.readPivotTable(ptPart, name); ok {
				out = append(out, pt)
			}
		}
	}
	return out
}

// readPivotTable decodes one pivotTable part plus its cache definition.
func (f *File) readPivotTable(partName, targetSheet string) (PivotTable, bool) {
	data := f.rawPartCurrent(partName)
	if data == nil {
		return PivotTable{}, false
	}
	var doc struct {
		Name           string `xml:"name,attr"`
		CacheID        int    `xml:"cacheId,attr"`
		RowGrandTotals string `xml:"rowGrandTotals,attr"`
		ColGrandTotals string `xml:"colGrandTotals,attr"`
		Location       struct {
			Ref string `xml:"ref,attr"`
		} `xml:"location"`
		PivotFields struct {
			Field []struct {
				Axis string `xml:"axis,attr"`
			} `xml:"pivotField"`
		} `xml:"pivotFields"`
		RowFields struct {
			Field []struct {
				X int `xml:"x,attr"`
			} `xml:"field"`
		} `xml:"rowFields"`
		ColFields struct {
			Field []struct {
				X int `xml:"x,attr"`
			} `xml:"field"`
		} `xml:"colFields"`
		PageFields struct {
			Field []struct {
				Fld int `xml:"fld,attr"`
			} `xml:"pageField"`
		} `xml:"pageFields"`
		DataFields struct {
			Field []struct {
				Name     string `xml:"name,attr"`
				Fld      int    `xml:"fld,attr"`
				Subtotal string `xml:"subtotal,attr"`
			} `xml:"dataField"`
		} `xml:"dataFields"`
		Style struct {
			Name string `xml:"name,attr"`
		} `xml:"pivotTableStyleInfo"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return PivotTable{}, false
	}

	pt := PivotTable{
		Name:        doc.Name,
		TargetSheet: targetSheet,
		Location:    doc.Location.Ref,
		StyleName:   doc.Style.Name,
		// The OOXML default for the grand-total attributes is TRUE.
		RowGrandTotals: doc.RowGrandTotals == "" || onOff(doc.RowGrandTotals),
		ColGrandTotals: doc.ColGrandTotals == "" || onOff(doc.ColGrandTotals),
	}

	// The cache definition supplies the source range and the field names.
	fields := []string{}
	if cachePart, ok := f.cacheDefFor(partName, doc.CacheID); ok {
		if cd := f.rawPartCurrent(cachePart); cd != nil {
			var cache struct {
				Source struct {
					WS struct {
						Ref   string `xml:"ref,attr"`
						Sheet string `xml:"sheet,attr"`
					} `xml:"worksheetSource"`
				} `xml:"cacheSource"`
				Fields struct {
					Field []struct {
						Name string `xml:"name,attr"`
					} `xml:"cacheField"`
				} `xml:"cacheFields"`
			}
			if err := xml.Unmarshal(cd, &cache); err == nil {
				pt.SourceSheet = cache.Source.WS.Sheet
				pt.SourceRange = cache.Source.WS.Ref
				for _, cf := range cache.Fields.Field {
					fields = append(fields, cf.Name)
				}
			}
		}
	}
	fieldName := func(idx int) string {
		if idx >= 0 && idx < len(fields) {
			return fields[idx]
		}
		return ""
	}
	for _, rf := range doc.RowFields.Field {
		pt.Rows = append(pt.Rows, fieldName(rf.X))
	}
	for _, cf := range doc.ColFields.Field {
		pt.Cols = append(pt.Cols, fieldName(cf.X))
	}
	for _, pf := range doc.PageFields.Field {
		pt.Filters = append(pt.Filters, fieldName(pf.Fld))
	}
	for _, df := range doc.DataFields.Field {
		agg := df.Subtotal
		if agg == "" {
			agg = "sum"
		}
		pt.Values = append(pt.Values, PivotValueField{
			Field: fieldName(df.Fld), Aggregation: agg, DisplayName: df.Name,
		})
	}
	return pt, true
}

// cacheDefFor resolves a pivot table's cache definition part: through the
// pivot part's own rels, falling back to the workbook pivotCaches by cacheId.
func (f *File) cacheDefFor(ptPart string, cacheID int) (string, bool) {
	if targets := f.sheetRelTargets(ptPart, relPivotCacheDef); len(targets) > 0 {
		return targets[0], true
	}
	wb, err := f.part("xl/workbook.xml")
	if err != nil {
		return "", false
	}
	pcs := xmlpart.FindChild(wb.Root(), "pivotCaches")
	if pcs == nil {
		return "", false
	}
	for _, pc := range xmlpart.Children(pcs, "pivotCache") {
		if id, _ := strconv.Atoi(pc.SelectAttrValue("cacheId", "-1")); id == cacheID {
			rid := pc.SelectAttrValue("id", "")
			rels, err := f.part("xl/_rels/workbook.xml.rels")
			if err != nil {
				return "", false
			}
			for _, ch := range xmlpart.Children(rels.Root(), "Relationship") {
				if ch.SelectAttrValue("Id", "") == rid {
					return path.Clean(path.Join("xl", ch.SelectAttrValue("Target", ""))), true
				}
			}
		}
	}
	return "", false
}

// RemovePivotTables deletes every pivot table, cache, and their wiring — the
// clean slate an authoritative save rebuilds onto (re-adding without removing
// would duplicate caches).
func (f *File) RemovePivotTables() error {
	ct, err := f.mutatePart("[Content_Types].xml")
	if err != nil {
		return err
	}
	removeOverride := func(part string) {
		for _, ch := range xmlpart.Children(ct.Root(), "Override") {
			if ch.SelectAttrValue("PartName", "") == "/"+part {
				xmlpart.Remove(ct.Root(), ch)
			}
		}
	}
	dropPart := func(name string) {
		f.deleted[name] = true
		delete(f.added, name)
		delete(f.parsed, name)
		delete(f.dirty, name)
		removeOverride(name)
	}

	for _, name := range f.SheetNames() {
		sh, err := f.Sheet(name)
		if err != nil {
			continue
		}
		ptParts := f.sheetRelTargets(sh.partName, relPivotTable)
		if len(ptParts) == 0 {
			continue
		}
		for _, ptPart := range ptParts {
			for _, cache := range f.sheetRelTargets(ptPart, relPivotCacheDef) {
				for _, recs := range f.sheetRelTargets(cache, relPivotCacheRecs) {
					dropPart(recs)
				}
				dropPart(path.Clean(path.Join(path.Dir(cache), "_rels", path.Base(cache)+".rels")))
				dropPart(cache)
			}
			dropPart(path.Clean(path.Join(path.Dir(ptPart), "_rels", path.Base(ptPart)+".rels")))
			dropPart(ptPart)
		}
		// Strip the sheet's pivot relationships.
		relsName := sheetRelsName(sh.partName)
		if rels, err := f.mutatePart(relsName); err == nil {
			for _, ch := range xmlpart.Children(rels.Root(), "Relationship") {
				if ch.SelectAttrValue("Type", "") == relPivotTable {
					xmlpart.Remove(rels.Root(), ch)
				}
			}
		}
	}

	// Workbook: drop pivotCaches and their rels.
	wb, err := f.mutatePart("xl/workbook.xml")
	if err != nil {
		return err
	}
	if pcs := xmlpart.FindChild(wb.Root(), "pivotCaches"); pcs != nil {
		rels, err := f.mutatePart("xl/_rels/workbook.xml.rels")
		if err != nil {
			return err
		}
		for _, pc := range xmlpart.Children(pcs, "pivotCache") {
			rid := pc.SelectAttrValue("id", "")
			for _, ch := range xmlpart.Children(rels.Root(), "Relationship") {
				if ch.SelectAttrValue("Id", "") == rid {
					target := path.Clean(path.Join("xl", ch.SelectAttrValue("Target", "")))
					dropPart(target)
					dropPart(path.Clean(path.Join(path.Dir(target), "_rels", path.Base(target)+".rels")))
					xmlpart.Remove(rels.Root(), ch)
				}
			}
		}
		xmlpart.Remove(wb.Root(), pcs)
	}
	return nil
}

// AddPivotTable creates a pivot table: the cache definition (field names read
// from the source range's header row, refreshOnLoad so consumers recompute),
// empty cache records, the table definition, and all the wiring.
func (f *File) AddPivotTable(pt PivotTable) error {
	src, err := f.Sheet(pt.SourceSheet)
	if err != nil {
		return err
	}
	srcRange, err := ParseRange(pt.SourceRange)
	if err != nil {
		return err
	}
	if _, err := f.Sheet(pt.TargetSheet); err != nil {
		return err
	}
	if pt.Location == "" {
		return fmt.Errorf("xlsx: pivot table %q needs a target location", pt.Name)
	}

	// Field names from the header row.
	var fields []string
	fieldIdx := map[string]int{}
	for col := srcRange.StartCol; col <= srcRange.EndCol; col++ {
		name := src.Cell(srcRange.StartRow, col).Value.S
		if name == "" {
			name = "Field" + strconv.Itoa(col-srcRange.StartCol+1)
		}
		fieldIdx[name] = len(fields)
		fields = append(fields, name)
	}
	resolve := func(names []string, what string) ([]int, error) {
		var out []int
		for _, n := range names {
			idx, ok := fieldIdx[n]
			if !ok {
				return nil, fmt.Errorf("xlsx: pivot %s field %q is not a source column", what, n)
			}
			out = append(out, idx)
		}
		return out, nil
	}
	rowIdx, err := resolve(pt.Rows, "row")
	if err != nil {
		return err
	}
	colIdx, err := resolve(pt.Cols, "column")
	if err != nil {
		return err
	}
	pageIdx, err := resolve(pt.Filters, "filter")
	if err != nil {
		return err
	}
	type dataField struct {
		idx     int
		agg     string
		display string
	}
	var dataFields []dataField
	for _, v := range pt.Values {
		idx, ok := fieldIdx[v.Field]
		if !ok {
			return fmt.Errorf("xlsx: pivot value field %q is not a source column", v.Field)
		}
		agg := v.Aggregation
		if agg == "" {
			agg = "sum"
		}
		display := v.DisplayName
		if display == "" {
			display = strings.ToUpper(agg[:1]) + agg[1:] + " of " + v.Field
		}
		dataFields = append(dataFields, dataField{idx: idx, agg: agg, display: display})
	}

	// Allocate part names and ids.
	n := 1
	for f.partExists(fmt.Sprintf("xl/pivotCache/pivotCacheDefinition%d.xml", n)) {
		n++
	}
	cachePart := fmt.Sprintf("xl/pivotCache/pivotCacheDefinition%d.xml", n)
	recsPart := fmt.Sprintf("xl/pivotCache/pivotCacheRecords%d.xml", n)
	tn := 1
	for f.partExists(fmt.Sprintf("xl/pivotTables/pivotTable%d.xml", tn)) {
		tn++
	}
	tablePart := fmt.Sprintf("xl/pivotTables/pivotTable%d.xml", tn)

	// Cache definition + empty records.
	var cd strings.Builder
	cd.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	cd.WriteString(`<pivotCacheDefinition ` + spreadsheetMainXNS + ` ` + relationshipsXNS +
		` r:id="rId1" refreshOnLoad="1" recordCount="0">`)
	cd.WriteString(`<cacheSource type="worksheet"><worksheetSource ref="` + xmlEscape(pt.SourceRange) +
		`" sheet="` + xmlEscape(pt.SourceSheet) + `"/></cacheSource>`)
	cd.WriteString(`<cacheFields count="` + strconv.Itoa(len(fields)) + `">`)
	for _, name := range fields {
		cd.WriteString(`<cacheField name="` + xmlEscape(name) + `" numFmtId="0"><sharedItems/></cacheField>`)
	}
	cd.WriteString(`</cacheFields></pivotCacheDefinition>` + "\n")
	if err := f.setPart(cachePart, []byte(cd.String())); err != nil {
		return err
	}
	if err := f.setPart(recsPart, []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<pivotCacheRecords `+spreadsheetMainXNS+` count="0"/>
`)); err != nil {
		return err
	}
	if err := f.setPart(path.Dir(cachePart)+"/_rels/"+path.Base(cachePart)+".rels", []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="`+relPivotCacheRecs+`" Target="`+path.Base(recsPart)+`"/>
</Relationships>
`)); err != nil {
		return err
	}

	// Workbook pivotCaches entry + rel.
	cacheID, err := f.registerPivotCache(cachePart)
	if err != nil {
		return err
	}

	// The pivot table definition.
	style := pt.StyleName
	if style == "" {
		style = defaultPivotStyle
	}
	gt := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	axis := map[int]string{}
	for _, i := range rowIdx {
		axis[i] = "axisRow"
	}
	for _, i := range colIdx {
		axis[i] = "axisCol"
	}
	for _, i := range pageIdx {
		axis[i] = "axisPage"
	}
	inData := map[int]bool{}
	for _, df := range dataFields {
		inData[df.idx] = true
	}
	var tb strings.Builder
	tb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	tb.WriteString(`<pivotTableDefinition ` + spreadsheetMainXNS + ` name="` + xmlEscape(pt.Name) +
		`" cacheId="` + strconv.Itoa(cacheID) + `" dataCaption="Values"` +
		` rowGrandTotals="` + gt(pt.RowGrandTotals) + `" colGrandTotals="` + gt(pt.ColGrandTotals) + `">`)
	tb.WriteString(`<location ref="` + xmlEscape(pt.Location) + `" firstHeaderRow="1" firstDataRow="2" firstDataCol="1"/>`)
	tb.WriteString(`<pivotFields count="` + strconv.Itoa(len(fields)) + `">`)
	for i := range fields {
		tb.WriteString("<pivotField")
		if ax, ok := axis[i]; ok {
			tb.WriteString(` axis="` + ax + `"`)
		}
		if inData[i] {
			tb.WriteString(` dataField="1"`)
		}
		tb.WriteString(` showAll="0"/>`)
	}
	tb.WriteString(`</pivotFields>`)
	writeAxisFields := func(el, item string, idxs []int) {
		if len(idxs) == 0 {
			return
		}
		tb.WriteString(`<` + el + ` count="` + strconv.Itoa(len(idxs)) + `">`)
		for _, i := range idxs {
			tb.WriteString(`<` + item + ` x="` + strconv.Itoa(i) + `"/>`)
		}
		tb.WriteString(`</` + el + `>`)
	}
	writeAxisFields("rowFields", "field", rowIdx)
	writeAxisFields("colFields", "field", colIdx)
	if len(pageIdx) > 0 {
		tb.WriteString(`<pageFields count="` + strconv.Itoa(len(pageIdx)) + `">`)
		for _, i := range pageIdx {
			tb.WriteString(`<pageField fld="` + strconv.Itoa(i) + `" hier="-1"/>`)
		}
		tb.WriteString(`</pageFields>`)
	}
	if len(dataFields) > 0 {
		tb.WriteString(`<dataFields count="` + strconv.Itoa(len(dataFields)) + `">`)
		for _, df := range dataFields {
			tb.WriteString(`<dataField name="` + xmlEscape(df.display) + `" fld="` + strconv.Itoa(df.idx) + `"`)
			if df.agg != "sum" {
				tb.WriteString(` subtotal="` + xmlEscape(df.agg) + `"`)
			}
			tb.WriteString(` baseField="0" baseItem="0"/>`)
		}
		tb.WriteString(`</dataFields>`)
	}
	tb.WriteString(`<pivotTableStyleInfo name="` + xmlEscape(style) + `" showRowHeaders="1" showColHeaders="1" showLastColumn="1"/>`)
	tb.WriteString(`</pivotTableDefinition>` + "\n")
	if err := f.setPart(tablePart, []byte(tb.String())); err != nil {
		return err
	}
	relToCache, err := relPath(path.Dir(tablePart), cachePart)
	if err != nil {
		return err
	}
	if err := f.setPart(path.Dir(tablePart)+"/_rels/"+path.Base(tablePart)+".rels", []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="`+relPivotCacheDef+`" Target="`+relToCache+`"/>
</Relationships>
`)); err != nil {
		return err
	}

	// Target sheet references the pivot part; content types register all three.
	target, err := f.Sheet(pt.TargetSheet)
	if err != nil {
		return err
	}
	if err := target.addSheetRel(relPivotTable, tablePart); err != nil {
		return err
	}
	ct, err := f.mutatePart("[Content_Types].xml")
	if err != nil {
		return err
	}
	addOverride := func(part, ctype string) {
		over := etree.NewElement("Override")
		over.CreateAttr("PartName", "/"+part)
		over.CreateAttr("ContentType", ctype)
		ct.Root().AddChild(over)
	}
	addOverride(cachePart, ctPivotCacheDef)
	addOverride(recsPart, ctPivotCacheRecs)
	addOverride(tablePart, ctPivotTable)
	return nil
}

// registerPivotCache adds a workbook pivotCaches entry (allocating the next
// cacheId + workbook rel) for a cache definition part.
func (f *File) registerPivotCache(cachePart string) (int, error) {
	wb, err := f.mutatePart("xl/workbook.xml")
	if err != nil {
		return 0, err
	}
	rels, err := f.mutatePart("xl/_rels/workbook.xml.rels")
	if err != nil {
		return 0, err
	}
	maxRel := 0
	for _, ch := range xmlpart.Children(rels.Root(), "Relationship") {
		id := ch.SelectAttrValue("Id", "")
		if strings.HasPrefix(id, "rId") {
			if n, err := strconv.Atoi(id[3:]); err == nil && n > maxRel {
				maxRel = n
			}
		}
	}
	rid := fmt.Sprintf("rId%d", maxRel+1)
	rel := etree.NewElement("Relationship")
	rel.CreateAttr("Id", rid)
	rel.CreateAttr("Type", relPivotCacheDef)
	rel.CreateAttr("Target", strings.TrimPrefix(cachePart, "xl/"))
	rels.Root().AddChild(rel)

	// CT_Workbook order: pivotCaches follows definedNames/calcPr.
	pcs := xmlpart.EnsureChildInOrder(wb.Root(), "pivotCaches", workbookOrder)
	maxCache := 0
	for _, pc := range xmlpart.Children(pcs, "pivotCache") {
		if id, err := strconv.Atoi(pc.SelectAttrValue("cacheId", "0")); err == nil && id > maxCache {
			maxCache = id
		}
	}
	pc := etree.NewElement("pivotCache")
	pc.CreateAttr("cacheId", strconv.Itoa(maxCache+1))
	pc.CreateAttr("r:id", rid)
	pcs.AddChild(pc)
	return maxCache + 1, nil
}

// workbookOrder is the CT_Workbook child sequence.
var workbookOrder = []string{
	"fileVersion", "fileSharing", "workbookPr", "workbookProtection", "bookViews",
	"sheets", "functionGroups", "externalReferences", "definedNames", "calcPr",
	"oleSize", "customWorkbookViews", "pivotCaches", "smartTagPr", "smartTagTypes",
	"webPublishing", "fileRecoveryPr", "webPublishObjects", "extLst",
}

// addSheetRel appends a relationship on the sheet's rels part (created when
// absent), returning nothing; ids allocate past the current maximum.
func (s *SheetEdit) addSheetRel(relType, target string) error {
	relsName := sheetRelsName(s.partName)
	if !s.file.partExists(relsName) {
		s.file.added[relsName] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>
`)
	}
	rels, err := s.file.mutatePart(relsName)
	if err != nil {
		return err
	}
	maxRel := 0
	for _, ch := range xmlpart.Children(rels.Root(), "Relationship") {
		id := ch.SelectAttrValue("Id", "")
		if strings.HasPrefix(id, "rId") {
			if n, err := strconv.Atoi(id[3:]); err == nil && n > maxRel {
				maxRel = n
			}
		}
	}
	relTarget, err := relPath(path.Dir(s.partName), target)
	if err != nil {
		return err
	}
	el := etree.NewElement("Relationship")
	el.CreateAttr("Id", fmt.Sprintf("rId%d", maxRel+1))
	el.CreateAttr("Type", relType)
	el.CreateAttr("Target", relTarget)
	rels.Root().AddChild(el)
	return nil
}

// SetDefinedNames replaces the workbook's defined names wholesale.
func (f *File) SetDefinedNames(names []DefinedName) error {
	wb, err := f.mutatePart("xl/workbook.xml")
	if err != nil {
		return err
	}
	if dn := xmlpart.FindChild(wb.Root(), "definedNames"); dn != nil {
		xmlpart.Remove(wb.Root(), dn)
	}
	if len(names) == 0 {
		return nil
	}
	dn := xmlpart.EnsureChildInOrder(wb.Root(), "definedNames", workbookOrder)
	for _, n := range names {
		el := etree.NewElement("definedName")
		el.CreateAttr("name", n.Name)
		if n.LocalSheet != nil {
			el.CreateAttr("localSheetId", strconv.Itoa(*n.LocalSheet))
		}
		if n.Hidden {
			el.CreateAttr("hidden", "1")
		}
		el.SetText(n.RefersTo)
		dn.AddChild(el)
	}
	return nil
}

// DefinedNames reads the workbook's defined names (editor view).
func (f *File) DefinedNames() []DefinedName {
	wb, err := f.part("xl/workbook.xml")
	if err != nil {
		return nil
	}
	dn := xmlpart.FindChild(wb.Root(), "definedNames")
	if dn == nil {
		return nil
	}
	var out []DefinedName
	for _, el := range xmlpart.Children(dn, "definedName") {
		d := DefinedName{
			Name:     el.SelectAttrValue("name", ""),
			RefersTo: strings.TrimSpace(el.Text()),
			Hidden:   onOff(el.SelectAttrValue("hidden", "")),
		}
		if ls := el.SelectAttrValue("localSheetId", ""); ls != "" {
			if n, err := strconv.Atoi(ls); err == nil {
				d.LocalSheet = &n
			}
		}
		out = append(out, d)
	}
	return out
}
