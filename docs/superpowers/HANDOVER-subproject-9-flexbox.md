# Handover — Sub-project 9: CSS flexbox (`display: flex`)

**Status:** Not started. Sub-project 8 (web fonts) is **DONE** — a CSS `@font-face` family now resolves to a
real downloaded face (raw TTF/OTF + WOFF1 + WOFF2 incl. the glyf/loca transform), fetched through the
existing `ResourceLoader`, with `local()` via a `SystemFontProvider`/`DiskFontProvider`. See CLAUDE.md
"Done" (the web-fonts bullet) and `docs/superpowers/specs/2026-06-26-html-webfonts-design.md`. It merged as
the tip of the HTML stack: **PR #12 (`feat/html-webfonts`) on top of #11 (`feat/html-tables`)**.

Flexbox is the **next** roadmap item (CLAUDE.md §6, first in the remaining-slices list now that web fonts
is off it). Unlike web fonts (a font-infrastructure slice), flexbox is a **real layout algorithm** — a new
formatting context alongside the block, inline, and table ones. It is the closest analogue to **sub-project
7 (tables)**: a self-contained layout mode above the block formatting context that reuses the existing
block+inline layout for the *contents* of each flex item. Read the tables slice
(`docs/superpowers/specs/2026-06-25-html-tables-design.md` + the tables Done bullet) before starting — the
shape of that work (new layout file, box-tree vocabulary, fixups, a golden + reftests, RTL deferred) is the
template for this one.

**Next action:** Same flow as every prior slice — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / stacked PR. Flexbox is **large** (the
CSS Flexible Box algorithm is intricate — flex-basis resolution, grow/shrink distribution, cross-axis
alignment, wrapping); a written spec + plan are essential, and the scope decision (how much of the spec to
land in slice 1 vs defer) is the load-bearing brainstorm question.

---

## The PR stack — where to branch from

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats ← #7 positioning ← #8 overflow ← #9 z-index ← #10 zindex-6b
     ← #11 tables ← #12 web-fonts
