# SVG Support — Design

**Date:** 2026-08-25
**Status:** Approved design, pending implementation plan
**Decisions locked:** input-only · vector-native everywhere · built in-house · resvg-subset test corpus

## Goal

Render all real-world SVG with full fidelity, in every input context the engine has:

1. **Standalone `.svg`** as a top-level input document (→ PNG pages, → PDF, → any output).
2. **Inline `<svg>`** in HTML and EPUB markup.
3. **`<img src="*.svg">`** in HTML/EPUB/Markdown (fixes today's broken EPUB covers — the
   EPUB-recommended cover pattern is an SVG wrapping a raster `<image>`).
4. **CSS `background-image: url(*.svg)`**.

SVG is **input only**. An SVG-writer `render.Device` backend is a separate future project
(the Device seam makes it separable).

Esoteric features are dropped with graceful degradation; everything commonly found in
real-world SVGs is rendered faithfully.

## Why in-house (library survey, Aug 2026)

No existing pure-Go library is adoptable:

| Library | Blocker |
|---|---|
| `benoitkugler/webrender/svg` | Best coverage (text, gradients, patterns, clip, mask, use, markers), BSD-3, backend-agnostic canvas — but hard-depends on LGPL-2.1 `textprocessing`, violating the MIT rule |
| `tdewolff/canvas` | SVG input misses `<g>`, `<use>`, `<clipPath>`, `<mask>`, `<tspan>`, filters; ~90 transitive deps; LGPL bidi port |
| `cogentcore.org/core/svg` | Drags in the entire Cogent GUI framework module; filters parsed-not-rendered; no mask |
| `srwiley/oksvg` | No text/clip/mask/CSS; couples parse to rasterization; dormant since 2023 |
| `JoshVarga/svgparser`, `lafriks/go-svg`, `gogpu/gg` | Parse-only / experimental / unvetted |

**webrender/svg (BSD-3) is the study reference** for gnarly semantics (objectBoundingBox
units, `use` cycles, marker orientation, pattern recursion). Its own code may be freely
studied; its LGPL dep is why we cannot import it.

The repo already owns every hard piece: CSS cascade (`pkg/css`), harfbuzz shaping
(`pkg/layout/inline`), rasterx stroking (1:1 with SVG's stroke model: caps, joins,
miter limit, dashes), axial/radial gradient math (`pkg/render/raster/shading.go`),
16 blend modes, path clipping, and `beevik/etree` for XML. **No new dependency is needed.**
What's missing is exactly the layer no library provides reusably: XML → styled scene
graph → `render.Device` paint ops.

## Feature scope

### In scope — full fidelity

- **Structure**: `svg` (incl. nested), `g`, `defs`, `use`, `symbol`, `switch` (basic
  conditional processing), `title`/`desc` (parsed, not rendered).
- **Shapes & paths**: `rect` (rx/ry), `circle`, `ellipse`, `line`, `polyline`, `polygon`,
  `path` with the complete grammar (`M L H V C S Q T A Z`, absolute + relative, implicit
  repeats, pathological whitespace). Quadratics elevate exactly to cubics; elliptical arcs
  decompose to cubics (`render.Path` keeps its four segment kinds).
- **Coordinates**: `viewBox`, full `preserveAspectRatio` (all alignments, `meet`/`slice`/
  `none`), transform lists (`translate/scale/rotate/skewX/skewY/matrix`), length units
  (px/pt/pc/mm/cm/in/em/ex/%).
- **Painting**: `fill`/`stroke` with all color syntaxes + `currentColor` + `none`;
  `fill-rule`, `fill-opacity`; complete stroke set (`stroke-width`, `-linecap`,
  `-linejoin`, `-miterlimit`, `-dasharray`, `-dashoffset`, `-opacity`); group `opacity`;
  `visibility`/`display`; `paint-order`; `vector-effect: non-scaling-stroke`.
- **Paint servers**: `linearGradient`, `radialGradient` (incl. focal fx/fy), both
  `gradientUnits` systems, `gradientTransform`, `spreadMethod` pad/reflect/repeat,
  `href`/`xlink:href` attribute inheritance, `<stop>` with `offset`/`stop-color`/
  `stop-opacity`; `<pattern>` (both unit systems, `patternTransform`, pattern `viewBox`)
  via offscreen tile.
- **Clip & mask**: `clip-path`/`clipPath` (userSpaceOnUse + objectBoundingBox,
  `clip-rule`, transforms on clip shapes); `<mask>` (luminance and alpha).
- **Markers**: `marker-start/mid/end`, `orient` (angle, `auto`, `auto-start-reverse`),
  `markerUnits`, marker `viewBox`/`refX`/`refY`.
