# Handover — Sub-project 6: Full CSS 2.1 Appendix E z-index stacking (+ the relative-clip-escape fix)

**Status:** Not started. Sub-project **5c (overflow clipping)** is DONE on branch `feat/html-overflow`,
PR **#8** open against `feat/html-positioning`.
**Next action:** Same flow as #1–#5c — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / PR.

This is the slice that turns the **minimal** stacking pass (positioned boxes paint in document order)
into the **full** CSS 2.1 Appendix E z-index ordering — negative-`z-index` boxes painting behind
in-flow content, and numeric `z-index` sorting within a stacking context. It also folds in the one
clipping gap 5c deferred (a positioned descendant of a *non-positioned* `overflow:hidden` box escapes the
clip), because the fix needs the same machinery this slice reworks.

---

## Where we are (the PR stack)

All chained to `main`; retarget each PR up the chain as the one below it merges.

- **#2 CSS parse+cascade** — `feat/css-parse-cascade` → `main`.
- **#3 HTML box generation** — `feat/html-box-generation` → `feat/css-parse-cascade`.
- **#4 block + inline normal flow** — `feat/html-block-inline-flow` → `feat/html-box-generation`.
- **#5 replaced content + images** — `feat/html-replaced-images` → `feat/html-block-inline-flow`.
- **#5a floats + clear** — `feat/html-floats` → `feat/html-replaced-images` (**PR #6**).
- **#5b positioning** — `feat/html-positioning` → `feat/html-floats` (**PR #7**).
- **#5c overflow clipping** — `feat/html-overflow` → `feat/html-positioning` (**PR #8**).

If the stack has merged to `main`, branch sub-project 6 off `main`. Otherwise branch **6 off
`feat/html-overflow`** (it builds directly on the stacking pass + clip machinery 5c shipped). Tell every
subagent explicitly: you are on `feat/html-zindex` (or whatever you name it), do NOT
checkout/stash/switch branches.

## What sub-project 5c delivered (the foundation 6 builds on)

Design: `docs/superpowers/specs/2026-06-24-html-overflow-design.md` (read "The clip in the stacking
pass", "Float-height enclosure", "Sibling-BFC float avoidance", "Deferred", and "Review fixes folded
in"). All overflow logic lives in `pkg/layout/css` + a small painter addition; the shared inline core
stayed untouched.

- **`pkg/css/cascade.go`** — `Overflow string` (not inherited). `ZIndex int` + `ZIndexAuto bool` were
  already added in 5b and are **parsed but not sorted on** — this slice is where they finally bite.
- **`pkg/layout/css/fragment.go`** — **the seam 6 re-points.** `Fragment` carries `IsStackingContext`,
  `Positioned []*Fragment`, and (5c) `PositionedClip []bool`, `Clips bool`, `ClipRect rect`. `AppendItems`
  paints a stacking context / BFC in Appendix E phases: decorations → floats → in-flow content → **the
  positioned layer** (`appendPositioned`). `appendPositioned(dst, onlyCBOwned)` walks `f.Positioned` **in
  document order** (the minimal cut — NO z-sort) and applies a relative box's `RelOffset` over its emitted
  item range. For a clipping fragment the positioned layer is split: CB-owned entries (`PositionedClip[i]`)
  paint inside the clip bracket, escaped entries after `ClipPop`. **This `appendPositioned` loop + the
  bracket placement is exactly what z-index re-points** (see "What 6 must build").
- **`pkg/layout/css/block.go`** — collects positioned descendants onto the nearest stacking-context
  ancestor's `Positioned` (4 append sites, each keeping `PositionedClip` parallel: `layoutTree`,
  `layoutBlock` consume, `placeFloat`, `resolveAbsolute`). `establishesStackingContext(b)` = any positioned
  box; `establishesNewBFC(b)` now also true for `overflow≠visible` (via `clips(b)`).
- **`pkg/layout` + `pkg/layout/paint`** — `ClipPushKind`/`ClipPopKind` flat-stream items + the `PaintPage`
  Save/PushClip/Restore wiring (reused by this slice if any z-layer needs its own clip).
- Plus float-height enclosure (`floatContext.maxBottom()` folded into a BFC box's auto-height content
  height) and sibling-BFC float avoidance (a placement loop in `layoutBlockChildren`). Neither touches the
  z-index work, but the restored `float-row` golden/reftest exist now.

## What sub-project 6 must build (roadmap §6, z-index slice)

The full CSS 2.1 Appendix E paint order **within a stacking context**, replacing the minimal
document-order cut. In Appendix E order, a stacking context paints:

