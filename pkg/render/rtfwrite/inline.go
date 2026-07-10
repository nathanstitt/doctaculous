package rtfwrite

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// paragraph emits one paragraph from a box's inline content.
func (w *writer) paragraph(b *cssbox.Box, extraProps string) {
	w.emitParagraph(w.inlineRuns(b), extraProps, justifyOf(b))
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
			// A literal is pre-built RTF from the image callback (a \pict blob,
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

// emitParagraph writes one paragraph group: {\pard <props> <runs>\par} —
// grouping consecutive runs that share a link target under one HYPERLINK
// field. An empty run list still emits an empty paragraph when props are set
// (an hr or a blank quote line carries meaning); otherwise it emits nothing.
func (w *writer) emitParagraph(runs []boxwalk.StyledRun, props, align string) {
	if len(runs) == 0 && props == "" {
		return
	}
	w.body.WriteString(`{\pard` + props + align + " ")
	w.writeRuns(runs)
	w.body.WriteString(`\par}` + "\n")
}

// writeRuns emits styled runs, wrapping href groups in HYPERLINK fields.
func (w *writer) writeRuns(runs []boxwalk.StyledRun) {
	for i := 0; i < len(runs); {
		if href := runs[i].Href; href != "" {
			j := i
			for j < len(runs) && runs[j].Href == href {
				j++
			}
			w.body.WriteString(`{\field{\*\fldinst{HYPERLINK "` + escapeRTF(href) + `"}}{\fldrslt {\cf1\ul `)
			for ; i < j; i++ {
				w.run(runs[i])
			}
			w.body.WriteString(`}}}`)
			continue
		}
		w.run(runs[i])
		i++
	}
}

// run writes one styled run: a literal (pre-built RTF — a picture or an
// alt-text run) verbatim, everything else as a formatting group.
func (w *writer) run(r boxwalk.StyledRun) {
	if r.Literal != "" {
		w.body.WriteString(r.Literal)
		return
	}
	var flags strings.Builder
	if r.Code {
		flags.WriteString(`\f1`) // the monospace font — the inline-code carrier
	}
	if r.Bold {
		flags.WriteString(`\b`)
	}
	if r.Italic {
		flags.WriteString(`\i`)
	}
	if r.Strike {
		flags.WriteString(`\strike`)
	}
	if flags.Len() == 0 {
		w.body.WriteString(escapeRTF(r.Text))
		return
	}
	w.body.WriteString("{" + flags.String() + " " + escapeRTF(r.Text) + "}")
}

// justifyOf maps the box's computed text-align to an alignment control ("" =
// default left, omitted).
func justifyOf(b *cssbox.Box) string {
	switch b.Style.TextAlign {
	case "center":
		return `\qc`
	case "right":
		return `\qr`
	case "justify":
		return `\qj`
	}
	return ""
}

// escapeRTF renders text as RTF: the syntax characters escaped, tabs and
// newlines as their control words, and non-ASCII as \u escapes (with the
// spec's "?" ANSI fallback; astral runes as a UTF-16 surrogate pair).
func escapeRTF(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\t':
			sb.WriteString(`\tab `)
		case r == '\n':
			sb.WriteString(`\line `)
		case r < 0x80:
			sb.WriteRune(r)
		case r <= 0xFFFF:
			fmt.Fprintf(&sb, `\u%d?`, int16(r))
		default:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&sb, `\u%d?\u%d?`, int16(r1), int16(r2))
		}
	}
	return sb.String()
}

// collapseWS maps every whitespace run (spaces, tabs, newlines) to a single
// space, keeping at most one leading/trailing space so word boundaries between
// adjacent runs survive.
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
