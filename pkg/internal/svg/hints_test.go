package svg

import (
	"reflect"
	"testing"
)

// svgPresentationAttrsNotConsumedByApply lists the svgPresentationAttrs
// entries that Style.apply() deliberately never reads, because they are
// resolved somewhere else entirely (see the doc comments on
// svgPresentationAttrs and on each field below for exactly where):
// stop-color/stop-opacity only ever apply to a <stop> element, consumed by
// parseStops in stops.go; flood-color/flood-opacity/color-interpolation-
// filters only ever apply to an <fe*> filter primitive, consumed by
// resolveFloodColor/primitiveColorSpace in filter.go. Neither group is
// describable by the general cascade's Style, so apply() never reads them —
// but both must stay listed in svgPresentationAttrs so the attribute and
// stylesheet-rule forms rank identically.
var svgPresentationAttrsNotConsumedByApply = map[string]bool{
	"stop-color":                  true,
	"stop-opacity":                true,
	"flood-color":                 true,
	"flood-opacity":               true,
	"color-interpolation-filters": true,
}

// TestSVGPresentationHintsSyncedWithStyleApply guards the exact failure mode
// hints.go's package comment warns about by hand: a property added to
// Style.apply()'s appliers but never listed in svgPresentationAttrs (or vice
// versa) silently stops being settable from a presentation attribute, with
// no compiler error and no other test catching it. This drives every
// attribute name in svgPresentationAttrs through a real Style.apply() call
// (with a sentinel value chosen to be valid for that property) and asserts
// SOME observable field of the resulting Style changed from the default —
// proving the attribute actually reaches an applyXxx call, not just that
// hints.go lists it. An attribute apply() legitimately never consumes (the
// stop-color/stop-opacity carve-out) is skipped via the map above, mirroring
// the exception hints.go's own doc comment documents.
func TestSVGPresentationHintsSyncedWithStyleApply(t *testing.T) {
	sentinels := map[string]string{
		"color":              "rgb(1,2,3)",
		"fill":               "rgb(4,5,6)",
		"fill-opacity":       "0.42",
		"fill-rule":          "evenodd",
		"stroke":             "rgb(7,8,9)",
		"stroke-opacity":     "0.42",
		"stroke-width":       "7",
		"stroke-linecap":     "round",
		"stroke-linejoin":    "round",
		"stroke-miterlimit":  "7",
		"stroke-dasharray":   "1 2 3",
		"stroke-dashoffset":  "7",
		"opacity":            "0.42",
		"display":            "none",
		"visibility":         "hidden",
		"clip-path":          "url(#x)",
		"clip-rule":          "evenodd",
		"mask":               "url(#x)",
		"mask-type":          "alpha",
		"filter":             "url(#x)",
		"overflow":           "visible",
		"marker-start":       "url(#x)",
		"marker-mid":         "url(#x)",
		"marker-end":         "url(#x)",
		"font":               "italic 42px Verdana",
		"font-family":        "Verdana",
		"font-size":          "42",
		"font-weight":        "bold",
		"font-style":         "italic",
		"font-stretch":       "condensed",
		"font-variant":       "small-caps",
		"kerning":            "0",
		"font-kerning":       "none",
		"writing-mode":       "vertical-rl",
		"text-orientation":   "upright",
		"text-anchor":        "middle",
		"direction":          "rtl",
		"unicode-bidi":       "bidi-override",
		"letter-spacing":     "7",
		"word-spacing":       "7",
		"dominant-baseline":  "middle",
		"alignment-baseline": "middle",
		"baseline-shift":     "super",
		"text-decoration":    "underline",
	}

	for _, name := range svgPresentationAttrs {
		if svgPresentationAttrsNotConsumedByApply[name] {
			continue
		}
		val, ok := sentinels[name]
		if !ok {
			t.Errorf("svgPresentationAttrs contains %q but the sync test has no sentinel value for it; add one (or add it to svgPresentationAttrsNotConsumedByApply if apply() deliberately never reads it)", name)
			continue
		}
		before := defaultStyle()
		after := applyAttrs(before, map[string]string{name: val})
		if reflectDeepEqualStyle(before, after) {
			t.Errorf("apply() with %s=%q produced no change: %q is listed in svgPresentationAttrs but Style.apply() never consumes it (hints.go/style.go are out of sync)", name, val, name)
		}
	}
}

