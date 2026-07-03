package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestLowerNilYieldsEmptyBlockRoot(t *testing.T) {
	root := Lower(nil, nil)
	if root == nil {
		t.Fatal("Lower(nil, nil) = nil, want non-nil root")
	}
	if root.Kind != lcssbox.BoxBlock {
		t.Errorf("root.Kind = %v, want BoxBlock", root.Kind)
	}
	if len(root.Children) != 0 {
		t.Errorf("root has %d children, want 0", len(root.Children))
	}
}

func TestGeometry(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{
			PageW: 12240, PageH: 15840, // Letter
			MarginTop: 1440, MarginBottom: 1440, MarginLeft: 1440, MarginRight: 1440,
		},
	}
	g := Geometry(d)
	if got := g.ContentWidthPt(); got != 468 {
		t.Errorf("ContentWidthPt() = %v, want 468", got)
	}
	if got := g.ContentHeightPt(); got != 648 {
		t.Errorf("ContentHeightPt() = %v, want 648", got)
	}
	if g.PageHeightPt != 792 {
		t.Errorf("PageHeightPt = %v, want 792", g.PageHeightPt)
	}

	root := Lower(d, style.NewResolver(d, nil))
	if root == nil || root.Kind != lcssbox.BoxBlock {
		t.Errorf("Lower(d, r) root = %+v, want BoxBlock root", root)
	}
}
