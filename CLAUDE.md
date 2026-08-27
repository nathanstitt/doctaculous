# Doctaculous

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/HTML, and rasterize pages to images. The core pipeline (parse → interpret →
raster) is working end-to-end and renders real-world PDFs, DOCX, HTML, EPUB, and SVG faithfully; see
FEATURES.md for the full inventory of what has shipped, and "Status & roadmap" at the bottom for what
is next.

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
  feature). Design rationale lives in the commit and PR history — read `git log` for the area you
  are extending; each sub-project's PR body carries the reasoning and the alternatives rejected.

## Non-negotiable constraints

- **Pure Go. No CGo, no native bindings, no WASM engines.** No PDFium / MuPDF / Poppler.
- **MIT licensed.** Every dependency must be MIT/BSD/Apache and pure Go. No GPL/AGPL.
- Approved deps: `golang.org/x/image/*` (BSD), `github.com/srwiley/rasterx` (BSD),
  `github.com/benoitkugler/textlayout` (font parsing, plus its pure-Go harfbuzz port for Arabic
  contextual shaping and `unicodedata` for bracket mirroring), `golang.org/x/net/html` (HTML parse),
  `golang.org/x/text` (BSD — `unicode/bidi`, a complete UAX#9 incl. bracket pairs; promoted from
  indirect when inline bidi reordering landed, no new module),
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
- **SVG corpus (`testdata/svg/resvg`, 848 fixtures)**: curated from resvg-test-suite (MIT, pinned
  commit), one feature per file, with our own committed goldens under
  `pkg/doctaculous/testdata/golden/svg-resvg/`. Provenance and every curation/exclusion decision live
  in that directory's README — read it before adding or excluding a fixture.
  - **The `.png` beside each fixture is NOT resvg's output.** The suite's README says `results.csv`
    holds "results of manual testing via `tools/vdiff`", and `vdiff` renders each SVG *live* across
    Chrome, QtSvg, Inkscape, librsvg and Batik for a human to compare — so those PNGs are curated
    expected-result images. Verified: `shapes/circle/simple-case.png`, a plain circle, differs from a
    fresh resvg render by 0.66% along the antialiased edge alone. **Do not try to "refresh" them from
    a resvg build** — that is not a meaningful operation, and one attempt reported 45% of the corpus
    as stale before the premise was caught.
  - They are still the right thing to eyeball against: compare *intent and geometry*, not pixels,
    since our bundled fonts and rasterizer differ. Vendor a fixture only when its geometric claim
    stays verifiable under font substitution; otherwise skip it with a recorded reason rather than
    committing a golden that locks in a substituted rendering as correct.
  - When a fidelity question turns on what resvg *actually does*, build it and look
    (`~/code/vendor/resvg`, `cargo` works out of the box). Reading `usvg`'s resolved tree settled in
    minutes a cyclic-mask question that days of pixel archaeology could not — and revealed that one
    committed reference was simply out of step with current resvg.
- **Golden-image tests** (`pkg/render/raster/golden_test.go`, plus the `pkg/doctaculous` `docx-*` /
  `html-*` / `htmldoc-*` goldens): render at 72 DPI, compare to committed PNGs with a per-pixel
  tolerance (±4/channel) + 0.2% differing-pixel budget. Regenerate an intentional render change with
  `go test ./pkg/render/raster -run TestGolden -update`, then **eyeball every changed PNG in the PR**
  — an unexplained golden diff is a regression. Goldens are committed; the fixtures that produce them
  are generated, so the chain stays hermetic. HTML/DOCX also carry WPT-style reftests.
  - **`-update` rewriting a golden does NOT mean it was stale.** A handful of goldens
    (`docx-model-specimen`, `md-specimen`, and some masking ones) get new BYTES on every
    `-update` while remaining pixel-identical or well inside tolerance — PNG encoding is not
    byte-stable across runs even when the rendered pixels are. Rendering itself IS
    deterministic (verified: repeated renders hash identically). Judge staleness by the
    tolerance check, never by `git status`.
  - **Compare PNGs by decoded pixels, not raw bytes.** These files use per-row PNG filters
    (Paeth/Average/Sub/Up), so a byte-level diff reads filter RESIDUALS, where one changed
    pixel perturbs every byte after it — a 1/255 difference can look like 7% of the file.
    Use the harness's own `compareImages`, or decode properly, before concluding anything.
