package doctaculous

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/pptx"
)

// OpenPPTX reads and renders a PresentationML (.pptx) deck: each visible
// slide becomes one fixed-size page with its shapes absolutely positioned.
// For additional options use OpenPPTXFile.
func OpenPPTX(path string) (*Document, error) {
	return OpenPPTXFile(path)
}

// OpenPPTXFile reads and renders a .pptx file at path, applying any options.
func OpenPPTXFile(path string, opts ...HTMLOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open pptx %q: %w", path, err)
	}
	return OpenPPTXBytes(data, opts...)
}

// OpenPPTXBytes renders an in-memory presentation, applying any options, and
// returns a Document ready to rasterize or convert. One page per visible
// slide at the deck's slide size; text frames, pictures (embedded as data:
// URIs), and tables position absolutely within each page. For the structure
// writers (Markdown, DOCX, ...) shapes order title-first then top-to-bottom,
// so a converted deck reads sensibly. Animations/transitions/SmartArt/theme
// styling are not modeled (the content still extracts).
func OpenPPTXBytes(data []byte, opts ...HTMLOption) (*Document, error) {
	pres, err := pptx.OpenBytes(data)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: %w", err)
	}
	// The slide size is the page size; a caller's own WithPageSize wins.
	all := append([]HTMLOption{WithPageSize(pres.SlideWPt, pres.SlideHPt)}, opts...)
	doc, err := OpenHTMLBytes([]byte(presentationToHTML(pres)), all...)
	if err != nil {
		return nil, err
	}
	doc.format = FormatPPTX
	return doc, nil
}

// presentationToHTML synthesizes the deck as fixed-size pages with absolutely
// positioned shape frames.
func presentationToHTML(p *pptx.Presentation) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<style>\n")
	b.WriteString("body { margin: 0; font-family: sans-serif; }\n")
	fmt.Fprintf(&b, "@page { size: %spx %spx; margin: 0 }\n", pt(p.SlideWPt), pt(p.SlideHPt))
	fmt.Fprintf(&b, ".slide { position: relative; width: %spx; height: %spx; overflow: hidden }\n",
		pt(p.SlideWPt), pt(p.SlideHPt))
	b.WriteString(".shape { position: absolute }\n")
	b.WriteString("table { border-collapse: collapse; width: 100% }\n")
	b.WriteString("td, th { border: 1px solid #b0b6bc; padding: 2px 6px; vertical-align: top }\n")
	b.WriteString("ul, ol { margin: 0; padding-left: 1.2em }\n")
	b.WriteString("h2, p { margin: 0 }\n")
	b.WriteString("</style>\n</head>\n<body>\n")
	for i, slide := range p.Slides {
		style := ""
		if i > 0 {
			style = ` style="break-before: page"`
		}
		b.WriteString(`<div class="slide"` + style + ">\n")
		for _, sh := range readingOrder(slide.Shapes) {
			writeShape(&b, sh)
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// readingOrder sorts shapes for structure: titles first, then top-to-bottom,
// left-to-right. (Visual placement is absolute, so the order only affects the
// conversion writers and paint stacking.)
func readingOrder(shapes []pptx.Shape) []pptx.Shape {
	out := make([]pptx.Shape, len(shapes))
	copy(out, shapes)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsTitle != out[j].IsTitle {
			return out[i].IsTitle
		}
		if out[i].YPt != out[j].YPt {
			return out[i].YPt < out[j].YPt
		}
		return out[i].XPt < out[j].XPt
	})
	return out
}

// writeShape emits one absolutely positioned shape frame.
func writeShape(b *strings.Builder, sh pptx.Shape) {
	var pos strings.Builder
	fmt.Fprintf(&pos, "left:%spx;top:%spx", pt(sh.XPt), pt(sh.YPt))
	if sh.WPt > 0 {
		fmt.Fprintf(&pos, ";width:%spx", pt(sh.WPt))
	}
	if sh.HPt > 0 && sh.Kind != pptx.ShapeText {
		// A text frame keeps auto height (its content may overflow the
		// declared frame; a slide clips at the page anyway).
		fmt.Fprintf(&pos, ";height:%spx", pt(sh.HPt))
	}
	b.WriteString(`<div class="shape" style="` + pos.String() + `">`)
	switch sh.Kind {
	case pptx.ShapeText:
		writeTextShape(b, sh)
	case pptx.ShapePicture:
		mime := http.DetectContentType(sh.Image.Data)
		fmt.Fprintf(b, `<img src="data:%s;base64,%s" width="%s" height="%s">`,
			mime, base64.StdEncoding.EncodeToString(sh.Image.Data), pt(sh.WPt), pt(sh.HPt))
	case pptx.ShapeTable:
		writeTableShape(b, sh)
	}
	b.WriteString("</div>\n")
}

