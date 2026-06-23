package css

import (
	"image/color"
	"testing"
)

func TestInitialComputedStyle(t *testing.T) {
	cs := initialStyle()
	if cs.Display != "inline" { // CSS initial value of display is inline
		t.Fatalf("initial display = %q, want inline", cs.Display)
	}
	if cs.Color != (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("initial color = %v, want black", cs.Color)
	}
	if cs.FontSizePt != 16 { // 16px default medium, expressed in px-as-pt placeholder
		t.Fatalf("initial font-size = %v, want 16", cs.FontSizePt)
	}
	if cs.FontFamily != "serif" {
		t.Errorf("initial font-family = %q, want serif", cs.FontFamily)
	}
	if cs.LineHeight.Unit != UnitAuto {
		t.Errorf("initial line-height unit = %v, want UnitAuto (normal)", cs.LineHeight.Unit)
	}
	if cs.TextAlign != "left" {
		t.Errorf("initial text-align = %q, want left", cs.TextAlign)
	}
	if cs.Width.Unit != UnitAuto || cs.Height.Unit != UnitAuto {
		t.Errorf("initial width/height = %v/%v, want auto/auto", cs.Width.Unit, cs.Height.Unit)
	}
	// BackgroundColor is transparent by zero value (alpha 0 = "not set").
	if cs.BackgroundColor != (color.RGBA{}) {
		t.Errorf("initial background-color = %v, want transparent zero value", cs.BackgroundColor)
	}
	// Unset margins/paddings are 0px (the Length zero value).
	if cs.MarginBottom != (Length{}) || cs.PaddingLeft != (Length{}) {
		t.Errorf("initial margin-bottom/padding-left = %v/%v, want zero 0px", cs.MarginBottom, cs.PaddingLeft)
	}
}

func TestApplyDeclaration(t *testing.T) {
	cs := initialStyle()
	apply := func(prop, val string) { applyDeclaration(&cs, Declaration{Property: prop, Value: val}) }

	apply("display", "block")
	apply("color", "red")
	apply("background-color", "#ffffff")
	apply("font-weight", "bold")
	apply("font-style", "italic")
	apply("text-align", "center")
	apply("margin-top", "10px")
	apply("width", "50%")

	if cs.Display != "block" {
		t.Errorf("display = %q", cs.Display)
	}
	if cs.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v", cs.Color)
	}
	if cs.BackgroundColor != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("background = %v", cs.BackgroundColor)
	}
	if !cs.Bold {
		t.Errorf("bold not set")
	}
	if !cs.Italic {
		t.Errorf("italic not set")
	}
	if cs.TextAlign != "center" {
		t.Errorf("text-align = %q", cs.TextAlign)
	}
	if cs.MarginTop != (Length{10, UnitPx}) {
		t.Errorf("margin-top = %v", cs.MarginTop)
	}
	if cs.Width != (Length{50, UnitPercent}) {
		t.Errorf("width = %v", cs.Width)
	}
}

func TestApplyUnknownPropertyIgnored(t *testing.T) {
	cs := initialStyle()
	before := cs
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "rotate(5deg)"})
	if cs != before {
		t.Fatalf("unknown property changed the computed style")
	}
}

func TestApplyDeclarationDegradationAndFamily(t *testing.T) {
	// A malformed value leaves the prior value intact (the documented contract).
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "color", Value: "blue"})
	applyDeclaration(&cs, Declaration{Property: "color", Value: "notacolor"})
	if cs.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("color = %v, want blue preserved (malformed value must not overwrite)", cs.Color)
	}
	// Likewise for a length.
	applyDeclaration(&cs, Declaration{Property: "margin-top", Value: "12px"})
	applyDeclaration(&cs, Declaration{Property: "margin-top", Value: "garbage"})
	if cs.MarginTop != (Length{12, UnitPx}) {
		t.Errorf("margin-top = %v, want 12px preserved", cs.MarginTop)
	}
	// font-family: first family wins, quotes stripped, through applyDeclaration.
	applyDeclaration(&cs, Declaration{Property: "font-family", Value: `"Helvetica Neue", Arial, sans-serif`})
	if cs.FontFamily != "Helvetica Neue" {
		t.Errorf("font-family = %q, want \"Helvetica Neue\"", cs.FontFamily)
	}
	// A bare single family resolves to itself.
	applyDeclaration(&cs, Declaration{Property: "font-family", Value: "Georgia"})
	if cs.FontFamily != "Georgia" {
		t.Errorf("font-family = %q, want Georgia", cs.FontFamily)
	}
	// font-size: auto is dropped (not a valid font-size), prior size preserved.
	before := cs.FontSizePt
	applyDeclaration(&cs, Declaration{Property: "font-size", Value: "auto"})
	if cs.FontSizePt != before {
		t.Errorf("font-size after auto = %v, want %v preserved", cs.FontSizePt, before)
	}
}
