package css

import "testing"

func TestParseStylesheet(t *testing.T) {
	src := `
		/* comment */
		h1, .title { color: red; font-size: 24px; }
		p { margin-top: 10px }
		@media print { p { color: black } }   /* whole at-rule skipped */
	`
	sheet := Parse(src)
	if len(sheet.Rules) != 2 {
		t.Fatalf("got %d rules, want 2 (the @media block is skipped): %+v", len(sheet.Rules), sheet.Rules)
	}
	// First rule has 2 selectors and 2 declarations.
	if len(sheet.Rules[0].Selectors) != 2 {
		t.Fatalf("rule[0] selectors = %d, want 2", len(sheet.Rules[0].Selectors))
	}
	if len(sheet.Rules[0].Declarations) != 2 {
		t.Fatalf("rule[0] declarations = %d, want 2", len(sheet.Rules[0].Declarations))
	}
}

func TestParseDeclarations(t *testing.T) {
	decls := parseDeclarations("color: red; margin-top: 10px; ; bogus")
	// The empty declaration and the value-less "bogus" are dropped.
	if len(decls) != 2 {
		t.Fatalf("got %d declarations, want 2: %+v", len(decls), decls)
	}
	if decls[0].Property != "color" || decls[0].Value != "red" {
		t.Fatalf("decl[0] = %+v, want {color red}", decls[0])
	}
	if decls[1].Property != "margin-top" || decls[1].Value != "10px" {
		t.Fatalf("decl[1] = %+v, want {margin-top 10px}", decls[1])
	}
}

func TestParseDeclarationImportant(t *testing.T) {
	decls := parseDeclarations("color: red !important")
	if len(decls) != 1 || !decls[0].Important || decls[0].Value != "red" {
		t.Fatalf("decl = %+v, want {color red important=true}", decls)
	}
}

func TestParseDeclarationImportantEdgeCases(t *testing.T) {
	// Uppercase property is lowercased; uppercase !IMPORTANT is recognized.
	d := parseDeclarations("MARGIN-TOP: 10px !IMPORTANT")
	if len(d) != 1 || d[0].Property != "margin-top" || d[0].Value != "10px" || !d[0].Important {
		t.Fatalf("got %+v, want {margin-top 10px important}", d)
	}
	// "!important" as a substring of a value (not a trailing token) is NOT a flag.
	d = parseDeclarations("background: url(x!important.png)")
	if len(d) != 1 || d[0].Important || d[0].Value != "url(x!important.png)" {
		t.Fatalf("got %+v, want {background url(x!important.png) not-important}", d)
	}
	// A declaration that is only "!important" with no value is skipped.
	d = parseDeclarations("color: !important")
	if len(d) != 0 {
		t.Fatalf("got %+v, want no declarations (empty value)", d)
	}
	// One malformed declaration in the MIDDLE doesn't drop its neighbors.
	d = parseDeclarations("color: red; bogus-no-colon; margin: 0")
	if len(d) != 2 || d[0].Property != "color" || d[1].Property != "margin" {
		t.Fatalf("got %+v, want [color, margin]", d)
	}
	// Value case is preserved (not lowercased) even with !important.
	d = parseDeclarations("color: Red !important")
	if len(d) != 1 || d[0].Value != "Red" || !d[0].Important {
		t.Fatalf("got %+v, want {color Red important}", d)
	}
}
