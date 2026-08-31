package css

import "testing"

func TestParseStringSet(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "string-set", Value: `title content()`})
	if len(cs.StringSet) != 1 || cs.StringSet[0].Name != "title" {
		t.Fatalf("StringSet = %+v, want one entry named title", cs.StringSet)
	}
	if !cs.StringSet[0].UseContent {
		t.Errorf("entry should use content() (the element's text)")
	}
}

func TestParseStringSetLiteral(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "string-set", Value: `chapter "Ch. " content()`})
	if len(cs.StringSet) != 1 {
		t.Fatalf("want 1 entry, got %d", len(cs.StringSet))
	}
	e := cs.StringSet[0]
	if e.Name != "chapter" || e.Prefix != "Ch. " || !e.UseContent {
		t.Errorf("entry = %+v, want {chapter, prefix \"Ch. \", UseContent}", e)
	}
}
