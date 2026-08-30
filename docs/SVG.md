# SVG — status and open work

SVG ships as an input format (8 PRs: core, styling, paint servers, groups/clip/mask,
`use`/`symbol`/markers, text, filters, HTML/EPUB integration) and as an **output** format
(`pkg/render/svgwrite`). The full shipped inventory is in [../FEATURES.md](../FEATURES.md); this
file holds what is NOT done.

**Vector-native throughout.** An SVG reaching PDF via `<img>`, inline markup,
`background-image`, or an EPUB cover emits real path operators, never a rasterized image —
asserted on the emitted PDF, not on pixels. Routing SVG through the raster
`imageCache`/`ImageContent` path would silently undo that; use the
`layout.VectorScene`/`VectorItem` seam instead.

## Not implemented — the SVG WRITER (`pkg/render/svgwrite`)

- **`<text>` output with embedded fonts.** Glyphs are emitted as `<path>` outlines. The pipeline
  already carries what real text needs (`render.GlyphRef` has `Face`, `GID` and `Runes`, and the
  concrete `*font.Face` is recoverable by type assertion, exactly as `pdfwrite/device.go` does),
  so the blocker is purely font embedding:

  - there is **no WOFF/WOFF2 encoder** in the repo — `pkg/font/woff1.go`/`woff2.go` are
    decode-only and unexported. WOFF2 output would need a Brotli encoder.
  - `Face.ProgramBytes()` returns nil for `ProgramKindUnknown`.
  - decisively, the bundled substitutes are **TeX Gyre Type1 `.pfb`** (`pkg/font/standard/fonts/`),
    which browsers cannot load through `@font-face` at all — so the default-font case, the common
    one, cannot produce working `<text>` regardless of the other two.

  Closing it means an `SVGOptions.TextMode` emitting `data:font/ttf;base64` `@font-face` for SFNT
  faces and falling back to outlines otherwise, mirroring pdfwrite's per-glyph
  embeddable-or-outline degradation. The registry would mirror `pkg/render/pdfwrite/font.go`
  (a `map[*font.Face]` table with deterministic ordering), swapping `/F0` names for CSS families.
  Note it could **never cover PDF input**: `content.drawGlyph` calls `dev.FillGlyph` with an
  already-flattened outline and `content.Glyph` carries no `Face`/`GID`, so PDF text would need a
  new interpreter seam. Scope it to reflow inputs.

- **Native `<clipPath>` for a clip-path union.** `Device.BuildClipMask` returns
  `GroupMask = *image.Alpha`, which collapses the `[]MaskPath` union to pixels, so a group's clip
  is embedded as a rasterized `<mask>`. SVG can express the union natively and would stay
  resolution-independent, but only by keeping the path list, which the return type forbids. The
  sentinel-pointer trick in `pkg/render/pdfwrite/softmask.go` is the documented way around it —
  note `render.Device.EndGroup`'s warning about recognizing a mask by identity before copying it.

- **PDF-sourced gradients rasterize.** `DescribeShading` is gated on `alphaFromFn`
  (`pkg/render/raster/shading.go`), so only CSS/SVG-constructed gradients describe themselves; a
  gradient that came from a PDF `/Shading` dictionary takes the sampled-`<image>` fallback. This
  is an upstream decision (round-tripping a parsed shading back into a description risks the
  CMYK/component-count confusion `alphaFromFn` exists to avoid), not a writer limitation.
  Gradients from HTML/SVG input stay native.

- **Stroke width is isotropic on the PDF path.** `pkg/pdf/content/paths.go` scales line width by
  `ctm.ScaleFactor()`, a single scalar, so a non-uniform CTM loses stroke anisotropy before the
  Device sees it. Pre-existing and inherited, not introduced by the writer; SVG's `vector-effect`
  cannot recover it.

- **Round-trip verification is partial.** `TestSVGOutputRoundTripsThroughOwnParser` re-reads the
  emitted SVG through `pkg/svg` and compares pixels, but skips fixtures that would measure a
  READER gap rather than the writer. Of 35 core fixtures, 18 are pixel-compared and 17 skip:
  10 emit `<image>` (9 `image-*` plus `inline-image`), 5 are `shading-*` (PDF shadings are not
  self-describing upstream, so they take the sampled-`<image>` fallback and hit the same gap),
  and 2 are `blend-*` (`mix-blend-mode` is not honored by the reader). Those paths are asserted
  structurally instead. Implementing `<image>` in the reader would close 15 of the 17.

