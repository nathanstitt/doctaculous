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

**Reflowable documents** (DOCX today; HTML/EPUB next) share a second pipeline that meets the PDF
pipeline at `render.Device`. During the HTML-rendering program there are **two box models**: the
existing **flat** model (`pkg/layout/box.Document` — DOCX's `pkg/docx` parse → `pkg/docx/style`
cascade → `pkg/docx/lower` → `pkg/layout` reflow engine → `pkg/layout/paint`), and a **recursive,
format-neutral** model (`pkg/layout/cssbox`) that the CSS layout engine (`pkg/layout/css`) consumes. A
reflow frontend is a parse+lower step producing one of these box models (DOCX → `box.Document` today;
HTML → `cssbox` via `pkg/html` + `pkg/css` + `pkg/layout/css`); it never touches line-breaking or
pagination. Both engines now share one **inline-layout core** (`pkg/layout/inline`: shaping,
greedy line-breaking, alignment/justification math), so the flat engine and the CSS inline formatting
context use the same shaper and breaker. These converge late: a dedicated sub-project re-points DOCX
lowering onto `cssbox` and retires the flat model, so one recursive engine drives every reflow format.
Font outlines for both pipelines come from `pkg/font` (`pkg/font/family.go` exposes named-family faces
for reflow); `pkg/layout/font` caches them.

The `Device` interface is the seam: the interpreter (PDF) and the reflow engine (DOCX/HTML/EPUB)
must stay backend-agnostic so we can add an SVG/other backend later without touching parsing,
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
  system (`pkg/pdf/content/shading.go`). Also **mesh shadings** (Types 4–7,
  `pkg/render/raster/shading_mesh.go`): free-form Gouraud triangles (Type 4) and lattice-form
  (Type 5) are decoded from the packed bit stream and Gouraud-filled exactly; Coons (Type 6) and
  tensor (Type 7) patches are tessellated to a fixed grid (a bilinear-corner approximation of the
  patch surface). Malformed mesh streams degrade gracefully (no panic, skip + log). Tiling patterns
  (PatternType 1) remain pending (see TODO).
- **Images**: raw samples in DeviceGray / DeviceRGB / DeviceCMYK / Indexed / ICCBased (by `/N`) at
  1/2/4/8/16 bpc, baseline JPEG (DCTDecode), grayscale `/SMask` soft-mask alpha, 1-bit `/ImageMask`
  stencils painted in the fill color, `/Decode` arrays, and inline images (`BI`/`ID`/`EI`)
  (`pkg/render/raster/image.go`, `page.go`).
- **Page geometry**: `/Rotate` (0/90/180/270), MediaBox/CropBox.
- **Concurrency**: bounded worker pool sized to `GOMAXPROCS`; per-page recover so one bad page can't
  kill a batch.
- **Reflowable documents — DOCX** (covered by `testdata/gen/docx` fixtures + `pkg/doctaculous`
  `docx-*` golden images): open a `.docx` via `OpenDOCX`/`OpenDOCXBytes` and rasterize its pages
  through the shared reflow engine. Parsing (`pkg/docx`): ZIP/OPC container, relationship + main-part
  resolution, `document.xml` (paragraphs, runs, `w:t` with `xml:space`, `w:br`, `w:tab`), run
  properties (bold/italic/underline, `w:sz`, `w:color`, `w:rFonts`), paragraph properties
  (`w:jc`, `w:spacing`, `w:ind`, `w:pStyle`, `w:pageBreakBefore`), and section geometry
  (`w:sectPr` pgSz/pgMar). Styles (`pkg/docx/style`): the full `docDefaults → basedOn chain →
  direct` cascade with a cycle guard. Layout (`pkg/layout`): greedy line-breaking, vertical flow,
  and pagination on overflow with real font metrics; line height = font metrics × 1.15 for
  `lineRule=auto`; left/right/center/justify alignment; first-line/left/right indents. Fonts:
  named families resolve to the bundled base-14 substitutes (`pkg/font/family.go`, Office defaults
  like Calibri/Cambria aliased), glyphs resolved by name then cmap. Single section; one engine
  drives the same `render.Device`/raster as PDF.
