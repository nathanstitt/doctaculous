package html

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

// uaStyle builds a resolver from the UA sheet alone and computes a tag's style.
func uaStyle(tag string) css.ComputedStyle {
	r := css.NewResolver([]css.OriginSheet{{Sheet: UAStylesheet, Origin: css.OriginUA}}, nil)
	return r.ComputeRoot(&fakeElem{tag: tag})
}

func TestUADisplayDefaults(t *testing.T) {
	blocks := []string{"div", "p", "h1", "h6", "section", "ul", "ol", "table", "blockquote"}
	for _, tag := range blocks {
		if d := uaStyle(tag).Display; d != "block" {
			t.Errorf("%s display = %q, want block", tag, d)
		}
	}
	if d := uaStyle("li").Display; d != "list-item" {
		t.Errorf("li display = %q, want list-item", d)
	}
	for _, tag := range []string{"head", "script", "style", "title"} {
		if d := uaStyle(tag).Display; d != "none" {
			t.Errorf("%s display = %q, want none", tag, d)
		}
	}
}

func TestUAHeadingSizes(t *testing.T) {
	h1 := uaStyle("h1")
	if !h1.Bold {
		t.Error("h1 should be bold by UA default")
	}
	// h1 font-size should be larger than the 16pt initial.
	if h1.FontSizePt <= 16 {
		t.Errorf("h1 font-size = %v, want > 16", h1.FontSizePt)
	}
}

// fakeElem is a minimal css.Node for UA-sheet tests (no real DOM needed).
type fakeElem struct {
	tag     string
	id      string
	classes []string
	parent  css.Node
}

func (f *fakeElem) Tag() string                { return f.tag }
func (f *fakeElem) ID() string                 { return f.id }
func (f *fakeElem) Classes() []string          { return f.classes }
func (f *fakeElem) Parent() css.Node           { return f.parent }
func (f *fakeElem) Attr(string) (string, bool) { return "", false }
