package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

func TestMarginBoxRectSharedTrailWidth(t *testing.T) {
	// A trailing (right/bottom) box's rect must be its OWN width w at the pinned edge —
	// NOT the full band width — so appendMarginText's text-align:right resolves inside it
	// (a full-band width double-applies the alignment, pushing text off the page). Page
	// 300x200, margins 20; top band x in [20,280) (width 260). A 60pt-wide right box pins
	// to x=280-60=220 with width 60 (its right edge = 280 = the band's right edge, inside).
	g := pageGeom{pageW: 300, pageH: 200, marginL: 20, marginT: 20, contentW: 260, contentH: 160}
	widths := map[gcss.MarginBoxSlot]float64{gcss.MarginTopRight: 60}
	r := marginBoxRectShared(gcss.MarginTopRight, g, widths)
	if r.w != 60 {
		t.Errorf("trail box width = %.1f, want 60 (its own width, not the band)", r.w)
	}
	if r.x != 220 {
		t.Errorf("trail box x = %.1f, want 220 (pinned right)", r.x)
	}
	if r.x+r.w > 280.5 {
		t.Errorf("trail box right edge = %.1f, want <= 280 (inside the band, not off-page)", r.x+r.w)
	}
	// An unmeasured box (w=0, e.g. an element() box) falls back to the full band width.
	r0 := marginBoxRectShared(gcss.MarginTopRight, g, map[gcss.MarginBoxSlot]float64{})
	if r0.w != 260 {
		t.Errorf("unmeasured trail box width = %.1f, want 260 (band fallback)", r0.w)
	}
}

func TestResolveMarginContent(t *testing.T) {
	cases := []struct {
		content    string
		page, npag int
		want       string
	}{
		{`"Title"`, 1, 3, "Title"},
		{`counter(page)`, 2, 5, "2"},
		{`counter(pages)`, 2, 5, "5"},
		{`counter(page) " / " counter(pages)`, 3, 8, "3 / 8"},
		{`"Page " counter(page) " of " counter(pages)`, 1, 4, "Page 1 of 4"},
		{`counter(page, decimal)`, 7, 9, "7"}, // style arg ignored (decimal)
		{`string(title)`, 1, 2, ""},           // deferred → empty
		{`normal`, 1, 2, ""},
		{``, 1, 2, ""},
	}
	for _, c := range cases {
		got := resolveMarginContent(c.content, c.page, c.npag)
		if got != c.want {
			t.Errorf("resolveMarginContent(%q, %d, %d) = %q, want %q", c.content, c.page, c.npag, got, c.want)
		}
	}
}

func TestResolveMarginContentCounterStyle(t *testing.T) {
	cases := []struct {
		content    string
		page, npag int
		want       string
	}{
		{`counter(page, lower-roman)`, 4, 9, "iv"},
		{`counter(page, upper-roman)`, 4, 9, "IV"},
		{`counter(pages, upper-alpha)`, 1, 3, "C"},
		{`counter(page, decimal-leading-zero)`, 7, 9, "07"},
		{`"p. " counter(page, lower-roman)`, 2, 5, "p. ii"},
		{`counter(page, bogus-style)`, 5, 9, "5"}, // unknown style → decimal fallback
	}
	for _, c := range cases {
		got := resolveMarginContent(c.content, c.page, c.npag)
		if got != c.want {
			t.Errorf("resolveMarginContent(%q,%d,%d) = %q, want %q", c.content, c.page, c.npag, got, c.want)
		}
	}
}

func TestResolveMarginContentString(t *testing.T) {
	ps := pageStrings{
		Start: map[string]string{"t": "S"},
		First: map[string]string{"t": "F"},
		Last:  map[string]string{"t": "L"},
	}
	chk := func(content, want string) {
		if got := resolveMarginContentWithStrings(content, 3, 9, ps); got != want {
			t.Errorf("%q = %q, want %q", content, got, want)
		}
	}
	chk(`string(t)`, "L")
	chk(`string(t, first)`, "F")
	chk(`string(t, start)`, "S")
	chk(`string(t, last)`, "L")
	chk(`string(t) " p" counter(page)`, "L p3")
	chk(`string(missing)`, "")
}

