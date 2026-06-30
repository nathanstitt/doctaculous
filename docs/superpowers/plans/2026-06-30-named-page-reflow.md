# Named-Page Multi-Width Reflow — Implementation Sub-Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** A top-level block carrying `page: <name>` that selects a differently-**sized** named `@page` rule lays its content out at THAT page's content width (not the default), so a portrait document can contain a landscape data-table section that actually reflows to the wider page. Replaces the original "Task 11" (deferred approximation) of `2026-06-30-paged-media-deferrals.md` — the owner chose to implement it.

**Architecture:** Today `LayoutPagedDoc` lays the whole document out ONCE at page 0's content width (`layoutTree(root, g0.contentW)`), then `paginateDoc` buckets `body.Children`. This sub-plan groups the document's top-level blocks into **consecutive runs sharing a resolved page name** (a `page` change between two adjacent blocks forces a break, CSS GCPM), lays out the **whole document once per DISTINCT content width** present, picks each run's block fragments from the layout matching that run's width, buckets each run against its own page geometry, and concatenates the resulting page lists in document order. The single-width case (every block resolves to the same width — the overwhelming default, since `page:` is rarely used) is byte-identical to today: one layout, one bucketing pass.

**Tech Stack:** Go stdlib only. `pkg/layout/css/pagemodel.go` (the driver), `pkg/css` (already parses `page` onto `ComputedStyle.Page`). Golden test.

**Key facts (verified):**
- `ComputedStyle.Page` is parsed (lowercased) but currently READ NOWHERE in layout — `resolvePageGeom` is always called with name `""`. So named-page SIZE currently has no effect; this sub-plan is the first reader.
- `layoutTree(ctx, root, viewportW) *Fragment` lays out the whole `root` cssbox tree at one width.
- `bodyFragment(root)` returns the body fragment; `body.Children` are the top-level blocks; each block fragment carries `.Box` (the source `*cssbox.Box`) whose `.Style.Page` is the resolved name.
- `cfg.resolvePageGeom(i, name, blank) pageGeom` already takes a name and resolves the named `@page` (size/margins/marginboxes) — it just is never called with a non-empty name yet.

---

## Design: the multi-width algorithm

A **run** is a maximal consecutive sub-sequence of `body.Children` whose blocks resolve to the same page **name** (and therefore the same content width). Runs partition the blocks in document order. For each run:
- its **content width** = `cfg.resolvePageGeom(0, runName, false).contentW`;
- lay out the whole document at that width (cached per distinct width), find the body, take the run's blocks BY INDEX from that layout;
- bucket those blocks against the run's page geometry (size/height/margins/marginboxes resolved with `runName`);
- the run's pages append to the document's page list, page-indexed continuing from the prior run (so `counter(page)` and string snapshots are global).

**Page numbering + running content across runs:** the page counter and string snapshots must be GLOBAL (page 3 of 10 even if it's in run 2). So bucketing per run produces buckets, but the page-number / string-snapshot resolution happens once over the CONCATENATED bucket list. Structure: collect every run's buckets (each carrying its `pageGeom`), concatenate, THEN build string snapshots + assemble pages over the full list with a per-bucket geometry.

**Why lay out the whole doc per width (not a sub-tree):** building a partial cssbox sub-tree per run and laying it out separately would lose cross-block context (margin collapsing across the run boundary is already broken by the forced page break, so that's fine) BUT re-deriving a sub-tree is fiddlier and riskier than just laying the whole doc out at each width and slicing. Distinct widths are few (usually 1, occasionally 2). The cost is N layouts for N distinct widths — acceptable.

---

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `pkg/layout/css/pagemodel.go` | run grouping; multi-width layout; per-run bucketing; stitch | 1, 2, 3 |
| `pkg/layout/css/namedpage_test.go` (new) | run grouping + end-to-end width tests | 1, 2 |
| `pkg/doctaculous/pagedmedia_golden_test.go` | landscape-section golden | 3 |

---

## Task 1: Resolve each top-level block's page name + group into runs

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (add `blockPageName`, `groupRuns`)
- Test: `pkg/layout/css/namedpage_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `pkg/layout/css/namedpage_test.go`:

```go
package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

func blockNamed(name string) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
	box.Style = gcss.ComputedStyle{Page: name}
	return &Fragment{Box: box, H: 10}
}

func TestGroupRuns(t *testing.T) {
	// Blocks: "", "", "wide", "wide", "" ⇒ three runs: [0,1] default, [2,3] wide, [4] default.
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("wide"), blockNamed("wide"), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3: %+v", len(runs), runs)
	}
	if runs[0].name != "" || runs[0].start != 0 || runs[0].end != 2 {
		t.Errorf("run0 = %+v, want {name:\"\" start:0 end:2}", runs[0])
	}
	if runs[1].name != "wide" || runs[1].start != 2 || runs[1].end != 4 {
		t.Errorf("run1 = %+v, want {name:wide start:2 end:4}", runs[1])
	}
	if runs[2].name != "" || runs[2].start != 4 || runs[2].end != 5 {
		t.Errorf("run2 = %+v, want {name:\"\" start:4 end:5}", runs[2])
	}
}

