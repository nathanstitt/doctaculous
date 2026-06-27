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
	if cs.ObjectFit != "fill" {
		t.Errorf("initial object-fit = %q, want fill", cs.ObjectFit)
	}
}

// TestApplyObjectFit: each valid object-fit keyword is accepted; an invalid one is
// dropped and the default preserved.
func TestApplyObjectFit(t *testing.T) {
	for _, kw := range []string{"fill", "contain", "cover", "none", "scale-down"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "object-fit", Value: kw})
		if cs.ObjectFit != kw {
			t.Errorf("object-fit %q not applied, got %q", kw, cs.ObjectFit)
		}
	}
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "object-fit", Value: "stretch"}) // invalid
	if cs.ObjectFit != "fill" {
		t.Errorf("object-fit after invalid keyword = %q, want fill preserved", cs.ObjectFit)
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
	// Verify that applying an unknown property leaves all fields at their initial
	// values. We check a representative sample of comparable fields; the full
	// struct is not directly comparable because ComputedStyle contains slices and
	// maps (TrackList.entries, GridAreas.Named).
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "transform", Value: "rotate(5deg)"})
	if cs.Display != "inline" {
		t.Errorf("Display changed after unknown property: %q", cs.Display)
	}
	if cs.FlexDirection != "row" {
		t.Errorf("FlexDirection changed after unknown property: %q", cs.FlexDirection)
	}
	if cs.GridAutoFlow != "row" {
		t.Errorf("GridAutoFlow changed after unknown property: %q", cs.GridAutoFlow)
	}
	if cs.JustifyItems != "stretch" {
		t.Errorf("JustifyItems changed after unknown property: %q", cs.JustifyItems)
	}
	if !cs.GridTemplateColumns.IsEmpty() {
		t.Error("GridTemplateColumns changed after unknown property")
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

// TestInitialFloatClear: the cascade defaults float and clear to "none".
func TestInitialFloatClear(t *testing.T) {
	cs := initialStyle()
	if cs.Float != "none" {
		t.Errorf("initial float = %q, want none", cs.Float)
	}
	if cs.Clear != "none" {
		t.Errorf("initial clear = %q, want none", cs.Clear)
	}
}

// TestApplyFloatClear: each valid float/clear keyword is accepted; an invalid one
// is dropped (leaving the prior value).
func TestApplyFloatClear(t *testing.T) {
	for _, kw := range []string{"left", "right", "none"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "float", Value: kw})
		if cs.Float != kw {
			t.Errorf("float %q not applied, got %q", kw, cs.Float)
		}
	}
	for _, kw := range []string{"left", "right", "both", "none"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "clear", Value: kw})
		if cs.Clear != kw {
			t.Errorf("clear %q not applied, got %q", kw, cs.Clear)
		}
	}
	// Invalid values are dropped, default preserved.
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "float", Value: "center"})
	if cs.Float != "none" {
		t.Errorf("float after invalid keyword = %q, want none preserved", cs.Float)
	}
	applyDeclaration(&cs, Declaration{Property: "clear", Value: "all"})
	if cs.Clear != "none" {
		t.Errorf("clear after invalid keyword = %q, want none preserved", cs.Clear)
	}
}

// TestFloatNotInherited: float/clear are not inherited (a child without its own
// float defaults to none even if the parent floats).
func TestFloatNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Float = "left"
	parent.Clear = "both"
	child := inheritFrom(parent)
	if child.Float != "none" || child.Clear != "none" {
		t.Errorf("float/clear inherited: got float=%q clear=%q, want none/none", child.Float, child.Clear)
	}
}

// TestInitialPosition: the cascade defaults position to "static", the offsets to
// auto, and z-index to auto.
func TestInitialPosition(t *testing.T) {
	cs := initialStyle()
	if cs.Position != "static" {
		t.Errorf("initial position = %q, want static", cs.Position)
	}
	for name, l := range map[string]Length{"top": cs.Top, "right": cs.Right, "bottom": cs.Bottom, "left": cs.Left} {
		if l.Unit != UnitAuto {
			t.Errorf("initial %s = %+v, want UnitAuto", name, l)
		}
	}
	if !cs.ZIndexAuto {
		t.Errorf("initial z-index not auto (ZIndexAuto=%v)", cs.ZIndexAuto)
	}
}

