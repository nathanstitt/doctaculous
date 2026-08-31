package css

import "testing"

// px is a shorthand for a pixel Length in the expectations below.
func px(v float64) Length { return Length{v, UnitPx} }

// pct is a shorthand for a percentage Length.
func pct(v float64) Length { return Length{v, UnitPercent} }

// radii returns a style's four corners in the CSS writing order so a test can
// compare them as one value.
func radii(cs *ComputedStyle) [4]CornerRadius {
	return [4]CornerRadius{
		cs.BorderTopLeftRadius, cs.BorderTopRightRadius,
		cs.BorderBottomRightRadius, cs.BorderBottomLeftRadius,
	}
}

func all(c CornerRadius) [4]CornerRadius { return [4]CornerRadius{c, c, c, c} }

func TestBorderRadiusShorthand(t *testing.T) {
	circ := func(v float64) CornerRadius { return CornerRadius{px(v), px(v)} }

	tests := []struct {
		name  string
		value string
		want  [4]CornerRadius
	}{
		{"one value rounds every corner", "10px", all(circ(10))},
		{
			// The 2-value form pairs DIAGONALLY OPPOSITE corners, unlike the
			// clockwise side rule margin/padding use.
			"two values pair opposite corners", "10px 20px",
			[4]CornerRadius{circ(10), circ(20), circ(10), circ(20)},
		},
		{
			"three values", "10px 20px 30px",
			[4]CornerRadius{circ(10), circ(20), circ(30), circ(20)},
		},
		{
			"four values in corner order", "10px 20px 30px 40px",
			[4]CornerRadius{circ(10), circ(20), circ(30), circ(40)},
		},
		{
			// The slash form: horizontal radii before, vertical after. Every corner
			// becomes elliptical.
			"slash makes corners elliptical", "10px / 20px",
			all(CornerRadius{px(10), px(20)}),
		},
		{
			"slash with no surrounding spaces", "10px/20px",
			all(CornerRadius{px(10), px(20)}),
		},
		{
			"slash with independent 4-value groups", "10px 20px 30px 40px / 1px 2px 3px 4px",
			[4]CornerRadius{
				{px(10), px(1)}, {px(20), px(2)}, {px(30), px(3)}, {px(40), px(4)},
			},
		},
		{
			// The two groups expand independently, so a 1-value vertical group
			// applies to all four corners while the horizontal one varies.
			"groups expand independently", "10px 20px / 5px",
			[4]CornerRadius{
				{px(10), px(5)}, {px(20), px(5)}, {px(10), px(5)}, {px(20), px(5)},
			},
		},
		{"percentages are kept unresolved", "50%", all(CornerRadius{pct(50), pct(50)})},
		{
			"percentage slash form", "50% / 25%",
			all(CornerRadius{pct(50), pct(25)}),
		},
		{"zero", "0", all(CornerRadius{px(0), px(0)})},
		{"em radii", "2em", all(CornerRadius{Length{2, UnitEm}, Length{2, UnitEm}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := &ComputedStyle{}
			applyBorderRadius(cs, tt.value)
			if got := radii(cs); got != tt.want {
				t.Errorf("border-radius: %s\n got %v\nwant %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestBorderRadiusInvalid checks the whole-declaration-drop policy: an invalid
// component anywhere leaves the PRIOR radii untouched rather than partially
// applying, matching how browsers treat an invalid shorthand.
func TestBorderRadiusInvalid(t *testing.T) {
	prior := all(CornerRadius{px(7), px(7)})
	for _, value := range []string{
		"-5px",                     // negative radius is a parse error, not a clamp
		"10px -5px",                // one bad component voids the whole declaration
		"auto",                     // `auto` is not a valid radius
		"10px 20px 30px 40px 50px", // 5 components: over-length
		"",                         // empty
		"/ 10px",                   // empty horizontal group
		"10px /",                   // empty vertical group
		"10px / 20px / 30px",       // two slashes
		"red",                      // not a length at all
	} {
		t.Run(value, func(t *testing.T) {
			cs := &ComputedStyle{
				BorderTopLeftRadius: prior[0], BorderTopRightRadius: prior[1],
				BorderBottomRightRadius: prior[2], BorderBottomLeftRadius: prior[3],
			}
			applyBorderRadius(cs, value)
			if got := radii(cs); got != prior {
				t.Errorf("invalid %q should have left radii untouched, got %v", value, got)
			}
		})
	}
}

func TestCornerRadiusLonghand(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  CornerRadius
		drop  bool // true => the declaration is invalid and must not apply
	}{
		{name: "single value is circular", value: "10px", want: CornerRadius{px(10), px(10)}},
		{name: "two values are elliptical", value: "10px 20px", want: CornerRadius{px(10), px(20)}},
		{name: "percentage", value: "50%", want: CornerRadius{pct(50), pct(50)}},
		{name: "mixed length and percentage", value: "10px 50%", want: CornerRadius{px(10), pct(50)}},
		{name: "a slash is invalid in a longhand", value: "10px / 20px", drop: true},
		{name: "negative is invalid", value: "-1px", drop: true},
		{name: "three values is invalid", value: "1px 2px 3px", drop: true},
		{name: "auto is invalid", value: "auto", drop: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := CornerRadius{px(3), px(4)}
			got := prior
			applyCornerRadius(&got, tt.value)
			want := tt.want
			if tt.drop {
				want = prior
			}
			if got != want {
				t.Errorf("%q: got %v, want %v", tt.value, got, want)
			}
		})
	}
}

// TestBorderRadiusThroughCascade checks the properties are reachable by name
// through applyDeclaration, so the longhands and the shorthand actually cascade
// (source order, longhand-overrides-shorthand) like every other property.
func TestBorderRadiusThroughCascade(t *testing.T) {
	cs := &ComputedStyle{}
	for _, d := range []Declaration{
		{Property: "border-radius", Value: "10px"},
		{Property: "border-top-left-radius", Value: "1px 2px"},
	} {
		applyDeclaration(cs, d)
	}
	if got, want := cs.BorderTopLeftRadius, (CornerRadius{px(1), px(2)}); got != want {
		t.Errorf("longhand should override the earlier shorthand: got %v, want %v", got, want)
	}
	if got, want := cs.BorderTopRightRadius, (CornerRadius{px(10), px(10)}); got != want {
		t.Errorf("untouched corner should keep the shorthand: got %v, want %v", got, want)
	}
}

func TestCornerRadiusZero(t *testing.T) {
	// A corner is square when EITHER semi-axis is zero: an ellipse with a zero
	// axis is degenerate (Backgrounds 3 §5.1).
	tests := []struct {
		name string
		c    CornerRadius
		want bool
	}{
		{"both zero", CornerRadius{}, true},
		{"horizontal zero", CornerRadius{px(0), px(5)}, true},
		{"vertical zero", CornerRadius{px(5), px(0)}, true},
		{"both non-zero", CornerRadius{px(5), px(5)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Zero(); got != tt.want {
				t.Errorf("Zero() = %v, want %v", got, tt.want)
			}
		})
	}
}
