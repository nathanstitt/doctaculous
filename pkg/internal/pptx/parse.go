package pptx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Relationship type URIs (OPC).
const (
	relOfficeDocument = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relSlide          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	relSlideLayout    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	relSlideMaster    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
	relImage          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
)

// pkgReader resolves and reads OPC parts (mirroring pkg/xlsx).
type pkgReader struct {
	parts map[string]*zip.File
}

func (p *pkgReader) read(name string) []byte {
	f, ok := p.parts[cleanPart(name)]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxPartSize+1))
	if err != nil || len(data) > maxPartSize {
		return nil
	}
	return data
}

func cleanPart(name string) string { return strings.TrimPrefix(name, "/") }

type relationship struct {
	ID     string
	Type   string
	Target string
}

// relsOf returns a part's relationships keyed by id, targets resolved against
// the part's directory. The package root ("/" or "") resolves _rels/.rels.
func (p *pkgReader) relsOf(partName string) map[string]relationship {
	partName = cleanPart(partName)
	dir := "."
	relsName := "_rels/.rels"
	if partName != "" {
		dir = path.Dir(partName)
		relsName = path.Join(dir, "_rels", path.Base(partName)+".rels")
	}
	data := p.read(relsName)
	if data == nil {
		return nil
	}
	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make(map[string]relationship, len(doc.Rels))
	for _, r := range doc.Rels {
		target := r.Target
		if r.Mode != "External" {
			target = cleanPart(path.Clean(path.Join(dir, r.Target)))
		}
		out[r.ID] = relationship{ID: r.ID, Type: r.Type, Target: target}
	}
	return out
}

// firstRelOfType returns a part's first relationship of relType.
func (p *pkgReader) firstRelOfType(partName, relType string) (relationship, bool) {
	for _, r := range p.relsOf(partName) {
		if r.Type == relType {
			return r, true
		}
	}
	return relationship{}, false
}

// phKey identifies a placeholder for inheritance: its type, else its index.
type phKey struct {
	typ string
	idx string
}

// phFrame is an inherited placeholder frame.
type phFrame struct {
	x, y, w, h float64
	has        bool
}

// parsePresentation drives the whole package.
func parsePresentation(pkg *pkgReader) (*Presentation, error) {
	mainPart := "ppt/presentation.xml"
	if rel, ok := pkg.firstRelOfType("/", relOfficeDocument); ok && pkg.read(rel.Target) != nil {
		mainPart = rel.Target
	}
	// firstRelOfType on the package root reads "_rels/.rels" via relsOf("/")…
	// path.Join normalizes; ensure the root part resolves.
	data := pkg.read(mainPart)
	if data == nil {
		return nil, fmt.Errorf("%w: missing presentation part", ErrNotPPTX)
	}

	var presDoc struct {
		SldSz struct {
			CX int64 `xml:"cx,attr"`
			CY int64 `xml:"cy,attr"`
		} `xml:"sldSz"`
		SldIDLst struct {
			SldID []struct {
				RID string `xml:"id,attr"` // r:id resolves by local name
			} `xml:"sldId"`
		} `xml:"sldIdLst"`
	}
	if err := xml.Unmarshal(data, &presDoc); err != nil {
		return nil, fmt.Errorf("%w: presentation: %v", ErrNotPPTX, err)
	}

	pres := &Presentation{
		SlideWPt: float64(presDoc.SldSz.CX) / emuPerPt,
		SlideHPt: float64(presDoc.SldSz.CY) / emuPerPt,
	}
	if pres.SlideWPt <= 0 || pres.SlideHPt <= 0 {
		// The spec default (4:3, 10in × 7.5in).
		pres.SlideWPt, pres.SlideHPt = 720, 540
	}

	rels := pkg.relsOf(mainPart)
	for _, s := range presDoc.SldIDLst.SldID {
		rel, ok := rels[s.RID]
		if !ok || rel.Type != relSlide {
			continue
		}
		slideData := pkg.read(rel.Target)
		if slideData == nil {
			continue
		}
		if slideHidden(slideData) {
			continue
		}
		ph := pkg.placeholderFrames(rel.Target)
		slideRels := pkg.relsOf(rel.Target)
		slide, err := parseSlidePart(slideData, ph, slideRels, pkg)
		if err != nil {
			return nil, err
		}
		pres.Slides = append(pres.Slides, slide)
	}
	return pres, nil
}

