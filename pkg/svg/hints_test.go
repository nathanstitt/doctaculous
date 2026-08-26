package svg

import (
	"testing"
)

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
