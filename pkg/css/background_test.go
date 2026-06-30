package css

import (
	"image/color"
	"testing"
)

func TestBackgroundImageLonghand(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background-image", Value: `url("bg.png")`})
	if cs.BackgroundImage != "bg.png" {
		t.Errorf("background-image = %q, want %q", cs.BackgroundImage, "bg.png")
	}
	// none clears it.
	applyDeclaration(&cs, Declaration{Property: "background-image", Value: "none"})
	if cs.BackgroundImage != "" {
		t.Errorf("background-image:none = %q, want empty", cs.BackgroundImage)
	}
	// A gradient is not a url(): left unchanged (prior value kept).
	cs.BackgroundImage = "keep.png"
	applyDeclaration(&cs, Declaration{Property: "background-image", Value: "linear-gradient(red, blue)"})
	if cs.BackgroundImage != "keep.png" {
		t.Errorf("gradient should be ignored, got %q", cs.BackgroundImage)
	}
	// url() is case-insensitive.
	applyDeclaration(&cs, Declaration{Property: "background-image", Value: `URL("up.png")`})
	if cs.BackgroundImage != "up.png" {
		t.Errorf("uppercase URL() = %q, want up.png", cs.BackgroundImage)
	}
}

// The background shorthand resets background-color too: `background: url(x)` with no
// color clears a previously-set color to transparent (CSS shorthand reset semantics).
func TestBackgroundShorthandResetsColor(t *testing.T) {
	loaderColor := rgbaOf(1, 2, 3)
	cs := initialStyle()
	cs.BackgroundColor = loaderColor
	applyDeclaration(&cs, Declaration{Property: "background", Value: "url(x.png)"})
	if cs.BackgroundColor != (color.RGBA{}) {
		t.Errorf("shorthand kept color %+v, want transparent", cs.BackgroundColor)
	}
	if cs.BackgroundImage != "x.png" {
		t.Errorf("shorthand image = %q, want x.png", cs.BackgroundImage)
	}
}

func TestBackgroundLonghandsInitials(t *testing.T) {
	cs := initialStyle()
	if cs.BackgroundRepeat != "repeat" || cs.BackgroundOrigin != "padding-box" ||
		cs.BackgroundClip != "border-box" || cs.BackgroundAttach != "scroll" {
		t.Errorf("initials wrong: repeat=%q origin=%q clip=%q attach=%q",
			cs.BackgroundRepeat, cs.BackgroundOrigin, cs.BackgroundClip, cs.BackgroundAttach)
	}
	if cs.BackgroundPosition.X.Unit != UnitPercent || cs.BackgroundPosition.X.Value != 0 ||
		cs.BackgroundPosition.Y.Value != 0 {
		t.Errorf("initial position = %+v, want 0%% 0%%", cs.BackgroundPosition)
	}
	if cs.BackgroundSize.Kind != BgSizeAuto {
		t.Errorf("initial size kind = %v, want auto", cs.BackgroundSize.Kind)
	}
}

func TestBackgroundPositionParsing(t *testing.T) {
	cases := []struct {
		in     string
		wantXV float64
		wantXU LengthUnit
		wantYV float64
		wantYU LengthUnit
	}{
		{"left top", 0, UnitPercent, 0, UnitPercent},
		{"right bottom", 100, UnitPercent, 100, UnitPercent},
		{"center", 50, UnitPercent, 50, UnitPercent},
		{"top", 50, UnitPercent, 0, UnitPercent},             // single keyword → other axis center
		{"bottom right", 100, UnitPercent, 100, UnitPercent}, // keyword order swapped
		{"25% 75%", 25, UnitPercent, 75, UnitPercent},
		{"10px 20px", 10, UnitPx, 20, UnitPx},
		// Mixed keyword + length: the keyword locks its axis and the length goes to the
		// other axis (CSS background-position).
		{"left 25%", 0, UnitPercent, 25, UnitPercent},    // left → X=0, 25% → Y
		{"center 10px", 50, UnitPercent, 10, UnitPx},     // center → X (default 50%), 10px → Y
		{"bottom 30px", 30, UnitPx, 100, UnitPercent},    // bottom → Y=100, 30px → X
		{"right 40%", 100, UnitPercent, 40, UnitPercent}, // right → X=100, 40% → Y
	}
	for _, c := range cases {
		p, ok := parseBackgroundPosition(c.in)
		if !ok {
			t.Errorf("parseBackgroundPosition(%q) ok=false", c.in)
			continue
		}
		if p.X.Value != c.wantXV || p.X.Unit != c.wantXU || p.Y.Value != c.wantYV || p.Y.Unit != c.wantYU {
			t.Errorf("position(%q) = %+v, want X{%v,%v} Y{%v,%v}", c.in, p, c.wantXV, c.wantXU, c.wantYV, c.wantYU)
		}
	}
}

