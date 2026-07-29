package crop

import "image"

// Default scoring weights. Edge energy dominates; skin only nudges. See the
// bias note in the package docs before changing defaultSkinWeight.
const (
	defaultEdgeWeight       = 1.0
	defaultSaturationWeight = 0.3
	defaultSkinWeight       = 0.5
	defaultCenterWeight     = 0.3
)

// saliencyRect is implemented in Task 2. Until then it behaves as a center
// crop so the package compiles and gravity tests pass.
func saliencyRect(img image.Image, win image.Point, opts Options) image.Rectangle {
	return anchorWindow(img.Bounds(), win, StrategyCenter)
}
