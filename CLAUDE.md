# Doctaculous

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/EPUB/HTML, and rasterize pages to images. **Current focus: high-fidelity PDF
page rasterization.** The core pipeline (parse → interpret → raster) is working end-to-end and
renders real-world PDFs faithfully; see "Status & roadmap" at the bottom for what's done and what's
next.

## Non-negotiable constraints

- **Pure Go. No CGo, no native bindings, no WASM engines.** No PDFium / MuPDF / Poppler.
- **MIT licensed.** Every dependency must be MIT/BSD/Apache and pure Go. No GPL/AGPL.
- Approved deps: `golang.org/x/image/*` (BSD), `github.com/srwiley/rasterx` (BSD). Add new deps
  only if pure-Go + permissive; record the reason in the PR.
- **Concurrency-first.** Multi-page work fans out across goroutines (bounded worker pool sized to
  `GOMAXPROCS`). A parsed `*Document` is read-only after Open so it's shared without locks.
- Module path: `github.com/nathanstitt/doctaculous`.

## Architecture (layers — keep them separate and independently testable)

`pkg/pdf` parse · `pkg/pdf/filter` stream decode · `pkg/pdf/content` content-stream interpreter ·
`pkg/render` device-independent paint ops (`Device` interface) · `pkg/render/raster` bitmap
backend · `pkg/doctaculous` public API · `cmd/doctaculous` thin CLI.

The `Device` interface is the seam: the interpreter must stay backend-agnostic so we can add an
SVG/other backend later without touching parsing or interpretation.

## Go practices

- Target the current stable Go; `go.mod` pins the version.
- `gofmt`/`goimports` clean. `go vet ./...` and `golangci-lint run` must pass in CI and locally.
- Errors: wrap with `fmt.Errorf("...: %w", err)`; define sentinel/typed errors for conditions
  callers branch on (e.g. `ErrUnsupportedFilter`, `ErrEncrypted`). Never `panic` on malformed
  input — return an error. Recover at the page boundary so one bad page can't kill a batch.
- Accept interfaces, return concrete types. Public API takes `io.ReaderAt`+size or a path.
- All exported identifiers have doc comments. Keep packages cohesive; no cyclic deps between layers.
- Context-aware: long/parallel operations take `context.Context` and honor cancellation.
- No global mutable state. Pass dependencies explicitly.
- Prefer the standard library; reach for a dep only when it removes real, risky work.

## Testing (this project lives or dies on its test corpus)

- **Every layer has unit tests**: parser (objects, xref tables AND xref streams, object streams),
  filters (round-trip + predictors), interpreter (per-operator behavior), rasterizer (shapes).
- **Prefer generating test PDFs deterministically in `testdata/`** with a small hermetic Go
  generator (`testdata/gen`), one fixture per feature so failures localize:
  text-only, vector-paths-only, image-only (Flate + DCT/JPEG), rotated page (`/Rotate`),
  xref-stream PDF, object-stream PDF, multi-page (for the parallel path), and a couple of
  intentionally-malformed PDFs (to prove graceful degradation, no panics). Generation is the
  default because the fixture stays readable Go and failures localize to one feature. But it is
  not a hard rule: **committing real PDFs is fine** when a fixture is impractical to generate —
  e.g. complex real-world files, output from specific producers, or fidelity/integration cases.
  Commit such PDFs under `testdata/`, keep them as small as the case allows, and note their
  provenance and license in the PR. Use `cmd/dumpfixtures` to materialize generated fixtures when
  you need to inspect them.
