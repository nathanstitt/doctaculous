# CSS `filter:` for HTML/DOCX — Design

**Date:** 2026-08-27
**Status:** Approved design (autonomous), pending implementation
**Base branch:** `feat/css-filter`, off `main`
**Deferred from:** [2026-08-27-svg-html-epub-design.md](2026-08-27-svg-html-epub-design.md) decision 3

## Goal

Support the CSS `filter:` property on HTML content — `blur()`, `drop-shadow()`,
`grayscale()`, `brightness()`, `contrast()`, `saturate()`, `hue-rotate()`,
`invert()`, `sepia()`, `opacity()` — reusing the filter machinery the SVG series
already built.

## What already exists

This is deliberately the *last* piece, because the SVG series built everything
underneath it:

- **`pkg/filtereffects`** — the CSS filter shorthand parser, written
  dependency-free in PR 7 explicitly for this reuse (`go list -deps` shows zero
  internal dependencies). `Parse(value string, resolve LengthResolver) ([]Function, bool)`.
- **The pixel math** — every function maps to a primitive chain that already
  ships and is corpus-tested (`pkg/svg/filter`).
- **The offscreen primitives** — `BeginGroup`/`EndGroup` (PR 4) and
  `RenderOffscreen` (PR 7) are on the shared `render.Device`, implemented by both
  backends, and are *not* SVG-specific.

Nothing about the pixels is new. **All the risk is in the plumbing.**

## The actual problem: brackets across a flat, paginated item list

`layout.Page.Items` is a **flat** `[]Item`. A filtered box has to bracket its
subtree, and the codebase already has a bracketing pattern —
`ClipPushKind`/`ClipPopKind` (`pkg/layout/page.go:124-133`), painted as
`dev.Save()` + `clipRect` / `dev.Restore()` (`pkg/layout/paint/paint.go:44-51`).

**But that pattern has never been exercised across a page break.** Its only
emission site is `pkg/layout/css/control.go:384`, bracketing a *form control's
text* — a leaf that cannot span pages. Grepping `paginate.go` and `pagemodel.go`
for either kind returns nothing: pagination has no concept of brackets at all.

A filtered `<div>` can absolutely straddle a page break. Split naively, the
`Push` lands on page 1 and the `Pop` on page 2, giving one page an unbalanced
`Save` and the next an unbalanced `Restore`. The painter tolerates a stray
`Restore` (it mirrors `Restore`'s forgiving contract) but the *rendering* is
wrong either way, and a filter is worse than a clip here: an unclosed filter
group means an entire page's remaining content gets filtered.

**This is the design's core question, and it must be settled before any pixels.**

### Chosen: brackets are page-local, re-emitted per fragment

When pagination splits a filtered box, each page's slice gets its own balanced
`FilterPush`/`FilterPop` pair around the portion that lands on that page.

- **Correct** for every filter whose output at a pixel depends only on that
  pixel (`grayscale`, `invert`, `sepia`, `saturate`, `hue-rotate`, `brightness`,
  `contrast`, `opacity`) — the large majority of real usage.
- **An approximation** for the spatial ones (`blur`, `drop-shadow`): a blur
  applied per page-slice cannot sample content that fell on the other page, so
  the seam at a page break differs from an unbroken render.

That approximation is accepted deliberately and must be **logged once** when a
spatial filter is split across a page. The alternative — making pagination
filter-aware so a filtered box is kept whole or its offscreen surface spans
pages — is a far larger change to the pagination model, and CSS itself has no
defined answer for a filtered element fragmented across pages.

Rejected: emitting brackets only on the first page (silently drops filtering
from continuation pages); keeping the box whole via an implicit
`break-inside: avoid` (silently changes layout, and can't work for a box taller
than a page).

## Architecture

**Cascade.** `Filter string` on `ComputedStyle` (the raw declaration), parsed via
`pkg/filtereffects` at use time rather than at cascade time, matching how
`BackgroundImage` keeps its `url()` ref raw. Not inherited, per spec. `none` is
the initial value. Note `ComputedStyle` currently carries an explicit comment
recording `filter` as deliberately absent — that comment gets replaced, not
deleted.

**Item kinds.** `FilterPushKind` (carrying the parsed function list and the
box's device-space rect) and `FilterPopKind`, mirroring the clip pair.

**Paint.** `FilterPushKind` → `dev.BeginGroup()`; `FilterPopKind` → run the
function chain over the group's pixels and composite. The chain runs through the
same `pkg/svg/filter` primitives, so blur premultiplication, the linearRGB
default, and colour-space handling are inherited rather than reimplemented.

**pdfwrite degrades, and that is correct.** Its `RenderOffscreen` returns nil, so
filtered HTML in PDF output paints **unfiltered** — content visible and correctly
placed, minus the effect. This keeps PDF vector-native rather than rasterizing a
page region, and it is the same honest degradation the SVG side already
documents. It must be logged and stated in FEATURES.md, not implied away.

## Out of scope

- `backdrop-filter` — needs the backdrop, not the element's own pixels; a
  different mechanism entirely.
- Filter regions/`filter-margin`. CSS filters use the border box; SVG's
  `filterUnits`/`x`/`y`/`width`/`height` do not apply here.
- Native PDF filter emulation via soft masks.
- Animating or interpolating filter values.

## Testing

- **Each function's pixel math** already has unit tests in `pkg/svg/filter`;
  what is new is the *mapping* from CSS function to primitive chain. Test that
  mapping directly (e.g. `grayscale(1)` produces the spec's exact colour matrix),
  not just that output changed.
- **The page-break case, which is the whole risk.** A filtered box split across
  a page must produce **balanced brackets on both pages** — assert on the emitted
  item list, since an unbalanced `Save`/`Restore` is invisible in a golden until
  it corrupts a later page. Include a box taller than one page.
- A spatial filter split across a page logs its approximation; a per-pixel one
  does **not** log (a warning on every `grayscale()` would be noise).
- Nesting: a filtered box inside another filtered box.
- `filter: none` and an unparseable value both leave rendering byte-identical —
  the regression guard for every unfiltered document.
- PDF output paints unfiltered with a log; assert on emitted PDF operators (no
  image XObject), the same way PR 8 proved the vector claim.
- No pre-existing golden may move. This touches the shared reflow path, so
  HTML/DOCX/PDF suites must stay green.
