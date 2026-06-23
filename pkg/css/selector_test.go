package css

import "testing"

func TestSelectorSpecificity(t *testing.T) {
	cases := []struct {
		in   string
		spec Specificity // {ids, classes, types}
	}{
		{"*", Specificity{0, 0, 0}},
		{"div", Specificity{0, 0, 1}},
		{".intro", Specificity{0, 1, 0}},
		{"#lead", Specificity{1, 0, 0}},
		{"div.intro", Specificity{0, 1, 1}},
		{"div p", Specificity{0, 0, 2}},
		{"#lead .intro p", Specificity{1, 1, 1}},
	}
	for _, c := range cases {
		sels := parseSelectorList(c.in)
		if len(sels) != 1 {
			t.Fatalf("parseSelectorList(%q) = %d selectors, want 1", c.in, len(sels))
		}
		if sels[0].Specificity() != c.spec {
			t.Fatalf("%q specificity = %v, want %v", c.in, sels[0].Specificity(), c.spec)
		}
	}
}

func TestParseSelectorGroup(t *testing.T) {
	sels := parseSelectorList("h1, h2, .title")
	if len(sels) != 3 {
		t.Fatalf("got %d selectors, want 3", len(sels))
	}
}
