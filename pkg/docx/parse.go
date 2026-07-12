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

	doc.Rels = pkg.allRels(mainName)
	doc.Media = pkg.mediaParts()
	headers, footers, err := resolveHeadersFooters(pkg, doc.Rels)
	if err != nil {
		return nil, err
	}
	doc.Headers, doc.Footers = headers, footers

	// Footnotes/endnotes parts: prefer the relationship target, fall back to the
	// convention. The two parts share their grammar (parseNotes).
	const footnotesType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"
	if data, ok := pkg.part(resolveByType(pkg, mainName, footnotesType, "word/footnotes.xml")); ok {
		fn, err := parseNotes(data, "footnote")
		if err != nil {
			return nil, err
		}
		doc.Footnotes = fn
	}
	const endnotesType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes"
	if data, ok := pkg.part(resolveByType(pkg, mainName, endnotesType, "word/endnotes.xml")); ok {
		en, err := parseNotes(data, "endnote")
		if err != nil {
			return nil, err
		}
		doc.Endnotes = en
	}

	// Comments part.
	const commentsType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	if data, ok := pkg.part(resolveByType(pkg, mainName, commentsType, "word/comments.xml")); ok {
		cm, err := parseComments(data)
		if err != nil {
			return nil, err
		}
		doc.Comments = cm
	}

	// Preserve customXml parts verbatim so app-specific data survives a
	// read-modify-write cycle (Document.ExtraParts).
	doc.ExtraParts = pkg.partsWithPrefix("customXml/")

	// Resolve every hyperlink's Target now that the relationships are loaded,
	// so consumers (and the writer) see the URL without a rels lookup.
	resolveHyperlinkTargets(doc)
	return doc, nil
}

// resolveHyperlinkTargets fills Hyperlink.Target from the document rels for
// every external hyperlink in the body, notes, comments, and headers/footers.
func resolveHyperlinkTargets(doc *Document) {
	if len(doc.Rels) == 0 {
		return
	}
	resolve := func(h *Hyperlink) {
		if h.Target == "" && h.RelID != "" {
			if rel, ok := doc.Rels[h.RelID]; ok && rel.External {
				h.Target = rel.Target
			}
		}
	}
	var walkChildren func(children []ParaChild)
	var walkBlocks func(blocks []Block)
	walkChildren = func(children []ParaChild) {
		for i := range children {
			switch {
			case children[i].Hyperlink != nil:
				resolve(children[i].Hyperlink)
			case children[i].Revision != nil:
				walkChildren(children[i].Revision.Content)
			}
		}
	}
	walkBlocks = func(blocks []Block) {
		for i := range blocks {
			switch {
			case blocks[i].Paragraph != nil:
				walkChildren(blocks[i].Paragraph.Content)
			case blocks[i].Table != nil:
				for _, row := range blocks[i].Table.Rows {
					for _, cell := range row.Cells {
						walkBlocks(cell.Blocks)
					}
				}
			}
		}
	}
	walkBlocks(doc.Body)
	for _, notes := range []*Notes{doc.Footnotes, doc.Endnotes} {
		if notes == nil {
			continue
		}
		for _, blocks := range notes.ByID {
			walkBlocks(blocks)
		}
	}
	for _, c := range doc.Comments {
		walkBlocks(c.Body)
	}
	for _, hf := range doc.Headers {
		walkBlocks(hf.Blocks)
	}
	for _, hf := range doc.Footers {
		walkBlocks(hf.Blocks)
	}
}

// parseComments parses a word/comments.xml part into id-keyed comments.
func parseComments(data []byte) (map[int]*Comment, error) {
	out := map[int]*Comment{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: comments: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != "comment" {
			continue
		}
		c := &Comment{}
		c.ID, _ = wAttrInt(se, "id")
		c.Author, _ = wAttr(se, "author")
		c.Initials, _ = wAttr(se, "initials")
		c.Date, _ = wAttr(se, "date")
		if err := fillBlocksUntil(dec, "comment", &c.Body); err != nil {
			return nil, err
		}
		out[c.ID] = c
	}
	return out, nil
}