- **Core corpus (`gen.Core` in `testdata/gen/core.go`)**: a canonical set of ~10 fixtures —
  `text`, `vector`, `flate`, `multipage`, `rotated`, `image-flate`, `image-jpeg`, `xref-stream`,
  `objstm`, `bad-xref` — each locking down one distinct must-always-work path from parsing
  through rasterization. These are generated (not committed binaries), so the core corpus is
  reproducible and hermetic. Every entry satisfies a uniform contract: parses to a valid
  `Document`, reports its declared `Pages` count, and rasterizes without error (`bad-xref`
  recovers via the object-scan rebuild path). Not every test must iterate the whole set — use it
  where a uniform sweep makes sense (e.g. parser round-trip, golden-image rendering, the
  parallel-render benchmark) by ranging over `gen.Core`; targeted feature tests and edge-case
  fixtures (most malformed inputs, extreme rotations) stay separate. When you add a fixture that
  represents a new core path, add it to `gen.Core` so downstream layers pick it up for free.
- **Golden-image tests** (`pkg/render/raster/golden_test.go`): `TestGolden` ranges over
  `gen.Core`, renders each fixture's first page via `raster.RenderPage` at 72 DPI, and compares
  to a committed PNG in `pkg/render/raster/testdata/golden/<name>.png`. Tolerance is per-pixel
  (±4/channel) plus a 0.2% differing-pixel budget, absorbing anti-aliasing jitter without masking
  real changes. Regenerate after an intentional render change with
  `go test ./pkg/render/raster -run TestGolden -update`, then **eyeball every changed PNG in the
  PR** — an unexplained golden diff is a regression. Goldens are committed (not gitignored); the
  fixtures that produce them are generated, so the whole chain stays hermetic. Glyph rendering is
  implemented for fixtures with embedded font programs (`embedded-truetype`, `type0`, `cff` show
  real glyphs); the standard-font fixtures (`text`, `flate`, `multipage`) still render blank
  because non-embedded base-14 fonts aren't supported yet (`ErrNoEmbeddedProgram`, glyphs skipped)
  — that is the expected baseline and changes only when base-14 support lands.
- **Benchmarks**: `BenchmarkRasterizePages` proves goroutine speedup vs. `--workers 1`. Add a
  race-detector run (`go test -race ./...`) since concurrency is core.
- Tests must be hermetic and fast: no network. Generated fixtures are preferred; committed PDFs
  are allowed where generation is impractical (see above) — keep them small and provenance-noted.
- New feature ⇒ new fixture + test in the same PR. Unsupported PDF features must degrade
  gracefully (skip + debug log), and that behavior must be covered by a test.

## Status & roadmap

The core rasterization pipeline is implemented and validated against a real-world corpus
(`testdata/external/`). Keep this list current as features land — it is the source of truth for
what is done vs. pending.

### Done (covered by `gen.Core` fixtures + golden images unless noted)

- **Parsing**: classic xref tables, xref streams (`/Type /XRef`), object streams (`/ObjStm`),
  object-scan rebuild for broken `startxref`.
- **Encryption**: Standard Security Handler, empty user password — RC4 (V1/V2), AES-128 (V4/AESV2),
  AES-256 (V5/R6/AESV3), verified against `/U` (`pkg/pdf/crypt.go`). Documents needing a real
  password return `ErrEncryptedNeedsPassword`; unsupported handlers return `ErrEncrypted`.
- **Filters**: Flate, LZW, ASCIIHex, ASCII85, RunLength (+ PNG/TIFF predictors), CCITTFax
  (Group 4 / Group 3 1D+2D, `pkg/pdf/filter/ccitt.go`). DCTDecode (JPEG) decoded at image-draw time.
- **Content interpreter**: full path construction/painting, graphics state (`q/Q/cm/w/J/j/M/d`),
  device color (`g/rg/k/cs/sc/scn`), clipping (`W/W*`), text operators, `Do` XObjects.
- **Fills**: nonzero and even-odd winding (the even-odd rasterizer is hand-rolled, dep-free).
- **Strokes**: line joins (miter/round/bevel + miter limit), caps (butt/round/square), and dashes,
  via `github.com/srwiley/rasterx` (`pkg/render/raster/stroke.go`).