- **Benchmarks**: `BenchmarkRasterizePages` proves goroutine speedup vs. `--workers 1`. Run the
  race detector (`go test -race ./...`) since concurrency is core.
- Tests must be hermetic and fast: no network (HTTP paths use `net/http/httptest` loopback).
- New feature ⇒ new fixture + test + showcase entry in the same PR. Unsupported features must
  degrade gracefully (skip + debug log / typed error), and that behavior must be covered by a test.

## Status & roadmap

The full inventory of shipped features lives in **[FEATURES.md](FEATURES.md)** — keep it current:
every feature that lands gets a bullet there in the same PR. This section keeps only what is NOT
done yet. The detailed per-item working checklist (with the rationale for each deferral) lives in
**[docs/FIDELITY-BACKLOG.md](docs/FIDELITY-BACKLOG.md)**.

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
8. **CSS selector coverage** (`pkg/css/selector.go`) — the engine supports type, class, id,
   universal, descendant, grouping, and the structural pseudo-classes, but has NO child (`>`),
   adjacent-sibling (`+`), general-sibling (`~`), attribute (`[foo]`, `[foo=bar]`), `:not()`/`:is()`/
   `:where()`, or namespace (`svg|rect`) selectors. `parseOneSelector` splits on whitespace
   (`strings.Fields`), so `>` and `[attr]` cannot be represented. These **fail safe** (a rule with an
   unsupported selector is dropped, never mis-matched). The **silent** half is now CLOSED: a dropped
   selector is recorded on `Stylesheet.Unsupported` and reported warn-once per construct by
   `NewResolver` (HTML/DOCX) and `pkg/svg`'s index (SVG-internal `<style>`) — see FEATURES.md. This
   affects **HTML as much as SVG**, since `pkg/css` is shared. Two resvg fixtures are excluded from
   the SVG corpus for this reason (see `testdata/svg/resvg/README.md`). Still wanted, roughly in
   value order: `>` (common in hand-authored SVG and real stylesheets), attribute selectors, then the
   sibling combinators. Related and unfixed: `parseOneSelector`'s whitespace split also drops the
   valid spaced `An+B` form (`:nth-last-child(2n + 1)`), which the same parser rework would fix.

**Open fidelity follow-ups** (the engine renders these paths; these are the known approximations —
each degrades gracefully and is documented in the relevant spec):

- **RTL / `direction` / bidi** — **DONE** (backlog A1, five slices): cascade plumbing
  (`text-align: start|end`, `unicode-bidi`, the `dir` attribute); box-level mirroring for tables,
  flex, and grid, retiring all three "laying out LTR" logs; inline bidi reordering (UAX#9 L2 per line
  + L4 bracket mirroring, so Hebrew reads right-to-left); Arabic contextual shaping through harfbuzz,
  so letters connect; and visual→logical PDF extraction, so RTL text extracts in reading order. Noto
  Sans Hebrew + Noto Naskh Arabic (OFL) ship bundled, resolved by per-rune script fallback.
  Remaining approximations: GPOS vertical offsets are not applied (marks sit on the baseline),
  `font-feature-settings` is not plumbed through, nested bidi embeddings deeper than one level
  collapse, and a digit sequence embedded in RTL text reverses with its word on extraction.
- **Multi-line flexbox** — DONE (backlog H1): `flex-wrap`, `align-content`, the cross gap, and
  `flex-flow` all ship; wrapped rows paginate between lines. Remaining flex approximation: a single
  line taller than the page still moves whole rather than splitting its items.