- **CSS engine — parse + cascade** (`pkg/css`, unit-tested in isolation; no layout/rendering yet):
  a hand-written, dependency-free CSS tokenizer + parser (rules, declarations, `!important`, at-rule
  skipping, comment stripping), selector matching (type / universal / class / id / descendant /
  grouping) with specificity, and the full cascade (specificity + source order + inheritance +
  `!important` + inline `style=""`) producing a `ComputedStyle` for the normal-flow property subset
  (display, color/background, font-*, line-height, text-align, margin/padding/border, width/height).
  This is the first landed slice of the HTML reflow frontend (sub-project 1 of the HTML-rendering
  roadmap); it is consumed by box generation next. Unsupported selectors/properties degrade
  gracefully (skipped). See `docs/superpowers/specs/2026-06-23-html-rendering-design.md`.
- **HTML frontend — parse + box generation** (`pkg/html`, `pkg/layout/cssbox`, `pkg/layout/css`,
  `pkg/resource`; unit-tested by structural assertions, no rendering yet): parse HTML via
  `golang.org/x/net/html` into an owned DOM implementing the `pkg/css` `Node` interface, collect
  `<style>`/`<link>`/inline `style=""`, and generate a recursive `cssbox` tree by driving the CSS
  cascade per element. Includes a minimal user-agent default stylesheet, cascaded below author rules
  via a new origin-aware cascade in `pkg/css` (`Origin`/`OriginSheet`, plus `ComputeRoot` for the root
  base); anonymous-box fixups (inline-in-block wrapping and block-in-inline splitting); whitespace
  collapsing/stripping; and `display:none` pruning. Recognized-but-unimplemented display modes
  (flex/grid/table) are preserved on the box (the layout engine does the block fallback later);
  genuinely unknown display values normalize to block. `<img>` becomes a replaced leaf box (no
  decoding yet). External `<link>` stylesheets resolve through a `pkg/resource.ResourceLoader` seam
  with hermetic in-memory/testdata loaders (no HTTP yet). This is the second landed slice of the HTML
  reflow frontend (sub-project 2). See
  `docs/superpowers/specs/2026-06-23-html-box-generation-design.md`.
- **HTML rendering — block + inline normal flow** (`pkg/layout/inline`, `pkg/layout/css`, extended
  `pkg/layout/paint`, `pkg/doctaculous` `OpenHTML`; covered by box/fragment-position assertions, the
  `html-*` golden images, and WPT-style normal-flow reftests): the CSS layout engine turns a `cssbox`
  tree into a positioned fragment tree and paints it, so **`OpenHTML(path)` / `OpenHTMLBytes(data,
  opts...)` render a real page** (single tall image at a fixed viewport, default 1280px; returns the
  same `*Document` the toolkit rasterizes, reusing `reflowRenderer`). This is sub-project 3 of the
  HTML-rendering roadmap (the first-pixels milestone). Pieces: a shared **inline-layout core**
  (`pkg/layout/inline`: `Shape`/`Break`/`Place` — styled runs → shaped glyphs → greedy lines →
  alignment math) extracted from the flat DOCX engine, which now delegates to it (DOCX goldens
  unchanged = the extraction is behavior-preserving); the **block formatting context**
  (`pkg/layout/css/block.go`: the box model — width incl. `auto`/`%`, `box-sizing`, `min/max-width`,
  padding, borders, backgrounds, em→pt and %→pt resolution, **vertical margin collapsing** for
  adjacent siblings + parent/first-child + parent/last-child through zero border/padding, auto/fixed
  height); the **inline formatting context** (`pkg/layout/css/inline.go`: text shaping/breaking,
  `text-align`, `line-height`, and inline-block/replaced atoms); the **fragment tree**
  (`pkg/layout/css/fragment.go`, flattened to `layout.Item`); and **paint** extended with backgrounds
  and 4-side styled borders (solid/dashed/dotted/double). Two enabling additions: **CSS shorthand
  expansion** in `pkg/css` (`margin`/`padding`/`border`/`background` → longhands, so real pages style
  boxes) and **`min/max-width`/`-height` + `box-sizing`** on `ComputedStyle`; box generation now treats
  **inline-block as inline-level outer** so it flows in the IFC. Unsupported layout modes (flex/grid/
  table) fall back to block normal flow; the engine recovers at the page boundary (never panics). See
  `docs/superpowers/specs/2026-06-23-html-block-inline-flow-design.md`.
