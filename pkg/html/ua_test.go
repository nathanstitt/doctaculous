package html

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/css"
)

// cssColor builds an opaque RGBA for comparing computed colors.
func cssColor(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }

// uaStyle builds a resolver from the UA sheet alone and computes a tag's style.
func uaStyle(tag string) css.ComputedStyle {
	r := css.NewResolver([]css.OriginSheet{{Sheet: UAStylesheet, Origin: css.OriginUA}}, nil)
	return r.ComputeRoot(&fakeElem{tag: tag})
}

func TestUADisplayDefaults(t *testing.T) {
	blocks := []string{"div", "p", "h1", "h6", "section", "ul", "ol", "blockquote"}
	for _, tag := range blocks {
		if d := uaStyle(tag).Display; d != "block" {
			t.Errorf("%s display = %q, want block", tag, d)
		}
	}
	if d := uaStyle("li").Display; d != "list-item" {
		t.Errorf("li display = %q, want list-item", d)
	}
	// Table parts: box generation switches on exactly these values.
	if d := uaStyle("table").Display; d != "table" {
		t.Errorf("table display = %q, want table", d)
	}
	if d := uaStyle("tr").Display; d != "table-row" {
		t.Errorf("tr display = %q, want table-row", d)
	}
	for _, tag := range []string{"td", "th"} {
		if d := uaStyle(tag).Display; d != "table-cell" {
			t.Errorf("%s display = %q, want table-cell", tag, d)
		}
	}
	tableParts := map[string]string{
		"thead":    "table-header-group",
		"tbody":    "table-row-group",
		"tfoot":    "table-footer-group",
		"col":      "table-column",
		"colgroup": "table-column-group",
		"caption":  "table-caption",
	}
	for tag, want := range tableParts {
		if d := uaStyle(tag).Display; d != want {
			t.Errorf("%s display = %q, want %q", tag, d, want)
		}
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
	// Heading font-sizes and top margins both decrease monotonically h1..h6
	// (the W3C sample UA sheet shape). This locks the margins against the
	// inverted-order regression where smaller headings got larger margins.
	prevSize, prevMargin := h1.FontSizePt+1, h1.MarginTop.Value+1
	for _, tag := range []string{"h1", "h2", "h3", "h4", "h5", "h6"} {
		cs := uaStyle(tag)
		if !cs.Bold {
			t.Errorf("%s should be bold", tag)
		}
		if cs.FontSizePt >= prevSize {
			t.Errorf("%s font-size %v should be < previous %v (sizes must decrease h1..h6)", tag, cs.FontSizePt, prevSize)
		}
		if cs.MarginTop.Value > prevMargin {
			t.Errorf("%s margin-top %v should be <= previous %v (margins must not invert)", tag, cs.MarginTop.Value, prevMargin)
		}
		prevSize, prevMargin = cs.FontSizePt, cs.MarginTop.Value
	}
}

// TestUALinkDefault: an <a href> gets the classic blue underlined link style via the
// UA a:link rule; a bare <a> (no href) does not match :link and stays unstyled.
func TestUALinkDefault(t *testing.T) {
	linked := &fakeElem{tag: "a", attrs: map[string]string{"href": "/x"}}
	cs := css.NewResolver([]css.OriginSheet{{Sheet: UAStylesheet, Origin: css.OriginUA}}, nil).ComputeRoot(linked)
	if cs.Color != (cssColor(0x00, 0x00, 0xee)) {
		t.Errorf("a:link color = %+v, want #0000ee", cs.Color)
	}
	if cs.TextDecorationLine != "underline" {
		t.Errorf("a:link text-decoration = %q, want underline", cs.TextDecorationLine)
	}
	bare := &fakeElem{tag: "a"} // no href → not :link
	cs2 := css.NewResolver([]css.OriginSheet{{Sheet: UAStylesheet, Origin: css.OriginUA}}, nil).ComputeRoot(bare)
	if cs2.TextDecorationLine == "underline" {
		t.Error("bare <a> (no href) should not get the link underline")
	}
}

// fakeElem is a minimal css.Node for UA-sheet tests (no real DOM needed).
type fakeElem struct {
	tag     string
	id      string
	classes []string
	parent  css.Node
	attrs   map[string]string
}

func (f *fakeElem) Tag() string       { return f.tag }
func (f *fakeElem) ID() string        { return f.id }
func (f *fakeElem) Classes() []string { return f.classes }
func (f *fakeElem) Parent() css.Node  { return f.parent }
func (f *fakeElem) Attr(k string) (string, bool) {
	v, ok := f.attrs[k]
	return v, ok
}
