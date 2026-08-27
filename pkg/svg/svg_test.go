package svg

import (
	"fmt"
	"image/color"
	"math"
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

// assertFinite fails t if v is NaN or ±Inf.
func assertFinite(t *testing.T, label string, v float64) {
	t.Helper()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		t.Errorf("%s is non-finite: %v", label, v)
	}
}

// TestInvalidViewBoxFallsBackToIdentity is a regression test for a
// call-site invariant this task is responsible for enforcing: viewBoxMatrix
// must never be reached with a viewBox parseViewBox did not accept, since it
// divides by the extent unchecked. Every case here — too few/many fields,
// non-numeric tokens, a non-positive extent, and (critically) a NaN extent,
// which passes both "<= 0" and "> 0" comparisons and so is NOT caught by a
// naive non-positive check alone — must fall back to rootM = Identity, log a
// line, and never let a NaN/Inf leak into Document.WidthPt/HeightPt.
func TestInvalidViewBoxFallsBackToIdentity(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	cases := []string{
		"0 0 0 100",       // zero width
		"0 0 -5 10",       // negative width
		"garbage",         // unparseable
		"",                // empty
		"0 0 100",         // too few fields
		"0 0 100 50 7",    // too many fields
		"NaN NaN NaN NaN", // all-NaN
		"0 0 NaN 100",     // NaN width specifically
		"0 0 100 Infinity",
	}
	for _, vb := range cases {
		src := fmt.Sprintf(`<svg %s width="100" height="100" viewBox=%q><rect width="10" height="10"/></svg>`, hdr, vb)
		var logged bool
		d, err := Parse([]byte(src), func(string, ...any) { logged = true })
		if err != nil {
			t.Fatalf("viewBox=%q: Parse error: %v", vb, err)
		}
		if !logged {
			t.Errorf("viewBox=%q: expected a log line, got none", vb)
		}
		rootM, _ := d.Root()
		if rootM.A != 1 || rootM.B != 0 || rootM.C != 0 || rootM.D != 1 || rootM.E != 0 || rootM.F != 0 {
			t.Errorf("viewBox=%q: rootM = %+v, want Identity", vb, rootM)
		}
		assertFinite(t, fmt.Sprintf("viewBox=%q: WidthPt", vb), d.WidthPt)
		assertFinite(t, fmt.Sprintf("viewBox=%q: HeightPt", vb), d.HeightPt)
	}
}

// TestDisplayNoneSkipsSubtree proves the tree walker never descends into a
// display:none element's children at all — it doesn't merely filter shapes
// after the fact. A child that sets display="inline" on itself must NOT
// re-appear (display is not inherited in the CSS sense here; the parent's
// display:none is enforced by the walker skipping the whole subtree), and a
// nested <g> with its own shape must likewise be dropped entirely.
func TestDisplayNoneSkipsSubtree(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="100" height="100">
	  <g display="none">
	    <rect width="10" height="10"/>
	    <rect display="inline" width="10" height="10"/>
	    <g><rect width="5" height="5"/></g>
	  </g>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	if len(root.Kids) != 0 {
		t.Errorf("root kids = %d, want 0 (display:none subtree must be fully skipped)", len(root.Kids))
	}
}

// TestUnknownElementRecursesAsContainer covers the "forgiving container"
// default: an unrecognized SVG-namespace element is logged once and its
// children still reach the scene, as a plain group with an Identity
// transform (unknown elements carry no transform semantics of their own).
func TestUnknownElementRecursesAsContainer(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	src := `<svg ` + hdr + ` width="100" height="100">
	  <someWeirdWrapper><rect width="10" height="10"/></someWeirdWrapper>
	</svg>`
	var logs []string
	d, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d, want 1 (unknown element recurses as a container)", len(root.Kids))
	}
	g, ok := root.Kids[0].(*Group)
	if !ok || len(g.Kids) != 1 {
		t.Fatalf("g = %#v, want a Group with 1 kid", root.Kids[0])
	}
	if _, ok := g.Kids[0].(*Shape); !ok {
		t.Errorf("g.Kids[0] = %#v, want *Shape", g.Kids[0])
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "<someWeirdWrapper>") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a log line naming <someWeirdWrapper>, got %v", logs)
	}
}

// TestGroupOpacityLoggedOnce covers the group-opacity observability fix: a
// <g opacity="..."> below 1 has no representation in the scene graph (Group
// carries only a transform, not a Style), so per-paint alpha through its
// children would be a plausible-but-wrong render rather than an honestly
// flat one. Since true compositing is deferred, the scene builder must at
// least say so — once per document, no matter how many such groups appear —
// while a group with opacity=1 (or no opacity attribute at all) is fully
// supported today and must NOT be logged.
func TestGroupOpacityLoggedOnce(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`

	opacityLogCount := func(logs []string) int {
		n := 0
		for _, l := range logs {
			if strings.Contains(l, "<g opacity>") {
				n++
			}
		}
		return n
	}

	t.Run("below 1 logs once regardless of occurrence count", func(t *testing.T) {
		src := `<svg ` + hdr + ` width="100" height="100">
		  <g opacity="0.5"><rect width="10" height="10"/></g>
		  <g opacity="0.25"><rect width="10" height="10"/></g>
		</svg>`
		var logs []string
		if _, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatal(err)
		}
		if n := opacityLogCount(logs); n != 1 {
			t.Errorf("group-opacity log count = %d, want 1 (once per document, not once per group)", n)
		}
	})

	t.Run("opacity=1 and absent opacity are not logged", func(t *testing.T) {
		src := `<svg ` + hdr + ` width="100" height="100">
		  <g opacity="1"><rect width="10" height="10"/></g>
		  <g><rect width="10" height="10"/></g>
		</svg>`
		var logs []string
		if _, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatal(err)
		}
		if n := opacityLogCount(logs); n != 0 {
			t.Errorf("group-opacity log count = %d, want 0 (guard is on <1, not on mere presence)", n)
		}
	})
}
