package paint

import (
	"image"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// bgItem builds a BackgroundImageItem with a 10×10 intrinsic image and the origin and
// clip boxes equal to the given rect, defaulting to repeat + auto size + top-left.
func bgItem(x, y, w, h float64) *layout.BackgroundImageItem {
	return &layout.BackgroundImageItem{
		Img:        image.NewRGBA(image.Rect(0, 0, 10, 10)),
		IntrinsicW: 10, IntrinsicH: 10,
		OriginX: x, OriginY: y, OriginW: w, OriginH: h,
		ClipX: x, ClipY: y, ClipW: w, ClipH: h,
		SizeKind:  layout.BgSizeAuto,
		PosXIsPct: true, PosYIsPct: true, PosXFrac: 0, PosYFrac: 0,
	}
}

func TestBackgroundNoRepeatDrawsOnce(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 100, 100) // 10×10 tile, no repeat → 1 draw
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 1 {
		t.Errorf("no-repeat DrawImage calls = %d, want 1", len(dev.ctms))
	}
}

func TestBackgroundRepeatXTilesAcross(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 100, 100)
	it.RepeatX = true // 100/10 = 10 tiles across, one row
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 10 {
		t.Errorf("repeat-x DrawImage calls = %d, want 10", len(dev.ctms))
	}
}

func TestBackgroundRepeatBothTilesGrid(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 100, 50)
	it.RepeatX, it.RepeatY = true, true // 10 across × 5 down = 50
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 50 {
		t.Errorf("repeat DrawImage calls = %d, want 50", len(dev.ctms))
	}
}

// A repeating background clips to its clip box (one Save/PushClip pair).
func TestBackgroundRepeatClips(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 30, 30)
	it.RepeatX, it.RepeatY = true, true
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if dev.pushClip != 1 {
		t.Errorf("clip pushes = %d, want 1", dev.pushClip)
	}
}

// background-size: cover scales the tile to cover the origin box (here a 10×10 image
// into a 100×40 box → cover scale 10, tile 100×100), drawn once for no-repeat.
func TestBackgroundCoverSize(t *testing.T) {
	it := bgItem(0, 0, 100, 40)
	it.SizeKind = layout.BgSizeCover
	w, h := backgroundTileSize(it)
	if w != 100 || h != 100 {
		t.Errorf("cover tile = %v×%v, want 100×100", w, h)
	}
}

// background-size: contain fits the tile inside the origin box (10×10 into 100×40 →
// contain scale 4, tile 40×40).
func TestBackgroundContainSize(t *testing.T) {
	it := bgItem(0, 0, 100, 40)
	it.SizeKind = layout.BgSizeContain
	w, h := backgroundTileSize(it)
	if w != 40 || h != 40 {
		t.Errorf("contain tile = %v×%v, want 40×40", w, h)
	}
}

// An explicit single-axis size preserves the intrinsic ratio on the auto axis.
func TestBackgroundExplicitSizeRatio(t *testing.T) {
	it := bgItem(0, 0, 100, 100)
	it.SizeKind = layout.BgSizeExplicit
	it.SizeW, it.SizeH = 50, 0 // height auto → 50 (square intrinsic)
	w, h := backgroundTileSize(it)
	if w != 50 || h != 50 {
		t.Errorf("explicit ratio tile = %v×%v, want 50×50", w, h)
	}
}

// A degenerate intrinsic size paints nothing (no DrawImage, no clip).
func TestBackgroundDegenerateSkips(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 100, 100)
	it.IntrinsicW = 0
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 0 || dev.pushClip != 0 {
		t.Errorf("degenerate painted: draws=%d clips=%d, want 0/0", len(dev.ctms), dev.pushClip)
	}
}

// background-position bottom-right places the single (no-repeat) tile against the far
// corner: the tile origin = origin box far edge − tile size.
func TestBackgroundPositionBottomRight(t *testing.T) {
	dev := &imageRecordDevice{}
	it := bgItem(0, 0, 100, 100)
	it.PosXFrac, it.PosYFrac = 1, 1 // 100% 100%
	paintBackgroundImage(dev, it, render.Scale(1, 1))
	if len(dev.ctms) != 1 {
		t.Fatalf("draws = %d, want 1", len(dev.ctms))
	}
	// The tile matrix is Scale(10,-10)·Translate(tx, ty+10). With a 10×10 tile in a
	// 100×100 box at 100%/100%, tx=ty=90, so the unit-square origin (0,0) — the tile's
	// top-left in image space, which the v-flip maps to the tile's BOTTOM-left in page
	// space — lands at (90, 100).
	x, y := dev.ctms[0].Apply(0, 0)
	if x != 90 || y != 100 {
		t.Errorf("bottom-right tile origin = (%v,%v), want (90,100)", x, y)
	}
}