func TestBackgroundSizeParsing(t *testing.T) {
	if s, ok := parseBackgroundSize("cover"); !ok || s.Kind != BgSizeCover {
		t.Errorf("cover = %+v ok=%v", s, ok)
	}
	if s, ok := parseBackgroundSize("contain"); !ok || s.Kind != BgSizeContain {
		t.Errorf("contain = %+v ok=%v", s, ok)
	}
	// Single explicit value sizes width, leaves height auto.
	s, ok := parseBackgroundSize("50px")
	if !ok || s.Kind != BgSizeExplicit || s.W.Value != 50 || s.W.Unit != UnitPx || s.H.Unit != UnitAuto {
		t.Errorf("50px = %+v ok=%v", s, ok)
	}
	// Two explicit values.
	s2, ok := parseBackgroundSize("50% auto")
	if !ok || s2.W.Unit != UnitPercent || s2.W.Value != 50 || s2.H.Unit != UnitAuto {
		t.Errorf("50%% auto = %+v ok=%v", s2, ok)
	}
}

func TestBackgroundShorthand(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background",
		Value: `#eee url(tile.png) no-repeat center / cover`})
	if cs.BackgroundColor != (rgbaOf(0xee, 0xee, 0xee)) {
		t.Errorf("shorthand color = %+v", cs.BackgroundColor)
	}
	if cs.BackgroundImage != "tile.png" {
		t.Errorf("shorthand image = %q", cs.BackgroundImage)
	}
	if cs.BackgroundRepeat != "no-repeat" {
		t.Errorf("shorthand repeat = %q", cs.BackgroundRepeat)
	}
	if cs.BackgroundPosition.X.Value != 50 || cs.BackgroundPosition.Y.Value != 50 {
		t.Errorf("shorthand position = %+v", cs.BackgroundPosition)
	}
	if cs.BackgroundSize.Kind != BgSizeCover {
		t.Errorf("shorthand size = %+v", cs.BackgroundSize)
	}
}

// The shorthand resets longhands: setting `background: red` after an image clears the
// image (per CSS shorthand reset semantics).
func TestBackgroundShorthandResets(t *testing.T) {
	cs := initialStyle()
	cs.BackgroundImage = "old.png"
	cs.BackgroundRepeat = "no-repeat"
	applyDeclaration(&cs, Declaration{Property: "background", Value: "red"})
	if cs.BackgroundImage != "" {
		t.Errorf("shorthand did not reset image: %q", cs.BackgroundImage)
	}
	if cs.BackgroundRepeat != "repeat" {
		t.Errorf("shorthand did not reset repeat: %q", cs.BackgroundRepeat)
	}
	if cs.BackgroundColor != (rgbaOf(255, 0, 0)) {
		t.Errorf("shorthand color = %+v, want red", cs.BackgroundColor)
	}
}

// background-origin / background-clip share box keywords in the shorthand: first sets
// both origin and clip, a second sets clip only.
func TestBackgroundShorthandBoxKeywords(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background",
		Value: "url(x.png) content-box padding-box"})
	if cs.BackgroundOrigin != "content-box" || cs.BackgroundClip != "padding-box" {
		t.Errorf("origin/clip = %q/%q, want content-box/padding-box", cs.BackgroundOrigin, cs.BackgroundClip)
	}
}

func rgbaOf(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }

// (text-decoration lives in the same value/cascade area; tested here for convenience.)
func TestTextDecorationParsing(t *testing.T) {
	cs := initialStyle()
	if cs.TextDecorationLine != "none" {
		t.Errorf("initial text-decoration = %q, want none", cs.TextDecorationLine)
	}
	applyDeclaration(&cs, Declaration{Property: "text-decoration", Value: "underline"})
	if cs.TextDecorationLine != "underline" {
		t.Errorf("text-decoration:underline = %q", cs.TextDecorationLine)
	}
	// The shorthand may carry color/style too; the underline keyword still wins.
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "text-decoration", Value: "underline red solid"})
	if cs2.TextDecorationLine != "underline" {
		t.Errorf("text-decoration shorthand = %q, want underline", cs2.TextDecorationLine)
	}
	// none clears it; an unsupported-only line (line-through) reads as none.
	applyDeclaration(&cs, Declaration{Property: "text-decoration-line", Value: "none"})
	if cs.TextDecorationLine != "none" {
		t.Errorf("text-decoration-line:none = %q", cs.TextDecorationLine)
	}
	applyDeclaration(&cs, Declaration{Property: "text-decoration", Value: "line-through"})
	if cs.TextDecorationLine != "none" {
		t.Errorf("unsupported line-through = %q, want none", cs.TextDecorationLine)
	}
}
