# Handover — pagination fidelity pass: what shipped + the remaining backlog

**Status:** This follow-up PR (stacked on `feat/html-pagination`) addressed the C1–C7 audit backlog from
`HANDOVER-pagination-fidelity-followups.md`, folded in four more rendering bugs an adversarial codebase review
surfaced, and took on three larger out-of-theme items the user approved. Everything below is **done, tested
(mutation-verified), and green** (whole repo: build, `go test ./...`, `-race` on the concurrent packages,
`golangci-lint` 0 issues, `gofmt` clean). The existing golden/reftest corpus is **byte-identical** except the
two intentional additions noted below.

---

## What shipped in this PR

### The C-series (the original audit backlog)

- **C1 + C2 (the real bug + its latent root cause).** `shiftFragment`/`translateFragment` recursed `Children`
  and `Floats` but not the other page-space fields a fragment owns. Fixed via a shared `shiftFragmentExtras`
  that moves `ClipRect`, `Collapsed` (border-collapse grid strips), `Positioned` (recursively — **skipping
  `position:relative` entries**, which are aliased with `Children` and would otherwise double-shift), and each
  `PositionedInfo.ClipChain`. This fixed C1 (abs descendant of a paginated relative block landing at the wrong
  Y) **and two pre-existing base-engine bugs** the goldens masked: a `border-collapse` table and an
  `overflow:hidden` box placed *below other content* painted their grid lines / clip rect at the un-shifted
  build-frame Y. Tests: `TestPaginateAbsChildOfRelativeBlockLandsAtCorrectY`,
  `TestCollapsedTableGridLinesHugCellsBelowContent`, `TestClipRectFollowsBoxBelowContent`,
  `TestPaginateRelativeOverflowAbsClipFollowsToPage`.
- **C3.** A forced `break-before`/`break-after` on a nested (non-top-level) block is **warned once**
  (`warnNestedForcedBreaks`) instead of silently dropped. Tests: `TestPaginateNestedForcedBreakWarns`,
  `TestPaginateTopLevelForcedBreakDoesNotWarn`.
- **C4 (controller chose: fragment the border properly, not suppress).** The per-page html/body wrapper clone
  is shifted into the page's local frame (`shiftFragmentSelf` — moves the wrapper's own box, not its children),
  so the page bitmap fragments the border: top edge on page 0, bottom on the last page, sides on every page.
  The first page's top is pulled up to the wrapper's border-box top (`wrapperDecorationTop`) so a `<body>` top
  border shows on page 0 with content below it. Test: `TestPaginateBodyBorderFragments`; new golden
  `html-paginate-body-border-p{0,1,2}` (eyeballed: top+sides on p0, sides on p1, sides+bottom on p2).
- **C5.** Documented the margin-top-collapsed-at-unforced-break simplification (comment in `bucketBlocks`) and
  corrected "logged once" → "logged per over-tall block" in the code/spec/CLAUDE.md.
- **C6.** Deleted the stale "real pagination is a later sub-project" sentence on `Fragment.Page`.
- **C7.** Replaced the hand-rolled `containsTallerThanPage` substring scan with `strings.Contains`.

### Adversarial-review bugs folded in (verified by render + data dump, mutation-tested)

- **F-A.** `translateItems` (applies a `position:relative` box's paint-time offset over its flattened item
  range) did not translate `ClipPushKind` — a relative box with `overflow:hidden` moved its content but not its
  clip. Fixed; `TestRelativeOffsetMovesOwnClip`.
- **F-B.** A `position:relative` block nested under a static wrapper, paginated to a later page, **vanished**
  (routed to page 0 at an off-page Y). Fixed by routing positioned entries to their nearest top-level ancestor
  block's page; `TestPaginateNestedRelativeBlockFollowsToPage`.
- **F-C.** In the non-clipping `AppendItems` branch, a stacking context emitted negative-z descendants **before
  its own background** (Appendix-E violation) — a `z-index:-1` child was hidden behind a host with its own
  background. Fixed; `TestZNegativeBehindHostOwnBackground` + new golden `html-zindex-neg-behind-own-bg`.
