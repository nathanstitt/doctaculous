# Doctaculous

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/HTML, and rasterize pages to images. The core pipeline (parse → interpret →
raster) is working end-to-end and renders real-world PDFs, DOCX, and HTML faithfully; see "Status &
roadmap" at the bottom for what's done and what's next. (**EPUB is out of scope** — the HTML
pipeline is the reflow target.)

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
  `github.com/andybalholm/brotli` (MIT, pure-Go — WOFF2 Brotli decompression only). Add new deps
  only if pure-Go + permissive; record the reason in the PR.
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

Validated against a real-world corpus (`testdata/external/`). Keep this list current as features
land. Each Done bullet is a one-line pointer; the detailed design/rationale for a sub-project is in
its `docs/superpowers/specs/` doc (and in git history).

### Done

**PDF pipeline** (covered by `gen.Core` fixtures + golden images):

- **Parsing**: classic xref tables, xref streams (`/Type /XRef`), object streams (`/ObjStm`),
  object-scan rebuild for broken `startxref`.
- **Encryption** (`pkg/pdf/crypt.go`): Standard Security Handler, empty user password — RC4
  (V1/V2), AES-128 (V4/AESV2), AES-256 (V5/R6/AESV3). Real-password docs →
  `ErrEncryptedNeedsPassword`; unsupported handlers → `ErrEncrypted`.
- **Filters**: Flate, LZW, ASCIIHex, ASCII85, RunLength (+ PNG/TIFF predictors), CCITTFax
  (Group 4 / Group 3 1D+2D), DCTDecode (JPEG). JBIG2 and JPX/JPEG2000 pending (`ErrUnsupported`).
- **Content interpreter** (`pkg/pdf/content`): path construction/painting, graphics state, device
  color + Separation/DeviceN spot color (tint-transform `/Function`), clipping, text operators
  (incl. text render modes), `Do` XObjects.
- **Fills**: nonzero + even-odd winding (hand-rolled even-odd rasterizer, dep-free).
- **Strokes** (`pkg/render/raster/stroke.go`): joins (miter/round/bevel + limit), caps, dashes.
- **Form XObjects**: recursion with `/Matrix`, scoped `/Resources`, depth guard, mandatory `/BBox`
  clip.
- **Fonts** (`github.com/benoitkugler/textlayout`): embedded TrueType (FontFile2), CFF/Type1C
  (FontFile3), classic Type1 (FontFile, eexec), Type0/CIDFont (Identity-H/V), symbolic subset
  TrueType, and non-embedded base-14 via bundled substitutes (`pkg/font/standard`: TeX Gyre
  Heros/Termes, Inconsolata).
- **Transparency**: ExtGState alpha `/ca`/`/CA` + all PDF blend modes (separable + non-separable)
  via `/BM` (`pkg/render/raster/blend.go`).
- **Shadings** (`pkg/render/raster/shading.go`, `render.Shader`): axial/radial/function-based via
  `sh`, shading patterns (PatternType 2) via `scn`, and mesh shadings (Types 4–7,
  `shading_mesh.go`; Coons/tensor tessellated with a bilinear-corner approximation). Tiling patterns
  (PatternType 1) pending.
- **Images** (`pkg/render/raster/image.go`): DeviceGray/RGB/CMYK/Indexed/ICCBased at 1–16 bpc,
  baseline JPEG, `/SMask` alpha, `/ImageMask` stencils, `/Decode` arrays (raw + DCT paths), inline
  images (`BI`/`ID`/`EI`).
- **Page geometry**: `/Rotate` (0/90/180/270), MediaBox/CropBox.
- **Concurrency**: `GOMAXPROCS`-bounded worker pool; per-page recover so one bad page can't kill a
  batch. Crafted-PDF panic sites guarded directly.

**Reflow engine (HTML + DOCX)** — shared CSS layout engine (`pkg/layout/css`), covered by
`html-*` / `docx-*` / `htmldoc-*` goldens, WPT-style reftests, and per-algorithm unit suites. Each
bullet's design doc is in `docs/superpowers/specs/`:

