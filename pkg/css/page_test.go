package css

import (
	"math"
	"testing"
)

func approxEq(a, b float64) bool { return math.Abs(a-b) < 0.5 }

func TestParseStylesheetCapturesPage(t *testing.T) {
	src := `
		p { color: red }
		@page { size: A4 landscape; margin: 2cm }
		@page :first { margin-top: 0 }
		@page narrow { size: letter; margin: 1in }
		@media screen { a { color: blue } }
	`
	ss := Parse(src)
	if len(ss.Pages) != 3 {
		t.Fatalf("got %d @page rules, want 3: %+v", len(ss.Pages), ss.Pages)
	}
	// The non-@page at-rule (@media) is still skipped, and the normal rule survives.
	if len(ss.Rules) != 1 || ss.Rules[0].Declarations[0].Value != "red" {
		t.Errorf("normal rules = %+v, want one p{color:red}", ss.Rules)
	}

	// Base rule: A4 landscape => 1123 x 794 (px@96), margin 2cm all sides.
	base := ss.Pages[0]
	if base.Name != "" || base.Pseudo != PageNone {
		t.Errorf("base selector = %q/%v, want unnamed/PageNone", base.Name, base.Pseudo)
	}
	up := Stylesheet{Pages: []PageRule{base}}.ResolvePage(0, "", false)
	if !approxEq(up.WidthPt, 297*pxPerMm) || !approxEq(up.HeightPt, 210*pxPerMm) {
		t.Errorf("A4 landscape size = %.1f x %.1f, want %.1f x %.1f (axes swapped)",
			up.WidthPt, up.HeightPt, 297*pxPerMm, 210*pxPerMm)
	}
	if !approxEq(up.MarginTop, 2*pxPerCm) || !approxEq(up.MarginLeft, 2*pxPerCm) {
		t.Errorf("margins = T%.1f L%.1f, want %.1f all", up.MarginTop, up.MarginLeft, 2*pxPerCm)
	}

	// :first selector.
	if ss.Pages[1].Pseudo != PageFirst {
		t.Errorf("page[1] pseudo = %v, want PageFirst", ss.Pages[1].Pseudo)
	}
	// Named page.
	if ss.Pages[2].Name != "narrow" {
		t.Errorf("page[2] name = %q, want narrow", ss.Pages[2].Name)
	}
}

func TestParsePageCapturesMarginBoxes(t *testing.T) {
	src := `@page {
		margin: 1in;
		@top-center { content: "Title"; text-align: center }
		@bottom-right { content: counter(page) }
	}`
	ss := Parse(src)
	if len(ss.Pages) != 1 {
		t.Fatalf("got %d @page rules, want 1", len(ss.Pages))
	}
	pr := ss.Pages[0]
	if len(pr.Margin) != 2 {
		t.Fatalf("got %d margin boxes, want 2: %+v", len(pr.Margin), pr.Margin)
	}
	// Both the page-box `margin` decl and the nested boxes are captured.
	if v, ok := lastDecl(pr.Decls, "margin"); !ok || v != "1in" {
		t.Errorf("page margin decl = %q/%v, want 1in", v, ok)
	}
	var top, bot *MarginBoxRule
	for i := range pr.Margin {
		switch pr.Margin[i].Box {
		case MarginTopCenter:
			top = &pr.Margin[i]
		case MarginBottomRight:
			bot = &pr.Margin[i]
		}
	}
	if top == nil || bot == nil {
		t.Fatalf("missing a slot: top=%v bot=%v", top, bot)
	}
	if c, _ := lastDecl(top.Decls, "content"); c != `"Title"` {
		t.Errorf("@top-center content = %q, want \"Title\"", c)
	}
	if c, _ := lastDecl(bot.Decls, "content"); c != "counter(page)" {
		t.Errorf("@bottom-right content = %q, want counter(page)", c)
	}
}

func TestResolvePageCascade(t *testing.T) {
	// Base sets margin 1in; :first overrides margin-top to 0. Page 0 gets the override,
	// page 1 the base.
	ss := Parse(`
		@page { margin: 1in }
		@page :first { margin-top: 0 }
	`)
	p0 := ss.ResolvePage(0, "", false)
	if !approxEq(p0.MarginTop, 0) {
		t.Errorf("page 0 margin-top = %.1f, want 0 (:first override)", p0.MarginTop)
	}
	if !approxEq(p0.MarginLeft, pxPerIn) {
		t.Errorf("page 0 margin-left = %.1f, want %.1f (base)", p0.MarginLeft, pxPerIn)
	}
	p1 := ss.ResolvePage(1, "", false)
	if !approxEq(p1.MarginTop, pxPerIn) {
		t.Errorf("page 1 margin-top = %.1f, want %.1f (base, no :first)", p1.MarginTop, pxPerIn)
	}
}

