# CSS and layout — status and open work

Shipped features are inventoried in [../FEATURES.md](../FEATURES.md); the detailed
per-item working checklist with the rationale for each deferral is in
[FIDELITY-BACKLOG.md](FIDELITY-BACKLOG.md). This file holds what is NOT done.

The engine renders every path below — these are the known approximations, each degrading
gracefully.

## Selectors

8. **CSS selector coverage** (`pkg/css/selector.go`) — the engine supports type, class, id,
  universal, descendant, grouping, and the structural pseudo-classes, but has NO child (`>`),
  adjacent-sibling (`+`), general-sibling (`~`), attribute (`[foo]`, `[foo=bar]`), `:not()`/`:is()`/
  `:where()`, or namespace (`svg|rect`) selectors. `parseOneSelector` splits on whitespace
  (`strings.Fields`), so `>` and `[attr]` cannot be represented. These **fail safe** (a rule with an
  unsupported selector is dropped, never mis-matched). The **silent** half is now CLOSED: a dropped
  selector is recorded on `Stylesheet.Unsupported` and reported warn-once per construct by
  `NewResolver` (HTML/DOCX) and `pkg/svg`'s index (SVG-internal `<style>`) — see FEATURES.md. This
  affects **HTML as much as SVG**, since `pkg/css` is shared. Two resvg fixtures are excluded from
  the SVG corpus for this reason (see `testdata/svg/resvg/README.md`). Still wanted, roughly in
  value order: `>` (common in hand-authored SVG and real stylesheets), attribute selectors, then the
  sibling combinators. Related and unfixed: `parseOneSelector`'s whitespace split also drops the
  valid spaced `An+B` form (`:nth-last-child(2n + 1)`), which the same parser rework would fix.


## Layout

- **Flexbox** — a single flex line taller than the page moves whole rather than splitting its
  items. (`flex-wrap`, `align-content`, the cross gap and `flex-flow` all ship, and wrapped rows
  paginate between lines — see FEATURES.md.)
- **Grid** — named-line placement (`[name]` tokens parsed-and-ignored → auto-placement), `subgrid`
  (→ `none`), `auto-fit` empty-track collapse approximate. (The ROW-track "width-proxy" entry was
  stale: row tracks already size from laid-out item heights — see backlog I4.)
- **Absolute/replaced sizing edge cases** — precise static-position solve for an all-`auto`-offset
  abs box (C1), `bottom`-only auto-height abs box (C5, needs vertical shrink-to-fit), percentage
  `top`/`bottom`/`height` against an auto-height CB (C4/D3), `position:relative` on a text-only
  inline box (C6, no fragment to carry the offset).
- **Pagination** — mid-cell / mid-item (flex/grid) content splitting of a genuinely-indivisible
  over-tall row/item overflows; positioned/float distribution within a different-width named-page run.

## Text and fonts

- **RTL / bidi approximations** — GPOS vertical offsets are not applied, so marks sit on the
  baseline; `font-feature-settings` is not plumbed through; nested bidi embeddings deeper than one
  level collapse; and a digit sequence embedded in RTL text reverses with its word on extraction.
  (The bidi pipeline itself ships in full — UAX#9 reordering, bracket mirroring, Arabic shaping via
  harfbuzz, bundled Noto Sans Hebrew / Noto Naskh Arabic — see FEATURES.md.)

- **`vertical-align`** — full keyword set (atom-baseline mechanics landed); `margin:auto`
  block centering; deferred margin-collapse edge cases (empty-block collapse-through, clearance,
  `min-height` interaction).

- **Web-font descriptors** — synthetic bold/oblique, `unicode-range` subsetting, `font-display`,
  variable-font axes, `local()` beyond the disk adapter; a content-addressed fetch cache (FaceCache
  is keyed `(family, style)`).

## Units

- **`rem` resolves against the element's font size, not the root** — `pkg/css/value.go` folds `rem`
  into `UnitEm` at parse time. The CSS `filter` property resolves it correctly (its lengths resolve
  at paint time, where the root is reachable), so `rem` is currently right for `filter` and wrong for
  every other property. Modelling it properly needs a distinct `UnitRem` carried to where the root
  font size is known, which every `Length` consumer would have to resolve.

## Filters

- **CSS `filter:`** — `backdrop-filter`
  (needs the backdrop, not the element's own pixels — a different mechanism); native PDF filter
  emulation via soft masks (PDF output currently paints filtered content **unfiltered**, keeping it
  vector rather than rasterizing a page region). Two degradations are **silent** because
  `pkg/layout/paint` has no logger — `PaintPage` takes only a Device, a Page, and a Matrix, unlike
  the SVG side whose Renderer carries a `Logf`: the over-cap/off-device region (`maxCSSFilterPixels`
  is 4M pixels, which a 300 DPI A4 page at ~8.7M exceeds, so a full-page filter degrades at print
  resolution) and the 4-deep nesting cap. Threading a logger through `PaintPage` is a small, contained
  fix worth doing. Also: the five colour-matrix helpers are DUPLICATED between
  `pkg/layout/paint/cssfilter.go` and `pkg/svg/filterfunc.go` — they agree today (verified
  byte-identical across all ten functions) by being kept in step, not by construction; moving them to
  a shared package would make that structural.
