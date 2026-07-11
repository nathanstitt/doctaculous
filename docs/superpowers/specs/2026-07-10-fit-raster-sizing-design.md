# Page geometry + fit-within-pixel raster sizing

Work area A2 of the tinycld-adoption plan: thumbnail generators ask for "render page 0 to fit
within 480×360" — not for a DPI they'd have to derive per page geometry. Two additions, both
resolved ABOVE the render backends so no painting code changes.

## Document.PageSize (pkg/doctaculous/doctaculous.go + both backends)

The private `renderer` interface gains `pageSize(index) (wPt, hPt float64, err error)`:

- **PDF** (`pdf_backend.go`): MediaBox width/height with the same validation raster.RenderPage
  applies (a crafted MediaBox must not feed NaN/Inf into fit math), **post-/Rotate** — a
  90°/270° page reports its landscape size, matching the pixel dimensions the raster actually
  produces (raster swaps after scaling; ceil-then-swap ≡ swap-then-ceil).
- **Reflow** (`reflow_backend.go`): the laid-out page's `WidthPt/HeightPt` with the shared
  bounds check.

Public `Document.PageSize(index) (widthPt, heightPt, error)` exposes it: callers get the aspect
ratio without rendering (UI placeholders), and it is the primitive fit sizing builds on.

## RasterOptions.MaxWidthPx / MaxHeightPx

When either is > 0, sizing switches from "render at DPI" to "render to fit within this pixel
box, aspect preserved". Design decisions:

- **Resolved to a concrete DPI above the backends** (`fitRaster`): scale = the tightest
  constrained axis's px/pt; opts return with DPI substituted and the Max fields cleared, so
  `renderPage` implementations are untouched and the existing `maxPixels` guard still applies.
  `RasterizePage` and each `RasterizePages` worker resolve **per page** — pages in one document
  differ in size.
- **DPI becomes a resolution ceiling, not a conflict.** Zero DPI (default) = the page always
  fills the box — upscaling a vector re-render is sharp, so "only downscale" would just waste
  the box. A positive DPI caps resolution, giving classic downscale-only thumbnail behavior
  (`{MaxWidthPx: 480, MaxHeightPx: 360, DPI: 300}`). Both fields zero = the exact prior DPI
  path (byte-identical; all goldens unchanged).
- **Ceil-safety**: backends size images as `ceil(pt·scale)`, and float artifacts can push an
  exact fit one pixel past the box (612 × (480/612) can round up). `fitRaster` steps the scale
  down with `math.Nextafter` until the ceiling fits — pinned by an exact-fit test (612×792 into
  612×792 is exactly 612×792, never 613).

Fit is pure sizing: a fit render is **pixel-identical** to rendering at the resolved DPI
(pinned by test), so no new goldens are needed.

## CLI (cmd/doctaculous rasterize + convert image output)

`--max-width` / `--max-height` (pixels, 0 = off). One wrinkle: both commands default
`--dpi 150`, which under "DPI = ceiling" would silently prevent box-filling — so `fitDPI` uses
`flag.Visit`: an unset `--dpi` with a max flag means pure fit (DPI 0); an explicitly passed
`--dpi` is an intentional ceiling. Exercised end to end by CLI tests decoding output dimensions.

## Tests

`pkg/doctaculous/fit_test.go`: PageSize on both backends incl. rotated (792×612) and
out-of-range; the fit table (letter→480×360 = 279×360 pinning the ceil convention, one-axis,
upscale-fill, DPI-ceiling, exact-fit clamp, rotated 466×360); fit ≡ explicit-DPI pixel
identity; per-page resolve inside RasterizePages workers (multipage fixture); WriteImage
bounds. CLI: fit + ceiling dimension assertions, negative rejection.
