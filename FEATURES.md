# Features

The complete inventory of shipped features, validated against a real-world corpus
(`testdata/external/`). Keep this list current as features land: every feature that ships gets a
bullet here in the same PR. Each bullet is a one-line pointer; the detailed design/rationale for a
sub-project is in its `docs/superpowers/specs/` doc (and in git history). What's *next* — the TODO
list and known approximations — lives in [CLAUDE.md → Status & roadmap](CLAUDE.md#status--roadmap).

**PDF pipeline** (covered by `gen.Core` fixtures + golden images):

- **Parsing**: classic xref tables, xref streams (`/Type /XRef`), object streams (`/ObjStm`),
  object-scan rebuild for broken `startxref`.
- **Encryption** (`pkg/pdf/crypt.go`): Standard Security Handler, empty user password — RC4
  (V1/V2), AES-128 (V4/AESV2), AES-256 (V5/R6/AESV3). Real-password docs →
  `ErrEncryptedNeedsPassword`; unsupported handlers → `ErrEncrypted`.
- **Filters**: Flate, LZW, ASCIIHex, ASCII85, RunLength (+ PNG/TIFF predictors), CCITTFax
  (Group 4 / Group 3 1D+2D), DCTDecode (JPEG), JBIG2 (vendored pure-Go Apache-2.0 decoder,
  `pkg/pdf/filter/jbig2`; wired at `decodeImageXObject`). JPX/JPEG2000 pending (`ErrUnsupported`; no
  viable pure-Go decoder). `2026-07-09-jbig2-image-decoding-design.md`.
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
  Heros/Termes, Inconsolata) — with **regular/bold/italic/bold-italic** variants, selected from the
  `/BaseFont` name + descriptor `/Flags` (PDF) or the computed `Style` (reflow). **Installed system
  fonts are the DEFAULT** source for non-embedded fonts — an `OSFontProvider` (`pkg/layout/font`, via
  `adrg/sysfont`, live-scanning the OS font dirs incl. macOS `.ttc` collections) resolves them, with
  the bundled substitutes as the fall-through. Hermetic **bundled-only** mode is an opt-out:
  `--bundled-fonts` (CLI), `RasterOptions.BundledFonts` / `PDFOptions.BundledFonts` /
  `WithBundledFonts()` (library); the golden tests pin it. An explicit
  `RasterOptions.FontProvider` (or reflow `WithSystemFontProvider`) still overrides both.
  `2026-07-08-weighted-base14-fonts-design.md`, `2026-07-08-system-font-loading-design.md`.
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

**Unified conversion core** (`pkg/doctaculous/format.go`+`detect.go`+`open.go`+`convert.go`+
`image_backend.go`, CLI `convert`):

- One `Format` type + capability table (`CanConvert`, typed `ErrUnknownFormat`/
  `ErrUnsupportedFormat`/`ErrSameFormat`); content-first `DetectFormat` (magic → extension hint →
  WHATWG HTML sniff; no UTF-8⇒text fallback). `Open`/`OpenBytes` sniff any supported format (the
  PDF path is byte-identical); `OpenAs`/`OpenBytesAs` skip detection; every opener stamps
  `Document.Format()`. Generic `Convert`/`ConvertFile`/`(*Document).Write` dispatch any valid
  input→output pair (the legacy `ConvertXToY` wrappers are shims, pinned byte-identical);
  same-format conversion is a deliberate `ErrSameFormat` on the generic path only. PNG/JPEG are
  output formats (`WriteImage`/`EncodeImage`; Convert-to-image writes one page, multi-page = CLI
  `%d` fan-out). CLI: `convert <in> <out>` with `--from`/`--to`; all subcommands share one
  detection-based opener (rasterize no longer assumes unknown extensions are PDF; topdf `--print`
  actually applies print media now). A new format lands by flipping its capability bit + one switch
  case in `openDetected`/`Write` — see the sibling contract in
  `2026-07-09-unified-conversion-core-design.md`.

**Markdown + plain-text input** (`pkg/markdown` via goldmark (MIT, pure Go, zero transitive
deps), `pkg/doctaculous/markdown_frontend.go`+`text_frontend.go`):

