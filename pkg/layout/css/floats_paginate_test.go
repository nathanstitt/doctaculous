package css_test

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
)

// TestFloatsPaintInNamedPageRun pins the fix: when a document uses a named @page
// (so pagination goes through paginateRuns), a floated block must still emit its
// paint items. Before the fix, paginateRuns nil'd Floats and never re-attached
// them, so the float — and any image inside it — vanished.
func TestFloatsPaintInNamedPageRun(t *testing.T) {
	src := []byte(`<!DOCTYPE html><html><head><style>
	  @page wide { size: 1200px 800px }
	  .land { page: wide }
	  .fig { float: left; width: 100px; height: 60px; background: #c00 }
	</style></head><body>
	  <section class="land">
	    <div class="fig"></div>
	    <p>text beside the float</p>
	  </section>
	</body></html>`)

	doc, err := html.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	root, faces, pages, running, err := layoutcss.BuildWithFontsPagesRunning(ctx, doc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := layoutcss.New(layoutfont.NewFaceCacheWithFonts(faces, nil, nil, nil), nil, nil)
	out, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged: true, FallbackW: 816, FallbackH: 1056, ExplicitSize: false,
		Pages: pages, Running: running,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFilledRect(out, 0xcc, 0x00, 0x00) {
		t.Fatal("floated block produced no paint items (float dropped in paginateRuns)")
	}
}

// hasFilledRect reports whether any page carries a Rule/Background item whose color
// approximately matches (r,g,b).
func hasFilledRect(pages *layout.Pages, r, g, b uint8) bool {
	near := func(a, want uint8) bool {
		if a > want {
			return a-want < 8
		}
		return want-a < 8
	}
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.RuleKind || it.Kind == layout.BackgroundKind {
				c := it.Rule.Color
				if near(c.R, r) && near(c.G, g) && near(c.B, b) {
					return true
				}
			}
		}
	}
	return false
}
