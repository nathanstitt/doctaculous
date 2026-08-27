# SVG Paint Servers (PR 3 of 8) — Design

**Date:** 2026-08-25
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-paint-servers`, stacked on `feat/svg-styling` (PR #103)

## Goal

`fill="url(#g)"` and `stroke="url(#g)"` render real gradients and patterns instead
of degrading to no-paint. Covers `<linearGradient>`, `<radialGradient>`, and
`<pattern>`, with `<stop>` parsing, both unit systems, `gradientTransform`/
`patternTransform`, all three `spreadMethod` values, and `href` inheritance
chains with cycle detection.

## Decisions taken (autonomous, with rationale)

1. **PDF output rasterizes gradients into an image XObject this PR.**
   `pdfwrite.FillShading` is currently a **no-op stub** (`pkg/render/pdfwrite/device.go:200`),
   so shipping gradients without touching it would render *nothing* in PDF — silently
   worse than today's honest "no fill". Emitting a real PDF shading dictionary is the
   better end state, but `render.Shader` is an opaque `ColorAt`-only interface, so
   pdfwrite cannot introspect it to recover coordinates and stops; that needs either a
   richer interface or a parallel paint description. PR 4 already opens pdfwrite for
   transparency groups, so the vector path belongs there. This PR makes pdfwrite sample
   the shader into an XObject via its existing working `DrawImage` path: correct output,
   self-contained, and honest about the fidelity loss in a log line.
2. **Shapes carry a resolved paint server; `FillPaint` stays solid-only.**
   Adding a `Shader` field to `render.FillPaint` would silently break the seven
   non-raster Device backends that read only `Color` (they would paint the zero value —
   a wrong-output failure, not a compile error). Instead the SVG renderer follows the
   existing precedent at `pkg/pdf/content/paths.go:70-79`: `Save` → `PushClip(path, rule)`
   → `FillShading` → `Restore`.
3. **Paint servers resolve during `Parse`, not at draw time.** `docIndex` is discarded
   when `Parse` returns and `Document` must stay read-only for the lock-free page
   fan-out, so a `Shape` carries an already-resolved paint server value.
4. **`(*render.Path).Bounds()` computes TRUE curve extrema.** `objectBoundingBox` units
   need the tight geometry bbox. The existing `raster.pathDeviceBounds` bounds control
   points and pads by a pixel — on a circle (four cubics) the control hull is ~10%
   larger per axis, which would visibly offset and mis-scale every bbox-unit gradient.

## Architecture

### 1. `pkg/render` — a path bounding box

`func (p *Path) Bounds() (minX, minY, maxX, maxY float64, ok bool)` in
`pkg/render/path.go`. Solves cubic extrema (roots of the derivative in t) rather than
bounding the control hull. `ok=false` for an empty path. Exported because PR 4
(clipPath/mask) and PR 7 (filter regions) need the same thing.

### 2. `pkg/render/raster` — PDF-free shader constructors

Export constructors that build the existing `shading` struct without a `*pdf.Document`:

```go
func NewAxialShader(x0, y0, x1, y1 float64, fn function.Func, spread Spread) render.Shader
func NewRadialShader(fx, fy, fr, cx, cy, cr float64, fn function.Func, spread Spread) render.Shader
```

The math (`atAxial`/`atRadial`/`ColorAt`) is untouched — the package's own tests already
construct `&shading{}` literally, proving separability.

**`spreadMethod` is genuinely new math.** PDF's `/Extend` models only `pad`; `reflect`
and `repeat` need new logic in the `sval` clamp (`shading.go:263`) and the radial
`consider` closure (`shading.go:299`). Implemented as a `Spread` enum threaded into the
struct, with `pad` preserving today's exact behavior so no PDF golden moves.

### 3. `pkg/svg` — paint-server resolution

New `pkg/svg/paintserver.go`:

- Parse `<stop>` children into an offset/color/opacity ramp implementing
  `function.Func` (a 1-in/3-out lerp; `shading_test.go:22`'s `linRamp` is the shape).
  Stops clamp to [0,1], sort stably, and a later stop at a lower offset takes the
  previous stop's offset (per spec).
- Resolve `href` chains: attribute inheritance is per-attribute first-defined-wins;
  stop inheritance is all-or-nothing from the nearest ancestor that has stops;
  cross-type (`linearGradient` href-ing a `radialGradient`) is legal. A non-gradient
  target is a no-op.
- **Cycle detection** lives in the chain walker: a visited set plus a depth cap. Written
  as a reusable helper because PR 5 (`use`/`symbol`) needs the same machinery, and the
  reference graph is NOT covered by the parse-time `maxElementDepth`.
- Memoize resolved servers on `sceneBuilder` so twenty shapes sharing one gradient
  resolve the chain once.

`Style` gains a paint-server reference (the fragment id) alongside the solid color,
plus SVG's `fill="url(#g) red"` fallback-color syntax. `applyPaint` stores the id;
resolution happens in the scene builder, which holds the index. New accessors
`FillServer()`/`StrokeServer()` let `pkg/svg/draw` branch.

**Lookup uses `ids`, not `defs`** — a gradient is referenceable from anywhere in the
document, and the corpus has fixtures with gradients outside `<defs>`.

### 4. `pkg/svg/svg.go` — the dispatch trap

`linearGradient`, `radialGradient`, and `pattern` move from `unsupportedElements` to
`skippedElements`, and `stop` is ADDED to `skippedElements`. This is the sharp edge the
survey flagged: `buildNode`'s `default` branch is a forgiving container that recurses
into unknown elements, so merely deleting `pattern` from the unsupported table would
paint its tile children directly into the visible scene at document coordinates. The
replacement branch must be explicit and total.

### 5. `pkg/svg/draw` — painting with a server

For a shape with a resolved server: `Save` → `PushClip(devicePath, rule)` →
`FillShading(shader, ctm, blend)` → `Restore`. The ctm composes
objectBoundingBox→user (`Translate(minX,minY) × Scale(w,h)`), then `gradientTransform`,
then `Shape.M`, then the accumulated matrix.

**Gradient strokes**: there is no `PushClipStroke`, so clipping to a stroked outline
needs a stroke-to-outline conversion. If `pkg/render/raster/stroke.go` does not expose
one, `stroke="url(#g)"` degrades to the fallback color (or no stroke) with a log —
documented, tested, and left to a follow-up. Gradient FILLS are the common real-world
case and are the priority.

**Patterns** render their tile to an offscreen image once, then paint via `DrawImage`
tiled across the shape's clip. If the tile work proves large, patterns may be deferred
to a follow-up PR with gradients shipping alone — decided during implementation, and
recorded either way.

## Out of scope

- Real PDF shading dictionaries (deferred to PR 4, which already opens pdfwrite).
- `patternContentUnits` beyond the common case, nested pattern recursion.
- Any selector/diagnostic work: the PR 2 reviewer suggested closing the
  silent-inert-selector gap here, but it is unrelated to paint servers and belongs in
  its own change.

## Testing

- Unit: stop parsing (invalid/missing offsets, out-of-order, opacity), href chains
  including cross-type and cycles, `Bounds()` against known curve extrema, spread modes.
- Integration: gradient fills render correct pixels at both unit systems; a cycle
  degrades without hanging; SVG→PDF produces visible output.
- Corpus: a curated tranche from resvg's 149 `paint-servers/**` fixtures (38 linear,
  45 radial, 31 pattern, 35 stop-related), excluding anything needing `<use>`, text,
  mask, or filters. Every new golden visually inspected — that step found two real bugs
  in PR 1 and one in PR 2.
- No pre-existing golden may move.
