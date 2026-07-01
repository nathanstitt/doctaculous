package css

import (
	"context"
	"image/color"
	"math"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func blockNamed(name string) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Page: name}
	return &Fragment{Box: box, H: 10}
}

func TestGroupRuns(t *testing.T) {
	// Blocks: "", "", "wide", "wide", "" ⇒ three runs: [0,1] default, [2,3] wide, [4] default.
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("wide"), blockNamed("wide"), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3: %+v", len(runs), runs)
	}
	if runs[0].name != "" || runs[0].start != 0 || runs[0].end != 2 {
		t.Errorf("run0 = %+v, want {name:\"\" start:0 end:2}", runs[0])
	}
	if runs[1].name != "wide" || runs[1].start != 2 || runs[1].end != 4 {
		t.Errorf("run1 = %+v, want {name:wide start:2 end:4}", runs[1])
	}
	if runs[2].name != "" || runs[2].start != 4 || runs[2].end != 5 {
		t.Errorf("run2 = %+v, want {name:\"\" start:4 end:5}", runs[2])
	}
}

func TestGroupRunsSingle(t *testing.T) {
	// All same name ⇒ one run spanning everything (the byte-identical default case).
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 1 || runs[0].start != 0 || runs[0].end != 3 {
		t.Fatalf("single-name should be one run [0,3); got %+v", runs)
	}
}

func TestNamedPageWidthReflow(t *testing.T) {
	// Default page 200 wide; a `.wide` section uses @page wide { size: 400px ... }. The
	// wide section's full-width block must be laid out at the wide content width (~360px
	// after 20px margins), NOT the default 160px. Two sections so we see both widths.
	src := `<html><head><style>
		@page { size: 200px 300px; margin: 20px }
		@page wide { size: 440px 300px; margin: 20px }
		.wide { page: wide }
		div { margin: 0 }
	</style></head><body>
		<div style="height:50px;background:rgb(1,1,1)">narrow</div>
		<div class="wide" style="height:50px;background:rgb(2,2,2)">wide</div>
	</body></html>`
	cfg := pagedConfigFor(`
		@page { size: 200px 300px; margin: 20px }
		@page wide { size: 440px 300px; margin: 20px }
	`, 200, 300, false)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("want 2 pages (page change forces a break), got %d", len(pages.Pages))
	}
	// Page 0 is the narrow page (200 wide); its block fills the 160px content width.
	if math.Abs(pages.Pages[0].WidthPt-200) > 0.5 {
		t.Errorf("page 0 width = %.0f, want 200 (default)", pages.Pages[0].WidthPt)
	}
	narrow := firstBackground(pages.Pages[0].Items, color.RGBA{1, 1, 1, 255})
	if narrow == nil || math.Abs(narrow.WPt-160) > 1 {
		t.Errorf("narrow block width = %v, want 160 (200-2*20)", narrow)
	}
	// Page 1 is the wide page (440 wide); its block fills the 400px content width.
	if math.Abs(pages.Pages[1].WidthPt-440) > 0.5 {
		t.Errorf("page 1 width = %.0f, want 440 (wide)", pages.Pages[1].WidthPt)
	}
	wide := firstBackground(pages.Pages[1].Items, color.RGBA{2, 2, 2, 255})
	if wide == nil || math.Abs(wide.WPt-400) > 1 {
		t.Errorf("wide block width = %v, want 400 (440-2*20)", wide)
	}
}
