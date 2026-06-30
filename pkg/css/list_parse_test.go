package css

import (
	"reflect"
	"testing"
)

func TestListAndCounterParsing(t *testing.T) {
	cs := initialStyle()
	if cs.ListStyleType != "disc" || cs.ListStylePosition != "outside" {
		t.Errorf("initial list-style = %q/%q", cs.ListStyleType, cs.ListStylePosition)
	}
	applyDeclaration(&cs, Declaration{Property: "list-style-type", Value: "lower-roman"})
	applyDeclaration(&cs, Declaration{Property: "list-style-position", Value: "inside"})
	if cs.ListStyleType != "lower-roman" || cs.ListStylePosition != "inside" {
		t.Errorf("list-style = %q/%q", cs.ListStyleType, cs.ListStylePosition)
	}
	// shorthand
	cs2 := initialStyle()
	applyDeclaration(&cs2, Declaration{Property: "list-style", Value: "square inside"})
	if cs2.ListStyleType != "square" || cs2.ListStylePosition != "inside" {
		t.Errorf("shorthand = %q/%q", cs2.ListStyleType, cs2.ListStylePosition)
	}
	// counter ops
	cs3 := initialStyle()
	applyDeclaration(&cs3, Declaration{Property: "counter-reset", Value: "section 0 page"})
	if !reflect.DeepEqual(cs3.CounterReset, []CounterOp{{"section", 0}, {"page", 0}}) {
		t.Errorf("counter-reset = %+v", cs3.CounterReset)
	}
	applyDeclaration(&cs3, Declaration{Property: "counter-increment", Value: "section"})
	if !reflect.DeepEqual(cs3.CounterIncrement, []CounterOp{{"section", 1}}) {
		t.Errorf("counter-increment = %+v (want default +1)", cs3.CounterIncrement)
	}
	// content
	cs4 := initialStyle()
	applyDeclaration(&cs4, Declaration{Property: "content", Value: `counter(section) ". "`})
	if len(cs4.Content) != 2 || cs4.Content[0].Kind != ContentCounter || cs4.Content[0].Name != "section" || cs4.Content[1].Kind != ContentString || cs4.Content[1].Text != ". " {
		t.Errorf("content = %+v", cs4.Content)
	}
	cs5 := initialStyle()
	applyDeclaration(&cs5, Declaration{Property: "content", Value: `counters(item, ".")`})
	if len(cs5.Content) != 1 || cs5.Content[0].Kind != ContentCounters || cs5.Content[0].Sep != "." {
		t.Errorf("counters content = %+v", cs5.Content)
	}
	// A separator containing a comma (or a close paren) must survive parsing intact —
	// the arg split and the paren scan are both quote-aware.
	cs6 := initialStyle()
	applyDeclaration(&cs6, Declaration{Property: "content", Value: `counters(item, ", ")`})
	if len(cs6.Content) != 1 || cs6.Content[0].Sep != ", " {
		t.Errorf("comma-separator content = %+v, want Sep=%q", cs6.Content, ", ")
	}
	cs7 := initialStyle()
	applyDeclaration(&cs7, Declaration{Property: "content", Value: `counter(c, upper-roman) ") "`})
	if len(cs7.Content) != 2 || cs7.Content[0].Style != "upper-roman" || cs7.Content[1].Text != ") " {
		t.Errorf("paren-in-string content = %+v", cs7.Content)
	}
}

// splitArgs keeps a quoted comma inside one argument.
func TestSplitArgsQuoteAware(t *testing.T) {
	got := splitArgs(`item, ", ", decimal`)
	want := []string{"item", `", "`, "decimal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitArgs = %#v, want %#v", got, want)
	}
}
