package css

import (
	"image/color"
	"strings"
	"testing"
)

// substTest exercises the var() substitution engine directly, without the
// cascade, so a failure localizes to the grammar rather than to resolution
// order.
func substTest(t *testing.T, props map[string]string, value string) (string, bool) {
	t.Helper()
	var cp CustomProps
	for k, v := range props {
		cp.set(k, v)
	}
	return substituteVars(value, cp)
}

func TestSubstituteVarsBasic(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]string
		in    string
		want  string
		ok    bool
	}{
		{"plain reference", map[string]string{"--c": "red"}, "var(--c)", "red", true},
		{"embedded in a larger value", map[string]string{"--w": "2px"}, "var(--w) solid black", "2px solid black", true},
		{"multiple references", map[string]string{"--a": "1px", "--b": "red"}, "var(--a) solid var(--b)", "1px solid red", true},
		{"indirection", map[string]string{"--a": "var(--b)", "--b": "blue"}, "var(--a)", "blue", true},
		{"function name is case-insensitive", map[string]string{"--c": "red"}, "VAR(--c)", "red", true},

		// Fallbacks.
		{"fallback used when undeclared", nil, "var(--nope, green)", "green", true},
		{"fallback ignored when declared", map[string]string{"--c": "red"}, "var(--c, green)", "red", true},
		{"fallback may contain commas", nil, "var(--nope, rgb(1, 2, 3))", "rgb(1, 2, 3)", true},
		{"fallback may contain var()", map[string]string{"--b": "blue"}, "var(--nope, var(--b))", "blue", true},
		{"empty fallback is valid", nil, "var(--nope,)", "", true},

		// A declared-but-empty property substitutes nothing and does NOT fall
		// back — the spec distinguishes "declared empty" from "undeclared".
		{"declared empty substitutes nothing", map[string]string{"--c": ""}, "var(--c, red)", "", true},

		// Invalid at computed-value time.
		{"undeclared with no fallback", nil, "var(--nope)", "", false},
		{"non-custom name", map[string]string{"--c": "red"}, "var(c)", "", false},
		{"unterminated var(", nil, "var(--c", "", false},

		// Values with no var() at all must come back byte-identical.
		{"no var() passes through", nil, "1px solid black", "1px solid black", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := substTest(t, tc.props, tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.ok, got)
			}
			if ok && strings.TrimSpace(got) != strings.TrimSpace(tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSubstituteVarsCycles covers CSS Variables 1 §3: a cyclic reference makes
// every property in the cycle invalid at computed-value time. Without the
// active-set check these would recurse until the depth limit or the stack, so
// each case asserts ok=false rather than merely "does not hang".
func TestSubstituteVarsCycles(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]string
		in    string
	}{
		{"self reference", map[string]string{"--a": "var(--a)"}, "var(--a)"},
		{"two-step cycle", map[string]string{"--a": "var(--b)", "--b": "var(--a)"}, "var(--a)"},
		{"three-step cycle", map[string]string{"--a": "var(--b)", "--b": "var(--c)", "--c": "var(--a)"}, "var(--a)"},
		{"cycle reached through a fallback", map[string]string{"--a": "var(--nope, var(--a))"}, "var(--a)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := substTest(t, tc.props, tc.in); ok {
				t.Error("cyclic reference resolved; want invalid at computed-value time")
			}
		})
	}
}

