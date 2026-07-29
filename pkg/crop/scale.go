package crop

import (
	"image"

	xdraw "golang.org/x/image/draw"
)

// Scale crops img per opts and resizes the result to exactly
// opts.Width×opts.Height. The returned image's bounds start at (0,0).
//
// Resampling is CatmullRom, which is sharper than bilinear on the downscales
// this is normally used for and does not ring the way Lanczos can on hard
// edges. Sources smaller than the target are upscaled rather than padded, so
// the result is always exactly the requested size.
func Scale(img image.Image, opts Options) (image.Image, error) {
	out, _, err := ScaleRect(img, opts)
	return out, err
}

// ScaleRect is Scale, and additionally reports the source rectangle it cropped,
// in img's coordinate space.
//
// The rectangle is the interesting output when Strategy is StrategySaliency:
// the scorer picks it from image content, so it is not predictable from the
// options alone. A caller that wants to record where a smart crop landed — to
// display it, to audit it, or to reproduce the identical crop later by passing
// it back as Options.Rect with StrategyRect — needs it returned.
func ScaleRect(img image.Image, opts Options) (image.Image, image.Rectangle, error) {
	r, err := Rect(img, opts)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, r, xdraw.Src, nil)
	return dst, r, nil
}