- `.md` (CommonMark + GFM: tables, strikethrough, task lists, autolinks, raw-HTML
  passthrough) and `.txt` (escaped `<pre>` + `pre-wrap`; hard line breaks preserved, long
  lines soft-wrap, .txt→.md is a lossless fenced block) open through the HTML pipeline —
  `OpenMarkdown*`/`OpenText*`, every `HTMLOption` applies, md→md round-trips are a fixed
  point. Detection is extension-only (no content magic; the hint step outranks HTML
  sniffing by design). Landed with a cross-cutting inline-core fix: empty forced lines
  (blank lines in pre/pre-wrap/pre-line) now get a CSS strut height instead of collapsing
  (`pkg/layout/inline` shape/break; all prior goldens byte-identical).
  `2026-07-09-markdown-text-input-design.md`.

**DOCX writer** (`pkg/render/docxwrite`, `WriteDOCX`/`ConvertHTMLToDOCX`, CLI `todocx` +
`convert ... out.docx`):

- Everything → .docx (HTML/Markdown/text, and PDF via extraction) — a cssbox STRUCTURE writer
  (boxwalk-based, like the Markdown one; not layout-faithful) emitting native Word constructs
  chosen so our own reader round-trips them: HeadingN pStyles (+ rPr scale), direct-rPr emphasis,
  `w:hyperlink` + External rels, Quote/CodeBlock/HorizontalRule styles (reader maps the latter two
  to pre/hr; `w:rStyle` now parsed so CodeChar marks inline code), one-paragraph code blocks via
  `w:br`, per-instance ordered-list numbering, deterministic OPC output. Round-trip parity matrix
  (HTML→docx→md ≡ HTML→md) + reopen-verified units + `docxout-basic` golden. Landed with a
  cross-cutting lowering fix: consecutive DOCX list paragraphs now group into nested list-container
  boxes (mixed bodies no longer drop non-list content from Markdown/HTML conversion; nested lists
  keep their depth). Tables ship natively (`boxwalk.BuildOccupancyGrid` → `w:tbl`/`gridSpan`/
  explicit `vMerge` chains, per-cell borders/shading, `tblHeader` rows — with a lowering addition
  marking header-row cells bold so headers round-trip; captions → a bold Caption style) and images
  embed as deduped media parts + `wp:inline` drawings fetched through a new `reflowResources`
  loader seam (no loader → alt text + log). Round-trip parity matrix incl. tables,
  `docxout-basic`/`docxout-htmldoc-p1` goldens + the `htmldoc.docx.md` showcase round-trip golden.
  `2026-07-09-docx-writer-design.md`.

**CSV/TSV input + output** (`pkg/doctaculous/csv_frontend.go`, `pkg/render/csvwrite`,
`OpenCSV*`/`OpenTSV*`, `WriteCSV`/`WriteTSV`):

- Input: stdlib `encoding/csv` (lazy quotes, ragged rows padded, BOM/CRLF) → an HTML table
  (first row = header) through the reflow pipeline; CSV and TSV are distinct formats (csv ⇄ tsv
  are real conversions), extension-only detection. Output: tables-only structure writer over the
  boxwalk occupancy grid (spans duplicated — the GFM strategy; multiple tables blank-line
  separated; prose dropped + logged, table-less documents produce empty output + a loud log) —
  which makes **PDF → CSV table extraction** work via the existing lattice/stream recognizer
  (pinned by test). `csv-specimen` golden. `2026-07-09-csv-tsv-io-design.md`.

**XLSX input** (`pkg/xlsx` hand-rolled reader + `pkg/doctaculous/xlsx_frontend.go`,
`OpenXLSX*`, `testdata/gen/xlsx` fixture builder):

- Read-only cached-value extraction (no formula evaluation; the dep audit that ruled out
  excelize/tealeg is in the spec): OPC container mirroring `pkg/docx/zip.go`, shared/rich strings,
  styles (bold/italic/fill/alignment), dates via builtin + heuristic numFmt codes against the
  1900 (Lotus-leap-safe) or 1904 epoch, General/percent number rendering, `mergeCells` → native
  spans, hidden sheets skipped (hidden rows/cols render — view state, not data). Visible sheets →
  `<h2>`-headed ruled tables through the HTML pipeline; a bold first row becomes the header row
  via the writers' existing detector. ZIP detection generalized to an OPC classifier
  (`word/`→DOCX, `xl/`→XLSX). `xlsx-specimen` golden. `2026-07-09-xlsx-input-design.md`.

**XLSX output** (`pkg/render/xlsxwrite`, `WriteXLSX`, `convert ... out.xlsx`):

