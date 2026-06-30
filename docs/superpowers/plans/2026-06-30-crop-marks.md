# `@page` Crop Marks + Bleed — Implementation Sub-Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Render CSS Paged Media `marks: crop | cross` registration marks and reserve a `bleed` area, so paginated output is print-ready (a press can trim to the crop marks). Replaces the original "Task 3" (record-only) of `2026-06-30-paged-media-deferrals.md` — the owner chose to actually draw marks (maximal fidelity).

**Architecture:** `marks`/`bleed` parse onto `UsedPage` (in `pkg/css`). The paginator grows each page's rect by `bleed` on all four sides, shifts content inward by `bleed`, and — when `marks` requests them — emits thin filled-rectangle marks (existing `RuleItem`/`BackgroundKind`, no new paint primitive) at the trim corners (crop) and edge midpoints (cross). The marks live in the bleed margin OUTSIDE the trim box, pointing at the trim corners. The `render.Device` seam, PDF/DOCX pipelines, and shared inline core stay untouched.

**Tech Stack:** Go stdlib only. `pkg/css` (parse), `pkg/layout/css` (`pagemodel.go`/`marginbox.go`/`paginate.go`), golden tests.

**Byte-identical guard:** a document with no `marks`/`bleed` (i.e. `bleed == 0` and `marks == ""`) produces pages identical to today — the bleed growth and mark emission are gated on non-zero/non-empty values.

---

## Print geometry reference (what we draw)

For a page whose **trim box** is `W × H` (the `@page size`), with `bleed = b`:

- The **page bitmap** (the "media box") is `(W + 2b) × (H + 2b)`.
- All page content (the existing margin-boxed content + running headers) is shifted by `(b, b)` so the trim box occupies `[b, b] .. [b+W, b+H]` within the bitmap.
- **Crop marks** (`marks: crop`): at each of the four trim corners, two short lines that do NOT touch the corner (they sit in the bleed margin, offset from the trim edge by a gap). Standard: line length ≈ the bleed (or a fixed ~half the bleed, min a few px), gap from the trim box ≈ a small constant. Each corner gets one horizontal and one vertical mark in the bleed band.
- **Cross marks / registration marks** (`marks: cross`): a small plus sign centered on each edge's midpoint, in the bleed band. (Some UAs draw a circled cross; we draw a simple plus — adequate registration target.)
- `marks: crop cross` draws both.
- Mark color: black; thickness: 1 device-ish unit (use 0.5pt so it's hairline at 1:1).

If `bleed == 0` but `marks` is set, draw the marks in a minimal synthesized margin (use a small default mark inset of 6pt so they're visible just outside the trim box, and grow the bitmap by that inset). Document this.

---

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `pkg/css/page.go` | `Marks`/`Bleed` on `UsedPage`; parse in `applyPageDecls` | 1 |
| `pkg/css/page_test.go` | parse tests | 1 |
| `pkg/layout/css/pagemodel.go` | `pageGeom.bleed`; resolve from `UsedPage`; grow page rect | 2 |
| `pkg/layout/css/cropmarks.go` (new) | emit crop/cross mark `RuleItem`s | 3 |
| `pkg/layout/css/paginate.go` (`assemblePages`) | shift content by bleed; append marks | 4 |
| `pkg/doctaculous/pagedmedia_golden_test.go` | end-to-end golden | 5 |

---

## Task 1: Parse `marks` / `bleed` onto `UsedPage`

**Files:**
- Modify: `pkg/css/page.go` (`UsedPage` struct, `applyPageDecls`)
- Test: `pkg/css/page_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/css/page_test.go`:

```go
func TestParsePageMarksBleed(t *testing.T) {
	ss := Parse(`@page { size: A4; marks: crop cross; bleed: 6pt }`)
	up := ss.ResolvePage(0, "", false)
	if up.Marks != "crop cross" {
		t.Errorf("Marks = %q, want \"crop cross\"", up.Marks)
	}
	if up.Bleed < 7.9 || up.Bleed > 8.1 { // 6pt → 8px @96
		t.Errorf("Bleed = %.2f, want ~8 (6pt)", up.Bleed)
	}
	// Defaults: no marks/bleed ⇒ empty/zero.
	none := Parse(`@page { size: A4 }`).ResolvePage(0, "", false)
	if none.Marks != "" || none.Bleed != 0 {
		t.Errorf("defaults = %q/%.2f, want \"\"/0", none.Marks, none.Bleed)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run (set dangerouslyDisableSandbox: true): `go test ./pkg/css/ -run TestParsePageMarksBleed -v`
Expected: FAIL ("up.Marks undefined").

- [ ] **Step 3: Add fields + parse**

In `pkg/css/page.go`, add to the `UsedPage` struct (after `HasRule bool`):

```go
	Marks string  // CSS `marks`: "crop", "cross", or "crop cross" (lowercased); "" = none
	Bleed float64 // CSS `bleed` in the px-as-pt scalar (the trim→media-box inset on each side)
```

In `applyPageDecls`'s `switch d.Property` add:

```go
		case "marks":
			up.Marks = strings.ToLower(strings.TrimSpace(d.Value))
		case "bleed":
			// `bleed: auto` (the initial) means 6pt when marks are present; an explicit
			// length sets the bleed directly. We store an explicit length; auto stays 0
			// (the paginator synthesizes a default inset when marks are set with no bleed).
			if v, ok := parseAbsLengthPx(d.Value); ok {
				up.Bleed = v
			}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/css/ -run TestParsePageMarksBleed -v`
Expected: PASS.

- [ ] **Step 5: Run the css suite**

Run: `go test ./pkg/css/`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
gofmt -w pkg/css/page.go pkg/css/page_test.go
git add pkg/css/page.go pkg/css/page_test.go
git commit -m "feat(css): parse @page marks/bleed onto UsedPage"
```

---

## Task 2: `pageGeom.bleed` + grow the page rect

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (`pageGeom`, `resolvePageGeom`)
- Test: `pkg/layout/css/pagemodel_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/pagemodel_test.go`:

```go
func TestResolvePageGeomBleed(t *testing.T) {
	cfg := pagedConfigFor(`@page { size: 200px 300px; bleed: 10px; marks: crop }`, 200, 300, false)
	g := cfg.resolvePageGeom(0, "", false)
	// Trim box is 200x300; with 10px bleed the MEDIA box (the page bitmap) is 220x320,
	// and the trim box is offset by (10,10).
	if g.mediaW() != 220 || g.mediaH() != 320 {
		t.Errorf("media size = %.0fx%.0f, want 220x320", g.mediaW(), g.mediaH())
	}
	if g.bleed != 10 {
		t.Errorf("bleed = %.1f, want 10", g.bleed)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestResolvePageGeomBleed -v`
Expected: FAIL ("g.bleed undefined" / "g.mediaW undefined").

- [ ] **Step 3: Add `bleed` + media-size accessors**

In `pkg/layout/css/pagemodel.go`, add `bleed float64` to the `pageGeom` struct (after `used`):

```go
	bleed float64 // @page bleed: the trim→media-box inset on each side (0 = none)
```

Add accessors (the media box is the trim box grown by bleed on all sides):

```go
// mediaW / mediaH are the page BITMAP size: the trim box (pageW/pageH) plus bleed on
// each side. With no bleed they equal pageW/pageH (byte-identical).
func (g pageGeom) mediaW() float64 { return g.pageW + 2*g.bleed }
func (g pageGeom) mediaH() float64 { return g.pageH + 2*g.bleed }

// marksRequested reports whether @page asked for crop and/or cross marks.
func (g pageGeom) marksRequested() bool { return g.used.Marks != "" }
```

In `resolvePageGeom`, after the existing geometry is computed (before `return g`), set the bleed — synthesizing a default inset when marks are requested with no explicit bleed:

```go
	g.bleed = up.Bleed
	if g.bleed == 0 && up.Marks != "" {
		g.bleed = defaultMarkInset // marks need room to draw outside the trim box
	}
```

Add the constant near the top of the file:

```go
// defaultMarkInset is the bleed-band width synthesized when @page marks are requested
// without an explicit bleed, so the marks have room outside the trim box (~6pt).
const defaultMarkInset = 8 // px-as-pt (≈6pt)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run TestResolvePageGeomBleed -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/layout/css/pagemodel.go pkg/layout/css/pagemodel_test.go
git add pkg/layout/css/pagemodel.go pkg/layout/css/pagemodel_test.go
git commit -m "feat(css): pageGeom bleed + media-box size accessors"
```

---

## Task 3: Emit crop / cross marks

**Files:**
- Create: `pkg/layout/css/cropmarks.go`
- Test: `pkg/layout/css/cropmarks_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/cropmarks_test.go`:

```go
package css

import (
	"image/color"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

func countRules(items []layout.Item) int {
	n := 0
	for _, it := range items {
		if it.Kind == layout.BackgroundKind || it.Kind == layout.RuleKind {
			n++
		}
	}
	return n
}

func TestAppendCropMarks(t *testing.T) {
	// Trim 200x300, bleed 10 ⇒ media 220x320, trim box at [10,10]..[210,310].
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10}
	g.used.Marks = "crop"
	var items []layout.Item
	items = appendCropMarks(items, g)
	// Crop marks: 2 per corner × 4 corners = 8 thin rules.
	if got := countRules(items); got != 8 {
		t.Errorf("crop marks rule count = %d, want 8 (2 per corner)", got)
	}
	// Every mark is black.
	for _, it := range items {
		if it.Rule.Color != (color.RGBA{0, 0, 0, 255}) {
			t.Errorf("mark color = %v, want black", it.Rule.Color)
		}
	}
	// All marks lie within the bleed band (outside the trim box [10,10]..[210,310]):
	// each mark rect must be entirely in the 10px border ring.
	for _, it := range items {
		r := it.Rule
		inTrim := r.XPt >= 10 && r.YPt >= 10 && r.XPt+r.WPt <= 210 && r.YPt+r.HPt <= 310
		if inTrim {
			t.Errorf("crop mark %+v lies inside the trim box; should be in the bleed band", r)
		}
	}
}

func TestAppendCrossMarks(t *testing.T) {
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10}
	g.used.Marks = "cross"
	var items []layout.Item
	items = appendCropMarks(items, g)
	// Cross marks: a plus (2 rules) at each of the 4 edge midpoints = 8 rules.
	if got := countRules(items); got != 8 {
		t.Errorf("cross marks rule count = %d, want 8", got)
	}
}

func TestNoMarksNoItems(t *testing.T) {
	g := pageGeom{pageW: 200, pageH: 300, bleed: 10} // Marks == ""
	var items []layout.Item
	items = appendCropMarks(items, g)
	if len(items) != 0 {
		t.Errorf("no marks requested ⇒ no items, got %d", len(items))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run 'TestAppendCropMarks|TestAppendCrossMarks|TestNoMarksNoItems' -v`
Expected: FAIL ("undefined: appendCropMarks").

- [ ] **Step 3: Implement `appendCropMarks`**

Create `pkg/layout/css/cropmarks.go`:

```go
package css

import (
	"image/color"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// markThickness is the hairline width of a printed registration mark (px-as-pt).
const markThickness = 0.5

// markLength is the length of a crop mark line (along the trim edge), in px-as-pt.
const markLength = 6

// markGap is the gap between the trim box edge and the near end of a crop mark, so the
// marks sit clear of the artwork (standard print practice), in px-as-pt.
const markGap = 2

// appendCropMarks appends the @page registration marks for page geometry g to items,
// drawn as thin black filled rectangles in the bleed band OUTSIDE the trim box. It
// honors "crop" (corner trim marks) and "cross" (edge-midpoint registration pluses);
// "crop cross" draws both. A no-op when g requests no marks. All coordinates are in the
// MEDIA-box frame (the trim box occupies [bleed,bleed]..[bleed+pageW,bleed+pageH]).
func appendCropMarks(items []layout.Item, g pageGeom) []layout.Item {
	marks := strings.Fields(g.used.Marks)
	wantCrop, wantCross := false, false
	for _, m := range marks {
		switch m {
		case "crop":
			wantCrop = true
		case "cross":
			wantCross = true
		}
	}
	if !wantCrop && !wantCross {
		return items
	}
	b := g.bleed
	// Trim box corners in media space.
	left, top := b, b
	right, bottom := b+g.pageW, b+g.pageH

	rule := func(x, y, w, h float64) {
		items = append(items, layout.Item{
			Kind: layout.BackgroundKind,
			Rule: layout.RuleItem{XPt: x, YPt: y, WPt: w, HPt: h, Color: color.RGBA{0, 0, 0, 255}},
		})
	}

	if wantCrop {
		// Each corner: one horizontal mark and one vertical mark, both in the bleed band,
		// offset from the trim edges by markGap, length markLength toward the page edge.
		// Top-left corner.
		rule(left-markGap-markLength, top, markLength, markThickness)        // horizontal, left of corner
		rule(left, top-markGap-markLength, markThickness, markLength)        // vertical, above corner
		// Top-right.
		rule(right+markGap, top, markLength, markThickness)
		rule(right-markThickness, top-markGap-markLength, markThickness, markLength)
		// Bottom-left.
		rule(left-markGap-markLength, bottom-markThickness, markLength, markThickness)
		rule(left, bottom+markGap, markThickness, markLength)
		// Bottom-right.
		rule(right+markGap, bottom-markThickness, markLength, markThickness)
		rule(right-markThickness, bottom+markGap, markThickness, markLength)
	}

	if wantCross {
		// A plus centered on each edge midpoint, drawn in the bleed band straddling the
		// page edge. Plus arm length markLength, centered.
		cx := (left + right) / 2
		cy := (top + bottom) / 2
		half := markLength / 2
		// midTop: centered at (cx, top-bleed/2) — in the top bleed band.
		midY := top - b/2
		rule(cx-half, midY-markThickness/2, markLength, markThickness)
		rule(cx-markThickness/2, midY-half, markThickness, markLength)
		// midBottom.
		midY = bottom + b/2
		rule(cx-half, midY-markThickness/2, markLength, markThickness)
		rule(cx-markThickness/2, midY-half, markThickness, markLength)
		// midLeft.
		midX := left - b/2
		rule(midX-half, cy-markThickness/2, markLength, markThickness)
		rule(midX-markThickness/2, cy-half, markThickness, markLength)
		// midRight.
		midX = right + b/2
		rule(midX-half, cy-markThickness/2, markLength, markThickness)
		rule(midX-markThickness/2, cy-half, markThickness, markLength)
	}
	return items
}
```

NOTE to implementer: the test `TestAppendCropMarks` asserts exactly 8 rules for `crop` and that none lie inside the trim box. The horizontal top-left mark is at `x = left-markGap-markLength` (entirely left of `left`), good. Verify each of the 8 crop rects is outside `[left,top]..[right,bottom]`; if any boundary rect grazes the trim box (e.g. a mark whose y == top and height extends down into the box), nudge it fully into the band. The `cross` test asserts 8 rules (a plus at 4 midpoints) — adjust the count if you draw a different mark shape, keeping the test in sync.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestAppendCropMarks|TestAppendCrossMarks|TestNoMarksNoItems' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/layout/css/cropmarks.go pkg/layout/css/cropmarks_test.go
git add pkg/layout/css/cropmarks.go pkg/layout/css/cropmarks_test.go
git commit -m "feat(css): emit @page crop + cross registration marks"
```

---

## Task 4: Wire bleed shift + marks into page assembly

**Files:**
- Modify: `pkg/layout/css/paginate.go` (`assemblePages`)
- Test: `pkg/layout/css/pagemodel_test.go` (end-to-end via LayoutPagedDoc)

- [ ] **Step 1: Write the failing test**

Add to `pkg/layout/css/pagemodel_test.go`:

```go
func TestPagedDocBleedShiftsAndMarks(t *testing.T) {
	src := `<html><head><style>
		@page { size: 200px 200px; bleed: 10px; marks: crop }
	</style></head><body>
		<div style="height:120px;margin:0;background:rgb(7,7,7)">x</div>
	</body></html>`
	cfg := pagedConfigFor(`@page { size: 200px 200px; bleed: 10px; marks: crop }`, 200, 200, false)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	p := pages.Pages[0]
	// The page bitmap is the MEDIA box: 220x220 (trim 200 + 2*10 bleed).
	if p.WidthPt != 220 || p.HeightPt != 220 {
		t.Errorf("page size = %.0fx%.0f, want 220x220 (media box)", p.WidthPt, p.HeightPt)
	}
	// The content block is shifted by (bleed,bleed) = (10,10): its background paints at x≥10.
	bg := firstBackground(p.Items, color.RGBA{7, 7, 7, 255})
	if bg == nil {
		t.Fatalf("content background not found")
	}
	if bg.XPt < 9.5 || bg.YPt < 9.5 {
		t.Errorf("content at (%.1f,%.1f), want shifted by bleed (≥10,≥10)", bg.XPt, bg.YPt)
	}
	// Crop marks present: ≥8 black hairline rules.
	black := color.RGBA{0, 0, 0, 255}
	marks := 0
	for _, it := range p.Items {
		if it.Kind == layout.BackgroundKind && it.Rule.Color == black && it.Rule.HPt <= 1 || (it.Kind == layout.BackgroundKind && it.Rule.Color == black && it.Rule.WPt <= 1) {
			marks++
		}
	}
	if marks < 8 {
		t.Errorf("crop mark rules = %d, want ≥8", marks)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestPagedDocBleedShiftsAndMarks -v`
Expected: FAIL (page is 200x200, content not shifted, no marks).

- [ ] **Step 3: Wire into `assemblePages`**

In `pkg/layout/css/paginate.go`, find `assemblePages`. Currently, near the end of its per-page loop it does (paraphrase):
```go
		if g.marginL != 0 || g.marginT != 0 {
			translateFragment(&pageRoot, g.marginL, g.marginT)
		}
		pg := pageRoot.Page(g.pageW, g.pageH)
		pg.Items = e.appendMarginBoxes(pg.Items, g, i, len(buckets))
		pages = append(pages, pg)
```

Change it so the bleed offset is added to the content translate, the page is sized to the MEDIA box, and crop marks are appended:

```go
		// Content is inset by the @page margins AND, when bleeding, shifted inward by the
		// bleed so the trim box sits at (bleed,bleed) within the larger media-box bitmap.
		dx, dy := g.marginL+g.bleed, g.marginT+g.bleed
		if dx != 0 || dy != 0 {
			translateFragment(&pageRoot, dx, dy)
		}
		// The page bitmap is the MEDIA box (trim + bleed all sides); with no bleed this is
		// the trim box, byte-identical to before.
		pg := pageRoot.Page(g.mediaW(), g.mediaH())
		// Margin boxes (running headers/footers) are positioned in the trim-box frame, so
		// they too must shift by the bleed. appendMarginBoxes computes rects from g (trim
		// frame); shift the items it adds by (bleed,bleed). Simplest: append them, then
		// translate only the newly-added items by the bleed.
		before := len(pg.Items)
		pg.Items = e.appendMarginBoxes(pg.Items, g, i, len(buckets))
		if g.bleed != 0 {
			translateItems(pg.Items, before, g.bleed, g.bleed)
		}
		// Registration marks draw in the bleed band, in MEDIA-box coordinates (no shift).
		pg.Items = appendCropMarks(pg.Items, g)
		pages = append(pages, pg)
```

NOTE to implementer: verify `translateItems(dst []layout.Item, start int, dx, dy float64)` exists in `pkg/layout/css/fragment.go` (it does — used by relative positioning). It translates items[start:] by (dx,dy). If its signature differs, adapt. The `appendCropMarks` items are in media space already, so they are NOT shifted.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run TestPagedDocBleedShiftsAndMarks -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite (byte-identical guard)**

Run: `go test ./pkg/layout/css/ ./pkg/doctaculous/`
Expected: ok. The existing paged-media goldens (no bleed/marks) MUST be unchanged — `g.bleed == 0` makes `mediaW/H == pageW/H` and `appendCropMarks` a no-op, so the page bytes are identical. If any existing golden diffs, STOP — the no-bleed path was perturbed.

- [ ] **Step 6: Commit**

```bash
gofmt -w pkg/layout/css/paginate.go pkg/layout/css/pagemodel_test.go
git add pkg/layout/css/paginate.go pkg/layout/css/pagemodel_test.go
git commit -m "feat(css): grow page to media box, shift content by bleed, draw @page marks"
```

---

## Task 5: End-to-end golden

**Files:**
- Modify: `pkg/doctaculous/pagedmedia_golden_test.go`

- [ ] **Step 1: Add a marks+bleed golden fixture**

Add to `pagedMediaGoldens`:

```go
	{
		// @page bleed + crop marks: the page bitmap is the media box (trim 300x220 + 16px
		// bleed on each side ⇒ 332x252); content sits inside the trim box; thin black crop
		// marks point at the four trim corners in the bleed band. Eyeball: 8 corner marks,
		// content inset, white bleed margin around a single page.
		name:    "page-crop-marks",
		wantPgs: 1,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 300px 220px; margin: 20px; bleed: 16px; marks: crop cross }
  body { margin: 0 }
</style></head><body><div style="height:180px;background:#bcd6f0">trim box content</div></body></html>`,
	},
```

- [ ] **Step 2: Generate + EYEBALL**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden -update`
Then READ `pkg/doctaculous/testdata/golden/html-page-crop-marks-p0.png` and confirm:
- the image is larger than the trim box (a white bleed margin surrounds the content);
- 8 thin black L-shaped crop marks sit just outside the four corners of the content/trim box, NOT touching it;
- 4 small plus-shaped cross marks at the edge midpoints (from `cross`);
- the blue content box is inset from the trim edges by the 20px margin.

If marks overlap the content or are missing, STOP and report (fix `cropmarks.go` offsets).

- [ ] **Step 3: Confirm existing goldens unchanged**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden`
Expected: PASS — only the new `html-page-crop-marks-p0.png` is added; `page-margins`, `widows-orphans`, `page-three-headers` unchanged (their `git status` should show no modification).

- [ ] **Step 4: Commit**

```bash
git add pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/html-page-crop-marks-p0.png
git commit -m "test(css): @page crop-marks + bleed golden"
```

---

## Sign-off ledger note

Because crop marks are now IMPLEMENTED (not deferred), the original ledger row #1
(`@page marks/bleed RENDERING ... signed-off deferral`) is NOT created. Instead, record
in `docs/paged-media-deferral-signoffs.md` (under a "Resolved (implemented, not deferred)"
note) that the owner chose to implement crop/cross marks, so this is no longer a deferral.
The only marks-related deferral, if any, is **circled-cross registration marks** (we draw a
plain plus, not the printer's circle-plus) — a cosmetic sub-deferral; create a signed row
for it only if the owner wants the circle.

## What this sub-plan deliberately does NOT do

- **Mid-content bleed** (artwork actually extending into the bleed area): content is shifted
  but not stretched into the bleed; a full-bleed background would need the page background to
  paint to the media box. A follow-up if needed.
- **Per-page-side marks** (different marks per `:left`/`:right`): marks are uniform.
- **`marks` on the screen-output default** (no `WithDefaultPaged`/`@page`): unaffected — no marks without an `@page` rule.