- **Grid** — named-line placement (`[name]` tokens parsed-and-ignored → auto-placement), `subgrid`
  (→ `none`), `auto-fit` empty-track collapse approximate. (The ROW-track "width-proxy" entry was
  stale: row tracks already size from laid-out item heights — see backlog I4.)
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
- **SVG** — **DONE** as an input format (8 PRs: core, styling, paint servers, groups/clip/mask,
  `use`/`symbol`/markers, text, filters, HTML/EPUB integration — see FEATURES.md). Vector-native
  throughout: an SVG reaching PDF via `<img>`, inline markup, `background-image`, or an EPUB cover
  emits real path operators, never a rasterized image (asserted on the emitted PDF, not on pixels).
  Remaining, roughly in value order:
  - **`<textPath>`** (44 corpus fixtures) — needs arc-length parameterization of a `render.Path`
    plus per-glyph tangent frames. `render.Vertices` (PR 5) gives tangents at *vertices*, not at an
    arbitrary distance along a curve, so it is a start and not a solution. Degrades to a straight
    baseline with a log.
  - **`writing-mode`** (25 fixtures) — needs `vhea`/`vmtx` vertical metrics `pkg/font` does not
    parse, plus a vertical advance model; every metric in the engine is horizontal-only. Degrades to
    horizontal with a log.
  - **`preserveAspectRatio` on an EMBEDDED SVG** — `fitSceneTo` scales per-axis, so a CSS box whose
    aspect differs from the document's squashes where a browser re-applies the SVG's own
    `preserveAspectRatio` against the used size and letterboxes. Exact whenever CSS sizing preserved
    the ratio (unsized `<img>`, one axis given, matching box) — the common case. Needs the parsed
    value retained on `svg.Document`; `resolveSize` consumes it into `rootM` and discards it.
  - **Filters not implemented** (each renders the element unfiltered with a log): `feTurbulence`,
    `feConvolveMatrix`, `feDiffuseLighting`/`feSpecularLighting` + light sources, `feMorphology`,
    `feImage`, `feTile`, `feComponentTransfer`, `feDisplacementMap`. `enable-background` is dropped
    outright (removed from the spec, implemented by no browser), as is `<tref>`.
  - **A non-uniform transform rasterizes a filter region at the wrong aspect** — `filterSpace` derives
    a single uniform scale (`pkg/svg/draw/filter.go`); a per-axis filter space threaded through
    `filterM`/`postM` and every primitive's subregion math would fix it. One corpus fixture measures
    2.78% differing pixels; the tolerance was NOT widened to hide it.
  - **`letter-spacing`/`word-spacing` are SVG-only** — implemented in `pkg/svg/style.go`, absent from
    `ComputedStyle`, so an SVG-internal declaration works but inheriting one from an enclosing HTML
    ancestor does not. Wiring them into reflow means facing line-breaking and justification.
- **CSS `filter:`** — DONE for HTML boxes (all ten shorthand functions). Remaining: `backdrop-filter`
  (needs the backdrop, not the element's own pixels — a different mechanism); native PDF filter
  emulation via soft masks (PDF output currently paints filtered content **unfiltered**, keeping it
  vector rather than rasterizing a page region). Two degradations are **silent** because
  `pkg/layout/paint` has no logger — `PaintPage` takes only a Device, a Page, and a Matrix, unlike
  the SVG side whose Renderer carries a `Logf`: the over-cap/off-device region (`maxCSSFilterPixels`
  is 4M pixels, which a 300 DPI A4 page at ~8.7M exceeds, so a full-page filter degrades at print
  resolution) and the 4-deep nesting cap. Threading a logger through `PaintPage` is a small, contained
  fix worth doing. Also: the five colour-matrix helpers are DUPLICATED between
  `pkg/layout/paint/cssfilter.go` and `pkg/svg/filterfunc.go` — they agree today (verified
  byte-identical across all ten functions) by being kept in step, not by construction; moving them to
  a shared package would make that structural.
- **`rem` resolves against the element's font size, not the root** — `pkg/css/value.go` folds `rem`
  into `UnitEm` at parse time. The CSS `filter` property resolves it correctly (its lengths resolve
  at paint time, where the root is reachable), so `rem` is currently right for `filter` and wrong for
  every other property. Modelling it properly needs a distinct `UnitRem` carried to where the root
  font size is known, which every `Length` consumer would have to resolve.

Out-of-scope, don't gold-plate without a concrete need: full ICC color management, JavaScript,
interactive AcroForm widget rendering, tagged-PDF/accessibility, digital-signature verification.
(EPUB — previously out of scope — landed as an input format when the any⇄any conversion goal
made it a requirement; DRM-protected books stay refused by design.)