- Tables-only writer (shared `boxwalk.CollectTables`/`CellPlainText` with csvwrite): one worksheet
  per table (caption-derived names, sanitized/unique/31-char), native `mergeCells` spans, bold
  header xf (the reader's header detector — headers round-trip), inlineStr + numeric cells (clean
  numbers stay numbers, so csv→xlsx→csv is byte-identical; `007` stays text), deterministic OPC;
  table-less documents write one empty sheet + a loud log. Round-trip parity via the `pkg/xlsx`
  reader; pdf→xlsx extraction pinned. v1 punts: alignment/fill write-back, typed date cells.
  `2026-07-09-xlsx-output-design.md`.

**Stream + MIME input surface** (`pkg/doctaculous` format.go/open.go, first tinycld-adoption PR):

- `FormatFromMIME`/`Format.MIME()` (params stripped/case-folded; explicit-Unknown pins for
  legacy binary Office — never the OOXML cousins — HEIC, zip, octet-stream; unlisted `text/*` →
  FormatText with `text/rtf` excepted; rows flip to PPTX/EPUB/RTF when those frontends land);
  `OpenReader`/`OpenReaderAs(ctx, ...)` stream entry points (fully buffered) threading a real
  open-time context through layout — a cancelled open ERRORS rather than returning a silently
  truncated document (boundary check; the engine itself degrades); `Convert`/`ConvertFile` now
  pass their ctx to open; `MarkdownOptions.MaxBytes` rune-safe text-output cap (search-index
  extraction). Capability gate for hosts = `FormatFromMIME(mt).ValidInput()`.
  `2026-07-10-mime-reader-open-design.md`.

**DOCX reader fidelity — the public-model PR 1/3** (`pkg/docx`, toward a supported read+write
document model consumed externally by tinycld/text):

- Tracked changes (w:ins/w:del as `ParaChild.Revision` containers; `w:delText`;
  rPr/pPr/tcPr `*Change` before-states; cellIns/cellDel), comments (part + range markers +
  reference runs; markers inside hyperlinks hoist outward), endnotes (`Notes` container with
  exported `ByID`, shared footnote/endnote parser), drop-cap frames, anchored-drawing wrap
  facts, `Border.Style` names, paragraph-attached `SectPr`, numbering restructure
  (`Abstract`/`Instances`/`Start`/`StartAt`), run `Shd`, `Relationship.Type`,
  `Hyperlink.Target` resolved at parse, `Style.Name`, `Document.ExtraParts` (customXml
  preserved verbatim). Rendering pins: revisions render FINAL state ("No Markup"), comments
  invisible, drop cap degrades; upgrades: endnote markers, run shading, list start/override
  seeding, anchored square-wrap images → CSS floats. `fidelity` core fixture + golden.
  `2026-07-10-docx-model-fidelity-design.md`.

**DOCX model writer — the public-model PR 2/3** (`pkg/docx` `Write`/`Bytes`):

- Full-vocabulary deterministic OPC emitter in pkg/docx itself (stdlib-only; schema-ordered
  props; rels preserved + structural/hyperlink rels allocated; tabs/delText/xml:space
  mirrored; Word-complete drawing scaffold; zero SectionProps → Letter defaults;
  `ErrInvalidDocument` hard-fails instead of dropping content). `DefaultStyles()`/`AddImage`
  constructors; package doc declares the vocabulary + additive-only stability promise.
  Round-trip contract Parse∘Write ≡ id pinned by: 15-fixture modelCore corpus, 200-doc seeded
  randomized sweep, per-fixture determinism, and a byte-level second-write fixed point over
  the gen corpus; `model-specimen` core fixture renders the construct→Write→reopen path into
  a golden. `2026-07-10-docx-model-writer-design.md`.

**XLSX reader enrichment — calc-adoption PR 1/5** (`pkg/xlsx`):

- Additive structured read surface: `Cell.Value` (typed Empty/String/Number/Bool/Date/Error;
  dates via the shared serial logic), `Cell.Formula` (shared formulas EXPANDED per member via
  a lexical A1 shifter — anchors fixed, literals/sheet-names opaque, "(" = function call),
  `Cell.StyleID`/`Cell.Style` (full font/fill/alignment/border-with-diagonal/numFmt/protection
  vocabulary; Color keeps rgb OR indexed OR theme+tint), sheet facts (visibility enum, tab
  color, frozen panes, sparse row heights/row styles/col widths, defaults), workbook
  `Date1904` + `DefinedNames`, 1-based coordinate helpers, complete builtin numFmt id table.
  Display path byte-identical (Text untouched; formatter keeps its subset).
  `2026-07-10-xlsx-reader-enrichment-design.md`.

