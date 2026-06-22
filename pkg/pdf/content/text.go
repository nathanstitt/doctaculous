package content

import "github.com/nathanstitt/doctaculous/pkg/render"

// textState holds the PDF text-related parameters set between BT/ET and by the
// text-state operators (Tc, Tw, Tz, TL, Tf, Tr, Ts).
type textState struct {
	font     *loadedFont
	fontSize float64

	charSpacing float64 // Tc, unscaled text-space units
	wordSpacing float64 // Tw
	hScale      float64 // Tz / 100 (so 1.0 == 100%)
	leading     float64 // TL
	rise        float64 // Ts
	renderMode  int     // Tr

	// matrix (Tm) and lineMatrix are the text and text-line matrices, reset at BT.
	matrix     render.Matrix
	lineMatrix render.Matrix
}

func newTextState() textState {
	return textState{
		hScale:     1,
		matrix:     render.Identity,
		lineMatrix: render.Identity,
	}
}
