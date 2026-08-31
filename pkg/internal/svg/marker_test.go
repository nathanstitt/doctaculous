package svg

import (
	"strings"
	"testing"
)

const markerHdr = `xmlns="http://www.w3.org/2000/svg"`

// firstShape returns the first *Shape found in a depth-first walk of g,
// which for these fixtures is always the marked path.
func firstShape(g *Group) *Shape {
	for _, k := range g.Kids {
		switch n := k.(type) {
		case *Shape:
			return n
		case *Group:
			if s := firstShape(n); s != nil {
				return s
			}
		}
	}
	return nil
}

// TestMarkerShorthandBeatsLonghandPresentationAttribute pins the CSS cascade
// order between the "marker" shorthand and its longhands, in the direction
// the pre-fix code got backwards. An inline style="" declaration is author
// origin; a marker-start="..." XML attribute is only a presentational hint,
// which every author declaration outranks. So the shorthand in style="" must
// win over the longhand attribute, even though the shorthand "expands to"
// the longhand.
//
// The pre-fix code applied the shorthand BEFORE the longhands inside
// Style.apply, which made a longhand beat a shorthand unconditionally,
// regardless of origin.
func TestMarkerShorthandBeatsLonghandPresentationAttribute(t *testing.T) {
	src := `<svg ` + markerHdr + ` viewBox="0 0 200 200">
	  <marker id="a" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="red"/></marker>
	  <marker id="b" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="lime"/></marker>
	  <path id="p" d="M 10 10 L 90 90" stroke="black"
	        style="marker:url(#a)" marker-start="url(#b)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := firstShape(root)
	if s == nil {
		t.Fatal("no Shape in scene")
	}
	ref, ok := s.Style.MarkerStartRef()
	if !ok {
		t.Fatal("no marker-start resolved")
	}
	if !strings.Contains(ref, "#a") {
		t.Errorf("marker-start = %q, want url(#a): an inline-style shorthand outranks a presentation-attribute longhand", ref)
	}
}

// TestMarkerShorthandBeatsEarlierLonghandInSameStyle pins the OTHER
// direction the pre-fix code got backwards: within one style="" attribute,
// both declarations are the same origin and specificity, so plain CSS source
// order decides — the LATER one wins. Here the shorthand is written last, so
// it must override the longhand written before it.
func TestMarkerShorthandBeatsEarlierLonghandInSameStyle(t *testing.T) {
	src := `<svg ` + markerHdr + ` viewBox="0 0 200 200">
	  <marker id="a" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="red"/></marker>
	  <marker id="b" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="lime"/></marker>
	  <path id="p" d="M 10 10 L 90 90" stroke="black"
	        style="marker-start:url(#b); marker:url(#a)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := firstShape(root)
	if s == nil {
		t.Fatal("no Shape in scene")
	}
	ref, ok := s.Style.MarkerStartRef()
	if !ok {
		t.Fatal("no marker-start resolved")
	}
	if !strings.Contains(ref, "#a") {
		t.Errorf("marker-start = %q, want url(#a): the shorthand is later in source order", ref)
	}
}

// TestMarkerLonghandBeatsEarlierShorthand is the control for the two tests
// above: expansion must not become substitution. A longhand written AFTER
// the shorthand still wins, because it simply overwrites the longhand key
// the shorthand set.
func TestMarkerLonghandBeatsEarlierShorthand(t *testing.T) {
	src := `<svg ` + markerHdr + ` viewBox="0 0 200 200">
	  <marker id="a" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="red"/></marker>
	  <marker id="b" markerWidth="10" markerHeight="10"><rect width="4" height="4" fill="lime"/></marker>
	  <path id="p" d="M 10 10 L 90 90" stroke="black"
	        style="marker:url(#a); marker-start:url(#b)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := firstShape(root)
	if s == nil {
		t.Fatal("no Shape in scene")
	}
	ref, ok := s.Style.MarkerStartRef()
	if !ok {
		t.Fatal("no marker-start resolved")
	}
	if !strings.Contains(ref, "#b") {
		t.Errorf("marker-start = %q, want url(#b): the longhand is later in source order", ref)
	}
	// The shorthand's other two longhands are untouched by the later
	// marker-start, so they keep the shorthand's value.
	if mid, ok := s.Style.MarkerMidRef(); !ok || !strings.Contains(mid, "#a") {
		t.Errorf("marker-mid = %q (ok=%v), want url(#a) from the shorthand", mid, ok)
	}
}

// TestMarkerOverflowVisibleViaStyle covers the marker half of the
// wantsViewportClip fix: overflow is resolved through the CASCADE, so
// style="overflow:visible" on a <marker> disables the default viewport clip
// exactly like the presentation attribute does. Reading the raw attribute
// (the pre-fix behavior) silently ignored this.
func TestMarkerOverflowVisibleViaStyle(t *testing.T) {
	for _, tc := range []struct {
		name      string
		markerAtt string
		wantClip  bool
	}{
		{"default (no overflow)", "", true},
		{"presentation attribute", `overflow="visible"`, false},
		{"inline style", `style="overflow:visible"`, false},
		{"inline style, padded and mixed case", `style="overflow:  VISIBLE  "`, false},
		{"explicit hidden", `overflow="hidden"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `<svg ` + markerHdr + ` viewBox="0 0 200 200">
			  <marker id="m" markerWidth="10" markerHeight="10" ` + tc.markerAtt + `>
			    <rect width="4" height="4" fill="red"/>
			  </marker>
			  <path id="p" d="M 10 10 L 90 90" stroke="black" marker-start="url(#m)"/>
			</svg>`
			doc, err := Parse([]byte(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, root := doc.Root()
			s := firstShape(root)
			if s == nil || s.MarkerStart == nil {
				t.Fatal("no marker resolved")
			}
			if got := s.MarkerStart.ClipToViewport; got != tc.wantClip {
				t.Errorf("ClipToViewport = %v, want %v", got, tc.wantClip)
			}
		})
	}
}