**XLSX preservation-first editor core — calc-adoption PR 2/5** (`pkg/xlsx` `Edit`/`New`/`Save`):

- Open-mutate-save with the strongest preservation contract: untouched parts copy
  byte-verbatim at the zip layer (no-op Edit+Save ⇒ part-for-part byte-identical, reads never
  dirty), dirty parts re-serialize through `internal/xmlpart` (beevik/etree pinned settings —
  unknown elements/attrs/prefixes survive in order; DOCTYPE rejected; keystone
  parse→serialize→reparse tree-equal property + fuzz). Sheet ops (add/delete/move/rename/
  visibility with last-visible guards, tab color), typed cell writes (SetString inlineStr/
  SetNumber/SetBool/SetDate with xf-clone date-format ensuring, ATOMIC
  SetFormula(src, cached)), ClearCell keeps style, Cells iteration, merges/frozen panes/
  dimension/row heights/col widths (range-splitting), stale calcChain dropped on first value
  edit (part + CT + rel). Deterministic saves; single-goroutine editor, 1-based coordinates.
  `2026-07-10-xlsx-editor-core-design.md`.

**XLSX style read-modify-write — calc-adoption PR 3/5** (`pkg/xlsx` `PatchCellStyle`):

- The patch-not-replace overlay contract: all-pointer-leaf `StylePatch` (fonts/fills/
  alignment/borders-with-Clear/numFmt; `*""` clears) applied by CLONING the cell's xf +
  font/fill/border records and editing nodes — unmodeled facets (diagonal borders, indent,
  rotation, protection, theme colors, unknown children like font `scheme`) ride along,
  pinned by test. Records dedupe semantically (`xmlpart.Equal`); numFmt patterns reuse
  builtin ids deterministically, reuse custom codes, else allocate ≥164. Whole-style
  `SetCellStyle` + row-style variants + memoized `CellStyle` reads. Per-leaf canary audit
  (editor read AND save/reopen), mirroring calc's style_attribute_registry.
  `2026-07-10-xlsx-styles-rmw-design.md`.

**RTF input** (`pkg/rtf`, `OpenRTF*`, `convert in.rtf ...`):

- Dependency-free tokenizer + converter → HTML through the reflow pipeline: paragraph/char
  formatting with 0-toggles, font/color tables, alignment/indents, cp1252 + `\uN`/`\ucN`
  escapes, hyperlink fields, `\trowd` tables, `\pngblip`/`\jpegblip` pictures (data: URIs;
  others logged + skipped), `\paperw`-family page geometry → `@page`; the RTF resilience rule
  (unknown words skipped, unknown `{\*}` destinations ignored) is the degrade story. Wiring:
  `{\rtf` magic, `.rtf`, MIME rows flipped, input capability bit. Landed with a
  cross-cutting engine fix: **data: image URIs decode without a resource loader**
  (`resource.LoadDataURL` short-circuits the image cache — the browser rule). `rtf-specimen`
  golden. `2026-07-10-rtf-input-design.md`.

**RTF output** (`pkg/render/rtfwrite`, `WriteRTF`/`ConvertHTMLToRTF`, `convert ... out.rtf`):

- Everything → .rtf — a cssbox STRUCTURE writer (boxwalk-based, the Markdown/DOCX shape) whose
  mappings our own reader round-trips: block semantics on stylesheet names (`\sN` "heading N"
  + `\outlinelevel`, Quote/CodeBlock/HorizontalRule — the reader now parses the stylesheet and
  maps the names back, which also upgrades real Word files), lists on `\ls`/`\ilvl` + a literal
  `\pntext` marker (reader now captures markers → nested `<ul>`/`<ol>`), inline code on the
  monospace font (reader: mono font → `<code>`), HYPERLINK fields, `\trowd` tables with
  `\trhdr` header rows (reader → `<th>`) and spans DUPLICATED into covered slots (the GFM
  strategy — round-tripped grids match direct conversion), captions as a bold line, `\pict`
  png/jpeg (data: URIs embed loaderless and round-trip byte-identically), `\uN?` escapes incl.
  surrogate pairs. Deterministic. 17-case html→rtf→md ≡ html→md parity matrix + md/pdf loops +
  `rtfout-basic` golden; RTF is in the convert matrix as input AND output.
  `2026-07-10-rtf-output-design.md`.