- **HTML rendering — replaced content + images** (`pkg/layout/css/image.go` + `replaced.go`, extended
  `pkg/layout/css/inline.go`/`block.go`/`fragment.go`, `pkg/layout/inline`, `pkg/layout/page.go` +
  `pkg/layout/paint`, `pkg/css` `object-fit`; covered by fragment-geometry assertions, the
  `html-image-*` golden images, an `img-vs-div` WPT reftest, and paint/inline unit tests): an `<img>`
  now **decodes → sizes → paints**. PNG/JPEG/GIF are decoded (stdlib, no new dep) at layout time via
  the existing `pkg/resource.ResourceLoader`, cached per-engine (mirroring the face cache, negative
  results included). The CSS replaced-element sizing algorithm (CSS 2.1 §10.3.2/§10.6.2) resolves the
  used size: CSS `width`/`height` win over the presentational `width`/`height` attributes; a single
  specified dimension derives the other from the decoded image's intrinsic aspect ratio; neither uses
  the intrinsic size; each axis is clamped by `min/max-width`/`-height`. The image paints through
  `render.Device.DrawImage` (the same seam the PDF side uses) via a unit-square→content-box matrix, with
  **`object-fit`** (`fill`/`contain`/`cover`/`none`/`scale-down`; `cover`/oversized clip to the content
  box). A replaced box flows as an inline atom (default/inline-block) or a block (`display:block`, where
  `width:auto` uses the intrinsic width, not container fill). Two inline-fidelity additions landed with
  it: **inline-block/replaced horizontal margins** participate in the inline advance, and an atomic
  box's **baseline/line-box ascent** is folded into line metrics separately from text (so a tall image
  drops the baseline below it without the line-height leading multiplier scaling the atom). An
  undecodable/404/missing-`src`/unsupported-format image degrades to a sized placeholder (reserves its
  box, paints nothing) + debug log, never panicking; recovery is at the page boundary. See
  `docs/superpowers/specs/2026-06-24-html-replaced-images-design.md`.
- **HTML rendering — floats + clear** (`pkg/layout/css/floats.go`, extended `block.go`/`inline.go`/
  `fragment.go`, `pkg/layout/inline` `BreakNext`, `pkg/css` `float`/`clear`; covered by float-context
  geometry unit tests, fragment-geometry assertions, the `html-float-figure` golden, and the
  `float-left` WPT reftest): `float:left/right` takes a box out of flow to the containing-block edge
  (positioned by its margin box); in-flow line boxes and block content narrow around floats via a
  per-BFC `floatContext` (`leftEdge`/`rightEdge`/`place`/`clearY`) the block stacker and inline
  formatting context query per vertical band (clamped to each box's own content box, so a non-BFC box
  narrower than its BFC still wraps at its own width). Multiple floats **stack and wrap to a new row**;
  `clear:left/right/both` drops a box below matching floats. Floats establish their own BFC and **paint
  in their own CSS layer** (CSS 2.1 Appendix E: in-flow block decorations → floats → in-flow inline
  content) via a phase-split `AppendItems`; a nested BFC paints atomically. The shared inline core
  stays float-agnostic (one additive `BreakNext` primitive — one greedy line at a width; DOCX
  unchanged). The float context is queried in a BFC-root-relative band frame (`bandOriginY`); the
  shift helpers carry a fragment's `Floats` so a repositioned nested BFC moves its floats. Degrades
  gracefully: an overflow-wide float overflows the edge (no spin), `float:auto` width approximates the
  resolved width, and a floated inline-level box is blockified (CSS 9.7). A parent **enclosing** its
  floats' height and a sibling BFC **shortening past** an outer float (the two float interactions a
  non-BFC parent did not yet handle here) landed with the overflow slice (an `overflow≠visible` box
  establishes the BFC both need — see the overflow bullet below). See
  `docs/superpowers/specs/2026-06-24-html-floats-design.md`.
