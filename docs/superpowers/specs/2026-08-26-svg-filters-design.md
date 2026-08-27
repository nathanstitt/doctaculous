# SVG filters (PR 7 of 8) — Design

**Date:** 2026-08-26
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-filters`, stacked on `feat/svg-text` (PR 6)

## Goal

Ship the SVG filter primitives that appear in **real-world documents**, with an
honest, logged degradation for the rest.

The parent spec scoped this PR as a "filters subset" and the corpus explains
why: **397 filter fixtures**, the largest tranche in the suite — larger even
than text. Shipping all of it is not one reviewable PR, and much of it is
machinery real documents never use.

## The scoping decision

The realistic-usage split is sharp. Filters that appear in hand-authored SVG,
icon sets, and design-tool exports are a small set; the rest are a test suite
exercising the spec's full generality.

**In scope** (the primitives real documents use):

| Primitive | Fixtures | Why |
|---|---|---|
| `feGaussianBlur` | 13 | The overwhelmingly common filter |
| `feOffset` | 9 | Half of every drop shadow |
| `feFlood` + `flood-color`/`flood-opacity` | 17 | The other half |
| `feMerge` | 3 | Composites the shadow under the source |
| `feBlend` | 10 | Blend modes already exist in `render.Device` |
| `feComposite` | 18 | Porter-Duff; needed by real shadow chains |
| `feColorMatrix` | 16 | Recolor/desaturate; common in icon systems |
| `feDropShadow` | 8 | The shorthand for the whole chain above |
| `filter-functions` | 43 | The CSS `filter:` shorthand (`blur()`, `drop-shadow()`, …) — what HTML/CSS authors actually write, and it feeds PR 8 |

That is ~137 fixtures covering the filters that matter.

**Deferred, each degrading with a log** (the element renders unfiltered rather
than disappearing — a visible approximation beats a blank):

`feTurbulence` (19, Perlin noise — large, self-contained, rarely authored by
hand), `feConvolveMatrix` (25), `feDiffuseLighting`/`feSpecularLighting` +
the light-source elements (50 combined — a lighting model), `feMorphology`
(14), `feImage` (27 — needs the image pipeline PR 8 brings), `feTile` (7),
`feComponentTransfer` (22), `feDisplacementMap` (1), and `enable-background`
(21 — **removed from the spec**, no browser implements it; this one is dropped
outright rather than deferred, like `<tref>` in PR 6).

Deferring is a scope decision, not a fidelity one: everything deferred here is
a candidate for a later PR, and the degradation is logged so a user knows what
they got.

## Architecture

### The offscreen primitive already exists

PR 4 built `BeginGroup`/`EndGroup` on `render.Device` for group opacity, clip,
and mask. A filter is the same shape: render the source into an isolated
offscreen buffer, transform its pixels, composite the result back. The filter
region needs pixel access rather than just compositing, so this PR adds a
narrow way to get the group's rasterized pixels back — the smallest extension
that does the job, following `BuildClipMask`/`BuildLuminanceMask`'s precedent of
letting the *backend* own rasterization while `pkg/svg/draw` stays
backend-agnostic.

### Filters are inherently raster

This is the one place the series' vector-native principle does not apply, and
it must be stated plainly rather than discovered later. A blur has no vector
representation; PDF has no filter operator. **Any filtered element rasterizes
in PDF output**, at a resolution chosen from the filter region and the current
transform.

That is not a regression — it is what every PDF producer does with SVG
filters — but FEATURES.md must say so rather than implying filters are vector.

### The filter graph

`<filter>` holds a sequence of primitives wired by `result`/`in`/`in2` names,
plus the implicit `SourceGraphic`/`SourceAlpha` inputs. Resolution happens at
**parse time**, like paint servers, clips, and masks — `docIndex` is discarded
when `Parse` returns. A primitive naming an undefined `result` falls back to the
previous primitive's output (spec behavior), and a cycle is impossible by
construction since `in` may only name an *earlier* `result`.

`filterUnits`/`primitiveUnits` (`objectBoundingBox` default) and
`x`/`y`/`width`/`height` define the filter region. The region matters more than
it looks: the default is `-10%,-10%,120%,120%`, so a blur has room to bleed,
and getting it wrong clips every shadow.

**`color-interpolation-filters` defaults to `linearRGB`** — unlike everything
else in this engine, which works in sRGB. The corpus tests it directly. Filters
must convert into linear space, operate, and convert back, or every blur comes
out visibly wrong. This is the single most likely source of subtly-off output.

### Text and filters

PR 6 flagged this: `textBBox` uses a crude half-em-per-character estimate, and
`objectBoundingBox` filter regions on text depend on it. A filter on text will
expose that approximation. Fixing it needs real ink extents from the shaped
glyph outlines — now cheap, since PR 6's `pkg/font` fix made glyph `Bounds`
correct. **In scope for this PR**, because filters are what makes it matter.

## Out of scope

- Everything in the deferred table above.
- `enable-background` — dropped (removed from the spec, no implementation).
- Vector filter output in PDF (impossible; documented, not attempted).
- Filters on the HTML/CSS side — PR 8, though `filter-functions` here is the
  shared parser that makes it nearly free.

## Testing

- Each shipped primitive gets unit tests on its pixel math, independent of any
  golden — a blur kernel, a color matrix, and a Porter-Duff mode are all
  checkable against hand-computed values.
- `color-interpolation-filters`: the same blur in `linearRGB` and `sRGB` must
  differ, and the linear one must match resvg. A test asserting only that a
  blur "looks blurred" would pass with the color space wrong.
- Filter-region math: the default `-10%/120%` region, explicit regions, and
  `objectBoundingBox` vs `userSpaceOnUse`.
- A drop-shadow chain end to end (`feOffset`→`feGaussianBlur`→`feFlood`→
  `feComposite`→`feMerge`), the canonical real-world graph.
- Filter on text, proving the ink-extent fix.
- Every deferred primitive degrades **unfiltered with a log**, covered by a test.
- SVG→PDF→reopen→raster for a filtered element, confirming the rasterized
  fallback is placed and scaled correctly.
- Every golden compared against **resvg's reference PNGs**, not eyeballed.
- No pre-existing golden may move.

**Tolerance note:** filter output is floating-point pixel math, so exact
equality with resvg is not expected. The existing ±4/channel and 0.2%
differing-pixel budget applies; a primitive that cannot meet it is reported
rather than having its tolerance widened to hide the gap.
