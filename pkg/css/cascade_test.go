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

func TestInitialSizingDefaults(t *testing.T) {
	cs := initialStyle()
	// min-* initial is 0px; max-* initial is "none" (modeled UnitAuto).
	if cs.MinWidth != (Length{0, UnitPx}) || cs.MinHeight != (Length{0, UnitPx}) {
		t.Errorf("initial min-width/min-height = %v/%v, want 0px/0px", cs.MinWidth, cs.MinHeight)
	}
	if cs.MaxWidth.Unit != UnitAuto || cs.MaxHeight.Unit != UnitAuto {
		t.Errorf("initial max-width/max-height = %v/%v, want none (UnitAuto)", cs.MaxWidth, cs.MaxHeight)
	}
	if cs.BoxSizing != "content-box" {
		t.Errorf("initial box-sizing = %q, want content-box", cs.BoxSizing)
	}
}

func TestApplyMinMaxSizingAndBoxSizing(t *testing.T) {
	cs := initialStyle()
	apply := func(prop, val string) { applyDeclaration(&cs, Declaration{Property: prop, Value: val}) }

	// div { width: 50%; max-width: 300px; min-width: 100px; box-sizing: border-box; }
	apply("width", "50%")
	apply("max-width", "300px")
	apply("min-width", "100px")
	apply("box-sizing", "border-box")

	if cs.Width != (Length{50, UnitPercent}) {
		t.Errorf("width = %v, want {50 percent}", cs.Width)
	}
	if cs.MaxWidth != (Length{300, UnitPx}) {
		t.Errorf("max-width = %v, want {300 px}", cs.MaxWidth)
	}
	if cs.MinWidth != (Length{100, UnitPx}) {
		t.Errorf("min-width = %v, want {100 px}", cs.MinWidth)
	}
	if cs.BoxSizing != "border-box" {
		t.Errorf("box-sizing = %q, want border-box", cs.BoxSizing)
	}

	// max-width: none yields the no-maximum sentinel (UnitAuto).
	apply("max-width", "none")
	if cs.MaxWidth.Unit != UnitAuto {
		t.Errorf("max-width after none = %v, want none (UnitAuto)", cs.MaxWidth)
	}

	// min-height / max-height carry em units through unchanged.
	apply("min-height", "2em")
	apply("max-height", "10em")
	if cs.MinHeight != (Length{2, UnitEm}) {
		t.Errorf("min-height = %v, want {2 em}", cs.MinHeight)
	}
	if cs.MaxHeight != (Length{10, UnitEm}) {
		t.Errorf("max-height = %v, want {10 em}", cs.MaxHeight)
	}
}

func TestApplySizingDegradation(t *testing.T) {
	// A malformed value leaves the default intact (the documented contract).
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "max-width", Value: "bogus"})
	if cs.MaxWidth.Unit != UnitAuto {
		t.Errorf("max-width after bogus = %v, want none (UnitAuto) preserved", cs.MaxWidth)
	}
	applyDeclaration(&cs, Declaration{Property: "min-width", Value: "-"})
	if cs.MinWidth != (Length{0, UnitPx}) {
		t.Errorf("min-width after bad value = %v, want 0px preserved", cs.MinWidth)
	}
	// An invalid box-sizing keyword is dropped, default preserved.
	applyDeclaration(&cs, Declaration{Property: "box-sizing", Value: "padding-box"})
	if cs.BoxSizing != "content-box" {
		t.Errorf("box-sizing after invalid keyword = %q, want content-box preserved", cs.BoxSizing)
	}
}

func TestSizingNotInherited(t *testing.T) {
	// Parent sets min/max-width and box-sizing; the child has no sizing
	// declarations and must reset to initial (these properties are NOT inherited).
	src := `div { max-width: 300px; box-sizing: border-box; min-width: 50px; }`
	sheet := Parse(src)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

	div := &fakeNode{tag: "div"}
	child := &fakeNode{tag: "span", parent: div}

	divStyle := r.ComputeRoot(div)
	childStyle := r.Compute(child, divStyle) // parent's computed style is the inheritance base

	// Sanity: the parent actually picked up the declarations.
	if divStyle.MaxWidth != (Length{300, UnitPx}) || divStyle.BoxSizing != "border-box" {
		t.Fatalf("parent sizing not applied: max-width=%v box-sizing=%q", divStyle.MaxWidth, divStyle.BoxSizing)
	}
	// Child resets to initial, not inherited.
	if childStyle.MaxWidth.Unit != UnitAuto {
		t.Errorf("child max-width = %v, want none/UnitAuto (initial, not inherited 300px)", childStyle.MaxWidth)
	}
	if childStyle.MinWidth != (Length{0, UnitPx}) {
		t.Errorf("child min-width = %v, want 0px (initial, not inherited)", childStyle.MinWidth)
	}
	if childStyle.BoxSizing != "content-box" {
		t.Errorf("child box-sizing = %q, want content-box (initial, not inherited)", childStyle.BoxSizing)
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
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

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
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

	div := &fakeNode{tag: "div"}
	child := &fakeNode{tag: "span", parent: div}

	divStyle := r.ComputeRoot(div)
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
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)
	p := &fakeNode{tag: "p", id: "lead"}
	cs := r.ComputeRoot(p)
	// !important beats higher specificity.
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (!important wins over id)", cs.Color)
	}
}

