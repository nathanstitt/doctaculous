package svg

import (
	"fmt"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

// docIndex is the whole-document information the scene walk needs up front:
// author stylesheets (which may appear anywhere, including after the
// elements they style), and the id/defs tables that url(#...) references
// resolve through. Built by one pre-order walk before scene building; owned
// by sceneBuilder and discarded when Parse returns.
//
// PRs 3-5 (gradients, <use>, clipPath) all resolve url(#...) references and
// extend this structure as their consumers; buildIndex itself stays
// agnostic about what a caller does with ids/defs beyond looking them up.
type docIndex struct {
	// sheets holds every usable author stylesheet, in document order. The
	// cascade (a later PR) applies them in this order so later sheets win
	// ties, matching CSS's source-order tie-break.
	sheets []css.Stylesheet

	// ids maps an id attribute to the first element in document order that
	// carries it, for every SVG-namespace element with a non-empty id
	// anywhere in the document (including inside <defs>). This is the table
	// an id selector (#foo) or a same-document url(#foo) fragment resolves
	// through.
	ids map[string]*element

	// defs maps an id to the element that carries it, restricted to
	// elements that are descendants of a <defs>. PRs 3-5 use this table (not
	// ids) to resolve references that are only meaningful when the
	// referenced node lives in a <defs> subtree (e.g. a <linearGradient> or
	// <clipPath> definition), so a same-id element painted directly into the
	// visible tree cannot be mistaken for a definition.
	defs map[string]*element
}

// buildIndex walks root's subtree once, in document (pre-)order, collecting
// every <style> element's stylesheet, the whole-document id table, and the
// defs-scoped id table. Unlike the scene walk, this walk descends into
// <defs> and into every SVG-namespace element regardless of "display",
// because a <style> element inside a hidden or deferred subtree still
// applies to the whole document.
//
// warn is called at most once per distinct key (the caller, typically
// sceneBuilder.warnOnceMsg, is expected to dedupe by key) so a document with
// many repeated problems logs one line per problem kind, not one per
// occurrence. buildIndex never panics on malformed input: a nil root yields
// an empty index, and elements with a nil attrs map, empty text, or
// pathological nesting are all handled without special-casing by the
// element type's own zero values.
func buildIndex(root *element, warn func(key, msg string)) *docIndex {
	idx := &docIndex{
		ids:  make(map[string]*element),
		defs: make(map[string]*element),
	}
	if root == nil {
		return idx
	}
	if warn == nil {
		warn = func(string, string) {}
	}
	walkIndex(root, false, idx, warn)
	return idx
}

// walkIndex recurses pre-order over el and its kids, populating idx. inDefs
// is true when el is a descendant of a <defs> element (not when el is the
// <defs> itself, so the <defs> element's own id — if any — is recorded only
// in ids, matching "elements that are descendants of a <defs>").
func walkIndex(el *element, inDefs bool, idx *docIndex, warn func(key, msg string)) {
	if el.space == svgNS {
		if el.id != "" {
			recordID(el, inDefs, idx, warn)
		}
		if el.local == "style" {
			indexStyleSheet(el, idx, warn)
		}
	}

	childInDefs := inDefs || (el.space == svgNS && el.local == "defs")
	for _, kid := range el.kids {
		walkIndex(kid, childInDefs, idx, warn)
	}
}

// recordID adds el to idx.ids (first occurrence wins; a duplicate warns
// once per id) and, when inDefs, also to idx.defs.
func recordID(el *element, inDefs bool, idx *docIndex, warn func(key, msg string)) {
	if _, dup := idx.ids[el.id]; dup {
		warn("svg-dup-id-"+el.id, fmt.Sprintf("svg: duplicate id %q, using first occurrence", el.id))
	} else {
		idx.ids[el.id] = el
	}
	if inDefs {
		if _, dup := idx.defs[el.id]; !dup {
			idx.defs[el.id] = el
		}
	}
}

// indexStyleSheet parses a <style> element's text content into idx.sheets
// when its type attribute (if any) names CSS, appending in document order.
// A non-CSS type is skipped with a warn-once; an @import in the text is
// skipped with a separate warn-once because pkg/css does not resolve
// imports and no loader exists yet to fetch them.
func indexStyleSheet(el *element, idx *docIndex, warn func(key, msg string)) {
	if !isCSSType(el.attrs["type"]) {
		warn("svg-style-type", fmt.Sprintf("svg: <style type=%q> is not text/css (skipped)", el.attrs["type"]))
		return
	}
	src := el.text
	if strings.TrimSpace(src) == "" {
		return
	}
	if hasImportRule(src) {
		warn("svg-style-import", "svg: <style> @import is not supported (skipped)")
	}
	idx.sheets = append(idx.sheets, css.Parse(src))
}

// isCSSType reports whether a <style> element's type attribute (possibly
// absent) names a CSS stylesheet: absent, empty, or "text/css" ignoring case
// and any trailing ";charset=..."-style parameters. Anything else (e.g.
// "text/nonsense") is not CSS and the element is skipped.
func isCSSType(t string) bool {
	if t == "" {
		return true
	}
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = t[:i]
	}
	return strings.EqualFold(strings.TrimSpace(t), "text/css")
}

// hasImportRule reports whether src contains an @import at-rule, scanning
// the raw text before parsing. pkg/css.Parse drops unknown/unhandled
// at-rules silently, so detecting @import there is not possible; this scan
// runs first so buildIndex can still warn about it. The check is
// deliberately simple (case-insensitive substring match on "@import"
// preceded only by whitespace/comment-safe boundaries in practice) since a
// false positive inside e.g. a string value is harmless — it only affects
// whether a warning is logged, not what gets parsed.
func hasImportRule(src string) bool {
	lower := strings.ToLower(src)
	return strings.Contains(lower, "@import")
}
