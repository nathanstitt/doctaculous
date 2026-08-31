package svg

import (
	"image/color"
	"testing"
)

func TestParseColorValue(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		ok   bool
	}{
		{"red", color.RGBA{255, 0, 0, 255}, true},
		{"Rebeccapurple", color.RGBA{102, 51, 153, 255}, true},
		{"cornflowerblue", color.RGBA{100, 149, 237, 255}, true},
		{"teal", color.RGBA{0, 128, 128, 255}, true},
		{"goldenrod", color.RGBA{218, 165, 32, 255}, true},
		{"lavenderblush", color.RGBA{255, 240, 245, 255}, true},
		{"transparent", color.RGBA{0, 0, 0, 0}, true},
		{"#f00", color.RGBA{255, 0, 0, 255}, true},
		{"#ff000080", color.RGBA{255, 0, 0, 128}, true},
		{"#f008", color.RGBA{255, 0, 0, 136}, true},
		{"rgb(1,2,3)", color.RGBA{1, 2, 3, 255}, true},
		{"rgb(100%, 0%, 0%)", color.RGBA{255, 0, 0, 255}, true},
		{"rgba(255, 0, 0, 0.5)", color.RGBA{255, 0, 0, 128}, true},
		{"rgb(255 0 0 / 50%)", color.RGBA{255, 0, 0, 128}, true},
		{"rgb(300,-5,0)", color.RGBA{255, 0, 0, 255}, true}, // clamped
		{"hsl(0, 100%, 50%)", color.RGBA{255, 0, 0, 255}, true},
		{"hsl(120 100% 25%)", color.RGBA{0, 128, 0, 255}, true},
		{"hsla(240,100%,50%,0.5)", color.RGBA{0, 0, 255, 128}, true},
		{"url(#grad)", color.RGBA{}, false}, // paint server: not a color (caller degrades)
		{"blurple", color.RGBA{}, false},
		{"", color.RGBA{}, false},
		{"rgb(", color.RGBA{}, false},
		{"rgb()", color.RGBA{}, false},
		{"hsl(1)", color.RGBA{}, false},
		{"#", color.RGBA{}, false},
		{"#12345", color.RGBA{}, false},
		{"rgb(a,b,c)", color.RGBA{}, false},
		{"#f", color.RGBA{}, false},
		{"ééé", color.RGBA{}, false}, // unicode, not a keyword
	}
	for _, c := range cases {
		got, ok := parseColorValue(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseColorValue(%q) = %v,%v; want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