// resolveByType returns the part name for the first relationship of relType on
// the main part, falling back to fallback.
func resolveByType(pkg *pkgReader, mainName, relType, fallback string) string {
	if rels := pkg.relsForByType(mainName, relType); rels != "" {
		return rels
	}
	return fallback
}

// resolveHeadersFooters parses every header/footer part referenced by the
// document relationships, keyed by relationship id. Header and footer parts are
// distinguished by relationship type. A malformed part is a hard error, matching
// the other optional parts (styles/numbering/footnotes).
func resolveHeadersFooters(pkg *pkgReader, rels map[string]Relationship) (headers, footers map[string]*HeaderFooter, err error) {
	const hdrType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	const ftrType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	for id, rel := range rels {
		switch rel.Type {
		case hdrType:
			if data, ok := pkg.part(rel.Target); ok {
				hf, err := parseHdrFtr(data, "hdr")
				if err != nil {
					return nil, nil, err
				}
				if headers == nil {
					headers = map[string]*HeaderFooter{}
				}
				headers[id] = hf
			}
		case ftrType:
			if data, ok := pkg.part(rel.Target); ok {
				hf, err := parseHdrFtr(data, "ftr")
				if err != nil {
					return nil, nil, err
				}
				if footers == nil {
					footers = map[string]*HeaderFooter{}
				}
				footers[id] = hf
			}
		}
	}
	return headers, footers, nil
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

// allRels returns every relationship for a source part, keyed by id, with targets
// resolved relative to the part's directory for internal (package) targets and
// left verbatim for external ones.
func (p *pkgReader) allRels(partName string) map[string]Relationship {
	partName = cleanPart(partName)
	dir, base := splitPart(partName)
	relsName := joinPart(dir, "_rels", base+".rels")
	data, ok := p.part(relsName)
	if !ok {
		return nil
	}
	var doc struct {
		Rels []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
			Type       string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make(map[string]Relationship, len(doc.Rels))
	for _, r := range doc.Rels {
		external := r.TargetMode == "External"
		target := r.Target
		if !external {
			target = joinPart(dir, r.Target)
		}
		out[r.ID] = Relationship{ID: r.ID, Target: target, External: external, Type: r.Type}
	}
	return out
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
				doc.Sections = append(doc.Sections, sect)
			default:
				sect, err := appendBlockChild(dec, t, &doc.Body)
				if err != nil {
					return err
				}
				if sect != nil {
					doc.Section = *sect
					doc.Sections = append(doc.Sections, *sect)
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
//
// A w:sdt (structured document tag / content control) is NOT handled here — it can
// unwrap to multiple blocks, which this single-block signature can't express. Block
// contexts route through appendBlockChild, which unwraps sdt; the few callers that
// use parseBlockChild directly do so where sdt cannot appear.
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

// appendBlockChild parses one block-level start element and appends the resulting
// block(s) to blocks, returning any section break the element carried. It differs
// from parseBlockChild in one respect: a w:sdt (content control) is transparently
// unwrapped — its w:sdtContent children are parsed as if they sat inline in the
// parent, appending every inner block. This keeps content-control text/tables/lists
// from being silently dropped. The last inner sectPr (if any) is returned.
func appendBlockChild(dec *xml.Decoder, start xml.StartElement, blocks *[]Block) (*SectionProps, error) {
	if start.Name.Local == "sdt" {
		return parseSdtBlocks(dec, blocks)
	}
	blk, sect, err := parseBlockChild(dec, start)
	if err != nil {
		return nil, err
	}
	if blk != nil {
		*blocks = append(*blocks, *blk)
	}
	return sect, nil
}

// parseSdtBlocks consumes a block-level w:sdt, unwrapping its w:sdtContent and
// appending each inner block to blocks (transparently, as if the sdt wrapper were
// absent). w:sdtPr / w:sdtEndPr and any other children are skipped. Returns the last
// section break found among the inner blocks, if any.
func parseSdtBlocks(dec *xml.Decoder, blocks *[]Block) (*SectionProps, error) {
	var lastSect *SectionProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return lastSect, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "sdtContent" {
				for {
					ctok, cerr := dec.Token()
					if cerr != nil {
						return lastSect, fmt.Errorf("%w: sdtContent: %v", ErrMalformedXML, cerr)
					}
					switch ct := ctok.(type) {
					case xml.StartElement:
						if ct.Name.Space != wNS {
							if err := dec.Skip(); err != nil {
								return lastSect, fmt.Errorf("%w: sdtContent: %v", ErrMalformedXML, err)
							}
							continue
						}
						// Nested sdt unwraps recursively via appendBlockChild.
						sect, aerr := appendBlockChild(dec, ct, blocks)
						if aerr != nil {
							return lastSect, aerr
						}
						if sect != nil {
							lastSect = sect
						}
					case xml.EndElement:
						if ct.Name.Local == "sdtContent" {
							goto drainSdt
						}
					}
				}
			}
			if err := dec.Skip(); err != nil {
				return lastSect, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "sdt" {
				return lastSect, nil
			}
		}
	}
drainSdt:
	// sdtContent closed; consume up to the sdt end.
	for {
		tok, err := dec.Token()
		if err != nil {
			return lastSect, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
		}
		if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "sdt" {
			return lastSect, nil
		}
	}
}

// fillBlocksUntil consumes block content until the named end element, appending
// to blocks. It is the shared block-consumption loop for the header/footer and
// footnote part parsers (both wrap a run of w:body-grammar blocks in a single
// container element).
func fillBlocksUntil(dec *xml.Decoder, end string, blocks *[]Block) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
				}
				continue
			}
			if _, err := appendBlockChild(dec, t, blocks); err != nil {
				return err
			}
		case xml.EndElement:
			if t.Name.Local == end {
				return nil
			}
		}
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
				p.Props.SectPr = s // keep the attachment point for the writer
				sect = s
			default:
				children, err := parseParaChild(dec, t)
				if err != nil {
					return nil, nil, err
				}
				p.Content = append(p.Content, children...)
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				return p, sect, nil
			}
		}
	}
}