- **CSS parse + cascade** (`pkg/css`): dependency-free tokenizer/parser, selector matching +
  specificity, full cascade (specificity + source order + inheritance + `!important` + inline
  `style` + origins), shorthand expansion. `2026-06-23-html-rendering-design.md`.
- **HTML frontend — box generation** (`pkg/html`, `pkg/layout/cssbox`): owned DOM, UA stylesheet,
  anonymous-box fixups, whitespace collapsing, `display:none` pruning; `<link>` via
  `pkg/resource.ResourceLoader`. `2026-06-23-html-box-generation-design.md`.
- **Block + inline normal flow** (`pkg/layout/inline`, `pkg/layout/css/block.go`+`inline.go`,
  `pkg/layout/paint`, `OpenHTML`/`OpenHTMLBytes`): box model (width/`auto`/%, `box-sizing`,
  min/max, margins incl. vertical collapsing, padding, borders, backgrounds), IFC (shaping/breaking,
  `text-align`, `line-height`), fragment tree. `2026-06-23-html-block-inline-flow-design.md`.
- **Replaced content + images** (`pkg/layout/css/image.go`+`replaced.go`): `<img>` decode (PNG/JPEG/
  GIF, stdlib) → CSS replaced-sizing → paint via `DrawImage`, with `object-fit`/`object-position`.
  `2026-06-24-html-replaced-images-design.md`.
- **Floats + clear** (`pkg/layout/css/floats.go`): per-BFC float context, narrowing/wrapping,
  `clear`, own paint layer. `2026-06-24-html-floats-design.md`.
- **Positioning** (`pkg/layout/css/positioning.go`): relative (paint-time offset) + absolute/fixed
  (out-of-flow, two-pass against containing block), stacking contexts.
  `2026-06-24-html-positioning-design.md`.
- **Overflow clipping** (`pkg/css` `overflow`, `layout.ClipPush/PopKind`): clip to padding box +
  BFC establishment + deferred float interactions. `2026-06-24-html-overflow-design.md`.
- **Full z-index stacking** (`pkg/layout/css/fragment.go`): Appendix E bands (negative-z behind
  in-flow, then auto/0 doc order, then positive), relative clip-escape (sub-project 6b).
  `2026-06-25-html-zindex-design.md`.
- **CSS 2.1 §17 tables** (`pkg/layout/css/table.go`+`tableborder.go`+`tablefix.go`+`measure.go`):
  anonymous-table fixup, grid model, fixed + auto column-width solve, colspan/rowspan,
  `vertical-align`, captions, `<col>`/`<colgroup>`, both `border-collapse` models.
  `2026-06-25-html-tables-design.md`.
- **Web fonts** (`pkg/css/fontface.go`, `pkg/font/sfnt.go`/`woff1.go`/`woff2*.go`,
  `pkg/layout/font`): `@font-face` capture, WOFF1/WOFF2 decode (incl. glyf/loca transform), `local()`
  via `DiskFontProvider`, family-fallback-list resolution. `2026-06-26-html-webfonts-design.md`.
- **Flexbox** (single-line; `pkg/layout/css/flex.go`+`flexfix.go`): axis-abstracted layout,
  §9.7 flexible-length resolution, `justify-content`/`align-items`/`align-self`, `inline-flex`.
  `2026-06-26-html-flexbox-design.md`.
- **CSS Grid** (explicit grid; `pkg/layout/css/grid.go`+`grid_track.go`+`grid_place.go`+`gridfix.go`
  +`baseline.go`): §11 track-sizing + §8 placement (spans, named areas, auto-placement sparse/dense),
  item + content-distribution alignment, `inline-grid`, cross-cutting baseline backport (grid + flex
  + table cells). `2026-06-26-html-grid-design.md`.
- **`OpenURL` + HTTP loader** (`pkg/resource/http.go`): fetch HTML over HTTP(S), resolve relative
  refs, `data:` URIs, URL-userinfo Basic auth (redacted from logs). `2026-06-28-html-openurl-design.md`.
