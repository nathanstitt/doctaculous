package svg

import (
	"fmt"
	"testing"
	"time"
)

const maskHdr = `xmlns="http://www.w3.org/2000/svg"`

func TestMaskResolvesOnShape(t *testing.T) {
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1"><circle cx="50" cy="50" r="20" fill="white"/></mask>
	  <rect id="r1" width="100" height="100" mask="url(#m1)"/>
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
	if s.Mask == nil {
		t.Fatal("shape's mask did not resolve")
	}
	if s.Mask.Kids == nil || len(s.Mask.Kids.Kids) != 1 {
		t.Fatalf("Mask.Kids = %#v, want 1 child", s.Mask.Kids)
	}
}

func TestMaskNoneAndInvalidMeanNoMask(t *testing.T) {
	cases := []string{
		`mask="none"`,
		`mask="url(#missing)"`,
		`mask="not-a-funciri"`,
	}
	for _, attr := range cases {
		src := fmt.Sprintf(`<svg %s width="100" height="100"><rect width="10" height="10" %s/></svg>`, maskHdr, attr)
		d, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatalf("%s: %v", attr, err)
		}
		_, root := d.Root()
		s := root.Kids[0].(*Shape)
		if s.Mask != nil {
			t.Errorf("%s: Mask = %#v, want nil", attr, s.Mask)
		}
	}
}

func TestMaskEmptyResolvesWithNoKids(t *testing.T) {
	// An empty <mask> (no children at all) must still resolve to a non-nil
	// *Mask with zero content -- this is "fully transparent", not "no
	// mask" -- the caller (pkg/svg/draw) must distinguish it from the mask
	// attribute being absent/invalid (Mask == nil).
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="empty"></mask>
	  <rect width="100" height="100" mask="url(#empty)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.Mask == nil {
		t.Fatal("Mask = nil, want a resolved (but empty) *Mask")
	}
	if s.Mask.Kids == nil {
		t.Fatal("Mask.Kids = nil, want a Group (even if empty)")
	}
	if len(s.Mask.Kids.Kids) != 0 {
		t.Errorf("Mask.Kids.Kids = %d, want 0", len(s.Mask.Kids.Kids))
	}
}

func TestMaskDefaultUnits(t *testing.T) {
	// maskUnits defaults to objectBoundingBox; maskContentUnits defaults to
	// userSpaceOnUse -- the OPPOSITE default, a classic point of confusion
	// the design doc calls out explicitly.
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1"><rect width="10" height="10"/></mask>
	  <rect width="100" height="100" mask="url(#m1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.Mask.Units != "objectBoundingBox" {
		t.Errorf("Units = %q, want objectBoundingBox (default)", s.Mask.Units)
	}
	if s.Mask.ContentUnits != "userSpaceOnUse" {
		t.Errorf("ContentUnits = %q, want userSpaceOnUse (default)", s.Mask.ContentUnits)
	}
	// Default region: -10%, -10%, 120%, 120% of the bbox, i.e. fractions
	// -0.1, -0.1, 1.2, 1.2 in objectBoundingBox space.
	if s.Mask.RegionX != -0.1 || s.Mask.RegionY != -0.1 {
		t.Errorf("RegionX/Y = %v/%v, want -0.1/-0.1", s.Mask.RegionX, s.Mask.RegionY)
	}
	if s.Mask.RegionW != 1.2 || s.Mask.RegionH != 1.2 {
		t.Errorf("RegionW/H = %v/%v, want 1.2/1.2", s.Mask.RegionW, s.Mask.RegionH)
	}
}