func TestGroupRunsSingle(t *testing.T) {
	// All same name ⇒ one run spanning everything (the byte-identical default case).
	blocks := []*Fragment{blockNamed(""), blockNamed(""), blockNamed("")}
	runs := groupRuns(blocks)
	if len(runs) != 1 || runs[0].start != 0 || runs[0].end != 3 {
		t.Fatalf("single-name should be one run [0,3); got %+v", runs)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run (dangerouslyDisableSandbox: true): `go test ./pkg/layout/css/ -run TestGroupRuns -v`
Expected: FAIL ("undefined: groupRuns").

- [ ] **Step 3: Implement `blockPageName` + `groupRuns`**

In `pkg/layout/css/pagemodel.go`:

```go
// pageRun is a maximal consecutive run of top-level blocks sharing a resolved page
// name (and therefore a content width). start/end are indices into body.Children
// (half-open [start,end)).
type pageRun struct {
	name       string
	start, end int
}

// blockPageName returns a block fragment's resolved CSS `page` name ("" = the default,
// un-named page). A nil Box reads as default.
func blockPageName(f *Fragment) string {
	if f == nil || f.Box == nil {
		return ""
	}
	return f.Box.Style.Page
}

// groupRuns partitions blocks into maximal consecutive runs sharing a page name. A page
// name change between adjacent blocks starts a new run (CSS GCPM: a `page` change forces
// a break). Returns one run spanning everything when all blocks share a name (the common
// single-width case).
func groupRuns(blocks []*Fragment) []pageRun {
	var runs []pageRun
	for i := 0; i < len(blocks); {
		name := blockPageName(blocks[i])
		j := i + 1
		for j < len(blocks) && blockPageName(blocks[j]) == name {
			j++
		}
		runs = append(runs, pageRun{name: name, start: i, end: j})
		i = j
	}
	return runs
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run TestGroupRuns -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w pkg/layout/css/pagemodel.go pkg/layout/css/namedpage_test.go
git add pkg/layout/css/pagemodel.go pkg/layout/css/namedpage_test.go
git commit -m "feat(css): group top-level blocks into per-named-page runs"
```

---

## Task 2: Multi-width layout + per-run bucketing + stitch

This rewrites `LayoutPagedDoc`/`paginateDoc` to lay out per distinct width and bucket per run. The single-width path stays byte-identical.

**Files:**
- Modify: `pkg/layout/css/pagemodel.go` (`LayoutPagedDoc`, `paginateDoc`)
- Test: `pkg/layout/css/namedpage_test.go`

- [ ] **Step 1: Write the failing end-to-end test**

Add to `pkg/layout/css/namedpage_test.go`:

```go
import (
	"context"
	"image/color"
	"math"
	// ... plus the existing imports
)

func TestNamedPageWidthReflow(t *testing.T) {
	// Default page 200 wide; a `.wide` section uses @page wide { size: 400px ... }. The
	// wide section's full-width block must be laid out at the wide content width (~360px
	// after 20px margins), NOT the default 160px. Two sections so we see both widths.
	src := `<html><head><style>
		@page { size: 200px 300px; margin: 20px }
		@page wide { size: 440px 300px; margin: 20px }
		.wide { page: wide }
		div { margin: 0 }
	</style></head><body>
		<div style="height:50px;background:rgb(1,1,1)">narrow</div>
		<div class="wide" style="height:50px;background:rgb(2,2,2)">wide</div>
	</body></html>`
	cfg := pagedConfigFor(`
		@page { size: 200px 300px; margin: 20px }
		@page wide { size: 440px 300px; margin: 20px }
	`, 200, 300, false)
	root := buildRoot(t, src, nil)
	pages, err := New(nil, nil, nil).LayoutPagedDoc(context.Background(), root, cfg)
	if err != nil {
		t.Fatalf("LayoutPagedDoc: %v", err)
	}
	if len(pages.Pages) != 2 {
		t.Fatalf("want 2 pages (page change forces a break), got %d", len(pages.Pages))
	}
	// Page 0 is the narrow page (200 wide); its block fills the 160px content width.
	if math.Abs(pages.Pages[0].WidthPt-200) > 0.5 {
		t.Errorf("page 0 width = %.0f, want 200 (default)", pages.Pages[0].WidthPt)
	}
	narrow := firstBackground(pages.Pages[0].Items, color.RGBA{1, 1, 1, 255})
	if narrow == nil || math.Abs(narrow.WPt-160) > 1 {
		t.Errorf("narrow block width = %v, want 160 (200-2*20)", narrow)
	}
	// Page 1 is the wide page (440 wide); its block fills the 400px content width.
	if math.Abs(pages.Pages[1].WidthPt-440) > 0.5 {
		t.Errorf("page 1 width = %.0f, want 440 (wide)", pages.Pages[1].WidthPt)
	}
	wide := firstBackground(pages.Pages[1].Items, color.RGBA{2, 2, 2, 255})
	if wide == nil || math.Abs(wide.WPt-400) > 1 {
		t.Errorf("wide block width = %v, want 400 (440-2*20)", wide)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/layout/css/ -run TestNamedPageWidthReflow -v`
Expected: FAIL — today both blocks lay out at the default 160px width and the wide block is not 400px; page 1 width may also be wrong.

- [ ] **Step 3: Rewrite `LayoutPagedDoc` to drive multi-width**

Replace `LayoutPagedDoc`'s tail (from `g0 := cfg.resolvePageGeom(...)` onward) so it groups runs, lays out per distinct width, and delegates to a new `paginateRuns`. Keep the `!cfg.Paged`, recover, and canvas-background blocks unchanged.

```go
	// Lay out once at the default width to discover the top-level block list + each
	// block's resolved page name (the cssbox tree is the same regardless of width, so the
	// run grouping is width-independent; only the per-run geometry differs).
	g0 := cfg.resolvePageGeom(0, "", false)
	base := e.layoutTree(ctx, root, g0.contentW)
	if base == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: g0.pageW, HeightPt: g0.pageH}}}, nil
	}
	body := bodyFragment(base)
	if body == nil || len(body.Children) == 0 {
		// No top-level blocks: single page (with margin boxes), as paginateDoc did.
		page := base.Page(g0.pageW, g0.pageH)
		page.Items = e.appendMarginBoxes(page.Items, g0, 0, 1, pageStrings{})
		return &layout.Pages{Pages: []layout.Page{page}}, nil
	}
	runs := groupRuns(body.Children)
	// Fast path: a single run at the default name ⇒ the existing single-width pipeline,
	// byte-identical. (groupRuns returns one run named "" when no block sets `page`.)
	if len(runs) == 1 && runs[0].name == "" {
		return e.paginateDoc(base, cfg), nil
	}
	return e.paginateRuns(ctx, root, base, cfg, runs), nil
```

- [ ] **Step 4: Implement `paginateRuns`**

Add to `pkg/layout/css/pagemodel.go`. It lays out the doc once per distinct run width, buckets each run from its width's layout, concatenates buckets (carrying per-bucket geometry), then assembles + numbers pages globally.

```go
// runBucket is a bucket plus the page geometry its run resolved to (size/margins/
// marginboxes for that run's named page), so the global assembly can size + inset + chrome
// each page correctly even though pages come from different runs.
type runBucket struct {
	bucket pageBucket
	geom   pageGeom
}

// paginateRuns paginates a document whose top-level blocks resolve to more than one
// named page (different content widths). It lays the document out once per DISTINCT run
// content width, takes each run's block fragments from the layout matching its width,
// buckets each run against its own page geometry, then assembles + numbers all pages
// globally (so counter(page)/string() are document-wide).
func (e *Engine) paginateRuns(ctx context.Context, root *cssbox.Box, base *Fragment, cfg PagedConfig, runs []pageRun) *layout.Pages {
	// Cache one full-document layout per distinct content width.
	layoutByWidth := map[float64]*Fragment{}
	bodyByWidth := map[float64][]*Fragment{}
	getLayout := func(name string) (geom pageGeom, blocks []*Fragment) {
		geom = cfg.resolvePageGeom(0, name, false)
		w := geom.contentW
		frag, ok := layoutByWidth[w]
		if !ok {
			if math.Abs(w-contentWidth(bodyFragment(base), cfgFallback(cfg))) < 0.01 {
				frag = base // reuse the base layout when the width matches it
			} else {
				frag = e.layoutTree(ctx, root, w)
			}
			layoutByWidth[w] = frag
			bodyByWidth[w] = nil
			if b := bodyFragment(frag); b != nil {
				bodyByWidth[w] = b.Children
			}
		}
		return geom, bodyByWidth[w]
	}

	var all []runBucket
	for _, r := range runs {
		geom, blocks := getLayout(r.name)
		// Take this run's blocks BY INDEX from the matching-width layout. The cssbox tree
		// is identical across widths, so body.Children indices align run-for-run.
		if r.end > len(blocks) {
			continue // defensive: layout produced fewer blocks (shouldn't happen)
		}
		runBlocks := blocks[r.start:r.end]
		cb := contentWidth(bodyFragment(layoutByWidth[geom.contentW]), geom.contentW)
		bks := bucketBlocks(runBlocks, geom.contentH, cb, e.logf)
		for _, bk := range bks {
			all = append(all, runBucket{bucket: bk, geom: geom})
		}
	}
	if len(all) == 0 {
		g0 := cfg.resolvePageGeom(0, "", false)
		return &layout.Pages{Pages: []layout.Page{{WidthPt: g0.pageW, HeightPt: g0.pageH}}}
	}

	// Global string snapshots over the concatenated bucket list.
	buckets := make([]pageBucket, len(all))
	for i := range all {
		buckets[i] = all[i].bucket
	}
	snaps := buildStringSnapshots(buckets)

	pages := make([]layout.Page, 0, len(all))
	for i := range all {
		bk := all[i].bucket
		g := all[i].geom
		// Shift this bucket's blocks to local Y 0 (each run's blocks are in their own
		// layout's page space).
		shiftFragments(bk.blocks, -bk.top)
		// Build a minimal page root carrying just this bucket's blocks. We synthesize a
		// shallow body wrapper so AppendItems flattens the blocks; the per-run base body
		// is reused for its box/styling.
		pageRoot := *base
		runBody := *bodyFragment(layoutByWidth[g.contentW])
		runBody.Children = bk.blocks
		runBody.Positioned, runBody.PositionedInfo, runBody.Floats = nil, nil, nil
		children := make([]*Fragment, len(base.Children))
		copy(children, base.Children)
		children[len(children)-1] = &runBody
		pageRoot.Children = children
		pageRoot.Positioned, pageRoot.PositionedInfo, pageRoot.Floats = nil, nil, nil

		dx, dy := g.marginL+g.bleed, g.marginT+g.bleed
		if dx != 0 || dy != 0 {
			translateFragment(&pageRoot, dx, dy)
		}
		pg := pageRoot.Page(g.mediaW(), g.mediaH())
		before := len(pg.Items)
		pg.Items = e.appendMarginBoxes(pg.Items, g, i, len(all), snaps[i])
		if g.bleed != 0 {
			translateItems(pg.Items, before, g.bleed, g.bleed)
		}
		pg.Items = appendCropMarks(pg.Items, g)
		pages = append(pages, pg)
	}
	return &layout.Pages{Pages: pages}
}

// cfgFallback returns the content width the base layout was built at (page 0 default
// content width), used to detect when a run's width matches the base layout so it can be
// reused instead of re-laid-out.
func cfgFallback(cfg PagedConfig) float64 {
	return cfg.resolvePageGeom(0, "", false).contentW
}
```

NOTE to implementer: this `paginateRuns` is a SIMPLIFIED assembler — it does NOT distribute positioned/float layers per run (out-of-flow content in a named-page section is rare; it rides its run's first page via the base layout, or is dropped if it bubbled to the root). Document this as a sub-deferral (Task 3 ledger row). The mainline single-width path (`paginateDoc`) keeps full positioned/float distribution. If the byte-identical guard or a positioned test fails, confirm the fast-path (`len(runs)==1 && name==""`) is taken for all existing tests — it must be, since none use `page:`.

Also verify these helpers exist and match: `contentWidth(f, cbWidth)`, `shiftFragments`, `translateFragment`, `translateItems`, `appendMarginBoxes(items, g, idx, count, snap)`, `appendCropMarks`, `mediaW/mediaH`, `bodyFragment`. Add `import "math"` if not present.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/layout/css/ -run 'TestNamedPage|TestGroupRuns' -v`
Expected: PASS.

- [ ] **Step 6: BYTE-IDENTICAL GUARD — the whole suite**

Run: `go test ./pkg/layout/css/ ./pkg/doctaculous/`
Expected: ok. Every existing test/golden takes the single-width fast path (none use `page:`), so they MUST be unchanged. If any diffs, STOP — the fast-path guard is not catching an existing case.

- [ ] **Step 7: Lint**

Run: `golangci-lint run ./pkg/layout/css/`
Expected: 0 issues (no unused helpers — `cfgFallback`/`paginateRuns`/`runBucket` are all used).

- [ ] **Step 8: Commit**

```bash
gofmt -w pkg/layout/css/pagemodel.go pkg/layout/css/namedpage_test.go
git add pkg/layout/css/pagemodel.go pkg/layout/css/namedpage_test.go
git commit -m "feat(css): multi-width named-page reflow (lay out per @page width, stitch)"
```

---

## Task 3: End-to-end golden + sub-deferral ledger

**Files:**
- Modify: `pkg/doctaculous/pagedmedia_golden_test.go`
- Modify: `docs/paged-media-deferral-signoffs.md`

- [ ] **Step 1: Add a landscape-section golden**

Add to `pagedMediaGoldens`:

```go
	{
		// Named-page reflow: a portrait document with a `.land` section that selects
		// @page land { size: landscape }. Page 0 (portrait) holds the intro at the narrow
		// width; page 1 (landscape) holds the wide section reflowed to the wider content
		// box. Eyeball: page 1 is WIDER than page 0, and its block fills the wide width.
		name:    "named-page",
		wantPgs: 2,
		html: `<!DOCTYPE html><html><head><style>
  @page { size: 300px 240px; margin: 20px }
  @page land { size: 460px 240px; margin: 20px }
  .land { page: land }
  div { margin: 0 }
</style></head><body>
  <div style="height:160px;background:#f0c0c0">Portrait intro section</div>
  <div class="land" style="height:160px;background:#c0c0f0">Landscape wide section</div>
</body></html>`,
	},
```

- [ ] **Step 2: Generate + EYEBALL**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden -update`
Restore any RE-ENCODED pre-existing PNG (`git checkout` them; they pass unchanged). Then READ `html-named-page-p0.png` and `html-named-page-p1.png`:
- p0 is the narrower portrait page; the pink block fills its content width.
- p1 is the WIDER landscape page; the blue block fills the wider content width (visibly wider than p0's block).
If p1 is not wider, or its block didn't reflow, STOP and report.

- [ ] **Step 3: Confirm existing goldens unchanged**

Run: `go test ./pkg/doctaculous/ -run TestHTMLPagedMediaGolden`
Expected: PASS (only `html-named-page-p{0,1}.png` added).

- [ ] **Step 4: Add the sub-deferral ledger row**

Append to the "## Deferred (owner-signed)" table in `docs/paged-media-deferral-signoffs.md`:

```markdown
| 3 | Positioned/float distribution WITHIN a named-page (different-width) run | Multi-width named-page reflow lays out + paginates per @page width; out-of-flow (abs/fixed/float) content inside a differently-sized named-page section is not distributed per run (rides the run's layout / may drop). The mainline single-width path retains full positioned/float distribution. | Nathan | 2026-06-30 |
```

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/pagedmedia_golden_test.go pkg/doctaculous/testdata/golden/html-named-page-p0.png pkg/doctaculous/testdata/golden/html-named-page-p1.png docs/paged-media-deferral-signoffs.md
git commit -m "test(css): named-page landscape-section golden + sub-deferral sign-off"
```

---

## What this sub-plan deliberately does NOT do

- **Mid-run page-size change** (a `page:` change INSIDE a block, not between top-level blocks): only top-level block boundaries switch pages (consistent with the between-blocks fragmentation model).
- **Positioned/float distribution across different-width runs** (the signed sub-deferral above).
- **Margin collapsing across a run boundary**: a page break already cancels it (correct).
