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
	// unsupported element logged once (per element NAME, not per occurrence).
	src := `<svg ` + hdr + ` width="100" height="100">
	  <defs><rect id="d" width="5" height="5"/></defs>
	  <g fill="red" transform="translate(10,0)"><rect width="20" height="20"/></g>
	  <image href="a.png"/><image href="b.png"/>
	</svg>`
	var logs []string
	d, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d, want 1 (defs and image skipped)", len(root.Kids))
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
	imageLogs := 0
	for _, l := range logs {
		if strings.Contains(l, "<image>") {
			imageLogs++
		}
	}
	if imageLogs != 1 {
		t.Errorf("image logged %d times, want once per element name", imageLogs)
	}
}

// TestPaintServerElementsSkipped asserts the scene walk contributes ZERO
// nodes for linearGradient/radialGradient/pattern/stop, even though they
// are now fully supported (resolved out-of-band through the document
// index, not by appearing as scene nodes). It asserts kid COUNTS, not just
// pixels: an empty Group would silently change the tree shape while still
// painting nothing, which a pixel-only golden comparison would not catch.
// It also asserts these elements never produce a "not yet supported" log
// line, since they are no longer in unsupportedElements.
func TestPaintServerElementsSkipped(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`
	// linearGradient/radialGradient/pattern are placed as DIRECT children of
	// <svg> (valid SVG; not nested inside <defs>) so the scene walk's
	// buildNode actually dispatches on them itself — a <defs>-wrapped
	// placement would be skipped one level up and wouldn't exercise the
	// buildNode case this test targets. The pattern's <rect> tile child is
	// the trap: a forgiving-container fallthrough would paint it directly
	// into the visible scene at document coordinates.
	src := `<svg ` + hdr + ` width="100" height="100">
	  <linearGradient id="g1"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>
	  <radialGradient id="g2"><stop offset="0" stop-color="green"/></radialGradient>
	  <pattern id="p1" width="10" height="10"><rect width="10" height="10" fill="yellow"/></pattern>
	  <rect width="20" height="20" fill="url(#g1)"/>
	</svg>`

	var logs []string
	d, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()

	// The only scene contribution is the <rect> that references the
	// (unresolved-here) gradient. The linearGradient/radialGradient/pattern
	// elements — and the pattern's <rect> tile, and the gradients' <stop>
	// children — must contribute NOTHING: no extra Shape or Group.
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d, want 1 (only the referencing <rect>; paint-server subtrees contribute nothing)", len(root.Kids))
	}
	if _, ok := root.Kids[0].(*Shape); !ok {
		t.Fatalf("root.Kids[0] = %#v, want *Shape", root.Kids[0])
	}

	for _, l := range logs {
		if strings.Contains(l, "not yet supported") {
			t.Errorf("unexpected 'not yet supported' log for a paint-server element: %q", l)
		}
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

// TestGroupOpacity covers the scene-graph half of group opacity: a <g
// opacity="..."> must set Group.Opacity to the (clamped) resolved value, no
// warning logged, so pkg/svg/draw has something to composite. Compositing
// itself (BeginGroup/EndGroup) is covered by pkg/svg/draw's tests.
func TestGroupOpacity(t *testing.T) {
	const hdr = `xmlns="http://www.w3.org/2000/svg"`

	opacityOf := func(t *testing.T, src string, index int) float64 {
		t.Helper()
		var logs []string
		doc, err := Parse([]byte(src), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range logs {
			if strings.Contains(l, "opacity") {
				t.Errorf("unexpected opacity-related log line: %q (group opacity must not warn anymore)", l)
			}
		}
		_, root := doc.Root()
		g, ok := root.Kids[index].(*Group)
		if !ok {
			t.Fatalf("kid[%d] = %#v, want *Group", index, root.Kids[index])
		}
		return g.Opacity
	}

	t.Run("below 1 is carried on the Group", func(t *testing.T) {
		src := `<svg ` + hdr + ` width="100" height="100">
		  <g opacity="0.5"><rect width="10" height="10"/></g>
		  <g opacity="0.25"><rect width="10" height="10"/></g>
		</svg>`
		if got := opacityOf(t, src, 0); got != 0.5 {
			t.Errorf("group[0].Opacity = %v, want 0.5", got)
		}
		if got := opacityOf(t, src, 1); got != 0.25 {
			t.Errorf("group[1].Opacity = %v, want 0.25", got)
		}
	})

	t.Run("opacity=1 and absent opacity default to 1", func(t *testing.T) {
		src := `<svg ` + hdr + ` width="100" height="100">
		  <g opacity="1"><rect width="10" height="10"/></g>
		  <g><rect width="10" height="10"/></g>
		</svg>`
		if got := opacityOf(t, src, 0); got != 1 {
			t.Errorf("group[0].Opacity = %v, want 1", got)
		}
		if got := opacityOf(t, src, 1); got != 1 {
			t.Errorf("group[1].Opacity = %v, want 1", got)
		}
	})

	t.Run("clamped to [0,1]", func(t *testing.T) {
		src := `<svg ` + hdr + ` width="100" height="100">
		  <g opacity="5"><rect width="10" height="10"/></g>
		  <g opacity="-1"><rect width="10" height="10"/></g>
		</svg>`
		if got := opacityOf(t, src, 0); got != 1 {
			t.Errorf("group[0].Opacity = %v, want 1 (clamped)", got)
		}
		if got := opacityOf(t, src, 1); got != 0 {
			t.Errorf("group[1].Opacity = %v, want 0 (clamped)", got)
		}
	})
}

// TestRootOpacity covers root-<svg>-level opacity: <svg opacity="..."> is
// legal SVG and, before Group gained an Opacity field, was unreachable.
func TestRootOpacity(t *testing.T) {
	t.Run("root opacity is carried on the root Group", func(t *testing.T) {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" opacity="0.5">
		  <rect width="10" height="10"/>
		</svg>`
		doc, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, root := doc.Root()
		if root.Opacity != 0.5 {
			t.Errorf("root.Opacity = %v, want 0.5", root.Opacity)
		}
	})

	t.Run("absent root opacity defaults to 1", func(t *testing.T) {
		src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
		  <rect width="10" height="10"/>
		</svg>`
		doc, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, root := doc.Root()
		if root.Opacity != 1 {
			t.Errorf("root.Opacity = %v, want 1", root.Opacity)
		}
	})
}

func TestStylesheetReachesTheScene(t *testing.T) {
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	src := []byte(`<svg ` + ns + ` width="20" height="20">
	  <style>.hot { fill: #00ff00 } rect { stroke-width: 3 }</style>
	  <rect class="hot" width="10" height="10" fill="red" stroke="blue"/>
	</svg>`)
	doc, err := Parse(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root kids = %d (the <style> element must not paint)", len(root.Kids))
	}
	sh, ok := root.Kids[0].(*Shape)
	if !ok {
		t.Fatalf("kid = %#v", root.Kids[0])
	}
	fp, okf := sh.Style.FillPaint()
	if !okf || fp.Color != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("fill = %+v, want the stylesheet's green over the attribute's red", fp.Color)
	}
	sp, oks := sh.Style.StrokePaint()
	if !oks || sp.Width != 3 {
		t.Errorf("stroke width = %v, want 3 from the sheet", sp.Width)
	}
	if sp.Color != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("stroke color = %+v, want the attribute's blue (sheet did not set it)", sp.Color)
	}
}

func TestStyleElementNoLongerLogsUnsupported(t *testing.T) {
	var logs []string
	_, err := Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><style>rect{fill:red}</style><rect width="1" height="1"/></svg>`),
		func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if strings.Contains(l, "<style>") && strings.Contains(l, "not yet supported") {
			t.Errorf("style is supported now but still logs unsupported: %q", l)
		}
	}
}