- **HTML rendering — positioning (relative / absolute / fixed)** (`pkg/layout/css/positioning.go`,
  extended `block.go`/`fragment.go`/`build.go`, `pkg/css` `position`/`top`/`right`/`bottom`/`left`/
  `z-index`; covered by positioning-geometry unit tests, fragment-geometry + flag-combination + paint-
  coordinate assertions, the `html-position-relative`/`html-position-absolute` goldens, and the
  `abs-pos`/`relative-offset` WPT reftests): `position:relative` offsets a box from its normal-flow
  position **at paint time** (flow and siblings unchanged; the box reserves its un-offset space, and
  `top`/`left` win over `bottom`/`right` per CSS 9.4.3). `position:absolute`/`fixed` take a box **out of
  flow** and position it against its containing block — the nearest positioned ancestor's content box for
  `absolute`, the page (viewport, in the single-tall-page model) for `fixed` or when there is no
  positioned ancestor — resolved in a **second pass** (`resolveAbsolute`) once containing-block geometry
  is final (`absRect`/`relativeOffset` in `positioning.go` carry the geometry). Positioned boxes paint in
  their **own layer** after in-flow content: `AppendItems` is generalized from the float phase-split into
  a **minimal stacking pass** (decorations → floats → in-flow content → positioned layer, in document
  order), and a relative offset is applied as a translate over the fragment's flattened item range (so an
  abs-pos descendant of a relative box rides the same shift). A positioned box establishes a stacking
  context; an abs/fixed box also establishes a BFC. Box generation maps `position` (`positionOf`),
  forces `float:none` under `absolute`/`fixed` (CSS 9.7), and blockifies a positioned inline-level box
  (`applyBlockify`). The shared inline core is **untouched** (positioning needs no inline primitive).
  Degrades gracefully: `z-index` is **now honored** — full Appendix E negative/numeric ordering within a
  stacking context (positioned boxes sorted by (z-index, document order); negatives paint behind in-flow
  content) — see the full-z-index-stacking bullet below; an all-`auto`-offset abs box uses
  a static-position approximation (containing-block top-left); `margin:auto` and abs `width:auto`
  shrink-to-fit stay approximate; and `position:relative` on any **inline-level box** (text-only inline
  or an inline-block/replaced atom) is a no-op — relative offset takes effect only on block-level boxes.
  Non-positioned pages stay byte-identical (the existing goldens/reftests are unchanged). See
  `docs/superpowers/specs/2026-06-24-html-positioning-design.md`.
