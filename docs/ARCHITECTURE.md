# Architecture

Layers — keep them separate and independently testable. No cyclic deps between them.

`pkg/pdf` parse · `pkg/pdf/filter` stream decode · `pkg/pdf/content` content-stream interpreter ·
`pkg/render` device-independent paint ops (`Device` interface) · `pkg/render/raster` bitmap
backend · `pkg/render/pdfwrite` PDF-writer backend · `pkg/render/svgwrite` SVG-writer
backend · `pkg/omnidoc` public API ·
`cmd/omnidoc` thin CLI.

**Reflowable documents** (DOCX and HTML) share a second pipeline that meets the PDF pipeline at
`render.Device`. There is **one recursive, format-neutral box model** (`pkg/layout/cssbox`) that the
CSS layout engine (`pkg/layout/css`) consumes, driving **every** reflow format. A reflow frontend is
a parse+lower step producing a `cssbox` tree with resolved `css.ComputedStyle`:
DOCX → `cssbox` via `pkg/docx` parse → `pkg/docx/style` cascade → `pkg/docx/cssbox` lowering;
HTML → `cssbox` via `pkg/html` + `pkg/css` + `pkg/layout/css` box generation. A frontend never
touches line-breaking or pagination. The engine uses one **inline-layout core** (`pkg/layout/inline`:
shaping, greedy line-breaking, alignment/justification math). `pkg/layout` retains the shared output
types (`Pages`/`Page`/`Item`) and `pkg/layout/paint`. Font outlines come from `pkg/font`
(`pkg/font/family.go` exposes named-family faces for reflow); `pkg/layout/font` caches them.

**SVG** is a third frontend and does NOT go through `cssbox`: it is not reflowable, so `pkg/svg`
parses to its own read-only scene graph (shapes, groups, paint servers, clips/masks, text, filters)
and `pkg/svg/draw` paints that straight onto a `render.Device`. It shares `pkg/css` for the cascade,
`pkg/layout/inline` for text shaping (`inline.Shape` is a pure function, not fused to line-breaking,
so SVG skips `Break`/`MakeLine`/`Place`), and `pkg/font`/`pkg/layout/font` for faces.
`pkg/svg/filter` holds the filter pixel math; `pkg/filtereffects` is a dependency-free parser for the
CSS `filter` shorthand, shared by the SVG frontend and `pkg/layout/paint`'s HTML filter path.

SVG reaches HTML/EPUB through `layout.VectorScene`/`VectorItem` — a `Fragment.Vector` carrier
parallel to `Image`, painted by `paint.paintVector`. That seam is what keeps an embedded SVG
**vector** into PDF rather than a bitmap; routing it through the raster `imageCache`/`ImageContent`
path would silently undo it.

The `Device` interface is the seam: the interpreter (PDF), the reflow engine (DOCX/HTML), and the
SVG painter stay backend-agnostic so a new backend can be added without touching parsing,
interpretation, or layout.

`pkg/render/svgwrite` is what that seam buys. Because all three frontends already paint through
`render.Device`, one backend gives **every** input format vector SVG output — PDF included — with
no per-format work. Output goes through `vectorPages` (`pkg/omnidoc/svgwrite_backend.go`), NOT
`reflowPages`: the latter hands back `*layout.Pages`, which an opened PDF has no equivalent of, so
a writer built on it is reflow-only (that is why `WritePDF` is). `vectorPages` passes the `Device`
instead, and `raster.RunPage` shares the PDF page-setup between the raster and SVG backends so the
two cannot drift.

