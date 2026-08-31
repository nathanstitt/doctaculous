package omnidoc

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
)

// fillBackground paints img a solid color before content is drawn. It mirrors the
// raster backend's own background fill (which is unexported); the operation is a
// single draw.Src so duplicating it here avoids widening the raster API.
func fillBackground(img *image.RGBA, c color.Color) {
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
}

// errPageOutOfRange reports a page index outside [0,count). It wraps
// [ErrPageOutOfRange] so a caller can branch on the condition with errors.Is
// rather than string-matching, while the message still names the real bounds.
func errPageOutOfRange(index, count int) error {
	return fmt.Errorf("%w: page index %d out of range [0,%d)", ErrPageOutOfRange, index, count)
}

// maxRasterPixels bounds a rasterized page's pixel count. It must stay equal to
// pkg/render/raster's unexported maxPixels: both guard the same "don't allocate
// an attacker-controlled image" hazard on their own rasterization path (PDF vs.
// reflow), so a change to one bound should be a change to both. At 1<<27
// (~134M px) that's roughly an 11600x11600 square, or a 200in-wide poster at
// over 300 DPI — generous for any legitimate document, bounded against one that
// isn't (e.g. an SVG with width="1e6").
const maxRasterPixels = 1 << 27
