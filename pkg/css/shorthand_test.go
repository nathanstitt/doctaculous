package css

import (
	"image/color"
	"testing"
)

// applyOne is a small helper mirroring the style of TestApplyDeclaration: it runs
// one declaration through applyDeclaration against the given style.
func applyOne(cs *ComputedStyle, prop, val string) {
	applyDeclaration(cs, Declaration{Property: prop, Value: val})
}

func TestMarginShorthandOneValue(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "margin", "10px")
	want := Length{10, UnitPx}
	if cs.MarginTop != want || cs.MarginRight != want || cs.MarginBottom != want || cs.MarginLeft != want {
		t.Errorf("margin:10px = T%v R%v B%v L%v, want all %v",
			cs.MarginTop, cs.MarginRight, cs.MarginBottom, cs.MarginLeft, want)
	}
}

func TestMarginShorthandTwoValues(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "margin", "10px 20px")
	// top/bottom = 10, right/left = 20.
	if cs.MarginTop != (Length{10, UnitPx}) || cs.MarginBottom != (Length{10, UnitPx}) {
		t.Errorf("margin:10px 20px top/bottom = %v/%v, want 10px/10px", cs.MarginTop, cs.MarginBottom)
	}
	if cs.MarginRight != (Length{20, UnitPx}) || cs.MarginLeft != (Length{20, UnitPx}) {
		t.Errorf("margin:10px 20px right/left = %v/%v, want 20px/20px", cs.MarginRight, cs.MarginLeft)
	}
}

func TestMarginShorthandFourValuesClockwise(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "margin", "1px 2px 3px 4px")
	// Clockwise: Top=1, Right=2, Bottom=3, Left=4.
	if cs.MarginTop != (Length{1, UnitPx}) {
		t.Errorf("margin top = %v, want 1px", cs.MarginTop)
	}
	if cs.MarginRight != (Length{2, UnitPx}) {
		t.Errorf("margin right = %v, want 2px", cs.MarginRight)
	}
	if cs.MarginBottom != (Length{3, UnitPx}) {
		t.Errorf("margin bottom = %v, want 3px", cs.MarginBottom)
	}
	if cs.MarginLeft != (Length{4, UnitPx}) {
		t.Errorf("margin left = %v, want 4px", cs.MarginLeft)
	}
}

func TestMarginShorthandThreeValuesAndAuto(t *testing.T) {
	cs := initialStyle()
	// 3 values: top=auto, right/left=10px, bottom=20px.
	applyOne(&cs, "margin", "auto 10px 20px")
	if cs.MarginTop.Unit != UnitAuto {
		t.Errorf("margin top = %v, want auto", cs.MarginTop)
	}
	if cs.MarginRight != (Length{10, UnitPx}) || cs.MarginLeft != (Length{10, UnitPx}) {
		t.Errorf("margin right/left = %v/%v, want 10px/10px", cs.MarginRight, cs.MarginLeft)
	}
	if cs.MarginBottom != (Length{20, UnitPx}) {
		t.Errorf("margin bottom = %v, want 20px", cs.MarginBottom)
	}
}

func TestPaddingShorthandOneValueEm(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "padding", "1em")
	want := Length{1, UnitEm}
	if cs.PaddingTop != want || cs.PaddingRight != want || cs.PaddingBottom != want || cs.PaddingLeft != want {
		t.Errorf("padding:1em = T%v R%v B%v L%v, want all %v",
			cs.PaddingTop, cs.PaddingRight, cs.PaddingBottom, cs.PaddingLeft, want)
	}
}

func TestPaddingShorthandRejectsAuto(t *testing.T) {
	// "auto" is invalid for padding; per whole-declaration-drop the prior values
	// (all 0px) are preserved.
	cs := initialStyle()
	applyOne(&cs, "padding", "5px")
	applyOne(&cs, "padding", "auto")
	want := Length{5, UnitPx}
	if cs.PaddingTop != want || cs.PaddingLeft != want {
		t.Errorf("padding after invalid auto = T%v L%v, want 5px preserved", cs.PaddingTop, cs.PaddingLeft)
	}
}

