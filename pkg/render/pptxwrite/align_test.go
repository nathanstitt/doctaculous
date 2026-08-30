package pptxwrite

import (
	"testing"

	gcss "github.com/nathanstitt/omnidoc/pkg/css"
	"github.com/nathanstitt/omnidoc/pkg/layout/cssbox"
)

// TestAlignOfDropsDirectionRelativeKeywords pins the allowlist that makes the
// direction-relative text-align keywords safe on this path.
//
// alignOf emits its input VERBATIM into the .pptx as <a:pPr algn="...">, and the
// only thing keeping "start"/"end" out of the file is the explicit allowlist below.
// The CSS initial value of text-align is "start" (not physical "left"), so EVERY
// paragraph that does not set text-align now arrives here as "start". Widening this
// switch — or replacing it with a passthrough — would silently emit an invalid
// DrawingML alignment attribute on every such paragraph.
//
// "start" and "end" must map to "" (omitted, i.e. inherit the layout default), the
// same as the pre-RTL behavior for an unset alignment.
func TestAlignOfDropsDirectionRelativeKeywords(t *testing.T) {
	cases := []struct{ textAlign, want string }{
		{"start", ""}, // the CSS initial value — the important case
		{"end", ""},   // direction-relative, no DrawingML equivalent
		{"", ""},      // unset
		{"left", ""},  // physical-left is the default; omitted
		{"bogus", ""}, // unknown values never reach the file
		{"center", "center"},
		{"right", "right"},
		{"justify", "justify"},
	}
	for _, c := range cases {
		b := &cssbox.Box{Style: gcss.ComputedStyle{TextAlign: c.textAlign}}
		if got := alignOf(b); got != c.want {
			t.Errorf("alignOf(text-align:%q) = %q, want %q", c.textAlign, got, c.want)
		}
	}
}