func TestResolvePageNamed(t *testing.T) {
	ss := Parse(`
		@page { size: letter }
		@page wide { size: A3 landscape }
	`)
	// Unnamed page gets letter.
	up := ss.ResolvePage(0, "", false)
	if !approxEq(up.WidthPt, 8.5*pxPerIn) {
		t.Errorf("unnamed page width = %.1f, want letter %.1f", up.WidthPt, 8.5*pxPerIn)
	}
	// A box with page:wide selects the named rule (A3 landscape => 420mm wide).
	w := ss.ResolvePage(0, "wide", false)
	if !approxEq(w.WidthPt, 420*pxPerMm) {
		t.Errorf("named 'wide' width = %.1f, want A3-landscape %.1f", w.WidthPt, 420*pxPerMm)
	}
}

func TestResolvePageNoRule(t *testing.T) {
	ss := Parse(`p { color: red }`)
	up := ss.ResolvePage(0, "", false)
	if up.HasRule {
		t.Errorf("ResolvePage with no @page should have HasRule=false: %+v", up)
	}
}

func TestParsePageSize(t *testing.T) {
	cases := []struct {
		in   string
		w, h float64
		ok   bool
	}{
		{"A4", 210 * pxPerMm, 297 * pxPerMm, true},
		{"a4 portrait", 210 * pxPerMm, 297 * pxPerMm, true},
		{"A4 landscape", 297 * pxPerMm, 210 * pxPerMm, true},
		{"letter", 8.5 * pxPerIn, 11 * pxPerIn, true},
		{"legal landscape", 14 * pxPerIn, 8.5 * pxPerIn, true},
		{"8.5in 11in", 8.5 * pxPerIn, 11 * pxPerIn, true},
		{"200px", 200, 200, true}, // single length => square
		{"auto", 0, 0, false},
		{"portrait", 0, 0, false}, // orientation only => no size basis
		{"garbage", 0, 0, false},
	}
	for _, c := range cases {
		w, h, ok := parsePageSize(c.in)
		if ok != c.ok || (ok && (!approxEq(w, c.w) || !approxEq(h, c.h))) {
			t.Errorf("parsePageSize(%q) = %.1f,%.1f,%v; want %.1f,%.1f,%v",
				c.in, w, h, ok, c.w, c.h, c.ok)
		}
	}
}

func TestParseAbsLengthPx(t *testing.T) {
	cases := []struct {
		in string
		v  float64
		ok bool
	}{
		{"1in", 96, true},
		{"2.54cm", 96, true},
		{"25.4mm", 96, true},
		{"72pt", 96, true},
		{"6pc", 96, true},
		{"96px", 96, true},
		{"0", 0, true},
		{"10", 0, false},  // unitless non-zero invalid
		{"2em", 0, false}, // relative not absolute
		{"50%", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		v, ok := parseAbsLengthPx(c.in)
		if ok != c.ok || (ok && !approxEq(v, c.v)) {
			t.Errorf("parseAbsLengthPx(%q) = %.2f,%v; want %.2f,%v", c.in, v, ok, c.v, c.ok)
		}
	}
}

func TestAtKeyword(t *testing.T) {
	cases := []struct {
		prelude string
		want    string
		ok      bool
	}{
		{"@page", "", true},
		{"@page :first", ":first", true},
		{"@PAGE narrow", "narrow", true},
		{"@page-break-inside", "", false}, // longer keyword: not a match
		{"@font-face", "", false},
		{"@pages", "", false},
	}
	for _, c := range cases {
		rest, ok := atKeyword(c.prelude, "@page")
		if ok != c.ok || rest != c.want {
			t.Errorf("atKeyword(%q) = %q,%v; want %q,%v", c.prelude, rest, ok, c.want, c.ok)
		}
	}
}

func TestPageBreakWidowsOrphansCascade(t *testing.T) {
	// break-inside, page, widows, orphans resolve onto ComputedStyle; widows/orphans
	// default to 2 (CSS initial) and are inherited.
	cs := initialStyle()
	if cs.Widows != 2 || cs.Orphans != 2 {
		t.Errorf("initial widows/orphans = %d/%d, want 2/2", cs.Widows, cs.Orphans)
	}
	apply := func(prop, val string) ComputedStyle {
		c := initialStyle()
		applyDeclaration(&c, Declaration{Property: prop, Value: val})
		return c
	}
	if c := apply("break-inside", "avoid"); c.BreakInside != "avoid" {
		t.Errorf("break-inside = %q, want avoid", c.BreakInside)
	}
	if c := apply("page", "Landscape"); c.Page != "landscape" {
		t.Errorf("page = %q, want lowercased landscape", c.Page)
	}
	if c := apply("widows", "4"); c.Widows != 4 {
		t.Errorf("widows = %d, want 4", c.Widows)
	}
	if c := apply("orphans", "3"); c.Orphans != 3 {
		t.Errorf("orphans = %d, want 3", c.Orphans)
	}
	// A non-integer widows keeps the initial 2 (graceful).
	if c := apply("widows", "junk"); c.Widows != 2 {
		t.Errorf("widows=junk = %d, want 2 (kept initial)", c.Widows)
	}
}
