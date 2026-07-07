package htmlwrite

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// inline serializes a box's inline subtree to a single line of HTML. It flattens the
// subtree to a stream of styled runs — each run is a maximal span of text sharing
// bold/italic/strike/code/link state — then wraps each run in the appropriate inline tags
// (<a>/<strong>/<em>/<s>/<code>), coalescing adjacent runs that share styling so a single
// element split into several text leaves emits one tag pair. Whitespace is collapsed to
// single spaces (the normal-flow model); a <pre> box is handled by codeBlock, not here.
// Mirrors markdown/inline.go.
func (w *writer) inline(b *cssbox.Box) string {
	return w.inlineOpt(b, false)
}

// inlineOpt is inline with an option to suppress bold emphasis. Callers whose containing
// element already conveys bold — a heading (<h1>) or a table header cell (<th>) — pass
// suppressBold=true so a UA-stylesheet font-weight:bold does not add a redundant <strong>
// around the whole run. Italic/code/link/strike emphasis is still honored.
func (w *writer) inlineOpt(b *cssbox.Box, suppressBold bool) string {
	var runs []styledRun
	collectRuns(b, inlineState{}, &runs)
	runs = coalesce(runs)
	var sb strings.Builder
	for _, r := range runs {
		if suppressBold {
			r.bold = false
		}
		sb.WriteString(renderRun(r))
	}
	return collapseSpaces(sb.String())
}

// inlineState is the inherited inline styling in force at a point in the walk.
type inlineState struct {
	bold   bool
	italic bool
	strike bool
	code   bool
	href   string // non-empty inside a link
}

// styledRun is a run of text with its resolved inline styling. literal, when non-empty, is
// pre-formatted HTML (e.g. an <img> tag) emitted verbatim without escaping or emphasis
// wrapping; a literal run is never merged with a neighbor.
type styledRun struct {
	text    string
	literal string
	inlineState
}

// collectRuns walks b's inline subtree, threading the styling state, appending a styledRun
// for each text leaf. Bold/italic come from the computed style (so DOCX bold, which has no
// <strong> tag, is honored) as well as the SemTag; code and href come from SemTag/Href.
// Mirrors markdown/inline.go's collectRuns.
func collectRuns(b *cssbox.Box, st inlineState, out *[]styledRun) {
	// Refine the state from this box before descending.
	if b.Style.Bold {
		st.bold = true
	}
	if b.Style.Italic {
		st.italic = true
	}
	if b.Style.TextDecorationLine == "line-through" {
		st.strike = true
	}
	switch b.SemTag {
	case "strong":
		st.bold = true
	case "em":
		st.italic = true
	case "s":
		st.strike = true
	case "code":
		st.code = true
	case "a":
		if b.Href != "" {
			st.href = b.Href
		}
	}
	if b.Kind == cssbox.BoxText {
		if b.Text != "" {
			*out = append(*out, styledRun{text: b.Text, inlineState: st})
		}
		return
	}
	if b.Kind == cssbox.BoxReplaced && b.Replaced != nil && b.Replaced.Tag == "img" {
		*out = append(*out, styledRun{literal: imageMarkup(b.Replaced), inlineState: st})
		return
	}
	for _, c := range b.Children {
		collectRuns(c, st, out)
	}
}

// imageMarkup renders an <img> replaced box as an HTML <img> tag with src and (when
// present) alt attributes. A missing src yields "" (dropped).
func imageMarkup(rc *cssbox.ReplacedContent) string {
	src := rc.Attrs["src"]
	if src == "" {
		return ""
	}
	s := `<img src="` + escapeAttr(src) + `"`
	if alt, ok := rc.Attrs["alt"]; ok {
		s += ` alt="` + escapeAttr(alt) + `"`
	}
	return s + ">"
}

// coalesce merges adjacent runs with identical styling so a single element split into
// multiple text leaves emits one tag pair.
func coalesce(runs []styledRun) []styledRun {
	var out []styledRun
	for _, r := range runs {
		if n := len(out); n > 0 && r.literal == "" && out[n-1].literal == "" && out[n-1].inlineState == r.inlineState {
			out[n-1].text += r.text
			continue
		}
		out = append(out, r)
	}
	return out
}

// renderRun emits one styled run with its HTML tags. Tag nesting order is link-outermost,
// then strong, then em, then strike, then code, which produces well-formed markup like
// "<a href="url"><strong>text</strong></a>". Inline code is verbatim (unescaped inside
// <code>, as browsers render it literally); other text is HTML-escaped.
func renderRun(r styledRun) string {
	if r.literal != "" {
		return r.literal
	}
	var s string
	if r.code {
		s = "<code>" + escapeText(r.text) + "</code>"
	} else {
		s = escapeText(r.text)
	}
	if r.bold {
		s = "<strong>" + s + "</strong>"
	}
	if r.italic {
		s = "<em>" + s + "</em>"
	}
	if r.strike {
		s = "<s>" + s + "</s>"
	}
	if r.href != "" {
		s = `<a href="` + escapeAttr(r.href) + `">` + s + "</a>"
	}
	return s
}

// collapseSpaces collapses runs of whitespace to a single space and trims the ends, the
// normal-flow whitespace model for inline content.
func collapseSpaces(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
