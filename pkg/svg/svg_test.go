package svg

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func TestParseAndSizing(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	cases := []struct {
		src  string
		w, h float64
	}{
		{`<svg ` + hdr + ` width="200" height="100"/>`, 200, 100},
		{`<svg ` + hdr + ` width="2in" height="1in"/>`, 192, 96},
		{`<svg ` + hdr + ` viewBox="0 0 400 300"/>`, 400, 300},
		{`<svg ` + hdr + ` width="200" viewBox="0 0 400 300"/>`, 200, 150}, // ratio-derived height
		{`<svg ` + hdr + ` width="50%" viewBox="0 0 400 300"/>`, 400, 300}, // % falls to viewBox
		{`<svg ` + hdr + `/>`, 300, 150},
	}
	for _, c := range cases {
		d, err := Parse([]byte(c.src), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if d.WidthPt != c.w || d.HeightPt != c.h {
			t.Errorf("%s: size = %gx%g, want %gx%g", c.src, d.WidthPt, d.HeightPt, c.w, c.h)
		}
	}

	// Scene: g transform + inherited fill reach the shape; defs skipped;
	// unsupported element logged once.
	src := `<svg ` + hdr + ` width="100" height="100">
	  <defs><rect id="d" width="5" height="5"/></defs>
	  <g fill="red" transform="translate(10,0)"><rect width="20" height="20"/></g>
	  <text>skip me</text><text>and me</text>
	</svg>`
	var logs []string
	d, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d, want 1 (defs and text skipped)", len(root.Kids))
	}
	g, ok := root.Kids[0].(*Group)
	if !ok || len(g.Kids) != 1 {
		t.Fatalf("g = %#v", root.Kids[0])
	}
	if x, _ := g.M.Apply(0, 0); x != 10 {
		t.Errorf("g transform tx = %g", x)
	}
	sh := g.Kids[0].(*Shape)
	fp, okf := sh.Style.FillPaint()
	if !okf || fp.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("shape fill = %+v %v", fp, okf)
	}
	textLogs := 0
	for _, l := range logs {
		if strings.Contains(l, "<text>") {
			textLogs++
		}
	}
	if textLogs != 1 {
		t.Errorf("text logged %d times, want once per element name", textLogs)
	}
}
