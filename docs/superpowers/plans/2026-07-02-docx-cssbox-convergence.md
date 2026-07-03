# DOCX → cssbox Convergence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-point DOCX rendering off the flat `box.Document` engine onto the recursive `cssbox` +
CSS layout engine, then delete the flat engine so one engine drives every reflow format.

**Architecture:** A new `pkg/docx/cssbox` lowering builds `*cssbox.Box` trees directly from the
existing `style.Resolver` cascade output — no HTML serialization, no CSS re-parse. Three small
*additive* engine additions (`text-indent`, line-height "at least" mode, DOCX auto multiplier) fill
the vocabulary gaps; each is inert for HTML (byte-identical). `docxDocument` is re-pointed to the CSS
engine (both paths already return `*layout.Pages`), the `docx-*` goldens are regenerated + eyeballed,
and the flat engine (`pkg/layout/box`, `flow.go`, flat paint, old `pkg/docx/lower`) is deleted in the
same PR.

**Tech Stack:** Go; `pkg/css` (ComputedStyle), `pkg/layout/cssbox` (box tree), `pkg/layout/css`
(engine), `pkg/docx` + `pkg/docx/style` (parse + cascade), golden-image tests.

**Spec:** `docs/superpowers/specs/2026-07-02-docx-cssbox-convergence-design.md`

---

## File Structure

- **Create:** `pkg/docx/cssbox/lower.go` — `Lower(d *docx.Document, r *style.Resolver) *cssbox.Box`.
  Builds the `cssbox` tree (body block → paragraph blocks → run text boxes). Separate package (like
  the flat `pkg/docx/lower`) to avoid the `pkg/docx/style` import cycle.
- **Create:** `pkg/docx/cssbox/lower_test.go` — structural assertions on the produced tree.
- **Modify:** `pkg/css/cascade.go` — add `TextIndent Length` and line-height "at least" carrier
  (`LineHeightMin Length`) to `ComputedStyle`.
- **Modify:** `pkg/layout/css/inline.go` — honor `TextIndent` (first-line offset) and `LineHeightMin`
  (line-height floor) in the IFC.
- **Modify:** `pkg/doctaculous/reflow_backend.go` — `docxDocument` builds a `cssbox.Box` and runs the
  CSS engine (paged) instead of the flat engine.
- **Delete (final task):** `pkg/layout/box/`, `pkg/layout/flow.go`, `pkg/layout/flow_test.go`,
  `pkg/docx/lower/`, and any flat-only paint code no longer referenced.

---

## Task 1: Add `text-indent` to the CSS cascade

**Files:**
- Modify: `pkg/css/cascade.go` (ComputedStyle struct + the property applier)
- Test: `pkg/css/cascade_test.go` (or the existing cascade test file)

- [ ] **Step 1: Write the failing test**

Add to `pkg/css/cascade_test.go`:

```go
func TestTextIndentCascade(t *testing.T) {
	sheet := MustParse(`p { text-indent: 24px }`)
	cs := computeFor(t, sheet, "p") // existing helper pattern in this file
	if cs.TextIndent.Unit != UnitPx || cs.TextIndent.Value != 24 {
		t.Fatalf("TextIndent = %+v, want {24 px}", cs.TextIndent)
	}
	// Absent → zero length (no indent).
	cs2 := computeFor(t, MustParse(`p { color: red }`), "p")
	if cs2.TextIndent.Value != 0 {
		t.Fatalf("absent TextIndent = %+v, want zero", cs2.TextIndent)
	}
}
```

> If `MustParse`/`computeFor` are named differently in this file, mirror the existing test in
> `pkg/css/cascade_test.go` for another Length property (e.g. how `MarginTop` is tested) — reuse that
> exact helper.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/css -run TestTextIndentCascade -v`
Expected: FAIL — `cs.TextIndent undefined (type ComputedStyle has no field TextIndent)`.

- [ ] **Step 3: Add the field**

In `pkg/css/cascade.go`, add to the `ComputedStyle` struct near `TextAlign`:

```go
	TextIndent Length // first-line indent (signed; negative = hanging). Zero length = none. Inherited.
```

- [ ] **Step 4: Apply the property in the cascade**

Find the switch that maps declaration names to `ComputedStyle` fields (where `text-align` /
`margin-top` are handled) and add a case:

```go
	case "text-indent":
		if l, ok := parseLength(tokenOf(decl.Value)); ok {
			cs.TextIndent = l
		}
```

> Match the surrounding style: use whatever single-token length parse the neighboring length
> properties use (`parseLength` + the file's token accessor). `text-indent` is **inherited** — ensure
> it is copied in the inheritance step alongside `TextAlign`/`Color` (find where inherited properties
> are propagated to children and add `TextIndent`).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/css -run TestTextIndentCascade -v`
Expected: PASS.

- [ ] **Step 6: Verify HTML byte-identity is not affected**

Run: `go test ./pkg/layout/css ./pkg/doctaculous -run Golden`
Expected: PASS (no golden changes — `text-indent` is unset on every existing fixture).

