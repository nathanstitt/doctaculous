package htmlwrite

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/render/internal/boxwalk"
)

// inline serializes a box's inline subtree to a single line of HTML. It flattens the
// subtree to a stream of styled runs — each run is a maximal span of text sharing
// bold/italic/strike/code/link state — then wraps each run in the appropriate inline tags
// (<a>/<strong>/<em>/<s>/<code>), coalescing adjacent runs that share styling so a single
// element split into several text leaves emits one tag pair. Whitespace is collapsed to
// single spaces (the normal-flow model); a <pre> box is handled by codeBlock, not here.
func (w *writer) inline(b *cssbox.Box) string {
	return w.inlineOpt(b, false)
}

// inlineOpt is inline with an option to suppress bold emphasis. Callers whose containing
// element already conveys bold — a heading (<h1>) or a table header cell (<th>) — pass
// suppressBold=true so a UA-stylesheet font-weight:bold does not add a redundant <strong>
// around the whole run. Italic/code/link/strike emphasis is still honored.
func (w *writer) inlineOpt(b *cssbox.Box, suppressBold bool) string {
	var runs []boxwalk.StyledRun
	boxwalk.CollectRuns(b, boxwalk.InlineState{}, w.imageMarkup, &runs)
	runs = boxwalk.Coalesce(runs)
	var sb strings.Builder
	for _, r := range runs {
		if suppressBold {
			r.Bold = false
		}
		sb.WriteString(renderRun(r))
	}
	return boxwalk.CollapseSpaces(sb.String())
}

// imageMarkup renders an <img> replaced box as an HTML <img> tag with src and (when
// present) alt attributes. A missing src yields "" (dropped); the ImageSrc hook, when
// set, rewrites the reference first.
func (w *writer) imageMarkup(rc *cssbox.ReplacedContent) string {
	src := rc.Attrs["src"]
	if src == "" {
		return ""
	}
	if w.opts.ImageSrc != nil {
		src = w.opts.ImageSrc(src)
		if src == "" {
			return ""
		}
	}
	s := `<img src="` + escapeAttr(src) + `"`
	if alt, ok := rc.Attrs["alt"]; ok {
		s += ` alt="` + escapeAttr(alt) + `"`
	}
	if w.opts.XHTML {
		return s + "/>"
	}
	return s + ">"
}

// renderRun emits one styled run with its HTML tags. Tag nesting order, outermost to
// innermost, is link, then strike, then em, then strong, then code, which produces
// well-formed markup like "<a href="url"><s><strong>text</strong></s></a>". Inline code
// is verbatim (unescaped inside <code>, as browsers render it literally); other text is
// HTML-escaped.
func renderRun(r boxwalk.StyledRun) string {
	if r.Literal != "" {
		return r.Literal
	}
	var s string
	if r.Code {
		s = "<code>" + escapeText(r.Text) + "</code>"
	} else {
		s = escapeText(r.Text)
	}
	if r.Bold {
		s = "<strong>" + s + "</strong>"
	}
	if r.Italic {
		s = "<em>" + s + "</em>"
	}
	if r.Strike {
		s = "<s>" + s + "</s>"
	}
	if r.Href != "" {
		s = `<a href="` + escapeAttr(r.Href) + `">` + s + "</a>"
	}
	return s
}