func TestInlineStyleAttributeWins(t *testing.T) {
	src := `#lead { color: red; }`
	sheet := Parse(src)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)
	// style="color: green" must beat the id rule (inline style has higher origin).
	p := &fakeNode{tag: "p", id: "lead", attrs: map[string]string{"style": "color: green"}}
	cs := r.ComputeRoot(p)
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (inline style wins)", cs.Color)
	}
}

func TestInlineImportantBeatsAuthorImportant(t *testing.T) {
	src := `p { color: red !important; }`
	r := NewResolver([]OriginSheet{{Sheet: Parse(src), Origin: OriginAuthor}}, nil)
	p := &fakeNode{tag: "p", attrs: map[string]string{"style": "color: green !important"}}
	cs := r.ComputeRoot(p)
	if cs.Color != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (inline !important > author !important)", cs.Color)
	}
}

func TestAuthorImportantBeatsInlineNormal(t *testing.T) {
	src := `p { color: red !important; }`
	r := NewResolver([]OriginSheet{{Sheet: Parse(src), Origin: OriginAuthor}}, nil)
	p := &fakeNode{tag: "p", attrs: map[string]string{"style": "color: green"}}
	cs := r.ComputeRoot(p)
	if cs.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (author !important > inline normal)", cs.Color)
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
	r := NewResolver([]OriginSheet{{Sheet: Parse(src), Origin: OriginAuthor}}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
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
	r := NewResolver([]OriginSheet{{Sheet: Parse(src), Origin: OriginAuthor}}, nil)
	// <p id="lead"> matches "#lead" in the second rule (spec {1,0,0}), which beats
	// the "p" rule (spec {0,0,1}); the "em" selector in the group doesn't match.
	cs := r.ComputeRoot(&fakeNode{tag: "p", id: "lead"})
	if cs.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("color = %v, want blue (#lead in the group beats p)", cs.Color)
	}
}

func TestOriginUALosesToAuthorAcrossSpecificity(t *testing.T) {
	// UA rule has STRICTLY HIGHER specificity (an id selector) and is listed LAST,
	// so neither specificity nor source order favors the author — only origin
	// precedence can make the author's lower-specificity rule win. This is what
	// "author normal beats UA normal" actually means.
	author := Parse(`div { color: green; }`) // type selector: specificity (0,0,1)
	ua := Parse(`#lead { color: red; }`)     // id selector: specificity (1,0,0)
	r := NewResolver([]OriginSheet{
		{Sheet: author, Origin: OriginAuthor},
		{Sheet: ua, Origin: OriginUA}, // listed last: source order would favor it, but origin must override
	}, nil)
	node := &fakeNode{tag: "div", id: "lead"} // matches both selectors
	cs := r.ComputeRoot(node)
	if (cs.Color != color.RGBA{0, 128, 0, 255}) {
		t.Errorf("color = %v, want green (author normal beats UA normal despite UA's higher specificity)", cs.Color)
	}
}

func TestUAImportantBeatsAuthorNormal(t *testing.T) {
	ua := Parse(`p { color: red !important; }`)
	author := Parse(`p { color: green; }`)
	r := NewResolver([]OriginSheet{
		{Sheet: ua, Origin: OriginUA},
		{Sheet: author, Origin: OriginAuthor},
	}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
	if (cs.Color != color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (UA !important beats author normal)", cs.Color)
	}
}

func TestUAImportantBeatsAuthorImportant(t *testing.T) {
	// The top of the cascade ladder: UA-important outranks author-important. The
	// author rule is listed last and has equal specificity, so only origin
	// precedence in the important pass can make the UA rule win.
	author := Parse(`p { color: green !important; }`)
	ua := Parse(`p { color: red !important; }`)
	r := NewResolver([]OriginSheet{
		{Sheet: ua, Origin: OriginUA},
		{Sheet: author, Origin: OriginAuthor},
	}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "p"})
	if (cs.Color != color.RGBA{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red (UA !important beats author !important)", cs.Color)
	}
}

func TestComputeRootUsesInitialBase(t *testing.T) {
	r := NewResolver([]OriginSheet{{Sheet: Parse(``), Origin: OriginAuthor}}, nil)
	cs := r.ComputeRoot(&fakeNode{tag: "html"})
	if cs.FontSizePt != 16 || cs.Color != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("root base = {size %v color %v}, want initial {16 black}", cs.FontSizePt, cs.Color)
	}
}