- **Form XObjects**: recursion with `/Matrix` composition, scoped `/Resources`, depth guard.
- **Fonts** (via `github.com/benoitkugler/textlayout`): embedded TrueType (FontFile2), CFF/Type1C
  (FontFile3), classic Type1 (FontFile, eexec), Type0/CIDFont (Identity-H/V), symbolic subset
  TrueType (raw-code / code-as-GID glyph lookup), and non-embedded base-14 fonts via bundled
  permissively-licensed substitutes (`pkg/font/standard`: TeX Gyre Heros/Termes, Inconsolata).
- **Transparency**: ExtGState constant alpha `/ca` (fill/text) and `/CA` (stroke), plus all PDF
  blend modes — separable (Multiply, Screen, Overlay, Darken, Lighten, ColorDodge, ColorBurn,
  HardLight, SoftLight, Difference, Exclusion) and non-separable (Hue, Saturation, Color,
  Luminosity) via `/BM` (`pkg/render/raster/blend.go`) — applied to fills, strokes, glyphs, images.
- **Shadings**: the `sh` operator with axial (Type 2), radial (Type 3), and function-based (Type 1)
  shadings, mapping device pixels → parametric value → color via the PDF Function evaluator
  (`pkg/render/raster/shading.go`, `render.Shader` seam). Honors `/Domain`, `/Extend`, the shading
  `/Matrix`, the active clip, and `/BM` blend modes. Also **shading patterns** (`/Pattern` color
  space + `scn`, PatternType 2): a shading pattern set via `scn` fills a subsequent path with the
  shading clipped to it, with the pattern `/Matrix` resolved against the page default coordinate
  system (`pkg/pdf/content/shading.go`). Tiling patterns (PatternType 1) and mesh shadings
  (Types 4–7) are still pending (see TODO).
- **Images**: raw samples in DeviceGray / DeviceRGB / DeviceCMYK / Indexed / ICCBased (by `/N`) at
  1/2/4/8/16 bpc, baseline JPEG (DCTDecode), grayscale `/SMask` soft-mask alpha, 1-bit `/ImageMask`
  stencils painted in the fill color, `/Decode` arrays, and inline images (`BI`/`ID`/`EI`)
  (`pkg/render/raster/image.go`, `page.go`).
- **Page geometry**: `/Rotate` (0/90/180/270), MediaBox/CropBox.
- **Concurrency**: bounded worker pool sized to `GOMAXPROCS`; per-page recover so one bad page can't
  kill a batch.

### TODO (roughly priority order — pick these up next)

Each item should land with a new fixture/test in the same PR (see Testing). Unsupported cases must
already degrade gracefully (skip + debug log / typed error); a TODO becoming supported just turns
that skip into real output.

1. **Remaining scan filters** — JBIG2 and JPX/JPEG2000 (CCITTFax is done). Currently
   `ErrUnsupported` (`pkg/pdf/filter/filter.go`).
2. **Shadings / gradients (remaining)** — **mesh shadings** (Types 4–7: free-form/lattice Gouraud
   triangles and Coons/tensor patches) and **tiling patterns** (PatternType 1). The `sh` operator
   with axial/radial/function-based shadings, the PDF Function evaluator (Types 0/2/3/4), and
   shading patterns (PatternType 2) via `scn` are done. Also **luminosity soft masks** (`/SMask` in
   ExtGState) and **transparency groups**.
3. **Encryption follow-ups** — non-empty user/owner passwords (no password API today), per-stream
   `/Crypt` filter overrides, `/Perms` validation. Empty-password Standard handler is done.
4. **Base-14 weights & symbol fonts** — bold/italic/oblique currently map to the regular face;
   Symbol and ZapfDingbats have no substitute (skipped). Bundle weighted faces + symbol look-alikes,
   and ideally standard AFM widths for exact base-14 metrics.

Out-of-scope, don't gold-plate without a concrete need: full ICC color management, JavaScript,
interactive AcroForm widget rendering, tagged-PDF/accessibility, digital-signature verification.