func TestSplitContentComponents(t *testing.T) {
	got := splitContentComponents(`"Page " counter(page) " of " counter(pages)`)
	want := []string{`"Page "`, `counter(page)`, `" of "`, `counter(pages)`}
	if len(got) != len(want) {
		t.Fatalf("got %d components %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMarginBoxRect(t *testing.T) {
	// Page 200x300, margins 20 all sides ⇒ content 160x260 at (20,20).
	g := pageGeom{pageW: 200, pageH: 300, marginL: 20, marginT: 20, contentW: 160, contentH: 260}
	// Top-center: y in [0,20), x in [20,180), full content width.
	tc := marginBoxRect(gcss.MarginTopCenter, g)
	if tc.x != 20 || tc.y != 0 || tc.w != 160 || tc.h != 20 {
		t.Errorf("top-center rect = %+v, want {20,0,160,20}", tc)
	}
	// Bottom-right: bottom band y in [280,300).
	br := marginBoxRect(gcss.MarginBottomRight, g)
	if br.y != 280 || br.h != 20 || br.w != 160 {
		t.Errorf("bottom-right rect = %+v, want y280 h20 w160", br)
	}
	// Top-left corner: [0,20)x[0,20).
	tlc := marginBoxRect(gcss.MarginTopLeftCorner, g)
	if tlc.x != 0 || tlc.y != 0 || tlc.w != 20 || tlc.h != 20 {
		t.Errorf("top-left-corner rect = %+v, want {0,0,20,20}", tlc)
	}
}

func TestMarginBoxRectThreeAcrossTopEdge(t *testing.T) {
	// Page 300x200, margins 20 all sides ⇒ content 260x160 at (20,20). The top edge
	// band is y in [0,20), x in [20,280) (width 260). With all three top boxes present
	// and ~60pt-wide content each, left pins to x=20, right pins to x=280-60=220, center
	// sits at 20+(260-60)/2=120.
	g := pageGeom{pageW: 300, pageH: 200, marginL: 20, marginT: 20, contentW: 260, contentH: 160}
	widths := map[gcss.MarginBoxSlot]float64{
		gcss.MarginTopLeft:   60,
		gcss.MarginTopCenter: 60,
		gcss.MarginTopRight:  60,
	}
	l := marginBoxRectShared(gcss.MarginTopLeft, g, widths)
	c := marginBoxRectShared(gcss.MarginTopCenter, g, widths)
	r := marginBoxRectShared(gcss.MarginTopRight, g, widths)
	if l.x != 20 {
		t.Errorf("top-left x = %.1f, want 20 (pinned left)", l.x)
	}
	if c.x < 119 || c.x > 121 {
		t.Errorf("top-center x = %.1f, want ~120 (centered)", c.x)
	}
	if r.x < 219 || r.x > 221 {
		t.Errorf("top-right x = %.1f, want ~220 (pinned right)", r.x)
	}
	// A lone center box (no left/right) still centers in the full band.
	only := map[gcss.MarginBoxSlot]float64{gcss.MarginTopCenter: 60}
	c2 := marginBoxRectShared(gcss.MarginTopCenter, g, only)
	if c2.x < 119 || c2.x > 121 {
		t.Errorf("lone top-center x = %.1f, want ~120", c2.x)
	}
}

// TestMarginBoxPageCounterEndToEnd renders a 3-page document with a bottom-center page
// counter and asserts each page paints glyphs in its bottom margin band (the footer).
func TestMarginBoxPageCounterEndToEnd(t *testing.T) {
	src := `<html><head><style>
		@page { size: 400px 250px; margin: 30px; @bottom-center { content: counter(page) " / " counter(pages) } }
	</style></head><body>
		<div style="height:200px;margin:0;background-color:rgb(1,1,1)">a</div>
		<div style="height:200px;margin:0;background-color:rgb(2,2,2)">b</div>
		<div style="height:200px;margin:0;background-color:rgb(3,3,3)">c</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: 400px 250px; margin: 30px; @bottom-center { content: counter(page) " / " counter(pages) } }`, 400, 250, false)
	root := buildRoot(t, src, nil)
	pages, err := New(layoutfont.NewFaceCache(), nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) < 2 {
		t.Fatalf("expected ≥2 pages, got %d", len(pages.Pages))
	}
	// Each page's bottom margin band (y ≥ pageH-30) must contain glyph items (the footer).
	for i, p := range pages.Pages {
		bandTop := p.HeightPt - 30
		glyphs := 0
		for _, it := range p.Items {
			if it.Kind == layout.GlyphKind && it.Glyph.YPt >= bandTop-5 {
				glyphs++
			}
		}
		if glyphs == 0 {
			t.Errorf("page %d: no footer glyphs in bottom margin band (y≥%.0f)", i, bandTop)
		}
	}
}