- **Text**: `text`, `tspan` (x/y/dx/dy/rotate arrays), `text-anchor`, full `font-*`
  resolution through the layout `FaceCache` (family lists, weights, styles, script
  fallback), `letter-spacing`/`word-spacing`, common `dominant-baseline`/
  `alignment-baseline` keywords. Shaped through `pkg/layout/inline` (harfbuzz + bidi),
  so RTL/Arabic SVG text works; emitted as `DrawGlyph`, so the PDF backend gets real
  text, not outlines.
- **Images**: `<image>` referencing raster formats or nested SVG via `ResourceLoader`
  (and `data:` URIs), with `preserveAspectRatio`.
- **Styling**: presentation attributes at their spec cascade slot (above UA, below
  author — the `pkg/css/hints.go` mechanism); `style` attribute; `<style>` element
  sheets with real selector matching + specificity via `pkg/css`; `class`; inheritance;
  `currentColor` propagation from HTML context for inline SVG.
- **Filters — real-world subset**: `feGaussianBlur`, `feDropShadow`, `feOffset`,
  `feFlood`, `feMerge`/`feMergeNode`, `feBlend`, `feColorMatrix`, `feComposite`,
  `feComponentTransfer`; filter regions (`x/y/width/height`, both unit systems),
  `in`/`in2`/`result` plumbing with `SourceGraphic`/`SourceAlpha`. Covers drop shadows,
  glows, and recolors — ~95% of filters in the wild. (No pure-Go library implements
  `feGaussianBlur` today; this is new ground.)

### Degrade gracefully (skip + debug log; each covered by a test)

- Remaining filter primitives: `feTurbulence`, `feDisplacementMap`, `feConvolveMatrix`,
  `feMorphology`, `feImage`, `feTile`, `feDiffuseLighting`, `feSpecularLighting`.
- `foreignObject` — explicit follow-up: the engine owns an HTML layout engine, so
  rendering HTML-in-SVG is feasible later; initially skipped.
- `textPath` (curved text) — follow-up slice candidate; initially skipped.
- SVG fonts, `altGlyph`.
- `color-interpolation-filters: linearRGB` — filters compute in sRGB (documented
  approximation).

### Out of scope

- SMIL/CSS animation: the static initial frame renders; `<animate>`/`<set>`/
  `<animateTransform>` elements are ignored.
- Scripting (`<script>`), interactivity (`:hover`, cursor, focus).

## Architecture

A third reflow-side frontend, meeting the pipeline at `render.Device` like PDF and
DOCX/HTML do:

```
pkg/svg        XML → namespace-aware DOM → styled scene graph
               - parse via etree (case-sensitive attrs, namespaces preserved)
               - style resolution reusing pkg/css value parsers, parseDeclarations,
                 and selector matching; SVG-specific property applier (kept out of
                 css.ComputedStyle so HTML boxes don't carry SVG fields)
               - geometry: path grammar, arc→cubic, quad→cubic, transform lists,
                 viewBox/preserveAspectRatio matrices
               - scene graph is read-only after parse (shared lock-free across the
                 page-render fan-out, per house concurrency rules)
pkg/svg/draw   scene walker → render.Device ops
               - gradients as render.Shader implementations (the axial/radial math
                 in pkg/render/raster/shading.go gets a PDF-free constructor;
                 the Shader interface is already PDF-free)
               - filters rasterize their region offscreen and composite via DrawImage
```

### Extensions to existing layers

1. **`render.Matrix`**: add `Rotate` and `Skew` constructors (struct already supports
   arbitrary affine).
2. **`render.Device` group API** — the one real gap. Group opacity, masks, and blend
   on groups need offscreen composition:
   - `BeginGroup()` / `EndGroup(alpha float64, blendMode string, mask Mask)` (mask nil
     for plain opacity groups; exact signature refined in the implementation plan).
   - Raster backend: render the group to a scratch RGBA, composite with alpha/blend/mask.
   - PDF-writer backend: real PDF transparency groups + ExtGState/SMask — vectors
     survive into the PDF.
   - **Filters are the exception**: a filtered subtree always rasterizes at output DPI
     (browsers effectively do the same for print), then composites via `DrawImage` —
     graceful in both backends.
3. **`pkg/layout`**: new `VectorKind` item carrying a parsed scene + viewport mapping;
   `pkg/layout/paint` walks it into Device ops with the page→device transform composed.
   This is what keeps inline and `<img>` SVGs vector all the way into PDF output.

### Integration points

- **Standalone `.svg`**: `FormatSVG` in `format.go` (const, `formatCaps` input-only,
  `ParseFormat`, `mimeFormats` `image/svg+xml`, `MIME()`, `FormatFromPath` `.svg`/`.svgz`),
  `detectMagic` sniffing `<svg`/`<?xml`+`<svg` **before** the HTML sniff (fixing the
  existing bug where `<?xml` bytes detect as HTML), `openDetected` case. Page geometry
  from `width`/`height` (absolute units) else `viewBox` ratio, following the
  `OpenImageBytes` frontend pattern.