func TestBorderWidthShorthand(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "border-width", "1px 2px")
	// 2 values: top/bottom = 1px, right/left = 2px.
	if cs.BorderTopWidth != (Length{1, UnitPx}) || cs.BorderBottomWidth != (Length{1, UnitPx}) {
		t.Errorf("border-width top/bottom = %v/%v, want 1px/1px", cs.BorderTopWidth, cs.BorderBottomWidth)
	}
	if cs.BorderRightWidth != (Length{2, UnitPx}) || cs.BorderLeftWidth != (Length{2, UnitPx}) {
		t.Errorf("border-width right/left = %v/%v, want 2px/2px", cs.BorderRightWidth, cs.BorderLeftWidth)
	}
}

func TestBorderStyleShorthand(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "border-style", "solid dashed")
	// 2 values: top/bottom = solid, right/left = dashed.
	if cs.BorderTopStyle != "solid" || cs.BorderBottomStyle != "solid" {
		t.Errorf("border-style top/bottom = %q/%q, want solid/solid", cs.BorderTopStyle, cs.BorderBottomStyle)
	}
	if cs.BorderRightStyle != "dashed" || cs.BorderLeftStyle != "dashed" {
		t.Errorf("border-style right/left = %q/%q, want dashed/dashed", cs.BorderRightStyle, cs.BorderLeftStyle)
	}
}

func TestBorderColorShorthand(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "border-color", "red blue")
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	// 2 values: top/bottom = red, right/left = blue.
	if cs.BorderTopColor != red || cs.BorderBottomColor != red {
		t.Errorf("border-color top/bottom = %v/%v, want red/red", cs.BorderTopColor, cs.BorderBottomColor)
	}
	if cs.BorderRightColor != blue || cs.BorderLeftColor != blue {
		t.Errorf("border-color right/left = %v/%v, want blue/blue", cs.BorderRightColor, cs.BorderLeftColor)
	}
}

func TestBorderShorthandAllSides(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	check := func(name string, cs ComputedStyle) {
		t.Helper()
		w := Length{1, UnitPx}
		widths := [4]Length{cs.BorderTopWidth, cs.BorderRightWidth, cs.BorderBottomWidth, cs.BorderLeftWidth}
		styles := [4]string{cs.BorderTopStyle, cs.BorderRightStyle, cs.BorderBottomStyle, cs.BorderLeftStyle}
		colors := [4]color.RGBA{cs.BorderTopColor, cs.BorderRightColor, cs.BorderBottomColor, cs.BorderLeftColor}
		for i := 0; i < 4; i++ {
			if widths[i] != w {
				t.Errorf("%s: side %d width = %v, want 1px", name, i, widths[i])
			}
			if styles[i] != "solid" {
				t.Errorf("%s: side %d style = %q, want solid", name, i, styles[i])
			}
			if colors[i] != black {
				t.Errorf("%s: side %d color = %v, want black", name, i, colors[i])
			}
		}
	}

	// Order-independence: the three components may appear in any order.
	for _, val := range []string{"1px solid black", "solid 1px black", "black solid 1px"} {
		cs := initialStyle()
		applyOne(&cs, "border", val)
		check(val, cs)
	}
}

func TestBorderShorthandResetsOmitted(t *testing.T) {
	// `border: solid` sets style=solid and RESETS width to medium (3px) and color
	// to currentColor (cs.Color), per CSS shorthand reset semantics.
	cs := initialStyle()
	cs.Color = color.RGBA{0, 0, 255, 255} // currentColor = blue
	applyOne(&cs, "border", "solid")
	if cs.BorderTopStyle != "solid" {
		t.Errorf("border:solid style = %q, want solid", cs.BorderTopStyle)
	}
	if cs.BorderTopWidth != (Length{3, UnitPx}) {
		t.Errorf("border:solid width = %v, want medium 3px", cs.BorderTopWidth)
	}
	if cs.BorderTopColor != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("border:solid color = %v, want currentColor blue", cs.BorderTopColor)
	}
}

