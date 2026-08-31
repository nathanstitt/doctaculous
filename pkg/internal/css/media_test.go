package css

import "testing"

// TestMediaPrintRulesCapturedAndFiltered checks @media print/screen blocks are
// captured (not discarded), tagged with their media type, and selectable via
// RulesForMedia.
func TestMediaPrintRulesCapturedAndFiltered(t *testing.T) {
	src := `
		p { color: red }
		@media print { p { color: black } }
		@media screen { p { color: blue } }
	`
	sheet := Parse(src)

	all := sheet.RulesForMedia(MediaAll)
	if len(all) != 1 {
		t.Fatalf("MediaAll rules = %d; want 1 (the top-level p)", len(all))
	}
	printRules := sheet.RulesForMedia(MediaPrint)
	if len(printRules) != 2 {
		t.Fatalf("MediaPrint rules = %d; want 2 (top-level + @media print)", len(printRules))
	}
	screenRules := sheet.RulesForMedia(MediaScreen)
	if len(screenRules) != 2 {
		t.Fatalf("MediaScreen rules = %d; want 2 (top-level + @media screen)", len(screenRules))
	}
}

// TestMediaAllBlockAppliesEverywhere checks @media all rules apply in every context.
func TestMediaAllBlockAppliesEverywhere(t *testing.T) {
	sheet := Parse(`@media all { a { color: green } }`)
	if got := len(sheet.RulesForMedia(MediaPrint)); got != 1 {
		t.Fatalf("MediaPrint = %d; want 1 (@media all applies)", got)
	}
	if got := len(sheet.RulesForMedia(MediaScreen)); got != 1 {
		t.Fatalf("MediaScreen = %d; want 1 (@media all applies)", got)
	}
}

// TestMediaNestedRulesTagged checks each rule inside an @media block is tagged with
// that block's media type.
func TestMediaNestedRulesTagged(t *testing.T) {
	sheet := Parse(`@media print { h1 { color: black } h2 { color: gray } }`)
	if len(sheet.Rules) != 2 {
		t.Fatalf("captured %d rules; want 2", len(sheet.Rules))
	}
	for _, r := range sheet.Rules {
		if r.Media != MediaPrint {
			t.Errorf("rule %v tagged %v; want MediaPrint", r.Selectors, r.Media)
		}
	}
}
