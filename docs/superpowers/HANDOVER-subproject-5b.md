# Handover — Sub-project 5b: Positioning (relative / absolute / fixed + z-index)

**Status:** Not started. Sub-project **5a (floats + clear)** is DONE on branch `feat/html-floats`
(off `feat/html-replaced-images`), PR **#6** open against `feat/html-replaced-images`.
**Next action:** Same flow as #1–#5a — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / PR.

This is the **second of three slices** sub-project 5 was split into:
- **5a — floats + clear** ✅ done (this branch, PR #6). Spec: `docs/superpowers/specs/2026-06-24-html-floats-design.md`.
- **5b — positioning** (this doc): `position: relative/absolute/fixed`, z-index, the full stacking pass.
- **5c — overflow clipping**: `overflow:hidden/scroll/auto` + the deferred float interactions (see below).

---

## Where we are (the PR stack)

- **#1 CSS parse+cascade** — `feat/css-parse-cascade`.
- **#2 HTML box generation** — `feat/html-box-generation` → `feat/css-parse-cascade`.
- **#3 block + inline normal flow** — `feat/html-block-inline-flow` → `feat/html-box-generation`.
- **#4 replaced content + images** — `feat/html-replaced-images` → `feat/html-block-inline-flow`.
- **#5a floats + clear** — `feat/html-floats` → `feat/html-replaced-images` (**PR #6**).

Retarget each PR up the chain as the one below it merges; if the stack has merged to `main`, branch
sub-project 5b off `main`. Otherwise branch **5b off `feat/html-floats`** (it builds directly on the
float paint layer).

## What sub-project 5a delivered (the foundation 5b builds on)

Design: `docs/superpowers/specs/2026-06-24-html-floats-design.md` (read its "Architecture / seams" and
"Review fixes folded in" sections — the latter has hard-won lessons). All float logic lives in
`pkg/layout/css`; the shared inline core stayed format-neutral.

- **`pkg/layout/css/floats.go`** — the per-BFC `floatContext{cbLeft, cbRight, floats}` and its pure
  geometry: `leftEdge(y,h)`/`rightEdge(y,h)` (content boundary in a band, narrowed by overlapping
  floats), `place(side,w,h,y)` (lowest fitting Y, wrap-to-row, overflow-wide guard), `nextDropY`,
  `clearY(clear,y)`, `floats2frags()`. Margin-box rects in page space.
- **`pkg/layout/css/fragment.go`** — **this is the seam 5b generalizes.** `Fragment` gained
  `IsFloat bool`, `IsBFC bool`, `Floats []*Fragment`. `AppendItems` was refactored into **CSS 2.1
  Appendix E paint phases** for an `IsBFC` fragment: `appendDecorations` (in-flow block backgrounds/
  borders, skipping floated + nested-BFC children) → the **float layer** (`Floats`) →
  `appendContent` (in-flow inline content, painting a nested BFC atomically via its own `AppendItems`).
  Helpers: `appendDecorations`/`appendContent`/`appendSelfDecorations`/`appendSelfContent`. The
  `AppendItems` doc comment **already forward-points to 5b**: "The 3-phase BFC ordering is the seam the
  positioning slice generalizes into a full stacking-context pass (CSS 2.1 Appendix E z-index layers):
  positioned descendants and z-ordered layers will be inserted relative to the float phase, so keep the
  phase split intact when extending this."
- **`pkg/layout/css/block.go`** — `layoutTree`/`layoutBlock`/`layoutInterior`/`layoutBlockChildren`
  thread `bandOriginY float64` (the box's content-box top in the **BFC-root-local frame** — the frame
  the float context is queried in) and `fc *floatContext`. `placeFloat` places a float out of flow in
  that frame and records its fragment on the BFC root's `Floats`. `clear` raises a box below matching
  floats. `establishesNewBFC(b)` now returns true for `DisplayInlineBlock || Float != FloatNone`.
  `shiftFragment` recurses `f.Floats` (so a repositioned nested BFC carries its floats).
- **`pkg/layout/css/inline.go`** — `layoutInline` breaks line-by-line via `inline.BreakNext` at the
  per-line float-narrowed band, **clamped to the box's own content box** `[contentX, contentX+contentW]`
  (a non-BFC box narrower than its BFC must wrap at its own width, not the BFC's). `translateFragment`
  recurses `f.Floats`. New helper `lineHeightGuess(b)`.
- **`pkg/layout/inline/break.go`** — additive `BreakNext(glyphs, widthPt) (line, rest)`: one greedy
  line at a width (the per-line driver the float IFC needs). `Break` is unchanged (DOCX unaffected).
- **`pkg/css/cascade.go`** — additive `Float string`, `Clear string` on `ComputedStyle` (not inherited).
- **`pkg/layout/css/build.go`** — `floatOf(cs)` maps the keyword; `applyFloatBlockify(b, cs)` blockifies
  a floated inline-level element (CSS 9.7). **`positionOf(cs)` is STILL A STUB** returning `PosStatic` —
  this is where 5b starts (see below).
- **`pkg/doctaculous`** — `html-float-figure` golden (figure + wrapping text + cleared caption) and
  `float-left` WPT reftest. (The planned `float-row` golden/reftest was dropped — a float-only body has
  zero content height → degenerate 1×1 page; covered by `floats_test.go` geometry instead.)

## What sub-project 5b must build (roadmap §6, positioning slice)

`position: relative | absolute | fixed`, `z-index`, and the **stacking-context paint pass** that the
float layer's phase split was deliberately designed to grow into. The box already records `Position`
(`pkg/layout/cssbox` `PositionKind`: `PosStatic/PosRelative/PosAbsolute/PosFixed`), but box generation
hard-codes `positionOf` to `PosStatic` and the engine ignores it — every box lays out in normal flow
today (graceful no-op, the same starting point floats had).

- **Cascade + box-gen** (`pkg/css`, `pkg/layout/css/build.go`): add `position` + the offset properties
  `top`/`right`/`bottom`/`left` + `z-index` to `ComputedStyle` (mirror how `float`/`clear`/`object-fit`
  were added — keyword/length switch, not inherited). Wire `positionOf(cs)` (currently a stub) and
  carry the offsets/z-index onto the box. CSS blockifies an absolutely-positioned element too (like a
  float) — reuse/extend the `applyFloatBlockify` pattern.
- **`relative`** — offsets a box from its normal-flow position at **paint time** (flow is unchanged: the
  box still occupies its in-flow space; only its painted position shifts by `top`/`left`). This is the
  simplest case and a good first task. It interacts with the stacking pass (a positioned box, even
  `relative` with `z-index:auto`, paints in a later Appendix E step than normal-flow content).
- **`absolute`/`fixed`** — taken out of flow, positioned against the **nearest positioned ancestor**
  (the "containing block" for abs-pos; viewport for `fixed`, which in the single-tall-page model is the
  page). This needs the engine to track the current positioned-ancestor rectangle down the layout
  recursion (a new threaded parameter, analogous to how `bandOriginY`/`fc` thread for floats). An
  abs-pos box does not affect siblings' flow.
- **Stacking order / z-index** — the load-bearing addition. The current `AppendItems` is float-aware
  (decorations → floats → inline content) but otherwise tree-order. Positioning requires the **full CSS
  2.1 Appendix E painting order** within each stacking context: (1) bg/border of the stacking-context
  root, (2) negative-z-index stacking contexts, (3) in-flow block backgrounds, (4) **floats**, (5)
  in-flow inline content, (6) z-index:0 / `z-index:auto`-positioned descendants, (7) positive-z-index
  stacking contexts. Generalize the phase-split `AppendItems`: `Floats` becomes one input (the float
  layer) to a richer per-stacking-context collection that also gathers positioned descendants by
  z-index and sorts them. A positioned box with a non-auto `z-index` (and the root) establishes a
  **stacking context**; keep the nested-BFC-as-atom logic (a stacking context paints as a unit in its
  parent's order). **Have the spec reviewer verify the layer ordering with an adversarial overlap test**
  (a positioned box over a float over in-flow text — assert the paint order), exactly as 5a did for the
  float layer.

Each lands with fixtures + golden/WPT tests, degrades gracefully (already: `Position` is recorded but
ignored → normal flow), and recovers at the page boundary.

## Carried-forward deferrals 5b should fold in or scope out

From 5a's spec "Deferred" section (`docs/superpowers/specs/2026-06-24-html-floats-design.md`):
- **Float-height enclosure** and **floats intruding across an `overflow≠visible` BFC boundary** —
  grouped with **5c (overflow)**, NOT 5b. (`overflow≠visible` establishes the enclosing BFC.) Leave
  these for 5c unless they block a positioning test.
- **True shrink-to-fit** (`float:auto` width), **CSS 8.3.1 clearance**, negative-margin float overlap —
  still open; not 5b's concern unless touched.
- The replaced/inline deferrals from #4 (`object-position`, ratio-preserving min/max, percentage-height
  basis, `background-image`, full `vertical-align`, `margin:auto` centering) remain open.

Note: `margin:auto` horizontal centering is still a stub (`usedEdges` computes auto margins to 0). If a
positioning test needs centering it may surface here, but it's independent of positioning.

## Process reminders (held across #1–#5a — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go`/`golangci-lint`/`gofmt` (and `gh pr create`)
  with the sandbox disabled. The branch push works under the sandbox (SSH/git) but the `.git/config`
  write and any HTTPS API call do not.
- **Editor diagnostics LAG badly** — after a subagent adds code you'll see stale "undefined" / "wrong
  arg count" errors and phantom `zz_*` scratch files. **Trust `go build`/`go test`, not the panel.**
  After any review subagent, `find . -name 'zz_*' -delete` and confirm `git status` is clean (5a's
  reviewers left scratch files twice; subagents also occasionally `git stash`/switch branches — tell
  every subagent explicitly: you are on `feat/html-positioning`, do NOT checkout/stash/switch).
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages, not the repo root. **The repo uses NO `//nolint`** and **declines all "modernize" hints**
  (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit `if x < y { x = y }` clamps and indexed
  loops. golangci-lint **does** flag `if !(a && b)` (QF1001) and an **unused unexported struct field**
  (the `unused` check) — write ordering assertions as `if a>=b || b>=c`, and make sure any new field is
  actually read in the same PR.
- **The zero-value `Length` trap** (bit 5a twice): a `cssbox.ComputedStyle` literal that omits
  `Width`/`MaxWidth` reads as an explicit `0` (`{0, UnitPx}`), NOT the cascade's `auto`/`none`. Test
  fixtures built as raw structs (not via `blockBox`, which now defaults them) must set
  `Width`/`Height`/`MaxWidth`/`MaxHeight` to `UnitAuto`. `positionOf`/offset fixtures will hit the same.
- **Test the FLAG COMBINATIONS, not each flag alone** (5a's worst miss): a real float is
  `IsBFC && IsFloat`, and the Task-5 paint tests passed with `IsFloat`-only fragments while the engine
  produced `IsBFC && IsFloat` — so floats painted nothing end-to-end until the `float-figure` golden
  caught it. 5b adds `positioned` + `establishes-stacking-context` flags on top of `IsFloat`/`IsBFC`;
  **explicitly test the combinations** (a positioned float, a positioned inline-block, a z-indexed
  abs-pos inside a relative parent), and **eyeball every golden** — the golden render is what caught the
  paint bug, not the unit tests.
- **The two-stage review earns its keep.** In 5a it caught: a wrong test-expectation in the plan
  (float wrap math), a missing containing-block clamp in the IFC, the end-to-end float paint bug, and
  the zero-value-Length fixture trap. Have spec reviewers **verify load-bearing math/paint-order with
  throwaway adversarial tests** (for 5b: the stacking/z-index paint order and the abs-pos
  containing-block geometry), and **delete the throwaways** (confirm `git status` clean).
- **Subagents implementing a large task** read the exact task lines from the plan file (point them at
  the line range) rather than having the controller re-transcribe 200+ lines — more reliable for
  intricate code. For small tasks, paste the full text.
- **Eyeball every new/changed golden PNG** in the PR (the controller, via the Read tool — not just the
  implementer, who cannot see it). **Confirm no pre-existing golden changed** (`git status --short
  pkg/doctaculous/testdata/ pkg/render/raster/testdata/` should show only new files). A paint-pass
  change like 5b's stacking pass is high-risk for silently reordering existing pages — the no-float/
  no-position pages MUST stay byte-identical (the non-stacking path must reproduce today's order).
- **Propagate review fixes back into the spec/plan**, and **update CLAUDE.md's Done/TODO** when the PR
  lands (move positioning out of the §6 TODO; the TODO already lists positioning as the next slice and
  notes it "generalizes the float paint layer's phase-split `AppendItems`").