func TestBorderSideShorthandOnlySetsOneSide(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "border-top", "2px dashed red")
	// Top side is set.
	if cs.BorderTopWidth != (Length{2, UnitPx}) {
		t.Errorf("border-top width = %v, want 2px", cs.BorderTopWidth)
	}
	if cs.BorderTopStyle != "dashed" {
		t.Errorf("border-top style = %q, want dashed", cs.BorderTopStyle)
	}
	if cs.BorderTopColor != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("border-top color = %v, want red", cs.BorderTopColor)
	}
	// Other sides remain at initial (zero width, empty style, zero color).
	if cs.BorderRightWidth != (Length{0, UnitPx}) || cs.BorderBottomWidth != (Length{0, UnitPx}) || cs.BorderLeftWidth != (Length{0, UnitPx}) {
		t.Errorf("non-top widths changed: R%v B%v L%v", cs.BorderRightWidth, cs.BorderBottomWidth, cs.BorderLeftWidth)
	}
	if cs.BorderRightStyle != "" || cs.BorderBottomStyle != "" || cs.BorderLeftStyle != "" {
		t.Errorf("non-top styles changed: R%q B%q L%q", cs.BorderRightStyle, cs.BorderBottomStyle, cs.BorderLeftStyle)
	}
	if cs.BorderRightColor != (color.RGBA{}) || cs.BorderBottomColor != (color.RGBA{}) || cs.BorderLeftColor != (color.RGBA{}) {
		t.Errorf("non-top colors changed: R%v B%v L%v", cs.BorderRightColor, cs.BorderBottomColor, cs.BorderLeftColor)
	}
}

func TestBackgroundShorthandColor(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "background", "#eee")
	// #eee expands to #eeeeee.
	want := color.RGBA{0xee, 0xee, 0xee, 255}
	if cs.BackgroundColor != want {
		t.Errorf("background:#eee = %v, want %v", cs.BackgroundColor, want)
	}
}

func TestBackgroundShorthandFindsColorAmongComponents(t *testing.T) {
	// A url()/keyword soup with a color component: the color is picked up, the rest
	// ignored (minimal color-only support).
	cs := initialStyle()
	applyOne(&cs, "background", "url(x.png) no-repeat red")
	if cs.BackgroundColor != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("background with url+color = %v, want red", cs.BackgroundColor)
	}
}

func TestBackgroundShorthandNoColorPreserved(t *testing.T) {
	// No color component: BackgroundColor is left at its prior value.
	cs := initialStyle()
	applyOne(&cs, "background-color", "green")
	applyOne(&cs, "background", "url(x.png) no-repeat")
	if cs.BackgroundColor != (color.RGBA{0, 128, 0, 255}) {
		t.Errorf("background with no color = %v, want green preserved", cs.BackgroundColor)
	}
}

func TestMarginShorthandThenLonghandSourceOrder(t *testing.T) {
	// `margin: 0; margin-top: 5px;` => top=5, others 0. Proves the shorthand
	// expands into longhand fields so later longhands override per source order.
	cs := initialStyle()
	applyOne(&cs, "margin", "0")
	applyOne(&cs, "margin-top", "5px")
	if cs.MarginTop != (Length{5, UnitPx}) {
		t.Errorf("margin-top after margin:0; margin-top:5px = %v, want 5px", cs.MarginTop)
	}
	if cs.MarginRight != (Length{0, UnitPx}) || cs.MarginBottom != (Length{0, UnitPx}) || cs.MarginLeft != (Length{0, UnitPx}) {
		t.Errorf("other margins = R%v B%v L%v, want 0px", cs.MarginRight, cs.MarginBottom, cs.MarginLeft)
	}
}

func TestLonghandThenMarginShorthandResetsAll(t *testing.T) {
	// `margin-top: 5px; margin: 0;` => all 0; the later shorthand resets top too.
	cs := initialStyle()
	applyOne(&cs, "margin-top", "5px")
	applyOne(&cs, "margin", "0")
	zero := Length{0, UnitPx}
	if cs.MarginTop != zero || cs.MarginRight != zero || cs.MarginBottom != zero || cs.MarginLeft != zero {
		t.Errorf("margins after margin-top:5px; margin:0 = T%v R%v B%v L%v, want all 0px",
			cs.MarginTop, cs.MarginRight, cs.MarginBottom, cs.MarginLeft)
	}
}