// TestApplyPosition: each valid position keyword is accepted; an invalid one is
// dropped (leaving the prior value).
func TestApplyPosition(t *testing.T) {
	for _, kw := range []string{"static", "relative", "absolute", "fixed"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "position", Value: kw})
		if cs.Position != kw {
			t.Errorf("position %q not applied, got %q", kw, cs.Position)
		}
	}
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "position", Value: "sticky"}) // unsupported here
	if cs.Position != "static" {
		t.Errorf("position after unsupported keyword = %q, want static preserved", cs.Position)
	}
}

// TestApplyOffsets: top/right/bottom/left parse as lengths; auto is accepted.
func TestApplyOffsets(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "top", Value: "10px"})
	applyDeclaration(&cs, Declaration{Property: "left", Value: "20px"})
	applyDeclaration(&cs, Declaration{Property: "right", Value: "auto"})
	if cs.Top.Unit != UnitPx || cs.Top.Value != 10 {
		t.Errorf("top = %+v, want 10px", cs.Top)
	}
	if cs.Left.Unit != UnitPx || cs.Left.Value != 20 {
		t.Errorf("left = %+v, want 20px", cs.Left)
	}
	if cs.Right.Unit != UnitAuto {
		t.Errorf("right = %+v, want UnitAuto", cs.Right)
	}
}

// TestApplyZIndex: an integer z-index is parsed (ZIndexAuto=false); "auto" stays
// auto; a non-integer is dropped.
func TestApplyZIndex(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "5"})
	if cs.ZIndexAuto || cs.ZIndex != 5 {
		t.Errorf("z-index 5: got ZIndex=%d ZIndexAuto=%v, want 5/false", cs.ZIndex, cs.ZIndexAuto)
	}
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "-2"})
	if cs.ZIndexAuto || cs.ZIndex != -2 {
		t.Errorf("z-index -2: got ZIndex=%d ZIndexAuto=%v, want -2/false", cs.ZIndex, cs.ZIndexAuto)
	}
	cs.ZIndex, cs.ZIndexAuto = 7, false
	applyDeclaration(&cs, Declaration{Property: "z-index", Value: "auto"})
	if !cs.ZIndexAuto {
		t.Errorf("z-index auto: ZIndexAuto=%v, want true", cs.ZIndexAuto)
	}
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "z-index", Value: "1.5"}) // non-integer dropped
	if !cs2.ZIndexAuto {
		t.Errorf("z-index 1.5 should be dropped, ZIndexAuto=%v", cs2.ZIndexAuto)
	}
}

// TestPositionNotInherited: position/offsets/z-index are not inherited.
func TestPositionNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Position = "relative"
	parent.Top = Length{Value: 10, Unit: UnitPx}
	parent.ZIndex, parent.ZIndexAuto = 3, false
	child := inheritFrom(parent)
	if child.Position != "static" {
		t.Errorf("position inherited: got %q, want static", child.Position)
	}
	if child.Top.Unit != UnitAuto {
		t.Errorf("top inherited: got %+v, want UnitAuto", child.Top)
	}
	if !child.ZIndexAuto {
		t.Errorf("z-index inherited: ZIndexAuto=%v, want true (auto)", child.ZIndexAuto)
	}
}

// TestApplyOverflow: each valid overflow keyword is accepted; an invalid one is
// dropped (the prior value is preserved).
func TestApplyOverflow(t *testing.T) {
	for _, kw := range []string{"visible", "hidden", "scroll", "auto"} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "overflow", Value: kw})
		if cs.Overflow != kw {
			t.Errorf("overflow %q not applied, got %q", kw, cs.Overflow)
		}
	}
	cs := initialStyle()
	cs.Overflow = "hidden"
	applyDeclaration(&cs, Declaration{Property: "overflow", Value: "clip"}) // unsupported
	if cs.Overflow != "hidden" {
		t.Errorf("overflow after invalid keyword = %q, want hidden preserved", cs.Overflow)
	}
}

