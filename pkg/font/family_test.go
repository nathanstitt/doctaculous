package font

import "testing"

func TestLoadStandardArial(t *testing.T) {
	face, ok := LoadStandard("Arial", Style{})
	if !ok {
		t.Fatal("LoadStandard(Arial): want a bundled substitute, got ok=false")
	}
	outline, adv, ok := face.Glyph('A')
	if !ok {
		t.Fatal("Glyph('A'): want ok=true")
	}
	if outline == nil || outline.Empty() {
		t.Error("Glyph('A'): want a non-empty outline")
	}
	// A capital A in a typical sans advances well under a full em and clearly more
	// than nothing; a loose sanity band catches a broken advance without pinning a
	// specific face's metrics.
	if adv <= 0.1 || adv >= 1.5 {
		t.Errorf("Glyph('A') advance = %v em, want within (0.1, 1.5)", adv)
	}

	asc, desc, _ := face.Metrics()
	if asc <= 0 || desc <= 0 {
		t.Errorf("Metrics() ascent=%v descent=%v, want both positive", asc, desc)
	}
}

func TestLoadStandardSpaceHasAdvanceNoOutline(t *testing.T) {
	face, ok := LoadStandard("Times New Roman", Style{})
	if !ok {
		t.Fatal("LoadStandard(Times New Roman): want ok=true")
	}
	outline, adv, ok := face.Glyph(' ')
	if !ok {
		t.Fatal("Glyph(' '): want ok=true")
	}
	if outline != nil && !outline.Empty() {
		t.Error("Glyph(' '): want a nil/empty outline for a space")
	}
	if adv <= 0 {
		t.Errorf("Glyph(' ') advance = %v, want > 0", adv)
	}
}

func TestLoadStandardOfficeDefaults(t *testing.T) {
	for _, family := range []string{"Calibri", "Cambria", "Consolas"} {
		if _, ok := LoadStandard(family, Style{}); !ok {
			t.Errorf("LoadStandard(%q): want a bundled substitute", family)
		}
	}
}

func TestLoadStandardUnsupported(t *testing.T) {
	for _, family := range []string{"Wingdings", "Symbol", "ZapfDingbats", "NoSuchFont"} {
		if face, ok := LoadStandard(family, Style{}); ok || face != nil {
			t.Errorf("LoadStandard(%q): want ok=false, nil face; got ok=%v", family, ok)
		}
	}
}