// reflectDeepEqualStyle reports whether a and b are field-for-field equal.
// Style is not comparable with == (it holds a []float64 dashes slice), and
// it has no exported fields for reflect.DeepEqual to reach from outside the
// package — but this test lives inside package svg, so reflect.DeepEqual on
// the whole struct works directly, unexported fields included.
func reflectDeepEqualStyle(a, b Style) bool {
	return reflect.DeepEqual(a, b)
}

// TestMarkerShorthandNotAPresentationAttribute pins the ONE intentional
// exception hints.go's svgPresentationAttrs doc comment calls out: the
// "marker" shorthand is consumed by the cascade (setResolved expands it into
// the three longhands) but deliberately excluded from svgPresentationAttrs,
// so a bare XML
// marker="url(#m)" attribute must have NO effect (matching resvg's
// the-marker-property.svg, titled "Should be ignored"), while the exact same
// value reaching apply() through style="" (which cascade.go resolves
// independently of svgPresentationAttrs) DOES take effect (matching
// the-marker-property-in-CSS.svg/recursive-4.svg).
func TestMarkerShorthandNotAPresentationAttribute(t *testing.T) {
	asAttr := applyAttrs(defaultStyle(), map[string]string{"marker": "url(#m)"})
	if _, ok := asAttr.MarkerStartRef(); ok {
		t.Error("bare marker=\"url(#m)\" attribute applied a marker; want it ignored (not a presentation attribute)")
	}

	// applyAttrs threads a nil cascadeCtx, which short-circuits to
	// presentation-hints-only (see cascadeCtx.resolve's doc comment) and
	// never even looks at style="" — a real ctx (non-nil idx) is needed to
	// exercise the inline-style path at all.
	el := &element{attrs: map[string]string{"style": "marker:url(#m)"}}
	ctx := &cascadeCtx{idx: &docIndex{}}
	asStyle := defaultStyle().apply(el, ctx)
	if _, ok := asStyle.MarkerStartRef(); !ok {
		t.Error("style=\"marker:url(#m)\" did not apply a marker; want the shorthand honored via inline style")
	}
}

func TestSVGPresentationHintsDeterminism(t *testing.T) {
	// Verify that repeated calls return the same order (no map randomization).
	el := &element{attrs: map[string]string{
		"fill": "red", "stroke": "blue", "opacity": "0.5", "color": "green",
		"stroke-width": "2", "fill-opacity": "0.8", "display": "none",
	}}

	// Call multiple times and ensure order is identical each time.
	var previous []string
	for i := 0; i < 3; i++ {
		hints := svgPresentationHints(el)
		var current []string
		for _, h := range hints {
			current = append(current, h.Property)
		}
		if i == 0 {
			previous = current
		} else if len(current) != len(previous) || func() bool {
			for j, p := range previous {
				if j >= len(current) || current[j] != p {
					return true
				}
			}
			return false
		}() {
			t.Errorf("run %d: order changed from %v to %v", i, previous, current)
		}
	}
}

func TestSVGPresentationHints(t *testing.T) {
	el := &element{attrs: map[string]string{
		"fill": "red", "stroke-width": "2", "class": "x", "style": "fill:blue",
		"id": "n", "d": "M0 0", "width": "10",
	}}
	got := svgPresentationHints(el)
	byProp := map[string]string{}
	for _, d := range got {
		if d.Important {
			t.Errorf("hint %q must not be !important", d.Property)
		}
		byProp[d.Property] = d.Value
	}
	if byProp["fill"] != "red" || byProp["stroke-width"] != "2" {
		t.Errorf("hints = %v", byProp)
	}
	for _, notHint := range []string{"class", "style", "id", "d", "width"} {
		if _, ok := byProp[notHint]; ok {
			t.Errorf("%q must not be a presentation hint", notHint)
		}
	}
	if len(svgPresentationHints(&element{})) != 0 {
		t.Error("no attributes should yield no hints")
	}
	if svgPresentationHints(nil) != nil {
		t.Error("nil element must not panic and should yield nil")
	}
}
