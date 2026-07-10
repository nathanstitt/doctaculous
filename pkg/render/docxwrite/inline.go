package docxwrite

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// paragraph emits one w:p from a box's inline content. pStyle, when non-empty,
// is the paragraph style id (Heading1..6, Quote, ListParagraph, ...) — the
// carrier of block semantics through the reader's round trip.
func (w *writer) paragraph(b *cssbox.Box, pStyle string) {
	runs := w.inlineRuns(b)
	w.emitParagraph(runs, pStyle, "", justifyOf(b))
}

// inlineRuns collects and normalizes a box's inline content into styled runs:
// coalesced, whitespace-collapsed (the normal-flow model; <pre> goes through
// codeBlock instead), with empty runs dropped and the paragraph's leading and
// trailing space trimmed.
func (w *writer) inlineRuns(b *cssbox.Box) []boxwalk.StyledRun {
	var raw []boxwalk.StyledRun
	boxwalk.CollectRuns(b, boxwalk.InlineState{}, w.imageRun, &raw)
	raw = boxwalk.Coalesce(raw)

	runs := raw[:0]
	for _, r := range raw {
		if r.Literal != "" {
			// A literal is pre-built run XML from the image callback (a drawing,
			// or an alt-text run); it passes through verbatim.
			runs = append(runs, r)
			continue
		}
		r.Text = collapseWS(r.Text)
		if r.Text == "" {
			continue
		}
		runs = append(runs, r)
	}
	if len(runs) > 0 {
		if runs[0].Literal == "" {
			runs[0].Text = strings.TrimLeft(runs[0].Text, " ")
		}
		if last := len(runs) - 1; runs[last].Literal == "" {
			runs[last].Text = strings.TrimRight(runs[last].Text, " ")
		}
	}
	return runs
}

// emitParagraph writes one w:p: the paragraph properties (style, numbering,
// justification), then each styled run — grouping consecutive runs that share
// a link target under one w:hyperlink. An empty run list still emits an empty
// paragraph when a pStyle is set (an hr or a blank quote line carries meaning);
// otherwise it emits nothing.
func (w *writer) emitParagraph(runs []boxwalk.StyledRun, pStyle, numPr, jc string) {
	if len(runs) == 0 && pStyle == "" {
		return
	}
	w.body.WriteString("<w:p>")
	if pStyle != "" || numPr != "" || jc != "" {
		w.body.WriteString("<w:pPr>")
		if pStyle != "" {
			w.body.WriteString(`<w:pStyle w:val="` + escAttr.Replace(pStyle) + `"/>`)
		}
		w.body.WriteString(numPr)
		if jc != "" {
			w.body.WriteString(`<w:jc w:val="` + jc + `"/>`)
		}
		w.body.WriteString("</w:pPr>")
	}
	for i := 0; i < len(runs); {
		if href := runs[i].Href; href != "" {
			j := i
			for j < len(runs) && runs[j].Href == href {
				j++
			}
			id := w.hyperlinkRel(href)
			w.body.WriteString(`<w:hyperlink r:id="` + escAttr.Replace(id) + `">`)
			for ; i < j; i++ {
				w.run(runs[i])
			}
			w.body.WriteString("</w:hyperlink>")
			continue
		}
		w.run(runs[i])
		i++
	}
	w.body.WriteString("</w:p>")
}

// run writes one styled run: a literal (pre-built run XML — a drawing or an
// alt-text run) verbatim, everything else through the property emitter.
func (w *writer) run(r boxwalk.StyledRun) {
	if r.Literal != "" {
		w.body.WriteString(r.Literal)
		return
	}
	w.textRun(r)
}

// textRun writes one w:r with its direct run properties. Character formatting
// is always direct rPr (the reader models no character-style cascade); inline
// code additionally carries the CodeChar style id, which the reader maps back
// to the code semantic.
func (w *writer) textRun(r boxwalk.StyledRun) {
	w.body.WriteString("<w:r>")
	var props strings.Builder
	if r.Code {
		props.WriteString(`<w:rStyle w:val="CodeChar"/>` + monoFonts)
	}
	if r.Bold {
		props.WriteString("<w:b/>")
	}
	if r.Italic {
		props.WriteString("<w:i/>")
	}
	if r.Strike {
		props.WriteString("<w:strike/>")
	}
	if r.Href != "" {
		// Link chrome as direct properties (the Hyperlink character style is not
		// modeled by the reader).
		props.WriteString(`<w:color w:val="0563C1"/><w:u w:val="single"/>`)
	}
	if props.Len() > 0 {
		w.body.WriteString("<w:rPr>" + props.String() + "</w:rPr>")
	}
	w.body.WriteString(`<w:t xml:space="preserve">` + escText.Replace(r.Text) + "</w:t></w:r>")
}

// codeBlock emits a <pre> box as ONE CodeBlock-styled paragraph whose lines are
// separated by w:br. The reader lowers each w:br to a preserved newline, so the
// paragraph's raw text round-trips as a single fenced block.
func (w *writer) codeBlock(b *cssbox.Box) {
	text := strings.TrimRight(boxwalk.RawText(b), "\n")
	if text == "" {
		return
	}
	w.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="CodeBlock"/></w:pPr><w:r><w:rPr>` + monoFonts + `</w:rPr>`)
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			w.body.WriteString("<w:br/>")
		}
		w.body.WriteString(`<w:t xml:space="preserve">` + escText.Replace(line) + "</w:t>")
	}
	w.body.WriteString("</w:r></w:p>")
}

// horizontalRule emits an <hr> as an empty HorizontalRule-styled paragraph.
// The style carries a bottom border for Word; the reader maps the style id back
// to the hr semantic (paragraph borders themselves are not modeled).
func (w *writer) horizontalRule() {
	w.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="HorizontalRule"/></w:pPr></w:p>`)
}

// monoFonts is the monospace run-font selection shared by inline code and code
// blocks. "Courier New" resolves to the bundled monospace substitute in this
// engine and to a real monospace face in Word.
const monoFonts = `<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`

// justifyOf maps the box's computed text-align to a w:jc value ("" = default
// left, omitted).
func justifyOf(b *cssbox.Box) string {
	switch b.Style.TextAlign {
	case "center":
		return "center"
	case "right":
		return "right"
	case "justify":
		return "both"
	}
	return ""
}

// collapseWS maps every whitespace run (spaces, tabs, newlines) to a single
// space, keeping at most one leading/trailing space so word boundaries between
// adjacent runs survive. (Box generation pre-collapses HTML text; this also
// normalizes tree sources that preserve raw text.)
func collapseWS(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			inSpace = true
			continue
		}
		if inSpace {
			sb.WriteByte(' ')
			inSpace = false
		}
		sb.WriteRune(r)
	}
	if inSpace {
		sb.WriteByte(' ')
	}
	return sb.String()
}
