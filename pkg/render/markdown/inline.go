package markdown

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// inline serializes a box's inline subtree to a single line of Markdown (or plain
// text). It flattens the subtree to a stream of styled runs — each run is a maximal
// span of text sharing bold/italic/code/link state — then emits the appropriate
// markers around each run, coalescing adjacent runs that share a marker so we do not
// emit "**a****b**". Whitespace is collapsed to single spaces (the normal-flow model);
// a <pre> box is handled by codeBlock, not here.
func (w *writer) inline(b *cssbox.Box) string {
	return w.inlineOpt(b, false)
}

// inlineOpt is inline with an option to suppress bold emphasis. Callers whose context
// already implies bold — a heading (the "#" conveys it) or a table header cell — pass
// suppressBold=true so a UA-stylesheet font-weight:bold does not add stray "**" markers
// around the whole run. Italic/code/link emphasis is still honored.
func (w *writer) inlineOpt(b *cssbox.Box, suppressBold bool) string {
	var runs []styledRun
	collectRuns(b, inlineState{}, &runs)
	runs = coalesce(runs)
	var sb strings.Builder
	for _, r := range runs {
		if suppressBold {
			r.bold = false
		}
		sb.WriteString(w.renderRun(r))
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

// styledRun is a run of text with its resolved inline styling. literal, when non-empty,
// is pre-formatted Markdown (e.g. an image "![](...)") emitted verbatim without escaping
// or emphasis markers; a literal run is never merged with a neighbor.
type styledRun struct {
	text    string
	literal string
	inlineState
}

// collectRuns walks b's inline subtree, threading the styling state, appending a
// styledRun for each text leaf. Bold/italic come from the computed style (so DOCX
// bold, which has no <strong> tag, is honored); code and href come from SemTag/Href.
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

// imageMarkup renders an <img> replaced box as a Markdown image "![alt](src)". A
// missing src yields "" (dropped); a missing alt yields an empty label.
func imageMarkup(rc *cssbox.ReplacedContent) string {
	src := rc.Attrs["src"]
	if src == "" {
		return ""
	}
	return "![" + escapeText(rc.Attrs["alt"]) + "](" + escapeURL(src) + ")"
}

// coalesce merges adjacent runs with identical styling so a single element split into
// multiple text leaves emits one marker pair.
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

// renderRun emits one styled run with its Markdown markers. In plain mode all markers
// and the link URL are dropped, leaving the text (a link renders as its label). Marker
// nesting order is link-outermost, then bold, then italic, then code, which produces
// well-formed GFM like "[**text**](url)".
func (w *writer) renderRun(r styledRun) string {
	if r.literal != "" {
		if w.opts.Plain {
			return plainLiteral(r.literal)
		}
		return r.literal
	}
	if w.opts.Plain {
		return r.text
	}
	s := escapeText(r.text)
	if r.code {
		// Inline code is verbatim (no escaping inside backticks); use the raw text.
		s = "`" + r.text + "`"
	}
	if r.bold {
		s = "**" + s + "**"
	}
	if r.italic {
		s = "_" + s + "_"
	}
	if r.strike {
		s = "~~" + s + "~~"
	}
	if r.href != "" {
		s = "[" + s + "](" + escapeURL(r.href) + ")"
	}
	return s
}

// rawText concatenates every text leaf under b verbatim (no whitespace collapsing),
// used for <pre> content where whitespace is significant.
func rawText(b *cssbox.Box) string {
	var sb strings.Builder
	var walk func(*cssbox.Box)
	walk = func(n *cssbox.Box) {
		if n.Kind == cssbox.BoxText {
			sb.WriteString(n.Text)
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(b)
	return sb.String()
}

// collapseSpaces collapses runs of whitespace to a single space and trims the ends,
// the normal-flow whitespace model for inline content.
func collapseSpaces(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// mdEscape are the characters escaped in ordinary Markdown text so literal syntax
// characters do not trigger formatting. Kept intentionally small: over-escaping harms
// readability, so we escape only the markers this writer itself produces plus the ones
// that most commonly start a line-level construct.
var mdEscaper = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"[", `\[`,
	"]", `\]`,
)

// escapeText escapes Markdown metacharacters in ordinary text.
func escapeText(s string) string { return mdEscaper.Replace(s) }

// plainLiteral reduces a pre-formatted Markdown literal to plain text: an image
// "![alt](src)" becomes its alt label (or "" when empty). Other literals pass through.
func plainLiteral(lit string) string {
	if strings.HasPrefix(lit, "![") {
		if end := strings.Index(lit, "]("); end >= 0 {
			return lit[2:end]
		}
	}
	return lit
}

// escapeURL escapes the characters that would break a Markdown link destination
// (spaces and parens); other characters are left as-is so URLs stay readable.
func escapeURL(s string) string {
	r := strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29")
	return r.Replace(s)
}
