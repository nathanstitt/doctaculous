package svg

import (
	"fmt"
	"testing"
	"time"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

const clipHdr = `xmlns="http://www.w3.org/2000/svg"`

func TestClipPathResolvesOnShape(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1"><circle cx="50" cy="50" r="20"/></clipPath>
	  <rect id="r1" width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s, ok := root.Kids[0].(*Shape)
	if !ok {
		t.Fatalf("root.Kids[0] = %#v, want *Shape", root.Kids[0])
	}
	if s.ClipPath == nil {
		t.Fatal("shape's clip-path did not resolve")
	}
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("clipPath kids = %d, want 1", len(s.ClipPath.Kids))
	}
}

func TestClipPathNoneAndInvalidMeanNoClip(t *testing.T) {
	cases := []string{
		`clip-path="none"`,
		`clip-path="url(#missing)"`,
		`clip-path="not-a-funciri"`,
	}
	for _, attr := range cases {
		src := fmt.Sprintf(`<svg %s width="100" height="100"><rect width="10" height="10" %s/></svg>`, clipHdr, attr)
		d, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatalf("%s: %v", attr, err)
		}
		_, root := d.Root()
		s := root.Kids[0].(*Shape)
		if s.ClipPath != nil {
			t.Errorf("%s: ClipPath = %#v, want nil", attr, s.ClipPath)
		}
	}
}

func TestClipPathEmptyClipsToNothing(t *testing.T) {
	// An empty <clipPath> (no children at all) must resolve to a non-nil
	// ClipPath with zero Kids -- this is "clip to nothing", not "no clip":
	// the caller (pkg/svg/draw) must be able to distinguish it from the
	// clip-path attribute being absent/invalid (ClipPath == nil).
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="empty"></clipPath>
	  <rect width="100" height="100" clip-path="url(#empty)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath == nil {
		t.Fatal("ClipPath = nil, want a resolved (but empty) *ClipPath")
	}
	if len(s.ClipPath.Kids) != 0 {
		t.Errorf("ClipPath.Kids = %d, want 0", len(s.ClipPath.Kids))
	}
}

func TestClipPathInvalidChildIgnored(t *testing.T) {
	// A <g> child (and other invalid kinds) inside a <clipPath> must be
	// dropped, not recursed into as a forgiving container -- unlike the
	// ordinary scene walk's unknown-element default.
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <g><rect width="10" height="10"/></g>
	    <image width="10" height="10"/>
	    <switch><rect width="10" height="10"/></switch>
	    <circle cx="10" cy="10" r="5"/>
	  </clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath == nil {
		t.Fatal("ClipPath did not resolve")
	}
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("ClipPath.Kids = %d, want 1 (only the <circle>; <g>/<image>/<switch> must be dropped)", len(s.ClipPath.Kids))
	}
}

func TestClipPathDisplayNoneChildRemoved(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <circle cx="10" cy="10" r="5" display="none"/>
	    <circle cx="20" cy="20" r="5"/>
	  </clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("ClipPath.Kids = %d, want 1 (display:none child removed)", len(s.ClipPath.Kids))
	}
}

func TestClipPathVisibilityHiddenChildDropped(t *testing.T) {
	// visibility:hidden behaves the SAME as display:none on a clipPath
	// child: per SVG 1.1 section 14.3.5 and SVG2's clipPath rendering
	// model, a child that is not rendered — for either reason — does not
	// contribute to the union. Verified against the resvg corpus's
	// invisible-child-1 reference render (a visibility:hidden-only child
	// clips its target to nothing, i.e. the same as invisible-child-2's
	// display:none case), not just a spec reading.
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <circle cx="10" cy="10" r="5" visibility="hidden"/>
	    <circle cx="20" cy="20" r="5"/>
	  </clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("ClipPath.Kids = %d, want 1 (visibility:hidden child must be dropped)", len(s.ClipPath.Kids))
	}
}

