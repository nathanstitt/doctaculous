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

func TestCascadeSpecificityAndInheritance(t *testing.T) {
	src := `
		p { color: green; font-size: 12px; }
		.intro { color: blue; }
		#lead { color: red; }
	`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)

	body := &fakeNode{tag: "body"}
	// <p id="lead" class="intro"> inside <body>
	p := &fakeNode{tag: "p", id: "lead", classes: []string{"intro"}, parent: body}

	cs := r.Compute(p, initialStyle())
	// #lead (id) beats .intro (class) beats p (type): red wins.
	if cs.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (id wins)", cs.Color)
	}
	// font-size only set by the p rule: 12.
	if cs.FontSizePt != 12 {
		t.Errorf("font-size = %v, want 12", cs.FontSizePt)
	}
}

func TestCascadeInheritsColorButNotMargin(t *testing.T) {
	src := `div { color: blue; margin-top: 20px; }`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)

	div := &fakeNode{tag: "div"}
	child := &fakeNode{tag: "span", parent: div}

	divStyle := r.Compute(div, initialStyle())
	childStyle := r.Compute(child, divStyle) // parent's computed style is the inheritance base

	// color inherits:
	if childStyle.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("child color = %v, want inherited blue", childStyle.Color)
	}
	// margin does NOT inherit: child keeps the initial 0.
	if childStyle.MarginTop != (Length{0, UnitPx}) {
		t.Errorf("child margin-top = %v, want 0 (not inherited)", childStyle.MarginTop)
	}
}

func TestCascadeImportantWins(t *testing.T) {
	src := `
		#lead { color: red; }
		p { color: green !important; }
	`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)
	p := &fakeNode{tag: "p", id: "lead"}
	cs := r.Compute(p, initialStyle())
	// !important beats higher specificity.
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (!important wins over id)", cs.Color)
	}
}

func TestInlineStyleAttributeWins(t *testing.T) {
	src := `#lead { color: red; }`
	sheet := Parse(src)
	r := NewResolver(sheet, nil)
	// style="color: green" must beat the id rule (inline style has higher origin).
	p := &fakeNode{tag: "p", id: "lead", attrs: map[string]string{"style": "color: green"}}
	cs := r.Compute(p, initialStyle())
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (inline style wins)", cs.Color)
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

func TestCascadeSourceOrderTiebreak(t *testing.T) {
	// Two rules of EQUAL specificity (both type selectors) — the later one wins.
	src := `p { color: red; } p { color: blue; }`
	r := NewResolver(Parse(src), nil)
	cs := r.Compute(&fakeNode{tag: "p"}, initialStyle())
	if cs.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("color = %v, want blue (later equal-specificity rule wins)", cs.Color)
	}
}

func TestCascadeBestMatchUsesMaxSpecificity(t *testing.T) {
	// A rule whose selector GROUP contains a high-specificity selector matching
	// the node contributes that higher specificity, beating a separate type rule.
	src := `
		p { color: red; }
		em, #lead { color: blue; }
	`
	r := NewResolver(Parse(src), nil)
	// <p id="lead"> matches "#lead" in the second rule (spec {1,0,0}), which beats
	// the "p" rule (spec {0,0,1}); the "em" selector in the group doesn't match.
	cs := r.Compute(&fakeNode{tag: "p", id: "lead"}, initialStyle())
	if cs.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("color = %v, want blue (#lead in the group beats p)", cs.Color)
	}
}
