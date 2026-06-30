package paint

import (
	"math"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// maxBackgroundTiles caps the number of tiles a single background image may paint, a
// backstop against a pathological tiny-tile × huge-box blow-up. Over the cap the
// painter draws up to the cap and stops (the visible region is covered first because
// tiling starts at the clip box's leading edge).
const maxBackgroundTiles = 100000

// paintBackgroundImage paints a CSS background image: it computes the painted tile
// size (background-size), the first tile's top-left within the origin box
// (background-position), tiles along the repeat axes (background-repeat), and clips
// every tile to the clip box (background-clip). It mirrors paintImage's matrix recipe
// (unit square → tile rect, Y-flipped for DrawImage's bottom-up convention).
func paintBackgroundImage(dev render.Device, it *layout.BackgroundImageItem, mat render.Matrix) {
	if it.Img == nil || it.IntrinsicW <= 0 || it.IntrinsicH <= 0 {
		return
	}
	if it.ClipW <= 0 || it.ClipH <= 0 || it.OriginW <= 0 || it.OriginH <= 0 {
		return
	}

	tw, th := backgroundTileSize(it)
	if tw <= 0 || th <= 0 {
		return // degenerate computed size: nothing to paint
	}

	// First-tile top-left within the origin box, per axis.
	x0 := it.OriginX + bgAxisOffset(it.PosXIsPct, it.PosXFrac, it.PosXPx, it.OriginW, tw)
	y0 := it.OriginY + bgAxisOffset(it.PosYIsPct, it.PosYFrac, it.PosYPx, it.OriginH, th)

	// Tile span: one tile when the axis doesn't repeat, else from the first tile that
	// can touch the clip box's leading edge to the last that can touch its far edge.
	xs := tilePositions(x0, tw, it.RepeatX, it.ClipX, it.ClipX+it.ClipW)
	ys := tilePositions(y0, th, it.RepeatY, it.ClipY, it.ClipY+it.ClipH)
	if len(xs) == 0 || len(ys) == 0 {
		return
	}

	dev.Save()
	clipRect(dev, mat, it.ClipX, it.ClipY, it.ClipX+it.ClipW, it.ClipY+it.ClipH)
	drawn := 0
	for _, ty := range ys {
		for _, tx := range xs {
			if drawn >= maxBackgroundTiles {
				dev.Restore()
				return
			}
			mImg := render.Scale(tw, -th).Mul(render.Translate(tx, ty+th))
			dev.DrawImage(it.Img, mImg.Mul(mat), 1, "")
			drawn++
		}
	}
	dev.Restore()
}

// backgroundTileSize computes the painted size of one background tile from the size
// mode and the intrinsic size, scaling against the origin box for cover/contain/%.
func backgroundTileSize(it *layout.BackgroundImageItem) (w, h float64) {
	iw, ih := it.IntrinsicW, it.IntrinsicH
	switch it.SizeKind {
	case layout.BgSizeCover:
		s := scaleRatio(it.OriginW/iw, it.OriginH/ih, false) // larger ratio
		return iw * s, ih * s
	case layout.BgSizeContain:
		s := scaleRatio(it.OriginW/iw, it.OriginH/ih, true) // smaller ratio
		return iw * s, ih * s
	case layout.BgSizeExplicit:
		w, h = it.SizeW, it.SizeH
		switch {
		case w <= 0 && h <= 0: // both auto → intrinsic
			return iw, ih
		case w <= 0: // width auto → preserve intrinsic ratio from the height
			return h * (iw / ih), h
		case h <= 0: // height auto → preserve intrinsic ratio from the width
			return w, w * (ih / iw)
		default:
			return w, h
		}
	default: // BgSizeAuto → intrinsic
		return iw, ih
	}
}

// bgAxisOffset resolves a background-position component to the tile's leading-edge
// offset within the origin box on one axis. A percentage positions the tile so its
// frac point aligns with the box's frac point: offset = (originSize − tileSize)·frac.
// A length is an absolute offset from the leading edge.
func bgAxisOffset(isPct bool, frac, px, originSize, tileSize float64) float64 {
	if isPct {
		return (originSize - tileSize) * frac
	}
	return px
}

// tilePositions returns the tile leading-edge coordinates along one axis. When repeat
// is false it is just [start] (a single tile). When repeat is true it walks whole tile
// steps so the tiles cover [clipLo, clipHi]: it backs start off to the last step at or
// before clipLo, then steps forward until past clipHi. step ≤ 0 yields a single tile.
func tilePositions(start, step float64, repeat bool, clipLo, clipHi float64) []float64 {
	if !repeat || step <= 0 {
		return []float64{start}
	}
	// First tile origin at or before clipLo.
	first := start - math.Ceil((start-clipLo)/step)*step
	var out []float64
	for x := first; x < clipHi; x += step {
		out = append(out, x)
		if len(out) > maxBackgroundTiles {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, first)
	}
	return out
}
