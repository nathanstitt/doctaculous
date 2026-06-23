package css

import "testing"

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
