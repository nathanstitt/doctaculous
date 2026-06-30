package css

import (
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// buildStringSnapshots returns, for each page bucket, the running CSS string values
// (name → value) in effect at the START of that page: the most recent string-set value
// contributed by any block bucketed on an earlier or the same page, in document order.
// CSS GCPM `string()` (default, == the "first that starts on the page or the carried
// value") is approximated by the running last-set value through the page — adequate for
// the dominant running-header-from-headings pattern. A page with no new setter inherits
// the prior page's values (carried forward).
func buildStringSnapshots(buckets []pageBucket) []map[string]string {
	out := make([]map[string]string, len(buckets))
	running := map[string]string{}
	for i := range buckets {
		// Apply this page's setters first, so a setter on this page is reflected in the
		// page's own snapshot (the "or the same page" clause: a running header shows the
		// last heading seen up to AND INCLUDING this page). A page with no new setter
		// keeps the prior page's running values (carried forward).
		for _, b := range buckets[i].blocks {
			applyBlockStringSets(b, running)
		}
		// Snapshot the running values in effect through the end of this page.
		snap := make(map[string]string, len(running))
		for k, v := range running {
			snap[k] = v
		}
		out[i] = snap
	}
	return out
}

// applyBlockStringSets walks a block fragment's subtree in document order, updating
// running with each box's string-set assignments (Prefix + content() text + Suffix).
func applyBlockStringSets(f *Fragment, running map[string]string) {
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
		}
	}
	for _, c := range f.Children {
		applyBlockStringSets(c, running)
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