**PPTX output** (`pkg/render/pptxwrite`, `WritePPTX`/`ConvertHTMLToPPTX`, `convert ... out.pptx`):

- Everything → .pptx — a cssbox STRUCTURE writer: every `<h1>`/`<h2>` starts a new slide with
  that heading as the title placeholder; following blocks become the body (text box paragraphs,
  `buChar`/`buAutoNum`+`lvl` lists, native `a:tbl` with `gridSpan`/`rowSpan` + `hMerge`/`vMerge`
  continuations, `p:pic` media parts with loaderless data:-URI embedding). Logged degrades:
  h3–h6 → bold paragraphs, quote/code flatten, links drop targets, hr skipped. Deterministic
  OPC (the gen-fixture package shape). Reopen-verified per-construct round trips through
  pkg/pptx + slide-count pin + `pptxout-basic` golden; PPTX joins the convert matrix as input
  AND output. Landed with a D1 frontend fix the round trip exposed: nested-list `<ul>` now
  opens INSIDE its parent `<li>` (structure writers previously dropped nested items).
  `2026-07-10-pptx-output-design.md`.

**EPUB output** (`pkg/render/epubwrite`, `WriteEPUB`/`ConvertHTMLToEPUB`, `convert ... out.epub`)
— **completes the any⇄any table: all 13 formats are both inputs AND outputs**:

- Deterministic EPUB 3 built ON htmlwrite (content documents ARE XHTML — a new byte-identical
  `XHTML` mode self-closes voids, a new `ImageSrc` hook rewrites srcs during serialization):
  stored `mimetype` first entry, container.xml, OPF (title from option → first `<h1>` →
  "Document"; fixed dcterms:modified), `nav.xhtml` TOC, chapter split at `<h1>` (heading-less
  → one chapter), images as deduped manifest items via the loader seam (data: URIs stay
  inline and round-trip verbatim). Pinned by the STRICT parity bar — 17-case
  html→epub→md ≡ html→md exact equality — plus package-shape pins (stored-mimetype-first,
  nav links, chapters ⇒ pages), md→epub→md loop, `epubout-basic` golden; EPUB joins the
  convert matrix as input AND output. `2026-07-10-epub-output-design.md`.

**DOCX writer unification** (`pkg/render/docxwrite` → `docx.Write`) — the public-model
PR 3/3:

- docxwrite's cssbox walk now BUILDS a `*docx.Document` (DefaultStyles/AddImage/model
  paragraphs-runs-hyperlinks-tables-drawings, parse-shaped runs) and serializes via
  `docx.Write` — ONE OPC emitter for the repo; its private XML/zip machinery (opc.go,
  styles.go, numbering.go, xml.go) is deleted, the public `docxwrite.Write` API unchanged.
  Facts the old XML carried land as additive model fields (parsed + written + fixture-
  covered): `ParagraphProps.Borders` (w:pBdr — HorizontalRule keeps its visible rule),
  `NumLevel.IndentLeft/Hanging` (per-level list indents), `TableProps.LayoutFixed`.
  Semantically 1:1 (parity matrix + reopen units unchanged; PNG goldens byte-identical);
  a linked image now survives the round trip BESIDE its link group (the reader dropped
  drawings inside w:hyperlink), pinned by test. `2026-07-10-docxwrite-unification-design.md`.

**PPTX input** (`pkg/pptx`, `OpenPPTX*`, `convert deck.pptx ...`):

- Hand-rolled PresentationML reader: visible slides' shape trees (text frames with
  level/bullet/alignment + run b/i/sz/color, pictures, spanned tables), frames resolved
  through slide→layout→master placeholder inheritance; hidden slides skipped; animations/
  SmartArt/themes out of scope (content still extracts). Frontend renders one fixed-size
  page per slide (absolutely positioned frames; pictures as data: URIs; titles → h2;
  bullets → nested ul/ol with kind-switch handling), shapes ordered title-first/top-down
  for the structure writers. `classifyOPC` gains ppt/; `.pptx`/`.pptm`; presentationml MIME
  row flipped; input capability bit (output = D2). `pptx-specimen` golden.
  `2026-07-10-pptx-input-design.md`.

**EPUB input** (`pkg/epub`, `OpenEPUB*`, `convert book.epub ...` — reverses the old
out-of-scope note):

