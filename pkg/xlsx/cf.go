package xlsx

import (
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/nathanstitt/doctaculous/pkg/xlsx/internal/xmlpart"
)

// ConditionalFormatting is one <conditionalFormatting> block: the ranges it
// applies to and its rules in priority order.
type ConditionalFormatting struct {
	// Ranges are the sqref ranges ("A1:B4", space-separated in the file).
	Ranges []string
	Rules  []CFRule
}

// CFRule is one conditional-format rule. The typed fields cover the rule
// vocabulary an application edits (cellIs, containsText, expression, ...);
// Raw carries the VERBATIM <cfRule> XML for lossless passthrough — a consumer
// re-emitting a rule kind it does not model hands Raw back unchanged and
// nothing is lost (data bars, color scales, icon sets ride through).
type CFRule struct {
	// Type is the raw OOXML rule type ("cellIs", "containsText", "expression",
	// "colorScale", "dataBar", ...).
	Type string
	// Operator is the comparison for cellIs/containsText rules.
	Operator string
	// Formulas are the rule's formula operands in order.
	Formulas []string
	// Text is the containsText operand.
	Text string
	// Priority is the rule's evaluation priority (1 = highest).
	Priority int
	// StopIfTrue stops lower-priority rules when this one matches.
	StopIfTrue bool
	// Style is the rule's resolved differential format (dxf), or nil.
	Style *Style
	// Raw is the verbatim <cfRule> element XML.
	Raw []byte
}

// parseCF extracts a worksheet part's conditional formatting through the
// raw-fidelity tree (shared by the Workbook reader and the editor, so Raw is
// byte-faithful in both). dxfs resolves dxfId indices.
func parseCF(root *etree.Element, dxfs []*Style) []ConditionalFormatting {
	var out []ConditionalFormatting
	for _, cfEl := range xmlpart.Children(root, "conditionalFormatting") {
		cf := ConditionalFormatting{}
		if sqref := cfEl.SelectAttrValue("sqref", ""); sqref != "" {
			cf.Ranges = strings.Fields(sqref)
		}
		for _, ruleEl := range xmlpart.Children(cfEl, "cfRule") {
			rule := CFRule{
				Type:     ruleEl.SelectAttrValue("type", ""),
				Operator: ruleEl.SelectAttrValue("operator", ""),
				Text:     ruleEl.SelectAttrValue("text", ""),
			}
			rule.Priority, _ = strconv.Atoi(ruleEl.SelectAttrValue("priority", "0"))
			rule.StopIfTrue = onOff(ruleEl.SelectAttrValue("stopIfTrue", ""))
			for _, f := range xmlpart.Children(ruleEl, "formula") {
				rule.Formulas = append(rule.Formulas, f.Text())
			}
			if dxfAttr := ruleEl.SelectAttrValue("dxfId", ""); dxfAttr != "" {
				if id, err := strconv.Atoi(dxfAttr); err == nil && id >= 0 && id < len(dxfs) {
					rule.Style = dxfs[id]
				}
			}
			doc := etree.NewDocument()
			doc.SetRoot(ruleEl.Copy())
			if raw, err := doc.WriteToBytes(); err == nil {
				rule.Raw = raw
			}
			cf.Rules = append(cf.Rules, rule)
		}
		out = append(out, cf)
	}
	return out
}

// ConditionalFormats reads the sheet's conditional formatting (editor view).
func (s *SheetEdit) ConditionalFormats() []ConditionalFormatting {
	root, err := s.doc()
	if err != nil {
		return nil
	}
	return parseCF(root, s.file.resolvedStyles().dxfs)
}

// SetConditionalFormats REPLACES the sheet's conditional formatting wholesale
// (the authoritative-save shape). A rule with Raw set re-emits verbatim —
// the lossless passthrough for rule kinds the caller does not model — with
// only its priority renumbered (and, when Style is also set, a freshly
// minted dxfId stamped on — see cfRuleElement); a typed rule synthesizes its
// element, minting a dxf from Style when present. Priorities renumber 1..N
// across the sheet in order. An empty set removes every block.
func (s *SheetEdit) SetConditionalFormats(cfs []ConditionalFormatting) error {
	root, err := s.mut()
	if err != nil {
		return err
	}
	for _, old := range xmlpart.Children(root, "conditionalFormatting") {
		xmlpart.Remove(root, old)
	}
	if len(cfs) == 0 {
		return nil
	}

	// A placeholder claims the schema position; every block inserts before it
	// in cfs order, then the placeholder goes away.
	placeholder := xmlpart.EnsureChildInOrder(root, "conditionalFormatting", worksheetOrder)
	priority := 0
	for _, cf := range cfs {
		cfEl := etree.NewElement("conditionalFormatting")
		if len(cf.Ranges) > 0 {
			cfEl.CreateAttr("sqref", strings.Join(cf.Ranges, " "))
		}
		for _, rule := range cf.Rules {
			priority++
			ruleEl, err := s.file.cfRuleElement(rule, priority)
			if err != nil {
				return err
			}
			cfEl.AddChild(ruleEl)
		}
		xmlpart.InsertBefore(root, cfEl, placeholder)
	}
	xmlpart.Remove(root, placeholder)
	return nil
}

