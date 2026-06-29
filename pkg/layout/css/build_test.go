package css

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// build is a test helper: parse HTML, run Build with an optional loader.
func build(t *testing.T, src string, loader resource.ResourceLoader) *cssbox.Box {
	t.Helper()
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := Build(context.Background(), doc, loader, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return root
}

// firstByDisplay returns the first box (depth-first) with the given display kind.
func firstByDisplay(b *cssbox.Box, d cssbox.DisplayKind) *cssbox.Box {
	if b.Display == d {
		return b
	}
	for _, c := range b.Children {
		if got := firstByDisplay(c, d); got != nil {
			return got
		}
	}
	return nil
}

// firstByKind returns the first box (depth-first) with the given kind, or nil.
func firstByKind(b *cssbox.Box, k cssbox.BoxKind) *cssbox.Box {
	if b.Kind == k {
		return b
	}
	for _, c := range b.Children {
		if got := firstByKind(c, k); got != nil {
			return got
		}
	}
	return nil
}

// parentOfText returns the box whose direct child is a text run equal to text.
func parentOfText(b *cssbox.Box, text string) *cssbox.Box {
	for _, c := range b.Children {
		if c.Kind == cssbox.BoxText && c.Text == text {
			return b
		}
		if got := parentOfText(c, text); got != nil {
			return got
		}
	}
	return nil
}

func TestBuildMapsDisplay(t *testing.T) {
	root := build(t, `<html><body><div>x</div><span>y</span></body></html>`, nil)
	if root.Display != cssbox.DisplayBlock { // html is block per UA
		t.Errorf("root display = %v, want block", root.Display)
	}
	if firstByKind(root, cssbox.BoxBlock) == nil {
		t.Error("expected a block box (div)")
	}
	if firstByKind(root, cssbox.BoxInline) == nil {
		t.Error("expected an inline box (span)")
	}
}

func TestBuildPrunesDisplayNone(t *testing.T) {
	// head (display:none via UA) must not appear in the box tree.
	root := build(t, `<html><head><title>t</title></head><body><p>hi</p></body></html>`, nil)
	if firstByDisplay(root, cssbox.DisplayNone) != nil {
		t.Error("display:none subtree should be pruned, not emitted")
	}
	if len(root.Children) != 1 {
		t.Errorf("html should have 1 child (body) after pruning head, got %d", len(root.Children))
	}
}

func TestBuildAuthorOverridesUA(t *testing.T) {
	// author makes div inline, overriding the UA block default.
	root := build(t, `<html><head><style>div{display:inline}</style></head><body><div>x</div></body></html>`, nil)
	div := firstByKind(root, cssbox.BoxInline)
	if div == nil || div.Style.Display != "inline" {
		t.Error("author display:inline should override UA block for div")
	}
}

func TestBuildReplacedImg(t *testing.T) {
	root := build(t, `<html><body><img src="a.png" alt="a"></body></html>`, nil)
	img := firstByKind(root, cssbox.BoxReplaced)
	if img == nil || img.Replaced == nil {
		t.Fatal("expected a replaced box for img")
	}
	if img.Replaced.Tag != "img" || img.Replaced.Attrs["src"] != "a.png" {
		t.Errorf("replaced facts = %+v", img.Replaced)
	}
}

func TestBuildFlexPreservedAsDisplay(t *testing.T) {
	root := build(t, `<html><head><style>div{display:flex}</style></head><body><div>x</div></body></html>`, nil)
	flex := firstByDisplay(root, cssbox.DisplayFlex)
	if flex == nil {
		t.Fatal("flex display should be preserved (not normalized to block)")
	}
	if flex.Formatting != cssbox.FlexFC {
		t.Errorf("flex box formatting = %v, want FlexFC", flex.Formatting)
	}
	if flex.Kind != cssbox.BoxBlock { // flex container is block-level
		t.Errorf("flex container kind = %v, want BoxBlock", flex.Kind)
	}
}

func TestBuildUnknownDisplayNormalizesToBlock(t *testing.T) {
	root := build(t, `<html><head><style>div{display:wobble}</style></head><body><div>x</div></body></html>`, nil)
	var found *cssbox.Box
	var walk func(*cssbox.Box)
	walk = func(b *cssbox.Box) {
		if b.Style.Display == "wobble" {
			found = b
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatal("div not found")
	}
	if found.Display != cssbox.DisplayBlock || found.Kind != cssbox.BoxBlock {
		t.Errorf("unknown display = (%v,%v), want (block, BoxBlock)", found.Display, found.Kind)
	}
}

func TestBuildResolvesLinkSheet(t *testing.T) {
	loader := resource.MapLoader{
		"theme.css": {Data: []byte(`p{color:red}`), ContentType: "text/css"},
	}
	root := build(t, `<html><head><link rel="stylesheet" href="theme.css"></head><body><p>x</p></body></html>`, loader)
	pBox := parentOfText(root, "x")
	if pBox == nil {
		t.Fatal("p box not found")
	}
	if pBox.Style.Color.R != 255 || pBox.Style.Color.G != 0 {
		t.Errorf("p color = %v, want red from linked sheet", pBox.Style.Color)
	}
}

func TestBuildMissingLinkDegrades(t *testing.T) {
	root := build(t, `<html><head><link rel="stylesheet" href="missing.css"></head><body><p>x</p></body></html>`, resource.MapLoader{})
	if root == nil {
		t.Fatal("Build should succeed despite a missing link")
	}
}
