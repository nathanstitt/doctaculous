package svg

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"
)

const useHdr = `xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"`

// runWithTimeout runs fn and fails the test if it does not return within d —
// the mechanism for asserting a cycle guard actually terminates rather than
// hanging forever (a hang would otherwise show up only as an opaque overall
// test-suite timeout with no indication of which test caused it).
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not complete within %s (likely infinite recursion)", d)
	}
}

// TestUseStyleInheritance1 mirrors resvg-test-suite's
// structure/use/style-inheritance-1.svg: <use fill="red" href="#rect1">
// over <rect id="rect1" fill="green"/>. The target's OWN fill attribute must
// win over the use site's — GREEN, not red — because <use> only inherits
// into what the target leaves UNSET.
func TestUseStyleInheritance1(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
	  </defs>
	  <use id="use1" xlink:href="#rect1" fill="red"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("no Shape found in scene")
	}
	fp, ok := s.Style.FillPaint()
	if !ok {
		t.Fatal("FillPaint not ok")
	}
	want := color.RGBA{0, 128, 0, 255} // "green"
	if fp.Color != want {
		t.Errorf("fill = %#v, want green %#v (target's own fill must win)", fp.Color, want)
	}
}

// TestUseComplexStyleResolvingOrder mirrors resvg-test-suite's
// structure/use/complex-style-resolving-order.svg:
//
//	<use x="20" y="20" href="#rect1" fill="url(#lg1)" fill-opacity="0">
//	<rect id="rect1" width="160" height="160" fill-opacity="1"/>
//
// The gradient fill IS inherited from the use site (rect1 sets no fill of
// its own), but fill-opacity is NOT — rect1's own fill-opacity="1" wins over
// the use site's "0". This is the sharp case: two DIFFERENT properties on
// the same use site resolve with opposite winners depending on whether the
// target overrides them.
func TestUseComplexStyleResolvingOrder(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <linearGradient id="lg1">
	      <stop offset="0" stop-color="green" stop-opacity="0.7"/>
	    </linearGradient>
	    <rect id="rect1" width="160" height="160" fill-opacity="1"/>
	  </defs>
	  <use id="use1" x="20" y="20" xlink:href="#rect1" fill="url(#lg1)" fill-opacity="0"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("no Shape found in scene")
	}
	if s.FillGradient == nil {
		t.Error("FillGradient is nil, want the inherited url(#lg1) gradient to resolve")
	}
	if _, ok := s.Style.FillServer(); !ok {
		t.Error("FillServer not reported, want the inherited fill=url(#lg1) reference")
	}
	// fill-opacity must be rect1's OWN 1, not the use site's 0 — checked
	// directly on the unexported field since both files are package svg.
	if s.Style.fillOpacity != 1 {
		t.Errorf("fillOpacity = %v, want 1 (the target's own fill-opacity, not the use site's 0)", s.Style.fillOpacity)
	}
}

// TestUseTransformAttribute1 mirrors structure/use/transform-attribute-1.svg:
// a <use> with both an implied translate (none here) and its own
// transform="translate(20 20)" composes the transform onto the target.
// Verifies the Group.M composition order: translate(x,y) then the <use>'s
// own transform attribute.
func TestUseTransformAttribute1(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <rect id="rect1" width="160" height="160"/>
	  </defs>
	  <use id="use1" xlink:href="#rect1" fill="green" transform="translate(20 20)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	g, ok := root.Kids[0].(*Group)
	if !ok {
		t.Fatalf("root.Kids[0] = %#v, want *Group (the <use> instantiation)", root.Kids[0])
	}
	x, y := g.M.Apply(0, 0)
	if x != 20 || y != 20 {
		t.Errorf("use Group.M maps (0,0) -> (%v,%v), want (20,20)", x, y)
	}
}