```

If the stack has merged to `main` by the time you start, branch flexbox off `main`. Otherwise branch **off
`feat/html-webfonts`** (the tip, PR #12). Name it e.g. `feat/html-flexbox`. Tell every subagent: you are on
`feat/html-flexbox`, do NOT checkout/stash/switch branches, do NOT commit unless asked, and delete any
`zz_*`/`*probe*` scratch file before finishing (confirm `git status` clean + `find . -name 'zz_*'` empty).
The per-task reviewers WILL write throwaway probe tests — make deleting them an explicit instruction (across
every prior slice the editor panel showed deleted probes as phantoms; trust `git status`/`find`, not the
panel).

## Scope (propose this in the spec; the in-vs-deferred split is the key brainstorm decision)

CSS Flexible Box Layout (CSS Flexbox, https://www.w3.org/TR/css-flexbox-1/). Turn the **block fallback** a
`FlexFC` box currently uses (it logs "`FlexFC ... not yet implemented; falling back to block normal flow`"
and stacks children as blocks — `pkg/layout/css/block.go:505-511`) into a **real flex layout**. The
recommended slice-1 scope (confirm/adjust in brainstorm):

**Likely in scope (single-line flex, the common case):**
- **The flex container + flex items** — `display: flex` (and `inline-flex`) establishes a flex formatting
  context; in-flow children become flex items (with the usual blockification of inline-level items, CSS
  Flexbox §4). Anonymous flex items wrap contiguous runs of inline content / text (§4 — the flex analogue of
  the anonymous-table-box and inline-in-block fixups already in `pkg/layout/css/anon.go`/`tablefix.go`).
- **Main/cross axis from `flex-direction`** — `row` / `row-reverse` / `column` / `column-reverse`. (RTL
  affects `row` start/end — see deferral.)
- **`flex-basis` + `flex-grow` + `flex-shrink`** (and the `flex` shorthand) — resolve each item's base size,
  then distribute free space: grow when there's surplus, shrink (min-content-floored) when there's deficit.
  This is the heart of the algorithm (CSS Flexbox §9.7) and the biggest correctness risk — build it
  test-first with hand-computed expected sizes.
- **Main-axis alignment** — `justify-content` (`flex-start`/`flex-end`/`center`/`space-between`/
  `space-around`/`space-evenly`).
- **Cross-axis alignment** — `align-items` + per-item `align-self` (`stretch`/`flex-start`/`flex-end`/
  `center`/`baseline`). `stretch` is the default and interacts with item cross-size.
- **Item min/max sizing + the automatic minimum size** (`min-width:auto` → min-content, CSS Flexbox §4.5 —
  the famous "flex items don't shrink below min-content" rule). At minimum honor explicit `min-*`/`max-*`;
  the auto-minimum is fiddly but important for realistic pages.
- **Each flex item's contents lay out through the existing block/inline path** (reuse `layoutInterior` /
  the block+inline engine, exactly as a table cell does — a flex item establishes a BFC for its contents).

**Likely deferrals (state each in the spec's Degradation section with a graceful fallback):**
- **Multi-line flex (`flex-wrap: wrap`/`wrap-reverse`) + `align-content`.** Single-line (`nowrap`, the
  initial value) is a complete, useful slice; wrapping + `align-content` (aligning the lines) is a large
  add and a natural sub-project 9b. If deferred, `flex-wrap: wrap` must degrade to `nowrap` (overflow rather
  than wrap), logged.
- **RTL / `direction`.** The engine has **no** `direction`/bidi support anywhere (it was the sole table
  deferral too). `row-reverse`/`column-reverse` are achievable without bidi (they're explicit), but
  RTL-driven main-start/end in `row` is not — parse `direction` but act LTR, logged. Pin this in the spec.
- **`order`** (reordering items by an integer) — small but additive; defer unless cheap.
- **Baseline cross-axis alignment** across items with different fonts — the baseline *participation*
  mechanics may be partial; `align-items: baseline` can approximate to `flex-start` initially if needed
  (state it).
- **`gap`/`row-gap`/`column-gap` on flex** — common in modern CSS; include if affordable, else defer with a
  note (treated as 0).

The in-vs-deferred line (especially **single-line-first vs wrap-included**) is THE brainstorm decision.
Recommendation: **single-line flex (`nowrap`) fully — direction, basis/grow/shrink, justify-content,
align-items/self, min/max + auto-min — and defer wrap/`align-content` to 9b.** That mirrors how tables
landed the core algorithm with RTL deferred.

## What already exists (the foundation — grounded in the code, verified for this handover)

- **`pkg/layout/css/block.go:505-511`** — the `default` case of the formatting-context switch handles BOTH
  `FlexFC` and `GridFC` with `layoutBlockChildren` (block fallback) + a debug log. **This is the seam you
  replace:** add `case cssbox.FlexFC: in = e.layoutFlex(...)` next to the existing
  `case cssbox.TableFC: in = e.layoutTable(...)` (line 503). The table slice is the exact precedent.
- **`pkg/layout/cssbox/box.go`** — the box vocabulary already exists: `DisplayFlex` (line 71) and `FlexFC`
  (line 91) are defined. Box generation already classifies `display:flex` →
  `BoxBlock`/`DisplayFlex`/`FlexFC` (`pkg/layout/css/build.go:282-283`). So a flex box already arrives at the
  engine with the right `Formatting` — you do not need new box-tree plumbing to *recognize* flex, only to
  *lay it out*. (You WILL add flex-item box vocabulary + the anonymous-flex-item fixup.)
- **`pkg/layout/css/anon.go`** — `normalize` does the inline-in-block / block-in-inline fixups and, at
  line 64-68, **deliberately leaves table/flex/grid boxes' keyword-derived context intact** ("table/flex/grid:
  keep the keyword-derived context"). The anonymous-flex-item fixup (wrapping inline runs as flex items) is
  the flex analogue of `tablefix.go`'s anonymous-table-box insertion — add it here or in a new `flexfix.go`,
  called from `normalize` like `fixupTables` is from `Build`.
- **`pkg/layout/css/table.go`** — the **structural template** for `flex.go`: a self-contained layout
  function (`layoutTable`) that computes a geometry model, lays out each child's contents through the shared
  block/inline path, and returns positioned fragments. `table.go:462-489` even has a free-space
  distribution-in-proportion-to-flex-room helper that is conceptually the same shape as flex grow/shrink —
  read it for the pattern (do NOT share the code; flex's distribution has its own rules: frozen items,
  min/max clamping, multiple passes per CSS Flexbox §9.7).
- **`pkg/layout/css` block + inline engine** — a flex item's *contents* lay out through the existing
  `layoutInterior`/block formatting machinery (a flex item establishes a BFC), exactly like a table cell.
  Reuse it; do not reimplement block/inline layout. The **min/max-content measurement** added for tables
  (`pkg/layout/css/measure.go`) is what you need for `flex-basis: auto`/`content` and the automatic minimum
  size — reuse it.
- **`pkg/css` cascade** — **no flex properties are parsed today** (`flex-direction`, `flex-grow`,
  `flex-shrink`, `flex-basis`, the `flex` shorthand, `justify-content`, `align-items`, `align-self`,
  `flex-wrap`, `align-content`, `order`, `gap` are all absent from the cascade — verified). You add them to
  `ComputedStyle` + the cascade, plus the `flex` shorthand expansion (mirroring the `margin`/`border`
  shorthand expansion already in `pkg/css`). Unknown/unsupported values must degrade (skipped), as the
  cascade already does.
- **`pkg/layout/inline`** (the shared inline core) — **untouched** by flex (as it was by tables/positioning).
  Flex needs no new inline primitive; items lay out their contents through the existing path.
- **The `render.Device` seam + the PDF pipeline** — untouched. Flex is pure layout above the block FC.

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

Flexbox lives entirely in the **CSS layout engine** (`pkg/layout/css`) + the **cascade** (`pkg/css`). The
`render.Device` seam, the PDF pipeline, the DOCX pipeline, and the shared inline core are **untouched** — a
flex container produces ordinary positioned fragments that paint through the existing fragment/paint path
(backgrounds, borders, and the item contents all already paint). Concretely:

- **`pkg/css`** — add the flex properties to `ComputedStyle` + cascade + the `flex` shorthand expansion. No
  layout here.
- **`pkg/layout/cssbox`** — add flex-item box vocabulary if needed (a flex item is a block-level box with a
  flex-item role; you likely need a marker so the fixup + layout know which children are items). The
  `unused`-linter rule applies: add a field only when the consuming task reads it.
- **`pkg/layout/css`** — the new `flex.go` (`layoutFlex` + the flex algorithm), an anonymous-flex-item fixup
  (`flexfix.go` or in `anon.go`), wired into `normalize`/the FC switch in `block.go`. Reuse
  `measure.go` (min/max-content) and the block/inline engine for item contents.
- **`pkg/layout/css/fragment.go`** — flex items are positioned fragments; confirm the existing flatten/paint
  path covers them (it should — they're block boxes at computed offsets, like table cells).

**No new dependencies.** Flexbox is pure-Go layout math; nothing to add to `go.mod`.

## Testing (this project lives or dies on its test corpus — see CLAUDE.md "Testing")

Every layer gets tests **in the same PR** (new feature ⇒ new fixture + test). Hermetic, no network.

- **`pkg/css` flex-property parse/cascade tests** — `display:flex`, `flex: 1 1 200px` (shorthand →
  longhands), `flex-direction`, `justify-content`, `align-items`/`align-self`, etc. parse to the right
  `ComputedStyle`. Assert the parsed structure directly (like the existing cascade/shorthand tests).
- **`pkg/layout/css` flex-geometry unit tests** — the load-bearing layer. Hand-compute expected item
  main-sizes and positions for: pure grow (surplus split by `flex-grow`), pure shrink (deficit split by
  `flex-shrink × base`, min-content-floored), `flex-basis` resolution (length / percentage / auto/content),
  each `justify-content` value (the free-space distribution), each `align-items`/`align-self` value
  (cross-axis placement incl. `stretch`), `flex-direction: column` (axes swapped), and the automatic minimum
  size. These mirror the tables slice's grid/width-solve/span unit tests — assert fragment rects directly.
- **Anonymous-flex-item fixup tests** — inline runs / text inside a flex container become anonymous flex
  items (structural assertions, like the anon-table-box tests).
- **Golden images** (`pkg/doctaculous`, `htmlGoldens` + committed PNGs) — a few eyeball-able flex layouts:
  a `justify-content: space-between` row, a `flex-grow` distribution, an `align-items: center` row of
  different-height items, a `flex-direction: column`. Generate with
  `go test ./pkg/doctaculous -run TestHTMLGolden -update`, then **the controller eyeballs every new PNG via
  the Read tool** (the implementer has no image vision — have it STOP after `-update` and hand back the PNG
  paths). This caught real bugs every prior slice.
- **WPT-style reftests** (`pkg/doctaculous`, `wptReftests` + `NAME.html`/`NAME-ref.html`) — flex has GREAT
  reftest potential (unlike fonts): a flex layout == the same boxes placed by hand at the computed positions
  (absolute/inline-block). E.g. `justify-content: space-between` of three fixed-width items == three
  inline-blocks at the hand-computed x-offsets; a `flex-grow` row == divs at the grown widths. Author
  several — they're the strongest correctness proof for layout math.
- **Byte-identical guard.** Flex ADDS a layout mode; **no existing page should change** (every current
  fixture uses block/inline/table, which are unaffected). After each task, run goldens/reftests WITHOUT
  `-update` and confirm `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` shows only
  NEW files. A changed existing golden means flex leaked into the block/table path — fix before proceeding.
- **Degradation tests.** Each deferral (wrap→nowrap, RTL→LTR, `order` ignored, `align-items:baseline`
  approximation, `gap`→0 if deferred) degrades gracefully and is covered by a test asserting no panic + the
  documented fallback + the debug log. An empty/degenerate flex container is a zero-size fragment (no panic).

## Process reminders (carried across #1–#8 — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`,
  `git push` over HTTPS) with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with
  cache/permission errors that are NOT real failures; if you see "no go files to analyze" from
  `golangci-lint`, that's the sandbox — re-run disabled.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared"/"not in go.mod" errors and **phantom `zz_*`/`*probe*` scratch files**
  that no longer exist on disk. Trust `go build`/`go test` and `find . -name 'zz_*'`, not the panel. (Across
  sub-project 8 the panel showed `undefined: LoadSFNT`, `could not import brotli`, and several deleted probe
  files — all phantom; the real `go test`/`golangci-lint` were green every time.)
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root. **NO `//nolint`**;
  the repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int) — keep explicit
  `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`. golangci-lint
  flags `if !(a && b)` (QF1001 — write the De Morgan form) and bare `x.Close()` (errcheck — use
  `_ = x.Close()`). The `unused` linter IS enforced — a struct field you add must be *read* by code in the
  same PR; if a field is for a later task, defer adding it until that task reads it (in sub-project 7 the grid
  struct fields had to be removed and re-added per consuming task to keep `unused` happy).