- **Pagination** (`pkg/layout/css/paginate.go`, `WithPageSize`): fixed-height page fragmentation,
  break cascade, between-block + forced breaks, per-page distribution of relative/abs/fixed/float +
  html/body border. `2026-06-28-html-pagination-design.md`,
  `2026-06-28-html-pagination-fidelity-bundle-design.md`.
- **CSS Paged Media** (`pkg/css/page.go`+`pagesize.go`, `pkg/layout/css/pagemodel.go`+
  `fragmentpage.go`+`marginbox.go`, `WithDefaultPaged`): `@page` size/margins/named/pseudo + 16
  margin boxes, `break-inside`, widows/orphans via mid-block line fragmentation, running
  headers/footers with page counters, `@page marks`/`bleed`, `string-set`/`string()`,
  `position: running()`/`content: element()`, named-page multi-width reflow.
  `2026-06-30-html-paged-media-design.md` (+ sub-plans under `docs/superpowers/plans/2026-06-30-*`).
- **`white-space`** (`pkg/css` + `pkg/layout/inline`): normal/nowrap/pre/pre-wrap/pre-line + tab
  stops. `2026-06-29-html-white-space-design.md`.
- **List markers + CSS counters** (`pkg/css/counter_format.go`, `pkg/layout/css/counters.go`,
  `pkg/font/bullet.go`): `list-style-*`, `counter-reset`/`-increment`/`-set`, `content: counter()`;
  synthetic bullet outlines. `2026-06-29-html-lists-counters-design.md`.
- **`background-image`** (`pkg/css/background.go`, `pkg/layout/css/background.go` + paint):
  `url(...)`, `-repeat`/`-position`/`-size`/`-origin`/`-clip`. `2026-06-30-html-background-image-design.md`.
- **Link pseudo-classes + `text-decoration: underline`** (`pkg/css/selector.go`, `pkg/html/ua.go`):
  `:link`/`:visited` + general pseudo-class parsing. `2026-06-30-html-link-pseudo-classes-design.md`.
- **Legacy presentational-attribute hints** (`pkg/css/hints.go`): `bgcolor`/`align`/`valign`/
  `width`/`cellspacing`/`cellpadding`/`border`/`<font>`/`<ol type/start>`/`<body link>`… mapped to
  CSS below author rules (HN renders with its bgcolor). `2026-06-30-html-presentational-attributes-design.md`.
- **Static form controls** (`pkg/layout/css/control.go`): `<input>`/`<button>`/`<textarea>`/
  `<select>` as static native widgets (classic chrome, non-interactive).
  `2026-06-29-html-forms-design.md`.
- **End-to-end "specimen" showcase** (`testdata/htmldoc/`, `htmldoc-*` goldens): one multi-file doc
  exercising every HTML/CSS/image slice, served over loopback HTTP via `OpenURL` + `WithPageSize`.

**DOCX frontend** (`OpenDOCX`/`OpenDOCXBytes`, `docx-*` goldens):

- **Parse + cascade** (`pkg/docx`, `pkg/docx/style`): ZIP/OPC container, `document.xml`
  (paragraphs, runs, `w:t`/`w:br`/`w:tab`), run + paragraph properties, section geometry
  (`w:sectPr`), the full `docDefaults → basedOn → direct` cascade.
- **CSS-engine convergence** (`pkg/docx/cssbox`): DOCX lowers directly to `cssbox` + `ComputedStyle`
  and runs through the shared CSS engine (page geometry as a synthesized `@page` stylesheet); the old
  flat model/engine are deleted. `2026-07-02-docx-cssbox-convergence-design.md`.