func TestClipRuleInheritedFromParent(t *testing.T) {
	// clip-rule is inherited: a clipPath child with no own clip-rule takes
	// its ancestor's (even outside the <clipPath> subtree, since Style.apply
	// resolves it via the ordinary cascade).
	src := `<svg ` + clipHdr + ` width="100" height="100" clip-rule="evenodd">
	  <clipPath id="c1"><circle cx="10" cy="10" r="5"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if got := s.ClipPath.Kids[0].Rule; got != render.EvenOdd {
		t.Errorf("inherited clip-rule = %v, want EvenOdd", got)
	}
}

func TestClipRulePerChildOverride(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1">
	    <circle cx="10" cy="10" r="5" clip-rule="evenodd"/>
	    <circle cx="20" cy="20" r="5"/>
	  </clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	kids := root.Kids[0].(*Shape).ClipPath.Kids
	if kids[0].Rule != render.EvenOdd {
		t.Errorf("child 0 rule = %v, want EvenOdd", kids[0].Rule)
	}
	if kids[1].Rule != render.NonZero {
		t.Errorf("child 1 rule = %v, want NonZero (default)", kids[1].Rule)
	}
}

func TestClipPathIgnoresFillStrokeOpacity(t *testing.T) {
	// fill/stroke/opacity on a clipPath child must have NO effect: they are
	// simply not read anywhere in ClipPathChild, so this is really a
	// structural assertion that resolution still succeeds and produces a
	// path regardless of these attributes being present.
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1"><circle cx="10" cy="10" r="5" fill="none" stroke="red" opacity="0.5"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if len(s.ClipPath.Kids) != 1 || s.ClipPath.Kids[0].Path == nil {
		t.Fatalf("clip child with fill/stroke/opacity did not resolve: %#v", s.ClipPath)
	}
}

func TestClipPathUnitsObjectBoundingBox(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1" clipPathUnits="objectBoundingBox"><rect width="1" height="1"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath.Units != "objectBoundingBox" {
		t.Errorf("Units = %q, want objectBoundingBox", s.ClipPath.Units)
	}
}

func TestClipPathUnitsDefaultsToUserSpaceOnUse(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1"><rect width="10" height="10"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath.Units != "userSpaceOnUse" {
		t.Errorf("Units = %q, want userSpaceOnUse (the default)", s.ClipPath.Units)
	}
}

func TestClipPathSelfReferenceTerminates(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1" clip-path="url(#c1)"><circle cx="10" cy="10" r="5"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	done := make(chan struct{})
	var d *Document
	var err error
	go func() {
		d, err = Parse([]byte(src), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Parse did not terminate on a self-referencing clipPath")
	}
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath == nil {
		t.Fatal("ClipPath did not resolve at all")
	}
	// The cycle breaks Self resolving to itself; the union's own children
	// still resolve normally.
	if len(s.ClipPath.Kids) != 1 {
		t.Errorf("ClipPath.Kids = %d, want 1", len(s.ClipPath.Kids))
	}
	if s.ClipPath.Self != nil {
		t.Errorf("Self = %#v, want nil (self-reference must not resolve)", s.ClipPath.Self)
	}
}

func TestClipPathMutualCycleTerminates(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1" clip-path="url(#c2)"><circle cx="10" cy="10" r="5"/></clipPath>
	  <clipPath id="c2" clip-path="url(#c1)"><circle cx="20" cy="20" r="5"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#c1)"/>
	</svg>`
	done := make(chan struct{})
	var d *Document
	var err error
	go func() {
		d, err = Parse([]byte(src), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Parse did not terminate on a mutually-cyclic clipPath reference")
	}
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath == nil || len(s.ClipPath.Kids) != 1 {
		t.Fatalf("ClipPath = %#v", s.ClipPath)
	}
}

func TestClipPathChildOwnClipPathIntersects(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="inner"><circle cx="10" cy="10" r="5"/></clipPath>
	  <clipPath id="outer"><rect id="child" width="50" height="50" clip-path="url(#inner)"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#outer)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if len(s.ClipPath.Kids) != 1 {
		t.Fatalf("Kids = %d, want 1", len(s.ClipPath.Kids))
	}
	if s.ClipPath.Kids[0].Self == nil {
		t.Fatal("child's own clip-path did not resolve onto ClipPathChild.Self")
	}
}

func TestClipPathOnClipPathElementIntersectsUnion(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="inner"><circle cx="10" cy="10" r="5"/></clipPath>
	  <clipPath id="outer" clip-path="url(#inner)"><rect width="50" height="50"/></clipPath>
	  <rect width="100" height="100" clip-path="url(#outer)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath.Self == nil {
		t.Fatal("clipPath element's own clip-path did not resolve onto ClipPath.Self")
	}
}

func TestClipPathOnGroupResolves(t *testing.T) {
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <clipPath id="c1"><circle cx="10" cy="10" r="5"/></clipPath>
	  <g clip-path="url(#c1)"><rect width="10" height="10"/></g>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	g := root.Kids[0].(*Group)
	if g.ClipPath == nil {
		t.Fatal("group's clip-path did not resolve")
	}
}

func TestClipPathNonClipPathTargetIgnored(t *testing.T) {
	// A clip-path referencing something other than a <clipPath> element
	// (e.g. a <rect>) must not resolve.
	src := `<svg ` + clipHdr + ` width="100" height="100">
	  <rect id="notAClipPath" width="10" height="10"/>
	  <rect width="100" height="100" clip-path="url(#notAClipPath)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.ClipPath != nil {
		t.Errorf("ClipPath = %#v, want nil", s.ClipPath)
	}
}