// TestUseTransformAttribute2 mirrors structure/use/transform-attribute-2.svg:
// <use> targets a <rect> inside a <g transform="scale(4)">. The target's
// OWN ancestor transform (the <g>'s scale) must still apply via the ordinary
// buildNode(target, ...) recursion, composed under the <use>'s own
// translate(20,20) transform.
func TestUseTransformAttribute2(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <g id="g1" transform="scale(4)">
	      <rect id="rect1" width="160" height="160"/>
	    </g>
	  </defs>
	  <use id="use1" xlink:href="#rect1" fill="green" transform="translate(20 20)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	g, ok := root.Kids[0].(*Group)
	if !ok {
		t.Fatalf("root.Kids[0] = %#v, want *Group", root.Kids[0])
	}
	// <use> targets #rect1 directly (not #g1), so the <g>'s scale(4) is NOT
	// part of the instantiation: SVG's <use> only clones the referenced
	// element itself (and its OWN descendants), not its ancestor chain.
	x, y := g.M.Apply(0, 0)
	if x != 20 || y != 20 {
		t.Errorf("use Group.M maps (0,0) -> (%v,%v), want (20,20)", x, y)
	}
	s := findFirstShape(g)
	if s == nil {
		t.Fatal("no Shape found under the use Group")
	}
	// rect1 itself has no transform, so its Shape.M must be Identity —
	// the ancestor <g>'s scale must not leak in.
	sx, sy := s.M.Apply(1, 1)
	if sx != 1 || sy != 1 {
		t.Errorf("target Shape.M maps (1,1) -> (%v,%v), want (1,1) (ancestor <g> transform must not apply)", sx, sy)
	}
}

// TestUseSelfRecursiveTerminates mirrors structure/use/self-recursive.svg:
// <use id="use1" href="#use1"/>. Must terminate (not hang) and resolve to
// nothing.
func TestUseSelfRecursiveTerminates(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <use id="use1" xlink:href="#use1"/>
	</svg>`
	runWithTimeout(t, 5*time.Second, func() {
		doc, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, root := doc.Root()
		if len(root.Kids) != 0 {
			t.Errorf("root.Kids = %d, want 0 (self-recursive <use> resolves to nothing)", len(root.Kids))
		}
	})
}

// TestUseRecursiveHrefChainTerminates mirrors structure/use/recursive.svg:
// two <use>s referencing each other via href (use1 -> use2 -> use1). Must
// terminate.
func TestUseRecursiveHrefChainTerminates(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <use id="use1" xlink:href="#use2"/>
	  <use id="use2" xlink:href="#use1"/>
	</svg>`
	runWithTimeout(t, 5*time.Second, func() {
		doc, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, root := doc.Root()
		if len(root.Kids) != 0 {
			t.Errorf("root.Kids = %d, want 0 (mutually-recursive <use> pair resolves to nothing)", len(root.Kids))
		}
	})
}