- **DOCX fidelity** (lists/numbering, tables, images, headers/footers + multi-section — most reuse
  the CSS engine's existing vocabulary via lowering). `2026-07-02-docx-fidelity-design.md`.

**HTML/DOCX → PDF writer** (`pkg/render/pdfwrite`, `ConvertHTMLToPDF`/`WritePDF`):

- A second `render.Device` that emits a real PDF with **selectable/searchable text** (Type0/
  Identity-H CIDFontType2 with glyf-subsetted `/FontFile2` for TrueType, simple `/Type1` with
  `/FontFile` for the bundled substitutes; `/ToUnicode` on every face). Concurrent per-band assembly,
  deterministic output, `@media print` capture (`pkg/css/media.go`). Byte-identical for the raster
  corpus (the new `DrawGlyph` seam rasterizes via the outline). `2026-06-26-html-to-pdf-writer-design.md`.

**HTML/DOCX → Markdown & plain text** (`pkg/render/markdown`, `ConvertHTMLToMarkdown`/`WriteMarkdown`
+ `WriteText`, CLI `tomd`):

- A conversion backend that walks the shared `cssbox` tree (not the paint seam — it needs structure,
  not glyphs), so one walker serves HTML and DOCX. Small additive semantic annotations on `cssbox.Box`
  (`SemTag`/`HeadingLvl`/`Href`) captured by both frontends carry the facts computed style drops
  (heading level, link URLs, DOCX style identity); layout/raster/PDF ignore them (byte-identical).
  Emits GFM: headings, bold/italic/strikethrough/code, links, images, blockquotes, fenced code,
  nested + task lists, thematic breaks, and **high-fidelity pipe tables** (colspan/rowspan expanded by
  content duplication, alignment, caption). `2026-07-07-html-docx-markdown-design.md`.

**PDF → Markdown & HTML** (`pkg/pdf/extract`, `pkg/render/htmlwrite`, `ConvertPDFToMarkdown`/
`ConvertPDFToHTML` + `WriteHTML`, CLI `tomd <pdf>` / `tohtml`):

- Structure recovery from a PDF's positioned glyphs + vector paths. The content interpreter gains
  optional, paint-neutral capture sinks (`content.Options.TextSink`/`GraphicsSink`, nil =
  byte-identical); `pkg/pdf/extract` reconstructs words→lines→**XY-cut** reading-order blocks (columns
  handled) + **automatic table recognition** (lattice from ruling lines + stream from whitespace,
  auto-selected), lowering to a synthetic `cssbox` tree the Markdown writer reuses. A new
  `pkg/render/htmlwrite` serializes `cssbox`→HTML (native `colspan`/`rowspan`). PDF `Document`
  satisfies `reflowTree` via lazy extraction. ToUnicode CMaps (Type0/CID text), font weight/slant, and
  scanned-PDF OCR are follow-ups. `2026-07-08-pdf-to-html-markdown-design.md`.

### TODO (roughly priority order)

Each item lands with a new fixture/test + showcase entry in the same PR. Unsupported cases already
degrade gracefully; a TODO becoming supported just turns that skip into real output.

1. **Remaining scan filters** — JBIG2 and JPX/JPEG2000 (`pkg/pdf/filter/filter.go`, `ErrUnsupported`).
2. **Shadings / gradients (remaining)** — tiling patterns (PatternType 1; skipped + logged),
   higher-fidelity Coons/tensor patches (Types 6/7, currently bilinear-corner), luminosity soft
   masks (`/SMask` in ExtGState), and transparency groups.
3. **Encryption follow-ups** — non-empty user/owner passwords (no password API today), per-stream
   `/Crypt` overrides, `/Perms` validation.
4. **Base-14 weights & symbol fonts** — bold/italic/oblique map to the regular face (affects DOCX
   and PDF); Symbol/ZapfDingbats have no substitute. Bundle weighted faces + symbol look-alikes;
   ideally AFM widths for exact base-14 metrics. This also fixes DOCX bold/italic fidelity.
5. **DOCX embedded fonts** — de-obfuscate `word/fonts/*` (also improves bold/italic fidelity).
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

Out-of-scope, don't gold-plate without a concrete need: **EPUB** (`OpenEPUB` / ebook reading — the
HTML pipeline is the reflow target), full ICC color management, JavaScript, interactive AcroForm
widget rendering, tagged-PDF/accessibility, digital-signature verification.
