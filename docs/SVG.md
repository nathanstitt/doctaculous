# SVG — status and open work

SVG ships as an input format (8 PRs: core, styling, paint servers, groups/clip/mask,
`use`/`symbol`/markers, text, filters, HTML/EPUB integration). The full shipped inventory is
in [../FEATURES.md](../FEATURES.md); this file holds what is NOT done.

**Vector-native throughout.** An SVG reaching PDF via `<img>`, inline markup,
`background-image`, or an EPUB cover emits real path operators, never a rasterized image —
asserted on the emitted PDF, not on pixels. Routing SVG through the raster
`imageCache`/`ImageContent` path would silently undo that; use the
`layout.VectorScene`/`VectorItem` seam instead.

## Not implemented

- **`<textPath>`** (44 corpus fixtures) — needs arc-length parameterization of a `render.Path`
  plus per-glyph tangent frames. `render.Vertices` (PR 5) gives tangents at *vertices*, not at an
  arbitrary distance along a curve, so it is a start and not a solution. Degrades to a straight
  baseline with a log.

- **`writing-mode`** (25 fixtures) — needs `vhea`/`vmtx` vertical metrics `pkg/font` does not
  parse, plus a vertical advance model; every metric in the engine is horizontal-only. Degrades to

  horizontal with a log.

- **Filters not implemented** (each renders the element unfiltered with a log): `feTurbulence`,
  `feConvolveMatrix`, `feDiffuseLighting`/`feSpecularLighting` + light sources, `feMorphology`,
  `feImage`, `feTile`, `feComponentTransfer`, `feDisplacementMap`. `enable-background` is dropped

  2.78% differing pixels; the tolerance was NOT widened to hide it.

## Known approximations

- **`preserveAspectRatio` on an EMBEDDED SVG** — `fitSceneTo` scales per-axis, so a CSS box whose
  aspect differs from the document's squashes where a browser re-applies the SVG's own
  `preserveAspectRatio` against the used size and letterboxes. Exact whenever CSS sizing preserved
  the ratio (unsized `<img>`, one axis given, matching box) — the common case. Needs the parsed

  outright (removed from the spec, implemented by no browser), as is `<tref>`.

- **A non-uniform transform rasterizes a filter region at the wrong aspect** — `filterSpace` derives
  a single uniform scale (`pkg/svg/draw/filter.go`); a per-axis filter space threaded through
  `filterM`/`postM` and every primitive's subregion math would fix it. One corpus fixture measures


  value retained on `svg.Document`; `resolveSize` consumes it into `rootM` and discards it.

- **`letter-spacing`/`word-spacing` are SVG-only** — implemented in `pkg/svg/style.go`, absent from
  `ComputedStyle`, so an SVG-internal declaration works but inheriting one from an enclosing HTML
  ancestor does not. Wiring them into reflow means facing line-breaking and justification.