// parseParaChild dispatches one inline-level paragraph child (already-read
// start element t): runs, hyperlinks, revision wrappers, and comment range
// markers. Unknown elements are skipped. It is shared by the paragraph and
// revision loops (w:ins/w:del wrap the same inline grammar).
func parseParaChild(dec *xml.Decoder, t xml.StartElement) ([]ParaChild, error) {
	switch t.Name.Local {
	case "r":
		runs, drawings, err := parseRun(dec)
		if err != nil {
			return nil, err
		}
		var out []ParaChild
		for i := range runs {
			// Copy into a fresh local: &runs[i] would alias parseRun's slice.
			r := runs[i]
			out = append(out, ParaChild{Run: &r})
		}
		for _, dr := range drawings {
			out = append(out, ParaChild{Drawing: dr})
		}
		return out, nil
	case "hyperlink":
		h, before, after, err := parseHyperlink(dec, t)
		if err != nil {
			return nil, err
		}
		out := append(before, ParaChild{Hyperlink: h})
		return append(out, after...), nil
	case "ins", "del":
		rev, err := parseRevision(dec, t)
		if err != nil {
			return nil, err
		}
		return []ParaChild{{Revision: rev}}, nil
	case "commentRangeStart":
		id, _ := wAttrInt(t, "id")
		if err := dec.Skip(); err != nil {
			return nil, fmt.Errorf("%w: commentRangeStart: %v", ErrMalformedXML, err)
		}
		return []ParaChild{{CommentStart: &CommentMark{ID: id}}}, nil
	case "commentRangeEnd":
		id, _ := wAttrInt(t, "id")
		if err := dec.Skip(); err != nil {
			return nil, fmt.Errorf("%w: commentRangeEnd: %v", ErrMalformedXML, err)
		}
		return []ParaChild{{CommentEnd: &CommentMark{ID: id}}}, nil
	case "sdt":
		// Inline content control: transparently unwrap w:sdtContent's inline
		// children (runs, hyperlinks, nested revisions) as if the wrapper were
		// absent, so the run text is preserved rather than dropped.
		return parseInlineSdt(dec)
	default:
		if err := dec.Skip(); err != nil {
			return nil, fmt.Errorf("%w: p: %v", ErrMalformedXML, err)
		}
		return nil, nil
	}
}