- **HTML rendering — overflow clipping + deferred float interactions** (`pkg/css` `overflow`, extended
  `pkg/layout/css/block.go`/`floats.go`/`fragment.go`, `pkg/layout` `ClipPushKind`/`ClipPopKind`,
  `pkg/layout/paint`; covered by clip-geometry + adversarial clip-vs-stacking + flag-combination + float
  unit tests, a paint raster clip test, the `html-overflow-hidden`/`html-float-row` goldens, and the
  `overflow-hidden`/`float-row` WPT reftests): `overflow: hidden/scroll/auto` clips a box's content to its
  **padding box**, and **`overflow≠visible` establishes a BFC** (the trigger for the two float
  interactions below). Clipping is expressed as two flat-stream `layout.Item` kinds
  (`ClipPushKind`/`ClipPopKind`) that `Fragment.AppendItems` brackets a clipping fragment's contents with
  (own background/border paint **outside** the bracket; CB-owned abs-pos descendants inside via a parallel
  `PositionedInfo.CBOwned` flag — since renamed from `PositionedClip`, now also carrying the clip-escape
  `ClipChain`; a positioned descendant whose containing block is **outside** the box paints after
  `ClipPop`, unclipped — CSS abs-pos clipping); `PaintPage` maps them onto the painter's existing
  `Save`/`PushClip`/`Restore` clip stack (the same one `object-fit:cover` uses), so clips **nest**.
  `scroll`/`auto` clip exactly like `hidden` (no scroll position or scrollbar chrome in the single-tall-page
  model; logged). Two float interactions land with it: **float-height enclosure** — a BFC box (incl.
  `overflow:hidden`) grows to enclose its floats (`floatContext.maxBottom()` folded into an auto-height BFC
  box's content height, CSS 10.6.7 — the `overflow:hidden` "clearfix"; restores the `float-row`
  golden/reftest 5a had to drop); and **sibling-BFC float avoidance** — a BFC box laid out next to an outer
  float shifts/narrows its border box past the float band, or drops below it when the band is too narrow
  (CSS 9.5). Degrades gracefully: a **`position:relative` descendant of a *non-positioned* `overflow:hidden`
  box is now clipped** to that box (the clip rect rides the descendant's bubble to the ancestor's positioned
  layer as a `PositionedInfo.ClipChain`; see the full-z-index-stacking bullet) — but two narrower clip-escape
  sub-cases remain deferred: an **`absolute`/`fixed` descendant** whose containing block lies *beyond* an
  intervening `overflow:hidden` box still paints unclipped by that box (it needs clip-ancestor threading
  through layout — deferred to a follow-up 6b), and a **`position:relative` descendant of a *positioned*
  `overflow:hidden` box** paints unclipped in the escaped band (it lands on the box's own positioned layer
  with `CBOwned=false`; pre-existing 5c behavior, still deferred). A clip chain captured **inside a float**
  is not re-translated by `placeFloat` (rare; logged). `overflow-x`/`overflow-y` are not modeled (single
  shorthand only). Non-overflow pages stay byte-identical; the shared inline core is **untouched**. See
  `docs/superpowers/specs/2026-06-24-html-overflow-design.md`.
- **HTML rendering — full z-index stacking** (`pkg/layout/css/fragment.go` sort/bands, `block.go`
  clip-chain bubble; covered by `zindex_layout_test.go` item-stream order tests, the `html-zindex-*`
  goldens, and the `zindex-negative`/`zindex-order`/`relative-clip-escape` WPT reftests): the positioned
  layer is z-sorted into CSS 2.1 Appendix E bands — **negative-z paints behind in-flow content**, z:auto/0
  in document order, positive-z last — via a stable `(z-index, document-order)` sort
  (`sortedPositioned`/`appendBand`, `sort.SliceStable` over a fresh local copy so the shared fragment tree
  stays read-only). All-`auto` pages are **byte-identical** to the prior document-order pass (the empty-band
  identity), so the whole existing corpus is unchanged. Folds in the **relative clip-escape** fix: a
  `position:relative` descendant of a *non-positioned* `overflow:hidden` box is clipped to that box even
  though it paints in an ancestor's positioned layer (the clip rect rides the descendant's bubble as a
  `PositionedInfo.ClipChain`, bracketing its item range in `appendBand`). The `Fragment` now retains its
  source `cssbox.Box` (the z-index source, read — never mutated — at flatten time; motivated by future
  SPA-snapshot re-flow). Deferred to **6b**: the *absolute/fixed* intervening-clip sub-case (needs new
  clip-ancestor threading through layout) and the *positioned-clip-box* relative-escape sub-case (paints in
  the escaped band); a float-internal clip chain is not re-translated (logged). See
  `docs/superpowers/specs/2026-06-25-html-zindex-design.md`.

### TODO (roughly priority order — pick these up next)

Each item should land with a new fixture/test in the same PR (see Testing). Unsupported cases must
already degrade gracefully (skip + debug log / typed error); a TODO becoming supported just turns
that skip into real output.

1. **Remaining scan filters** — JBIG2 and JPX/JPEG2000 (CCITTFax is done). Currently
   `ErrUnsupported` (`pkg/pdf/filter/filter.go`).
2. **Shadings / gradients (remaining)** — **tiling patterns** (PatternType 1; currently skipped +
   logged) and higher-fidelity **Coons/tensor patches** (Types 6/7 are tessellated with a bilinear
   corner approximation — evaluating the actual bicubic boundary would improve curved patches). The
   `sh` operator with axial/radial/function-based shadings, the PDF Function evaluator
   (Types 0/2/3/4), shading patterns (PatternType 2) via `scn`, and mesh shadings (Types 4–7) are
   done. Also **luminosity soft masks** (`/SMask` in ExtGState) and **transparency groups**.
