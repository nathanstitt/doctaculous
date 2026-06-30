package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// pageStrings holds, for one page, the CSS string values needed to resolve string(name,
// <keyword>): the value carried INTO the page (Start), the first value SET on the page
// (First), and the last value in effect through the page end (Last == the default). A
// name absent from a map means "not set"; First lacking a name means no setter on that
// page (the keyword resolves to Start, the carried value).
type pageStrings struct {
	Start map[string]string // running value at the page's start (before this page's setters)
	First map[string]string // first value SET on this page (only names set on the page)
	Last  map[string]string // running value through the page end (default for string(name))
}

// stringValue resolves string(name, keyword) for this page. keyword is "" (==last),
// "last", "first", "start", or "first-except". Unknown keyword falls back to last.
func (ps pageStrings) stringValue(name, keyword string) string {
	switch keyword {
	case "first":
		if v, ok := ps.First[name]; ok {
			return v
		}
		return ps.Start[name] // no setter on this page -> the carried value
	case "start":
		return ps.Start[name]
	case "first-except":
		// Like first, but blank when the page only carries the value (no setter here) —
		// used to suppress a header on a continuation page.
		if v, ok := ps.First[name]; ok {
			return v
		}
		return ""
	default: // "" or "last"
		return ps.Last[name]
	}
}

// buildStringSnapshots returns, for each page bucket, the CSS string values needed to
// resolve CSS GCPM `string(name, <keyword>)` per page: the value carried INTO the page
// (Start), the first value SET on the page (First), and the running value through the
// page end (Last). The values come from each block's string-set assignments applied in
// document order; a page with no new setter carries the prior page's running values
// forward (Start == Last == the carried value, First empty for that name).
func buildStringSnapshots(buckets []pageBucket) []pageStrings {
	out := make([]pageStrings, len(buckets))
	running := map[string]string{} // carried value across pages
	for i := range buckets {
		start := cloneStrings(running) // value entering this page
		first := map[string]string{}   // first setter on this page (per name)
		for _, b := range buckets[i].blocks {
			applyBlockStringSetsTracking(b, running, first)
		}
		out[i] = pageStrings{Start: start, First: first, Last: cloneStrings(running)}
	}
	return out
}

// cloneStrings returns a shallow copy of m (a fresh map so later mutation of the source
// does not alias a captured snapshot). A nil m yields an empty (non-nil) map.
func cloneStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyBlockStringSetsTracking walks a block fragment's subtree in document order,
// updating running with each box's string-set assignments (Prefix + content() text +
// Suffix) and recording into first the FIRST value set for each name on this page (first
// wins: a name already present in first is not overwritten), so a page's
// string(name, first) can resolve to its leading setter.
func applyBlockStringSetsTracking(f *Fragment, running, first map[string]string) {
	if f == nil {
		return
	}
	if f.Box != nil {
		for _, e := range f.Box.Style.StringSet {
			val := e.Prefix
			if e.UseContent {
				val += boxText(f.Box)
			}
			val += e.Suffix
			running[e.Name] = val
			if _, ok := first[e.Name]; !ok {
				first[e.Name] = val
			}
		}
	}
	for _, c := range f.Children {
		applyBlockStringSetsTracking(c, running, first)
	}
}

// boxText returns the concatenated text of a box's BoxText leaf descendants (content()).
func boxText(b *cssbox.Box) string {
	if b == nil {
		return ""
	}
	if b.Kind == cssbox.BoxText {
		return b.Text
	}
	var sb strings.Builder
	for _, c := range b.Children {
		sb.WriteString(boxText(c))
	}
	return sb.String()
}
