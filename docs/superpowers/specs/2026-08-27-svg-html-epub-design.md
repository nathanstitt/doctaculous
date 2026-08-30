# SVG in HTML and EPUB (PR 8 of 8) — Design

**Date:** 2026-08-27
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-html`, stacked on `feat/svg-filters` (PR #109)

## Goal

Make SVG work where people actually put it: `<img src="…svg">`, inline
`<svg>` markup, and EPUB covers — **without ever rasterizing**.

## The central fact the survey established

**The vector seam already exists and is proven in production.**
`layout.VectorItem` + `layout.VectorScene` (`pkg/layout/page.go:251-263`) are
painted by `paint.paintVector` (`pkg/layout/paint/paint.go:275-283`), which
calls `Scene.DrawVector(dev, ctm)` straight onto the same `render.Device` the
HTML and PDF pipelines use. On `pdfwrite` that emits **real vector PDF
operators** — no bitmap anywhere. `pkg/svg/draw.Renderer` already implements
`VectorScene`, and the standalone SVG frontend already uses this path.

The gap is entirely on the **producer** side: nothing in `pkg/layout/css` ever
constructs a `VectorItem`. `Fragment` carries only `*ImageContent`, whose
`image.Image` is the wrong carrier for SVG.

**So the one decision that matters is: route SVG through the existing
`VectorItem` seam, never through `imageCache`/`decodeImageBytes`/`ImageContent`.**
Routing it through the raster path is the only thing that would force a bitmap
round-trip and undo the whole series.

## Decisions taken (autonomous, with rationale)

### 1. `svg.Document` needs an un-defaulted intrinsic-size accessor

`svg.Document` exposes only resolved `WidthPt`/`HeightPt` — `resolveSize`
(`pkg/svg/svg.go:293-325`) has already applied CSS's 300×150 default. That is
right for a standalone SVG (it *is* the sizing authority) and wrong for an
embedded one, where the outer `<img>`'s CSS supplies an axis and the SVG should
contribute only a ratio.

`replacedUsedSize` (`pkg/layout/css/replaced.go:32`) is already structured for
the three-way split (`hasW`/`hasH`/`haveIntrinsic`), but `intrinsicSize`
returns `(iw, ih, ok)` — it cannot express "a 2:1 ratio, no absolute size".

Add an accessor on `svg.Document` reporting the pre-default state, and teach
`intrinsicSize` to use it. Without this, `<img src="x.svg" width="600">` on a
viewBox-only SVG sizes to 300×150 and ignores the CSS width's implied height.

### 2. Inline `<svg>` re-serializes rather than bridging DOM to DOM

`x/net/html` fully implements HTML5 foreign content: the subtree is preserved
with `Namespace: "svg"` and camelCase tag/attribute names repaired
(`svgTagNameAdjustments`). But `pkg/html`'s `buildElement` ignores `Namespace`
entirely, so `<circle>`/`<path>` currently generate meaningless HTML block
boxes.

`svg.Parse` takes raw XML bytes, not a parsed DOM. Two options: bridge
`x/net/html.Node` → `pkg/svg`'s AST directly, or re-serialize the subtree and
hand it to `svg.Parse`.

**Chosen: re-serialize.** The bridge duplicates `pkg/svg`'s entire
element/attribute construction against a second node type, and every future
`pkg/svg` parser fix would have to land twice. Re-serializing is one function
against an already-namespace-repaired tree, and the SVG parser stays the single
source of truth. The cost is a serialize/reparse round trip per inline `<svg>`,
which is negligible against parsing the document at all.

`<svg>` joins `replacedTags` so box generation stops recursing into it.

### 3. `filter:` for HTML is deferred — with the reason recorded

`pkg/filtereffects` was built dependency-free in PR 7 explicitly for reuse
here, and `BeginGroup`/`EndGroup`/`RenderOffscreen` are all on the shared
`render.Device`. So the *pixel* work is done.

What is not done is the plumbing: `layout.Page.Items` is a **flat** list, and a
filtered box must bracket its subtree the way `Clips` does with
`ClipPushKind`/`ClipPopKind`. That means new item kinds, changes to
`AppendItems`/`appendSelfContent`, and a paint-stage grouping pass — a
structural change to the reflow item format that has nothing to do with SVG.

Bundling it here would mix a layout-format change into an SVG PR and risk the
reflow engine. It is a clean follow-up: the parser is already shared and
waiting. **Deferred and recorded**, not forgotten.

### 4. `background-image: url(…svg)` is in scope; SVG background tiling is not

`resolveBackgroundImage` (`pkg/layout/css/background.go:20`) uses the same
`imageCache`, so SVG needs its own carrier here too. The common real-world case
— a single non-repeating SVG background sized by `cover`/`contain`/explicit
length — is in scope.

`background-repeat` tiling of a vector image interacts with the SVG's own
viewBox scaling in a corner most engines special-case. Tiling degrades to
painting the image once, with a log.

### 5. EPUB needs almost nothing, and gets a cover path

`epubLoader` already maps `.svg` → `image/svg+xml`
(`pkg/omnidoc/epub_frontend.go:105`), and `extractChapter`'s regex scan
passes inline `<svg>` through verbatim into the HTML path. So EPUB inherits
decisions 2 and 4 for free.

The one real gap: the OPF manifest's cover-image item (EPUB3
`properties="cover-image"`, EPUB2 `<meta name="cover">`) is **parsed and
discarded** (`pkg/epub/epub.go:111`), so a cover only renders if some chapter
happens to `<img>` it. Surfacing it is small and is what makes SVG covers —
a common real-world case — actually work.

## The shared-CSS selector gap

`pkg/css/selector.go` has no `>`, `+`, `~`, or attribute selectors, and
`parseOneSelector` splits on whitespace so `svg > path` parses into three parts
with a bogus tag `">"` that never matches. It **fails safe but silently**.

This is pre-existing and shared, but **this PR is where it starts biting HTML
authors**: design-tool SVG exports lean heavily on `[class^="cls-"]` and
`.icon > path`, and an inline `<svg>` with its own `<style>` will silently lose
those rules.

Fixing the selector engine is its own project (CLAUDE.md roadmap item 8). What
this PR adds is the cheap half already identified there: **a warn-once at
selector-parse time** when a rule is dropped for an unsupported combinator, so
the failure stops being silent. That is a few lines and closes the worst part
of the gap.

## Out of scope

- `filter:` for HTML (decision 3) — deferred with the reason recorded.
- SVG `background-repeat` tiling (decision 4) — degrades with a log.
- The selector engine itself — only the warn-once lands here.
- `letter-spacing`/`word-spacing` inheriting across the HTML→SVG boundary:
  `ComputedStyle` has no such fields (PR 6 implemented them SVG-only). An
  SVG-internal declaration works; inheritance from an HTML ancestor does not.
  Recorded.
- Interactive/animated SVG (`<script>`, SMIL) — out of scope for the series.

## Testing

- **The vector claim, which is the whole point:** an `<img src="…svg">` in a
  document rendered to PDF emits vector operators and **no image XObject**.
  Assert on the emitted PDF, not on pixels — a golden alone would pass with a
  rasterized round trip.
- Intrinsic sizing: explicit width/height; viewBox-only (ratio) with one CSS
  axis given; neither (300×150); and `width` alone deriving height from the
  ratio.
- Inline `<svg>` renders, and its camelCase children (`clipPath`,
  `linearGradient`) survive the HTML parser's adjustments — the re-serialize
  round trip must preserve them.
- Inline `<svg>` no longer generates HTML boxes for `<circle>`/`<path>`.
- EPUB with an SVG cover, and an EPUB chapter with inline SVG.
- `background-image` with an SVG; tiling degrades with a log, covered by a test.
- The unsupported-selector warn-once fires for `>`/`+`/`~`/`[attr]` and does
  **not** fire for supported selectors.
- No pre-existing golden may move; the HTML/DOCX/PDF suites must stay green,
  since this touches the shared reflow path.
