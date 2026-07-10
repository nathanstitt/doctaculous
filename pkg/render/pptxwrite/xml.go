package pptxwrite

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// emuPerPt converts points to EMU.
const emuPerPt = 12700

// xmlDecl is the standard part prolog.
const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// escText escapes character data; escAttr additionally escapes quotes.
var (
	escText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	escAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

// assemble serializes the accumulated deck as a deterministic OPC package
// (fixed timestamps, parts in a fixed order — the gen-fixture shape our own
// reader is tested against).
func (w *writer) assemble() ([]byte, error) {
	type part struct {
		name string
		data []byte
	}
	var parts []part
	add := func(name, data string) { parts = append(parts, part{name, []byte(data)}) }

	// [Content_Types].xml
	var ct strings.Builder
	ct.WriteString(xmlDecl)
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Default Extension="png" ContentType="image/png"/>
<Default Extension="jpeg" ContentType="image/jpeg"/>
<Default Extension="gif" ContentType="image/gif"/>
<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
`)
	for i := range w.slides {
		fmt.Fprintf(&ct, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`+"\n", i+1)
	}
	ct.WriteString("</Types>\n")
	add("[Content_Types].xml", ct.String())

	add("_rels/.rels", xmlDecl+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>
`)

	// presentation.xml + rels.
	var sldIds, presRels strings.Builder
	for i := range w.slides {
		fmt.Fprintf(&sldIds, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+1)
		fmt.Fprintf(&presRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`+"\n", i+1, i+1)
	}
	add("ppt/presentation.xml", xmlDecl+fmt.Sprintf(
		`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst>%s</p:sldIdLst><p:sldSz cx="%d" cy="%d"/></p:presentation>`,
		sldIds.String(), emu(w.opts.SlideWidthPt), emu(w.opts.SlideHeightPt)))
	add("ppt/_rels/presentation.xml.rels", xmlDecl+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
`+presRels.String()+`</Relationships>
`)

	// Minimal layout + master (no placeholders — every shape carries its own
	// transform, so nothing needs inheritance).
	add("ppt/slideLayouts/slideLayout1.xml", xmlDecl+`<p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld></p:sldLayout>`)
	add("ppt/slideLayouts/_rels/slideLayout1.xml.rels", xmlDecl+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>
`)
	add("ppt/slideMasters/slideMaster1.xml", xmlDecl+`<p:sldMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld><p:sldLayoutIdLst/></p:sldMaster>`)

	// Slides + their rels.
	for i, s := range w.slides {
		xml, mediaRefs := w.slideXML(s)
		add(fmt.Sprintf("ppt/slides/slide%d.xml", i+1), xml)
		var rels strings.Builder
		rels.WriteString(xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rIdL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
`)
		for _, mi := range mediaRefs {
			fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`+"\n",
				100+mi, w.mediaParts[mi].name)
		}
		rels.WriteString("</Relationships>\n")
		add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), rels.String())
	}
	for _, m := range w.mediaParts {
		parts = append(parts, part{"ppt/media/" + m.name, m.data})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(p.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// slideXML serializes one slide: the title placeholder, then the body shapes
// stacked top to bottom (reading order = y order, which the frontend re-sorts
// by). Returns the media indices the slide references.
func (w *writer) slideXML(s *slideAcc) (string, []int) {
	slideW, slideH := w.opts.SlideWidthPt, w.opts.SlideHeightPt
	marginX := slideW * 0.05
	marginY := slideH * 0.05
	contentW := slideW - 2*marginX

	var sb strings.Builder
	sb.WriteString(xmlDecl)
	sb.WriteString(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	sb.WriteString(`<p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`)

	id := 2
	y := marginY
	var mediaRefs []int
	if s.title != nil {
		titleH := slideH * 0.12
		fmt.Fprintf(&sb, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>`, id)
		id++
		sb.WriteString(frameXML(marginX, y, contentW, titleH))
		sb.WriteString(`<p:txBody><a:bodyPr/><a:lstStyle/>`)
		writeParaXML(&sb, para{runs: s.title}, 30)
		sb.WriteString(`</p:txBody></p:sp>`)
		y += titleH + marginY
	}

	for _, shape := range s.shapes {
		switch sh := shape.(type) {
		case textShape:
			h := float64(len(sh.paras)) * 24
			fmt.Fprintf(&sb, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Content"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`, id)
			id++
			sb.WriteString(frameXML(marginX, y, contentW, h))
			sb.WriteString(`<p:txBody><a:bodyPr/><a:lstStyle/>`)
			for _, p := range sh.paras {
				writeParaXML(&sb, p, 0)
			}
			sb.WriteString(`</p:txBody></p:sp>`)
			y += h + marginY/2
		case tableShape:
			h := float64(len(sh.grid.Slots)) * 28
			fmt.Fprintf(&sb, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>`, id)
			id++
			fmt.Fprintf(&sb, `<p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm>`,
				emu(marginX), emu(y), emu(contentW), emu(h))
			sb.WriteString(`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">`)
			writeTableXML(&sb, sh.grid, contentW, w)
			sb.WriteString(`</a:graphicData></a:graphic></p:graphicFrame>`)
			y += h + marginY/2
		case picShape:
			wPt := float64(sh.pxW) * 0.75 // px (96dpi) -> pt
			hPt := float64(sh.pxH) * 0.75
			if wPt > contentW && wPt > 0 {
				hPt *= contentW / wPt
				wPt = contentW
			}
			fmt.Fprintf(&sb, `<p:pic><p:nvPicPr><p:cNvPr id="%d" name="Picture" descr="%s"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr>`,
				id, escAttr.Replace(sh.alt))
			id++
			fmt.Fprintf(&sb, `<p:blipFill><a:blip r:embed="rId%d"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>`, 100+sh.mediaIdx)
			fmt.Fprintf(&sb, `<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`,
				emu(marginX), emu(y), emu(wPt), emu(hPt))
			mediaRefs = append(mediaRefs, sh.mediaIdx)
			y += hPt + marginY/2
		}
	}
	if y > slideH {
		w.opts.Logf("pptxwrite: slide content overflows its frame (clipped visually; content preserved)")
	}
	sb.WriteString(`</p:spTree></p:cSld></p:sld>`)
	return sb.String(), dedupeInts(mediaRefs)
}

// frameXML renders a shape's spPr with its transform (points -> EMU).
func frameXML(x, y, w, h float64) string {
	return fmt.Sprintf(`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>`,
		emu(x), emu(y), emu(w), emu(h))
}

// writeParaXML renders one a:p. sizePt > 0 forces a run font size (titles).
func writeParaXML(sb *strings.Builder, p para, sizePt float64) {
	sb.WriteString("<a:p><a:pPr")
	if p.level > 0 {
		fmt.Fprintf(sb, ` lvl="%d"`, p.level)
	}
	if a := algnOf(p.align); a != "" {
		fmt.Fprintf(sb, ` algn="%s"`, a)
	}
	sb.WriteString(">")
	switch p.bullet {
	case "char":
		sb.WriteString(`<a:buChar char="•"/>`)
	case "auto":
		sb.WriteString(`<a:buAutoNum type="arabicPeriod"/>`)
	default:
		sb.WriteString(`<a:buNone/>`)
	}
	sb.WriteString("</a:pPr>")
	for _, r := range p.runs {
		sb.WriteString("<a:r><a:rPr lang=\"en-US\"")
		if sizePt > 0 {
			fmt.Fprintf(sb, ` sz="%d"`, int(sizePt*100))
		}
		if r.Bold {
			sb.WriteString(` b="1"`)
		}
		if r.Italic {
			sb.WriteString(` i="1"`)
		}
		sb.WriteString(`/><a:t>` + escText.Replace(r.Text) + `</a:t></a:r>`)
	}
	sb.WriteString("</a:p>")
}

// writeTableXML renders the occupancy grid as an a:tbl: origin cells carry
// gridSpan/rowSpan, covered slots are hMerge/vMerge continuation cells — the
// exact shape pkg/pptx reconstructs spans from.
func writeTableXML(sb *strings.Builder, grid boxwalk.Grid, widthPt float64, w *writer) {
	sb.WriteString(`<a:tbl><a:tblPr/><a:tblGrid>`)
	colW := emu(widthPt) / int64(grid.Cols)
	for c := 0; c < grid.Cols; c++ {
		fmt.Fprintf(sb, `<a:gridCol w="%d"/>`, colW)
	}
	sb.WriteString(`</a:tblGrid>`)
	for r := range grid.Slots {
		fmt.Fprintf(sb, `<a:tr h="%d">`, emu(28))
		for c := 0; c < grid.Cols; c++ {
			idx := grid.Slots[r][c]
			if idx < 0 {
				sb.WriteString(`<a:tc><a:txBody><a:bodyPr/><a:p/></a:txBody><a:tcPr/></a:tc>`)
				continue
			}
			cell := grid.Cells[idx]
			origin := cell.Row == r && cell.Col == c
			sb.WriteString("<a:tc")
			if origin {
				if cell.ColSpan > 1 {
					fmt.Fprintf(sb, ` gridSpan="%d"`, cell.ColSpan)
				}
				if cell.RowSpan > 1 {
					fmt.Fprintf(sb, ` rowSpan="%d"`, cell.RowSpan)
				}
			} else {
				if cell.ColSpan > 1 && c > cell.Col {
					sb.WriteString(` hMerge="1"`)
				}
				if cell.RowSpan > 1 && r > cell.Row {
					sb.WriteString(` vMerge="1"`)
				}
			}
			sb.WriteString(">")
			if origin {
				sb.WriteString(`<a:txBody><a:bodyPr/>`)
				runs := w.inlineRuns(cell.Box)
				if grid.HeaderRow[r] {
					for i := range runs {
						runs[i].Bold = true
					}
				}
				writeParaXML(sb, para{runs: runs}, 0)
				sb.WriteString(`</a:txBody>`)
			} else {
				sb.WriteString(`<a:txBody><a:bodyPr/><a:p/></a:txBody>`)
			}
			sb.WriteString(`<a:tcPr/></a:tc>`)
		}
		sb.WriteString("</a:tr>")
	}
	sb.WriteString(`</a:tbl>`)
}

// algnOf maps the model alignment to the ST_TextAlignType token.
func algnOf(align string) string {
	switch align {
	case "center":
		return "ctr"
	case "right":
		return "r"
	case "justify":
		return "just"
	}
	return ""
}

// emu converts points to EMU.
func emu(pt float64) int64 { return int64(pt*emuPerPt + 0.5) }

// dedupeInts returns the unique values of in, order-preserving.
func dedupeInts(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