- Container reader: container.xml → OPF (title, manifest, spine; `linear="no"` skipped;
  EPUB 2/3 via the spine, NCX ignored); spine documents' body markup concatenates in reading
  order (each chapter `break-before: page` when paginated) with collected package CSS +
  inline styles; images/fonts/linked CSS resolve from the container through a loader adapter
  (the OPF-directory layout every real-world book uses; the dir-loader default is skipped so
  the container loader wins). DRM (META-INF/encryption.xml) → typed `epub.ErrEncrypted`.
  Detection: the OCF `mimetype` zip entry in classifyOPC; `.epub`; MIME row flipped; input
  capability bit (output = E2). `epub-specimen` golden. `2026-07-10-epub-input-design.md`.

**PNG/JPEG input — images as documents** (`OpenImage*`):

- The any⇄any principle applied to the image formats: an image opens as a single page exactly
  its pixel size (format stamped from the actual encoding via DecodeConfig; data:-URI embed
  through the reflow pipeline). image→PDF fills the page edge to edge (pinned), image→JPEG
  transcodes, markdown carries a data: URI, plain-text/tables-only outputs are empty by
  design; png→png stays ErrSameFormat. Input capability bits flipped; conversion matrix
  extended. `2026-07-10-image-input-design.md`.

**XLSX conditional formats + cell notes — calc-adoption PR 4/5** (`pkg/xlsx`):

- CF: one raw-fidelity read path for Workbook AND editor (`CFRule` typed fields + resolved
  dxf + VERBATIM `Raw` — data bars/color scales pass through byte-faithfully);
  `SetConditionalFormats` replaces wholesale (raw rules re-emit verbatim with renumbered
  priorities; typed rules mint deduped dxfs). Notes: `Comment` (1-based A1-space) read on
  both views; `SetComment`/`RemoveComment` regenerate the comments part + legacy VML
  wholesale, wiring rels/content-types/legacyDrawing on first use. Editor-core fixes:
  `sheetRelTarget` reads current bytes; `File.setPart` lets an original part be regenerated
  through the dirty machinery. `2026-07-10-xlsx-cf-comments-design.md`.

**XLSX pivots + defined-names write — calc-adoption PR 5/5** (`pkg/xlsx`):

- `PivotTables()` read (definition joined with its cache for source + field names),
  `RemovePivotTables()` (clean slate — parts, rels, workbook caches, CTs), `AddPivotTable`
  (cache w/ `refreshOnLoad` + empty records — definitions round-trip, values recompute; full
  wiring in one call; axis/value fields by source header name, hard error on unknowns).
  `SetDefinedNames`/`DefinedNames()` replace/read the workbook names (sheet-local + hidden).
  Editor-core fix: `setPart` resurrects a deleted part (remove-then-add in one session — calc's
  save shape). `2026-07-10-xlsx-pivots-names-design.md`.

**External office corpus — real-world DOCX/XLSX preservation fixtures**
(`testdata/external/{docx,xlsx}`):

- 24 committed Word-/Mac-Office-/Excel-/LibreOffice-authored files (Apache-2.0 from POI,
  MPL-2.0 from LibreOffice core, MIT from Open-XML-SDK incl. an ISO-strict workbook —
  per-file provenance + license texts in each dir's README, isolated like the CC-BY-SA PDF
  corpus). Swept by: the xlsx preservation contract (pinned feature counts, no-op Edit+Save
  BYTE-IDENTICAL, edit+reopen), the docx save-cycle contract (Parse∘Write fixed point — the
  external half this contract always promised), and a full-pipeline
  open/raster/convert smoke. Landing them caught two real fidelity bugs, fixed at the
  source: `Run.CommentRef`'s zero sentinel dropped id-0 comment references (both Word and
  LibreOffice number comments from 0 — now `HasCommentRef`), and a bare `<w:ilvl>` without
  `numId` (Word's own Subtitle style) was dropped on write.

**Page geometry + fit-within raster sizing** (`pkg/doctaculous`, CLI `--max-width/--max-height`):

- `Document.PageSize(i)` (points, post-/Rotate for PDF — always the rendered aspect);
  `RasterOptions.MaxWidthPx/MaxHeightPx` fit-within-box sizing resolved per page to a concrete
  DPI above the backends (painting untouched — fit ≡ explicit-DPI, pixel-identical, test-pinned;
  ceil-safe exact fits). DPI becomes a resolution CEILING alongside the box (zero = fill the
  box, upscaling vector-sharp; positive = downscale-only thumbnails). CLI flags on `rasterize` +
  `convert` image output (unset `--dpi` = pure fit via flag.Visit).
  `2026-07-10-fit-raster-sizing-design.md`.