// slideHidden reports the p:sld root's show="0" attribute.
func slideHidden(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "sld" {
				for _, a := range se.Attr {
					if a.Name.Local == "show" && (a.Value == "0" || a.Value == "false") {
						return true
					}
				}
			}
			return false // only the root matters
		}
	}
}

// placeholderFrames resolves a slide's inherited placeholder frames: the
// layout's placeholders overlaid on the master's.
func (p *pkgReader) placeholderFrames(slidePart string) map[phKey]phFrame {
	frames := map[phKey]phFrame{}
	layoutRel, ok := p.firstRelOfType(slidePart, relSlideLayout)
	if !ok {
		return frames
	}
	if masterRel, ok := p.firstRelOfType(layoutRel.Target, relSlideMaster); ok {
		collectPlaceholders(p.read(masterRel.Target), frames)
	}
	collectPlaceholders(p.read(layoutRel.Target), frames)
	return frames
}

// collectPlaceholders scans a layout/master part for placeholder shapes with
// explicit transforms, overlaying onto frames.
func collectPlaceholders(data []byte, frames map[phKey]phFrame) {
	if data == nil {
		return
	}
	shapes, err := scanShapes(data)
	if err != nil {
		return
	}
	for _, sh := range shapes {
		if sh.ph == nil || !sh.frame.has {
			continue
		}
		frames[*sh.ph] = sh.frame
		// A type-keyed entry also registers under its index, and vice versa,
		// so a slide's ph that names only one of them still resolves.
		if sh.ph.typ != "" {
			frames[phKey{typ: sh.ph.typ}] = sh.frame
		}
		if sh.ph.idx != "" {
			frames[phKey{idx: sh.ph.idx}] = sh.frame
		}
	}
}

// rawShape is a scanned shape before model conversion.
type rawShape struct {
	kind    ShapeKind
	ph      *phKey
	frame   phFrame
	paras   []Paragraph
	blipRel string
	table   [][]TableCell
}

// scanShapes walks a slide/layout/master part's shape tree.
func scanShapes(data []byte) ([]rawShape, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var shapes []rawShape
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return shapes, nil
			}
			return nil, fmt.Errorf("%w: %v", ErrNotPPTX, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "sp":
			sh, err := parseSp(dec)
			if err != nil {
				return nil, err
			}
			shapes = append(shapes, sh)
		case "pic":
			sh, err := parsePic(dec)
			if err != nil {
				return nil, err
			}
			shapes = append(shapes, sh)
		case "graphicFrame":
			sh, err := parseGraphicFrame(dec)
			if err != nil {
				return nil, err
			}
			shapes = append(shapes, sh)
		}
	}
}

// consumeShape walks one shape element's tokens, dispatching by local name.
// end is the shape element's local name; handlers fire on start elements.
func consumeShape(dec *xml.Decoder, end string, handle func(se xml.StartElement) error) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrNotPPTX, end, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := handle(t); err != nil {
				return err
			}
			if t.Name.Local == end {
				depth++
			}
		case xml.EndElement:
			if t.Name.Local == end {
				depth--
			}
		}
	}
	return nil
}