- [ ] **Step 7: Commit**

```bash
git add pkg/css/cascade.go pkg/css/cascade_test.go
git commit -m "css: add text-indent to the cascade (inherited)"
```

---

## Task 2: Honor `text-indent` in the inline formatting context

**Files:**
- Modify: `pkg/layout/css/inline.go` (first-line width offset in `layoutInline`)
- Test: `pkg/layout/css/inline_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/inline_test.go`:

```go
func TestTextIndentOffsetsFirstLine(t *testing.T) {
	// A block with text-indent:20pt starts its first line 20pt in; wrapped lines do not.
	b := blockWithText(t, "one two three four five six seven eight nine ten", func(cs *gcss.ComputedStyle) {
		cs.TextIndent = gcss.Length{Value: 20, Unit: gcss.UnitPt}
	})
	e := newTestEngine(t)
	lines, _, _ := e.layoutInline(context.Background(), b, 120, 0, 0, 0, nil)
	if len(lines) < 2 {
		t.Fatalf("want the text to wrap to >=2 lines, got %d", len(lines))
	}
	if lines[0].X <= lines[1].X {
		t.Fatalf("first line X (%v) should be indented past wrapped line X (%v)", lines[0].X, lines[1].X)
	}
}
```

> Use the test's existing engine/box constructors — mirror another test in `inline_test.go` that
> calls `e.layoutInline`. `blockWithText`/`newTestEngine`/the `LineFragment.X` field name must match
> what that file already uses; adapt names to the real helpers, keep the assertion (first line is
> indented past the wrapped line).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestTextIndentOffsetsFirstLine -v`
Expected: FAIL — first line X equals wrapped line X (no indent applied yet).

- [ ] **Step 3: Apply the indent in `layoutInline`**

In `pkg/layout/css/inline.go`, `layoutInline` computes a first-line width for the breaker
(`inline.Break(glyphs, maxWidthPt, firstWidthPt)`) and an X for each line. Read the block's
`text-indent` and apply it: reduce the first line's available width by the indent and shift its X by
the same amount. Add near where `align` is resolved (~line 44):

```go
	indent := textIndentPt(b) // resolved points; 0 for anonymous/unset
```

Add the helper (place beside `effectiveTextAlign`):

```go
// textIndentPt resolves a block's text-indent to points against its font size.
// Percentages resolve against the content width by the caller if needed; only
// px/pt/em are produced by DOCX lowering, so those three are resolved here and
// anything else (incl. an anonymous box's zero style) is 0.
func textIndentPt(b *cssbox.Box) float64 {
	ti := b.Style.TextIndent
	switch ti.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return ti.Value
	case gcss.UnitEm:
		return ti.Value * b.Style.FontSizePt
	default:
		return 0
	}
}
```

Then thread `indent` into the first line: pass `firstWidthPt = contentW - indent` to the breaker (it
already accepts a distinct first-line width), and add `indent` to the first line's start X when
placing lines (the first `LineFragment` only). Leave wrapped lines unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestTextIndentOffsetsFirstLine -v`
Expected: PASS.

- [ ] **Step 5: Verify HTML byte-identity**

Run: `go test ./pkg/layout/css ./pkg/doctaculous -run Golden`
Expected: PASS — `text-indent` is 0 on all existing fixtures, so `indent == 0` and the code path is
a no-op.

- [ ] **Step 6: Commit**

```bash
git add pkg/layout/css/inline.go pkg/layout/css/inline_test.go
git commit -m "css/inline: honor text-indent on the first line (hanging when negative)"
```

---

## Task 3: Add line-height "at least" (minimum) to the engine

**Files:**
- Modify: `pkg/css/cascade.go` (ComputedStyle: `LineHeightMin Length`)
- Modify: `pkg/layout/css/inline.go` (`effectiveLineHeight` floors by the minimum)
- Test: `pkg/layout/css/inline_test.go`