1. the context root's own background + border;
2. **negative-z-index** child stacking contexts (most-negative first) — painted **behind** in-flow content;
3. in-flow, non-positioned block descendants' backgrounds/borders;
4. floats;
5. in-flow, non-positioned inline content;
6. **z-index:auto / z-index:0** positioned descendants (and non-positioned-but-stacking, e.g. `opacity<1`
   — not modeled), in document order;
7. **positive-z-index** child stacking contexts (least-positive first).

Today (5c) the engine collapses steps 2/6/7 into one document-order `appendPositioned` loop after step 5.
This slice splits them. In rough order:

- **Sort key + sub-layering** (`pkg/layout/css/fragment.go`): give the positioned layer a stable sort by
  `(z-index, document-order)`. The `z-index` is on `Box.Style` (`ZIndex`/`ZIndexAuto`) — it needs to be
  **carried onto the `Fragment`** (a new `ZIndex int` + `ZIndexAuto bool`, or read from a retained box
  pointer; decide in the spec — note `Fragment` does NOT currently hold a `*cssbox.Box`). `appendPositioned`
  becomes: partition `f.Positioned` into negative / zero-or-auto / positive by z-index; **stable-sort**
  each numeric partition ascending (document order breaks ties — Go's `sort.SliceStable`, but the repo
  **declines `slices.*`** so use `sort.SliceStable` or a hand-rolled stable sort, NOT `slices.SortStableFunc`
  unless the lint config allows it — check `golangci-lint` locally first); then emit **negatives before
  appendDecorations** (step 2, behind in-flow content), zero/auto after appendContent (step 6, where they
  are today), positives last (step 7). The negative-before-decorations move is the load-bearing change —
  it means `appendPositioned` can no longer be a single call after content; the stacking-context branch of
  `AppendItems` needs a negatives pass *before* `appendDecorations` and a zero+positives pass *after*
  `appendContent`.
- **Stamp z-index on the fragment** (`pkg/layout/css/block.go`): wherever a positioned fragment is built
  / collected, copy `b.Style.ZIndex`/`ZIndexAuto` onto it (relative in `layoutBlockChildren`, abs/fixed in
  `resolveAbsolute`, positioned float in `placeFloat`). The `z-index` only applies to a box that
  establishes a stacking context (positioned boxes — all of `f.Positioned` qualify), so no extra gating.
- **Remove the `logZIndexUnsupported` degradation** (`block.go`) — z-index is now honored, so the "not yet
  sorted on" log goes away (and the spec/CLAUDE.md degradation note flips to supported).
- **The relative-clip-escape fix (5c's deferred gap), fold in here:** a `position:relative` (or other
  positioned) descendant of a *non-positioned* `overflow:hidden` box currently bubbles **past** the clip to
  a higher stacking context's positioned layer and paints **unclipped** (browsers clip it). The fix needs
  the clip to reach an item range that an **ancestor's** positioned phase emits — which is exactly the
  cross-layer machinery this slice already reworks. Approach to spec: when a positioned descendant escapes
  to an ancestor's `Positioned` but passed *through* a clipping box on the way, it must still be clipped to
  that intervening clip (CSS: the clip of any `overflow≠visible` ancestor between the box and its
  containing block applies). Today `PositionedClip` only encodes "is the holder the CB"; this needs to
  additionally carry the **intervening clip rect(s)** a positioned descendant must be clipped to even when
  it paints in an ancestor's layer. Have the spec reviewer verify this with an adversarial test (a
  `position:relative` child of a non-positioned `overflow:hidden` box, whose offset pushes it past the clip
  edge — it must be cut at the clip edge, NOT paint outside). **This is the subtle integration; scope it
  carefully — if it bloats the slice, it is acceptable to split it into a 6b**, but it is grouped here
  because the seam is shared.

Each lands with fixtures + golden/WPT tests, degrades gracefully, and recovers at the page boundary.

## Carried-forward deferrals 6 should fold in or scope out

From 5c's (and earlier) specs' "Deferred" sections:
- **Full z-index ordering** (negative/numeric) — **this slice's** core.
- **The relative-clip-escape gap** — fold in here (above), or split to 6b if it bloats.
- **A float inside a `position:relative` (non-BFC) box not riding the relative paint offset** (5b/5c
  deferral) — still open; likely interacts with the stacking work. Revisit if a test surfaces it.
- Still open and NOT 6's concern unless touched: the **precise static-position solve** for an all-`auto`
  abs box, abs `width:auto` **shrink-to-fit**, abs `margin:auto` centering, a `bottom`-only auto-height
  abs box, **`position:relative` on any inline-level box** (a no-op — needs inline-box fragments), the
  replaced/inline deferrals (`object-position`, ratio-preserving min/max, percentage-height basis,
  `background-image`, full `vertical-align`, `margin:auto` centering, margin-collapse edge cases), and the
  bigger slices: **tables**, **web fonts** (`@font-face` + WOFF/WOFF2), **flexbox** then **grid**,
  **`OpenURL` + HTTP `ResourceLoader`**, **pagination / paged media**, **EPUB**.

