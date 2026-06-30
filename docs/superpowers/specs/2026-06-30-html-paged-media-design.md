# HTML rendering — CSS Paged Media (`@page`, `break-inside`, widows/orphans, running headers/footers)

**Date:** 2026-06-30
**Status:** Design approved (scope: full — incl. widows/orphans and running headers/footers); ready for
implementation plan.
**Branch:** `feat/html-paged-media` (off `main`; the pagination stack is fully merged — confirm with
`git merge-base --is-ancestor origin/main HEAD` is irrelevant, just branch from `main`).
**Spec references:** CSS Paged Media Module Level 3 (`@page` rule, page size, margins, the 16 margin boxes,
named pages, `page:` property); CSS Fragmentation Module Level 3 (`break-inside`, `widows`, `orphans`, the
class-A/B/C break points and the forced/unforced break model — the prior slice shipped the forced-break
vocabulary). This slice implements the **full** spec surface the project's "maximal, most browser-faithful"
directive calls for, with documented approximations only where the single-tall-layout-then-post-pass model
genuinely cannot reach a behavior without a relayout the engine does not yet do.

## Summary

The pagination slice (sub-project 12, `2026-06-28-html-pagination-design.md` + its two fidelity passes) made
`OpenHTML`/`OpenURL` paginate via an explicit `WithPageSize(w, h)` option and a **between-top-level-blocks
post-pass** (`pkg/layout/css/paginate.go`). It deliberately deferred everything `@page`-shaped: page size and
margins from CSS, `break-inside: avoid`, widows/orphans (which need **mid-block, line-level** fragmentation —
the one thing the post-pass model explicitly does not do), and running headers/footers. This is the last
non-fidelity HTML slice (CLAUDE.md item 6).

This slice closes all of it:

