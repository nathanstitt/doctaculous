# Doctaculous

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/HTML, and rasterize pages to images. The core pipeline (parse → interpret →
raster) is working end-to-end and renders real-world PDFs, DOCX, and HTML faithfully; see FEATURES.md for the full
inventory of what has shipped, and "Status & roadmap" at the bottom for what is next.

## Working directives (how to build here)

- **Always implement the maximal, most browser-faithful option.** For any feature with a
  fidelity/scope choice (which CSS values, how complete the algorithm, edge cases), pick the fullest
  spec-compliant behavior — do NOT ask which subset to do. Lean to thoroughness; only surface a
  question for a genuine product decision that cannot be inferred, and even then default to maximal.
- **Every feature lands with tests AND a visual entry.** Add unit/golden tests for each part, and
  add the feature to the `testdata/htmldoc/` showcase (a new section + regenerated, eyeballed
  goldens) so it is visually exercised end to end.
- **Each engine feature/sub-project is its own branch → PR off `main`**, merged when CI is green.
  Keep changes additive and byte-identical for untouched callers (DOCX/PDF, pages not using the
  feature). Design docs for each sub-project live in `docs/superpowers/specs/` — read the relevant
  one before extending an area; that is where the detailed history and rationale live.

## Non-negotiable constraints

- **Pure Go. No CGo, no native bindings, no WASM engines.** No PDFium / MuPDF / Poppler.
- **MIT licensed.** Every dependency must be MIT/BSD/Apache and pure Go. No GPL/AGPL.
- Approved deps: `golang.org/x/image/*` (BSD), `github.com/srwiley/rasterx` (BSD),
  `github.com/benoitkugler/textlayout` (font parsing), `golang.org/x/net/html` (HTML parse),
  `github.com/andybalholm/brotli` (MIT, pure-Go — WOFF2 Brotli decompression only),
  `github.com/beevik/etree` (BSD-2, pure-Go, zero deps — the raw-fidelity XML DOM the xlsx
  editor rewrites dirty parts through; prefixes/attr order/CDATA preserved, verified in source
  before adoption). Add new deps only if pure-Go + permissive; record the reason in the PR.
