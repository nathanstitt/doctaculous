# Pagination / fixed-height page fragmentation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the HTML engine's single tall page into fixed-height pages. Add `WithPageSize(w, h)` to opt in; with it, `engine.LayoutPaged` fragments the laid-out fragment tree into `h`-tall `layout.Page`s, breaking **between top-level in-flow block fragments** and at **forced page breaks** (`break-before`/`break-after: page|always` + the `page-break-*` aliases). Without `WithPageSize`, output is **byte-identical** to today (a single tall page).

**Architecture:** A **post-pass** over the finished fragment tree — no change to the fragment-tree builder, the flatten, `render.Device`, the PDF/DOCX pipelines, or the shared inline core. Three touch points: (1) `pkg/css` — two `ComputedStyle` break fields + cascade; (2) `pkg/layout/css` — a new `paginate.go` (the pass + a `LayoutPaged` entry that delegates to the existing `Layout` when `pageH <= 0`); (3) `pkg/doctaculous` — `WithPageSize` + `LetterWidthPt`/`LetterHeightPt` + threading a page height through `htmlConfig`. The pass walks `body.Children` (top-level blocks; positioned/float content is lifted out of `Children` into the root's `Positioned`/`Floats`, so the walk only sees in-flow blocks — verified), buckets blocks into pages by accumulated height + forced breaks, shifts each page's blocks to local Y 0 via the existing `shiftFragment`, wraps them in a shallow-cloned root/body, and flattens per page.

**Tech Stack:** Go stdlib only. **No new dependency.**

**Spec:** `docs/superpowers/specs/2026-06-28-html-pagination-design.md`.

---

## Process rules for the implementer (carried from sub-projects 1–11 — read before starting)

- **Branch:** you are on `feat/html-pagination` (off `feat/html-openurl`). Do **NOT** checkout/stash/switch branches. Do **NOT** commit unless a step says to. **Scope every `git add` to the exact files named** — NEVER `git add -A` / `git add .` (the repo may have unrelated dirty docs; do not touch them). Run `git status` + `git show --stat` to confirm scope before any commit a step asks for.
- **Sandbox blocks the Go build cache:** run every `go` / `gofmt` / `golangci-lint` command with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with cache/permission/"no go files to analyze" errors that are NOT real failures; re-run disabled. (Pagination tests are pure local — no network — but the **build cache** still needs the flag.)
- **Editor diagnostics lag badly** — after you add a field/file you may see stale "undefined"/"unused"/"redeclared" errors and **phantom** `zz_*` / `*probe*` scratch files (often in packages you never touched). Trust `go build` / `go test` / `golangci-lint` / `find . -name 'zz_*' -o -name '*probe*'`, NOT the editor panel. Delete any scratch you create before finishing; confirm `find` is empty and `git status` is clean.
- **Lint:** run `golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...` (specific packages, not the repo root). **NO `//nolint`.** The repo **declines all "modernize" hints** — keep explicit `if x < y { x = y }` clamps and indexed `for i := 0; i < n; i++` loops; do NOT introduce `slices.*` / `maps.*` / `min()` / `max()` builtins / range-over-int / `SplitSeq`. Keep `sort.SliceStable`. golangci-lint here does **NOT** run gofmt — run `gofmt -l` on changed files separately and fix any it lists. errcheck wants `_ = x.Close()` for ignored errors. The **`unused` linter IS enforced**: a field/const/function/**exported symbol** you add must be *read* by code in the same PR — the `ComputedStyle` break fields must be read by the pagination pass, and `LetterWidthPt`/`LetterHeightPt` must be read by a test, all in this PR.
- **Verify against the code, don't trust the plan blindly.** Two facts the spec leans on were verified at spec time but **re-confirm against the CURRENT code** before relying on them: (a) positioned boxes are appended to a stacking context's `Positioned` slice and floats to `Floats`, NOT to the parent's `Children` (so `body.Children` is in-flow blocks only — `block.go:667-674`, `:87-88`); (b) `shiftFragment` (`block.go:1178`) recurses `Children` + `Floats` + `Lines` + `Image` but **NOT** `Positioned`. If either has changed, STOP and re-derive — do not paper over it by editing a passing test.
- **A change that forces inverting a passing test is a red flag** — re-verify the algorithm against the spec, don't edit the test to match a wrong implementation.
- **Hermetic + fast:** all fixtures are inline HTML strings; no network, no committed binaries except the new `html-paginate-*` golden PNGs (keep them small). Run `go test -race ./pkg/layout/css/... ./pkg/doctaculous/...` at the end.

---

## File Structure

- **Modify `pkg/css/cascade.go`** — add `BreakBefore`/`BreakAfter` to `ComputedStyle`; initialize to `""`; cascade `break-before`/`break-after` + `page-break-before`/`page-break-after` in `applyDeclaration`.
- **Modify `pkg/css/cascade_test.go`** (or the nearest existing cascade test file) — break-property cascade tests.
- **Create `pkg/layout/css/paginate.go`** — `LayoutPaged`, `paginate`, `isForcedBreak`, `bodyFragment`, `breakBefore`/`breakAfter`, `clonePageRoot`. One responsibility: turn the finished root fragment into one-or-many `layout.Page`s.
- **Create `pkg/layout/css/paginate_test.go`** — pagination-pass unit tests (page bucketing, forced breaks, over-tall degradation, the `pageH<=0` passthrough, `isForcedBreak`).
- **Modify `pkg/doctaculous/html_backend.go`** — `WithPageSize`, `LetterWidthPt`/`LetterHeightPt`, `htmlConfig.pageHeightPt`, and call `LayoutPaged`.
- **Create `pkg/doctaculous/pagination_test.go`** — end-to-end `WithPageSize` page-count + non-blank-page tests and the byte-identical-default guard.
- **Add golden fixture + PNGs** — a `html-paginate-*` fixture (match the existing `html-*` golden convention; find where the `html-*` goldens are registered) rendered with `WithPageSize`, producing multiple page PNGs.
- **Modify `CLAUDE.md`** (final task) — move sub-project 12 to "Done", update the §6 done-slices parenthetical, add fidelity follow-ups.

No change to `render.Device`, `pkg/render/raster`, `pkg/layout/paint`, the PDF pipeline, the DOCX pipeline, or `pkg/layout/inline`.

---

### Task 1: break properties on the cascade (`pkg/css`)

The smallest, most isolated piece — a cascade-only change with no layout effect. Land it first so the pagination pass (Task 2) has fields to read.

**Files:**
- Modify: `pkg/css/cascade.go`
- Modify: the cascade test file (find it: `grep -l "applyDeclaration\|func TestCascade\|func TestApply" pkg/css/*_test.go`)

- [ ] **Step 1: Write failing cascade tests**

Add tests asserting:
- `break-before: page` → `ComputedStyle.BreakBefore == "page"`.
- `break-after: always` → `BreakAfter == "always"`.
- `page-break-before: always` (legacy) → `BreakBefore == "always"`.
- `page-break-after: avoid` (legacy) → `BreakAfter == "avoid"`.
- A value is stored **lowercased** (`break-before: PAGE` → `"page"`) if the cascade lowercases other keyword values — match what `Overflow`/`display` do; if they store raw, store raw and assert raw. (Check `applyDeclaration`'s existing behavior for `case "overflow"` and mirror it exactly.)
- Default (no declaration) → `BreakBefore == "" && BreakAfter == ""`.
- Modern-vs-legacy precedence: with both `page-break-before: avoid; break-before: page` in one rule, the **last** wins (`"page"`) — they target the same field, so normal source-order cascade applies (no test of specificity needed beyond same-rule order).

Run; confirm they fail to compile (fields don't exist) / fail.

- [ ] **Step 2: Add the fields + cascade**

In `pkg/css/cascade.go`:
- Add to `ComputedStyle` (near `Overflow`):
  ```go
  // BreakBefore / BreakAfter are the CSS fragmentation break hints (break-before /
  // break-after, plus the legacy page-break-before / page-break-after aliases). Read
  // only by the pagination pass (never by layout); a forced value ("page"/"always"/
  // named page sides) starts the box on a new page. Initial "" (auto). Not inherited.
  BreakBefore string
  BreakAfter  string
  ```
- The initial-`ComputedStyle` builder sets string fields by zero value, so `""` is automatic — **only** add explicit initialization if the builder explicitly sets other string initials (it sets `Overflow: "visible"`; break initial is `""` which is the zero value, so no explicit line needed — but add a one-line comment if the surrounding code documents initials).
- In `applyDeclaration`'s switch, add cases (mirroring `case "overflow"`):
  ```go
  case "break-before", "page-break-before":
      cs.BreakBefore = d.Value // store as overflow/display do (lowercased if they lowercase)
  case "break-after", "page-break-after":
      cs.BreakAfter = d.Value
  ```
  Match the exact value-handling of the neighboring string cases (if they trim/lowercase via a helper, use it; the spec stores lowercased — confirm `d.Value` is already lowercased by the tokenizer, else apply the same normalization the other keyword properties use).

- [ ] **Step 3: Run + lint**
- `go test ./pkg/css/...` (sandbox disabled) — green.
- `gofmt -l pkg/css/cascade.go pkg/css/<testfile>` — empty.
- `golangci-lint run ./pkg/css/...` (sandbox disabled) — clean. (The `unused` linter will flag the new fields **until Task 2 reads them** — that is expected mid-task; if lint runs green here it is because the test reads them. If `unused` complains, it is satisfied once the pass in Task 2 reads the fields; note it and proceed — do NOT add a fake reader.)

**Acceptance:** break properties cascade onto `ComputedStyle`; existing `pkg/css` tests still pass.

---

### Task 2: the pagination pass (`pkg/layout/css/paginate.go`)

The core. A post-pass over the finished fragment tree. Reuses `layoutTree`, `shiftFragment`/`shiftFragments`, and `Fragment.Page`/`AppendItems` — invents only the bucketing + per-page wrapper.

**Files:**
- Create: `pkg/layout/css/paginate.go`
- Create: `pkg/layout/css/paginate_test.go`

- [ ] **Step 1: Re-verify the load-bearing code facts**

Before writing, confirm against the CURRENT code (cite line numbers in your notes):
- `body.Children` holds in-flow block fragments only (positioned → `Positioned`, floats → `Floats`): re-read `block.go` around the `IsPositioned`/`Floats` appends.
- `shiftFragment(f, dy)` recurses `Children`+`Floats`+`Lines`+`Image` (not `Positioned`): re-read `block.go:1178`.
- `Fragment.Page(w, h)` = `layout.Page{WidthPt: w, HeightPt: h, Items: f.AppendItems(nil)}`: re-read `fragment.go:551`.
- The test harness `layoutTreeFor`/`bodyOf`/`layoutBody` exist in `block_test.go` for driving the engine in tests.

If any differs, STOP and reconcile with the spec before coding.

- [ ] **Step 2: Write failing unit tests**

Create `pkg/layout/css/paginate_test.go`. Use the existing harness (`layoutTreeFor` builds a root fragment; you need the *paged* path, so call `New(nil,nil,logf).LayoutPaged(ctx, root, w, h)` where `root` is the **cssbox** root — build it via `Build(...)` like `layoutTreeFor` does). Tests:

1. `TestPaginateByHeight` — HTML with three stacked block `<div>`s each a known height (~0.5×pageH; set via inline `style="height:Npx"` and a viewport where px==pt). With `pageH` ≈ 1.2× one block, assert `len(pages.Pages) == 2` (blocks 1+2 on page 0, block 3 on page 1). Assert each page's `HeightPt == pageH`. (Geometry of which block is where is asserted via item-stream / fragment inspection — see note below.)
2. `TestPaginateForcedBreakBefore` — two short blocks that both fit one page; block 2 has `page-break-before: always`. Assert 2 pages.
3. `TestPaginateForcedBreakAfter` — block 1 has `break-after: page`. Assert block 2 lands on page 1 ⇒ 2 pages.
4. `TestPaginateSingleBlockFits` — one short block, large `pageH` ⇒ exactly 1 page.
5. `TestPaginateOverTallBlock` — one block taller than `pageH` ⇒ exactly **1** page (no phantom page 2); a logf capture records the "taller than page" message; the page's `Items` is non-empty.
6. `TestLayoutPagedZeroHeightIsSingleTall` — `LayoutPaged(ctx, root, w, 0)` returns the SAME shape as `Layout(ctx, root, w)`: `len == 1` and `pages.Pages[0].HeightPt` equals the content height (NOT a fixed page height) — assert it equals what `Layout` returns for the same input.
7. `TestIsForcedBreak` — table: `page`/`always`/`left`/`right`/`recto`/`verso` → true; `auto`/`avoid`/`avoid-page`/`""`/`"junk"` → false.

**How to assert "which block on which page" without a brittle Y read:** the cleanest observable is **page count + each page's content presence**. To assert a specific block moved to page 1 at local Y≈0, inspect the **flattened page** (`pages.Pages[1].Items`) for a primitive (e.g. a Background `RuleItem` of the known block color) whose `YPt` is near 0 — OR, preferred, expose the bucketing via a small **internal** test seam: have `paginate` return its buckets to a thin internal helper the test can call (e.g. test the unexported `bucketBlocks(blocks, pageH)` directly, asserting block→page assignment by fragment identity). Prefer testing the **pure bucketing helper** (`bucketBlocks`) for assignment, and the full `LayoutPaged` for page count + heights + non-empty items. This keeps assignment tests robust (fragment-pointer identity) and end-to-end tests honest (real flatten).

Run; confirm they fail (no `LayoutPaged`/`paginate`/`isForcedBreak`).

- [ ] **Step 3: Implement the pass**

Create `pkg/layout/css/paginate.go` per the spec's Component 2. Structure:

- `func (e *Engine) LayoutPaged(ctx, root *cssbox.Box, viewportW, pageH float64) (pages *layout.Pages, err error)`:
  - `if pageH <= 0 { return e.Layout(ctx, root, viewportW) }`.
  - Same `defer recover()` shape as `Layout`, but the fallback empty page is `pageH`-tall: `&layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: pageH}}}`.
  - `frag := e.layoutTree(ctx, root, viewportW)`; nil → single `pageH`-tall empty page.
  - `return e.paginate(frag, viewportW, pageH), nil`.
- `func (e *Engine) paginate(root *Fragment, viewportW, pageH float64) *layout.Pages` — resolve body, get `blocks := body.Children`; if no body / no blocks, return one page `root.Page(viewportW, pageH)`.
- `func bucketBlocks(blocks []*Fragment, pageH float64, logf func(string, ...any)) []pageBucket` — **pure** (testable): the bucketing loop from the spec (`type pageBucket struct{ top float64; blocks []*Fragment }`). Forced-before / overflow / forced-after logic exactly as the spec. Emit the over-tall log here.
  - **Edge cases:** empty `blocks` → one empty bucket with `top: 0` (caller handles); a single block → one bucket. Ensure the final non-empty `cur` is always appended, and that an all-forced-after sequence doesn't emit a trailing empty bucket (append `cur` only if `len(cur.blocks) > 0`, except guarantee at least one bucket overall).
- Per bucket: `clonePageRoot(root, body, bk.blocks)` (value-copy root+body; set body clone's `Children`; point root clone's `Children` at body clone preserving non-body children), then `shiftFragments(<bucket blocks>, -bk.top)`, then `pageRoot.Page(viewportW, pageH)`.
  - **Important:** shift the bucket's block fragments (the ones now under the cloned body), and shift **before** flattening. Because a block belongs to exactly one bucket, shifting in place is safe (no cross-page aliasing).
  - `clonePageRoot` returns a `*Fragment` (the cloned root); a helper to fetch its body's children for shifting, or shift `bk.blocks` directly (they ARE the body clone's children, same pointers).
- `isForcedBreak`, `bodyFragment`, `breakBefore`/`breakAfter` helpers as in the spec.

- [ ] **Step 4: Run + lint + race**
- `go test ./pkg/layout/css/...` (sandbox disabled) — green (Task 1's `unused` on the break fields now resolves: the pass reads them).
- `go test -race ./pkg/layout/css/...` — green.
- `gofmt -l pkg/layout/css/paginate.go pkg/layout/css/paginate_test.go` — empty.
- `golangci-lint run ./pkg/css/... ./pkg/layout/...` — clean.

**Acceptance:** the pass buckets blocks correctly, honors forced breaks, degrades on over-tall blocks, and `pageH<=0` is a true passthrough to `Layout`. The whole `pkg/layout/css` suite (every existing golden-feeding geometry test) still passes — pagination touched nothing in the single-tall path.

---

### Task 3: `WithPageSize` option + wiring (`pkg/doctaculous`)

Expose the trigger and thread the page height to `LayoutPaged`.

**Files:**
- Modify: `pkg/doctaculous/html_backend.go`
- Create: `pkg/doctaculous/pagination_test.go`

- [ ] **Step 1: Write failing end-to-end tests**

Create `pkg/doctaculous/pagination_test.go`:
- `TestWithPageSizeMultiPage` — `OpenHTMLBytes(multiBlockHTML, WithPageSize(W, H))` where the content is clearly taller than `H` ⇒ `doc.PageCount() > 1`; iterate pages, each `RasterizePage` succeeds and the image has non-zero bounds AND is **non-blank** (at least one non-background pixel — reuse the existing non-blank check the golden tests use, or scan pixels). Use `LetterWidthPt`/`LetterHeightPt` in at least one assertion so the constants are read (satisfies `unused`).
- `TestWithPageSizeSinglePageWhenShort` — short content + large `WithPageSize` ⇒ `PageCount() == 1`.
- `TestDefaultIsSingleTallPage` — `OpenHTMLBytes(multiBlockHTML)` (NO `WithPageSize`) ⇒ `PageCount() == 1`, and the single page's height equals the content height (the un-paginated invariant; assert it is **>** a typical page height for tall content, proving it was NOT sliced). This is the byte-identical-default guard at the API level.

Find `PageCount()`/`RasterizePage` on `*Document` (`grep -n "func (d \*Document)" pkg/doctaculous/*.go`) and the existing non-blank/pixel helpers used by reflow golden tests.

Run; confirm failure (`WithPageSize`/`LetterWidthPt` undefined).

- [ ] **Step 2: Implement**

In `pkg/doctaculous/html_backend.go`:
- Add `pageHeightPt float64` to `htmlConfig` (default 0).
- Add the constants + option:
  ```go
  // LetterWidthPt / LetterHeightPt are US-Letter (8.5in × 11in) at 96dpi (px:pt 1:1),
  // the conventional default page for WithPageSize.
  const (
      LetterWidthPt  = 816
      LetterHeightPt = 1056
  )

  // WithPageSize paginates output into fixed widthPt × heightPt (points) pages: the
  // document lays out at widthPt and is sliced into heightPt-tall pages, breaking
  // between top-level blocks and at forced page breaks. Without WithPageSize the
  // document renders as a single tall page (the default). widthPt/heightPt <= 0 are
  // ignored.
  func WithPageSize(widthPt, heightPt float64) HTMLOption {
      return func(c *htmlConfig) {
          if widthPt > 0 && heightPt > 0 {
              c.viewportPt = widthPt
              c.pageHeightPt = heightPt
          }
      }
  }
  ```
- In `htmlDocument`, change the layout call to `pages, err := engine.LayoutPaged(ctx, root, cfg.viewportPt, cfg.pageHeightPt)`.

- [ ] **Step 3: Run + lint + race**
- `go test ./pkg/doctaculous/...` (sandbox disabled) — green.
- `go test -race ./pkg/doctaculous/...` — green.
- `gofmt -l` the two files — empty.
- `golangci-lint run ./pkg/doctaculous/...` — clean.

**Acceptance:** `WithPageSize` opts into pagination end-to-end; the default (no option) stays one tall page; the constants are exported and exercised.

---

### Task 4: golden image(s) for a paginated document

Prove the pixels with an eyeball-able golden per page, matching the existing `html-*` golden convention.

**Files:**
- Add a fixture + register it in whatever drives the `html-*` reflow goldens (find it: `grep -rn "html-" pkg/render/raster/*_test.go pkg/doctaculous/*_test.go testdata/`), or add a focused golden test in `pkg/doctaculous` if that is where reflow goldens live.
- Add the generated PNGs under the goldens dir.

- [ ] **Step 1: Add the fixture + golden test**
- A small HTML doc with enough stacked blocks (distinct background colors per block, so each page is visually obvious) to span ≥2 pages at a modest `WithPageSize` (e.g. `WithPageSize(400, 300)`).
- Render with `WithPageSize`, write one golden PNG per page (`html-paginate-p0.png`, `-p1.png`, …) under the goldens dir. Follow the existing golden harness's compare + `-update` mechanism (tolerance, per-pixel budget) — do NOT hand-roll a comparator if the repo has one.

- [ ] **Step 2: Generate + eyeball**
- Generate with the repo's golden `-update` flow (e.g. `go test ./<pkg> -run Test<GoldenName> -update`, sandbox disabled).
- **Read every generated PNG** (open each `html-paginate-p*.png` in this session and look): page 0 shows the first blocks at the top, page 1 shows the continuation at local Y 0 — confirm the split is between blocks and the second page does not repeat page 0. Note the visual confirmation in your report.
- Re-run the golden test WITHOUT `-update` — it passes against the committed PNGs.

- [ ] **Step 3: Confirm the existing corpus is byte-identical**
- Run the full golden suite (sandbox disabled): every PRE-EXISTING golden passes unchanged (no `WithPageSize` anywhere in the old tests ⇒ the `pageH<=0` path ⇒ identical pixels). Only the NEW `html-paginate-*` PNGs are added. `git status` shows only new golden files + your source files.

**Acceptance:** a paginated document has committed, eyeballed golden pages; the existing golden corpus is untouched.

---

### Task 5: holistic verification + CLAUDE.md

- [ ] **Step 1: Whole-repo green**
- `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` (all sandbox disabled) — green.
- `golangci-lint run ./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...` — clean. `gofmt -l` on every changed file — empty.
- `find . -name 'zz_*' -o -name '*probe*'` — empty. `git status` — only the intended files.

- [ ] **Step 2: Adversarial probes (write, run, DELETE)**

As a separate reviewer pass, write throwaway probe tests (then delete every one; confirm `find` empty + `git status` clean) that vary what the unit tests hold fixed:
- A document whose blocks sum to EXACTLY `pageH` (boundary: the last block's bottom == pageH — does it stay on the page or push a phantom empty page? It should stay; the overflow check is strict `>`).
- A forced `break-before: always` on the **first** block (no preceding content — must NOT emit a leading empty page).
- A forced `break-after` on the **last** block (must NOT emit a trailing empty page).
- A consecutive `break-after` then `break-before` between two blocks (one break, not two empty pages).
- `WithPageSize` then render at a non-72 DPI via `RasterizePage` (the page pixel height scales correctly; no panic).
- A document with a top-level **positioned** box + paginated content: confirm it does NOT panic and the in-flow blocks still paginate (positioned content paints with page 0 per the documented deferral — verify it is not double-painted or crashing).
- Render an actual paginated page to `$TMPDIR` and eyeball it (the controller reads the PNG) to confirm real pixels at a page boundary.

Report what each probe found; fix any real bug (a phantom empty page, a double-paint, an off-by-one at the exact-fit boundary are the likely ones) by correcting the **pass**, not the test. Re-confirm green. **Delete all probes.**

- [ ] **Step 3: Update CLAUDE.md**
- Move sub-project 12 from §6 "remaining slices" into a new **"Done"** bullet ("HTML rendering — pagination …"), describing: the `WithPageSize` opt-in, between-block fragmentation, forced `break-before`/`break-after` (+ `page-break-*` aliases), the US-Letter `LetterWidthPt`/`LetterHeightPt` constants, **byte-identical** default (single tall page), the post-pass architecture (untouched builder/flatten/`Device`/PDF/DOCX/inline core), and the deferrals (mid-box/line/row/atom split, widows/orphans, `break-inside`, positioned/float per-page distribution, `@page`, headers/footers). Cite the spec.
- Update the §6 done-slices parenthetical to include pagination.
- Add a "pagination fidelity follow-ups" note capturing the deferrals (the spec's Deferrals table is the source).
- `git add` ONLY `CLAUDE.md` (and only when this step runs).

**Acceptance:** whole repo green/race-clean/lint-0; adversarial probes pass and are deleted; CLAUDE.md reflects the slice as the honest source of truth.

---

## Finishing (not a task — the controller runs this)

After all tasks: holistic final review (a fresh reviewer reads the diff against the spec + CLAUDE.md constraints, renders a paginated page, runs adversarial probes), then `superpowers:finishing-a-development-branch` → stacked PR off `feat/html-openurl` (or `main` if the stack merged). Keep the PR description short; do not credit Claude (per user CLAUDE.md). Confirm the sub-project A docs (if still present) are untouched: `git diff <base>..HEAD` shows zero `pdf-writer`/`html-to-pdf` files.