// TestOverflowInitialVisible: the initial value is "visible".
func TestOverflowInitialVisible(t *testing.T) {
	if cs := initialStyle(); cs.Overflow != "visible" {
		t.Errorf("initial overflow = %q, want visible", cs.Overflow)
	}
}

// TestOverflowNotInherited: overflow is not inherited (a child of an overflow:hidden
// parent computes "visible").
func TestOverflowNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Overflow = "hidden"
	child := inheritFrom(parent)
	if child.Overflow != "visible" {
		t.Errorf("child overflow = %q, want visible (not inherited)", child.Overflow)
	}
}

func TestTableProperties(t *testing.T) {
	sheet := Parse(`
		table { border-collapse: collapse; border-spacing: 4px 8px; table-layout: fixed;
		        caption-side: bottom; direction: rtl; }
		td { vertical-align: middle; }
	`)
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: sheet}}, nil)

	tbl := r.ComputeRoot(&fakeNode{tag: "table"})
	if tbl.BorderCollapse != "collapse" {
		t.Errorf("border-collapse = %q, want collapse", tbl.BorderCollapse)
	}
	if tbl.BorderSpacingH != 4 || tbl.BorderSpacingV != 8 {
		t.Errorf("border-spacing = %v,%v want 4,8", tbl.BorderSpacingH, tbl.BorderSpacingV)
	}
	if tbl.TableLayout != "fixed" {
		t.Errorf("table-layout = %q, want fixed", tbl.TableLayout)
	}
	if tbl.CaptionSide != "bottom" {
		t.Errorf("caption-side = %q, want bottom", tbl.CaptionSide)
	}
	if tbl.Direction != "rtl" {
		t.Errorf("direction = %q, want rtl", tbl.Direction)
	}
	td := r.ComputeRoot(&fakeNode{tag: "td"})
	if td.VerticalAlign != "middle" {
		t.Errorf("vertical-align = %q, want middle", td.VerticalAlign)
	}
}

func TestTablePropertyDefaults(t *testing.T) {
	cs := initialStyle()
	if cs.BorderCollapse != "separate" {
		t.Errorf("default border-collapse = %q, want separate", cs.BorderCollapse)
	}
	if cs.TableLayout != "auto" {
		t.Errorf("default table-layout = %q, want auto", cs.TableLayout)
	}
	if cs.VerticalAlign != "baseline" {
		t.Errorf("default vertical-align = %q, want baseline", cs.VerticalAlign)
	}
	if cs.CaptionSide != "top" {
		t.Errorf("default caption-side = %q, want top", cs.CaptionSide)
	}
	if cs.Direction != "ltr" {
		t.Errorf("default direction = %q, want ltr", cs.Direction)
	}
}

func TestBorderSpacingSingleValue(t *testing.T) {
	sheet := Parse(`table { border-spacing: 6px; }`)
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: sheet}}, nil)
	tbl := r.ComputeRoot(&fakeNode{tag: "table"})
	if tbl.BorderSpacingH != 6 || tbl.BorderSpacingV != 6 {
		t.Errorf("single border-spacing = %v,%v want 6,6", tbl.BorderSpacingH, tbl.BorderSpacingV)
	}
}

func TestTableLayoutAndVerticalAlignNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.TableLayout = "fixed"
	parent.VerticalAlign = "middle"
	child := inheritFrom(parent)
	if child.TableLayout != "auto" {
		t.Errorf("child table-layout = %q, want auto (not inherited)", child.TableLayout)
	}
	if child.VerticalAlign != "baseline" {
		t.Errorf("child vertical-align = %q, want baseline (not inherited)", child.VerticalAlign)
	}
}

