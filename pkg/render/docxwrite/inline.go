package docxwrite

import (
	"image/color"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// appendParagraph builds one w:p from a box's inline content and appends it to
// out (nothing is appended for a paragraph that would be empty). pStyle, when
// non-empty, is the paragraph style id (Heading1..6, Quote, Caption, ...) —
// the carrier of block semantics through the reader's round trip.
func (w *writer) appendParagraph(out *[]docx.Block, b *cssbox.Box, pStyle string) {
	jc, hasJC := justifyOf(b)
	if p := w.buildParagraph(w.inlineRuns(b), pStyle, nil, jc, hasJC); p != nil {
		*out = append(*out, docx.Block{Paragraph: p})
	}
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
			// A literal is the image callback's marker for a queued model child
			// (a drawing, or an alt-text run); it passes through.
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

// numPr is a paragraph's list membership: the numbering instance and level.
type numPr struct {
	numID, ilvl int
}

// buildParagraph assembles one docx.Paragraph: the paragraph properties
// (style, numbering, justification) plus the styled runs converted to model
// children. An empty run list still yields a paragraph when a pStyle is set
// (an hr or a blank quote line carries meaning); otherwise it yields nil.
func (w *writer) buildParagraph(runs []boxwalk.StyledRun, pStyle string, num *numPr, jc docx.Justify, hasJC bool) *docx.Paragraph {
	content := w.paraChildren(runs)
	if len(content) == 0 && pStyle == "" {
		return nil
	}
	props := docx.ParagraphProps{StyleID: pStyle, Justify: jc, HasJustify: hasJC}
	if num != nil {
		props.HasNum, props.NumID, props.ILvl = true, num.numID, num.ilvl
	}
	return &docx.Paragraph{Props: props, Content: content}
}

// paraChildren converts styled runs to model children, grouping consecutive
// runs that share a link target under one Hyperlink. A queued drawing inside a
// link is emitted BESIDE the group (the model's Hyperlink holds runs only):
// the image survives, the link target on the image itself is dropped — the
// reader discarded such drawings entirely, so nothing round-trippable is lost.
func (w *writer) paraChildren(runs []boxwalk.StyledRun) []docx.ParaChild {
	var out []docx.ParaChild
	var link *docx.Hyperlink // the open link group collecting runs
	flushLink := func() {
		if link != nil && len(link.Runs) > 0 {
			out = append(out, docx.ParaChild{Hyperlink: link})
		}
		link = nil
	}
	for _, r := range runs {
		if link != nil && r.Href != link.Target {
			flushLink()
		}
		if r.Href != "" && link == nil {
			link = &docx.Hyperlink{Target: r.Href}
		}
		if r.Literal != "" {
			child := w.popPending()
			switch {
			case link != nil && child.Run != nil:
				// Alt text inside a link joins the link's text.
				link.Runs = append(link.Runs, *child.Run)
			case link != nil:
				w.opts.Logf("docxwrite: image inside a hyperlink keeps the image but not the link target")
				flushLink()
				out = append(out, child)
				link = &docx.Hyperlink{Target: r.Href}
			default:
				out = append(out, child)
			}
			continue
		}
		run := docxRun(r)
		if link != nil {
			link.Runs = append(link.Runs, run)
			continue
		}
		out = append(out, docx.ParaChild{Run: &run})
	}
	flushLink()
	return out
}

// popPending pops the oldest queued model child (imageRun queues exactly one
// per literal marker run, so markers and queue entries pair off in order).
func (w *writer) popPending() docx.ParaChild {
	child := w.pending[0]
	w.pending = w.pending[1:]
	return child
}

// docxRun converts one styled text run to a model run with its direct run
// properties. Character formatting is always direct rPr (the reader models no
// character-style cascade); inline code additionally carries the CodeChar
// style id, which the reader maps back to the code semantic.
func docxRun(r boxwalk.StyledRun) docx.Run {
	var p docx.RunProps
	if r.Code {
		p.StyleID = "CodeChar"
		p.Family = monoFamily
	}
	if r.Bold {
		p.Bold, p.HasBold = true, true
	}
	if r.Italic {
		p.Italic, p.HasItalic = true, true
	}
	if r.Strike {
		p.Strike, p.HasStrike = true, true
	}
	if r.Href != "" {
		// Link chrome as direct properties (the Hyperlink character style is not
		// modeled by the reader).
		p.Color, p.HasColor = color.RGBA{R: 0x05, G: 0x63, B: 0xC1, A: 0xFF}, true
		p.Underline, p.HasUnderline = true, true
	}
	return docx.Run{Props: p, Text: r.Text}
}

// codeBlock emits a <pre> box as ONE CodeBlock-styled paragraph whose lines
// are separated by line-break runs. The reader lowers each break to a
// preserved newline, so the paragraph's raw text round-trips as a single
// fenced block. Runs are built parse-shaped (text OR break per run — the form
// the reader produces), so the model round-trips literally.
func (w *writer) codeBlock(b *cssbox.Box, out *[]docx.Block) {
	text := strings.TrimRight(boxwalk.RawText(b), "\n")
	if text == "" {
		return
	}
	mono := docx.RunProps{Family: monoFamily}
	p := &docx.Paragraph{Props: docx.ParagraphProps{StyleID: "CodeBlock"}}
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			p.Content = append(p.Content, docx.ParaChild{Run: &docx.Run{Props: mono, Break: docx.BreakLine}})
		}
		if line != "" {
			p.Content = append(p.Content, docx.ParaChild{Run: &docx.Run{Props: mono, Text: line}})
		}
	}
	*out = append(*out, docx.Block{Paragraph: p})
}

// horizontalRule emits an <hr> as an empty HorizontalRule-styled paragraph.
// The style carries a bottom border for Word; the reader maps the style id
// back to the hr semantic.
func (w *writer) horizontalRule(out *[]docx.Block) {
	*out = append(*out, docx.Block{Paragraph: &docx.Paragraph{
		Props: docx.ParagraphProps{StyleID: "HorizontalRule"},
	}})
}

// monoFamily is the monospace run-font selection shared by inline code and
// code blocks. "Courier New" resolves to the bundled monospace substitute in
// this engine and to a real monospace face in Word.
const monoFamily = "Courier New"

// justifyOf maps the box's computed text-align to a w:jc value (hasJC false =
// default left, omitted).
func justifyOf(b *cssbox.Box) (jc docx.Justify, hasJC bool) {
	switch b.Style.TextAlign {
	case "center":
		return docx.JustifyCenter, true
	case "right":
		return docx.JustifyRight, true
	case "justify":
		return docx.JustifyBoth, true
	}
	return docx.JustifyLeft, false
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