// TestSubstituteVarsNonCyclicFanOut guards the depth backstop. This chain is
// acyclic — the cycle detector cannot catch it — but each level doubles the
// number of references, so it must terminate via maxVarSubstitutionDepth rather
// than exhausting memory.
func TestSubstituteVarsNonCyclicFanOut(t *testing.T) {
	props := map[string]string{}
	const levels = 40 // deliberately deeper than maxVarSubstitutionDepth
	for i := 0; i < levels; i++ {
		props["--v"+string(rune('a'+i%26))+string(rune('0'+i/26))] = "x"
	}
	// Build --l0: var(--l1) var(--l1); --l1: var(--l2) var(--l2); ...
	for i := 0; i < levels; i++ {
		next := "--l" + itoa(i+1)
		props["--l"+itoa(i)] = "var(" + next + ") var(" + next + ")"
	}
	props["--l"+itoa(levels)] = "x"

	// The contract is termination with a definite answer, not a specific one.
	done := make(chan struct{})
	go func() {
		defer close(done)
		substTest(t, props, "var(--l0)")
	}()
	<-done
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestCustomPropertyNamesAreCaseSensitive locks the spec rule that --Foo and
// --foo are distinct properties, which is the one place custom properties
// diverge from normal CSS property-name handling.
func TestCustomPropertyNamesAreCaseSensitive(t *testing.T) {
	decls := ParseDeclarations("--Foo: red; --foo: blue; COLOR: green")
	var sawUpper, sawLower bool
	for _, d := range decls {
		switch d.Property {
		case "--Foo":
			sawUpper = true
		case "--foo":
			sawLower = true
		case "color":
			// Normal property names must still be normalized to lower case.
		case "COLOR":
			t.Error("normal property name was not lowercased")
		}
	}
	if !sawUpper || !sawLower {
		t.Fatalf("case was not preserved: got %+v", decls)
	}
}

// TestCustomPropsCopyOnWrite verifies that a child mutating its inherited map
// does not corrupt the parent's — the correctness half of the copy-on-write
// sharing that keeps deep trees from copying a map per element.
func TestCustomPropsCopyOnWrite(t *testing.T) {
	var parent CustomProps
	parent.set("--c", "red")

	child := parent // share by reference, as inheritFrom does
	child.set("--c", "blue")

	if got, _ := parent.Get("--c"); got != "red" {
		t.Errorf("parent mutated by child: --c = %q, want red", got)
	}
	if got, _ := child.Get("--c"); got != "blue" {
		t.Errorf("child did not take new value: --c = %q, want blue", got)
	}
}

// TestVarCascadeIntegration drives the whole path — parse, cascade, inherit,
// substitute — for the case the Luckfox report measured as broken: a palette
// declared once on :root and referenced by a descendant.
func TestVarCascadeIntegration(t *testing.T) {
	tests := []struct {
		name  string
		css   string
		want  color.RGBA
		style func(ComputedStyle) color.RGBA
	}{
		{
			name:  "root variable reaches a descendant",
			css:   `:root { --bg: rgb(11,13,18) } div { background-color: var(--bg) }`,
			want:  color.RGBA{11, 13, 18, 255},
			style: func(cs ComputedStyle) color.RGBA { return cs.BackgroundColor },
		},
		{
			name:  "fallback applies when the property is undeclared",
			css:   `div { background-color: var(--missing, rgb(1,2,3)) }`,
			want:  color.RGBA{1, 2, 3, 255},
			style: func(cs ComputedStyle) color.RGBA { return cs.BackgroundColor },
		},
		{
			name:  "later declaration wins the custom-property cascade",
			css:   `:root { --c: rgb(1,1,1) } :root { --c: rgb(2,2,2) } div { color: var(--c) }`,
			want:  color.RGBA{2, 2, 2, 255},
			style: func(cs ComputedStyle) color.RGBA { return cs.Color },
		},
		{
			name: "a var() declared after its use still resolves",
			// Custom properties settle in their own pass before any var() is
			// substituted, so source order between the two rules is irrelevant.
			css:   `div { color: var(--late) } :root { --late: rgb(9,9,9) }`,
			want:  color.RGBA{9, 9, 9, 255},
			style: func(cs ComputedStyle) color.RGBA { return cs.Color },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := &fakeNode{tag: "html"}
			div := &fakeNode{tag: "div", parent: root}

			r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: Parse(tc.css)}}, func(string, ...any) {})
			rootStyle := r.ComputeRoot(root)
			got := tc.style(r.Compute(div, rootStyle))
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVarInvalidAtComputedValueTime locks the distinction that makes var()
// unlike every other parse failure in this engine: an unresolvable var() must
// leave the property at its inherited/initial value rather than leaving an
// earlier cascaded value showing.
func TestVarInvalidAtComputedValueTime(t *testing.T) {
	root := &fakeNode{tag: "html"}
	div := &fakeNode{tag: "div", parent: root}

	// The second declaration wins the cascade but cannot resolve. Per §3.2 the
	// result is NOT the first declaration's red — the property acts as unset,
	// which for the non-inherited background-color means the initial value.
	css := `div { background-color: rgb(255,0,0) } div { background-color: var(--nope) }`
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: Parse(css)}}, func(string, ...any) {})
	cs := r.Compute(div, r.ComputeRoot(root))

	if cs.BackgroundColor == (color.RGBA{255, 0, 0, 255}) {
		t.Error("unresolvable var() left the earlier cascaded value in place; " +
			"want invalid-at-computed-value-time (property acts as unset)")
	}
}