1. **`@page` rule capture + cascade** (`pkg/css`): parse `@page` (and `@page :first`/`:left`/`:right` and
   named `@page name`) into a side table on `Stylesheet` (the `@font-face` precedent), and resolve the
   **used page** — size, margins, and the 16 margin-box content/style — for a given page index/name/position.
   Add the `page` property to `ComputedStyle` (selects a named page for a box's generated pages) and `widows`
   / `orphans` (integers, inherited, initial 2) alongside the existing `break-before`/`break-after`; add
   `break-inside`.
2. **Page geometry from `@page`** (`pkg/doctaculous` + `pkg/layout/css`): when the document has an `@page`
   rule, its `size` and `margin` drive pagination **even without `WithPageSize`** — `size: A4 landscape` plus
   `margin: 2cm` produces 1123×794pt pages with the content laid out in the 1123−2×(2cm) margin box.
   `WithPageSize` still works and still wins when the caller passes it explicitly (an API override beats the
   stylesheet, like an inline `style` beats author CSS); a new `WithDefaultPaged()` opts into "paginate using
   the document's `@page`, else Letter."
3. **`break-inside: avoid` + keep-together** (`pkg/layout/css/paginate.go`): the bucketer keeps an
   `break-inside: avoid` block whole — pushing it to the next page rather than letting the (existing)
   over-tall-overflow path cut it — and honors `break-before/after: avoid` between adjacent blocks.
4. **Widows & orphans via mid-block line fragmentation** (`pkg/layout/css/paginate.go` + a new
   `pkg/layout/css/fragmentpage.go`): the post-pass gains the ability to **split a block that establishes an
   inline formatting context at a line boundary**, honoring `orphans` (min lines left at the bottom of the
   page before a break) and `widows` (min lines carried to the top of the next page). This is the structural
   addition the prior slice deferred. It is bounded to **block-with-lines** fragmentation (the overwhelmingly
   common case — paragraphs); mid-table-row / mid-flex-item / mid-grid-item splitting stays deferred (each
   still overflows its page, logged), because those need formatting-context-internal fragmentation the engine
   does not model.
5. **Running headers/footers via `@page` margin boxes** (`pkg/layout/css/marginbox.go`, new): the 16 `@page`
   margin boxes (`@top-center`, `@bottom-right`, …) with `content:` — including the page counters
   `counter(page)` / `counter(pages)` and `content: "text"` / `string()` (the `string-set` subset) — render
   as **per-page generated content** in the page's margin band. This reuses the existing inline layout to lay
   out the margin-box content and the existing flatten to paint it.

**Backwards compatibility / byte-identical guard.** A document with **no `@page` rule** rendered **without**
`WithPageSize` is byte-identical to today (single tall page; the whole existing golden/reftest corpus is
unchanged). The new `ComputedStyle` fields default to their initial values and are read only by the
post-pass; the `@page` side table is empty for every existing fixture. `widows`/`orphans` default to 2 but
**only affect a paginated run** (the post-pass is the sole reader) — an un-paginated document never splits, so
the defaults are inert there.

**No `render.Device`, PDF, DOCX, or shared-inline-core change.** As with the pagination slice, this is all on
the reflow side at/above the `Layout`→pages boundary, plus CSS parsing. **No new dependency** (stdlib only).

## Architecture / seam fit

The seam map (CLAUDE.md "Architecture") is unchanged. Touch points, all reflow-side:

- **`pkg/css`** — `@page` capture into `Stylesheet.Pages []PageRule` (mirrors `Stylesheet.FontFaces`); a
  `ResolvePage(...)` that produces a `UsedPage` (size + margins + margin-box content) for a page
  index/position/name; new `ComputedStyle` fields `Page` (string), `BreakInside` (string), `Widows`,
  `Orphans` (int). The cascade is otherwise untouched — `@page` is a side table, not a selector that matches
  elements (its declarations style *pages*, not boxes).
- **`pkg/layout/css`** — `paginate.go` grows: page geometry from a `UsedPage` (size + margin band), the
  keep-together logic (`break-inside`/`break-*: avoid`), and line-level block splitting for widows/orphans
  (the new `fragmentpage.go` holds the block-with-lines splitter so `paginate.go` stays the orchestration).
  A new `marginbox.go` lays out and flattens the `@page` margin boxes per page. The fragment-tree builder
  (`layoutTree`) stays **untouched** — everything remains a post-pass over the finished tree, *plus* the new
  ability to split a leaf block's `Lines` (a local, page-space-only transform — no relayout).
- **`pkg/doctaculous`** — thread the document's resolved `@page` (parsed from the aggregated author sheets,
  the same aggregation `BuildWithFonts` already does for `@font-face`) into `LayoutPaged`; add
  `WithDefaultPaged()`; keep `WithPageSize` as the explicit override.

The `render.Device` seam, `pkg/render/raster`, the PDF pipeline (`pkg/pdf*`), the DOCX pipeline
(`pkg/docx*`, `pkg/layout` flat engine), and the shared inline core (`pkg/layout/inline`) are **untouched**.

## Component 1 — `@page` capture + cascade (`pkg/css`)

### 1a. Capturing the rule (the `@font-face` precedent)

`pkg/css/parse.go` already special-cases `@font-face` in the at-rule branch (`parse.go:45`) and consumes
every other at-rule's block. Add an `@page` arm next to it. `@page` differs from `@font-face` in two ways the
parser must handle:

- It has an optional **prelude** beyond the keyword: `@page`, `@page :first`, `@page :left`, `@page :right`,
  `@page :blank`, `@page name`, `@page name:first`. Parse the prelude into `{ name string; pseudo pagePseudo
  }` (pseudo ∈ {none, first, left, right, blank}).
- Its body holds **both declarations** (`size`, `margin`, `margin-top`, …, and any normal property that
  applies to the page box like `background`) **and nested margin-box rules** (`@top-center { content: … }`).
  So the `@page` body is parsed by a small dedicated parser that splits declarations from nested `@`-blocks,
  rather than `parseDeclarations` alone.

