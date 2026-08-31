package css

import (
	"image/color"
	"testing"
)

// A comma-separated `background` list parses into layers instead of being rejected.
// It used to fail as a whole: `background: <gradient>, <color>` — the ordinary way to
// give a gradient a fallback colour — dropped the entire declaration, so the element
// painted nothing and it read as "gradients are unsupported".
func TestBackgroundShorthandLayerList(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background", Value: "linear-gradient(#ff0000,#ff0000), #0000ff"})
	if n := len(cs.BackgroundLayers); n != 2 {
		t.Fatalf("got %d layers, want 2", n)
	}
	if cs.BackgroundLayers[0].Gradient == nil {
		t.Error("layer 0 has no gradient")
	}
	if cs.BackgroundLayers[1].HasImage() {
		t.Error("layer 1 should be the colour-only layer, with no image")
	}
	if want := (color.RGBA{B: 0xff, A: 0xff}); cs.BackgroundColor != want {
		t.Errorf("background-color = %v, want %v (from the LAST layer)", cs.BackgroundColor, want)
	}
}

// The single-value fields mirror layer 0, so a consumer that only understands one
// background keeps working.
func TestBackgroundLayersMirrorFirstLayer(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background", Value: "linear-gradient(#ff0000,#ff0000), linear-gradient(#00ff00,#00ff00)"})
	if len(cs.BackgroundLayers) != 2 {
		t.Fatalf("got %d layers, want 2", len(cs.BackgroundLayers))
	}
	if cs.BackgroundGradient != cs.BackgroundLayers[0].Gradient {
		t.Error("BackgroundGradient does not mirror layer 0")
	}
}

// A colour is only legal in the FINAL layer (CSS Backgrounds §3.10). One earlier is a
// parse error, not something to ignore — otherwise `background: red, blue` would
// quietly paint one of them.
func TestBackgroundColorOnlyInLastLayer(t *testing.T) {
	cs := initialStyle()
	cs.BackgroundColor = color.RGBA{G: 0xff, A: 0xff}
	applyDeclaration(&cs, Declaration{Property: "background", Value: "#ff0000, linear-gradient(#0000ff,#0000ff)"})
	if want := (color.RGBA{G: 0xff, A: 0xff}); cs.BackgroundColor != want {
		t.Errorf("background-color = %v, want the previous value preserved (a colour in a non-final layer is invalid)", cs.BackgroundColor)
	}
}

// One invalid layer invalidates the whole declaration, per CSS error handling, rather
// than painting a partial list the author did not write.
func TestBackgroundInvalidLayerDropsDeclaration(t *testing.T) {
	cs := initialStyle()
	cs.BackgroundColor = color.RGBA{G: 0xff, A: 0xff}
	applyDeclaration(&cs, Declaration{Property: "background", Value: "linear-gradient(#ff0000,#ff0000), notacolor"})
	if want := (color.RGBA{G: 0xff, A: 0xff}); cs.BackgroundColor != want {
		t.Errorf("background-color = %v, want the previous value preserved", cs.BackgroundColor)
	}
	if len(cs.BackgroundLayers) != 0 {
		t.Errorf("got %d layers for an invalid declaration, want 0", len(cs.BackgroundLayers))
	}
}

// background-image takes the same list.
func TestBackgroundImageLayerList(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background-image", Value: "linear-gradient(#ff0000,#ff0000), linear-gradient(#00ff00,#00ff00)"})
	if n := len(cs.BackgroundLayers); n != 2 {
		t.Fatalf("got %d layers, want 2", n)
	}
	for i, l := range cs.BackgroundLayers {
		if l.Gradient == nil {
			t.Errorf("layer %d has no gradient", i)
		}
	}
}

// background-image must NOT clobber the other background-* longhands, in either
// declaration order. An earlier version captured them when the image list was parsed,
// which silently discarded a background-size declared afterwards.
func TestBackgroundImageDoesNotClobberLonghands(t *testing.T) {
	for _, order := range []struct {
		name  string
		decls [][2]string
	}{
		{"size after image", [][2]string{{"background-image", "linear-gradient(#ff0000,#ff0000)"}, {"background-size", "50px 30px"}}},
		{"size before image", [][2]string{{"background-size", "50px 30px"}, {"background-image", "linear-gradient(#ff0000,#ff0000)"}}},
	} {
		cs := initialStyle()
		for _, d := range order.decls {
			applyDeclaration(&cs, Declaration{Property: d[0], Value: d[1]})
		}
		if cs.BackgroundSize.Kind == BgSizeAuto {
			t.Errorf("%s: background-size was lost", order.name)
		}
	}
}

// A single value still yields a one-layer list, so the common case is unchanged.
func TestSingleBackgroundStillWorks(t *testing.T) {
	cs := initialStyle()
	applyDeclaration(&cs, Declaration{Property: "background", Value: "linear-gradient(#ff0000,#ff0000)"})
	if n := len(cs.BackgroundLayers); n != 1 {
		t.Fatalf("got %d layers, want 1", n)
	}
	if cs.BackgroundGradient == nil {
		t.Error("the single-value gradient field was not set")
	}
}
