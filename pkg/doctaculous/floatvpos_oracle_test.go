package doctaculous

import (
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// TestShowcaseFloatVerticalPositionOracle is a RELIABLE oracle over the ACTUAL htmldoc
// showcase (loaded the same way TestHTMLDocShowcase does: OpenURL + WithDefaultPaged).
// Instead of scanning pixels (which derailed the prior session) it walks the paint-item
// stream and reports, in final page space, the FLOAT section's markers:
//
//   - the figure's image        (ImageItem 138x104)               → figure content box anchor
//   - the figure's border edges  (#1a1a18, 1px thick — NOT the 6px body border) → figure border box
//   - the lede's bottom border   (#c9c2b0, a horizontal BorderItem) → lede border-box bottom
//
// so we can compare the figure box to the lede in ONE frame and see whether the residual
// "figure too high" discrepancy the handoff describes actually exists in the fragment tree.
func TestShowcaseFloatVerticalPositionOracle(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir(htmlDocDir)))
	defer srv.Close()

	doc, err := OpenURL(srv.URL+"/index.html", WithDefaultPaged())
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	pages := doc.r.(*reflowRenderer).pages

	ledeBorder := color.RGBA{0xc9, 0xc2, 0xb0, 0xff}
	figBorder := color.RGBA{0x1a, 0x1a, 0x18, 0xff}

	// Anchor on the figure's decoded image (138x104 — unique in the document). The page
	// it lands on is the FLOAT section's page.
	figPage := -1
	var img layout.ImageItem
	for pi := range pages.Pages {
		for _, it := range pages.Pages[pi].Items {
			if it.Kind == layout.ImageKind &&
				math.Abs(it.Image.WPt-138) < 1 && math.Abs(it.Image.HPt-104) < 1 {
				figPage = pi
				img = it.Image
				break
			}
		}
		if figPage >= 0 {
			break
		}
	}
	if figPage < 0 {
		t.Fatal("figure image (138x104) not found on any page — is the figure painted?")
	}
	t.Logf("FLOAT section is on page %d (of %d)", figPage, len(pages.Pages))
	t.Logf("figure image: x=%.2f y=%.2f w=%.2f h=%.2f (bottom=%.2f)", img.XPt, img.YPt, img.WPt, img.HPt, img.YPt+img.HPt)

	// The figure border box: the four 1px #1a1a18 edges (thickness 1 distinguishes them
	// from the 6px body border of the same color). Collect their union.
	var figTop, figLeft, figRight, figBottom = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	foundFig := false
	ledeBottomY := math.Inf(-1)
	foundLede := false
	for _, it := range pages.Pages[figPage].Items {
		if it.Kind != layout.BorderKind {
			continue
		}
		b := it.Border
		thin := b.WPt < 3 || b.HPt < 3 // a 1px edge (one dimension is the thickness)
		isFigThickness := (b.Side == layout.EdgeTop || b.Side == layout.EdgeBottom) && b.HPt < 2 ||
			(b.Side == layout.EdgeLeft || b.Side == layout.EdgeRight) && b.WPt < 2
		if near3(b.Color, figBorder) && thin && isFigThickness {
			foundFig = true
			t.Logf("figure border edge: side=%v  x=%.2f y=%.2f w=%.2f h=%.2f", b.Side, b.XPt, b.YPt, b.WPt, b.HPt)
			figTop = math.Min(figTop, b.YPt)
			figLeft = math.Min(figLeft, b.XPt)
			figRight = math.Max(figRight, b.XPt+b.WPt)
			figBottom = math.Max(figBottom, b.YPt+b.HPt)
		}
		// The lede's #c9c2b0 border is its border-BOTTOM (side=EdgeBottom); the
		// .after-float block below the figure has a #c9c2b0 border-TOP (side=EdgeTop) of
		// the same color, so constrain to EdgeBottom to pick the lede's, not the after-
		// float's. Take the topmost such border (the lede sits above the after-float).
		if near3(b.Color, ledeBorder) && b.Side == layout.EdgeBottom {
			t.Logf("lede bottom border: side=%v  x=%.2f y=%.2f w=%.2f h=%.2f", b.Side, b.XPt, b.YPt, b.WPt, b.HPt)
			if !foundLede || b.YPt < ledeBottomY {
				ledeBottomY = b.YPt
			}
			foundLede = true
		}
	}

	if foundFig {
		t.Logf("figure border box: top=%.2f left=%.2f right=%.2f bottom=%.2f (w=%.2f h=%.2f)",
			figTop, figLeft, figRight, figBottom, figRight-figLeft, figBottom-figTop)
	}
	if foundLede {
		t.Logf("lede border-box bottom (its bottom-border Y): %.2f", ledeBottomY)
	}

	// CSS: .figure { margin: 4px 18px 8px 0 } ; .lede { padding-bottom:10; border-bottom:1px; margin-bottom:14 }.
	// The lede's bottom border sits at its border-box bottom edge (border is outside padding),
	// so lede border-box bottom = ledeBottomY. Its margin-box bottom = ledeBottomY + 14.
	// The figure's border-box top = figTop; its margin-box top = figTop - 4.
	// CSS 9.5: figure margin-box top must be >= lede margin-box bottom.
	if foundFig && foundLede {
		ledeMarginBottom := ledeBottomY + 14
		figMarginTop := figTop - 4
		gap := figMarginTop - ledeMarginBottom
		t.Logf("lede margin-box bottom = %.2f ; figure margin-box top = %.2f ; gap = %.2f",
			ledeMarginBottom, figMarginTop, gap)
		if gap < -0.5 {
			t.Errorf("FIGURE TOO HIGH: figure margin-box top %.2f is %.2f above the lede margin-box bottom %.2f",
				figMarginTop, -gap, ledeMarginBottom)
		}
	}
}

// near3 reports whether two colors match within a small per-channel tolerance.
func near3(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) < 8 && d(a.G, b.G) < 8 && d(a.B, b.B) < 8
}