```go
// pkg/css/page.go  (new)

// PageRule is one captured @page rule: its selector (optional name + pseudo), the
// page-box declarations (size/margin/normal properties), and the margin-box rules.
// The cascade does not match @page against elements; ResolvePage combines the
// matching rules for a given page into a UsedPage. Source order is preserved as a
// cascade tie-breaker (like FontFace).
type PageRule struct {
    Name   string            // "" for an un-named @page
    Pseudo PagePseudo        // PageNone / PageFirst / PageLeft / PageRight / PageBlank
    Decls  []Declaration     // size, margin*, and any normal property on the page box
    Margin []MarginBoxRule   // @top-left … @bottom-right content rules
    Order  int               // document order, for cascade tie-break
}

// MarginBoxRule is one @page margin box (@top-center, @bottom-right, …) with its
// declarations (content, plus text styling: font, color, text-align, …).
type MarginBoxRule struct {
    Box   MarginBoxSlot      // which of the 16 boxes
    Decls []Declaration
}
```

`Stylesheet` gains `Pages []PageRule` (exactly like `FontFaces []FontFace`). `Parse` appends to it when the
prelude's first token (case-insensitively) is `@page`.

### 1b. Resolving the used page

```go
// ResolvePage returns the UsedPage for the page at zero-based index i (page 1 ==
// i 0) with optional named page `name`. It applies the CSS Paged Media cascade over
// all PageRules whose selector matches this page:
//   - an un-named, un-pseudo @page matches every page (the base);
//   - @page :first matches only page index 0;
//   - @page :left / :right match by parity (right == recto == odd page number ==
//     even index in a left-to-right book starting on the right; we use the CSS
//     default: page 1 is :right). :blank matches a page with no content (only
//     reachable via forced breaks producing an empty page — rare; supported);
//   - @page name matches a page generated by a box whose `page: name` selected it.
// Declarations cascade by specificity (pseudo > named > bare, then source order),
// then margins/size resolve with CSS Paged Media defaults. Returns the zero UsedPage
// (caller's WithPageSize / Letter default) when no rule matches.
func (s Stylesheet) ResolvePage(i int, name string) UsedPage

// UsedPage is the resolved geometry + chrome for one page.
type UsedPage struct {
    WidthPt, HeightPt              float64 // resolved from `size` (keyword/explicit + orientation)
    MarginTop, MarginRight,
    MarginBottom, MarginLeft       float64 // resolved margins (default 0 here; see size source)
    MarginBoxes [16]*UsedMarginBox         // nil where no content; indexed by MarginBoxSlot
    HasRule     bool                       // false => no @page matched (use API/Letter default)
}
```

`size` parsing (CSS Paged Media §6.2): `auto`; a single length (square); two lengths (w h); a page-size
keyword `A5/A4/A3/B5/B4/JIS-B5/JIS-B4/letter/legal/ledger`; an optional `portrait`/`landscape` that swaps the
keyword's axes. Keyword dimensions are the standard pt values (A4 = 595×842pt at 72dpi — **but** the engine's
px:pt convention is 96dpi 1:1, so we use the **CSS px** sizes: A4 = 794×1123, letter = 816×1056, matching the
existing `LetterWidthPt`/`LetterHeightPt`; a table of the common sizes lives in `page.go`).

`margin` shorthand + longhands reuse the existing edge-shorthand expansion (the same logic
`pkg/css/shorthand` uses for `margin` on a box). Lengths resolve with the standard unit set (`cm`/`mm`/`in`/
`pt`/`px`); percentages on page margins resolve against the page box dimension (rare; supported).

### 1c. New `ComputedStyle` fields

```go
// Page is the CSS `page` property: the name of the @page rule whose geometry/chrome
// the pages generated by this box use (CSS Paged Media §3.1). Inherited; initial "".
// A forced page break is induced between two boxes with different used `page` values
// (so content with `page: landscape` lands on landscape pages). Read only by the
// pagination pass.
Page string

// BreakInside is the CSS break-inside hint ("auto"/"avoid"/"avoid-page"), lowercased.
// "avoid"/"avoid-page" ask the pagination pass to keep the box on one page (push it
// whole to the next page rather than splitting/overflowing). Not inherited; initial
// "auto". Read only by the pagination pass.
BreakInside string

// Widows / Orphans are the CSS widows / orphans counts: the minimum number of line
// boxes a fragmentation break may leave at the TOP (widows) / BOTTOM (orphans) of a
// page when splitting a block's inline content. Inherited; initial 2. Read only by
// the pagination pass's line-level splitter.
Widows  int
Orphans int
```

