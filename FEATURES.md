# Features

The complete inventory of shipped features, validated against a real-world corpus
(`testdata/external/`). Keep this list current as features land: every feature that ships gets a
bullet here in the same PR. Each bullet is a one-line pointer; the detailed design/rationale for a
sub-project is in the commit and PR history. What's *next* — the TODO
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
  viable pure-Go decoder).
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
bullet's design rationale is in its PR:

- **CSS parse + cascade** (`pkg/css`): dependency-free tokenizer/parser, selector matching +
  specificity, full cascade (specificity + source order + inheritance + `!important` + inline
  `style` + origins), shorthand expansion.
- **HTML frontend — box generation** (`pkg/html`, `pkg/layout/cssbox`): owned DOM, UA stylesheet,
  anonymous-box fixups, whitespace collapsing, `display:none` pruning; `<link>` via
  `pkg/resource.ResourceLoader`.
- **Block + inline normal flow** (`pkg/layout/inline`, `pkg/layout/css/block.go`+`inline.go`,
  `pkg/layout/paint`, `OpenHTML`/`OpenHTMLBytes`): box model (width/`auto`/%, `box-sizing`,
  min/max, margins incl. vertical collapsing, padding, borders, backgrounds), IFC (shaping/breaking,
  `text-align`, `line-height`), fragment tree.
- **Replaced content + images** (`pkg/layout/css/image.go`+`replaced.go`): `<img>` decode (PNG/JPEG/
  GIF stdlib, HEIC via `pkg/heif`) → CSS replaced-sizing → paint via `DrawImage`, with
  `object-fit`/`object-position`.
- **Floats + clear** (`pkg/layout/css/floats.go`): per-BFC float context, narrowing/wrapping,
  `clear`, own paint layer.
- **Positioning** (`pkg/layout/css/positioning.go`): relative (paint-time offset) + absolute/fixed
  (out-of-flow, two-pass against containing block), stacking contexts.
- **Overflow clipping** (`pkg/css` `overflow`, `layout.ClipPush/PopKind`): clip to padding box +
  BFC establishment + deferred float interactions.
- **Full z-index stacking** (`pkg/layout/css/fragment.go`): Appendix E bands (negative-z behind
  in-flow, then auto/0 doc order, then positive), relative clip-escape (sub-project 6b).
- **CSS 2.1 §17 tables** (`pkg/layout/css/table.go`+`tableborder.go`+`tablefix.go`+`measure.go`):
  anonymous-table fixup, grid model, fixed + auto column-width solve, colspan/rowspan,
  `vertical-align`, captions, `<col>`/`<colgroup>`, both `border-collapse` models.
- **Web fonts** (`pkg/css/fontface.go`, `pkg/font/sfnt.go`/`woff1.go`/`woff2*.go`,
  `pkg/layout/font`): `@font-face` capture, WOFF1/WOFF2 decode (incl. glyf/loca transform), `local()`
  via `DiskFontProvider`, family-fallback-list resolution.
- **Flexbox** (`pkg/layout/css/flex.go`+`flexfix.go`): axis-abstracted layout, §9.7
  flexible-length resolution, `justify-content`/`align-items`/`align-self`, `inline-flex`, and
  **multi-line wrapping** — `flex-wrap: wrap`/`wrap-reverse`, §9.3 line collection,
  `align-content` (incl. the flex `stretch` initial, which differs from the shared grid default),
  the cross-axis gap between lines, and the `flex-flow` shorthand. justify-content and baseline
  groups resolve per LINE; the §9.4 step-8 cross clamp is gated to single-line containers.
  wrap-reverse XORs with the RTL cross flip. Wrapped rows paginate between lines for free
  (`splitFlexGridForPage` is geometry-driven).
- **CSS Grid** (explicit grid; `pkg/layout/css/grid.go`+`grid_track.go`+`grid_place.go`+`gridfix.go`
  +`baseline.go`): §11 track-sizing + §8 placement (spans, named areas, auto-placement sparse/dense),
  item + content-distribution alignment, `inline-grid`, cross-cutting baseline backport (grid + flex
  + table cells).
- **`OpenURL` + HTTP loader** (`pkg/resource/http.go`): fetch HTML over HTTP(S), resolve relative
  refs, `data:` URIs, URL-userinfo Basic auth (redacted from logs).
- **Pagination** (`pkg/layout/css/paginate.go`, `WithPageSize`): fixed-height page fragmentation,
  break cascade, between-block + forced breaks, per-page distribution of relative/abs/fixed/float +
  html/body border.
- **Per-page bottom-anchored `position: fixed`** (`pkg/layout/css/paginate.go`): a `bottom`-anchored
  fixed box sits at the bottom of every page. It was previously resolved against the full single-tall
  document height, putting it below every page and making it invisible. Top-anchored boxes are
  untouched (their Y is already the page-local offset), and a percentage `bottom` is declined rather
  than mis-shifted.
- **Repeated `<thead>` on continuation pages** (`pkg/layout/css/tablepage.go`, `table.go`): a table
  split across pages repeats its header rows on every continuation, so a long table keeps its column
  headings. The head/body/footer distinction is flattened away by grid construction, so the header's
  bottom Y is recorded on the table fragment (`HeaderBottom`) and the cells above it are deep-cloned
  onto each tail — and the tail's own `HeaderBottom` is re-anchored to its copy, so a table spanning
  three or more pages keeps repeating.
- **Mid-cell table splitting** (`pkg/layout/css/tablepage.go`): a table row taller than the page —
  including a single-row table — splits THROUGH its cells rather than overflowing and being clipped.
  A cell's content is an ordinary fragment spine, so the recursive splitter handles it with no
  relayout. Cells fragment independently: one that cannot break rides the tail whole while its
  row-mates split. Breaking between whole rows is still preferred when a row boundary is available.
- **Mid-block forced breaks** (`pkg/layout/css/fragmentpage.go`, `paginate.go`): a `break-before`/
  `break-after` on a nested block that is neither at its ancestor's leading nor trailing edge now
  splits that ancestor at the break position instead of being dropped with a warning. Reuses the
  recursive splitter; the split Y comes from the author's break rather than the page boundary, so it
  applies even when the page is not full.
- **Recursive spine splitting** (`pkg/layout/css/fragmentpage.go`): a child straddling a page boundary
  is itself split rather than riding the tail whole, so a `section > div > p` spine breaks at a line
  boundary inside the paragraph instead of leaving the head page blank below the last whole child.
  The dispatcher needed no signature change — `pageBottom` is absolute page space and the fragment
  tree shares one coordinate system, so it was already valid at any depth. `break-inside: avoid` stops
  the recursion.
