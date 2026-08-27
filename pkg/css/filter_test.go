package css

import "testing"

// TestFilterDeclarationKeptRaw: the cascade stores the `filter` value verbatim (the
// grammar is parsed at use time, where a length resolver exists), and normalizes
// `none` — the initial value — to the empty string so an unfiltered box is the zero
// value and every downstream check is one emptiness test.
func TestFilterDeclarationKeptRaw(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"grayscale(1)", "grayscale(1)"},
		{"blur(2px) drop-shadow(1px 1px red)", "blur(2px) drop-shadow(1px 1px red)"},
		{"  grayscale(1)  ", "grayscale(1)"},
		{"none", ""},
		{"NONE", ""},
		{"  none  ", ""},
		// An unparseable value is stored raw too: rejecting it is the CONSUMER's job
		// (filtereffects.Parse), because only the consumer knows the length basis.
		{"nonsense(", "nonsense("},
	} {
		cs := initialStyle()
		applyDeclaration(&cs, Declaration{Property: "filter", Value: tc.in})
		if cs.Filter != tc.want {
			t.Errorf("filter:%q → %q, want %q", tc.in, cs.Filter, tc.want)
		}
	}
}

// TestFilterInitialIsNone: the initial value is "no filter".
func TestFilterInitialIsNone(t *testing.T) {
	if got := initialStyle().Filter; got != "" {
		t.Errorf("initial filter = %q, want \"\" (none)", got)
	}
}

// TestFilterNotInherited: `filter` is not an inherited property. Inheriting it would
// re-apply the effect at every descendant, compounding it.
func TestFilterNotInherited(t *testing.T) {
	parent := initialStyle()
	parent.Filter = "grayscale(1)"
	if got := inheritFrom(parent).Filter; got != "" {
		t.Errorf("child inherited filter = %q, want \"\" (filter does not inherit)", got)
	}
}

// TestFilterNoneResetsAPriorDeclaration: a later `filter: none` must clear an earlier
// value, not be ignored as an unrecognized keyword.
func TestFilterNoneResetsAPriorDeclaration(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "filter", Value: "sepia(1)"})
	applyDeclaration(&cs, Declaration{Property: "filter", Value: "none"})
	if cs.Filter != "" {
		t.Errorf("filter after none = %q, want \"\"", cs.Filter)
	}
}