## Not implemented — the SVG READER (`pkg/svg`)

- **`<image>`**, which the writer now emits (for raster content, masks, and sampled shadings) and
  the reader cannot draw. Verified by measurement: a document whose only content is a solid-blue
  `<image>` rasterizes blank white.

  This is the single highest-value gap to close. It blocks 15 of the 17 skipped round-trip
  fixtures (see above), and it also silently breaks **masks on re-read**: `svgwrite` emits a
  group's mask as `<mask mask-type="alpha">` wrapping an `<image>`, so on the way back in the
  mask content renders as nothing, which SVG interprets as "masked out entirely" — a masked
  element disappears rather than showing at partial coverage. The emitted markup is correct
  (browsers render it), so this is a reader limitation, not a writer one.

- **`<textPath>`** (44 corpus fixtures) — needs arc-length parameterization of a `render.Path`
  plus per-glyph tangent frames. `render.Vertices` (PR 5) gives tangents at *vertices*, not at an
  arbitrary distance along a curve, so it is a start and not a solution. Degrades to a straight
  baseline with a log.

- **`writing-mode` — remaining work.** Vertical `<text>` ships (see FEATURES.md): the pen walks
  down the page, `text-orientation` turns each glyph, and `text-anchor`, decorations and bidi
  reordering all follow the run's own axis. What is left:

  - **Four resvg fixtures are held back**, of the 23 in the upstream `text/writing-mode/` tranche:
    `japanese-with-tb`, `tb-and-punctuation`, `mixed-languages-with-tb`, and
    `mixed-languages-with-tb-and-underline`. Each is set in CJK, no bundled face covers it, and
    they render as columns of `.notdef` boxes — committing those goldens would lock in tofu as
    expected output. They land with a CJK face, if one is ever vendored (check `DEPENDENCIES.md`).
    The other 19 are in `testdata/svg/resvg/text/writing-mode/`.
  - **Missing glyphs do not log on the SVG text path.** Rendering the held-back fixtures produced
    full columns of `.notdef` with zero diagnostics; `pkg/layout/inline`'s `warnMissingGlyph` fires
    for the CSS path but the logger is evidently not threaded through here. That is its own bug and
    is not fixed by this work.
  - **Multi-line stacking.** One `<text>` is one vertical run, so `vertical-lr` places its run
    identically to `vertical-rl` — the two differ only in the side subsequent lines stack from.
    SVG 1.1's `tb`/`tb-rl` resolve onto `vertical-rl`.
  - **`glyph-orientation-vertical`/`-horizontal`**, the deprecated SVG 1.1 spellings of what
    `text-orientation` now does. Not parsed; a document using them gets the `mixed` default.
  - **letter-spacing/word-spacing on an UPRIGHT vertical run.** Those are defined along the inline
    axis, and an upright glyph's advance comes from the font's vertical metric rather than from the
    spacing-adjusted horizontal one. A *sideways* run honours them, since it advances by the
    adjusted horizontal extent.

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

- **NO computed CSS property inherits across the HTML→SVG boundary** — not `letter-spacing`, not
  `color`, not `font-family`. Inline `<svg>` is REPLACED content: box generation re-serializes the
  markup and `pkg/svg` re-parses it through `svg.Parse(data, logf)`, whose entire input is the
  markup and a logger. There is no seam for a `ComputedStyle` to cross.

  This was previously recorded as a consequence of `letter-spacing`/`word-spacing` being absent from
  `ComputedStyle`. That framing was wrong. Those fields now exist and are fully implemented for CSS
  reflow, and adding them did **not** change this behavior — verified by measurement:
  `<div style="letter-spacing:20px"><svg><text>III</text></svg></div>` renders ink spanning
  x=[2..26], identical to no declaration at all, while an SVG-internal `letter-spacing="20"`
  correctly spans x=[2..106].

  Closing it means giving `svg.Parse` (or the inline-SVG cache) an inherited-style parameter and
  seeding the SVG root's `Style` from it — a whole-boundary change worth doing once for the entire
  inherited set, not one property at a time.