func TestMarginShorthandInvalidComponentDropsWholeDeclaration(t *testing.T) {
	// Documented choice: an invalid component voids the WHOLE margin declaration
	// (CSS whole-declaration-drop). Prior values (10px on all sides) are preserved.
	cs := initialStyle()
	applyOne(&cs, "margin", "10px")
	applyOne(&cs, "margin", "10px bogus 5px")
	want := Length{10, UnitPx}
	if cs.MarginTop != want || cs.MarginRight != want || cs.MarginBottom != want || cs.MarginLeft != want {
		t.Errorf("margin after invalid component = T%v R%v B%v L%v, want all 10px preserved (whole-declaration-drop)",
			cs.MarginTop, cs.MarginRight, cs.MarginBottom, cs.MarginLeft)
	}
}

func TestBorderShorthandSkipsUnrecognizedComponent(t *testing.T) {
	// Documented choice for the border TRIPLE: an unrecognized component is skipped
	// while the recognized width+style still apply. `border: 1px notacolor solid`
	// => width=1px, style=solid; color resets to currentColor (the omitted-color
	// initial), the bad token ignored.
	cs := initialStyle()
	applyOne(&cs, "border", "1px notacolor solid")
	if cs.BorderTopWidth != (Length{1, UnitPx}) {
		t.Errorf("border width = %v, want 1px", cs.BorderTopWidth)
	}
	if cs.BorderTopStyle != "solid" {
		t.Errorf("border style = %q, want solid", cs.BorderTopStyle)
	}
	// "notacolor" is neither a length nor a style nor a parseable color, so it is
	// skipped; color falls back to currentColor (black initial here).
	if cs.BorderTopColor != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("border color = %v, want currentColor black (bad token skipped)", cs.BorderTopColor)
	}
}

func TestShorthandsNotInherited(t *testing.T) {
	// A parent sets box styling via shorthands; a child with no declarations must
	// reset these non-inherited longhands to initial (NOT inherit the parent's).
	src := `div { margin: 10px; padding: 2em; border: 1px solid red; background: #eee; }`
	sheet := Parse(src)
	r := NewResolver([]OriginSheet{{Sheet: sheet, Origin: OriginAuthor}}, nil)

	div := &fakeNode{tag: "div"}
	child := &fakeNode{tag: "span", parent: div}

	divStyle := r.ComputeRoot(div)
	childStyle := r.Compute(child, divStyle)

	// Sanity: the parent actually picked up the shorthands.
	if divStyle.MarginTop != (Length{10, UnitPx}) || divStyle.BorderTopStyle != "solid" {
		t.Fatalf("parent shorthands not applied: margin-top=%v border-top-style=%q", divStyle.MarginTop, divStyle.BorderTopStyle)
	}
	if divStyle.PaddingLeft != (Length{2, UnitEm}) {
		t.Fatalf("parent padding not applied: padding-left=%v", divStyle.PaddingLeft)
	}
	if divStyle.BackgroundColor != (color.RGBA{0xee, 0xee, 0xee, 255}) {
		t.Fatalf("parent background not applied: %v", divStyle.BackgroundColor)
	}

	// Child resets to initial, not inherited.
	if childStyle.MarginTop != (Length{0, UnitPx}) {
		t.Errorf("child margin-top = %v, want 0px (not inherited)", childStyle.MarginTop)
	}
	if childStyle.PaddingLeft != (Length{0, UnitPx}) {
		t.Errorf("child padding-left = %v, want 0px (not inherited)", childStyle.PaddingLeft)
	}
	if childStyle.BorderTopStyle != "" {
		t.Errorf("child border-top-style = %q, want empty (not inherited)", childStyle.BorderTopStyle)
	}
	if childStyle.BackgroundColor != (color.RGBA{}) {
		t.Errorf("child background-color = %v, want transparent (not inherited)", childStyle.BackgroundColor)
	}
}

func TestSplitComponentsHandlesParenGroups(t *testing.T) {
	// The component splitter must keep rgb(...) (with its internal commas/spaces)
	// as a single component.
	got := splitComponents("1px solid rgb(0, 128, 255)")
	want := []string{"1px", "solid", "rgb(0, 128, 255)"}
	if len(got) != len(want) {
		t.Fatalf("splitComponents = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d = %q, want %q", i, got[i], want[i])
		}
	}
	// And the rgb() component must still parse back to a color.
	if c, ok := parseColor(newTokenizer(got[2])); !ok || c != (color.RGBA{0, 128, 255, 255}) {
		t.Errorf("re-parsed rgb component = %v ok=%v, want {0,128,255,255} true", c, ok)
	}
}