// TestUseIndirectRecursiveTerminates mirrors
// structure/use/indirect-recursive-1.svg: a <g> contains a <use> targeting
// use2, and use2 targets the <g> itself — a cycle that runs through a
// non-<use> element. Must terminate.
func TestUseIndirectRecursiveTerminates(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <g id="g1">
	    <use id="use1" xlink:href="#use2"/>
	  </g>
	  <use id="use2" xlink:href="#g1"/>
	</svg>`
	runWithTimeout(t, 5*time.Second, func() {
		_, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestUseNestedRecursiveTreeWalkTerminates mirrors
// structure/use/nested-recursive-1.svg: <use id="use1" href="#use2"> whose
// DESCENDANT is <use id="use2" href="#use1"/>. This is the tree-recursion
// cycle shape the design calls out as NOT caught by followHrefChain alone
// (use2 is reached by tree walk, not by reading use1's href) — only
// buildingUse catches it. Must terminate.
func TestUseNestedRecursiveTreeWalkTerminates(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <use id="use1" xlink:href="#use2">
	    <use id="use2" xlink:href="#use1"/>
	  </use>
	</svg>`
	runWithTimeout(t, 5*time.Second, func() {
		_, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestUseTargetingOwnAncestorTerminates covers the second tree-recursion
// shape the design calls out explicitly: <g id="g1"><use href="#g1"/></g> —
// a <use> targeting its own DOM ancestor. Instantiating this naively would
// recurse forever (building g1 requires building its child <use>, which
// requires building g1 again, ...); must terminate via buildingUse.
func TestUseTargetingOwnAncestorTerminates(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <g id="g1">
	    <rect width="10" height="10" fill="green"/>
	    <use id="use1" xlink:href="#g1"/>
	  </g>
	</svg>`
	runWithTimeout(t, 5*time.Second, func() {
		doc, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, root := doc.Root()
		g, ok := root.Kids[0].(*Group)
		if !ok {
			t.Fatalf("root.Kids[0] = %#v, want *Group (<g id=g1>)", root.Kids[0])
		}
		// The <rect> sibling must still render; only the self-referencing
		// <use> resolves to nothing.
		if findFirstShape(g) == nil {
			t.Error("expected the <rect> sibling of the self-referencing <use> to still render")
		}
	})
}

// TestUseToDefsChildWorks verifies <use> to a <defs> child resolves (idx.ids
// includes defs descendants) by calling buildNode on the target directly,
// never routing through the defs skip that would otherwise make it
// contribute nothing.
func TestUseToDefsChildWorks(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <circle id="c1" cx="50" cy="50" r="20" fill="blue"/>
	  </defs>
	  <use xlink:href="#c1"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	if findFirstShape(root) == nil {
		t.Fatal("expected <use> to a <defs> child to render the referenced circle")
	}
}

// TestUseTargetOwnGradientStillResolves verifies a <use> target's OWN
// id-based references (here, a fill="url(#...)" gradient) still resolve
// correctly when reached through <use> — automatic since the resolvers
// live on sceneBuilder for the whole walk, but worth pinning down.
func TestUseTargetOwnGradientStillResolves(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 100 100">
	  <defs>
	    <linearGradient id="lg1">
	      <stop offset="0" stop-color="red"/>
	      <stop offset="1" stop-color="blue"/>
	    </linearGradient>
	    <rect id="r1" width="50" height="50" fill="url(#lg1)"/>
	  </defs>
	  <use xlink:href="#r1"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("no Shape found")
	}
	if s.FillGradient == nil {
		t.Error("FillGradient is nil, want the target's own gradient reference to resolve")
	}
}

// TestUseNoMemoization verifies two <use>s of the SAME target with
// different use-site fill colors produce genuinely DIFFERENT Shape.Style
// values (design decision 1: <use> is never memoized by target id, unlike
// clip/mask, because Shape.Style is by-value and per-use-site).
func TestUseNoMemoization(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <rect id="r1" width="10" height="10"/>
	  </defs>
	  <use xlink:href="#r1" fill="red"/>
	  <use xlink:href="#r1" fill="blue"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	if len(root.Kids) != 2 {
		t.Fatalf("root.Kids = %d, want 2", len(root.Kids))
	}
	s1 := findFirstShape(root.Kids[0])
	s2 := findFirstShape(root.Kids[1])
	if s1 == nil || s2 == nil {
		t.Fatal("expected both <use> instantiations to produce a Shape")
	}
	fp1, _ := s1.Style.FillPaint()
	fp2, _ := s2.Style.FillPaint()
	if fp1.Color == fp2.Color {
		t.Errorf("both use instantiations resolved to the same fill %#v; memoization would break this", fp1.Color)
	}
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	if fp1.Color != red || fp2.Color != blue {
		t.Errorf("fills = %#v, %#v, want red then blue", fp1.Color, fp2.Color)
	}
}

// TestSymbolSimpleCase mirrors structure/symbol/simple-case.svg: a plain
// <symbol> with no viewBox, instantiated via <use>, renders its content
// directly (viewBox mapping is Identity, viewport size defaults to the
// ambient viewport per <use>'s missing width/height).
func TestSymbolSimpleCase(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <symbol id="symbol1">
	    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
	  </symbol>
	  <use id="use1" xlink:href="#symbol1"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := findFirstShape(root)
	if s == nil {
		t.Fatal("expected the symbol's rect to render through <use>")
	}
	minX, minY, maxX, maxY, ok := s.Path.Bounds()
	if !ok || minX != 20 || minY != 20 || maxX != 180 || maxY != 180 {
		t.Errorf("rect bounds = (%v,%v,%v,%v), want (20,20,180,180)", minX, minY, maxX, maxY)
	}
}

// TestSymbolViewBoxMapping mirrors structure/symbol/with-viewBox.svg: a
// <symbol viewBox="-100 -100 400 400"> instantiated with no explicit
// width/height on <use> (so the viewport is the ambient 200x200) must map
// content through viewBoxMatrix, scaling it down by 0.5 (400 viewBox units
// -> 200 viewport units) and translating by +100,+100 to zero the viewBox
// origin.
func TestSymbolViewBoxMapping(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <symbol id="symbol1" viewBox="-100 -100 400 400">
	    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
	  </symbol>
	  <use id="use1" xlink:href="#symbol1"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	// root.Kids[0] is the <use> Group; its one kid is the symbol Group
	// carrying the viewBox matrix.
	useG, ok := root.Kids[0].(*Group)
	if !ok || len(useG.Kids) != 1 {
		t.Fatalf("root.Kids[0] = %#v, want a *Group with 1 kid", root.Kids[0])
	}
	symG, ok := useG.Kids[0].(*Group)
	if !ok {
		t.Fatalf("useG.Kids[0] = %#v, want *Group (the symbol instantiation)", useG.Kids[0])
	}
	// viewBoxMatrix(-100,-100,400,400 -> 200x200, xMidYMid meet) should map
	// the viewBox origin (-100,-100) to viewport (0,0).
	x, y := symG.M.Apply(-100, -100)
	if x != 0 || y != 0 {
		t.Errorf("symbol viewBox matrix maps (-100,-100) -> (%v,%v), want (0,0)", x, y)
	}
	// And scale by 0.5: viewBox width 400 -> viewport width 200.
	x2, _ := symG.M.Apply(300, -100) // viewBox's far edge (-100+400)
	if x2 != 200 {
		t.Errorf("symbol viewBox matrix maps x=300 -> %v, want 200 (0.5 scale)", x2)
	}
}

// TestSymbolCustomUseSize mirrors structure/symbol/with-custom-use-size.svg:
// <use width="100" height="100"> overrides the symbol's viewport size (no
// viewBox on the symbol here, so this only affects ViewportClip's extent,
// not any content scaling).
func TestSymbolCustomUseSize(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <symbol id="symbol1">
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
	if symG.ViewportClip == nil {
		t.Fatal("expected a ViewportClip (default overflow:hidden)")
	}
	minX, minY, maxX, maxY, ok := symG.ViewportClip.Bounds()
	if !ok || minX != 0 || minY != 0 || maxX != 100 || maxY != 100 {
		t.Errorf("ViewportClip bounds = (%v,%v,%v,%v), want (0,0,100,100)", minX, minY, maxX, maxY)
	}
}

// TestSymbolOverflowVisibleDisablesClip mirrors
// structure/symbol/with-overflow-visible.svg: overflow="visible" on the
// <symbol> disables the default viewport clip.
func TestSymbolOverflowVisibleDisablesClip(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <symbol id="symbol1" overflow="visible">
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
		t.Error("expected overflow:visible to disable the ViewportClip")
	}
}

// TestUnusedSymbolRendersNothingAndLogsNothing mirrors
// structure/symbol/unused-symbol.svg: a bare, never-<use>d <symbol> must
// contribute zero scene nodes AND must never trigger the "not yet
// supported" warnOnce path (symbol moved from unsupportedElements to
// skippedElements).
func TestUnusedSymbolRendersNothingAndLogsNothing(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
	  <symbol id="symbol1">
	    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
	  </symbol>
	  <rect id="frame" x="1" y="1" width="198" height="198" fill="none" stroke="black"/>
	</svg>`
	var logs []string
	doc, err := Parse([]byte(src), func(format string, args ...any) {
		logs = append(logs, format)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	if len(root.Kids) != 1 {
		t.Fatalf("root.Kids = %d, want 1 (only the frame rect; the symbol itself contributes nothing)", len(root.Kids))
	}
	for _, l := range logs {
		if l == "svg: <%s> not yet supported (skipped)" {
			t.Errorf("unexpected 'not yet supported' log for an unused <symbol>: %q", l)
		}
	}
}

// TestOpacityOnSymbolFixtures exercises the four opacity-on-*.svg symbol
// corpus fixtures' mechanics: opacity on <symbol> alone, <use> alone, both
// multiplicatively, and opacity on a <symbol> that also has a viewBox. Good
// regression value for PR 4's group compositing per the design's testing
// section.
func TestOpacityOnSymbolFixtures(t *testing.T) {
	cases := []struct {
		name       string
		symbolAttr string
		useAttr    string
		wantSymOp  float64
		wantUseOp  float64
	}{
		{"opacity-on-symbol", `opacity="0.5"`, "", 0.5, 1},
		{"opacity-on-use", "", `opacity="0.5"`, 1, 0.5},
		{"opacity-on-use-and-symbol", `opacity="0.5"`, `opacity="0.5"`, 0.5, 0.5},
		{"opacity-on-symbol-with-viewBox", `opacity="0.5" viewBox="-100 -100 400 400"`, "", 0.5, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
			  <symbol id="symbol1" ` + c.symbolAttr + `>
			    <rect id="rect1" x="20" y="20" width="160" height="160" fill="green"/>
			  </symbol>
			  <use id="use1" xlink:href="#symbol1" ` + c.useAttr + `/>
			</svg>`
			doc, err := Parse([]byte(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, root := doc.Root()
			useG, ok := root.Kids[0].(*Group)
			if !ok {
				t.Fatalf("root.Kids[0] = %#v, want *Group", root.Kids[0])
			}
			if useG.Opacity != c.wantUseOp {
				t.Errorf("use Group.Opacity = %v, want %v", useG.Opacity, c.wantUseOp)
			}
			symG, ok := useG.Kids[0].(*Group)
			if !ok {
				t.Fatalf("useG.Kids[0] = %#v, want *Group (symbol instantiation)", useG.Kids[0])
			}
			if symG.Opacity != c.wantSymOp {
				t.Errorf("symbol Group.Opacity = %v, want %v", symG.Opacity, c.wantSymOp)
			}
		})
	}
}

// TestUseWithinClipPath verifies the design's decision-5 restructuring:
// mirrors masking/clipPath/with-use-child.svg — a <use> inside a <clipPath>
// referencing a plain shape resolves to that shape's geometry.
func TestUseWithinClipPath(t *testing.T) {
	src := `<svg ` + useHdr + ` viewBox="0 0 200 200">
	  <defs>
	    <path id="path1" d="M 100 15 l 50 160 l -130 -100 l 160 0 l -130 100 z"/>
	    <clipPath id="clip1">
	      <use id="use1" xlink:href="#path1"/>
	    </clipPath>
	  </defs>
	  <rect id="rect1" x="0" y="0" width="200" height="200" fill="green" clip-path="url(#clip1)"/>
	</svg>`
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := doc.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath == nil {
		t.Fatal("clip-path did not resolve")
	}
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("ClipPath.Kids = %d, want 1 (the <use>'s resolved path target)", len(s.ClipPath.Kids))
	}
	if s.ClipPath.Kids[0].Path == nil || s.ClipPath.Kids[0].Path.Empty() {
		t.Error("resolved clip child has no geometry")
	}
}

// TestUseInvalidChildViaClipPathDropped mirrors
// masking/clipPath/with-invalid-child-via-use.svg and
// symbol-via-use-is-not-a-valid-child.svg: a <use> inside a <clipPath> whose
// target is a <g> (or a <symbol>) is invalid and must contribute NOTHING to
// the union — clipping the target to nothing, not "no clip".
func TestUseInvalidChildViaClipPathDropped(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			"g-target",
			`<svg ` + useHdr + ` viewBox="0 0 200 200">
			  <defs>
			    <g id="g1"><path id="path1" d="M 100 15 l 50 160 l -130 -100 l 160 0 l -130 100 z"/></g>
			    <clipPath id="clip1"><use id="use1" xlink:href="#g1"/></clipPath>
			  </defs>
			  <rect id="rect1" x="0" y="0" width="200" height="200" fill="red" clip-path="url(#clip1)"/>
			</svg>`,
		},
		{
			"symbol-target",
			`<svg ` + useHdr + ` viewBox="0 0 200 200">
			  <symbol id="symbol1"><rect id="rect1" x="20" y="20" width="160" height="160"/></symbol>
			  <clipPath id="clip1"><use id="use1" xlink:href="#symbol1"/></clipPath>
			  <rect id="rect2" x="0" y="0" width="200" height="200" fill="red" clip-path="url(#clip1)"/>
			</svg>`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Parse([]byte(c.src), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, root := doc.Root()
			s, ok := findLastShape(root)
			if !ok {
				t.Fatal("expected the clipped rect to be found")
			}
			if s.ClipPath == nil {
				t.Fatal("clip-path did not resolve")
			}
			if len(s.ClipPath.Kids) != 0 {
				t.Errorf("ClipPath.Kids = %d, want 0 (invalid <use> target contributes nothing)", len(s.ClipPath.Kids))
			}
		})
	}
}

// findLastShape returns the last *Shape found by a depth-first walk of n
// (root.Kids order), for tests where the interesting shape is not the
// first one in the scene.
func findLastShape(n Node) (*Shape, bool) {
	var last *Shape
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case *Shape:
			last = v
		case *Group:
			for _, kid := range v.Kids {
				walk(kid)
			}
		}
	}
	walk(n)
	return last, last != nil
}

// buildUseBomb generates the exponential-expansion <use> graph maxUseNodes
// guards against: n levels, each a <g> containing TWO <use>s of the level
// below it. Nothing here is cyclic and nothing exceeds maxUseDepth — every
// reference points strictly downward at a DISTINCT id — so neither
// buildingUse's cycle guard nor maxUseDepth sees anything wrong, yet the
// instantiated node count grows ~4x per level. Under 1.5 KB of markup
// reaches gigabytes by n=18 and hundreds of GB by n=24 without the budget.
func buildUseBomb(n int) []byte {
	var sb strings.Builder
	sb.WriteString(`<svg ` + useHdr + ` width="100" height="100"><defs>`)
	sb.WriteString(`<rect id="u0" width="1" height="1"/>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, `<g id="u%d"><use xlink:href="#u%d"/><use xlink:href="#u%d"/></g>`, i, i-1, i-1)
	}
	fmt.Fprintf(&sb, `</defs><use xlink:href="#u%d"/></svg>`, n)
	return []byte(sb.String())
}

// countSceneNodes returns the total number of Groups and Shapes reachable
// from n, i.e. how much scene graph the build actually materialized.
func countSceneNodes(n Node) int {
	switch v := n.(type) {
	case nil:
		return 0
	case *Group:
		if v == nil {
			return 0
		}
		c := 1
		for _, k := range v.Kids {
			c += countSceneNodes(k)
		}
		return c
	default:
		return 1
	}
}

// TestUseExpansionBombIsBounded is the regression test for the build-time
// exponential-expansion DoS: maxUseDepth bounds recursion DEPTH but not
// BREADTH, and breadth is where the cost is. The whole blowup happens inside
// Parse, so pkg/svg/draw's maxDrawCalls — a draw-time backstop — never gets
// the chance to fire.
//
// The assertion is deliberately on the guard's OBSERVABLE EFFECT (the
// warnOnceMsg line firing, and the materialized node count staying within
// the budget), never on wall-clock duration: a timing assertion here would
// be flaky on a loaded CI machine and would not actually prove the budget
// was what stopped the expansion.
//
// The n sweep is the real proof. Without the budget each extra level
// multiplies the work by ~4, so an unbounded build would blow up long before
// n=30; with it, every n produces the same bounded node count.
func TestUseExpansionBombIsBounded(t *testing.T) {
	for _, n := range []int{18, 24, 30} {
		t.Run(fmt.Sprintf("levels=%d", n), func(t *testing.T) {
			var logged bool
			logf := func(format string, args ...any) {
				if strings.Contains(fmt.Sprintf(format, args...), "instantiation budget exhausted") {
					logged = true
				}
			}
			doc, err := Parse(buildUseBomb(n), logf)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !logged {
				t.Error("expected the instantiation-budget degradation to be logged; it never fired")
			}
			_, root := doc.Root()
			// Each charged instantiation materializes a bounded, small
			// number of nodes (the <use>'s wrapper Group plus the target's
			// own content), so the scene stays within a small multiple of
			// the budget rather than growing with n.
			if got, limit := countSceneNodes(root), 4*maxUseNodes; got > limit {
				t.Errorf("materialized %d scene nodes, want <= %d: the budget did not bound the expansion", got, limit)
			}
		})
	}
}

// TestUseBudgetSurvivesAcrossSiblingSubtrees pins down that the budget is a
// whole-document total and not a per-subtree one. Each top-level <use> below
// is an independent subtree; a counter that unwound with the recursion (the
// useDepth/buildingUse shape) would be reset by every sibling and would let
// the bomb through, since the bomb's cost is spread across exactly such
// sibling instantiations.
func TestUseBudgetSurvivesAcrossSiblingSubtrees(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<svg ` + useHdr + ` width="100" height="100"><defs>`)
	sb.WriteString(`<rect id="u0" width="1" height="1"/>`)
	// A moderately deep chain, instantiated repeatedly as siblings. No single
	// sibling exhausts the budget; only their SUM does.
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&sb, `<g id="u%d"><use xlink:href="#u%d"/><use xlink:href="#u%d"/></g>`, i, i-1, i-1)
	}
	sb.WriteString(`</defs>`)
	for i := 0; i < 64; i++ {
		sb.WriteString(`<use xlink:href="#u12"/>`)
	}
	sb.WriteString(`</svg>`)

	var logged bool
	logf := func(format string, args ...any) {
		if strings.Contains(fmt.Sprintf(format, args...), "instantiation budget exhausted") {
			logged = true
		}
	}
	doc, err := Parse([]byte(sb.String()), logf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !logged {
		t.Error("budget never fired across sibling subtrees: it is being reset per subtree, which would let the expansion bomb through")
	}
	_, root := doc.Root()
	if got, limit := countSceneNodes(root), 4*maxUseNodes; got > limit {
		t.Errorf("materialized %d scene nodes, want <= %d", got, limit)
	}
}

// TestLegitimateSpriteSheetRendersCompletely is the other half of the
// budget's contract: it must never silently truncate a valid document. This
// models the common heavy-<use> case — an icon sprite sheet defining many
// <symbol>s and instantiating each — at a scale comfortably under
// maxUseNodes, and requires that EVERY instantiation produced its shape and
// that no degradation was logged.
func TestLegitimateSpriteSheetRendersCompletely(t *testing.T) {
	const (
		symbols  = 100
		perIcon  = 20
		wantUses = symbols * perIcon
	)
	var sb strings.Builder
	sb.WriteString(`<svg ` + useHdr + ` width="1000" height="1000"><defs>`)
	for i := 0; i < symbols; i++ {
		fmt.Fprintf(&sb, `<symbol id="icon%d" viewBox="0 0 10 10"><rect width="10" height="10" fill="green"/></symbol>`, i)
	}
	sb.WriteString(`</defs>`)
	for i := 0; i < symbols; i++ {
		for j := 0; j < perIcon; j++ {
			fmt.Fprintf(&sb, `<use xlink:href="#icon%d" x="%d" y="%d" width="10" height="10"/>`, i, j*10, i*10)
		}
	}
	sb.WriteString(`</svg>`)

	var degraded []string
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if strings.Contains(msg, "budget") || strings.Contains(msg, "unresolved") {
			degraded = append(degraded, msg)
		}
	}
	doc, err := Parse([]byte(sb.String()), logf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(degraded) != 0 {
		t.Errorf("a legitimate %d-instantiation sprite sheet was degraded: %v", wantUses, degraded)
	}

	// Every <use> must have produced a real, painted <rect>.
	var shapes int
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case *Group:
			if v == nil {
				return
			}
			for _, k := range v.Kids {
				walk(k)
			}
		case *Shape:
			shapes++
		}
	}
	_, root := doc.Root()
	walk(root)
	if shapes != wantUses {
		t.Errorf("sprite sheet rendered %d shapes, want %d: the budget truncated a legitimate document", shapes, wantUses)
	}
}