// parseSp reads a p:sp: placeholder identity, transform, and text body.
func parseSp(dec *xml.Decoder) (rawShape, error) {
	sh := rawShape{kind: ShapeText}
	err := consumeShape(dec, "sp", func(se xml.StartElement) error {
		switch se.Name.Local {
		case "ph":
			key := phKey{}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "type":
					key.typ = a.Value
				case "idx":
					key.idx = a.Value
				}
			}
			sh.ph = &key
		case "off":
			sh.frame.x, sh.frame.y = emuAttr(se, "x"), emuAttr(se, "y")
			sh.frame.has = true
		case "ext":
			sh.frame.w, sh.frame.h = emuAttr(se, "cx"), emuAttr(se, "cy")
			sh.frame.has = true
		case "p":
			para, err := parseParagraph(dec)
			if err != nil {
				return err
			}
			sh.paras = append(sh.paras, para)
		}
		return nil
	})
	return sh, err
}

// parsePic reads a p:pic: transform + blip relationship.
func parsePic(dec *xml.Decoder) (rawShape, error) {
	sh := rawShape{kind: ShapePicture}
	err := consumeShape(dec, "pic", func(se xml.StartElement) error {
		switch se.Name.Local {
		case "off":
			sh.frame.x, sh.frame.y = emuAttr(se, "x"), emuAttr(se, "y")
			sh.frame.has = true
		case "ext":
			sh.frame.w, sh.frame.h = emuAttr(se, "cx"), emuAttr(se, "cy")
			sh.frame.has = true
		case "blip":
			for _, a := range se.Attr {
				if a.Name.Local == "embed" {
					sh.blipRel = a.Value
				}
			}
		}
		return nil
	})
	return sh, err
}

// parseGraphicFrame reads a p:graphicFrame: transform + a:tbl.
func parseGraphicFrame(dec *xml.Decoder) (rawShape, error) {
	sh := rawShape{kind: ShapeTable}
	err := consumeShape(dec, "graphicFrame", func(se xml.StartElement) error {
		switch se.Name.Local {
		case "off":
			sh.frame.x, sh.frame.y = emuAttr(se, "x"), emuAttr(se, "y")
			sh.frame.has = true
		case "ext":
			sh.frame.w, sh.frame.h = emuAttr(se, "cx"), emuAttr(se, "cy")
			sh.frame.has = true
		case "tr":
			row, err := parseTableRow(dec)
			if err != nil {
				return err
			}
			sh.table = append(sh.table, row)
		}
		return nil
	})
	return sh, err
}

// parseTableRow reads an a:tr's cells.
func parseTableRow(dec *xml.Decoder) ([]TableCell, error) {
	var row []TableCell
	err := consumeShape(dec, "tr", func(se xml.StartElement) error {
		if se.Name.Local != "tc" {
			return nil
		}
		cell := TableCell{GridSpan: 1, RowSpan: 1}
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "gridSpan":
				if n, err := strconv.Atoi(a.Value); err == nil && n > 1 {
					cell.GridSpan = n
				}
			case "rowSpan":
				if n, err := strconv.Atoi(a.Value); err == nil && n > 1 {
					cell.RowSpan = n
				}
			case "hMerge", "vMerge":
				if a.Value == "1" || a.Value == "true" {
					cell.Merged = true
				}
			}
		}
		if err := consumeShape(dec, "tc", func(inner xml.StartElement) error {
			if inner.Name.Local == "p" {
				para, err := parseParagraph(dec)
				if err != nil {
					return err
				}
				cell.Paragraphs = append(cell.Paragraphs, para)
			}
			return nil
		}); err != nil {
			return err
		}
		row = append(row, cell)
		return nil
	})
	return row, err
}