func TestMaskUnitsUserSpaceOnUseRegion(t *testing.T) {
	src := `<svg ` + maskHdr + ` width="200" height="200">
	  <mask id="m1" maskUnits="userSpaceOnUse" x="30" y="50" width="100" height="120">
	    <rect width="10" height="10"/>
	  </mask>
	  <rect width="100" height="100" mask="url(#m1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.Mask.Units != "userSpaceOnUse" {
		t.Fatalf("Units = %q, want userSpaceOnUse", s.Mask.Units)
	}
	if s.Mask.RegionX != 30 || s.Mask.RegionY != 50 || s.Mask.RegionW != 100 || s.Mask.RegionH != 120 {
		t.Errorf("region = (%v,%v,%v,%v), want (30,50,100,120)",
			s.Mask.RegionX, s.Mask.RegionY, s.Mask.RegionW, s.Mask.RegionH)
	}
}

func TestMaskTypeAttributeAndStyle(t *testing.T) {
	cases := []struct {
		attr string
		want MaskType
	}{
		{`mask-type="alpha"`, MaskAlpha},
		{`mask-type="luminance"`, MaskLuminance},
		{`style="mask-type:alpha"`, MaskAlpha},
		{``, MaskLuminance},
	}
	for _, c := range cases {
		src := fmt.Sprintf(`<svg %s width="100" height="100">
		  <mask id="m1" %s><rect width="10" height="10"/></mask>
		  <rect width="100" height="100" mask="url(#m1)"/>
		</svg>`, maskHdr, c.attr)
		d, err := Parse([]byte(src), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.attr, err)
		}
		_, root := d.Root()
		s := root.Kids[0].(*Shape)
		if s.Mask.Type != c.want {
			t.Errorf("%s: Type = %v, want %v", c.attr, s.Mask.Type, c.want)
		}
	}
}

func TestMaskTransformAttributeIgnored(t *testing.T) {
	// A transform on the <mask> ELEMENT itself has no effect per SVG; only
	// this package's Mask type carries no M field at all (see the doc
	// comment on Mask), so there is nothing for pkg/svg/draw to even
	// accidentally apply.
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1" transform="skewX(30)"><rect width="10" height="10"/></mask>
	  <rect width="100" height="100" mask="url(#m1)"/>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	s := root.Kids[0].(*Shape)
	if s.Mask == nil {
		t.Fatal("Mask did not resolve")
	}
	// Nothing more to assert at the type level -- Mask has no M/transform
	// field to check; draw_test.go's render-level test proves the
	// transform truly has no visual effect.
}

func TestMaskSelfReferenceResolvesButTerminates(t *testing.T) {
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1" mask="url(#m1)"><rect width="100" height="100" fill="white"/></mask>
	  <rect width="100" height="100" mask="url(#m1)"/>
	</svg>`
	done := make(chan *Document, 1)
	go func() {
		d, err := Parse([]byte(src), nil)
		if err != nil {
			t.Error(err)
			return
		}
		done <- d
	}()
	select {
	case d := <-done:
		_, root := d.Root()
		s := root.Kids[0].(*Shape)
		if s.Mask == nil {
			t.Fatal("Mask did not resolve")
		}
		if s.Mask.Self != nil {
			t.Error("Mask.Self should be nil: self-reference must be ignored, not recursed into")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Parse did not terminate on a self-referencing mask")
	}
}

func TestMaskCycleResolvesButTerminates(t *testing.T) {
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1" mask="url(#m2)"><rect width="100" height="100" fill="white"/></mask>
	  <mask id="m2" mask="url(#m1)"><rect width="100" height="100" fill="white"/></mask>
	  <rect width="100" height="100" mask="url(#m2)"/>
	</svg>`
	done := make(chan *Document, 1)
	go func() {
		d, err := Parse([]byte(src), nil)
		if err != nil {
			t.Error(err)
			return
		}
		done <- d
	}()
	select {
	case d := <-done:
		_, root := d.Root()
		s := root.Kids[0].(*Shape)
		if s.Mask == nil {
			t.Fatal("Mask did not resolve")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Parse did not terminate on a cyclic mask chain")
	}
}

func TestMaskGroupElementResolvesMask(t *testing.T) {
	src := `<svg ` + maskHdr + ` width="100" height="100">
	  <mask id="m1"><rect width="100" height="100" fill="white"/></mask>
	  <g mask="url(#m1)"><rect width="50" height="50"/></g>
	</svg>`
	d, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, root := d.Root()
	g, ok := root.Kids[0].(*Group)
	if !ok {
		t.Fatalf("root.Kids[0] = %#v, want *Group", root.Kids[0])
	}
	if g.Mask == nil {
		t.Fatal("group's mask did not resolve")
	}
}
