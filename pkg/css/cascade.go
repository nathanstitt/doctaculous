package css

import "image/color"

// ComputedStyle is the resolved style of one element: the normal-flow property
// subset this sub-project supports, with every value concrete. Lengths remain in
// their CSS unit here (px/pt/em/%); the layout engine resolves em/% to absolute
// points against a containing context. Raw, unrecognized declarations are not on
// this struct — they are retained on the Rule for later sub-projects.
type ComputedStyle struct {
	Display string // "block" | "inline" | "none" | "list-item" | raw value

	Color           color.RGBA
	BackgroundColor color.RGBA // zero-alpha means transparent / not set

	FontFamily string
	FontSizePt float64 // resolved to an absolute size (px treated 1:1 as pt for now)
	Bold       bool
	Italic     bool
	LineHeight Length // UnitAuto = "normal"

	TextAlign string // "left" | "right" | "center" | "justify"

	MarginTop, MarginRight, MarginBottom, MarginLeft     Length
	PaddingTop, PaddingRight, PaddingBottom, PaddingLeft Length

	BorderTopWidth, BorderRightWidth, BorderBottomWidth, BorderLeftWidth Length
	BorderTopColor, BorderRightColor, BorderBottomColor, BorderLeftColor color.RGBA
	BorderTopStyle, BorderRightStyle, BorderBottomStyle, BorderLeftStyle string

	Width, Height Length // UnitAuto = "auto"
}

// initialStyle returns a ComputedStyle holding the CSS initial values, used as
// the base for the root element before any rule or inheritance is applied.
func initialStyle() ComputedStyle {
	black := color.RGBA{0, 0, 0, 255}
	return ComputedStyle{
		Display:     "inline",
		Color:       black,
		FontFamily:  "serif",
		FontSizePt:  16,
		LineHeight:  Length{Unit: UnitAuto},
		TextAlign:   "left",
		Width:       Length{Unit: UnitAuto},
		Height:      Length{Unit: UnitAuto},
		MarginTop:   Length{Unit: UnitPx},
		MarginRight: Length{Unit: UnitPx},
		// remaining margins/paddings default to zero px (the zero value of Length is {0,UnitPx})
	}
}