func TestBorderShorthandWithRGBColor(t *testing.T) {
	// End-to-end: a border whose color is an rgb() function (internal commas) is
	// classified correctly by applyBorderSide.
	cs := initialStyle()
	applyOne(&cs, "border", "2px dashed rgb(10, 20, 30)")
	if cs.BorderTopWidth != (Length{2, UnitPx}) {
		t.Errorf("width = %v, want 2px", cs.BorderTopWidth)
	}
	if cs.BorderTopStyle != "dashed" {
		t.Errorf("style = %q, want dashed", cs.BorderTopStyle)
	}
	if cs.BorderTopColor != (color.RGBA{10, 20, 30, 255}) {
		t.Errorf("color = %v, want rgb(10,20,30)", cs.BorderTopColor)
	}
}

func TestFlexShorthandKeywords(t *testing.T) {
	cases := []struct {
		val          string
		grow, shrink float64
		basisVal     float64
		basisUnit    LengthUnit
	}{
		{"none", 0, 0, 0, UnitAuto},
		{"auto", 1, 1, 0, UnitAuto},
		{"initial", 0, 1, 0, UnitAuto},
		{"1", 1, 1, 0, UnitPercent},    // flex:<number> => <n> 1 0%
		{"2 3", 2, 3, 0, UnitPercent},  // flex:<g> <s> => g s 0%
		{"100px", 1, 1, 100, UnitPx},   // flex:<length> => 1 1 <length>
		{"2 100px", 2, 1, 100, UnitPx}, // flex:<g> <basis> => g 1 basis
		{"2 0 50px", 2, 0, 50, UnitPx}, // flex:<g> <s> <basis>
	}
	for _, c := range cases {
		cs := initialStyle()
		applyOne(&cs, "flex", c.val)
		if cs.FlexGrow != c.grow || cs.FlexShrink != c.shrink {
			t.Errorf("flex:%q grow/shrink = %v/%v, want %v/%v", c.val, cs.FlexGrow, cs.FlexShrink, c.grow, c.shrink)
		}
		if cs.FlexBasis.Unit != c.basisUnit || cs.FlexBasis.Value != c.basisVal {
			t.Errorf("flex:%q basis = %v, want {%v %v}", c.val, cs.FlexBasis, c.basisVal, c.basisUnit)
		}
	}
}

func TestGapShorthand(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "gap", "10px")
	if cs.RowGap != (Length{10, UnitPx}) || cs.ColumnGap != (Length{10, UnitPx}) {
		t.Errorf("gap:10px = row %v col %v, want both 10px", cs.RowGap, cs.ColumnGap)
	}
	cs2 := initialStyle()
	applyOne(&cs2, "gap", "10px 20px")
	if cs2.RowGap != (Length{10, UnitPx}) || cs2.ColumnGap != (Length{20, UnitPx}) {
		t.Errorf("gap:10px 20px = row %v col %v, want 10px/20px", cs2.RowGap, cs2.ColumnGap)
	}
}

func TestGapShorthandInvalidPreservesPrior(t *testing.T) {
	cs := initialStyle()
	cs.RowGap = Length{5, UnitPx}
	cs.ColumnGap = Length{5, UnitPx}
	applyOne(&cs, "gap", "bogus")
	if cs.RowGap != (Length{5, UnitPx}) || cs.ColumnGap != (Length{5, UnitPx}) {
		t.Errorf("invalid gap should preserve prior values; got row %v col %v", cs.RowGap, cs.ColumnGap)
	}
	// Too many components is also invalid and preserves prior values.
	applyOne(&cs, "gap", "1px 2px 3px")
	if cs.RowGap != (Length{5, UnitPx}) || cs.ColumnGap != (Length{5, UnitPx}) {
		t.Errorf("3-value gap should preserve prior values; got row %v col %v", cs.RowGap, cs.ColumnGap)
	}
}

// --- Grid shorthand tests ---