// parseInlineSdt consumes an inline-level w:sdt inside a paragraph, returning the
// flattened inline children of its w:sdtContent (recursing through parseParaChild,
// so nested sdt/runs/hyperlinks all unwrap). Non-content children (w:sdtPr) are
// skipped.
func parseInlineSdt(dec *xml.Decoder) ([]ParaChild, error) {
	var out []ParaChild
	for {
		tok, err := dec.Token()
		if err != nil {
			return out, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "sdtContent" {
				for {
					ctok, cerr := dec.Token()
					if cerr != nil {
						return out, fmt.Errorf("%w: sdtContent: %v", ErrMalformedXML, cerr)
					}
					switch ct := ctok.(type) {
					case xml.StartElement:
						if ct.Name.Space != wNS {
							if err := dec.Skip(); err != nil {
								return out, fmt.Errorf("%w: sdtContent: %v", ErrMalformedXML, err)
							}
							continue
						}
						children, perr := parseParaChild(dec, ct)
						if perr != nil {
							return out, perr
						}
						out = append(out, children...)
					case xml.EndElement:
						if ct.Name.Local == "sdtContent" {
							goto drainInlineSdt
						}
					}
				}
			}
			if err := dec.Skip(); err != nil {
				return out, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "sdt" {
				return out, nil
			}
		}
	}
drainInlineSdt:
	for {
		tok, err := dec.Token()
		if err != nil {
			return out, fmt.Errorf("%w: sdt: %v", ErrMalformedXML, err)
		}
		if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "sdt" {
			return out, nil
		}
	}
}

// parseRevision consumes a w:ins / w:del wrapper (start is the already-read
// start element carrying id/author/date), recursing into its inline content —
// revisions are true containers: they wrap runs and hyperlinks and can nest.
func parseRevision(dec *xml.Decoder, start xml.StartElement) (*Revision, error) {
	rev := &Revision{Kind: RevisionInsert}
	if start.Name.Local == "del" {
		rev.Kind = RevisionDelete
	}
	rev.ID, _ = wAttrInt(start, "id")
	rev.Author, _ = wAttr(start, "author")
	rev.Date, _ = wAttr(start, "date")
	end := start.Name.Local
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
				}
				continue
			}
			children, err := parseParaChild(dec, t)
			if err != nil {
				return nil, err
			}
			rev.Content = append(rev.Content, children...)
		case xml.EndElement:
			if t.Name.Local == end {
				return rev, nil
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
				case "pBdr":
					b, err := parseBorders(dec, "pBdr")
					if err != nil {
						return props, nil, err
					}
					if b != (BoxBorders{}) {
						props.Borders = &b
					}
					continue
				case "tabs":
					applyTabs(&props, dec)
					continue
				case "pPrChange":
					// The outer pPr already holds the AFTER state; the nested pPr
					// is the before state.
					ch := &ParaPropsChange{Mark: parseRevisionMark(t)}
					if err := parsePPrChangeBody(dec, ch); err != nil {
						return props, nil, err
					}
					props.PPrChange = ch
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
	case "framePr":
		props.Frame = parseFramePr(e)
	}
}

// parseFramePr reads a w:framePr's attributes into a FramePr (drop caps plus
// captured frame geometry).
func parseFramePr(e xml.StartElement) *FramePr {
	f := &FramePr{}
	f.DropCap, _ = wAttr(e, "dropCap")
	if v, ok := wAttrInt(e, "lines"); ok {
		f.Lines = v
	}
	f.Wrap, _ = wAttr(e, "wrap")
	f.HAnchor, _ = wAttr(e, "hAnchor")
	f.VAnchor, _ = wAttr(e, "vAnchor")
	if v, ok := wAttrInt(e, "w"); ok {
		f.W = Twips(v)
	}
	if v, ok := wAttrInt(e, "h"); ok {
		f.H = Twips(v)
	}
	if v, ok := wAttrInt(e, "hSpace"); ok {
		f.HSpace = Twips(v)
	}
	return f
}

