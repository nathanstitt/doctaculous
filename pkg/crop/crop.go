// Package crop computes crop rectangles for exact-size image output.
//
// Sizing in the rest of omnidoc fits within a pixel box with aspect
// preserved (RasterOptions.MaxWidthPx/MaxHeightPx), which cannot produce an
// exact N×N image from a non-square source. Cropping fills the target instead,
// discarding what falls outside. Rect chooses the rectangle; Scale (scale.go)
// extracts and resizes it.
package crop

import (
	"errors"
	"fmt"
	"image"
)

// Strategy selects how the crop rectangle is placed.
type Strategy int

const (
	// StrategyCenter centers the crop. The zero value, and the safe default.
	StrategyCenter Strategy = iota
	// StrategyNorth anchors to the top edge, StrategySouth the bottom, and
	// StrategyEast/StrategyWest the right/left. The unconstrained axis is
	// still centered.
	StrategyNorth
	StrategySouth
	StrategyEast
	StrategyWest
	// StrategySaliency scores candidate windows on image content and picks
	// the highest. See saliency.go.
	StrategySaliency
	// StrategyRect uses Options.Rect verbatim, clamped to the image bounds.
	// This is how a caller applies a crop a human chose.
	StrategyRect
)

// ErrInvalidSize reports a non-positive target width or height.
var ErrInvalidSize = errors.New("crop: width and height must be positive")

// Options configures Rect and Scale.
type Options struct {
	// Strategy selects the placement. The zero value is StrategyCenter.
	Strategy Strategy
	// Width and Height are the target size in pixels. Both must be positive.
	// They set the crop's aspect ratio; the rectangle Rect returns is the
	// largest window of that ratio that fits the source, not necessarily
	// Width×Height pixels — Scale resizes it to exactly that.
	Width, Height int
	// Rect is the crop rectangle when Strategy is StrategyRect, in source
	// pixel coordinates. Ignored otherwise.
	Rect image.Rectangle

	// Weights tunes saliency scoring; used only by StrategySaliency. A nil
	// field takes the documented default. See Weights and saliency.go.
	Weights Weights
}

// Weights tunes the saliency scorer. Each field is a pointer so that zero is a
// settable value: a nil field takes its default, and an explicit 0 genuinely
// disables that term. A plain float64 could not express "off", because the zero
// value is indistinguishable from an unset one.
type Weights struct {
	// Edge weights Sobel edge energy — detail over flat regions. The dominant
	// term; default 1.0.
	Edge *float64
	// Saturation weights chroma distance from grey; default 0.3.
	Saturation *float64
	// Skin weights the YCbCr skin box; default 0.5. Do not raise this above
	// Edge — see the bias note in saliency.go.
	Skin *float64
	// Center weights the radial centre prior; default 0.3.
	Center *float64
}

// DefaultWeights returns a fully-populated Weights carrying the documented
// defaults, for callers that want to adjust a single term.
func DefaultWeights() Weights {
	edge, sat, skin, center := defaultEdgeWeight, defaultSaturationWeight, defaultSkinWeight, defaultCenterWeight
	return Weights{Edge: &edge, Saturation: &sat, Skin: &skin, Center: &center}
}

// resolve returns *p, or def when p is nil.
func resolve(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// Rect returns the crop rectangle for img under opts, in img's coordinate
// space. The result always lies within img.Bounds().
func Rect(img image.Image, opts Options) (image.Rectangle, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return image.Rectangle{}, fmt.Errorf("crop: %dx%d: %w", opts.Width, opts.Height, ErrInvalidSize)
	}
	b := img.Bounds()
	if b.Empty() {
		return image.Rectangle{}, fmt.Errorf("crop: source image is empty: %w", ErrInvalidSize)
	}
	if opts.Strategy == StrategyRect {
		r := opts.Rect.Intersect(b)
		if r.Empty() {
			return image.Rectangle{}, fmt.Errorf("crop: rect %v does not intersect bounds %v: %w", opts.Rect, b, ErrInvalidSize)
		}
		return r, nil
	}
	win := fitWindow(b, opts.Width, opts.Height)
	if opts.Strategy == StrategySaliency {
		return saliencyRect(img, win, opts), nil
	}
	return anchorWindow(b, win, opts.Strategy), nil
}

// fitWindow returns the largest width×height-ratio window that fits bounds.
// The result is always within bounds on both axes.
func fitWindow(bounds image.Rectangle, w, h int) image.Point {
	bw, bh := bounds.Dx(), bounds.Dy()
	// Compare bw/bh against w/h without floating point: bw*h vs w*bh.
	var win image.Point
	if bw*h > w*bh {
		// Source is wider than the target ratio — height is the limit.
		win = image.Pt(bh*w/h, bh)
	} else {
		win = image.Pt(bw, bw*h/w)
	}
	// Integer division truncates toward zero so the computed side cannot
	// exceed its limit, but clamp both axes anyway: this is the one place a
	// rounding or ratio-equality slip would hand an out-of-bounds window to
	// anchorWindow, and the summed-area table would index past its backing
	// slice. Clamping here keeps every strategy in bounds by construction.
	if win.X > bw {
		win.X = bw
	}
	if win.Y > bh {
		win.Y = bh
	}
	// A degenerate ratio against a tiny source can truncate to zero.
	if win.X < 1 {
		win.X = 1
	}
	if win.Y < 1 {
		win.Y = 1
	}
	return win
}