- **Inline `<svg>` in HTML/EPUB**: `pkg/html` preserves `n.Namespace` (fixing the live
  bug where SVG children flatten into the page and `<text>` content leaks as body text);
  box generation treats `svg` as a replaced element whose `ReplacedContent` carries the
  subtree — source facts only, honoring the box-tree "no decoded media" invariant.
  Layout parses via a memoized cache (the `imageCache` pattern), sizes with CSS
  replaced-element rules (attr dimensions → viewBox aspect ratio → 300×150 fallback),
  and emits `VectorKind`.
- **`<img src="*.svg">`**: `imageCache`/`decodeImageBytes` learns `image/svg+xml`;
  `decodedImage` gains an optional scene (intrinsic size from the SVG sizing rules);
  `ImageItem` carries the scene so paint stays vector. Fixes EPUB covers.
- **CSS `background-image`**: rasterized at paint resolution (device DPI, not intrinsic
  size — stays crisp; the repeat/tile model is raster-shaped).

### Security (standalone SVG is untrusted input)

- `use`/paint-server reference cycles detected and cut (with log).
- Recursion depth and expansion bounded (billion-laughs via nested `use`).
- External resource fetches only through the caller's `ResourceLoader` (same policy as
  HTML); no fetch in `data:`-only contexts.

## Error handling & degradation

- Never panic on malformed input: a bad attribute takes its initial value + debug log;
  a bad element skips its subtree + log; a path that fails mid-grammar renders the
  segments parsed so far (browser behavior).
- Unsupported features skip with a one-line debug log; each has a test asserting the
  document still renders.
- Missing fonts fall through the existing family-resolution/script-fallback chain.

## Testing

- **resvg corpus**: vendor a curated subset (~300–400 files) of the MIT-licensed
  resvg-test-suite under `testdata/svg/resvg/`, organized by feature area, provenance +
  license noted in-tree. Table-driven golden test renders each at 72 DPI against **our
  own committed goldens** (±4/channel, 0.2% differing-pixel budget — same as existing
  goldens). Each PR lands its feature tranche.
- **Unit suites**: path grammar (incl. pathological cases), arc→cubic + quad→cubic math,
  transform-list parsing, preserveAspectRatio matrices, cascade ordering (presentation
  attr < sheet < style attr), `use` cycle detection, gradient href inheritance.
- **Pipeline-seam fixtures (hand-authored)**: inline `<svg>` in HTML (sizing, flow,
  the fixed text-leak), `<img src=*.svg>`, SVG background, EPUB with SVG cover,
  standalone `.svg` → PNG and → PDF.
- **Showcase**: new SVG section appended to `testdata/htmldoc/index.html` (bump
  `htmlDocPages`, regenerate + eyeball goldens).
- **PDF-writer assertions**: SVG-bearing document → PDF content stream contains path/
  text operators (vectors survived); filtered group → image XObject (rasterized by
  design).
- Race detector (`go test -race`); scene graph read-only after parse.

## Delivery: 8 PRs, each branch → PR off main

1. **`pkg/svg` core** — DOM + parser, scene graph, path grammar with arc/quad→cubic,
   transforms, viewBox/preserveAspectRatio, shapes, solid fill/stroke; `pkg/svg/draw`;
   standalone `FormatSVG` frontend incl. the `<?xml` sniff fix; resvg scaffold + first
   tranche; `Matrix.Rotate`/`Skew`.
2. **Styling** — presentation attributes, `style` attr, `<style>` sheets with selectors/
   specificity via `pkg/css`, inheritance, `currentColor`.
3. **Paint servers** — gradients (PDF-free shader constructors) + patterns; spreadMethod,
   unit systems, href chains.
4. **Groups, clip, mask** — Device group API in both backends (raster offscreen
   composite; pdfwrite transparency groups), group opacity, clipPath, mask.
5. **`use`/`symbol`/markers** — with cycle detection.
6. **Text** — shaping via `pkg/layout/inline`, tspan positioning arrays, text-anchor,
   fonts through `FaceCache`.
7. **Filters subset** — offscreen filter-region rasterization, the nine primitives,
   feDropShadow shorthand.
8. **HTML/EPUB integration** — namespace preservation in `pkg/html` (fixes the text
   leak), replaced-element boxing, `VectorKind`, `<img>`/background/`<image>` paths,
   EPUB cover fix, showcase section.

Every PR: tests + resvg tranche + FEATURES.md entry; degradation logs turn into real
output tranche by tranche. Untouched callers stay byte-identical (PDF/DOCX paths,
pages without SVG).