- **Verify against the spec + the actual code, don't trust the handover blindly.** In sub-project 6b the
  handover's central premise was wrong per CSS 2.1; in 7 the plan read a nil field for a static cell; in 8 the
  WOFF2 transformed-glyf header was `reserved+optionFlags`, NOT the `Fixed version` the plan first wrote — the
  implementer caught it against the W3C spec. **Confirm the CSS Flexbox §9 algorithm steps against the spec
  (a `WebFetch` of https://www.w3.org/TR/css-flexbox-1/) before encoding the grow/shrink/align math** — the
  flex resolution algorithm has a specific multi-pass freezing structure that's easy to get subtly wrong. **A
  change that forces you to invert an existing, passing test is a red flag — stop and verify the spec.**
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review** earn their
  keep — across #7 they caught four real bugs the implementers' tests missed; in #8 the holistic review found
  that the WOFF2 composite-glyph path + the triplet sub-decoders had **zero** direct test coverage (the
  fixture was all simple glyphs) and that the face cache re-fetched per case-variant — both fixed before
  merge. **Have spec reviewers verify the load-bearing logic adversarially** (the grow/shrink distribution,
  the align math, the auto-minimum) **with throwaway tests, and delete the throwaways.** **Render real pages**
  at milestones (the controller, via the Read tool) — every visible bug across the project was caught by
  rendering, not by a passing unit test.
- **Strengthen weak assertions.** In #8 the shared glyph-compare helper only checked outline *presence*, not
  geometry; the holistic review caught it and it was upgraded to a full `reflect.DeepEqual` on the segment
  list. For flex, make geometry assertions compare actual rects (x/y/w/h), not just "an item exists" — a flex
  bug is a wrong *position/size*, which presence checks miss.
- **Prefer the simpler mechanism.** #7 reused the existing `BorderItem` paint path for collapsed borders
  instead of a new primitive; #8 reused `parseProgram` (decode WOFF *to* sfnt) and the existing
  `ResourceLoader`/`FaceCache` seams. For flex: reuse `layoutInterior` for item contents, `measure.go` for
  min/max-content, and the existing fragment/paint path — invent only the flex algorithm itself.
- **Update CLAUDE.md when the PR lands** — move flexbox from the §6 remaining-slices list into a new "Done"
  bullet (describing the flex FC, the properties + shorthand, the algorithm scope, what goldens/reftests
  cover, and the deferrals — wrap/RTL/etc.), and update the §6 done-slices parenthetical. Keep the Done/TODO
  the honest source of truth, as #7 and #8 did.

## Open questions to resolve in brainstorm (not blocking the start)

- **Single-line first, or wrap included?** Single-line (`nowrap`) is a complete, shippable slice; multi-line
  (`flex-wrap` + `align-content`) is a large add. Recommendation: **single-line first, wrap as 9b** — mirrors
  tables landing the core with RTL deferred.
- **How much of the automatic minimum size (§4.5)?** The `min-width:auto` → min-content rule is important for
  realistic pages but fiddly. Land it (you have `measure.go`), or approximate to `0`/explicit-min first and
  defer the auto-min? (Recommendation: land it — it's what makes flex behave like the web.)
- **`gap` on flex?** Common in modern CSS, cheap-ish (insert spacing between items on the main axis, and
  between lines on the cross axis when wrap lands). Include in slice 1 or defer to 0?
- **`order`?** Small reorder-by-integer step. Include if cheap, else defer (items in document order).
- **`align-items: baseline`** — full cross-baseline participation, or approximate to `flex-start` first?
- **`inline-flex`** — include (an inline-level flex container) or defer to block-level `flex` only first?

None block branching and writing the spec; they shape its scope. The recommendation, mirroring the tables
slice: **land single-line flexbox fully** (container + items + anonymous-item fixup, `flex-direction`
row/column/reverses, `flex-basis`/`grow`/`shrink` + the `flex` shorthand, `justify-content`,
`align-items`/`align-self` incl. `stretch`, min/max + the automatic minimum size, item contents through the
existing block/inline path), **with `gap` if affordable, and defer `flex-wrap`+`align-content` (→ nowrap),
RTL (→ LTR), `order`, and full baseline alignment** — each with a graceful, tested fallback, and an
eyeball-verified golden + several reftests. Pin the exact line in the spec.