Cascade arms mirror the existing `BreakBefore`/`BreakAfter` ones (`cascade.go:609`): `page` → `Page`;
`break-inside` → `BreakInside`; `widows`/`orphans` → integer parse (fallback to the inherited/initial 2 on a
non-integer). `Widows`/`Orphans` need an **initial value of 2** even with no declaration — handle in the
`ComputedStyle` initializer (where other non-zero initials live), and make them inherited in the inheritance
pass.

## Component 2 — page geometry from `@page` (`pkg/doctaculous` + `pkg/layout/css`)

Today `LayoutPaged(ctx, root, viewportW, pageH)` takes a width and a height; `pageH<=0` ⇒ single tall page.
This slice threads a resolved page **description** instead of a bare height, because the page now carries
margins and per-page chrome. To keep the byte-identical guard trivial and the diff small:

- Keep `LayoutPaged(ctx, root, viewportW, pageH)` as-is for the `WithPageSize` path (it ignores `@page`).
- Add `LayoutPagedDoc(ctx, root, cfg)` where `cfg` carries the **resolved page model**:
  `{ viewportW float64; pageH float64; pages css.Stylesheet (for ResolvePage); paged bool }`. When
  `paged` is false → single tall page (unchanged). When true, for each page index it calls
  `cfg.pages.ResolvePage(i, name)` to get size + margins + margin boxes.

`pkg/doctaculous/html_backend.go`:

- `htmlConfig` gains `paged bool` and a parsed-`@page` carrier (the aggregated author `Stylesheet`, already
  built for `@font-face` — surface its `Pages`).
- `WithDefaultPaged()` sets `paged = true` with **no explicit size**: pagination uses the document's `@page`
  size if present, else Letter (`LetterWidthPt × LetterHeightPt`).
