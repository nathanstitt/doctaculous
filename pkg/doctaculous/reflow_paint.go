package doctaculous

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

// errPageOutOfRange reports a page index outside [0,count).
func errPageOutOfRange(index, count int) error {
	return fmt.Errorf("page index %d out of range [0,%d)", index, count)
}