- **F-D.** `replacedFragment` left `Box == nil`, defeating the `isRelativeFragment` guard the C2 fix relies on
  (and z-index reads for a positioned `<img>`). Fixed by setting `frag.Box = b`.

### Larger items (out of theme, user-approved)

- **PDF page-boundary recover.** The PDF render path had **no `recover`**, so a panic in a `RasterizePages`
  worker was process-fatal — violating the CLAUDE.md "one bad page can't kill a batch" guarantee. Added a
  recover in `raster.RenderPage` (returns the partially-painted page on panic). Also **guarded the two known
  crafted-PDF panic sites** directly: a malformed single-element image `/ColorSpace` array
  (`resolveImageCSArray`, indexed `arr[1]` OOB) and a self-/mutually-referential Type 3 stitching function
  (`function.Parse` gained a depth guard). Tests: `TestRenderMalformedImageColorSpaceNoPanic` (end-to-end,
  exercises guard AND recover), `TestType3DepthGuard`, `TestResolveImageCSArrayMalformedNoPanic`. New
  generator `gen.MalformedImageColorSpacePDF`.
- **Perf: min/max-content measurement cache.** Table cells / grid items / flex items were gathered+shaped for
  BOTH min- and max-content (and `gatherInlineRuns` even lays out inline-block atoms), with no memo. Added a
  per-box `(box → {min,max})` cache on the `Engine` (created per layout; layout is single-threaded; the box
  tree is immutable during layout, so byte-identical). Tests: `TestMeasureContentCacheHit`,
  `TestMeasureContentCachePopulatesBothSizes`.
- **Perf: quadratic line-breaking.** `inline.VisibleWidth` re-summed every advance on each glyph append in
  `Break`/`BreakNext` → O(L²) per line, in BOTH engines. Now tracks running width incrementally (O(1)/glyph),
  carefully preserving the trailing-space exclusion. Behavior-preserving (whole corpus byte-identical). Tests:
  `TestBreakIncrementalWidthMatchesVisibleWidth`, `TestBreakConsecutiveTrailingSpaces`.

---

## Remaining backlog (documented follow-ups — NOT done)

### Pagination deferrals still open (bounded slice = between top-level blocks, post-pass)

These are genuine future slices (effort/impact noted):

- **Mid-box / mid-line / mid-table-row / mid-flex-or-grid-item fragmentation** — a block taller than a page
  overflows (clipped by the bitmap) rather than splitting. The big one; needs threading the split into the
  block stacker (or a fragmentation-aware re-layout), not a post-pass. **Large.**
- **Widows / orphans, `break-inside: avoid`, `break-*: avoid`** — parsed onto `ComputedStyle`, not acted on.
  Needs the mid-box machinery above (or at least line-count-aware bucketing). **Medium–large.**
- **Per-page distribution of page-CB abs/fixed + `fixed` repeating on every page** — an abs/fixed box whose
  containing block is the *page* still rides page 0; `fixed` should repeat on every page. (An abs descendant of
  a paginated *relative* ancestor now follows it — done.) **Medium.**
- **Honoring a forced break on a NESTED block** — today warned once and dropped; needs propagating the break up
  to the nearest top-level ancestor. **Small–medium.**
- **Retaining a leading margin-top at an unforced break** — today collapsed to 0. **Small.**
- **`@page` size/margins/named pages + running headers/footers** — page size comes only from `WithPageSize`;
  margins are zero. This is the CSS-paged-media slice (a sibling of EPUB). **Large.**

### Non-pagination rendering bugs found by the review but deferred (with repros)