Rationale: `box.LineHeightAtLeast` (the DOCX default `lineRule`) has no CSS equivalent — CSS
`line-height` is a fixed multiple/length or "normal", never "at least N". We carry it as a separate
`LineHeightMin` field that floors the resolved line height. HTML never sets it, so it is inert there.

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/inline_test.go`:

```go
func TestLineHeightMinFloorsHeight(t *testing.T) {
	e := newTestEngine(t)
	// A small font whose natural line box is < 40pt; LineHeightMin:40 raises it to 40.
	b := blockWithText(t, "hi", func(cs *gcss.ComputedStyle) {
		cs.FontSizePt = 10
		cs.LineHeightMin = gcss.Length{Value: 40, Unit: gcss.UnitPt}
	})
	_, height, _ := e.layoutInline(context.Background(), b, 500, 0, 0, 0, nil)
	if height < 40 {
		t.Fatalf("line height = %v, want floored to >= 40", height)
	}
	// Without the floor, the same text is much shorter.
	b2 := blockWithText(t, "hi", func(cs *gcss.ComputedStyle) { cs.FontSizePt = 10 })
	_, h2, _ := e.layoutInline(context.Background(), b2, 500, 0, 0, 0, nil)
	if h2 >= 40 {
		t.Fatalf("baseline line height = %v, expected < 40 (test fixture wrong)", h2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/layout/css -run TestLineHeightMinFloorsHeight -v`
Expected: FAIL — `cs.LineHeightMin undefined`.

- [ ] **Step 3: Add the field**

In `pkg/css/cascade.go` `ComputedStyle`, beside `LineHeight`:

```go
	LineHeightMin Length // "at least" floor for line height (DOCX lineRule=atLeast). Zero = no floor. Inherited.
```

Copy it in the inheritance step alongside `LineHeight`.

- [ ] **Step 4: Floor the resolved height in `effectiveLineHeight`**

In `pkg/layout/css/inline.go` `effectiveLineHeight`, after `h` is computed and before the atomic
check, add:

```go
	if min := lineHeightMinPt(b); min > h {
		h = min
	}
```

Add the helper (beside `textIndentPt`):

```go
// lineHeightMinPt resolves a block's line-height "at least" floor to points.
// px/pt are absolute; em resolves against the font size; anything else is 0 (no floor).
func lineHeightMinPt(b *cssbox.Box) float64 {
	m := b.Style.LineHeightMin
	switch m.Unit {
	case gcss.UnitPx, gcss.UnitPt:
		return m.Value
	case gcss.UnitEm:
		return m.Value * b.Style.FontSizePt
	default:
		return 0
	}
}
```

> Note: for an anonymous box `effectiveLineHeight` reads style from the first inline leaf; DOCX
> produces non-anonymous paragraph blocks carrying the style, so the `else` branch (reads `b.Style`)
> applies. The floor is applied unconditionally after both branches, which is correct.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/layout/css -run TestLineHeightMinFloorsHeight -v`
Expected: PASS.

- [ ] **Step 6: Verify HTML byte-identity**

Run: `go test ./pkg/layout/css ./pkg/doctaculous -run Golden`
Expected: PASS — `LineHeightMin` is zero on all HTML fixtures.

- [ ] **Step 7: Commit**

```bash
git add pkg/css/cascade.go pkg/layout/css/inline.go pkg/layout/css/inline_test.go
git commit -m "css: add line-height 'at least' floor (LineHeightMin) for DOCX lineRule=atLeast"
```

---

## Task 4: DOCX → cssbox lowering — page geometry + empty document

**Files:**
- Create: `pkg/docx/cssbox/lower.go`
- Create: `pkg/docx/cssbox/lower_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/docx/cssbox/lower_test.go`:

```go
package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func TestLowerNilIsEmptyRoot(t *testing.T) {
	root := Lower(nil, nil)
	if root == nil || root.Kind != lcssbox.BoxBlock {
		t.Fatalf("Lower(nil,nil) = %+v, want a non-nil BoxBlock root", root)
	}
	if len(root.Children) != 0 {
		t.Fatalf("empty document should have no child blocks, got %d", len(root.Children))
	}
}

func TestLowerPageGeometry(t *testing.T) {
	d := &docx.Document{Section: docx.SectionProps{
		PageW: docx.Twips(12240), PageH: docx.Twips(15840), // 8.5x11in
		MarginTop: docx.Twips(1440), MarginBottom: docx.Twips(1440),
		MarginLeft: docx.Twips(1440), MarginRight: docx.Twips(1440),
	}}
	root := Lower(d, style.NewResolver(d, nil))
	if root.Kind != lcssbox.BoxBlock {
		t.Fatalf("root kind = %v, want BoxBlock", root.Kind)
	}
	// Geometry is carried out-of-band (see PageGeometry accessor), not on the box.
	g := Geometry(d)
	if g.ContentWidthPt() <= 0 || g.PageHeightPt <= 0 {
		t.Fatalf("geometry not resolved: %+v", g)
	}
}
```

> Confirm the real constructors: `docx.Twips`, `docx.SectionProps` field names (`PageW`, `PageH`,
> `MarginTop`, …), and `style.NewResolver` are used exactly as in `pkg/docx/lower/lower.go` and
> `pkg/doctaculous/reflow_backend.go`. If `Twips` is a method (`.Points()`) rather than a constructor,
> mirror how `pkg/docx/lower/lower_test.go` builds a section.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run TestLower -v`
Expected: FAIL — package/functions do not exist.

- [ ] **Step 3: Create the lowering skeleton with geometry**

Create `pkg/docx/cssbox/lower.go`:

```go
// Package cssbox lowers a parsed DOCX document into the recursive cssbox tree the
// CSS layout engine consumes, replacing the flat pkg/docx/lower + pkg/layout/box
// path. It resolves each paragraph and run through the DOCX style cascade and emits
// concrete css.ComputedStyle values, so nothing DOCX-specific crosses the boundary.
// It lives outside pkg/docx to avoid an import cycle with pkg/docx/style.
package cssbox

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// PageGeometry is the DOCX section geometry in points, carried alongside the box
// tree (the cssbox tree itself is geometry-free; the engine takes width/height as
// layout inputs).
type PageGeometry struct {
	PageWidthPt, PageHeightPt                                float64
	MarginTopPt, MarginBottomPt, MarginLeftPt, MarginRightPt float64
}

// ContentWidthPt is the page width minus left/right margins (the layout viewport).
func (g PageGeometry) ContentWidthPt() float64 {
	return g.PageWidthPt - g.MarginLeftPt - g.MarginRightPt
}

// ContentHeightPt is the page height minus top/bottom margins (the pagination band).
func (g PageGeometry) ContentHeightPt() float64 {
	return g.PageHeightPt - g.MarginTopPt - g.MarginBottomPt
}

// Geometry resolves a document's section geometry into points. A nil document
// yields the zero geometry.
func Geometry(d *docx.Document) PageGeometry {
	if d == nil {
		return PageGeometry{}
	}
	s := d.Section
	return PageGeometry{
		PageWidthPt:    s.PageW.Points(),
		PageHeightPt:   s.PageH.Points(),
		MarginTopPt:    s.MarginTop.Points(),
		MarginBottomPt: s.MarginBottom.Points(),
		MarginLeftPt:   s.MarginLeft.Points(),
		MarginRightPt:  s.MarginRight.Points(),
	}
}

// Lower converts a parsed DOCX document into a cssbox tree rooted at a block box
// (the <body> analogue). A nil document or resolver yields an empty root rather
// than panicking. Page geometry is obtained separately via Geometry(d).
func Lower(d *docx.Document, r *style.Resolver) *lcssbox.Box {
	root := &lcssbox.Box{Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock, Formatting: lcssbox.BlockFC}
	if d == nil || r == nil {
		return root
	}
	for _, blk := range d.Body {
		if blk.Paragraph == nil {
			continue
		}
		root.Children = append(root.Children, lowerParagraph(blk.Paragraph, r)...)
	}
	return root
}

// lowerParagraph is implemented in Task 5.
func lowerParagraph(p *docx.Paragraph, r *style.Resolver) []*lcssbox.Box { return nil }
```

> Match `SectionProps`/`.Points()` to the real API used in `pkg/docx/lower/lower.go`
> (`lowerPage`). Reuse the exact same field names and `.Points()` method.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run TestLower -v`
Expected: PASS (paragraph lowering is a stub returning nil; geometry + empty-root tests pass).

- [ ] **Step 5: Commit**

```bash
git add pkg/docx/cssbox/lower.go pkg/docx/cssbox/lower_test.go
git commit -m "docx/cssbox: lowering skeleton — page geometry + empty root"
```

---

## Task 5: DOCX → cssbox lowering — paragraphs, runs, and styles

**Files:**
- Modify: `pkg/docx/cssbox/lower.go` (implement `lowerParagraph` + helpers)
- Test: `pkg/docx/cssbox/lower_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/docx/cssbox/lower_test.go`:

```go
func TestLowerParagraphAndRun(t *testing.T) {
	d := docWithParagraph(t, // helper: one centered paragraph, one bold run "Hi"
		docx.JustifyCenter,
		docx.Run{Text: "Hi", Props: boldRunProps()},
	)
	root := Lower(d, style.NewResolver(d, nil))
	if len(root.Children) != 1 {
		t.Fatalf("want 1 paragraph block, got %d", len(root.Children))
	}
	para := root.Children[0]
	if para.Kind != lcssbox.BoxBlock || para.Formatting != lcssbox.InlineFC {
		t.Fatalf("paragraph = kind %v fc %v, want BoxBlock/InlineFC", para.Kind, para.Formatting)
	}
	if para.Style.TextAlign != "center" {
		t.Fatalf("TextAlign = %q, want center", para.Style.TextAlign)
	}
	if len(para.Children) != 1 {
		t.Fatalf("want 1 run text box, got %d", len(para.Children))
	}
	run := para.Children[0]
	if run.Kind != lcssbox.BoxText || run.Text != "Hi" {
		t.Fatalf("run = kind %v text %q, want BoxText/\"Hi\"", run.Kind, run.Text)
	}
	if !run.Style.Bold {
		t.Fatalf("run.Style.Bold = false, want true")
	}
}

func TestLowerHardBreak(t *testing.T) {
	d := docWithParagraph(t, docx.JustifyLeft,
		docx.Run{Text: "a"},
		docx.Run{Break: docx.BreakLine},
		docx.Run{Text: "b"},
	)
	root := Lower(d, style.NewResolver(d, nil))
	kids := root.Children[0].Children
	if len(kids) != 3 {
		t.Fatalf("want 3 children (text, break, text), got %d", len(kids))
	}
	if !isHardBreak(kids[1]) {
		t.Fatalf("middle child should be a hard break, got %+v", kids[1])
	}
}

func TestLowerPageBreakSplitsBlocks(t *testing.T) {
	d := docWithParagraph(t, docx.JustifyLeft,
		docx.Run{Text: "before"},
		docx.Run{Break: docx.BreakPage},
		docx.Run{Text: "after"},
	)
	root := Lower(d, style.NewResolver(d, nil))
	if len(root.Children) != 2 {
		t.Fatalf("page break should split into 2 paragraph blocks, got %d", len(root.Children))
	}
	if root.Children[1].Style.BreakBefore != "page" {
		t.Fatalf("second block BreakBefore = %q, want page", root.Children[1].Style.BreakBefore)
	}
}
```

> `docWithParagraph`, `boldRunProps`, `docx.Run{Break:...}`, `docx.BreakLine/BreakPage`,
> `docx.JustifyCenter/Left` must mirror the real types in `pkg/docx` and the patterns in
> `pkg/docx/lower/lower_test.go`. `isHardBreak` is a local test helper asserting the box's marker for
> a hard break (see Step 3 — either a `BoxText` with a newline in a `pre-line` whitespace, or a
> dedicated break representation; assert whichever Step 3 produces).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/docx/cssbox -run TestLower -v`
Expected: FAIL — `lowerParagraph` returns nil, so `root.Children` is empty.

- [ ] **Step 3: Implement `lowerParagraph` and style mapping**

Replace the `lowerParagraph` stub in `pkg/docx/cssbox/lower.go` and add helpers:

```go
// lowerParagraph resolves a paragraph's effective formatting into a block box and
// its runs into styled text boxes. A page break inside a run splits the paragraph
// into two block boxes, the second carrying BreakBefore:"page".
func lowerParagraph(p *docx.Paragraph, r *style.Resolver) []*lcssbox.Box {
	eff := r.EffectiveParagraph(p.Props)
	newBlock := func() *lcssbox.Box {
		return &lcssbox.Box{
			Kind:       lcssbox.BoxBlock,
			Display:    lcssbox.DisplayBlock,
			Formatting: lcssbox.InlineFC,
			Style:      paragraphStyle(eff),
		}
	}
	var blocks []*lcssbox.Box
	cur := newBlock()
	for _, run := range p.Runs {
		switch run.Break {
		case docx.BreakPage:
			blocks = append(blocks, cur)
			cur = newBlock()
			cur.Style.BreakBefore = "page"
			continue
		case docx.BreakLine, docx.BreakColumn:
			cur.Children = append(cur.Children, hardBreakBox(cur.Style))
			continue
		}
		if run.Text == "" {
			continue
		}
		er := r.EffectiveRun(p.Props, run.Props)
		cur.Children = append(cur.Children, runTextBox(run.Text, er, cur.Style))
	}
	blocks = append(blocks, cur)
	return blocks
}

// paragraphStyle maps a resolved paragraph's block-level formatting onto a
// ComputedStyle. Alignment, spacing (as margins), indents (as margins + text-indent),
// and line spacing are all resolved to concrete values/points here.
func paragraphStyle(eff style.EffectiveParagraph) gcss.ComputedStyle {
	cs := gcss.ComputedStyle{
		Display:   "block",
		TextAlign: alignString(eff.Justify),
	}
	if eff.PageBreakBefore {
		cs.BreakBefore = "page" // paragraph-level w:pageBreakBefore (distinct from a mid-run BreakPage)
	}
	cs.MarginTop = pt(eff.SpaceBeforePt)
	cs.MarginBottom = pt(eff.SpaceAfterPt)
	cs.MarginLeft = pt(eff.IndentLeftPt)
	cs.MarginRight = pt(eff.IndentRightPt)
	cs.TextIndent = pt(eff.FirstLinePt) // negative = hanging
	applyLineHeight(&cs, eff)
	return cs
}

// runTextBox builds a BoxText carrying the run's resolved font/color style. The IFC
// (gatherInlineRuns) reads FontFamily/Bold/Italic/FontSizePt/Color/TextDecorationLine
// directly off the text box, so run style lives here, not on a wrapper inline.
func runTextBox(text string, er style.EffectiveRun, para gcss.ComputedStyle) *lcssbox.Box {
	cs := para // inherit block-level context (line-height/text-align) for the IFC
	cs.Display = "inline"
	cs.FontFamily = er.Family
	cs.Bold = er.Bold
	cs.Italic = er.Italic
	cs.FontSizePt = er.SizePt
	cs.Color = er.Color
	if er.Underline {
		cs.TextDecorationLine = "underline"
	} else {
		cs.TextDecorationLine = "none"
	}
	return &lcssbox.Box{Kind: lcssbox.BoxText, Text: text, Style: cs, Display: lcssbox.DisplayInline}
}

// hardBreakBox produces a box the IFC treats as a forced line break. The shared
// inline core forces a break on a run with Break=true; box generation surfaces that
// via a preserved newline in a pre-line context (white-space) OR the engine's <br>
// mechanism. Use the same mechanism <br> uses (see how pkg/html lowers <br>).
func hardBreakBox(para gcss.ComputedStyle) *lcssbox.Box {
	cs := para
	cs.Display = "inline"
	cs.WhiteSpace = "pre-line" // preserves the newline as a hard break in the IFC
	return &lcssbox.Box{Kind: lcssbox.BoxText, Text: "\n", Style: cs, Display: lcssbox.DisplayInline}
}

func alignString(j docx.Justify) string {
	switch j {
	case docx.JustifyCenter:
		return "center"
	case docx.JustifyRight:
		return "right"
	case docx.JustifyBoth:
		return "justify"
	default:
		return "left"
	}
}

func pt(v float64) gcss.Length { return gcss.Length{Value: v, Unit: gcss.UnitPt} }

// applyLineHeight maps the DOCX effective line spacing onto ComputedStyle's
// LineHeight (auto/exact) and LineHeightMin (atLeast). Auto with no explicit value
// leaves LineHeight zero-value (UnitAuto) so the engine applies its default multiplier.
func applyLineHeight(cs *gcss.ComputedStyle, eff style.EffectiveParagraph) {
	if !eff.HasLine {
		return // UnitAuto default
	}
	switch eff.LineRule {
	case docx.LineRuleExact:
		cs.LineHeight = pt(eff.LineValue)
	case docx.LineRuleAtLeast:
		cs.LineHeightMin = pt(eff.LineValue)
	default: // auto: LineValue in 240ths of a line
		if mult := eff.LineValue / 240; mult > 0 {
			cs.LineHeight = gcss.Length{Value: mult, Unit: gcss.UnitEm}
		}
	}
}
```

Add the `isHardBreak` test helper to `lower_test.go` matching what `hardBreakBox` produces:

```go
func isHardBreak(b *lcssbox.Box) bool {
	return b.Kind == lcssbox.BoxText && b.Text == "\n" && b.Style.WhiteSpace == "pre-line"
}
```

> **Verify the `<br>` mechanism before finalizing `hardBreakBox`.** Read how `pkg/html` lowers `<br>`
> into the box tree (grep `"br"` in `pkg/html`). If it emits a dedicated break box rather than a
> `pre-line` newline, mirror that exactly and update `isHardBreak`. The inline core forces a break on
> `inline.Run{Break: true}` (`pkg/layout/inline/shape.go:34`), so `gatherInlineRuns` must translate
> whatever `hardBreakBox` emits into a `Break` run — use the representation that already does.
> Confirm `style.EffectiveParagraph` / `style.EffectiveRun` field names (`SpaceBeforePt`, `FirstLinePt`,
> `Family`, `SizePt`, `Underline`, `HasLine`, `LineRule`, `LineValue`) against `pkg/docx/lower/lower.go`
> — they are copied from there.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/docx/cssbox -run TestLower -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Verify the whole package builds and vets**

Run: `go build ./... && go vet ./pkg/docx/cssbox`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add pkg/docx/cssbox/lower.go pkg/docx/cssbox/lower_test.go
git commit -m "docx/cssbox: lower paragraphs/runs/breaks into the cssbox tree"
```

---

## Task 6: Re-point `docxDocument` onto the CSS engine

**Files:**
- Modify: `pkg/doctaculous/reflow_backend.go` (`docxDocument`)
- Test: existing `pkg/doctaculous/docx_golden_test.go` (drives the change)

- [ ] **Step 1: Re-point the DOCX backend**

In `pkg/doctaculous/reflow_backend.go`, replace `docxDocument`'s body. Current:

```go
func docxDocument(d *docx.Document) (*Document, error) {
	resolver := style.NewResolver(d, nil)
	boxDoc := docxlower.Document(d, resolver)
	engine := layout.New(layoutfont.NewFaceCache(), nil)
	pages, err := engine.Layout(context.Background(), boxDoc)
	if err != nil {
		return nil, err
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
```

New:

```go
func docxDocument(d *docx.Document) (*Document, error) {
	resolver := style.NewResolver(d, nil)
	root := docxcssbox.Lower(d, resolver)
	geom := docxcssbox.Geometry(d)
	ctx := context.Background()
	faces := layoutfont.NewFaceCache()
	engine := layoutcss.New(faces, resource.MapLoader(nil), nil)
	pages, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged:        true,
		FallbackW:    geom.ContentWidthPt(),
		FallbackH:    geom.ContentHeightPt(),
		ExplicitSize: true,
	})
	if err != nil {
		return nil, err
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}
```

Update imports in `reflow_backend.go`: drop `docxlower` and the flat `layout` engine import if now
unused; add `docxcssbox "github.com/nathanstitt/doctaculous/pkg/docx/cssbox"`,
`layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"`, and the `resource` package.

> Confirm the loader: the CSS engine `New` requires a `resource.ResourceLoader`. DOCX has no external
> refs, so use an empty in-memory loader: `resource.MapLoader(nil)` (the package's hermetic loader;
> a nil map `Load`s nothing, which is exactly right for DOCX). Confirm `New` tolerates it (it should —
> DOCX emits no `<link>`/`<img>`/`@font-face` refs, so `Load` is never called). `LayoutPagedDoc` + `PagedConfig` field names are from `pkg/layout/css/pagemodel.go`;
> `FallbackW/FallbackH/ExplicitSize/Paged` are the real fields. The margin offset (content inset by
> top/left margins) is applied by the paged path exactly as for HTML `@page` margins — but DOCX has no
> `@page` rule, so if pages come out without the margin inset, thread the geometry via `PagedConfig`'s
> page rules or offset at raster in `reflowRenderer` (mirror the flat `page.go` origin offset). Verify
> against the golden in the next task.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean (fix any unused-import errors from the swap).

- [ ] **Step 3: Run the DOCX golden test (expect diffs, not errors)**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: either PASS (unlikely to be byte-identical) or FAIL with **image diffs** (not a panic/error).
A diff here is expected — Task 7 regenerates. If it **errors** (panic, nil pages, wrong page count),
stop and fix the wiring before regenerating.

- [ ] **Step 4: Commit the re-point (goldens regenerated next task)**

```bash
git add pkg/doctaculous/reflow_backend.go
git commit -m "doctaculous: render DOCX through the CSS engine (cssbox) instead of the flat engine"
```

---

## Task 7: Regenerate and eyeball the `docx-*` goldens

**Files:**
- Modify (regenerate): `pkg/doctaculous/testdata/golden/docx-{paragraph,styled,justify,multipage}.png`

- [ ] **Step 1: Confirm the update flag for the DOCX golden test**

Run: `grep -n "update\|golden.Update\|-update" pkg/doctaculous/docx_golden_test.go`
Expected: an `-update`/`update` flag gate (mirrors `TestGolden`). Note the exact flag name.

- [ ] **Step 2: Regenerate the DOCX goldens**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -update`
Expected: PASS; the four `docx-*.png` files are rewritten.

- [ ] **Step 3: Eyeball every changed golden**

Run: `git diff --stat pkg/doctaculous/testdata/golden/`
Then open each changed PNG (e.g. `open pkg/doctaculous/testdata/golden/docx-paragraph.png`) and
confirm the rendering is correct: text present, alignment (justify centered/justified), multipage
splits across pages, styled runs bold/colored/underlined. **Do not accept a blank or garbled page.**
If a page is wrong, the wiring in Task 6 (geometry/margins/line-height) is off — fix it, then
re-run Step 2. Document in the commit message what changed and why (e.g. "line spacing +1px from the
E5 auto-line-height math; verified faithful").

- [ ] **Step 4: Re-run the golden test clean**

Run: `go test ./pkg/doctaculous -run TestDOCXGolden -v`
Expected: PASS.

- [ ] **Step 5: Commit the regenerated goldens**

```bash
git add pkg/doctaculous/testdata/golden/docx-*.png
git commit -m "doctaculous: regenerate docx-* goldens for the CSS-engine DOCX path (eyeballed)"
```

---

## Task 8: Verify the full suite + DOCX→PDF path

**Files:** none (verification only)

- [ ] **Step 1: Run the whole test suite**

Run: `go test ./...`
Expected: PASS. In particular the **HTML** goldens/reftests and the **non-DOCX** corpus must be
unchanged (byte-identical) — the engine additions (Tasks 1–3) are inert when unset.

- [ ] **Step 2: Confirm the DOCX→PDF writer still works**

Run: `go test ./pkg/doctaculous -run PDF -v`
Expected: PASS — `pdfwrite_golden_test.go` drives `OpenDOCXBytes` → `WritePDF`; the writer consumes
`*layout.Pages` (unchanged output type), so the DOCX→PDF path rides the new engine transparently.
If its golden changed, eyeball + regenerate the same way (Task 7) and note it.

- [ ] **Step 3: Race detector (DOCX now shares the CSS engine's page fan-out)**

Run: `go test -race ./pkg/doctaculous ./pkg/layout/css ./pkg/docx/...`
Expected: PASS, no race warnings.

- [ ] **Step 4: Lint/vet**

Run: `go vet ./... && gofmt -l pkg/`
Expected: no output.

- [ ] **Step 5: Commit (if any fmt/vet fixes)**

```bash
git add -A
git commit -m "chore: gofmt/vet clean after DOCX convergence" || echo "nothing to commit"
```

---

## Task 9: Delete the flat engine

Only after Tasks 1–8 are green. This removes the now-dead second engine.

**Files:**
- Delete: `pkg/layout/box/box.go`, `pkg/layout/box/` (whole dir)
- Delete: `pkg/layout/flow.go`, `pkg/layout/flow_test.go`
- Delete: `pkg/docx/lower/lower.go`, `pkg/docx/lower/lower_test.go`, `pkg/docx/lower/` (whole dir)
- Modify: `pkg/layout/page.go` and `pkg/layout/paint/*` — remove flat-only code no longer referenced

- [ ] **Step 1: Find all references to the flat engine**

Run:
```bash
grep -rn "layout/box\"\|docx/lower\"\|layout\.New(\|layout\.Engine\|box\.Document\|box\.Block\|box\.Inline" pkg/ cmd/ | grep -v "_test.go:.*//"
```
Expected: after Task 6, the only references are inside the flat engine itself and its tests. If any
live caller remains (outside the files being deleted), stop — it must be migrated first.

- [ ] **Step 2: Delete the flat packages**

Run:
```bash
git rm -r pkg/layout/box pkg/docx/lower
git rm pkg/layout/flow.go pkg/layout/flow_test.go
```

- [ ] **Step 3: Remove flat-only code from `pkg/layout/page.go` / `paint`**

Run: `go build ./...`
For each compile error, remove the now-orphaned flat-only symbol (a `box.*`-typed function, a flat
paint entry point). **Keep** anything the CSS engine still uses: the `layout.Pages`/`layout.Item`
types, `paint.PaintPage`, `DrawGlyph`/background/image/border paint. Only delete code whose only
caller was the flat engine. Re-run `go build ./...` until clean.

- [ ] **Step 4: Confirm nothing else broke**

Run: `go build ./... && go test ./...`
Expected: PASS. The DOCX path (now on the CSS engine) and everything else is green without the flat
engine.

- [ ] **Step 5: Grep for stragglers**

Run: `grep -rn "layout/box\|docx/lower\|LineHeightAuto\|box\.Document" pkg/ cmd/`
Expected: no matches (all flat-model vocabulary is gone).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "layout: delete the flat reflow engine (box model, flow.go, docx/lower) — DOCX now on cssbox"
```

---

## Task 10: Update project docs (CLAUDE.md status)

**Files:**
- Modify: `CLAUDE.md` (Architecture section + Status "Done" bullet)

- [ ] **Step 1: Update the Architecture section**

In `CLAUDE.md`, the Architecture paragraph currently describes "two box models" (the flat
`box.Document` and the recursive `cssbox`) and says "These converge late: a dedicated sub-project
re-points DOCX lowering onto `cssbox` and retires the flat model." Rewrite it to state the
convergence is **done**: there is now **one** recursive `cssbox` engine driving every reflow format
(DOCX and HTML); the flat `box.Document` model and flat engine have been removed; DOCX lowers via
`pkg/docx/cssbox` through the same `style.Resolver` cascade.

- [ ] **Step 2: Add a Status "Done" bullet**

Under "### Done", add:

```markdown
- **Reflowable convergence — DOCX on the CSS engine** (`pkg/docx/cssbox` lowering replaces the flat
  `pkg/docx/lower`→`pkg/layout/box`→`pkg/layout/flow` path; `docxDocument` now runs `css.Engine`;
  the flat engine + flat box model are deleted). DOCX paragraphs/runs/geometry lower directly into a
  `cssbox.Box` tree with resolved `css.ComputedStyle`, then paginate through the shared CSS engine —
  so tables/lists/images added to the CSS engine are now reachable by DOCX for free. Three additive
  engine features filled the vocabulary gap (`text-indent`, line-height `at-least` floor
  `LineHeightMin`, DOCX auto multiplier), each inert for HTML (byte-identical). The `docx-*` goldens
  were regenerated + eyeballed (the CSS engine's line-height math differs slightly from the flat
  engine — the expected, approved baseline). See
  `docs/superpowers/specs/2026-07-02-docx-cssbox-convergence-design.md`.
```

- [ ] **Step 3: Move the convergence out of the two-box-model TODO framing**

Search `CLAUDE.md` for any remaining "flat model" / "converge late" / "sub-project 10" language in
the roadmap and update it to past tense (done), so the doc no longer implies the flat engine exists.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: mark DOCX→cssbox convergence done; one reflow engine"
```

---

## Self-Review Notes (addressed)

- **Spec coverage:** §3 approach → Tasks 4–6; §5 gaps 1–4 → Tasks 1–3 (text-indent, line-height
  at-least, auto multiplier via `applyLineHeight` UnitEm mult) + Task 5 hard-break (§5 gap 4);
  §6 package changes → Tasks 4–6, 9; §7 golden gate → Task 7; §8 deletion → Task 9; §9 testing →
  Tasks 1–3, 7, 8; §10 out-of-scope respected (no new DOCX features). CLAUDE.md update → Task 10.
- **§5 gap 2 (×1.15 auto multiplier):** handled by `applyLineHeight`'s auto branch emitting a
  `UnitEm` multiple (240ths→em), which `resolveLineHeight` multiplies by font size — matching the CSS
  engine's own line-height. Any residual difference is absorbed by the Task 7 eyeball gate, per spec.
- **Type consistency:** `Lower`/`Geometry`/`PageGeometry`/`ContentWidthPt`/`ContentHeightPt`,
  `paragraphStyle`/`runTextBox`/`hardBreakBox`/`applyLineHeight`/`alignString`/`pt`, `TextIndent`/
  `LineHeightMin`, `textIndentPt`/`lineHeightMinPt` are used consistently across tasks.
- **Verification caveats flagged inline:** the `<br>`/hard-break mechanism (Task 5), the empty
  resource loader (Task 6), and the margin-inset behavior (Task 6) each carry an explicit "confirm
  against the real API" note, since those are the spots most likely to differ from the assumed shape.
