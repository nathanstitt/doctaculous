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

func TestParseSelectorListSkipsMalformed(t *testing.T) {
	// Malformed groups (empty qualifier names) are skipped; valid ones survive.
	sels := parseSelectorList("h1, ., #, p.intro")
	if len(sels) != 2 {
		t.Fatalf("got %d selectors, want 2 (h1 and p.intro; '.' and '#' dropped)", len(sels))
	}
	// Confirm the survivors are the right ones by specificity.
	if sels[0].Specificity() != (Specificity{0, 0, 1}) { // h1
		t.Errorf("sels[0] specificity = %v, want {0 0 1} (h1)", sels[0].Specificity())
	}
	if sels[1].Specificity() != (Specificity{0, 1, 1}) { // p.intro
		t.Errorf("sels[1] specificity = %v, want {0 1 1} (p.intro)", sels[1].Specificity())
	}
}
