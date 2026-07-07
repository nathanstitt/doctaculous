package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// firstBySemTag returns the first box (depth-first) whose SemTag equals tag, or nil.
func firstBySemTag(b *cssbox.Box, tag string) *cssbox.Box {
	if b.SemTag == tag {
		return b
	}
	for _, c := range b.Children {
		if got := firstBySemTag(c, tag); got != nil {
			return got
		}
	}
	return nil
}

func TestSemanticsHeadings(t *testing.T) {
	root := build(t, `<html><body>
		<h1>One</h1><h2>Two</h2><h3>Three</h3>
		<h4>Four</h4><h5>Five</h5><h6>Six</h6>
	</body></html>`, nil)
	for lvl := 1; lvl <= 6; lvl++ {
		tag := "h" + string(rune('0'+lvl))
		b := firstBySemTag(root, tag)
		if b == nil {
			t.Fatalf("no box with SemTag %q", tag)
		}
		if b.HeadingLvl != lvl {
			t.Errorf("%s: HeadingLvl = %d, want %d", tag, b.HeadingLvl, lvl)
		}
	}
}

func TestSemanticsLink(t *testing.T) {
	root := build(t, `<html><body><p>see <a href="https://example.com/x">here</a></p></body></html>`, nil)
	a := firstBySemTag(root, "a")
	if a == nil {
		t.Fatal("no box with SemTag \"a\"")
	}
	if a.Href != "https://example.com/x" {
		t.Errorf("Href = %q, want https://example.com/x", a.Href)
	}
}

func TestSemanticsLinkNoHref(t *testing.T) {
	// A bare named-anchor <a> (no href) still gets SemTag "a" but an empty Href;
	// the writer degrades to plain text. This is the graceful-degradation path.
	root := build(t, `<html><body><a name="top">anchor</a></body></html>`, nil)
	a := firstBySemTag(root, "a")
	if a == nil {
		t.Fatal("no box with SemTag \"a\"")
	}
	if a.Href != "" {
		t.Errorf("Href = %q, want empty", a.Href)
	}
}

func TestSemanticsInlineRoles(t *testing.T) {
	root := build(t, `<html><body><p>
		<strong>s</strong><b>b</b><em>e</em><i>i</i>
		<code>c</code><blockquote>q</blockquote><pre>p</pre>
	</p></body></html>`, nil)
	// b/i normalize to strong/em.
	for _, want := range []string{"strong", "em", "code", "blockquote", "pre", "p"} {
		if firstBySemTag(root, want) == nil {
			t.Errorf("no box with SemTag %q", want)
		}
	}
}

func TestSemanticsGenericBoxUnannotated(t *testing.T) {
	// A generic div/span carries no SemTag, so it lays out identically to before.
	root := build(t, `<html><body><div><span>plain</span></div></body></html>`, nil)
	if b := firstBySemTag(root, "div"); b != nil {
		t.Errorf("div unexpectedly annotated: %+v", b.SemTag)
	}
}