3. **Encryption follow-ups** — non-empty user/owner passwords (no password API today), per-stream
   `/Crypt` filter overrides, `/Perms` validation. Empty-password Standard handler is done.
4. **Base-14 weights & symbol fonts** — bold/italic/oblique currently map to the regular face (now
   affecting DOCX rendering too, not just PDF); Symbol and ZapfDingbats have no substitute (skipped).
   Bundle weighted faces + symbol look-alikes, and ideally standard AFM widths for exact base-14
   metrics.
5. **DOCX features (reflow frontend)** — each a new `testdata/gen/docx` fixture + golden in the same
   PR; add new box-model vocabulary to `pkg/layout/box` (engine track) before the DOCX frontend
   emits it, so HTML/EPUB get it for free. In rough order: **lists/numbering** (`numbering.xml`,
   per-level counters, marker glyphs), **tables** (`w:tbl`, grid + column-width solve, spans, cell
   content recursion — the biggest engine addition), **images** (`w:drawing`→`a:blip`→media,
   PNG/JPEG decode, EMU placement → `dev.DrawImage`), **headers/footers + multi-section** (margin-band
   content, per-section geometry), and **embedded fonts** (de-obfuscate `word/fonts/*`, which also
   fixes bold/italic fidelity).
6. **HTML rendering — remaining slices** (the CSS parse+cascade, box generation, block+inline
   normal-flow layout/paint with `OpenHTML`, replaced content + images, **floats + clear**, and
   **positioning** (relative/absolute/fixed) are done — see the Done section). Roughly in order, each a
   parse/layout slice with its own fixtures + golden/WPT tests: **the abs/fixed intervening-clip escape**
   (sub-project 6b — full z-index stacking and the *relative* clip-escape landed; the remaining gap is an
   `absolute`/`fixed` descendant whose containing block is an ancestor *beyond* an intervening
   `overflow:hidden` box, which still paints unclipped by that box — the `ClipChain` flatten machinery
   exists, but capturing the chain for an abs box needs clip-ancestor threading through layout that the
   relative path did not; the *positioned-clip-box* relative-escape sub-case is grouped here too; see
   `docs/superpowers/HANDOVER-subproject-6b-abs-clip-escape.md`); **tables**; **web fonts**
   (`@font-face` + WOFF/WOFF2); **flexbox** then **grid** (today
   flex/grid/table fall back to block normal flow); **`OpenURL` + the HTTP `ResourceLoader`** (network
   fetching behind the existing seam, which currently has only hermetic loaders — also serves remote
   `<img src>` URLs); **pagination / CSS paged media** (the default stays a single tall image); and
   **EPUB** (`OpenEPUB`, ZIP + OPF spine reusing the HTML frontend per chapter). Positioning fidelity
   follow-ups within the existing engine: the **precise static-position solve** for an all-`auto`-offset
   abs box (today approximates to the containing block's top-left), abs `width:auto` **shrink-to-fit**
   (today fills the containing block), abs `margin:auto` centering, a percentage `top`/`bottom` against
   an auto-height containing block, a `bottom`-only auto-height abs box (positioned against a provisional
   height today), and `position:relative` on a **text-only inline box** (a no-op today — needs inline-box
   fragments). Replaced-content fidelity follow-ups: `object-position`, the ratio-preserving min/max
   sizing step (CSS 10.4; today min/max clamps per-axis after ratio derivation), a percentage `height`
   basis on replaced elements (today treated as auto), and CSS `background-image` decode. General
   inline/flow follow-ups still open: full `vertical-align` keyword set (only the atom baseline
   mechanics landed), `margin:auto` centering, and the deferred margin-collapse edge cases (empty-block
   collapse-through, clearance, `min-height` interaction).

Out-of-scope, don't gold-plate without a concrete need: full ICC color management, JavaScript,
interactive AcroForm widget rendering, tagged-PDF/accessibility, digital-signature verification.
