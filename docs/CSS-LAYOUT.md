# CSS and layout — status and open work

Shipped features are inventoried in [../FEATURES.md](../FEATURES.md); the per-item
working checklist with the rationale for each deferral is in
[BACKLOG.md](BACKLOG.md). This file holds what is NOT done.

The engine renders every path below — these are the known approximations, each degrading
gracefully.

## Selectors

8. **CSS selector coverage** (`pkg/internal/css/selector.go`) — the engine supports type, class, id,
  universal, descendant, grouping, and the structural pseudo-classes, but has NO child (`>`),
  adjacent-sibling (`+`), general-sibling (`~`), attribute (`[foo]`, `[foo=bar]`), `:not()`/`:is()`/
  `:where()`, or namespace (`svg|rect`) selectors. `parseOneSelector` splits on whitespace
  (`strings.Fields`), so `>` and `[attr]` cannot be represented. These **fail safe** (a rule with an
  unsupported selector is dropped, never mis-matched). The **silent** half is now CLOSED: a dropped
  selector is recorded on `Stylesheet.Unsupported` and reported warn-once per construct by
  `NewResolver` (HTML/DOCX) and `pkg/internal/svg`'s index (SVG-internal `<style>`) — see FEATURES.md. This
  affects **HTML as much as SVG**, since `pkg/internal/css` is shared. Two resvg fixtures are excluded from
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

- **Vertical writing modes — remaining work.** A single vertical line ships, with
  `text-orientation` deciding which way each glyph faces (see FEATURES.md). What is left is mostly
  what needs more than one line, plus two sizing seams:

  - **Vertical line wrapping**, and with it `vertical-lr`. A run longer than the block extent
    currently overflows and logs. Wrapping needs a break loop against the block extent (the breaker
    itself is axis-neutral — it takes a scalar limit — so this is placement, not breaking), and once
    there are multiple lines they must stack: right-to-left for `vertical-rl`, left-to-right for
    `vertical-lr`, which is the only difference between the two values.
  - **The `Vertical_Orientation` table (UAX #50).** `text-orientation: mixed` decides upright-vs-
    rotated per glyph from this Unicode property, and neither the standard library nor `textlayout`
    ships it, so the check approximates it from script tables plus the CJK punctuation and
    full-width blocks. Vendoring the real table is the correct fix — check `DEPENDENCIES.md` first,
    and note the approximation deliberately errs toward rotating, so the failure mode of the gap is
    visible rather than silent.
  - **Shrink-to-fit sizing on the block axis.** An inline-block, float, or auto-width absolute box in
    a vertical mode is sized by `measureMaxContent`/`measureMinContent`, which shape the content and
    break it at a *width* — so the box comes out as wide as its text is long instead of about one em.
    Measured: an inline-block holding "ABCDEF" at 20px is 78.9pt wide. It logs. Transposing means
    turning `measureContent`, which table, grid, flex and inline-block sizing all share, so it wants
    its own change with its own tests rather than riding along on a feature branch. Block-level
    vertical boxes do not go through it and are correct today.
  - **Atomic inline boxes, hard breaks, and decorations** in a vertical line: each is skipped with a
    log today. Decorations need span geometry computed on the block axis — `appendDecoRules` and
    `appendInlineBoxDecorations` build X ranges throughout.
  - **Float avoidance.** The vertical path places its baseline in the middle of the full content box
    and does not consult the float context, so a vertical line beside a float overlaps it. This is
    the one gap that paints WRONG ink rather than omitting it, so it logs. Fixing it means insetting
    the baseline against `leftEdge`/`rightEdge` — cheap for one line, and properly solved by the same
    band query wrapping needs.
  - **Alignment and justification** along the vertical axis: `Place` is pure scalar arithmetic and
    would transpose cleanly, but nothing calls it on the vertical path yet.

  The vertical path deliberately does NOT reuse the horizontal loop's float-band, alignment and
  indent machinery — each is stated in X, and threading an axis flag through it was rejected in
  favour of transposing at the `layoutInline` boundary, which keeps every horizontal document on its
  existing path. If wrapping lands, revisit that call: the duplication is cheap for one line and
  would stop being so.

  Out of scope regardless: vertical alternate glyph forms (`vert`/`vrt2` GSUB) and
  `text-combine-upright`.

- **Web-font descriptors** — synthetic bold/oblique, `unicode-range` subsetting, `font-display`,
  variable-font axes, `local()` beyond the disk adapter; a content-addressed fetch cache (FaceCache
  is keyed `(family, style)`).

## Units

- **`rem` resolves against the element's font size, not the root** — `pkg/internal/css/value.go` folds `rem`
  into `UnitEm` at parse time. The CSS `filter` property resolves it correctly (its lengths resolve
  at paint time, where the root is reachable), so `rem` is currently right for `filter` and wrong for
  every other property. Modelling it properly needs a distinct `UnitRem` carried to where the root
  font size is known, which every `Length` consumer would have to resolve.

## Filters

- **CSS `filter:`** — `backdrop-filter`
  (needs the backdrop, not the element's own pixels — a different mechanism); native PDF filter
  emulation via soft masks (PDF output currently paints filtered content **unfiltered**, keeping it
  vector rather than rasterizing a page region). The over-cap/off-device region and the 4-deep
  nesting cap now report through `PaintPageWithOptions`'s `Options.Logf` — see FEATURES.md.
  Still open: the surface caps stay UNREPORTED on the PDF path, deliberately, because
  `pkg/internal/pdfwrite` already says once per document that every filter paints unfiltered there.
  Also: the five colour-matrix helpers are DUPLICATED between
  `pkg/internal/layout/paint/cssfilter.go` and `pkg/internal/svg/filterfunc.go` — they agree today (verified
  byte-identical across all ten functions) by being kept in step, not by construction; moving them to
  a shared package would make that structural.