// TestSymbolOverflowVisibleViaStyle is the <symbol> half of the same fix:
// wantsViewportClip is shared, so style="overflow:visible" must work there
// too, not only the presentation attribute the existing
// TestSymbolOverflowVisibleDisablesClip covers.
func TestSymbolOverflowVisibleViaStyle(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <symbol id="symbol1" style="overflow:visible">
	    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
	  </symbol>
	  <use id="use1" xlink:href="#symbol1" width="100" height="100"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	useG := root.Kids[0].(*Group)
	symG := useG.Kids[0].(*Group)
	if symG.ViewportClip != nil {
		t.Error("expected style=\"overflow:visible\" to disable the ViewportClip")
	}
}

// TestMarkerOwnStyleInheritsToContent covers the <marker fill="blue"> case:
// a <marker>'s own presentation attributes and style cascade onto its
// children, so an unstyled child path picks up the marker's fill. Passing
// only the ancestors-only inherited style (the pre-fix behavior) left the
// child with the document default (black).
func TestMarkerOwnStyleInheritsToContent(t *testing.T) {
	src := `<svg ` + markerHdr + ` viewBox="0 0 200 200">
	  <marker id="m" markerWidth="10" markerHeight="10" fill="blue">
	    <path id="kid" d="M 0 0 L 4 0 L 4 4 Z"/>
	  </marker>
	  <path id="p" d="M 10 10 L 90 90" stroke="black" marker-start="url(#m)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := firstShape(root)
	if s == nil || s.MarkerStart == nil {
		t.Fatal("no marker resolved")
	}
	kid := firstShape(s.MarkerStart.Kids)
	if kid == nil {
		t.Fatal("marker has no content shape")
	}
	fp, ok := kid.Style.FillPaint()
	if !ok {
		t.Fatal("marker content has no fill")
	}
	if c := fp.Color; c.R != 0 || c.G != 0 || c.B != 255 {
		t.Errorf("marker content fill = %v, want blue (0,0,255) inherited from <marker fill=\"blue\">", c)
	}
}

// TestMarkerChainDepthGuardIsReachable pins that maxMarkerChainDepth is a
// real, reachable guard rather than dead code. A chain of DISTINCT marker
// ids, each drawing a shape that references the next, is acyclic — so
// buildingMarker never fires — and only the depth counter can stop it. The
// chain is built longer than maxMarkerChainDepth; the test asserts the parse
// terminates and that the markers past the cap resolve to nothing.
//
// Before the fix, resolveMarker took a `depth` parameter that only ever
// received 0 (nested markers re-enter through the ordinary scene walk, which
// had no depth to thread), so this guard could never fire at all.
func TestMarkerChainDepthGuardIsReachable(t *testing.T) {
	const n = maxMarkerChainDepth + 10
	var b strings.Builder
	b.WriteString(`<svg ` + markerHdr + ` viewBox="0 0 200 200">`)
	for i := 0; i < n; i++ {
		b.WriteString(`<marker id="m`)
		b.WriteString(itoa(i))
		b.WriteString(`" markerWidth="10" markerHeight="10">`)
		b.WriteString(`<path d="M 0 0 L 4 4"`)
		if i+1 < n {
			b.WriteString(` marker-start="url(#m` + itoa(i+1) + `)"`)
		}
		b.WriteString(`/></marker>`)
	}
	b.WriteString(`<path id="p" d="M 10 10 L 90 90" stroke="black" marker-start="url(#m0)"/>`)
	b.WriteString(`</svg>`)

	doc, err := Parse([]byte(b.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := firstShape(root)
	if s == nil || s.MarkerStart == nil {
		t.Fatal("no marker resolved")
	}

	// Walk the resolved chain and count how deep it actually goes. It must
	// stop at the cap rather than running the full n links.
	depth := 0
	cur := s.MarkerStart
	for cur != nil {
		depth++
		kid := firstShape(cur.Kids)
		if kid == nil {
			break
		}
		cur = kid.MarkerStart
	}
	if depth > maxMarkerChainDepth {
		t.Errorf("resolved chain depth = %d, want it capped at %d", depth, maxMarkerChainDepth)
	}
	if depth < 2 {
		t.Errorf("resolved chain depth = %d, want the guard to bite only after a real chain formed", depth)
	}
}