// parsePPrChangeBody consumes a w:pPrChange's children (the nested before-state
// w:pPr) up to its end element. The nested pPr uses the full pPr grammar; a
// sectPr inside it (unusual) is parsed and discarded.
func parsePPrChangeBody(dec *xml.Decoder, ch *ParaPropsChange) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: pPrChange: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "pPr" {
				prev, _, err := parsePPr(dec)
				if err != nil {
					return err
				}
				ch.Previous = prev
				continue
			}
			if err := dec.Skip(); err != nil {
				return fmt.Errorf("%w: pPrChange: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "pPrChange" {
				return nil
			}
		}
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

// applyTabs reads a w:tabs element's w:tab children into the paragraph's tab
// stops (position in twips + alignment).
func applyTabs(props *ParagraphProps, dec *xml.Decoder) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "tab" {
				var ts TabStop
				if v, ok := wAttrInt(t, "pos"); ok {
					ts.PosTwips = Twips(v)
				}
				if a, ok := wAttr(t, "val"); ok {
					ts.Align = a
				}
				props.TabStops = append(props.TabStops, ts)
			}
			_ = dec.Skip()
		case xml.EndElement:
			if t.Name.Local == "tabs" {
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
func parseRun(dec *xml.Decoder) ([]Run, []*Drawing, error) {
	var props RunProps
	var sb strings.Builder
	var out []Run
	var drawings []*Drawing
	flushText := func() {
		if sb.Len() > 0 {
			out = append(out, Run{Props: props, Text: sb.String()})
			sb.Reset()
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
				continue
			}
			switch t.Name.Local {
			case "rPr":
				props, err = parseRPr(dec)
				if err != nil {
					return nil, nil, err
				}
			case "t", "delText":
				// w:delText is the text of a run inside a w:del revision — same
				// content grammar as w:t; deletion-ness lives on the wrapper.
				text, err := parseText(dec, t)
				if err != nil {
					return nil, nil, err
				}
				sb.WriteString(text)
			case "tab":
				sb.WriteByte('\t')
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			case "br":
				flushText()
				out = append(out, Run{Props: props, Break: parseBreak(t)})
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			case "drawing":
				// A drawing accumulates separately from run text; within one w:r this
				// loses text/drawing interleaving, but Word emits each drawing in its
				// own run, so cross-run document order is preserved.
				dr, err := parseDrawing(dec)
				if err != nil {
					return nil, nil, err
				}
				if dr != nil {
					drawings = append(drawings, dr)
				}
			case "footnoteReference":
				if id, ok := wAttrInt(t, "id"); ok {
					flushText()
					out = append(out, Run{Props: props, FootnoteRef: id})
				}
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			case "endnoteReference":
				if id, ok := wAttrInt(t, "id"); ok {
					flushText()
					out = append(out, Run{Props: props, EndnoteRef: id})
				}
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			case "commentReference":
				if id, ok := wAttrInt(t, "id"); ok {
					flushText()
					out = append(out, Run{Props: props, CommentRef: id, HasCommentRef: true})
				}
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			default:
				if err := dec.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: r: %v", ErrMalformedXML, err)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "r" {
				flushText()
				return out, drawings, nil
			}
		}
	}
}

// parseDrawing consumes a w:drawing, extracting the image extent (wp:extent
// cx/cy in EMU), the blip's relationship id (a:blip r:embed), the anchor/wrap
// facts of a floating drawing, and the alt text. It walks by local name (the
// wp:/a:/pic: namespaces are distinct but the local names are unambiguous).
// Returns nil if no blip is found (an unsupported drawing shape).
func parseDrawing(dec *xml.Decoder) (*Drawing, error) {
	dr := &Drawing{}
	hasBlip := false
	inPositionH := false
	inAlign := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: drawing: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "anchor":
				dr.Anchored = true
			case "extent":
				if v, ok := attrInt64(t, "cx"); ok {
					dr.WidthEMU = v
				}
				if v, ok := attrInt64(t, "cy"); ok {
					dr.HeightEMU = v
				}
			case "blip":
				if id, ok := rAttr(t, "embed"); ok {
					dr.RelID = id
					hasBlip = true
				}
			case "docPr":
				if v, ok := wAttr(t, "descr"); ok {
					dr.Description = v
				}
				if v, ok := wAttr(t, "title"); ok {
					dr.Title = v
				}
			case "wrapSquare":
				dr.WrapKind = "square"
			case "wrapTopAndBottom":
				dr.WrapKind = "topAndBottom"
			case "wrapTight":
				dr.WrapKind = "tight"
			case "wrapThrough":
				dr.WrapKind = "through"
			case "wrapNone":
				dr.WrapKind = "none"
			case "positionH":
				inPositionH = true
			case "align":
				inAlign = inPositionH // only wp:positionH/wp:align is the horizontal alignment
			}
		case xml.CharData:
			if inAlign {
				if v := strings.TrimSpace(string(t)); v != "" {
					dr.AlignH = v
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "align":
				inAlign = false
			case "positionH":
				inPositionH = false
			case "drawing":
				if !hasBlip {
					return nil, nil
				}
				return dr, nil
			}
		}
	}
}