- **Page-split correctness** (`pkg/layout/css/fragmentpage.go`): a split routes out-of-flow children
  (floats, positioned boxes) to the fragment whose band contains them instead of dropping them from
  both; detaches the `BgImage`/`ClipChain`/`Collapsed` state the shallow clone would otherwise share
  between two pages (the per-page shift mutates those in place, so one fragment's shift moved the
  other's background); and clamps a clipping fragment's `ClipRect` to its own extent. Applied at the
  single `splitAnyBlockForPage` dispatch point.
- **CSS Paged Media** (`pkg/css/page.go`+`pagesize.go`, `pkg/layout/css/pagemodel.go`+
  `fragmentpage.go`+`marginbox.go`, `WithDefaultPaged`): `@page` size/margins/named/pseudo + 16
  margin boxes, `break-inside`, widows/orphans via mid-block line fragmentation, running
  headers/footers with page counters, `@page marks`/`bleed`, `string-set`/`string()`,
  `position: running()`/`content: element()`, named-page multi-width reflow.
- **`white-space`** (`pkg/css` + `pkg/layout/inline`): normal/nowrap/pre/pre-wrap/pre-line + tab
  stops.
- **List markers + CSS counters** (`pkg/css/counter_format.go`, `pkg/layout/css/counters.go`,
  `pkg/font/bullet.go`): `list-style-*`, `counter-reset`/`-increment`/`-set`, `content: counter()`;
  synthetic bullet outlines.
- **`background-image`** (`pkg/css/background.go`, `pkg/layout/css/background.go` + paint):
  `url(..)`, `-repeat`/`-position`/`-size`/`-origin`/`-clip`.
- **Link pseudo-classes + `text-decoration: underline`** (`pkg/css/selector.go`, `pkg/html/ua.go`):
  `:link`/`:visited` + general pseudo-class parsing.
- **Legacy presentational-attribute hints** (`pkg/css/hints.go`): `bgcolor`/`align`/`valign`/
  `width`/`cellspacing`/`cellpadding`/`border`/`<font>`/`<ol type/start>`/`<body link>`/`dir`… mapped to
  CSS below author rules (HN renders with its bgcolor).
- **Direction-relative alignment + bidi plumbing** (`pkg/css/cascade.go`, `pkg/css/hints.go`,
  `pkg/layout/css/inline.go`) — RTL slice 1 of 5: `text-align: start|end|match-parent` (the initial
  value is now `start`, byte-identical for LTR since every consumer defaults to left);
  `unicode-bidi` parsed + stored (not inherited); the global `dir` attribute as a hint (the
  selector engine has no attribute selectors, so the spec's `[dir=rtl]` UA rules are not
  expressible — hint rank is equivalent); `bdi`/`bdo` isolation; `effectiveDirection` (an anonymous
  box's Style is zero-valued, so `Direction` is `""` not `"ltr"` — never read the field directly);
  RTL text-indent edge. `dir=auto` degrades + logs.
- **Box-level RTL — tables, flex, grid** (`pkg/layout/css` table/tableborder/flex/grid) — RTL
  slice 2 of 5, retiring **all three** "laying out LTR" logs: tables mirror their solved column
  x-offsets (and `buildCollapsedBorders` flips its index→physical-side mapping, or collapsed
  borders resolve against the wrong neighbor with no log); flex resolves direction in `axisFor`
  (a row XORs `reverseMain`, so RTL composes with `row-reverse`; a column flips the CROSS axis,
  the case the old guard skipped silently); grid mirrors track positions AND resolves
  `justify-items`/`justify-self` `start`/`end` logically (both flips are required —
  mutation-verified independently). Also fixes `crossOffset` ignoring the Box Alignment
  `start`/`end` spellings. Showcase §15 + 4 WPT reftests. (Text WITHIN a line is reordered by the
  next slice; at this point it still rendered in logical order.).
- **Arabic contextual shaping** (`pkg/layout/inline/complex.go`) — RTL slice 4 of 5: a run of
  joining script (Arabic/Syriac/Thaana; Hebrew is non-joining and stays on the cheap per-rune
  path) is shaped as a whole segment through harfbuzz, resolving the font's GSUB tables so
  letters take their initial/medial/final/isolated forms and ligatures fuse. `Face.OpenTypeFont`
  exposes the SFNT, which satisfies `harfbuzz.FaceOpentype` directly. Shaping is forced
  LEFT-TO-RIGHT so the pipeline stays logical up to the single L2 reorder — harfbuzz would
  otherwise emit visual order and be reversed twice. Glyphs carry their cluster's runes exactly
  once, so `/ToUnicode` neither duplicates nor drops text. Showcase §15 "Real script".
- **Bundled RTL faces + per-rune script fallback** (`pkg/font/standard`, `pkg/layout/font/cache.go`,
  `pkg/layout/inline/shape.go`): Noto Sans Hebrew and Noto Naskh Arabic (both OFL 1.1, no Reserved
  Font Name) ship alongside the Latin substitutes. Because each bundled face covers exactly ONE
  script — the Latin faces have no Hebrew/Arabic and the Noto faces have no Latin — the covering
  face is resolved per **rune**, not per run: a Hebrew or Arabic phrase inside an otherwise-Latin
  paragraph now shapes instead of being silently dropped. Results cache per (script, style); the
  fallback consults bundled faces only. A fallback glyph carries the face it resolved from, since a
  GID is only meaningful against its own face.
- **Inline bidi reordering** (`pkg/layout/inline/bidi.go`) — RTL slice 3 of 5: shaping and breaking stay
  in LOGICAL order; `MakeVisualLine` applies UAX#9 rule L2 per line after the break is chosen, plus rule
  L4 bracket mirroring (`Glyph.Runes` keeps the ORIGINAL character so `/ToUnicode` recovers the authored
  text). `golang.org/x/text` promoted indirect→direct for UAX#9 — no new module. Line metrics are
  computed on the logical slice, because the space that ends the text reorders to an RTL line's visual
  START. Bidi control characters now survive shaping as zero-width glyphs (they were being dropped,
  silently discarding directional intent). (Arabic reordered correctly at this point but still rendered
  as isolated forms; the cluster model arrives in slice 4, below.).
- **Column flex vertical content sizing** (`pkg/layout/css/flex.go`): a column container's main axis is
  vertical, so `flex-basis: auto`/`content` (and the `min:auto` automatic minimum) now resolve to the
  item's content HEIGHT — measured by laying it out at its cross width and reading back the fragment
  height (`measureColumnMainContent`), the same two-phase pattern grid uses for row tracks — instead of
  a max-content WIDTH compared against a vertical budget. An auto-width column item's cross width is
  also clamped to the container, fixing a ~2.5x overflow for prose. Backlog H4.
- **Static form controls** (`pkg/layout/css/control.go`): `<input>`/`<button>`/`<textarea>`/
  `<select>` as static native widgets (classic chrome, non-interactive).
- **End-to-end "specimen" showcase** (`testdata/htmldoc/`, `htmldoc-*` goldens): one multi-file doc
  exercising every HTML/CSS/image slice, served over loopback HTTP via `OpenURL` + `WithPageSize`.

**DOCX frontend** (`OpenDOCX`/`OpenDOCXBytes`, `docx-*` goldens):

- **Parse + cascade** (`pkg/docx`, `pkg/docx/style`): ZIP/OPC container, `document.xml`
  (paragraphs, runs, `w:t`/`w:br`/`w:tab`), run + paragraph properties, section geometry
  (`w:sectPr`), the full `docDefaults → basedOn → direct` cascade.
- **CSS-engine convergence** (`pkg/docx/cssbox`): DOCX lowers directly to `cssbox` + `ComputedStyle`
  and runs through the shared CSS engine (page geometry as a synthesized `@page` stylesheet); the old
  flat model/engine are deleted.
- **DOCX fidelity** (lists/numbering, tables, images, headers/footers + multi-section — most reuse
  the CSS engine's existing vocabulary via lowering).

**HTML/DOCX → PDF writer** (`pkg/render/pdfwrite`, `WritePDF`):

- A second `render.Device` that emits a real PDF with **selectable/searchable text** (Type0/
  Identity-H CIDFontType2 with glyf-subsetted `/FontFile2` for TrueType, simple `/Type1` with
  `/FontFile` for the bundled substitutes; `/ToUnicode` on every face). Concurrent per-band assembly,
  deterministic output, `@media print` capture (`pkg/css/media.go`). Byte-identical for the raster
  corpus (the new `DrawGlyph` seam rasterizes via the outline).
- **`/ExtGState` resource emission**: a partially-transparent fill/stroke/glyph/image now survives
  into PDF output as `/ca` (non-stroking) or `/CA` (stroking) alpha, and a non-Normal blend mode as
  `/BM`, wrapped in a scoped `q`/`Q` so it never leaks to later content. States are deduplicated by
  content, so many shapes sharing one alpha/blend emit a single `/ExtGState` resource. Fully-opaque,
  Normal-blend output is unchanged byte-for-byte (no resource, no `gs` operator emitted).

**HTML/DOCX → Markdown & plain text** (`pkg/render/markdown`, `WriteMarkdown`
+ `WriteText`, CLI `tomd`):

- A conversion backend that walks the shared `cssbox` tree (not the paint seam — it needs structure,
  not glyphs), so one walker serves HTML and DOCX. Small additive semantic annotations on `cssbox.Box`
  (`SemTag`/`HeadingLvl`/`Href`) captured by both frontends carry the facts computed style drops
  (heading level, link URLs, DOCX style identity); layout/raster/PDF ignore them (byte-identical).
  Emits GFM: headings, bold/italic/strikethrough/code, links, images, blockquotes, fenced code,
  nested + task lists, thematic breaks, and **high-fidelity pipe tables** (colspan/rowspan expanded by
  content duplication, alignment, caption).

**PDF → Markdown & HTML** (`pkg/pdf/extract`, `pkg/render/htmlwrite`, `WriteHTML`,
CLI `tomd <pdf>` / `tohtml`):

- Structure recovery from a PDF's positioned glyphs + vector paths. The content interpreter gains
  optional, paint-neutral capture sinks (`content.Options.TextSink`/`GraphicsSink`, nil =
  byte-identical); `pkg/pdf/extract` reconstructs words→lines→**XY-cut** reading-order blocks (columns
  handled) + **automatic table recognition** (lattice from ruling lines + stream from whitespace,
  auto-selected), lowering to a synthetic `cssbox` tree the Markdown writer reuses. A new
  `pkg/render/htmlwrite` serializes `cssbox`→HTML (native `colspan`/`rowspan`). PDF `Document`
  satisfies `reflowTree` via lazy extraction. ToUnicode CMaps (Type0/CID text), font weight/slant, and
  scanned-PDF OCR are follow-ups.
- **Right-to-left text extracts in LOGICAL order** (`pkg/pdf/extract/bidi.go`). A PDF stores glyphs by
  POSITION, so sorting a line left-to-right yielded RTL script reversed. Each maximal RTL run is
  reversed back, at BOTH levels the PDF mirrors: the characters within a word and the order of
  consecutive RTL words. Runs after word grouping (which splits on x-gaps and would break on reordered
  glyphs), so each word keeps the geometry table/block detection needs. No-op for Latin.

**Unified conversion core** (`pkg/doctaculous/format.go`+`detect.go`+`open.go`+`convert.go`+
`image_backend.go`, CLI `convert`):

- One `Format` type + capability table (`CanConvert`, typed `ErrUnknownFormat`/
  `ErrUnsupportedFormat`/`ErrSameFormat`); content-first `DetectFormat` (magic → extension hint →
  WHATWG HTML sniff; no UTF-8⇒text fallback). `Open`/`OpenBytes` sniff any supported format (the
  PDF path is byte-identical); `OpenAs`/`OpenBytesAs` skip detection; every opener stamps
  `Document.Format()`. Generic `Convert`/`ConvertFile`/`(*Document).Write` dispatch any valid
  input→output pair (the legacy `ConvertXToY` wrappers were shims pinned byte-identical, since removed);
  same-format conversion is a deliberate `ErrSameFormat` on the generic path only. PNG/JPEG are
  output formats (`WriteImage`/`EncodeImage`; Convert-to-image writes one page, multi-page = CLI
  `%d` fan-out). CLI: `convert <in> <out>` with `--from`/`--to`; all subcommands share one
  detection-based opener (rasterize no longer assumes unknown extensions are PDF; topdf `--print`
  actually applies print media now). A new format lands by flipping its capability bit + one switch
  case in `openDetected`/`Write` — see the sibling contract in.

**Markdown + plain-text input** (`pkg/markdown` via goldmark (MIT, pure Go, zero transitive
deps), `pkg/doctaculous/markdown_frontend.go`+`text_frontend.go`):

- `.md` (CommonMark + GFM: tables, strikethrough, task lists, autolinks, raw-HTML
  passthrough) and `.txt` (escaped `<pre>` + `pre-wrap`; hard line breaks preserved, long
  lines soft-wrap,.txt→.md is a lossless fenced block) open through the HTML pipeline —
  `OpenMarkdown*`/`OpenText*`, every `HTMLOption` applies, md→md round-trips are a fixed
  point. Detection is extension-only (no content magic; the hint step outranks HTML
  sniffing by design). Landed with a cross-cutting inline-core fix: empty forced lines
  (blank lines in pre/pre-wrap/pre-line) now get a CSS strut height instead of collapsing
  (`pkg/layout/inline` shape/break; all prior goldens byte-identical).

**DOCX writer** (`pkg/render/docxwrite`, `WriteDOCX`, CLI `todocx` +
`convert.. out.docx`):

- Everything →.docx (HTML/Markdown/text, and PDF via extraction) — a cssbox STRUCTURE writer
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

**CSV/TSV input + output** (`pkg/doctaculous/csv_frontend.go`, `pkg/render/csvwrite`,
`OpenCSV*`/`OpenTSV*`, `WriteCSV`/`WriteTSV`):

- Input: stdlib `encoding/csv` (lazy quotes, ragged rows padded, BOM/CRLF) → an HTML table
  (first row = header) through the reflow pipeline; CSV and TSV are distinct formats (csv ⇄ tsv
  are real conversions), extension-only detection. Output: tables-only structure writer over the
  boxwalk occupancy grid (spans duplicated — the GFM strategy; multiple tables blank-line
  separated; prose dropped + logged, table-less documents produce empty output + a loud log) —
  which makes **PDF → CSV table extraction** work via the existing lattice/stream recognizer
  (pinned by test). `csv-specimen` golden.

**XLSX input** (`pkg/xlsx` hand-rolled reader + `pkg/doctaculous/xlsx_frontend.go`,
`OpenXLSX*`, `testdata/gen/xlsx` fixture builder):

- Read-only cached-value extraction (no formula evaluation; the dep audit that ruled out
  excelize/tealeg is in the spec): OPC container mirroring `pkg/docx/zip.go`, shared/rich strings,
  styles (bold/italic/fill/alignment), dates via builtin + heuristic numFmt codes against the
  1900 (Lotus-leap-safe) or 1904 epoch, General/percent number rendering, `mergeCells` → native
  spans, hidden sheets skipped (hidden rows/cols render — view state, not data). Visible sheets →
  `<h2>`-headed ruled tables through the HTML pipeline; a bold first row becomes the header row
  via the writers' existing detector. ZIP detection generalized to an OPC classifier
  (`word/`→DOCX, `xl/`→XLSX). `xlsx-specimen` golden.
- **Sheet selection** (`WithSheets(names..)` open option, `convert --sheet` CLI flag,
  repeatable/comma-separated): render only named worksheets, in the given order, instead of every
  visible sheet; a single selected sheet drops its heading, an explicitly named hidden sheet
  renders, and an unknown name fails with `ErrSheetNotFound`. `WithSheets` is a universal
  `OpenOption` (inert for non-XLSX inputs); the option type is now `OpenOption` with `HTMLOption`
  a back-compat alias.

**XLSX output** (`pkg/render/xlsxwrite`, `WriteXLSX`, `convert.. out.xlsx`):

- Tables-only writer (shared `boxwalk.CollectTables`/`CellPlainText` with csvwrite): one worksheet
  per table (caption-derived names, sanitized/unique/31-char), native `mergeCells` spans, bold
  header xf (the reader's header detector — headers round-trip), inlineStr + numeric cells (clean
  numbers stay numbers, so csv→xlsx→csv is byte-identical; `007` stays text), deterministic OPC;
  table-less documents write one empty sheet + a loud log. Round-trip parity via the `pkg/xlsx`
  reader; pdf→xlsx extraction pinned. v1 punts: alignment/fill write-back, typed date cells.

**Stream + MIME input surface** (`pkg/doctaculous` format.go/open.go, first tinycld-adoption PR):

- `FormatFromMIME`/`Format.MIME()` (params stripped/case-folded; explicit-Unknown pins for
  legacy binary Office — never the OOXML cousins — HEIC *sequences*, zip, octet-stream; unlisted `text/*` →
  FormatText with `text/rtf` excepted; rows flip to PPTX/EPUB/RTF when those frontends land);
  `OpenReader`/`OpenReaderAs(ctx,..)` stream entry points (fully buffered) threading a real
  open-time context through layout — a cancelled open ERRORS rather than returning a silently
  truncated document (boundary check; the engine itself degrades); `Convert`/`ConvertFile` now
  pass their ctx to open; `MarkdownOptions.MaxBytes` rune-safe text-output cap (search-index
  extraction). Capability gate for hosts = `FormatFromMIME(mt).ValidInput()`.

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

**DOCX model writer — the public-model PR 2/3** (`pkg/docx` `Write`/`Bytes`):

- Full-vocabulary deterministic OPC emitter in pkg/docx itself (stdlib-only; schema-ordered
  props; rels preserved + structural/hyperlink rels allocated; tabs/delText/xml:space
  mirrored; Word-complete drawing scaffold; zero SectionProps → Letter defaults;
  `ErrInvalidDocument` hard-fails instead of dropping content). `DefaultStyles()`/`AddImage`
  constructors; package doc declares the vocabulary + additive-only stability promise.
  Round-trip contract Parse∘Write ≡ id pinned by: 15-fixture modelCore corpus, 200-doc seeded
  randomized sweep, per-fixture determinism, and a byte-level second-write fixed point over
  the gen corpus; `model-specimen` core fixture renders the construct→Write→reopen path into
  a golden.

**XLSX reader enrichment — calc-adoption PR 1/5** (`pkg/xlsx`):

- Additive structured read surface: `Cell.Value` (typed Empty/String/Number/Bool/Date/Error;
  dates via the shared serial logic), `Cell.Formula` (shared formulas EXPANDED per member via
  a lexical A1 shifter — anchors fixed, literals/sheet-names opaque, "(" = function call),
  `Cell.StyleID`/`Cell.Style` (full font/fill/alignment/border-with-diagonal/numFmt/protection
  vocabulary; Color keeps rgb OR indexed OR theme+tint), sheet facts (visibility enum, tab
  color, frozen panes, sparse row heights/row styles/col widths, defaults), workbook
  `Date1904` + `DefinedNames`, 1-based coordinate helpers, complete builtin numFmt id table.
  Display path byte-identical (Text untouched; formatter keeps its subset).

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

**XLSX style read-modify-write — calc-adoption PR 3/5** (`pkg/xlsx` `PatchCellStyle`):

- The patch-not-replace overlay contract: all-pointer-leaf `StylePatch` (fonts/fills/
  alignment/borders-with-Clear/numFmt; `*""` clears) applied by CLONING the cell's xf +
  font/fill/border records and editing nodes — unmodeled facets (diagonal borders, indent,
  rotation, protection, theme colors, unknown children like font `scheme`) ride along,
  pinned by test. Records dedupe semantically (`xmlpart.Equal`); numFmt patterns reuse
  builtin ids deterministically, reuse custom codes, else allocate ≥164. Whole-style
  `SetCellStyle` + row-style variants + memoized `CellStyle` reads. Per-leaf canary audit
  (editor read AND save/reopen), mirroring calc's style_attribute_registry.

**RTF input** (`pkg/rtf`, `OpenRTF*`, `convert in.rtf..`):

- Dependency-free tokenizer + converter → HTML through the reflow pipeline: paragraph/char
  formatting with 0-toggles, font/color tables, alignment/indents, cp1252 + `\uN`/`\ucN`
  escapes, hyperlink fields, `\trowd` tables, `\pngblip`/`\jpegblip` pictures (data: URIs;
  others logged + skipped), `\paperw`-family page geometry → `@page`; the RTF resilience rule
  (unknown words skipped, unknown `{\*}` destinations ignored) is the degrade story. Wiring:
  `{\rtf` magic, `.rtf`, MIME rows flipped, input capability bit. Landed with a
  cross-cutting engine fix: **data: image URIs decode without a resource loader**
  (`resource.LoadDataURL` short-circuits the image cache — the browser rule). `rtf-specimen`
  golden.

**RTF output** (`pkg/render/rtfwrite`, `WriteRTF`, `convert.. out.rtf`):

- Everything →.rtf — a cssbox STRUCTURE writer (boxwalk-based, the Markdown/DOCX shape) whose
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

**PPTX output** (`pkg/render/pptxwrite`, `WritePPTX`, `convert.. out.pptx`):

- Everything →.pptx — a cssbox STRUCTURE writer: every `<h1>`/`<h2>` starts a new slide with
  that heading as the title placeholder; following blocks become the body (text box paragraphs,
  `buChar`/`buAutoNum`+`lvl` lists, native `a:tbl` with `gridSpan`/`rowSpan` + `hMerge`/`vMerge`
  continuations, `p:pic` media parts with loaderless data:-URI embedding). Logged degrades:
  h3–h6 → bold paragraphs, quote/code flatten, links drop targets, hr skipped. Deterministic
  OPC (the gen-fixture package shape). Reopen-verified per-construct round trips through
  pkg/pptx + slide-count pin + `pptxout-basic` golden; PPTX joins the convert matrix as input
  AND output. Landed with a D1 frontend fix the round trip exposed: nested-list `<ul>` now
  opens INSIDE its parent `<li>` (structure writers previously dropped nested items).

**EPUB output** (`pkg/render/epubwrite`, `WriteEPUB`, `convert.. out.epub`)
— **completes the any⇄any table: all 13 formats are both inputs AND outputs**:

- Deterministic EPUB 3 built ON htmlwrite (content documents ARE XHTML — a new byte-identical
  `XHTML` mode self-closes voids, a new `ImageSrc` hook rewrites srcs during serialization):
  stored `mimetype` first entry, container.xml, OPF (title from option → first `<h1>` →
  "Document"; fixed dcterms:modified), `nav.xhtml` TOC, chapter split at `<h1>` (heading-less
  → one chapter), images as deduped manifest items via the loader seam (data: URIs stay
  inline and round-trip verbatim). Pinned by the STRICT parity bar — 17-case
  html→epub→md ≡ html→md exact equality — plus package-shape pins (stored-mimetype-first,
  nav links, chapters ⇒ pages), md→epub→md loop, `epubout-basic` golden; EPUB joins the
  convert matrix as input AND output.

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
  drawings inside w:hyperlink), pinned by test.

**PPTX input** (`pkg/pptx`, `OpenPPTX*`, `convert deck.pptx..`):

- Hand-rolled PresentationML reader: visible slides' shape trees (text frames with
  level/bullet/alignment + run b/i/sz/color, pictures, spanned tables), frames resolved
  through slide→layout→master placeholder inheritance; hidden slides skipped; animations/
  SmartArt/themes out of scope (content still extracts). Frontend renders one fixed-size
  page per slide (absolutely positioned frames; pictures as data: URIs; titles → h2;
  bullets → nested ul/ol with kind-switch handling), shapes ordered title-first/top-down
  for the structure writers. `classifyOPC` gains ppt/; `.pptx`/`.pptm`; presentationml MIME
  row flipped; input capability bit (output = D2). `pptx-specimen` golden.

**EPUB input** (`pkg/epub`, `OpenEPUB*`, `convert book.epub..` — reverses the old
out-of-scope note):

- Container reader: container.xml → OPF (title, manifest, spine; `linear="no"` skipped;
  EPUB 2/3 via the spine, NCX ignored); spine documents' body markup concatenates in reading
  order (each chapter `break-before: page` when paginated) with collected package CSS +
  inline styles; images/fonts/linked CSS resolve from the container through a loader adapter
  (the OPF-directory layout every real-world book uses; the dir-loader default is skipped so
  the container loader wins). DRM (META-INF/encryption.xml) → typed `epub.ErrEncrypted`.
  Detection: the OCF `mimetype` zip entry in classifyOPC; `.epub`; MIME row flipped; input
  capability bit (output = E2). `epub-specimen` golden.

**PNG/JPEG input — images as documents** (`OpenImage*`):

- The any⇄any principle applied to the image formats: an image opens as a single page exactly
  its pixel size (format stamped from the actual encoding via DecodeConfig; data:-URI embed
  through the reflow pipeline). image→PDF fills the page edge to edge (pinned), image→JPEG
  transcodes, markdown carries a data: URI, plain-text/tables-only outputs are empty by
  design; png→png stays ErrSameFormat. Input capability bits flipped; conversion matrix
  extended.

**HEIF/HEIC input — pure-Go HEVC intra decoder** (`pkg/heif`, `pkg/heif/hevc`):

- A from-scratch, in-tree HEVC intra-only decoder (CABAC, all 35 intra modes, residual
  coding with sign hiding, deblocking, SAO, WPP + tiles — the full toolset real HEIC
  encoders emit; Main/Main 10/Main Still, 4:2:0, 8/10-bit) beneath a HEIF container layer
  (grids of tiles, auxiliary alpha, irot/imir/clap, nclx colour). Bit-exact against
  reference decodes on a 42-fixture corpus; WPP rows/tiles decode in parallel with
  byte-identical output. Registered with `image.RegisterFormat`; `.heic` lights up as a
  document input, inside HTML/EPUB `<img>`, and transcodes to PNG inside DOCX/PPTX/RTF/EPUB
  outputs (`pkg/render/imageconv`). Image *sequences* (msf1) and AVIF stay refused.

**XLSX conditional formats + cell notes — calc-adoption PR 4/5** (`pkg/xlsx`):

- CF: one raw-fidelity read path for Workbook AND editor (`CFRule` typed fields + resolved
  dxf + VERBATIM `Raw` — data bars/color scales pass through byte-faithfully);
  `SetConditionalFormats` replaces wholesale (raw rules re-emit verbatim with renumbered
  priorities; typed rules mint deduped dxfs). Notes: `Comment` (1-based A1-space) read on
  both views; `SetComment`/`RemoveComment` regenerate the comments part + legacy VML
  wholesale, wiring rels/content-types/legacyDrawing on first use. Editor-core fixes:
  `sheetRelTarget` reads current bytes; `File.setPart` lets an original part be regenerated
  through the dirty machinery.

**XLSX pivots + defined-names write — calc-adoption PR 5/5** (`pkg/xlsx`):

- `PivotTables()` read (definition joined with its cache for source + field names),
  `RemovePivotTables()` (clean slate — parts, rels, workbook caches, CTs), `AddPivotTable`
  (cache w/ `refreshOnLoad` + empty records — definitions round-trip, values recompute; full
  wiring in one call; axis/value fields by source header name, hard error on unknowns).
  `SetDefinedNames`/`DefinedNames()` replace/read the workbook names (sheet-local + hidden).
  Editor-core fix: `setPart` resurrects a deleted part (remove-then-add in one session — calc's
  save shape).

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

**DOCX model — content controls, drawing title, note separators** (`pkg/docx`, additive
read+write vocabulary for the tinycld text adoption path):

- **`w:sdt` content controls unwrapped** — block, inline, and nested structured-document-tag
  wrappers have their `w:sdtContent` parsed transparently (as if the wrapper were absent) so the
  inner text/tables/lists survive parsing instead of being silently dropped. Empty/unbound
  controls drain cleanly and contribute nothing.
- **`Drawing.Title`** — `wp:docPr@title` parsed/written, distinct from `@descr`/`Description`
  (Word's alt-text dialog exposes both).
- **`RunProps.HighlightName`** — the raw `w:highlight` name (e.g. `darkGreen`) preserved so a
  consumer can apply its own palette; the writer prefers it over remapping the resolved RGBA.
- **`VerbatimChar`** character style in `DefaultStyles` (the pandoc/HTML-export inline-code
  synonym of `CodeChar` other tools recognize on read).
- **`Run.NoteSep`** — the reserved footnote/endnote separator notes (ids -1/0) round-trip:
  `<w:separator/>` / `<w:continuationSeparator/>` write AND parse back, so a content-less
  separator run is a Parse∘Write fixed point rather than being culled.
- **val-less `<w:u>` fix** — a bare `<w:u w:color=../>` (Word's shorthand for single underline)
  now reads as underline-ON; it was previously read as underline-off.

**Page geometry + fit-within raster sizing** (`pkg/doctaculous`, CLI `--max-width/--max-height`):

- `Document.PageSize(i)` (points, post-/Rotate for PDF — always the rendered aspect);
  `RasterOptions.MaxWidthPx/MaxHeightPx` fit-within-box sizing resolved per page to a concrete
  DPI above the backends (painting untouched — fit ≡ explicit-DPI, pixel-identical, test-pinned;
  ceil-safe exact fits). DPI becomes a resolution CEILING alongside the box (zero = fill the
  box, upscaling vector-sharp; positive = downscale-only thumbnails). CLI flags on `rasterize` +
  `convert` image output (unset `--dpi` = pure fit via flag.Visit).

**Exact-size cropping incl. classical saliency** (`pkg/crop`, `ImageOptions.Crop`, CLI
`--crop/--crop-size`):

- Fit-within sizing preserves aspect, so it can never fill a non-matching target (a 4:3 source into
  720×720 comes back 720×540). `crop.Rect`/`crop.Scale` fill the box instead: center + N/S/E/W
  gravities, an explicit caller-supplied rectangle (`StrategyRect`), and a content-aware
  `StrategySaliency`. `ImageOptions.Crop` is a nil-able pointer applied inside `EncodeImage`, so
  every existing caller stays byte-identical; `WriteImage` and `Convert` both honour it.
- Saliency is classical — no model, no training data, pure Go: Sobel edge energy, HSV-style
  saturation, a deliberately wide luma-independent YCbCr skin box, and a radial centre prior,
  summed into a per-pixel score map and evaluated over candidate windows through a summed-area
  table (O(1) per candidate), fanned out across `GOMAXPROCS`. Ties resolve to the centred window
  and every worker seeds from one immutable candidate, so results are deterministic and race-free.
  Scoring weights are pointers (`crop.Weights`) so an explicit zero disables a term — a plain
  float64 cannot distinguish "off" from "unset". Skin weight defaults *below* edge weight
  deliberately: published YCbCr skin ranges underweight darker skin tones, so skin nudges the crop
  rather than deciding it.
- `crop.ScaleRect`, `EncodeImageRect` and `Document.WriteImageRect` report the source rectangle
  that was cropped. For saliency the window is chosen from image content, so it cannot be derived
  from the options; feeding the reported rect back as `Options.Rect` with `StrategyRect` replays a
  byte-identical crop, which is what lets a caller persist a smart crop.
- Golden crop rectangles over a committed CC0 photograph back the synthetic direction tests, which
  cannot catch a quality regression on real image statistics.

**SVG input — core scene graph** (`pkg/svg` parse, `pkg/svg/draw` scene → `render.Device`;
`OpenSVG*`, `convert in.svg..`):

- Standalone `.svg` and gzipped `.svgz` documents open as a single vector page. Detection fixes a
  pre-existing bug: an XML-prologed SVG (`<?xml ..?>` before the root) used to mis-sniff as HTML;
  content sniffing, the `.svg`/`.svgz` extensions, and `image/svg+xml` MIME all now route correctly,
  including a namespace-prefixed `<svg:svg>` root.
- Full path `d` grammar (all commands, relative/absolute, implicit repeats) with arcs and quadratics
  converted to cubics; the six basic shapes (`rect`/`circle`/`ellipse`/`line`/`polyline`/`polygon`);
  transform lists; `viewBox` + `preserveAspectRatio`. Solid fill/stroke via inherited presentation
  attributes with full CSS color syntax (`fill-rule`, opacities, dashes, caps, joins, miter limit).
- Rasterization renders at device resolution; PDF output keeps REAL VECTORS via a new
  `layout.VectorKind` item rather than rasterizing to an embedded image (an SVG circle → PDF has no
  image XObject). Landed with a cross-cutting fix in the shared rasterizer: an unclosed subpath used
  to fill incorrectly (also affecting the PDF content interpreter's `f` operator on unclosed paths).
- Group opacity (`<g opacity>`, including on the root `<svg>` element, and nested groups multiplying)
  composites correctly via a new `render.Device.BeginGroup`/`EndGroup` offscreen-compositing
  primitive (raster backend only; pdfwrite is a logged pass-through pending Form XObject support):
  overlapping children inside an opacity group blend once, at the flattened result, instead of each
  child's own paint alpha double-darkening the overlap. The same primitive fixes the analogous
  double-paint case on a single shape carrying both a fill AND a stroke at element `opacity` < 1 (the
  stroke's inner edge overlaps the fill) — routed through a group only when both a fill and a stroke
  are present and opacity < 1, so the common opaque/single-paint shape stays on the cheap per-paint
  path with no offscreen allocation.
- `clip-path`/`<clipPath>`: a clipPath's children form a UNION (not an intersection), flattened via a
  new `render.Device.BuildClipMask(paths []MaskPath) GroupMask` primitive that rasterizes EACH child
  under its OWN `clip-rule` and combines coverage with `max()` — the correctness-critical design
  point, since two non-overlapping children pushed as separate `PushClip` calls would intersect to
  empty. `clipPathUnits` (`userSpaceOnUse` default, `objectBoundingBox` reusing the same
  pre-transform-`Path.Bounds()` mapping gradients use), `clip-rule` (new inherited property, per-child,
  mixed nonzero/evenodd within one clipPath), a `clip-path` on the `<clipPath>` element itself
  (intersects the whole union) or on one of its children (intersects that child before it joins the
  union), and an explicit shape/`<text>`/`<use>` allowlist for valid children (a `<g>`/`<image>`/
  `<switch>` child is dropped, not recursed into as a forgiving container) all ship. An empty
  `<clipPath>` (no valid children) clips its target to NOTHING, distinct from `clip-path="none"`/an
  unresolved reference (no clipping at all). `display:none` removes a clip child from the union;
  `visibility:hidden` does not (clip children have no rendering pass of their own for visibility to
  gate). fill/stroke/opacity/filter/mask on a clip child have no effect — only geometry, transform,
  clip-rule, and clip-path matter. Resolved during `Parse` (like paint servers), with a
  `buildingClip`-style recursion guard so a self-referencing or mutually-cyclic clipPath terminates.
  raster implements `BuildClipMask` exactly (per-child rasterize + max union); pdfwrite — which has no
  offscreen surface to rasterize into and whose group compositing is still a pass-through stub —
  returns a documented rectangular bounding-box approximation instead of an empty/nil mask, ready for
  when transparency-group compositing lands there.
- `mask`/`<mask>`: the mask's rendered content's LUMINANCE (not its geometry) becomes per-pixel alpha,
  via a new `render.Device.BuildLuminanceMask(size, alphaOnly, paint func(dev Device)) GroupMask`
  primitive — the backend hands back a scratch surface, `pkg/svg/draw` paints the mask's subtree into
  it through the ordinary `Device` seam (never importing a concrete backend), and the backend converts
  the result to a mask. Default sRGB luminance (`0.2126R+0.7152G+0.0722B`, times the pixel's own
  alpha) rather than SVG 1.1's linearRGB, matching browsers/SVG2/resvg (following the letter of SVG
  1.1 here would make every mask golden visibly wrong). `mask-type` (SVG2, new non-inherited property
  in both `hints.go` and `style.go`) selects `luminance` (default) or `alpha` (reads the pixel's own
  alpha channel directly). `maskUnits` (`objectBoundingBox` default, `-10%/-10%/120%/120%` region —
  a real 10% bleed past the masked element's bbox) and `maskContentUnits` (`userSpaceOnUse` default —
  the OPPOSITE default from `maskUnits`) both ship, reusing the clip-path objectBoundingBox mapping. A
  `transform` on the `<mask>` element itself is ignored (only a transform on the masked element
  applies); an empty `<mask>` (no children) makes its target FULLY TRANSPARENT, distinct from
  `mask="none"`/an unresolved reference (no masking at all). Mask, clip-path, and opacity compose in
  that order (clip → mask → opacity) via mask intersection before a single `EndGroup` call. Nested
  masks (a mask referencing another mask) and a self-referencing/cyclic mask chain (a `buildingMask`
  recursion guard mirroring `buildingClip`) both terminate. Resolved during `Parse`, like clip-path;
  raster implements `BuildLuminanceMask` exactly (renders into a scratch `*image.RGBA`, converts per
  pixel); pdfwrite — which has no offscreen surface yet — returns `nil` (no masking) with a logged
  fidelity note, pending the luminosity soft-mask work.
- Not yet, each degrading with a `WithLogf` debug line rather than failing: `<use>`, text, filters,
  `<image>`, and inline `<svg>` inside HTML/`<img src=*.svg>` — tracked as the PR 5–8 slices in
  `docs/superpowers/specs/2026-08-25-svg-support-design.md`.
- 148 curated fixtures from the resvg test suite (MIT, commit `d8e064337faf01bc5a9579187a56dbdbe3eacc72`)
  with committed goldens; clip-path and mask are covered by hand-authored fixtures/tests in `pkg/svg`,
  `pkg/svg/draw`, and `pkg/render/raster` (no resvg `masking/**` tranche is vendored yet).

**SVG input — CSS styling** (`pkg/svg`, `pkg/css`):

- An SVG-local cascade (`pkg/svg/cascade.go`) mirroring `pkg/css/cascade.go`'s ladder minus the UA
  origin: `<style>` sheets (including CDATA-wrapped rule bodies and a non-CSS `type=` correctly
  skipped), the `style=""` inline attribute, `class`, and presentation attributes folded in as the
  lowest-priority cascade origin. Selectors: type (case-insensitive match, authored case preserved
  so `linearGradient`/`clipPath` etc. stay addressable), class, id, universal, descendant, and
  grouping, with full specificity comparison and `!important`. `element` gained parent/id/class
  tracking and a `css.Node` adapter so the shared selector matcher runs unmodified over the SVG
  tree; a document index pre-pass collects stylesheets (and an id/defs table used by later slices)
  once per document rather than per element. `Style.apply` resolves every presentation property
  (fill/stroke variants, opacity, fill-rule, linecap/linejoin/miterlimit, dasharray/dashoffset,
  display, visibility, color) through the cascade; `transform` and shape geometry stay XML-attribute-
  only, matching SVG 1's separation of presentation from geometry.
- Known gaps that fail safe rather than mismatch: attribute selectors (`[foo]`) and the
  combinators (`>`, `+`, `~`) parse without erroring but never match (the selector engine has no
  handling for either, so they parse into an inert simple selector); `@import` is recognized and
  skipped with a debug log rather than fetched. The selector gaps are shared with HTML (`pkg/css`)
  and are tracked as planned work — see CLAUDE.md's roadmap item 8, "CSS selector coverage".
- Landed two shared `pkg/css` fixes that also apply to HTML: `!important` is now recognized with no
  preceding whitespace (`red!important`), and `/* */` comments inside a `style=""` attribute value
  are stripped before parsing, matching what a `<style>` sheet's rule body already did.
- 13 curated fixtures from the same resvg test suite covering selector kinds, specificity,
  `!important`, cascade order, and CDATA — with committed goldens.

**SVG input — paint servers** (`pkg/svg` gradient/pattern resolution, `pkg/svg/draw` fill dispatch,
`pkg/render/raster` shading, `pkg/render/pdfwrite` native `/Shading` emission):

- `<linearGradient>` and `<radialGradient>`, both `gradientUnits` (`objectBoundingBox` default and
  `userSpaceOnUse`, including percentages in either), `gradientTransform`, and all three
  `spreadMethod` values (`pad`/`reflect`/`repeat`). `<stop>` parsing: `offset` as a number or
  percentage (clamped to `[0,1]`, non-decreasing across the list — an out-of-order stop clamps
  forward rather than sorting), `stop-color` (full color grammar plus `currentColor`, resolved
  against the stop's own `color`), and `stop-opacity` composed in as REAL alpha (a fading stop shows
  whatever is behind the shape, not a black composite).
- `xlink:href`/`href` reference chains: per-attribute inheritance (nearest element wins, walking
  outward), all-or-nothing stop inheritance, cross-type href (a `linearGradient` hrefing a
  `radialGradient` or vice versa) inheriting attributes/stops but painting as the referencing
  element's own kind, and cycle-safe chain walking (2-cycle, 3-cycle, and self-referencing chains all
  terminate and degrade to "own stops win" or "paints nothing" rather than looping).
- `<pattern>`: `patternUnits`/`patternContentUnits` in both unit systems (with percentages), a
  `viewBox` on the pattern (taking precedence over `patternContentUnits` when both are set, and
  resolvable through an href chain), `preserveAspectRatio`, `patternTransform` composing correctly
  with the referencing shape's own `transform`, `x`/`y` cell offset, and attribute/child inheritance
  via href (including a pattern nested inside another pattern's tile). A self-referencing tile or a
  mutual two-pattern cycle terminates via a build-time guard, degrading to "unpainted fill, the
  tile's own stroke still shows" rather than recursing forever.
- An unresolved `url(#id)` reference with no fallback color paints nothing (not the inherited solid
  color) — a real bug fixed while building this: `Style.applyPaint` now clears `hasFill`/`hasStroke`
  for a still-unresolved reference instead of leaving the previous cascade value in place.
- PDF output emits a native `/Shading` dictionary for an axial or radial gradient whose stops are all
  fully opaque and whose `spreadMethod` is `pad` (`render.ShadingDescriber`, a Shader-optional
  companion interface `pkg/render/raster`'s SVG-built shadings implement and `pkg/svg/draw`'s
  `alphaShader` delegates through): `/ShadingType 2`/`3`, `/Coords`, `/ColorSpace /DeviceRGB`,
  `/Extend [true true]`, and a `/Function` — a single `FunctionType 2` (exponential, linear) for two
  stops, or a `FunctionType 3` stitching function over one `FunctionType 2` per segment for more,
  painted with `sh` under the shape's existing clip. Coincident stop offsets (a hard color break) are
  nudged apart by a sub-point epsilon so `/Bounds` stays strictly increasing without visibly smearing
  the break. Proven by rendering an opaque multi-stop gradient two ways (SVG→raster directly, and
  SVG→PDF→reopen→raster) and asserting pixel-for-pixel equivalence, not just a well-formed dictionary.
  A gradient with any `stop-opacity` < 1 (no alpha channel in `/Shading` without a soft mask) or a
  `reflect`/`repeat` spread (no native `/Extend` equivalent) still rasterizes into an image XObject
  exactly as before, logged once with the reason — the previous stub-era "no vector output" gap is
  now the correctness boundary, not a permanent limitation.
- Known gaps, each verified by rendering rather than merely inferred, and excluded from the golden
  corpus rather than locked in as correct: **gradient/pattern strokes** (`stroke="url(#g)")`) degrade
  to the paint's fallback color (or no stroke) with a one-per-document warn-once log — no
  stroke-to-outline conversion exists in `pkg/render/raster/stroke.go` to clip a shading or tile
  against; **SVG2 `fr`** (radial focal radius) is not read at all; a radial gradient's focal point
  is not projected onto the `r` circle boundary when `fx`/`fy` lies outside it, per spec; a radial
  gradient with `r="0"` does not yet paint the spec-required solid fill of the last stop's color;
  `<pattern overflow="visible">` is not honored (every tile clips to its own cell); and a `<stop>`'s
  `currentColor`/`inherit` only ever resolves against the stop's own attributes, never a real
  ancestor's `color`/`stop-color` (`pkg/svg/stops.go`'s `resolveStopColor` has no inherited-style walk
  from a stop up through its parent gradient or an enclosing `<g>`).
- 110 curated fixtures from the same resvg test suite covering both gradient types, patterns, stop
  parsing, and the reference-chain/cycle machinery above — with committed goldens.
