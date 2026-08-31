package html

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNestingLimit covers the depth bound on Parse. The bound exists because
// x/net/html's close-tag handling is quadratic in the open-element count, so a
// deeply nested document is a denial of service the caller cannot escape:
// measured inside xhtml.Parse, 30,000 nested <div> took 3.7s and 60,000 took
// 15.1s, while 200,000 did not finish. The cost is in the dependency and lands
// before this package gets control, so declining the input is the only lever.
func TestNestingLimit(t *testing.T) {
	deep := func(n int) []byte {
		return []byte(strings.Repeat("<div>", n) + "x" + strings.Repeat("</div>", n))
	}

	// Just inside the limit still parses.
	if _, err := Parse(deep(maxNestingDepth)); err != nil {
		t.Errorf("depth %d should parse: %v", maxNestingDepth, err)
	}
	// One past it is refused, with a matchable sentinel.
	_, err := Parse(deep(maxNestingDepth + 1))
	if err == nil {
		t.Fatalf("depth %d was accepted; it should be refused", maxNestingDepth+1)
	}
	if !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("error = %v, want it to wrap ErrTooDeeplyNested", err)
	}
}

// TestNestingLimitIsCheap pins the property that actually matters: refusing a
// hostile document must not cost what parsing it would have. The check exits at
// the first over-deep tag, so the work is bounded by where the nesting is, not
// by the size of the file.
func TestNestingLimitIsCheap(t *testing.T) {
	// 500,000 levels: unbounded, this did not finish at all.
	src := []byte(strings.Repeat("<div>", 500000) + "x" + strings.Repeat("</div>", 500000))
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := Parse(src); err == nil {
			t.Error("a 500,000-level document was accepted")
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Parse did not return: the nesting check is not short-circuiting")
	}
}

// TestNestingCountIgnoresVoidElements checks that a long run of void elements --
// which never enter the open-element stack -- is not mistaken for deep nesting.
// Without this a page with thousands of <br> or <img> would be wrongly refused.
func TestNestingCountIgnoresVoidElements(t *testing.T) {
	for _, tag := range []string{"br", "img", "hr", "input", "meta", "link", "wbr"} {
		src := []byte("<div>" + strings.Repeat("<"+tag+">", maxNestingDepth*2) + "</div>")
		if _, err := Parse(src); err != nil {
			t.Errorf("%d <%s> elements were refused: %v", maxNestingDepth*2, tag, err)
		}
	}
}

// TestNestingCountHandlesUnclosedTags covers markup whose tags never close: the
// depth counter must still see the nesting, since the parser's open-element
// stack does too.
func TestNestingCountHandlesUnclosedTags(t *testing.T) {
	src := []byte(strings.Repeat("<div>", maxNestingDepth+100))
	if _, err := Parse(src); !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("unclosed deep nesting: err = %v, want ErrTooDeeplyNested", err)
	}
}

// TestOrdinaryDocumentsUnaffected is the guard against the bound being too
// tight. These are the shapes real documents take.
func TestOrdinaryDocumentsUnaffected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"simple", `<html><body><p>hello</p></body></html>`},
		{"table", `<table><tr><td><div><span>x</span></div></td></tr></table>`},
		{"nested lists", `<ul>` + strings.Repeat("<li><ul>", 50) + "<li>x" + strings.Repeat("</ul></li>", 50) + `</ul>`},
		{"many siblings", `<div>` + strings.Repeat("<p>x</p>", 10000) + `</div>`},
		{"deep but reasonable", strings.Repeat("<div>", 100) + "x" + strings.Repeat("</div>", 100)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Parse([]byte(c.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if doc.Root == nil {
				t.Error("nil root")
			}
		})
	}
}