// TestVarInvalidSpecExample is the worked example from CSS Variables 1 §3.1,
// transcribed as written. The spec states the <p> elements "will have
// transparent backgrounds (the initial value for background-color), rather than
// red backgrounds", and contrasts it with the plain syntax error below.
func TestVarInvalidSpecExample(t *testing.T) {
	root := &fakeNode{tag: "html"}
	p := &fakeNode{tag: "p", parent: root}

	compute := func(sheet string) ComputedStyle {
		r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: Parse(sheet)}}, func(string, ...any) {})
		return r.Compute(p, r.ComputeRoot(root))
	}

	red := color.RGBA{255, 0, 0, 255}

	// The var() case: --not-a-color holds a length, so substitution succeeds but
	// the resulting value does not parse as a colour. Either way the declaration
	// is invalid at computed-value time and must NOT leave red showing.
	got := compute(`:root { --not-a-color: 20px }
	                p { background-color: red }
	                p { background-color: var(--not-a-color) }`).BackgroundColor
	if got == red {
		t.Error("var() invalid at computed-value time left red in place; want the initial value")
	}

	// The contrast case the spec calls out: a plain syntax error is discarded at
	// PARSE time, so the earlier red does survive. This asserts the two failure
	// modes stay distinguishable — if this ever starts returning transparent,
	// the var() fix has over-reached into ordinary declaration dropping.
	got = compute(`p { background-color: red }
	               p { background-color: 20px }`).BackgroundColor
	if got != red {
		t.Errorf("a plain syntax error dropped the earlier declaration: got %v, want red", got)
	}
}

// TestVarInvalidRespectsInheritance covers the other half of `unset`: for an
// INHERITED property, an unresolvable var() must fall back to the parent's value
// rather than to the initial value.
func TestVarInvalidRespectsInheritance(t *testing.T) {
	root := &fakeNode{tag: "html"}
	div := &fakeNode{tag: "div", parent: root}

	css := `html { color: rgb(0,128,0) } div { color: var(--undefined) }`
	r := NewResolver([]OriginSheet{{Origin: OriginAuthor, Sheet: Parse(css)}}, func(string, ...any) {})
	cs := r.Compute(div, r.ComputeRoot(root))

	if want := (color.RGBA{0, 128, 0, 255}); cs.Color != want {
		t.Errorf("inherited property did not fall back to the inherited value: got %v, want %v", cs.Color, want)
	}
}

// TestRootPseudoClass covers the selector this feature depends on. :root is the
// canonical home for a custom-property palette, so it is tested on its own
// rather than only through var().
func TestRootPseudoClass(t *testing.T) {
	root := &fakeNode{tag: "html"}
	child := &fakeNode{tag: "div", parent: root}

	sels := parseSelectorList(":root")
	if len(sels) != 1 {
		t.Fatalf("parseSelectorList(:root) returned %d selectors", len(sels))
	}
	if !sels[0].Matches(root) {
		t.Error(":root did not match the root element")
	}
	if sels[0].Matches(child) {
		t.Error(":root matched a non-root element")
	}
	// :root carries class-level specificity (0,1,0), not zero.
	if sp := sels[0].Specificity(); sp.Classes != 1 || sp.IDs != 0 || sp.Types != 0 {
		t.Errorf("specificity = %+v, want one class-level unit", sp)
	}
}