// parseParagraph reads one a:p: properties then runs.
func parseParagraph(dec *xml.Decoder) (Paragraph, error) {
	para := Paragraph{}
	depth := 1
	var curRun *Run
	inText := false
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return para, fmt.Errorf("%w: a:p: %v", ErrNotPPTX, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				depth++
			case "pPr":
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "lvl":
						para.Level, _ = strconv.Atoi(a.Value)
					case "algn":
						switch a.Value {
						case "ctr":
							para.Align = "center"
						case "r":
							para.Align = "right"
						case "just":
							para.Align = "justify"
						}
					}
				}
			case "buNone":
				para.Bullet = ""
			case "buChar":
				para.Bullet = "char"
			case "buAutoNum":
				para.Bullet = "auto"
			case "r":
				para.Runs = append(para.Runs, Run{})
				curRun = &para.Runs[len(para.Runs)-1]
			case "br":
				para.Runs = append(para.Runs, Run{Text: "\n"})
				curRun = nil
			case "rPr":
				if curRun != nil {
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "b":
							curRun.Bold = a.Value == "1" || a.Value == "true"
						case "i":
							curRun.Italic = a.Value == "1" || a.Value == "true"
						case "sz":
							if n, err := strconv.Atoi(a.Value); err == nil {
								curRun.SizePt = float64(n) / 100
							}
						}
					}
				}
			case "srgbClr":
				if curRun != nil && curRun.ColorRGB == "" {
					for _, a := range t.Attr {
						if a.Name.Local == "val" && len(a.Value) == 6 {
							curRun.ColorRGB = strings.ToUpper(a.Value)
						}
					}
				}
			case "t":
				inText = curRun != nil
			}
		case xml.CharData:
			if inText && curRun != nil {
				curRun.Text += string(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "r":
				curRun = nil
			case "p":
				depth--
			}
		}
	}
	return para, nil
}

// emuAttr reads an EMU attribute as points.
func emuAttr(se xml.StartElement, name string) float64 {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			if n, err := strconv.ParseInt(a.Value, 10, 64); err == nil {
				return float64(n) / emuPerPt
			}
		}
	}
	return 0
}

// parseSlidePart converts a slide's scanned shapes into the model, resolving
// placeholder inheritance and image bytes.
func parseSlidePart(data []byte, inherited map[phKey]phFrame, rels map[string]relationship, pkg *pkgReader) (Slide, error) {
	raw, err := scanShapes(data)
	if err != nil {
		return Slide{}, err
	}
	var slide Slide
	for _, sh := range raw {
		out := Shape{
			Kind:       sh.kind,
			Paragraphs: sh.paras,
			Table:      sh.table,
		}
		if sh.ph != nil {
			out.IsTitle = sh.ph.typ == "title" || sh.ph.typ == "ctrTitle"
		}
		frame := sh.frame
		if !frame.has && sh.ph != nil {
			// Inherit the layout/master placeholder frame: exact key, then
			// type-only, then idx-only.
			for _, key := range []phKey{*sh.ph, {typ: sh.ph.typ}, {idx: sh.ph.idx}} {
				if f, ok := inherited[key]; ok && f.has {
					frame = f
					break
				}
			}
		}
		out.XPt, out.YPt, out.WPt, out.HPt = frame.x, frame.y, frame.w, frame.h
		if sh.kind == ShapePicture && sh.blipRel != "" {
			if rel, ok := rels[sh.blipRel]; ok && rel.Type == relImage {
				if data := pkg.read(rel.Target); data != nil {
					out.Image = ImageRef{Data: data, Name: rel.Target}
				}
			}
		}
		// A pictureless pic or an empty text frame still occupies space; keep
		// text shapes with content, pictures with bytes, tables with rows.
		switch out.Kind {
		case ShapeText:
			if !hasText(out.Paragraphs) {
				continue
			}
		case ShapePicture:
			if out.Image.Data == nil {
				continue
			}
		case ShapeTable:
			if len(out.Table) == 0 {
				continue
			}
		}
		slide.Shapes = append(slide.Shapes, out)
	}
	return slide, nil
}

// hasText reports whether any run carries non-whitespace content.
func hasText(paras []Paragraph) bool {
	for _, p := range paras {
		for _, r := range p.Runs {
			if strings.TrimSpace(r.Text) != "" {
				return true
			}
		}
	}
	return false
}