func TestPlaceItemsTwoValues(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "place-items", "center start")
	if cs.AlignItems != "center" {
		t.Errorf("place-items: center start => AlignItems=%q, want \"center\"", cs.AlignItems)
	}
	if cs.JustifyItems != "start" {
		t.Errorf("place-items: center start => JustifyItems=%q, want \"start\"", cs.JustifyItems)
	}
}

func TestPlaceItemsOneValue(t *testing.T) {
	// One value sets both align-items and justify-items.
	cs := initialStyle()
	applyOne(&cs, "place-items", "end")
	if cs.AlignItems != "end" {
		t.Errorf("place-items: end => AlignItems=%q, want \"end\"", cs.AlignItems)
	}
	if cs.JustifyItems != "end" {
		t.Errorf("place-items: end => JustifyItems=%q, want \"end\"", cs.JustifyItems)
	}
}

func TestPlaceContentTwoValues(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "place-content", "space-between center")
	if cs.AlignContent != "space-between" {
		t.Errorf("place-content: space-between center => AlignContent=%q, want \"space-between\"", cs.AlignContent)
	}
	if cs.JustifyContent != "center" {
		t.Errorf("place-content: space-between center => JustifyContent=%q, want \"center\"", cs.JustifyContent)
	}
}

func TestPlaceContentOneValue(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "place-content", "stretch")
	if cs.AlignContent != "stretch" {
		t.Errorf("place-content: stretch => AlignContent=%q, want \"stretch\"", cs.AlignContent)
	}
	if cs.JustifyContent != "stretch" {
		t.Errorf("place-content: stretch => JustifyContent=%q, want \"stretch\"", cs.JustifyContent)
	}
}

func TestPlaceSelfTwoValues(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "place-self", "end center")
	if cs.AlignSelf != "end" {
		t.Errorf("place-self: end center => AlignSelf=%q, want \"end\"", cs.AlignSelf)
	}
	if cs.JustifySelf != "center" {
		t.Errorf("place-self: end center => JustifySelf=%q, want \"center\"", cs.JustifySelf)
	}
}

func TestPlaceSelfOneValue(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "place-self", "start")
	if cs.AlignSelf != "start" {
		t.Errorf("place-self: start => AlignSelf=%q, want \"start\"", cs.AlignSelf)
	}
	if cs.JustifySelf != "start" {
		t.Errorf("place-self: start => JustifySelf=%q, want \"start\"", cs.JustifySelf)
	}
}

func TestGridTemplateShorthandRowsSlashCols(t *testing.T) {
	// grid-template: <rows> / <columns>
	cs := initialStyle()
	applyOne(&cs, "grid-template", "1fr / 2fr 3fr")
	rowTracks := cs.GridTemplateRows.Expand(0)
	if len(rowTracks) != 1 {
		t.Fatalf("grid-template: 1fr / 2fr 3fr => %d row tracks, want 1", len(rowTracks))
	}
	if rowTracks[0].Max.Fr != 1 {
		t.Errorf("row track[0].fr = %v, want 1", rowTracks[0].Max.Fr)
	}
	colTracks := cs.GridTemplateColumns.Expand(0)
	if len(colTracks) != 2 {
		t.Fatalf("grid-template: 1fr / 2fr 3fr => %d col tracks, want 2", len(colTracks))
	}
	if colTracks[0].Max.Fr != 2 {
		t.Errorf("col track[0].fr = %v, want 2", colTracks[0].Max.Fr)
	}
	if colTracks[1].Max.Fr != 3 {
		t.Errorf("col track[1].fr = %v, want 3", colTracks[1].Max.Fr)
	}
}

func TestGridShorthandDelegatesToTemplate(t *testing.T) {
	// grid: <grid-template> — the explicit-grid subset delegates to grid-template.
	cs := initialStyle()
	applyOne(&cs, "grid", "100px / 1fr 2fr")
	rowTracks := cs.GridTemplateRows.Expand(0)
	if len(rowTracks) != 1 {
		t.Fatalf("grid: 100px / 1fr 2fr => %d row tracks, want 1", len(rowTracks))
	}
	colTracks := cs.GridTemplateColumns.Expand(0)
	if len(colTracks) != 2 {
		t.Fatalf("grid: 100px / 1fr 2fr => %d col tracks, want 2", len(colTracks))
	}
}