## Process reminders (held across #1–#5c — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled. This repo's `origin` is HTTPS, so push with the
  sandbox disabled.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared" errors and **phantom `zz_*` scratch files in the diagnostics that no
  longer exist on disk**. Trust `go build`/`go test` and `find . -name 'zz_*'`, not the panel. Tell every
  subagent (and reviewer) to delete any `zz_*` throwaway before finishing and confirm `git status` clean;
  reviewers that probe with throwaway tests must clean them up (the 5c reviewers did, but the panel kept
  showing ghosts of deleted files for the rest of the session).
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. The repo uses **NO
  `//nolint`** and **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) — keep
  explicit `if x < y { x = y }` clamps and indexed loops. golangci-lint **does** flag `if !(a && b)`
  (QF1001 — write the De Morgan form `if a >= b || …`) and an **unused unexported field/func**. **5c
  lesson: write test assertions in De-Morgan'd form in the PLAN** (the 5c plan's `if !(a && b)` test
  snippets tripped QF1001 and needed a fixup commit), and **run `golangci-lint` per task, not just
  `gofmt`** — a `gofmt`-clean task can still fail CI lint.
- **The zero-value `Length` trap** — a `cssbox.ComputedStyle`/`Box` literal that omits
  `Width`/`MaxWidth`/offsets reads as explicit `0`, NOT the cascade's `auto`/`none`. Test fixtures built as
  raw structs must set these to `UnitAuto` where they mean auto. Reuse 5b's `posStyle()`/`posBox()` helpers
  (in `positioning_layout_test.go`) and 5a's `blockBox()` (in `floats_layout_test.go`); the 5c
  `overflow_layout_test.go` adds `clipBoundsReal`/`bgIndex` helpers (first ClipPush/last ClipPop indices;
  first BackgroundKind of a color) that the z-index paint-order tests can reuse.
- **Test the FLAG COMBINATIONS, not each flag alone** (every slice's worst-miss class) — for 6: a negative
  z-index box behind in-flow content; a positive z-index box over a z:auto box; a z-indexed box inside a
  clipping box; a z-indexed box that is ALSO a float; the relative-clip-escape case. **Assert paint ORDER
  via `AppendItems`** (the item stream), since z-index is a flatten-time fact, and **eyeball every golden**
  — the golden render caught the only paint bugs unit tests missed in 5b. Author goldens with **visibly
  overlapping** boxes of different z so the order is unambiguous to the eye.
- **Eyeball every new/changed golden PNG** in the PR (the controller, via the Read tool — the implementer
  has no image vision). A "passing" golden can still be WRONG. For 6 the goldens must show the stacking
  order (a higher-z box visibly ON TOP; a negative-z box visibly BEHIND in-flow content).
- **Confirm no pre-existing golden changed** (`git status --short pkg/doctaculous/testdata
  pkg/render/raster/testdata` shows only new files). A paint-order change is high-risk for silently
  reordering existing pages — **every existing golden/reftest MUST stay byte-identical** (run them without
  `-update` and confirm no diff). In particular: a page where **every** positioned box is `z-index:auto`
  must produce the **identical** item order to today's document-order cut (the sort is stable, so
  auto/auto ties keep document order) — this is the byte-identical guard for the whole existing corpus.
- **The two-stage review (spec + code-quality, per task) earns its keep**, and a **holistic final
  review** on the assembled diff is mandatory — in 5b it caught a real sizing bug, two missing degradation
  logs, and a false spec claim; in 5c the holistic review found nothing (the per-task reviews held), which
  is the bar. Have spec reviewers **verify the load-bearing paint order with throwaway adversarial tests**
  (for 6: the negative-z-behind-content ordering and the relative-clip-escape clipping), and **delete the
  throwaways** (confirm `git status` clean).
- **Subagents implementing a large task** read the exact task lines from the plan file (point them at the
  line range) or get the full text pasted; for small tasks paste the full text. The z-index sort + the
  negatives-before-decorations restructure of `AppendItems` is the one genuinely tricky task — give it the
  full context and the most capable model.
- **Propagate review fixes back into the spec/plan**, and **update CLAUDE.md's Done/TODO** when the PR
  lands (move full z-index stacking out of the §6 TODO into Done; flip the "z-index parsed but not sorted
  on" degradation note in the positioning + overflow Done bullets to supported).
