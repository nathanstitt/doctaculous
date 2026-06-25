package css

import (
	"context"
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
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

// firstElementBox descends depth-first to the styled test element's box: the
// first element-level box (block/inline/replaced — not an anonymous wrapper or
// text run) carrying an explicit pixel Width. The positioning wiring tests below
// all mark their subject element with `width:10px`, which the document's html/body
// wrappers (width:auto) never carry, so this unambiguously returns that box.
func firstElementBox(t *testing.T, b *cssbox.Box) *cssbox.Box {
	t.Helper()
	got := findElementBox(b)
	if got == nil {
		t.Fatal("no styled element box (width:10px) found")
	}
	return got
}

func findElementBox(b *cssbox.Box) *cssbox.Box {
	if b.Kind != cssbox.BoxAnonBlock && b.Kind != cssbox.BoxAnonInline && b.Kind != cssbox.BoxText {
		if b.Style.Width.Unit == gcss.UnitPx && b.Style.Width.Value == 10 {
			return b
		}
	}
	for _, c := range b.Children {
		if got := findElementBox(c); got != nil {
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

// TestFloatOf maps the computed float keyword to a FloatKind.
func TestFloatOf(t *testing.T) {
	cases := map[string]cssbox.FloatKind{
		"none":  cssbox.FloatNone,
		"left":  cssbox.FloatLeft,
		"right": cssbox.FloatRight,
		"":      cssbox.FloatNone, // unset/zero string
	}
	for in, want := range cases {
		if got := floatOf(gcss.ComputedStyle{Float: in}); got != want {
			t.Errorf("floatOf(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBlockifyFloatedInline: a floated inline-level element classifies as a
// block-level box (CSS 9.7), so the layout engine lays it out as a float.
func TestBlockifyFloatedInline(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline", Float: "left"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	// Pre-blockify it is inline; applyBlockify promotes it.
	applyBlockify(b, cs)
	if !b.Kind.IsBlockLevel() {
		t.Errorf("floated inline: Kind=%v not block-level", b.Kind)
	}
	if b.Display != cssbox.DisplayBlock {
		t.Errorf("floated inline: Display=%v, want DisplayBlock", b.Display)
	}
}

// TestNoBlockifyWithoutFloat: an inline element with no float stays inline.
func TestNoBlockifyWithoutFloat(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline", Float: "none"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	applyBlockify(b, cs)
	if b.Kind != cssbox.BoxInline {
		t.Errorf("non-floated inline: Kind=%v, want BoxInline", b.Kind)
	}
}

// TestNoBlockifyFloatedInlineBlock: a floated display:inline-block element already
// has a block-level Kind (classifyDisplay maps inline-block to BoxBlock), so
// applyBlockify leaves it unchanged — it does NOT reset DisplayInlineBlock to
// DisplayBlock.
func TestNoBlockifyFloatedInlineBlock(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline-block", Float: "left"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	applyBlockify(b, cs)
	if b.Kind != cssbox.BoxBlock {
		t.Errorf("floated inline-block: Kind=%v, want BoxBlock (unchanged)", b.Kind)
	}
	if b.Display != cssbox.DisplayInlineBlock {
		t.Errorf("floated inline-block: Display=%v, want DisplayInlineBlock (unchanged)", b.Display)
	}
}

// TestBlockifyFloatedInlinePreservesFormatting: a floated display:inline box becomes
// block-level but KEEPS its inline formatting context (InlineFC) so its inline/text
// children still lay out inline inside the now block-level box.
func TestBlockifyFloatedInlinePreservesFormatting(t *testing.T) {
	cs := gcss.ComputedStyle{Display: "inline", Float: "left"}
	b := &cssbox.Box{Style: cs}
	classifyDisplay(b, cs.Display)
	if b.Formatting != cssbox.InlineFC {
		t.Fatalf("precondition: classifyDisplay should set InlineFC for inline, got %v", b.Formatting)
	}
	applyBlockify(b, cs)
	if b.Formatting != cssbox.InlineFC {
		t.Errorf("floated inline: Formatting=%v, want InlineFC preserved", b.Formatting)
	}
}

// TestPositionOf: positionOf maps each keyword.
func TestPositionOf(t *testing.T) {
	cases := map[string]cssbox.PositionKind{
		"static": cssbox.PosStatic, "relative": cssbox.PosRelative,
		"absolute": cssbox.PosAbsolute, "fixed": cssbox.PosFixed, "": cssbox.PosStatic,
	}
	for kw, want := range cases {
		cs := gcss.ComputedStyle{Position: kw}
		if got := positionOf(cs); got != want {
			t.Errorf("positionOf(%q) = %v, want %v", kw, got, want)
		}
	}
}

// TestAbsPositionForcesFloatNone: an absolutely/fixed-positioned element computes
// float to none (CSS 9.7), so it is NOT placed as a float.
func TestAbsPositionForcesFloatNone(t *testing.T) {
	root := build(t, `<div style="position:absolute; float:left; width:10px; height:10px"></div>`, nil)
	abs := firstElementBox(t, root) // helper: descend to the styled div's box
	if abs.Position != cssbox.PosAbsolute {
		t.Fatalf("position = %v, want PosAbsolute", abs.Position)
	}
	if abs.Float != cssbox.FloatNone {
		t.Errorf("abs-pos box Float = %v, want FloatNone (CSS 9.7)", abs.Float)
	}
}

// TestRelativeFloatKeepsBoth: a relative+float box stays a float AND positioned.
func TestRelativeFloatKeepsBoth(t *testing.T) {
	root := build(t, `<div style="position:relative; float:left; width:10px; height:10px"></div>`, nil)
	b := firstElementBox(t, root)
	if b.Float != cssbox.FloatLeft {
		t.Errorf("Float = %v, want FloatLeft (relative does not override float)", b.Float)
	}
	if b.Position != cssbox.PosRelative {
		t.Errorf("Position = %v, want PosRelative", b.Position)
	}
}

// TestAbsInlineBlockifies: an inline element that is absolutely positioned
// blockifies (CSS 9.7), like a float.
func TestAbsInlineBlockifies(t *testing.T) {
	root := build(t, `<span style="position:absolute; width:10px; height:10px"></span>`, nil)
	b := firstElementBox(t, root)
	if !b.Kind.IsBlockLevel() {
		t.Errorf("abs-pos inline did not blockify: Kind=%v", b.Kind)
	}
}

func TestTableUADisplaysAndSpans(t *testing.T) {
	src := `<table><caption>C</caption><colgroup><col span="2"></colgroup>
		<thead><tr><th colspan="2">H</th></tr></thead>
		<tbody><tr><td rowspan="2">A</td><td>B</td></tr></tbody>
		<tfoot><tr><td>F</td></tr></tfoot></table>`
	root := build(t, src, nil)
	tbl := firstByDisplay(root, cssbox.DisplayTable)
	if tbl == nil {
		t.Fatal("no DisplayTable box; <table> UA rule missing")
	}
	if firstByDisplay(tbl, cssbox.DisplayTableCaption) == nil {
		t.Error("no caption box")
	}
	if firstByDisplay(tbl, cssbox.DisplayTableHeaderGroup) == nil {
		t.Error("no thead/table-header-group box")
	}
	if firstByDisplay(tbl, cssbox.DisplayTableFooterGroup) == nil {
		t.Error("no tfoot/table-footer-group box")
	}
	if firstByDisplay(tbl, cssbox.DisplayTableColumnGroup) == nil {
		t.Error("no colgroup/table-column-group box")
	}
	col := firstByDisplay(tbl, cssbox.DisplayTableColumn)
	if col == nil || col.ColSpan != 2 {
		t.Errorf("col span not read onto box: %+v", col)
	}
	th := firstByDisplay(tbl, cssbox.DisplayTableCell)
	if th == nil || th.ColSpan != 2 {
		t.Errorf("th colspan=2 not read; got %+v", th)
	}
	rs := findCellWithRowSpan(tbl, 2)
	if rs == nil {
		t.Error("td rowspan=2 not read onto a cell box")
	}
}

func findCellWithRowSpan(b *cssbox.Box, n int) *cssbox.Box {
	if b == nil {
		return nil
	}
	if b.Display == cssbox.DisplayTableCell && b.RowSpan == n {
		return b
	}
	for _, c := range b.Children {
		if r := findCellWithRowSpan(c, n); r != nil {
			return r
		}
	}
	return nil
}