func TestTablePropertiesInheritToChild(t *testing.T) {
	// border-collapse, border-spacing, caption-side, and direction are inherited:
	// a non-initial value on a parent must propagate to a child element.
	src := `table { border-collapse: collapse; border-spacing: 5px; caption-side: bottom; direction: rtl; }`
	sheet := Parse(src)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

	tbl := &fakeNode{tag: "table"}
	child := &fakeNode{tag: "td", parent: tbl}

	tblStyle := r.ComputeRoot(tbl)
	childStyle := r.Compute(child, tblStyle)

	if childStyle.BorderCollapse != "collapse" {
		t.Errorf("child border-collapse = %q, want inherited collapse", childStyle.BorderCollapse)
	}
	if childStyle.BorderSpacingH != 5 || childStyle.BorderSpacingV != 5 {
		t.Errorf("child border-spacing = %v,%v, want inherited 5,5", childStyle.BorderSpacingH, childStyle.BorderSpacingV)
	}
	if childStyle.CaptionSide != "bottom" {
		t.Errorf("child caption-side = %q, want inherited bottom", childStyle.CaptionSide)
	}
	if childStyle.Direction != "rtl" {
		t.Errorf("child direction = %q, want inherited rtl", childStyle.Direction)
	}
}

func TestFlexContainerProperties(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-direction", "column")
	applyOne(&cs, "flex-wrap", "wrap")
	applyOne(&cs, "justify-content", "space-between")
	applyOne(&cs, "align-items", "center")
	applyOne(&cs, "column-gap", "12px")
	applyOne(&cs, "row-gap", "8px")
	if cs.FlexDirection != "column" {
		t.Errorf("flex-direction = %q, want column", cs.FlexDirection)
	}
	if cs.FlexWrap != "wrap" {
		t.Errorf("flex-wrap = %q, want wrap", cs.FlexWrap)
	}
	if cs.JustifyContent != "space-between" {
		t.Errorf("justify-content = %q, want space-between", cs.JustifyContent)
	}
	if cs.AlignItems != "center" {
		t.Errorf("align-items = %q, want center", cs.AlignItems)
	}
	if cs.ColumnGap != (Length{12, UnitPx}) {
		t.Errorf("column-gap = %v, want 12px", cs.ColumnGap)
	}
	if cs.RowGap != (Length{8, UnitPx}) {
		t.Errorf("row-gap = %v, want 8px", cs.RowGap)
	}
}

func TestFlexItemProperties(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-grow", "2")
	applyOne(&cs, "flex-shrink", "0")
	applyOne(&cs, "flex-basis", "100px")
	applyOne(&cs, "align-self", "flex-end")
	applyOne(&cs, "order", "-1")
	if cs.FlexGrow != 2 {
		t.Errorf("flex-grow = %v, want 2", cs.FlexGrow)
	}
	if cs.FlexShrink != 0 {
		t.Errorf("flex-shrink = %v, want 0", cs.FlexShrink)
	}
	if cs.FlexBasis != (Length{100, UnitPx}) {
		t.Errorf("flex-basis = %v, want 100px", cs.FlexBasis)
	}
	if cs.AlignSelf != "flex-end" {
		t.Errorf("align-self = %q, want flex-end", cs.AlignSelf)
	}
	if cs.Order != -1 {
		t.Errorf("order = %v, want -1", cs.Order)
	}
}

func TestFlexBasisContentAndAuto(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "flex-basis", "content")
	if cs.FlexBasis.Unit != UnitContent {
		t.Errorf("flex-basis:content unit = %v, want UnitContent", cs.FlexBasis.Unit)
	}
	cs2 := initialStyle()
	if cs2.FlexBasis.Unit != UnitAuto {
		t.Errorf("default flex-basis unit = %v, want UnitAuto", cs2.FlexBasis.Unit)
	}
	if cs2.FlexShrink != 1 {
		t.Errorf("default flex-shrink = %v, want 1", cs2.FlexShrink)
	}
}

func TestFlexUnknownValueIgnored(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "justify-content", "bogus")
	if cs.JustifyContent != "flex-start" {
		t.Errorf("justify-content after bogus = %q, want flex-start (unchanged)", cs.JustifyContent)
	}
}