// writeTextShape renders paragraphs: the title as a heading, bulleted runs as
// (nested) lists, the rest as paragraphs.
func writeTextShape(b *strings.Builder, sh pptx.Shape) {
	i := 0
	for i < len(sh.Paragraphs) {
		para := sh.Paragraphs[i]
		if sh.IsTitle {
			b.WriteString("<h2" + alignAttr(para.Align) + ">")
			writeRuns(b, para.Runs)
			b.WriteString("</h2>")
			i++
			continue
		}
		if para.Bullet == "" {
			b.WriteString("<p" + alignAttr(para.Align) + ">")
			writeRuns(b, para.Runs)
			b.WriteString("</p>")
			i++
			continue
		}
		// A run of consecutive bulleted paragraphs becomes one (nested) list.
		j := i
		for j < len(sh.Paragraphs) && sh.Paragraphs[j].Bullet != "" {
			j++
		}
		writeBulletRun(b, sh.Paragraphs[i:j])
		i = j
	}
}

// writeBulletRun nests consecutive bulleted paragraphs by level. A deeper
// list opens INSIDE the still-open <li> (a nested list must be a child of its
// parent item, or the structure writers cannot see it), so an item's tag
// closes only when the walk returns to — or above — its level.
func writeBulletRun(b *strings.Builder, paras []pptx.Paragraph) {
	tag := func(p pptx.Paragraph) string {
		if p.Bullet == "auto" {
			return "ol"
		}
		return "ul"
	}
	var stack []string
	openLI := false
	closeOne := func() {
		if openLI {
			b.WriteString("</li>")
		}
		b.WriteString("</" + stack[len(stack)-1] + ">")
		stack = stack[:len(stack)-1]
		openLI = len(stack) > 0
	}
	for _, p := range paras {
		want := p.Level + 1
		for len(stack) > want {
			closeOne()
		}
		// A bullet-kind switch at the current level (• → numbered) closes the
		// open list and starts one of the right kind.
		if len(stack) == want && stack[want-1] != tag(p) {
			closeOne()
		}
		if len(stack) == want && openLI {
			b.WriteString("</li>")
			openLI = false
		}
		for len(stack) < want {
			t := tag(p)
			b.WriteString("<" + t + ">")
			stack = append(stack, t)
			openLI = false
		}
		b.WriteString("<li>")
		writeRuns(b, p.Runs)
		openLI = true
	}
	for len(stack) > 0 {
		closeOne()
	}
}

// writeTableShape renders an a:tbl grid with its spans.
func writeTableShape(b *strings.Builder, sh pptx.Shape) {
	b.WriteString("<table>")
	for _, row := range sh.Table {
		b.WriteString("<tr>")
		for _, cell := range row {
			if cell.Merged {
				continue
			}
			b.WriteString("<td")
			if cell.GridSpan > 1 {
				fmt.Fprintf(b, ` colspan="%d"`, cell.GridSpan)
			}
			if cell.RowSpan > 1 {
				fmt.Fprintf(b, ` rowspan="%d"`, cell.RowSpan)
			}
			b.WriteString(">")
			for _, para := range cell.Paragraphs {
				writeRuns(b, para.Runs)
			}
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// writeRuns emits a paragraph's runs with their direct formatting.
func writeRuns(b *strings.Builder, runs []pptx.Run) {
	for _, r := range runs {
		if r.Text == "\n" {
			b.WriteString("<br>")
			continue
		}
		var styles []string
		if r.SizePt > 0 {
			styles = append(styles, "font-size:"+pt(r.SizePt)+"px")
		}
		if r.ColorRGB != "" {
			styles = append(styles, "color:#"+r.ColorRGB)
		}
		open, closer := "", ""
		if len(styles) > 0 {
			open += `<span style="` + strings.Join(styles, ";") + `">`
			closer = "</span>" + closer
		}
		if r.Bold {
			open += "<b>"
			closer = "</b>" + closer
		}
		if r.Italic {
			open += "<i>"
			closer = "</i>" + closer
		}
		b.WriteString(open + htmlEscaper.Replace(r.Text) + closer)
	}
}

// alignAttr renders a paragraph alignment as a style attribute.
func alignAttr(align string) string {
	if align == "" {
		return ""
	}
	return ` style="text-align:` + align + `"`
}

// pt formats a point value tersely (px:pt is 1:1 in the layout engine).
func pt(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