- **F-E — `display:block` on `<img>` (or any replaced element) is silently ignored** (`anon.go`
  `isBlockLevelOuter` never special-cases a `display:block` replaced box → it's treated as inline-level and the
  block path `layoutBlockReplaced` is unreachable for the plain `display:block` case). Repro:
  `<div>AAA<img style="display:block;width:40px;height:40px" src="x.png">BBB</div>` — browser stacks three
  blocks; engine lays them on one line. **Visible bug, medium.** (Note: fixing this also requires setting
  `frag.Box` on replaced fragments — already done as F-D — so a relative block-level `<img>` doesn't
  double-shift.)
- **F-F — inline-block with text content is bottom-aligned instead of baseline-aligned** (`inline.go`
  `atomicRunFor` sets `BaselinePt: frag.H`, resting the atom's bottom margin edge on the line baseline). Per
  CSS 2.1 §10.8.1 a `vertical-align:baseline` inline-block with in-flow line boxes aligns its **last line
  box's baseline**. Repro: `<p>text <span style="display:inline-block">box</span> text</p>` — "box" sits too
  low. **Visible bug, medium** (the default-baseline case is common; full `vertical-align` is a known
  deferral).

### PDF spec divergences found by the review but deferred (with triggers)

The PDF **panic** risks are fixed (recover + the two guards). These remaining items are **wrong-output**, not
crashes:

- **Separation / DeviceN `scn` colors inverted** (`pkg/pdf/content/colorspace.go` `colorFromComponents`): a
  spot color set via `sc`/`scn` is mapped by component count with no tint-transform `/Function`, so a 1-comp
  tint of 1.0 (full ink) renders white. Trigger: any `Separation` spot color drawn with `scn`. **Medium.**
- **Form XObject `/BBox` clip never applied** (`pkg/pdf/content/xobject.go` + `page.go` `doXObject`): per ISO
  32000 §8.10.1 the form BBox is a mandatory clip. Trigger: a form that draws outside its BBox. **Medium.**
- **DCTDecode (JPEG) ignores `/Decode`** (`pkg/render/raster/page.go` DCT path): Adobe CMYK JPEGs ship
  `/Decode [1 0 1 0 1 0 1 0]` to invert; ignoring it yields inverted colors. The raw-sample path honors
  `/Decode`; only the DCT path doesn't. Trigger: CMYK JPEG with an inverting `/Decode`. **Small–medium.**
- **Text render modes 1/2/4/5/6** are all painted as fill (`pkg/pdf/content/showtext.go`): stroke-only text
  renders filled; clip modes don't contribute to the clip. Trigger: `Tr 1` outlined text. **Low** (mode 0
  dominates).

### Perf follow-ups still open (lower priority)

- **`buildCollapsedBorders` is O(cells²)** (`pkg/layout/css/table.go` — per-neighbor linear scan via the
  `cellAt` closure). Already documented in CLAUDE.md; a perf cliff only for very large collapsed grids. Fix:
  reuse `buildGrid`'s occupancy map for O(1) neighbor lookup.
- **Per-glyph paint allocations** (`paint.go` `transformPath` + `device.go` `rasterizeMask`): ~3 heap allocs
  per glyph per page. Inherent to the rasterizer design; only worth a transformed-glyph cache if profiling
  shows paint dominating.
- **`over()` straight-vs-premultiplied alpha** (`device.go`): correct only because both render entry points
  fill an opaque background first (dest alpha always 255). A latent fragility if a transparent page background
  is ever introduced — not a live bug.

### Audit items CONFIRMED correct (do not re-litigate)

`break-inside`/`break-*: avoid` graceful no-op, `@page` parsed-and-ignored, `break-before: column/region` →
non-forced, over-tall block stays 1 page, top-level float/abs/fixed not duplicated, `WithPageSize` width
side-effect documented, no blank pages, z-index across pages correct, `pageH<=0` passthrough, strict-`>`
exact-fit, recover-at-boundary. (And now: the LZW early-change boundary, CCITT B1/B2 parity, predictors,
ASCII85/Hex/RunLength edge cases, the crypt empty-password derivations, matrix composition order, and clip
timing were all re-verified solid by the PDF review.)