// cfRuleElement builds one cfRule element: verbatim from Raw when present
// (priority renumbered), else synthesized from the typed fields.
//
// A Raw rule WITH Style set additionally mints a dxf and stamps its dxfId
// onto the raw element — the legacy-upgrade shape: a caller re-synthesizing
// rule XML from a pre-doctaculous representation has the rule body but no
// valid dxf index to embed. A Raw rule WITHOUT Style keeps any embedded
// dxfId verbatim (valid inductively: this editor only appends to <dxfs>,
// never compacts it).
func (f *File) cfRuleElement(rule CFRule, priority int) (*etree.Element, error) {
	if len(rule.Raw) > 0 {
		p, err := xmlpart.Parse(rule.Raw)
		if err != nil {
			return nil, err
		}
		el := p.Root().Copy()
		if rule.Style != nil {
			id, err := f.dxfIDFor(*rule.Style)
			if err != nil {
				return nil, err
			}
			el.CreateAttr("dxfId", strconv.Itoa(id))
		}
		el.CreateAttr("priority", strconv.Itoa(priority))
		return el, nil
	}
	el := etree.NewElement("cfRule")
	if rule.Type != "" {
		el.CreateAttr("type", rule.Type)
	}
	if rule.Style != nil {
		id, err := f.dxfIDFor(*rule.Style)
		if err != nil {
			return nil, err
		}
		el.CreateAttr("dxfId", strconv.Itoa(id))
	}
	el.CreateAttr("priority", strconv.Itoa(priority))
	if rule.StopIfTrue {
		el.CreateAttr("stopIfTrue", "1")
	}
	if rule.Operator != "" {
		el.CreateAttr("operator", rule.Operator)
	}
	if rule.Text != "" {
		el.CreateAttr("text", rule.Text)
	}
	for _, formula := range rule.Formulas {
		fe := el.CreateElement("formula")
		fe.SetText(formula)
	}
	return el, nil
}

// dxfIDFor dedupes-or-appends a differential format built from a Style into
// the styles part's <dxfs>. A dxf carries only the facets the style DECLARES
// (fonts/fills/borders are differential, not complete records).
func (f *File) dxfIDFor(st Style) (int, error) {
	root, err := f.stylesRoot()
	if err != nil {
		return 0, err
	}
	dxfs := xmlpart.EnsureChildInOrder(root, "dxfs", styleSheetOrder)
	el := buildDxfNode(st)
	return dedupeAppend(dxfs, "dxf", el), nil
}

// buildDxfNode renders a Style as a differential format element.
func buildDxfNode(st Style) *etree.Element {
	dxf := etree.NewElement("dxf")
	if font := st.Font; font != (Font{}) {
		fe := dxf.CreateElement("font")
		if font.Bold {
			fe.CreateElement("b")
		}
		if font.Italic {
			fe.CreateElement("i")
		}
		if font.Strike {
			fe.CreateElement("strike")
		}
		if font.Underline != "" {
			u := fe.CreateElement("u")
			u.CreateAttr("val", font.Underline)
		}
		if !colorEmpty(font.Color) {
			buildColorAttrs(fe.CreateElement("color"), font.Color)
		}
		if font.Size != 0 {
			sz := fe.CreateElement("sz")
			sz.CreateAttr("val", trimFloat(font.Size))
		}
		if font.Name != "" {
			n := fe.CreateElement("name")
			n.CreateAttr("val", font.Name)
		}
	}
	if fill := st.Fill; fill.Pattern != "" || !colorEmpty(fill.Fg) || !colorEmpty(fill.Bg) {
		fillEl := dxf.CreateElement("fill")
		pf := fillEl.CreateElement("patternFill")
		if fill.Pattern != "" {
			pf.CreateAttr("patternType", fill.Pattern)
		}
		// In a dxf fill, bgColor carries the visible solid color (Excel's
		// differential-fill convention); emit whichever facets are declared.
		if !colorEmpty(fill.Fg) {
			buildColorAttrs(pf.CreateElement("fgColor"), fill.Fg)
		}
		if !colorEmpty(fill.Bg) {
			buildColorAttrs(pf.CreateElement("bgColor"), fill.Bg)
		}
		_ = fillEl
	}
	if border := st.Border; border != (Border{}) {
		dxf.AddChild(buildBorderNode(border))
	}
	return dxf
}
