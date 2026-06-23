package css

import (
	"image/color"
	"strings"
)

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

// applyDeclaration interprets one declaration and writes it onto cs. Properties
// outside the supported normal-flow subset are ignored (left for later
// sub-projects). Malformed values are dropped, leaving the prior value intact.
func applyDeclaration(cs *ComputedStyle, d Declaration) {
	switch d.Property {
	case "display":
		cs.Display = d.Value
	case "color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.Color = c
		}
	case "background-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BackgroundColor = c
		}
	case "font-family":
		cs.FontFamily = firstFamily(d.Value)
	case "font-size":
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok && l.Unit != UnitAuto {
			cs.FontSizePt = l.Value // px:pt 1:1 for now; em/% resolution is the engine's job
		}
	case "font-weight":
		cs.Bold = d.Value == "bold" || d.Value == "700" || d.Value == "800" || d.Value == "900"
	case "font-style":
		cs.Italic = d.Value == "italic" || d.Value == "oblique"
	case "line-height":
		if l, ok := parseLength(newTokenizer(d.Value).next()); ok {
			cs.LineHeight = l
		} else if d.Value == "normal" {
			cs.LineHeight = Length{Unit: UnitAuto}
		}
	case "text-align":
		switch d.Value {
		case "left", "right", "center", "justify":
			cs.TextAlign = d.Value
		}
	case "margin-top":
		setLength(&cs.MarginTop, d.Value)
	case "margin-right":
		setLength(&cs.MarginRight, d.Value)
	case "margin-bottom":
		setLength(&cs.MarginBottom, d.Value)
	case "margin-left":
		setLength(&cs.MarginLeft, d.Value)
	case "padding-top":
		setLength(&cs.PaddingTop, d.Value)
	case "padding-right":
		setLength(&cs.PaddingRight, d.Value)
	case "padding-bottom":
		setLength(&cs.PaddingBottom, d.Value)
	case "padding-left":
		setLength(&cs.PaddingLeft, d.Value)
	case "width":
		setLength(&cs.Width, d.Value)
	case "height":
		setLength(&cs.Height, d.Value)
	case "border-top-width":
		setLength(&cs.BorderTopWidth, d.Value)
	case "border-right-width":
		setLength(&cs.BorderRightWidth, d.Value)
	case "border-bottom-width":
		setLength(&cs.BorderBottomWidth, d.Value)
	case "border-left-width":
		setLength(&cs.BorderLeftWidth, d.Value)
	case "border-top-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderTopColor = c
		}
	case "border-right-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderRightColor = c
		}
	case "border-bottom-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderBottomColor = c
		}
	case "border-left-color":
		if c, ok := parseColor(newTokenizer(d.Value)); ok {
			cs.BorderLeftColor = c
		}
	case "border-top-style":
		cs.BorderTopStyle = d.Value
	case "border-right-style":
		cs.BorderRightStyle = d.Value
	case "border-bottom-style":
		cs.BorderBottomStyle = d.Value
	case "border-left-style":
		cs.BorderLeftStyle = d.Value
	}
	// default: unsupported property — ignored on purpose.
}

// setLength parses val as a length and writes it to dst when valid.
func setLength(dst *Length, val string) {
	if l, ok := parseLength(newTokenizer(val).next()); ok {
		*dst = l
	}
}

// firstFamily returns the first family name from a font-family list, stripping
// quotes and whitespace (e.g. `"Helvetica Neue", Arial` -> `Helvetica Neue`).
func firstFamily(val string) string {
	for _, part := range splitComma(val) {
		part = trimQuotes(strings.TrimSpace(part))
		if part != "" {
			return part
		}
	}
	return val
}

func splitComma(s string) []string { return strings.Split(s, ",") }

func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