- `WithPageSize(w, h)` keeps its current behavior (sets viewport `w` + page height `h`) and **also** sets
  `paged = true`; an explicit size **overrides** any `@page size` (API beats stylesheet). When both an
  explicit `WithPageSize` and an `@page { margin }` are present, the explicit *size* wins but the `@page`
  *margins* and *margin boxes* still apply (size and chrome are independent — a caller fixing the sheet size
  but wanting the document's headers is reasonable; documented).
- The build path resolves the `@page` rule once (from the aggregated sheets) and passes it in `htmlConfig`.

**The margin band changes the layout width.** A page with `margin: 1in` lays its content out in
`pageW − marginLeft − marginRight`. So when `@page` (or `WithPageSize` + `@page margin`) yields non-zero
margins, the **viewport width handed to `layoutTree` is the content-box width**, and every page's blocks are
**translated right by `marginLeft` and down by `marginTop`** (in addition to the existing per-page
`-bucket.top` shift) when placed on the page. The page itself is full `pageW × pageH`; content sits in the
margin box. (With zero margins — the current behavior and `WithPageSize` without `@page` — there is no
translate and width is unchanged: byte-identical.)

## Component 3 — `break-inside: avoid` + keep-together (`pkg/layout/css/paginate.go`)

`bucketBlocks` currently breaks between blocks on overflow and at forced breaks. Add keep semantics:

- **`break-inside: avoid` on a top-level block** that would otherwise be cut (it is taller than the remaining
  space on the current page but **not** taller than a whole page): instead of leaving it to overflow, **close
  the current page and start it on a fresh page** (where it fits). If it is taller than a whole page even on a
  fresh page, it still overflows (can't honor avoid — logged, unchanged degradation).
- **`break-before: avoid` / `break-after: avoid` between two adjacent blocks**: a class-B forced break must
  not be inserted between them. In the bounded between-blocks model this means: if block *N* has
  `break-after: avoid` (or block *N+1* has `break-before: avoid`) and an **unforced** (overflow) break would
  land between them, try to **move block *N* to the next page too** (keep the pair together), provided the
  pair fits on a page; if the pair cannot fit, the avoid is dropped (logged) and the overflow break stands.
  This is the bounded, no-relayout interpretation of `break-*: avoid` — it keeps *adjacent top-level blocks*
  together, the dominant real case (a heading with its first paragraph). The general "avoid anywhere in a
  chain" is approximated to pairwise keep.

This composes with widows/orphans (Component 4): `break-inside: avoid` is checked **first** — an avoid block
is never line-split; only an `auto` (or absent) `break-inside` block is eligible for the line-level splitter.

## Component 4 — widows & orphans via mid-block line fragmentation (`pkg/layout/css/fragmentpage.go`, new)

This is the structural addition the pagination slice deferred. The post-pass currently moves whole blocks
between pages; here it learns to **split a single block at a line boundary** when the block establishes an
inline formatting context (it has `Lines`) and does not fit on the current page.

### What can be split

A fragment is **line-splittable** iff:
- it has `len(f.Lines) > 0` (an inline formatting context — a paragraph, a heading, a `<div>` of text), AND
- its `break-inside` is not `avoid`, AND
- it has no `Children` that are themselves block fragments interleaved with the lines (a pure inline-content
  block; the common paragraph). A block mixing block children and lines (anonymous-block fixups) is **not**
  line-split in this slice — it falls back to whole-block placement (it overflows if too tall), logged. This
  keeps the splitter to the dominant, well-defined case and avoids re-deriving block/line interleaving order
  in the post-pass.

(Mid-table-row, mid-flex-item, mid-grid-item: a cell/item's content is inside a nested BFC fragment, not in
the top-level block's `Lines`; those are **not** line-splittable and stay deferred — each overflows its page,
logged, exactly as today.)

### The split algorithm

Given a line-splittable block `b` with lines `L[0..n)` (each `LineFragment` has a `BaselineY`; the block's
content box runs from `b.Y + topEdge` to `b.Y + b.H − bottomEdge`), and a page bottom `pageBottom` (in page
space):

1. Compute each line's **bottom extent** (`BaselineY + descent`); approximate the per-line band as
   `[prevBottom, thisBottom]`. Find `k` = the number of lines that fit entirely above `pageBottom`.
2. **Apply orphans:** if `k < orphans`, the break would leave fewer than `orphans` lines on this page — do
   **not** split here; push the **whole block** to the next page (the orphans rule forbids the split). (If the
   block is the first content on the page and still can't satisfy orphans because it's taller than a page,
   it overflows — logged.)
3. **Apply widows:** the remaining `n − k` lines go to the next page; if `n − k < widows`, pull lines back
   from this page so the next page gets at least `widows` (i.e. reduce `k` to `n − widows`). If that makes
   `k < orphans` (the block is too short to satisfy both, `n < widows + orphans`), the block **cannot be
   split** — push it whole to the next page (CSS: an unsplittable-by-rule block moves entirely).
4. If `1 ≤ k ≤ n−1` after the widows/orphans clamp, **split**: produce two fragments,
   - `head` = a shallow clone of `b` with `Lines = L[0..k)`, height shrunk to end just below line `k−1`'s
     band (so its border box bottom is the split point; its **bottom border/padding is suppressed** — CSS:
     a box split across a fragmentation break does not paint the break-side border/padding by default,
     `box-decoration-break: slice`), staying on the current page;
   - `tail` = a shallow clone of `b` with `Lines = L[k..n)`, its lines' `BaselineY` shifted so the tail's
     content starts at the tail block's top, height = remaining content, its **top border/padding suppressed**,
     flowing to the next page. The tail re-enters the bucketer as the next page's first block (it may itself
     be taller than a page and split again — the splitter is iterative).
5. If `k == 0` (not even one line fits) the block moves whole to the next page (handled by the existing
   overflow path; only reachable if the block is the page's first content and taller than the page → overflow).

The split is a **page-space-only transform**: it partitions `Lines`, clones the `Fragment` struct (shallow,
sharing glyph outlines — they are read-only `*render.Path`), and adjusts `Y`/`H` + suppresses the break-side
edges. No relayout, no inline re-shaping (the glyphs were already placed; we only choose where the page
boundary cuts the line list). This is exactly analogous to how `shiftFragment` already mutates page-space Ys.

### Where it plugs in

`bucketBlocks` becomes height-aware at the line level: when a block would overflow the current page and it is
line-splittable, call `splitBlockForPage(b, pageBottom, widows, orphans)` → `(head, tailOrNil)`. Put `head`
on the current page; if `tail != nil`, make it the lead block of a new page and continue bucketing the
*rest* of the blocks after it. The bucket's `top`/shift math already handles a block landing at a page top.

The default `widows=orphans=2` means a 1-line or 2-line paragraph at a page bottom is pushed whole to the next
page (the common, correct behavior) rather than orphaning a single line — so even the *default* paginated
output improves. A document **without** `WithPageSize`/`@page` never enters the post-pass, so this is inert
for the existing corpus (byte-identical guard holds).

## Component 5 — running headers/footers via `@page` margin boxes (`pkg/layout/css/marginbox.go`, new)

The 16 `@page` margin boxes form a ring around the page content area (4 corners + 3 per edge). This slice
renders the ones with `content:` as **per-page generated content**:

- For page `i`, `ResolvePage(i, name).MarginBoxes` gives the resolved `UsedMarginBox`es (each: resolved
  `content` value, font/color/text-align). The margin-box **rectangles** are computed from the page size and
  the resolved margins (CSS Paged Media §8.3.1 corner/edge geometry: e.g. `@top-center` spans the top margin
  band between the left/right corner boxes, height = `margin-top`).
- The `content` value is resolved to a string per page:
  - `content: "literal"` → the literal;
  - `content: counter(page)` → the 1-based page number; `counter(pages)` → the total page count (known after
    bucketing — resolved in a second pass once `len(pages)` is final);
  - `content: counter(page) " / " counter(pages)` → concatenation (the common `1 / 8` footer);
  - `content: string(name)` → the most recent `string-set: name content()` value seen **on or before** this
    page (the running-header-from-headings pattern). `string-set` is captured on `ComputedStyle` and the
    per-page value is the last-set value among blocks bucketed onto pages `≤ i` (a running value; the
    "first/last/start" variants beyond the default are deferred — documented).
- The resolved string is laid out with the **existing inline layout** (shape → break → place) at the margin
  box width, aligned per `text-align`, and emitted as a `Fragment` whose items are appended to that page's
  `layout.Page.Items` (after the content items, so headers paint over the margin band). No new paint
  primitive — margin-box text is glyphs + optional background/border, all existing `Item` kinds.

The page **counter** is special only in that `counter(pages)` needs the final page count; the bucketing
already produces all pages before margin boxes are laid out, so a two-phase build (bucket → then per-page
margin boxes with `len(pages)` known) resolves it. `counter(page)` is just `i+1`.

**Deferred within margin boxes** (each degrades gracefully — empty/omitted, logged where useful): the full
CSS counter *styles* on `counter(page, <style>)` beyond decimal (reuse the list-counter formatter where it's
trivial — roman/alpha — and fall back to decimal otherwise); `string-set` variants beyond the running default;
`element()` / running elements (moving a live element into the margin — a much larger feature); margin-box
`width: auto` distribution subtleties (use the simple "edge box fills its band, corner boxes are square at
the margin size" geometry). These are honest approximations of rarely-authored corners of the spec.

## Deferrals (each degrades gracefully — tested)

| Deferred | Behavior this slice | Why |
|---|---|---|
| **Mid-table-row / mid-flex-item / mid-grid-item splits** | The whole row/item rides one page; a too-tall one overflows (clipped by the bitmap), logged. | Needs formatting-context-internal fragmentation (content is in a nested BFC fragment, not the block's `Lines`); out of the post-pass model. |
| **A block mixing block children AND inline lines** (anonymous-block-fixup case) | Not line-split; placed whole (overflows if too tall), logged. | The post-pass does not re-derive block/line interleave order; the splitter is bounded to pure inline-content blocks (the dominant paragraph case). |
| **`break-inside: avoid` taller than a page** | Overflows the page (cannot honor avoid), logged. | A box that does not fit a page cannot be kept whole. |
| **`@page` named-page parity edge cases** (`:left`/`:right`/`:blank` interactions, `@page name:first`) | Resolved by the documented parity rule (page 1 = `:right`); `:blank` matches an empty forced-break page. | Faithful to the common cases; exotic combinations resolve deterministically. |
| **Margin-box `element()` running elements; `string-set` first/last/start; counter styles beyond decimal/roman/alpha** | Omitted / decimal fallback, logged. | Rarely authored; large surface for little real-world gain. |
| **`@page` `marks`/`bleed`/`page-orientation`** | Ignored (no crop marks / bleed in this raster model). | Print-production only; no device need. |

## Testing (CLAUDE.md "Testing" — new fixtures + tests in this PR)

Match the proof to the change. Each component gets focused tests; the byte-identical guard is paramount.

**Unit — `pkg/css`:**
1. `@page` capture: `@page { size: A4 landscape; margin: 2cm }` → one `PageRule`, size resolves to 1123×794,
   margins to 2cm in pt. `@page :first { margin: 0 }` and `@page name { size: letter }` capture with the
   right selector. A non-`@page` at-rule is still skipped (regression).
2. `ResolvePage`: base + `:first` cascade (page 0 gets `:first` margins, page 1 the base); named page; size
   keyword + orientation table; margin shorthand expansion; the zero `UsedPage` when no rule matches.
3. Margin-box capture: `@page { @top-center { content: counter(page) } }` → a `MarginBoxRule` at the
   `@top-center` slot with the `content` declaration.
4. `ComputedStyle` cascade: `page`, `break-inside`, `widows`, `orphans` resolve (incl. the inherited initial
   2 for widows/orphans with no declaration; integer-parse fallback).

**Unit — pagination pass (`pkg/layout/css/paginate_test.go`, `fragmentpage_test.go`):**
5. **`break-inside: avoid`** — a block that would be cut is pushed whole to the next page (assert it lands on
   page 1 at local Y 0, intact); a too-tall avoid block overflows page 0 (logged, 1 page).
6. **`break-*: avoid` pair-keep** — heading + paragraph where an overflow would split them; both move to the
   next page together when they fit.
7. **Orphans** — a block whose split would leave 1 line at the bottom (with `orphans: 2`) is pushed whole to
   the next page (no 1-line orphan). With `orphans: 1` it splits.
8. **Widows** — a block whose split would carry 1 line to the next page (with `widows: 2`) pulls a second
   line over (assert the tail has ≥ 2 lines, the head correspondingly fewer).
9. **Successful split** — a 10-line paragraph across a boundary with `widows:2 orphans:2` splits into head (k
   lines) + tail (10−k lines), k≥2 and 10−k≥2; assert the tail re-buckets to page 1, its lines' Y shifted to
   local 0, and the head's bottom border is suppressed / tail's top border suppressed.
10. **Unsplittable-by-rule short block** — a 3-line block with `widows:2 orphans:2` (3 < 2+2) at a boundary
    moves whole to the next page (cannot satisfy both).
11. **Iterative split** — a paragraph taller than a page splits across **three** pages (head, middle, tail).

**Unit — page geometry / margin band:**
12. **`@page` drives size without `WithPageSize`** — `WithDefaultPaged()` + `@page { size: A4; margin: 1in }`
    ⇒ pages are 794×1123, content laid out in the 794−2in width and translated right/down by 1in.
13. **`WithPageSize` overrides `@page size` but keeps margins** — explicit `WithPageSize` + `@page { margin }`
    ⇒ explicit size, `@page` margins applied.

**Unit — margin boxes (`marginbox_test.go`):**
14. **Page counters** — `@bottom-center { content: counter(page) " / " counter(pages) }` over a 3-page doc ⇒
    each page's margin-box string is `1 / 3`, `2 / 3`, `3 / 3` (assert the laid-out glyphs / a text readback).
15. **Literal + `string()` running header** — `@top-left { content: string(title) }` with `h1 { string-set:
    title content() }` ⇒ each page's header is the most recent heading text.

**End-to-end (`pkg/doctaculous`):**
16. **`WithDefaultPaged()` end-to-end** — a multi-section doc with `@page { size: letter; margin: 1in; @top-
    right { content: counter(page) } }` paginates, each page rasterizes non-blank, the page number appears in
    the top-right band.
17. **Byte-identical default guard** — `OpenHTMLBytes(doc)` (no `WithPageSize`, no `@page`) renders
    identically to before (single page, height == content height). And a doc **with** an `@page` rule but
    opened **without** `WithDefaultPaged`/`WithPageSize` is **also** single-tall (the `@page` rule is inert
    until pagination is requested) — assert 1 page. This keeps the existing corpus untouched.

**Golden images (the doctaculous reflow goldens, `html-*` convention):**
18. `html-page-margins-p{0,1}` — `@page { size; margin; @bottom-center { content: counter(page) } }` over a
    2-page doc: eyeball the margin band, the centered page number, content inset by the margins.
19. `html-widows-orphans-p{0,1}` — a multi-paragraph doc with a small page height and `widows:3 orphans:3`:
    eyeball that no page bottom/top has fewer than 3 lines of a split paragraph.
20. **Showcase:** add a "PAGED MEDIA" section to `testdata/htmldoc/` exercising `@page` size+margins, a
    running footer page counter, `break-inside: avoid` on a figure, and a multi-paragraph block that
    widows/orphans-splits — rendered via `OpenURL` + `WithDefaultPaged()`; regenerate + eyeball the
    `htmldoc-p*` goldens (page count grows; eyeball each).

**Byte-identical guard (critical):** the **entire existing golden/reftest corpus is unchanged** — no existing
test uses `@page` or `WithDefaultPaged`, and `WithPageSize` already existed. New `ComputedStyle` fields
default to their initial values, read only by the post-pass. CI proves it: existing goldens stay
byte-identical; only **new** `html-page-*` / `html-widows-*` goldens (and the grown showcase) are added.

**Hermetic / fast:** inline HTML strings + the loopback-served showcase (existing harness); no network, no new
committed binaries beyond the new goldens. Race-clean (`go test -race ./pkg/css/... ./pkg/layout/css/...
./pkg/doctaculous/...`).

## What this slice deliberately does NOT change

- **The fragment-tree builder** (`layoutTree` and everything it calls) — paged media stays a *post-pass*; the
  one new mutation is partitioning a leaf block's `Lines` (page-space only, no relayout, no re-shaping).
- **`Fragment.AppendItems` / the flatten** — reused verbatim, once per page (margin boxes append to a page's
  item list after the content flatten).
- **`render.Device`, `pkg/render/raster`, `pkg/layout/paint`** — a `layout.Page` is a `layout.Page`; margin
  boxes and split blocks emit only existing `Item` kinds.
- **The PDF and DOCX pipelines** — untouched (DOCX has its own pagination).
- **The shared inline core** (`pkg/layout/inline`) — untouched (the line splitter partitions *already-placed*
  lines; margin-box text reuses the core read-only, adding nothing to it).
- **Dependencies** — none added.

## Open questions resolved (from the scope decision)

- **Scope:** full paged media — `@page` size+margins, `break-inside`, **widows/orphans (mid-block line
  splitting)**, and **running headers/footers** (margin boxes + page counters). The user chose the maximal
  option, consistent with the project's "most browser-faithful" directive.
- **Page-size source precedence:** explicit `WithPageSize` size > `@page size` > Letter default; `@page`
  margins/margin-boxes apply regardless of which set the size.
- **Opt-in:** `WithDefaultPaged()` (paginate using `@page`/Letter) joins the existing explicit
  `WithPageSize`. With neither, output is a single tall page (byte-identical), even if the document has an
  `@page` rule.
- **Widows/orphans default:** 2 (CSS initial), inherited — so even default pagination avoids single-line
  orphans/widows. Inert without pagination.
- **Fragmentation bound:** line-level splitting is limited to **pure-inline-content blocks** (paragraphs);
  table-row / flex-item / grid-item / mixed-block-and-inline splitting stays deferred (overflow + log).