- Vendored (copied into the tree, not a `go get` dep): `github.com/xiaoqidun/jbig2` (Apache-2.0, pure
  Go — JBIG2 image decode) in `pkg/pdf/filter/jbig2/`, vendored because it is new/solo-authored (see
  that dir's README + NOTICE); its only dep is `golang.org/x/image` (already used). Excluded from
  golangci-lint via `.golangci.yml` as an unmodified third-party copy.
- **Concurrency-first.** Multi-page work fans out across goroutines (bounded worker pool sized to
  `GOMAXPROCS`). A parsed `*Document` is read-only after Open so it's shared without locks.
- Module path: `github.com/nathanstitt/doctaculous`.

## Architecture (layers — keep them separate and independently testable)

`pkg/pdf` parse · `pkg/pdf/filter` stream decode · `pkg/pdf/content` content-stream interpreter ·
`pkg/render` device-independent paint ops (`Device` interface) · `pkg/render/raster` bitmap
backend · `pkg/render/pdfwrite` PDF-writer backend · `pkg/doctaculous` public API ·
`cmd/doctaculous` thin CLI.

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

The `Device` interface is the seam: the interpreter (PDF) and the reflow engine (DOCX/HTML) stay
backend-agnostic so a new backend can be added without touching parsing, interpretation, or layout.

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

- **Every layer has unit tests**: parser (objects, xref tables AND streams, object streams), filters
  (round-trip + predictors), interpreter (per-operator behavior), rasterizer (shapes), and the CSS
  engine (box-gen, per-algorithm unit suites, fragment-geometry).
- **Prefer generating test PDFs deterministically** with the hermetic Go generator (`testdata/gen`),
  one fixture per feature so failures localize. **Committing real PDFs is fine** when a fixture is
  impractical to generate (complex real-world files, specific producers, fidelity/integration cases)
  — keep them small and note provenance/license in the PR. `cmd/dumpfixtures` materializes generated
  fixtures for inspection.
- **Core corpus (`gen.Core` in `testdata/gen/core.go`)**: ~10 fixtures (`text`, `vector`, `flate`,
  `multipage`, `rotated`, `image-flate`, `image-jpeg`, `xref-stream`, `objstm`, `bad-xref`) each
  locking one must-always-work path from parse through raster. Range over it where a uniform sweep
  fits (parser round-trip, golden rendering, the parallel-render benchmark). When you add a fixture
  for a new core path, add it to `gen.Core`.
- **Golden-image tests** (`pkg/render/raster/golden_test.go`, plus the `pkg/doctaculous` `docx-*` /
  `html-*` / `htmldoc-*` goldens): render at 72 DPI, compare to committed PNGs with a per-pixel
  tolerance (±4/channel) + 0.2% differing-pixel budget. Regenerate an intentional render change with
  `go test ./pkg/render/raster -run TestGolden -update`, then **eyeball every changed PNG in the PR**
  — an unexplained golden diff is a regression. Goldens are committed; the fixtures that produce them
  are generated, so the chain stays hermetic. HTML/DOCX also carry WPT-style reftests.
- **Benchmarks**: `BenchmarkRasterizePages` proves goroutine speedup vs. `--workers 1`. Run the
  race detector (`go test -race ./...`) since concurrency is core.
- Tests must be hermetic and fast: no network (HTTP paths use `net/http/httptest` loopback).
- New feature ⇒ new fixture + test + showcase entry in the same PR. Unsupported features must
  degrade gracefully (skip + debug log / typed error), and that behavior must be covered by a test.

## Status & roadmap

The full inventory of shipped features lives in **[FEATURES.md](FEATURES.md)** — keep it current:
every feature that lands gets a bullet there in the same PR (a one-line pointer; the detailed
design/rationale stays in the sub-project's `docs/superpowers/specs/` doc). This section keeps only
what is NOT done yet.

### TODO (roughly priority order)

Each item lands with a new fixture/test + showcase entry in the same PR. Unsupported cases already
degrade gracefully; a TODO becoming supported just turns that skip into real output.
1. **Remaining scan filter** — JPX/JPEG2000 only (`pkg/pdf/filter/filter.go`, `ErrUnsupported`); no
   viable pure-Go decoder exists (JBIG2 shipped via a vendored Apache-2.0 decoder — see FEATURES.md).
2. **Shadings / gradients (remaining)** — tiling patterns (PatternType 1; skipped + logged),
   higher-fidelity Coons/tensor patches (Types 6/7, currently bilinear-corner), luminosity soft
   masks (`/SMask` in ExtGState), and transparency groups.
3. **Encryption follow-ups** — non-empty user/owner passwords (no password API today), per-stream
   `/Crypt` overrides, `/Perms` validation.
4. **Base-14 residuals** — weighted/slanted substitutes now ship (see FEATURES.md); a caller-supplied
   `FontProvider` resolves Symbol/ZapfDingbats and exact-metric faces. Remaining, low-value: a bundled
   OFL Symbol look-alike for the no-provider case, AFM tables for exact base-14 advances when a PDF
   omits `/Widths`, and synthetic emboldening/obliquing for a family missing a real variant.
5. **DOCX fonts** — de-obfuscate embedded `word/fonts/*` (improves bold/italic fidelity), and give
   DOCX the system-font default (it currently resolves bundled-only; the `OSFontProvider` seam exists,
   it is just not installed in `docxDocument`).
6. **PDF-extraction quality** — the PDF → Markdown/HTML path ships (`pkg/pdf/extract`); the top lifts
   are **ToUnicode CMap parsing** (Type0/CID text — CJK / subsetted fonts currently yield `Rune==0`),
   font weight/slant through `GlyphSource` (emphasis + weight-based heading detection), and
   scanned-PDF OCR.
7. **Fuller paged-media in the PDF-writer path** — carry the CSS Paged Media features into
   `pkg/render/pdfwrite`.

**Open fidelity follow-ups** (the engine renders these paths; these are the known approximations —
each degrades gracefully and is documented in the relevant spec):

- **RTL / `direction` / bidi** — the engine has no bidi support; tables, flex, and grid lay out
  LTR-only (parsed but not acted on, logged). This is the single largest cross-cutting gap.
- **Multi-line flexbox** — `flex-wrap: wrap`/`wrap-reverse` + `align-content` (currently single-line
  `nowrap` with overflow); column `flex-basis: auto`/`content` uses a max-content-width proxy.
- **Grid** — named-line placement (`[name]` tokens parsed-and-ignored → auto-placement), `subgrid`
  (→ `none`), `auto-fit` empty-track collapse approximate, ROW-track content-height width-proxy.
- **Absolute/replaced sizing edge cases** — precise static-position solve for an all-`auto`-offset
  abs box (C1), `bottom`-only auto-height abs box (C5, needs vertical shrink-to-fit), percentage
  `top`/`bottom`/`height` against an auto-height CB (C4/D3), `position:relative` on a text-only
  inline box (C6, no fragment to carry the offset).
- **`vertical-align`** — full keyword set (atom-baseline mechanics landed); `margin:auto`
  block centering; deferred margin-collapse edge cases (empty-block collapse-through, clearance,
  `min-height` interaction).
- **Web-font descriptors** — synthetic bold/oblique, `unicode-range` subsetting, `font-display`,
  variable-font axes, `local()` beyond the disk adapter; a content-addressed fetch cache (FaceCache
  is keyed `(family, style)`).
- **Pagination** — mid-cell / mid-item (flex/grid) content splitting of a genuinely-indivisible
  over-tall row/item overflows; positioned/float distribution within a different-width named-page run.

Out-of-scope, don't gold-plate without a concrete need: full ICC color management, JavaScript,
interactive AcroForm widget rendering, tagged-PDF/accessibility, digital-signature verification.
(EPUB — previously out of scope — landed as an input format when the any⇄any conversion goal
made it a requirement; DRM-protected books stay refused by design.)
