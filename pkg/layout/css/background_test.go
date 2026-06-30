package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// findBgFragment walks the fragment tree for the first fragment carrying a decoded
// background image.
func findBgFragment(f *Fragment) *Fragment {
	if f == nil {
		return nil
	}
	if f.BgImage != nil {
		return f
	}
	for _, c := range f.Children {
		if got := findBgFragment(c); got != nil {
			return got
		}
	}
	return nil
}

// A decodable background-image url is attached to the box's fragment with the right
// origin/clip boxes derived from the box geometry.
func TestBackgroundImageAttached(t *testing.T) {
	loader := pngLoader(t, 8, 8)
	root := layoutWithLoader(t,
		`<body><div style="width:100px;height:60px;background-image:url(img.png)"></div></body>`,
		800, loader, nil)
	f := findBgFragment(root)
	if f == nil {
		t.Fatal("no fragment with a background image")
	}
	if f.BgImage.IntrinsicW != 8 || f.BgImage.IntrinsicH != 8 {
		t.Errorf("intrinsic = %v×%v, want 8×8", f.BgImage.IntrinsicW, f.BgImage.IntrinsicH)
	}
	// Default background-origin is padding-box; with no border/padding it equals the
	// content/border box here (100×60).
	if f.BgImage.OriginW != 100 || f.BgImage.OriginH != 60 {
		t.Errorf("origin box = %v×%v, want 100×60", f.BgImage.OriginW, f.BgImage.OriginH)
	}
	// Default repeat.
	if !f.BgImage.RepeatX || !f.BgImage.RepeatY {
		t.Errorf("default repeat = %v/%v, want true/true", f.BgImage.RepeatX, f.BgImage.RepeatY)
	}
}

// An undecodable/missing background image leaves BgImage nil (the box still lays out;
// its background color, if any, still paints).
func TestBackgroundImageMissingDegrades(t *testing.T) {
	root := layoutWithLoader(t,
		`<body><div style="width:100px;height:60px;background:#abc url(nope.png)"></div></body>`,
		800, resource.MapLoader{}, nil)
	if f := findBgFragment(root); f != nil {
		t.Errorf("missing background image attached a BgImage: %+v", f.BgImage)
	}
}

// The background image item is emitted AFTER the background color and BEFORE the
// border (CSS Backgrounds 3 paint order).
func TestBackgroundImagePaintOrder(t *testing.T) {
	loader := pngLoader(t, 8, 8)
	root := layoutWithLoader(t,
		`<body><div style="width:100px;height:60px;background-color:#abc;`+
			`background-image:url(img.png);border:2px solid #000"></div></body>`,
		800, loader, nil)
	var items []layout.Item
	items = root.AppendItems(items)

	colorIdx, imgIdx, borderIdx := -1, -1, -1
	for i, it := range items {
		switch it.Kind {
		case layout.BackgroundKind:
			if colorIdx < 0 {
				colorIdx = i
			}
		case layout.BackgroundImageKind:
			if imgIdx < 0 {
				imgIdx = i
			}
		case layout.BorderKind:
			if borderIdx < 0 {
				borderIdx = i
			}
		}
	}
	if colorIdx < 0 || imgIdx < 0 || borderIdx < 0 {
		t.Fatalf("missing items: color=%d image=%d border=%d", colorIdx, imgIdx, borderIdx)
	}
	if colorIdx >= imgIdx || imgIdx >= borderIdx {
		t.Errorf("paint order wrong: color=%d image=%d border=%d, want color<image<border",
			colorIdx, imgIdx, borderIdx)
	}
}

// A background image on a box that is NOT the first in flow must have its origin/clip
// boxes in PAGE space (shifted with the fragment), not the box's local 0-based frame —
// otherwise every box's background paints at the top of the page. (Regression test for
// the shiftFragmentExtras BgImage omission.)
func TestBackgroundImageShiftedToPageSpace(t *testing.T) {
	loader := pngLoader(t, 8, 8)
	root := layoutWithLoader(t,
		`<body><div style="height:100px"></div>`+ // a 100px spacer before the bg box
			`<div style="height:60px;background-image:url(img.png)"></div></body>`,
		800, loader, nil)
	f := findBgFragment(root)
	if f == nil {
		t.Fatal("no background fragment")
	}
	// The bg box's border-box top is ~100 (after the spacer); its background origin/clip
	// Y must track that, not 0.
	if f.BgImage.OriginY < 90 || f.BgImage.ClipY < 90 {
		t.Errorf("bg origin/clip Y = %v/%v, want ~100 (page space); fragment Y = %v",
			f.BgImage.OriginY, f.BgImage.ClipY, f.Y)
	}
	if f.BgImage.OriginY != f.Y || f.BgImage.ClipY != f.Y {
		t.Errorf("bg Y (%v/%v) should equal fragment Y (%v) for a no-border box",
			f.BgImage.OriginY, f.BgImage.ClipY, f.Y)
	}
}

// background-origin: content-box insets the origin box by border + padding.
func TestBackgroundOriginContentBox(t *testing.T) {
	loader := pngLoader(t, 8, 8)
	root := layoutWithLoader(t,
		`<body><div style="width:100px;height:60px;padding:10px;border:5px solid #000;`+
			`background-image:url(img.png);background-origin:content-box"></div></body>`,
		800, loader, nil)
	f := findBgFragment(root)
	if f == nil {
		t.Fatal("no background fragment")
	}
	// content box = width/height (content-box sizing: 100×60), origin inset to it.
	if f.BgImage.OriginW != 100 || f.BgImage.OriginH != 60 {
		t.Errorf("content-box origin = %v×%v, want 100×60", f.BgImage.OriginW, f.BgImage.OriginH)
	}
	// The clip box defaults to border-box: 100 + 2*(10+5) = 130 wide.
	if f.BgImage.ClipW != 130 || f.BgImage.ClipH != 90 {
		t.Errorf("border-box clip = %v×%v, want 130×90", f.BgImage.ClipW, f.BgImage.ClipH)
	}
}