// attrInt64 returns an int64-valued attribute by local name (EMU extents exceed
// int range on 32-bit, so use int64).
func attrInt64(e xml.StartElement, local string) (int64, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			n, err := strconv.ParseInt(strings.TrimSpace(a.Value), 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// parseHyperlink consumes a w:hyperlink: its runs plus the r:id / w:anchor
// attributes. start is the already-read start element (carrying the attributes).
// Comment range markers found INSIDE the link are hoisted out (Hyperlink.Runs
// stays a flat []Run): a start marker moves to just before the link and an end
// marker to just after, so the range grows outward to cover the whole link —
// positions inside a link are not representable in the model, and covering the
// link whole is the conservative reading.
func parseHyperlink(dec *xml.Decoder, start xml.StartElement) (h *Hyperlink, before, after []ParaChild, err error) {
	h = &Hyperlink{}
	if id, ok := rAttr(start, "id"); ok {
		h.RelID = id
	}
	if anchor, ok := wAttr(start, "anchor"); ok {
		h.Anchor = anchor
	}
	for {
		tok, terr := dec.Token()
		if terr != nil {
			return nil, nil, nil, fmt.Errorf("%w: hyperlink: %v", ErrMalformedXML, terr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "r":
					runs, _, err := parseRun(dec)
					if err != nil {
						return nil, nil, nil, err
					}
					h.Runs = append(h.Runs, runs...)
					continue
				case "commentRangeStart":
					if id, ok := wAttrInt(t, "id"); ok {
						before = append(before, ParaChild{CommentStart: &CommentMark{ID: id}})
					}
				case "commentRangeEnd":
					if id, ok := wAttrInt(t, "id"); ok {
						after = append(after, ParaChild{CommentEnd: &CommentMark{ID: id}})
					}
				}
			}
			if err := dec.Skip(); err != nil {
				return nil, nil, nil, fmt.Errorf("%w: hyperlink: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "hyperlink" {
				return h, before, after, nil
			}
		}
	}
}

// rNS is the officeDocument relationships namespace (the r: prefix).
const rNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// rAttr returns an r-namespaced attribute by local name (e.g. r:id).
func rAttr(e xml.StartElement, local string) (string, bool) {
	for _, a := range e.Attr {
		if a.Name.Local == local && (a.Name.Space == rNS || a.Name.Space == "") {
			return a.Value, true
		}
	}
	return "", false
}

// parseText reads the character data of a w:t verbatim. We deliberately do not
// trim regardless of xml:space: that attribute is Word's signal that whitespace
// is significant, but even without it a <w:t>'s character data is the run's
// content (producers do not indent inside <w:t>), so trimming here would drop
// spaces that separate adjacent runs. The attribute is therefore not consulted.
func parseText(dec *xml.Decoder, start xml.StartElement) (string, error) {
	end := start.Name.Local // "t" or "delText" — same content grammar
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("%w: %s: %v", ErrMalformedXML, end, err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.WriteString(string(t))
		case xml.EndElement:
			if t.Name.Local == end {
				return sb.String(), nil
			}
		}
	}
}

// parseRPr consumes a w:rPr into RunProps. end is "rPr" for a run's own
// properties; the nested before-state inside w:rPrChange shares the grammar.
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
				if t.Name.Local == "rPrChange" {
					// The outer rPr already holds the AFTER state (what Word shows
					// with markup off); the nested rPr is the before state.
					ch := &RunPropsChange{Mark: parseRevisionMark(t)}
					if err := parseRPrChangeBody(dec, ch); err != nil {
						return props, err
					}
					props.RPrChange = ch
					continue
				}
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

// parseRevisionMark reads the shared id/author/date attributes off a
// revision-carrying element (rPrChange/pPrChange/tcPrChange/cellIns/cellDel).
func parseRevisionMark(e xml.StartElement) RevisionMark {
	var m RevisionMark
	m.ID, _ = wAttrInt(e, "id")
	m.Author, _ = wAttr(e, "author")
	m.Date, _ = wAttr(e, "date")
	return m
}

// parseRPrChangeBody consumes a w:rPrChange's children (the nested before-state
// w:rPr) up to its end element.
func parseRPrChangeBody(dec *xml.Decoder, ch *RunPropsChange) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: rPrChange: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "rPr" {
				prev, err := parseRPr(dec)
				if err != nil {
					return err
				}
				ch.Previous = prev
				continue
			}
			if err := dec.Skip(); err != nil {
				return fmt.Errorf("%w: rPrChange: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "rPrChange" {
				return nil
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
		// A present w:u enables underline unless val="none"/"off". An absent
		// val defaults to "single" (Word writes a bare <w:u w:color=.../> to
		// mean single underline), so only an explicit off value disables it.
		val := wVal(e)
		props.Underline = val != "none" && val != "off"
		props.HasUnderline = true
		if val != "" && val != "none" && val != "off" {
			props.UnderlineStyle = val
		}
		if c, ok := parseColor(wColor(e)); ok {
			props.UnderlineColor = c
			props.HasUnderlineColor = true
		}
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
	case "rStyle":
		props.StyleID = wVal(e)
	case "strike", "dstrike":
		props.Strike = parseOnOff(wVal(e))
		props.HasStrike = true
	case "vertAlign":
		switch wVal(e) {
		case "superscript":
			props.VertAlign = VertAlignSuperscript
		case "subscript":
			props.VertAlign = VertAlignSubscript
		default:
			props.VertAlign = VertAlignBaseline
		}
	case "highlight":
		if c, ok := highlightColor(wVal(e)); ok {
			props.Highlight = c
			props.HasHighlight = true
			props.HighlightName = wVal(e)
		}
	case "caps":
		props.Caps = parseOnOff(wVal(e))
		props.HasCaps = true
	case "smallCaps":
		props.SmallCaps = parseOnOff(wVal(e))
		props.HasSmallCaps = true
	case "shd":
		props.Shd = parseShd(e)
	}
}

// highlightColor maps a w:highlight named color to an RGBA. Unknown names yield
// ok=false. These are the 16 WordprocessingML highlight names.
func highlightColor(name string) (color.RGBA, bool) {
	m := map[string]color.RGBA{
		"yellow":      {R: 0xFF, G: 0xFF, B: 0x00, A: 0xFF},
		"green":       {R: 0x00, G: 0xFF, B: 0x00, A: 0xFF},
		"cyan":        {R: 0x00, G: 0xFF, B: 0xFF, A: 0xFF},
		"magenta":     {R: 0xFF, G: 0x00, B: 0xFF, A: 0xFF},
		"blue":        {R: 0x00, G: 0x00, B: 0xFF, A: 0xFF},
		"red":         {R: 0xFF, G: 0x00, B: 0x00, A: 0xFF},
		"darkBlue":    {R: 0x00, G: 0x00, B: 0x8B, A: 0xFF},
		"darkCyan":    {R: 0x00, G: 0x8B, B: 0x8B, A: 0xFF},
		"darkGreen":   {R: 0x00, G: 0x64, B: 0x00, A: 0xFF},
		"darkMagenta": {R: 0x8B, G: 0x00, B: 0x8B, A: 0xFF},
		"darkRed":     {R: 0x8B, G: 0x00, B: 0x00, A: 0xFF},
		"darkYellow":  {R: 0x80, G: 0x80, B: 0x00, A: 0xFF},
		"darkGray":    {R: 0xA9, G: 0xA9, B: 0xA9, A: 0xFF},
		"lightGray":   {R: 0xD3, G: 0xD3, B: 0xD3, A: 0xFF},
		"black":       {R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
		"white":       {R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
	}
	c, ok := m[name]
	return c, ok
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
				if err := parseTcPr(dec, &cell); err != nil {
					return cell, err
				}
			default:
				if _, err := appendBlockChild(dec, t, &cell.Blocks); err != nil {
					return cell, err
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
				case "tblLayout":
					if v, _ := wAttr(t, "type"); v == "fixed" {
						props.LayoutFixed = true
					}
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

// parseTcPr consumes a w:tcPr, filling the cell's props, gridSpan, vMerge, and
// revision marks in place.
func parseTcPr(dec *xml.Decoder, cell *TableCell) error {
	for {
		tok, terr := dec.Token()
		if terr != nil {
			return fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, terr)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS {
				switch t.Name.Local {
				case "gridSpan":
					if v, ok := wAttrInt(t, "val"); ok && v > 0 {
						cell.GridSpan = v
					}
				case "vMerge":
					cell.VMerge = parseVMerge(t)
				case "tcW":
					// Cells carry only a dxa width in the model; a pct cell width is dropped.
					var pct int
					applyTblW(&cell.Props.WidthDxa, &pct, t)
				case "vAlign":
					cell.Props.VAlign = parseVAlign(wVal(t))
				case "tcBorders":
					b, berr := parseBorders(dec, "tcBorders")
					if berr != nil {
						return berr
					}
					cell.Props.Borders = b
					continue
				case "shd":
					cell.Props.Shading = parseShd(t)
				case "cellIns":
					m := parseRevisionMark(t)
					cell.Ins = &m
				case "cellDel":
					m := parseRevisionMark(t)
					cell.Del = &m
				case "tcPrChange":
					// The outer tcPr already holds the AFTER state; the nested tcPr
					// is the before state.
					ch := &CellPropsChange{Mark: parseRevisionMark(t)}
					if err := parseTcPrChangeBody(dec, ch); err != nil {
						return err
					}
					cell.Props.TcPrChange = ch
					continue
				}
			}
			if serr := dec.Skip(); serr != nil {
				return fmt.Errorf("%w: tcPr: %v", ErrMalformedXML, serr)
			}
		case xml.EndElement:
			if t.Name.Local == "tcPr" {
				return nil
			}
		}
	}
}

// parseTcPrChangeBody consumes a w:tcPrChange's children (the nested
// before-state w:tcPr) up to its end element.
func parseTcPrChangeBody(dec *xml.Decoder, ch *CellPropsChange) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: tcPrChange: %v", ErrMalformedXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == wNS && t.Name.Local == "tcPr" {
				var prev TableCell
				prev.GridSpan = 1
				if err := parseTcPr(dec, &prev); err != nil {
					return err
				}
				ch.Previous = prev.Props
				continue
			}
			if err := dec.Skip(); err != nil {
				return fmt.Errorf("%w: tcPrChange: %v", ErrMalformedXML, err)
			}
		case xml.EndElement:
			if t.Name.Local == "tcPrChange" {
				return nil
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
// w:val style). val="nil"/"none" marks the edge as no-border; any other style
// name is kept verbatim in Style (rendering draws it solid; conversion
// round-trips the name).
func parseBorder(e xml.StartElement) Border {
	var bd Border
	switch v := wVal(e); v {
	case "nil", "none":
		bd.None = true
	default:
		bd.Style = v
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
				case "headerReference":
					if typ, _ := wAttr(t, "type"); typ == "default" || typ == "" {
						if id, ok := rAttr(t, "id"); ok {
							sect.HeaderRefDefault = id
						}
					}
				case "footerReference":
					if typ, _ := wAttr(t, "type"); typ == "default" || typ == "" {
						if id, ok := rAttr(t, "id"); ok {
							sect.FooterRefDefault = id
						}
					}
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
