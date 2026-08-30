# Features

The complete inventory of shipped features, validated against a real-world corpus
(`testdata/external/`). Keep this list current as features land: every feature that ships gets a
bullet here in the same PR. Each bullet is a one-line pointer; the detailed design/rationale for a
sub-project is in the commit and PR history. What's *next* — the TODO
list and known approximations — lives in the per-subsystem docs: [docs/PDF.md](docs/PDF.md),
[docs/DOCX.md](docs/DOCX.md), [docs/CSS-LAYOUT.md](docs/CSS-LAYOUT.md), and [docs/SVG.md](docs/SVG.md).

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
  A system match is **verified against the face's own `name` table** (`Face.FamilyName`) and
  rejected when it names a different family: `sysfont.Match` never reports a miss, so without the
  check a request for an absent family came back as some unrelated installed font (measured: `DejaVu
  Sans` → Lucida Grande; `Roboto`, `IBM Plex Mono` and a nonexistent name → the same Arial Unicode
  bytes). A declared name may extend the request with style words (`Barlow Condensed SemiBold`
  satisfies `Barlow Condensed`), but a merely-shared prefix does not (`Times New Roman` ≠ `Times`).
  A face that declares no readable family is accepted rather than rejected. **This does not find
  fonts the matcher cannot identify at all** — it identifies installed files by filename against a
  fixed registry, so an unregistered family (Roboto, Barlow, IBM Plex on many hosts) stays unfound;
  `@font-face` with `url()` is the reliable route for non-standard families.
- **Font-family terminal fallback** (`pkg/layout/font/cache.go`): a `font-family` list where **no**
  candidate resolves degrades to the bundled serif and logs once per (list, style), instead of
  resolving to nothing and having the caller skip the run — which rendered a page whose every
  family was unavailable as an empty box. The fallback is style-aware (bold/italic select the
  matching bundled face). A list ending in a generic keyword is unaffected; showcase §29.
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
- **Custom properties + `var()`** (CSS Variables 1; `pkg/css/customprop.go`, `varsubst.go`):
  `--*` properties cascade by the normal rules and INHERIT, stored as raw token streams (the
  treatment `filter` already gets) and substituted at computed-value time. Substitution sits
  between the cascade and value parsing, so `var()` works in every property including
  shorthands — `border: var(--rule)` expands normally once substituted. Supports fallbacks
  (`var(--x, blue)`), nested fallbacks, recursive substitution (`--a: var(--b)`), and the
  case-sensitivity rule that makes `--Foo` and `--foo` distinct. Cycles are detected exactly
  (an active-reference set, not a depth guess) and a non-cyclic exponential fan-out is bounded
  by a depth cap. An unresolvable reference is **invalid at computed-value time**, not a
  dropped declaration: per spec the property falls back to its inherited-or-initial value as
  though `unset` were specified, rather than leaving an earlier declaration showing — the one
  case where this engine must NOT treat a bad value as "keep the previous one". `:root` also
  landed here (`pkg/css/selector.go`), since it is where a palette is normally declared.
- **HTML frontend — box generation** (`pkg/html`, `pkg/layout/cssbox`): owned DOM, UA stylesheet,
  anonymous-box fixups, whitespace collapsing, `display:none` pruning; `<link>` via
  `pkg/resource.ResourceLoader`.
- **Block + inline normal flow** (`pkg/layout/inline`, `pkg/layout/css/block.go`+`inline.go`,
  `pkg/layout/paint`, `OpenHTML`/`OpenHTMLBytes`): box model (width/`auto`/%, `box-sizing`,
  min/max, margins incl. vertical collapsing, padding, borders, backgrounds), IFC (shaping/breaking,
  `text-align`, `line-height`), fragment tree.
- **Replaced content + images** (`pkg/layout/css/image.go`+`replaced.go`): `<img>` decode (PNG/JPEG/
  GIF stdlib, HEIC via `pkg/heif`, WebP via `pkg/webp`) → CSS replaced-sizing → paint via
  `DrawImage`, with `object-fit`/`object-position`.
- **Floats + clear** (`pkg/layout/css/floats.go`): per-BFC float context, narrowing/wrapping,
  `clear`, own paint layer.
- **Positioning** (`pkg/layout/css/positioning.go`): relative (paint-time offset) + absolute/fixed
  (out-of-flow, two-pass against containing block), stacking contexts.
- **Overflow clipping** (`pkg/css` `overflow`, `layout.ClipPush/PopKind`): clip to padding box +
  BFC establishment + deferred float interactions. All four clip keywords are honored —
  `hidden`/`scroll`/`auto`/**`clip`** (`clip` differs only in forbidding programmatic scrolling and
  allowing `overflow-clip-margin`, neither of which exists in the single-tall-page model) — as are
  **`overflow-x`/`overflow-y`** and the **two-value shorthand**, which fold onto the one clip flag
  this engine models with the *clipping* keyword winning when the axes disagree (a deliberate
  over-clip on `visible hidden`; dropping the clip is the worse error). Showcase §31.
- **`max-height`/`min-height` on auto-height blocks** (`pkg/layout/css/block.go` `clampAutoHeight`):
  `max-height` previously applied only on the fixed-height path, so `max-height` *without* `height`
  never bounded anything and a clip built from that height clipped nothing. It now clamps the auto
  height after float enclosure and before the clip rect, so box, clip, and parent advance agree;
  `min-height` applies after `max-height` per CSS 10.7. Anonymous boxes are exempt — they copy the
  parent's computed style for inherited text properties but have no properties of their own
  (CSS 9.2.1.1), and clamping them truncated boxes the author never sized.
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
- **`overflow-wrap` / `word-break` — mid-word line breaking** (`pkg/css/cascade.go`,
  `pkg/layout/inline/wordbreak.go` + `grapheme.go`): `overflow-wrap: normal | break-word |
  anywhere` (plus the legacy `word-wrap` alias, which sets the same property) and `word-break:
  normal | break-all | keep-all`, both inherited. `break-word`/`anywhere` break inside a word only
  as a LAST RESORT — a word that fits on a line of its own is moved down whole — while
  `break-all` is eager and chops at the line edge regardless; `keep-all` suppresses mid-word
  breaking. `anywhere` and `break-all` additionally shrink intrinsic **min-content** width (CSS
  Text 3 §5.5) while `break-word` deliberately does not, which is the only difference between
  `break-word` and `anywhere`. Every break lands on a **grapheme-cluster boundary** — full UAX #29
  extended clusters (GB3–GB13, including emoji ZWJ sequences and regional-indicator flag pairs),
  using the UCD tables already vendored with `benoitkugler/textlayout`, so no combining mark, jamo,
  or emoji is ever split. `white-space: nowrap` outranks all of it. Untouched callers (DOCX, SVG
  text, any page not setting these) keep the whitespace-only breaking path byte-identical.
- **`letter-spacing` / `word-spacing` on the CSS text path** (`pkg/css/cascade.go`,
  `pkg/layout/inline/shape.go`, `pkg/layout/css/inline.go`): `letter-spacing: normal | <length>` and
  `word-spacing: normal | <length>`, both inherited, both accepting NEGATIVE lengths (which tighten)
  in `px`/`pt`/`em`. `letter-spacing` is added after **every** typographic character unit including
  the last one on a line — matching Chrome/Firefox/Safari rather than CSS Text 3's literal "between"
  wording, so a right-aligned tracked line correctly stops one tracking-width short of the edge.
  `word-spacing` is added only at word separators (U+0020 and U+00A0, per CSS Text 3 §8.2); U+00A0
  takes the spacing without becoming a break opportunity. `normal` is modeled as the zero length —
  the spec's only distinction is latitude this engine's justifier never takes — so it correctly
  RESETS an inherited value. Both are resolved against the run's own font size and folded into
  `Glyph.Advance` at shaping time, so **line breaking, min/max-content sizing, tab stops, and
  justification all compose with no change to any of them**: a justified line still lands flush
  because word-spacing widens the gaps before the slack is computed. Per-glyph advances are floored
  at zero so a large negative tracking overlaps glyphs (as browsers do) rather than producing
  negative advances the greedy breaker cannot represent. Bidi control marks stay zero-width.
  Untouched callers (DOCX, PDF, any page not setting these) are byte-identical.
  Two limits recorded rather than implied: tracking is applied to cursive scripts, where CSS Text 3
  says it should be suppressed for joined runs (harfbuzz join data is not surfaced through this
  engine's complex-shaping result); and these properties still do **not** inherit into an inline
  `<svg>` — nothing does, because inline SVG is replaced content re-parsed in isolation (see
  `docs/SVG.md`).
- **List markers + CSS counters** (`pkg/css/counter_format.go`, `pkg/layout/css/counters.go`,
  `pkg/font/bullet.go`): `list-style-*`, `counter-reset`/`-increment`/`-set`, `content: counter()`;
  synthetic bullet outlines.
- **`background-image`** (`pkg/css/background.go`, `pkg/layout/css/background.go` + paint):
  `url(..)`, `-repeat`/`-position`/`-size`/`-origin`/`-clip`.
- **The `background` shorthand validates before it commits** (`parseBackgroundShorthand` in
  `pkg/css/shorthand.go`): every component must classify, and if any one does not the WHOLE
  declaration is discarded with no longhand touched — CSS 2.1 §4.2 and CSS Syntax 3, which
  treat an invalid declaration as never having entered the cascade. This is what makes the
  standard fallback idiom work (`background: red` then `background: linear-gradient(…)` leaves
  red standing on an engine that cannot parse the gradient); an expander that reset first and
  tolerated unknown components would turn every such fallback transparent. Verified against
  Blink and Gecko, including the case where a bad component sits beside a good one
  (`notacolour url(x.png)` applies neither).
- **CSS gradients as a background image** (`pkg/css/gradient.go`, `pkg/layout/gradient.go`,
  `pkg/layout/paint/gradient.go`): `linear-gradient()`, `radial-gradient()` and both
  `repeating-*` forms. Linear takes an `<angle>` in any CSS unit (`deg`/`grad`/`rad`/`turn`),
  `to <side>`, or `to <corner>` — the corner case computes the spec's aspect-dependent
  gradient line (perpendicular to the box's other diagonal), **not** 45°, which is only
  right on a square. Radial supports `circle`/`ellipse` crossed with
  `closest-side`/`closest-corner`/`farthest-side`/`farthest-corner`, explicit
  `<length-percentage>` radii, and `at <position>`. Colour stops take positions in `%` or
  lengths, with the full CSS normalization: omitted endpoints default to 0%/100%, an
  unpositioned run is spread evenly between its bracketing stops, and a decreasing position
  is corrected UP to the running maximum (a forward clamp producing a hard break — never a
  sort, which would reorder the author's colours). Two stops at one position give a hard
  colour break. Interpolation is in **premultiplied alpha**, matching browsers: fading to
  `transparent` stays in its own hue instead of showing the grey/black band a straight-RGBA
  ramp produces.
  Gradients paint through the **same shading seam SVG paint servers use**
  (`raster.NewAxialShader`/`NewRadialShader` → `render.Device.FillShading`), so they are
  evaluated per device pixel rather than baked to a bitmap, and the PDF writer emits a native
  `/Shading` dictionary for them. A gradient has no intrinsic size, so it takes the
  background-origin box's — which makes `background-size`/`-position`/`-repeat`/`-origin`/
  `-clip` all apply to it through the unchanged geometry path (its geometry is resolved in
  TILE space, so a resized gradient really is resized).
  **Degrades honestly:** a colour hint (a bare `<length-percentage>` between two stops)
  needs a non-linear ramp the shared seam cannot express and is REJECTED at parse time, as
  are `conic-gradient()`, a unitless angle, and a one-stop list — the declaration is dropped,
  so the background colour still paints rather than a subtly wrong ramp appearing. An ending
  shape with a zero radius (e.g. `closest-side` centred on a box corner) establishes no
  geometry: nothing paints, the background colour remains, and the skip is logged via
  `warnOnce`.
- **CSS Color 4 colour values — ONE grammar for the whole engine** (`pkg/css/color.go`; `pkg/svg`
  delegates to it): the full 148-keyword named table (via `golang.org/x/image/colornames` +
  `rebeccapurple` + `transparent`), all four hex forms (`#rgb`/`#rgba`/`#rrggbb`/`#rrggbbaa`), and
  `rgb()`/`rgba()`/`hsl()`/`hsla()` in both the legacy comma syntax and the modern space syntax with
  `/` alpha, with integer or percentage channels. Alpha is LIVE end to end — parsing yields a
  `color.RGBA` the painter hands to the device unchanged and the rasteriser composites (verified by
  pixel: `background:rgba(0,0,0,0.9)` on an 80×80 box went from 0 painted pixels to 6400).
  Previously `pkg/css` had a hand-written parser covering only `#rgb`/`#rrggbb`/`rgb()` and eight
  keywords while `pkg/svg` carried a complete implementation, so any alpha-bearing value failed the
  cascade, the declaration was dropped per CSS error handling, and the element painted *nothing*.
  Merged into `pkg/css` (not the reverse) because `pkg/svg` already depends on it and `pkg/css`
  depends on no internal package. Malformed values still yield `ok=false` so the declaration drops
  and the prior value stands — through the `background` shorthand as well as through the
  longhands (`background-color`, `color`, `border-*-color`).
- **`border-radius`** (`pkg/css/borderradius.go`, `pkg/layout/borderradius.go`,
  `usedRadii` in `pkg/layout/css/block.go` + paint): CSS Backgrounds 3 §5 in full — the shorthand
  (1–4 values in CORNER order, i.e. diagonal pairing, not `expandBox`'s clockwise side rule), the
  `/` form for elliptical corners, all four longhands, and percentages. A corner's two semi-axes
  resolve against DIFFERENT bases (horizontal against the border box's width, vertical against its
  height), which is why radii stay unresolved `Length` pairs until layout. The §5.1 overlap
  correction scales all eight components by one shared factor `f = min` over sides, so
  `border-radius:100px` on an 80×80 box yields a true circle rather than four separately-clamped
  arcs. Backgrounds fill the rounded path directly (so the backend antialiases the arcs itself);
  background IMAGES are bracketed by a rounded clip, since `DrawImage` has no shape parameter.
  Borders paint as one even-odd RING (outer rounded rect minus inner), the inner radius being the
  outer minus the border width floored at zero — a uniformly-thick rounded border is NOT the outer
  path stroked, and a border thicker than its radius correctly squares the inner corner while the
  outside stays round. PDF keeps real curves (`pdfwrite` emits the same Béziers as `c` operators
  natively, for both the fill and the `W n` clip); DOCX is unaffected, as that writer builds a
  document model rather than painting and has no rounded-box primitive to target.
  **Degradations, logged by the layout engine and covered by tests:** a rounded border is filled in
  ONE colour as SOLID, so per-side border colours and the non-solid styles (dashed/dotted/double/
  ridge/groove/inset/outset) are approximated on a rounded box — square-cornered boxes still paint
  four fully-styled strips and are byte-identical.

- **`box-shadow`, outer and `inset`** (CSS Backgrounds 3 §6 — `pkg/css/boxshadow.go`,
  `pkg/layout/css/boxshadow.go`, `pkg/layout/paint/boxshadow.go`). The full grammar, including the
  `&&` combinator: `inset`, the 2–4 lengths and the colour may appear in **any order**, so
  `inset red 2px 2px` and `2px 2px inset red` are the same shadow. Comma-separated **lists** paint
  in the spec's order — the **first shadow is on top** — and CSS error handling is the engine's
  usual: one malformed entry invalidates the whole declaration (a negative *blur* is an error; a
  negative *spread* is legal and shrinks the shadow). An omitted colour, and the `currentColor`
  keyword, resolve to the element's own `color` at layout time, where the cascade is reachable.
  **`inset` is a genuinely different rendering, not a sign flip**: an outer shadow fills the region
  OUTSIDE the border box (so a transparent box shows a ring, never a filled blob) while an inset one
  fills the part of the PADDING box its own shape does not cover and can never escape the box,
  however far it is offset — and its spread sign is inverted, because shrinking the lit interior
  thickens the band. The two also sit in **different slots of the paint order** (outer behind the
  background, inset over both backgrounds but under the border), so a list carrying both shows both.
  The blur is `sigma = radius/2`, per the spec's "the shadow's edge transitions over a distance
  equal to the blur radius, centred on the edge", and it runs through **`pkg/svg/filter`'s existing
  `feGaussianBlur`** — the repo has exactly one blur implementation, shared by SVG filters, the CSS
  `filter` shorthand and now this. Square corners only: `border-radius` is not implemented, and the
  single integration point for it is documented at `paint.shadowOutline`.
- **A `box-shadow` with no blur stays fully vector, including in PDF** — it is a plain even-odd
  fill of the box's shape, so the common patterns (a hard offset, a spread ring, an `inset` colour
  spine) cost no rasterization anywhere. A **blurred** shadow needs an offscreen raster surface via
  `render.Device.RenderOffscreen`, which `pkg/render/pdfwrite` returns nil from by design (PDF has
  no blur operator and a blur has no vector representation). There — and whenever the surface would
  be degenerate, off-canvas, or over the per-shadow pixel cap — the shadow **degrades to the same
  shape with a HARD edge**, at the same place and size. That is the "a visible approximation beats a
  blank" rule the CSS `filter` path already follows, and deliberately NOT a rasterization of the
  page. **That degradation is currently SILENT**: `pkg/layout/paint` has no logger to report it
  through (`PaintPage` takes only a Device, a Page and a Matrix), exactly as the CSS-filter
  pixel-cap degradation in the same package does. **DOCX output carries no shadow at all** —
  `pkg/render/docxwrite` consumes the `cssbox` tree directly rather than the painted item list, so
  it never sees a shadow item and has no `box-shadow` analogue to map one onto.
- **Link pseudo-classes + `text-decoration: underline`** (`pkg/css/selector.go`, `pkg/html/ua.go`):
  `:link`/`:visited` + general pseudo-class parsing.
- **Inline emphasis UA defaults** (`pkg/html/ua.go`): `strong`/`b` bold, `em`/`i`/`cite`/`var`/`dfn`
  italic, `u`/`ins` underlined — each resolving to the same computed style as its CSS equivalent,
  and nesting without flattening. Previously these tags were structurally present (and survived
  conversion to Markdown) but visually identical to plain text in every rasterized format. The sheet
  spells the weight `bold`, not the spec's `bolder`: the cascade's `font-weight` is the binary
  bold/normal the four-style bundled families can express and rejects the relative keywords, so
  `bolder` would be dropped as invalid and the emphasis would stay invisible (a test pins this).
  `<small>`/`<big>` are omitted rather than given a hardcoded px size. `<mark>` carries the standard
  yellow highlight (it landed with inline-box backgrounds below; before that the rule would have
  cascaded and painted nothing). Showcase §30.
- **`transform`** (`pkg/css/transform.go`, `pkg/layout/css/fragment.go`): the 2D functions —
  `translate`/`translateX`/`translateY` (lengths and percentages of the box's own size),
  `scale`/`scaleX`/`scaleY`, `rotate`, `skew`/`skewX`/`skewY`, and `matrix()` — composed left to
  right. It is a PAINT-time effect and changes no layout (CSS Transforms 1 §3): the box keeps the
  space it occupied, and the matrix brackets its already-flattened items. A transformed element
  establishes a stacking context and a BFC, as the spec requires — which is also what lets the
  bracket wrap its background and content together rather than splitting them across Appendix E's
  phases. Not modeled: the 3D functions (`translate3d`, `rotateX`, `perspective`, `matrix3d`),
  refused rather than flattened since the engine has no 3D pipeline; and `transform-origin`, which
  is always the box centre. Showcase §42.
- **Absolute positioning in flex containers, and flex-derived heights** (`pkg/layout/css/flex.go`,
  `block.go`): an abs/fixed child of a flex container is out of flow and honours its offsets (CSS
  Flexbox §4.1) rather than being laid out as a flex item pinned to the edge; `top`+`bottom` with
  `height: auto` sizes the box to the space between them (CSS 10.6.4), matching what `left`+`right`
  already did; and an element whose height comes from flex layout (`align-items: stretch` or its own
  `flex: 1`) resolves `justify-content` for its own children, because the cross size is now definite
  BEFORE its interior lays out rather than written onto the fragment afterwards. Showcase §41.
- **SVG presentation attributes inherit from the root** (`pkg/svg/svg.go` `rootStyle`): `fill`,
  `stroke`, `stroke-width`, caps/joins/dashes and the rest of the inherited vocabulary set on the
  root `<svg>` reach its children, as CSS inheritance requires. Previously only the font and text
  properties were carried across — the root's paint properties were resolved and then discarded —
  so an icon authored as `<svg stroke="…">` with detail paths inheriting it painted its filled
  parts and none of its strokes. The set is built by INVERSION: start from the root's resolved
  style and clear only what CSS marks non-inherited (`opacity`, `clip-path`, `mask`, `filter`,
  `mask-type`, `overflow`, `display`), so a property added later defaults to inheriting rather than
  to being dropped. Showcase §40.
- **Comma-separated `background` / `background-image` layer lists** (`pkg/css/shorthand.go`,
  `pkg/layout/css/background.go`): multiple layers paint, first layer on top, with the
  background-color behind them — so `background: <gradient>, <color>`, the ordinary way to give a
  gradient a fallback, works. It previously made the whole declaration unparseable, so the element
  painted NOTHING and it read as "gradients are unsupported". A colour is accepted in the final
  layer only (CSS Backgrounds §3.10) and one earlier invalidates the declaration, as does any
  unparseable layer. Known limit: `background-size`/`-repeat`/`-position`/`-origin`/`-clip` are
  single-valued and apply to every layer — genuinely per-layer values are a separate slice.
  Showcase §39.
- **Margins on flex children** (`pkg/layout/css/flex.go`): honoured on both axes, including
  `margin: auto` absorbing free space (CSS Flexbox §8.1). Flex layout was previously margin-blind —
  an item's size and position came from its border box — so a margin on a flex child did nothing
  while the identical rule on a block child worked. Margins are counted where they must be: line
  packing uses the OUTER size, free-space distribution and `justify-content` position the margin
  box, the line grows to hold a cross margin, and `stretch` fills the line less the item's cross
  margins — and an auto-height container's own content extent encloses its children's margin boxes,
  so a trailing margin does not overflow the container that should have grown for it. Showcase §38.
- **`line-height`, all forms** (`pkg/css/value.go` `UnitNumber`, `pkg/layout/css/inline.go`): the
  **unitless multiplier** (`line-height: 1.5`) — the commonest spelling — plus `em`, `%`, lengths,
  and `normal`. The unitless form was previously rejected as an invalid length and the declaration
  dropped, so every block used the font-metric height and the property appeared inert. Units differ
  where it matters: a NUMBER inherits as a number and re-multiplies against each descendant's own
  font size, while `em`/`%` compute against the declaring element and inherit as a fixed length
  (CSS 2.1 §10.8.1). A unitless number is still not a valid length elsewhere — `width: 5` remains
  invalid. Showcase §37.
- **Colour fonts / emoji** (`pkg/font/colr.go`, `colrv1.go`, `bitmap.go`,
  `pkg/layout/paint/colrgradient.go`): colour glyphs paint in colour, through both families of
  table. **`COLR`/`CPAL`** (v0 and v1) decode to layered outlines — vector, so they scale like text
  — including per-layer **full affine transforms** (translation, mirror, rotation, scale) and
  **linear/radial gradients** with pad/repeat/reflect spreads. **`sbix`** (Apple) and
  **`CBDT`/`CBLC`** (Google) decode PNG strikes, choosing the strike nearest the used size and
  preferring a larger one so the image is downscaled rather than enlarged; these do NOT scale like
  outlines. A `.ttc` collection resolves its first face's tables. Emoji in ordinary prose reach an
  **installed** colour font through the script-fallback chain (Apple Color Emoji, Segoe UI Emoji,
  Noto Color Emoji…), because a colour emoji font is far too large to bundle; on a host with none,
  the run degrades to the missing-glyph path. A colour glyph keeps the FONT's palette rather than
  the CSS `color`, unless the font opts a layer into the foreground via the palette sentinel.
  Showcase §36. Not modeled: **sweep (conic) gradients** and **composite paints**, which are refused
  as a whole so the glyph falls back to its monochrome outline rather than a plausible wrong colour;
  CPAL light/dark palette variants (the first palette is used); and non-PNG strike payloads.
- **`text-overflow: ellipsis` and `-webkit-line-clamp`** (`pkg/layout/inline/ellipsis.go`,
  `pkg/layout/css/inline.go`): single-line ellipsis truncation and N-line clamping, the two ways CSS
  truncates text. Truncation runs in GLYPH units — whole glyphs are dropped until the ellipsis fits,
  so a cut never lands mid-character and the line never spills past the clip edge. Trailing
  whitespace is dropped before the ellipsis, and the ellipsis inherits the styling of the glyph it
  follows. `-webkit-line-clamp` (and the unprefixed `line-clamp`) is a LAYOUT effect: the box stops
  after N line boxes and its height shrinks to them, matching the height a browser reports, and the
  final line is marked only when text actually remains. `text-overflow` applies only where the box
  clips — an `overflow: visible` box still overflows visibly, as browsers do — and when even the
  ellipsis alone does not fit it is still rendered (CSS Overflow 3 §5). `text-overflow: clip` (the
  initial) and an over-large clamp are both inert. Showcase §35. Not modeled: `text-overflow`'s
  custom `<string>` and two-value forms, and per-axis clamping.
- **Host CSS cascades into inline `<svg>`** (`pkg/svg` `HostContext`/`ParseWithHost`,
  `pkg/layout/css/replaced.go`): a page author sheet styles inline SVG children, so
  `.icon { fill: blue }` works the way CSS says it should. Class, element, id, grouped, and
  **descendant selectors rooted outside the `<svg>`** all match — the ancestor chain continues past
  the SVG root into the host tree, so `#sidebar .icon` matches rather than silently doing nothing.
  `currentColor` resolves against the `color` the `<svg>` box inherits, and the host's font-size and
  font-family seed the SVG root. Precedence follows CSS: presentation attributes (zero specificity)
  < host sheets < the SVG's own `<style>` (the more specific context wins a tie) < inline `style=`.
  The inline-SVG parse cache is keyed by markup **plus the host context**, so two byte-identical
  subtrees under different rules, colors, or ancestors do not collide. An `<img src="*.svg">` is
  deliberately NOT reached — a referenced SVG is a separate document that CSS does not cascade
  into, and a test pins that direction too. Showcase §34.
- **`color-mix()`** (`pkg/css/colormix.go`, `pkg/css/colorspace.go`): CSS Color 5 §3, in every
  interpolation space — `srgb`, `srgb-linear`, `hsl`, `hwb`, `lab`, `lch`, `oklab`, `oklch`, `xyz`,
  `xyz-d50`, `xyz-d65` — plus all four hue-interpolation modes (`shorter`/`longer`/`increasing`/
  `decreasing hue`) for the polar spaces. Percentages may precede or follow either colour; omitted
  ones fill the remainder; two that sum under 100% normalize the weights **and** scale the result's
  alpha. Interpolation is premultiplied per CSS Color 4 §12.3, so a semi-transparent input
  contributes proportionally less hue. Evaluated inside the single shared colour grammar, so it
  works in every property that takes a colour, and nests. Expected values are pinned to **Chrome**
  (captured by canvas readback), not derived from the same arithmetic as the implementation. Two
  deliberate divergences, both toward exactness: mixing with `transparent` leaves the opaque
  colour's channels untouched (Chrome rounds up to 2/255 through an intermediate space), making
  `color-mix(in srgb, X N%, transparent)` exactly `rgba(X, N/100)`; and nested mixes stay
  unquantized between levels. An unknown space or a malformed component drops the declaration per
  CSS error handling. Showcase §33. Gamut mapping is not modeled — out-of-gamut results clamp.
- **Inline-box backgrounds and borders** (`pkg/layout/inline` `InlineBoxStyle`,
  `pkg/layout/css/fragment.go` `appendInlineBoxDecorations`): `background-color`, a uniform solid
  `border`, and horizontal `padding` on a non-replaced inline box (`<span>`, `<em>`, `<a>`…). Line
  breaking flattens inline boxes into glyph runs, so the box's identity is carried onto every glyph
  and consecutive glyphs are coalesced **per line box** — a span that wraps paints one rect per
  line, the same shape `text-decoration` uses. Identity is a POINTER, not a color: two adjacent
  spans with the same background stay two rects. Inkless glyphs are retained for this pass so a
  background stays continuous across intra-span spaces. Padding is part of LAYOUT, riding on a
  zero-ink edge glyph at each boundary, so the breaker/intrinsic sizing/alignment reserve it by
  reading advances alone. The rect is the content area (tallest ascent + deepest descent among its
  glyphs), so a span mixing font sizes is sized to its largest. Not modeled: background images,
  vertical padding/margins (which per CSS 10.6.1 overflow the line box rather than growing it), and
  per-edge or rounded inline borders. Showcase §32.
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
- **`writing-mode` parsed + reported** (`pkg/css/cascade.go`, `pkg/layout/css/block.go`):
  `horizontal-tb` (the initial value) is honoured; `vertical-rl`/`vertical-lr` are parsed, carried,
  and **degrade with a warn-once log** — they lay out horizontally, because the inline layer
  advances a pen along X and vertical text needs a vertical advance model. The deprecated SVG 1.1
  spellings (`lr`, `lr-tb`, `rl`, `rl-tb`) resolve to `horizontal-tb`, matching the SVG path;
  `sideways-rl`/`sideways-lr` are dropped rather than folded into a vertical value, so the log
  never misstates what was asked for. Inherited (CSS Writing Modes 4 §3.1) — pinned by a test,
  since `inheritFrom` silently resets an unregistered field instead of inheriting it. This
  property previously did not reach the cascade AT ALL: the declaration was dropped before
  computation, so an author got a correct stylesheet, a wrong page, and no diagnostic anywhere.
  Layout is deliberately unchanged; a test pins vertical boxes to horizontal dimensions so the
  degradation stays honest rather than half-applied. **Vertical layout itself is not implemented**
  — see `docs/CSS-LAYOUT.md`.
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
- **Vertical font metrics** (`pkg/font/program.go`, `family.go`): `Face.GlyphVAdvance` and
  `Face.VMetrics` expose `vmtx`/`vhea` alongside the horizontal `GlyphAdvance`/`Metrics`, for
  vertical writing modes. **The advance is normalized to a POSITIVE downward distance in em units**
  — the underlying library returns a negative one (it negates for a Y-down convention, so a
  1000-upem face reports -1000), and the flip happens once at the adapter so no caller carries the
  convention. A caller taking the raw sign would run the pen backwards up the page with nothing to
  catch it, so a test pins it. Three outcomes are kept distinct rather than collapsed: a face with a
  real `vhea` reports its authored values; a TrueType face WITHOUT `vmtx` still returns a usable
  advance (upstream synthesizes one em — the correct fallback, and what browsers do) but
  `VMetrics` reports `ok=false` so a synthesized metric is never presented as authored; Type1 and
  bare CFF report `ok=false` for both, because the formats have no vertical metrics at all. Both
  branches are covered by bundled faces (Inconsolata `.ttf`, TeX Gyre Heros `.pfb`). `FontVExtents`
  panics on inconsistent tables exactly as `FontHExtents` does, so it carries the same `recover`.
  This retires the "needs `vhea`/`vmtx` reading `pkg/font` does not have" blocker cited for vertical
  text; what remains is layout. **Nothing consumes these yet** — vertical layout is not implemented.
- **`.notdef` for unmappable runes** (`pkg/font/notdef.go`, `pkg/layout/inline/shape.go`): a rune that
  neither the run's family nor any script fallback can map now draws the tofu box instead of rendering
  as NOTHING. `Face.NotdefGlyph` follows the browser order — the font's own glyph 0 when it has
  geometry (DejaVu draws a hollow box, Noto a box of hex digits), a synthesized hollow rectangle when
  it does not, which is the branch the bundled TeX Gyre substitutes take since all of them ship a
  BLANK `.notdef`. The box carries a non-zero advance so line-breaking measures the text at its true
  width, and `Glyph.Runes` is retained so bidi sees the character's real class and SVG's
  glyph→character mapping still locates it; `Glyph.Face` is cleared so every backend fills the same
  outline (handing a text backend GID 0 would emit the font's blank `.notdef`, making the box visible
  in a raster and invisible in a PDF of the same page). **Each distinct missing rune is warned about
  exactly once per `Shape` call** via the `logf` the CSS engine and the SVG text path already thread
  in — the shaper is one of the degradation sites that genuinely has a logger, so this really does log
  rather than only claiming to. Invisible characters are excluded (`invisibleRune`): a space variant,
  format control, or variation selector draws no ink even where it IS mapped, so giving it a box would
  invent a mark the author never wrote — this repo's own showcase carries a U+202F that regressed
  exactly that way before the exclusion existed. Applies to the shared CSS/SVG text path; DOCX/PDF and
  any page whose glyphs all resolve stay byte-identical. Showcase §19.
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

**Unified conversion core** (`pkg/omnidoc/format.go`+`detect.go`+`open.go`+`convert.go`+
`image_backend.go`, CLI `convert`):

- One `Format` type + capability table (`CanConvert`, typed `ErrUnknownFormat`/
  `ErrUnsupportedFormat`/`ErrSameFormat`); content-first `DetectFormat` (magic → extension hint →
  WHATWG HTML sniff; no UTF-8⇒text fallback). `Open`/`OpenBytes` sniff any supported format (the
  PDF path is byte-identical); `OpenAs`/`OpenBytesAs` skip detection; every opener stamps
  `Document.Format()`. Generic `Convert`/`ConvertFile`/`(*Document).Write` dispatch any valid
  input→output pair (the legacy `ConvertXToY` wrappers were shims pinned byte-identical, since removed);
  same-format conversion is a deliberate `ErrSameFormat` on the generic path only. PNG/JPEG/WebP
  are output formats (`WriteImage`/`EncodeImage`; Convert-to-image writes one page, multi-page =
  CLI `%d` fan-out). CLI: `convert <in> <out>` with `--from`/`--to`; all subcommands share one
  detection-based opener (rasterize no longer assumes unknown extensions are PDF; topdf `--print`
  actually applies print media now). A new format lands by flipping its capability bit + one switch
  case in `openDetected`/`Write` — see the sibling contract in.

**Markdown + plain-text input** (`pkg/markdown` via goldmark (MIT, pure Go, zero transitive
deps), `pkg/omnidoc/markdown_frontend.go`+`text_frontend.go`):

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

**CSV/TSV input + output** (`pkg/omnidoc/csv_frontend.go`, `pkg/render/csvwrite`,
`OpenCSV*`/`OpenTSV*`, `WriteCSV`/`WriteTSV`):

- Input: stdlib `encoding/csv` (lazy quotes, ragged rows padded, BOM/CRLF) → an HTML table
  (first row = header) through the reflow pipeline; CSV and TSV are distinct formats (csv ⇄ tsv
  are real conversions), extension-only detection. Output: tables-only structure writer over the
  boxwalk occupancy grid (spans duplicated — the GFM strategy; multiple tables blank-line
  separated; prose dropped + logged, table-less documents produce empty output + a loud log) —
  which makes **PDF → CSV table extraction** work via the existing lattice/stream recognizer
  (pinned by test). `csv-specimen` golden.

**XLSX input** (`pkg/xlsx` hand-rolled reader + `pkg/omnidoc/xlsx_frontend.go`,
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

**Stream + MIME input surface** (`pkg/omnidoc` format.go/open.go, first tinycld-adoption PR):

- `FormatFromMIME`/`Format.MIME()` (params stripped/case-folded; explicit-Unknown pins for
  legacy binary Office — never the OOXML cousins — HEIC *sequences*, zip, octet-stream (`image/webp`
  was such a pin until the WebP reader landed); unlisted `text/*` →
  FormatText with `text/rtf` excepted; rows flip to PPTX/EPUB/RTF when those frontends land);
  `OpenReader`/`OpenReaderAs(ctx,..)` stream entry points (fully buffered) threading a real
  open-time context through layout — a cancelled open ERRORS rather than returning a silently
  truncated document (boundary check; the engine itself degrades); `Convert`/`ConvertFile` now
  pass their ctx to open; `MarkdownOptions.MaxBytes` rune-safe text-output cap (search-index
  extraction). Capability gate for hosts = `FormatFromMIME(mt).ValidInput()`.

- **Cancellable HTML render, end to end.** Ctx-taking twins for the HTML entry points that had
  none — `OpenHTMLBytesContext`/`OpenHTMLFileContext`/`OpenURLContext` (`OpenURLContext` also
  bounds the HTTP fetch of the page itself, which `OpenURL` ran under `context.Background()`).
  The no-ctx originals are unchanged and delegate with `context.Background()`, so every existing
  caller is source- and byte-compatible. Rasterization now actually honors its context:
  `reflowRenderer.renderPage` took `_ context.Context` and dropped it, so `RasterizePage`
  advertised a cancellation it never performed — it now checks before the allocation/paint and
  again after paint. Layout gained the two checks that bound the real worst case: `inline`'s new
  `ShapeContext` (checked between runs and every 1024 runes — shaping runs BEFORE line breaking,
  so it was the longest uninterruptible stretch) and a per-line check in the CSS engine's
  `layoutInline` (a single huge paragraph is one block child, which the pre-existing
  between-children check never revisits). Measured on a ~3s pathological layout, cancellation
  latency went from ~2.6s to ~2ms; the normal path is unchanged (benchstat over 12 runs: raster
  -1.3%, shape -2.0%, open flat). Cancellation degrades in the engine and hardens to an error at
  the open boundary, so a truncated document is never returned silently.

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

**WebP input + output** (`pkg/webp`, over `golang.org/x/image/webp` for decode — BSD, already an
approved dep — and `github.com/HugoSmits86/nativewebp` for encode — MIT, pure Go, whose only
dependency is `x/image`, so no new transitive surface):

- Still WebP decodes everywhere the other raster formats do: `.webp` as a document input
  (`FormatWebP`, MIME `image/webp`), content-first `DetectFormat`
  magic (the RIFF `WEBP` form type, so WAV/AVI stay unknown), inside HTML/EPUB `<img>` by
  content type or by sniffing, and transcoded to PNG inside DOCX/PPTX/RTF/EPUB outputs
  (`pkg/render/imageconv`). Lossy VP8, lossless VP8L, and the extended VP8X container with an
  alpha plane are all covered.
- **Animated WebP returns a typed `ErrAnimated`.** `x/image/webp` handles stills only, and its
  failure mode is misleading: `DecodeConfig` parses VP8X, ignores the animation flag, and returns
  the canvas size with NO error, while `Decode` then fails with a bare `webp: invalid format` —
  the same error corrupt bytes produce. `pkg/webp` reads the flag upstream skips and returns
  `ErrAnimated` from both entry points; `OpenImageBytes` and `TranscodeToPNG` check it, so a
  valid animation is reported as unsupported instead of broken.
- The `image.Decode` **sniffing** path cannot carry that check. `image.sniff` returns the first
  registered format whose magic matches, in registration order, and an importing package's `init`
  always runs after the package it imports — so no registration here can outrank
  `x/image/webp`'s. Code that must not be fooled by an animated file calls
  `webp.Decode`/`webp.DecodeConfig`, or `webp.IsAnimated` on the bytes; the two call sites that
  matter do. A test pins the upstream behavior so this gets revisited if it changes.
- **Output is lossless VP8L**, wired as a full image target: `Convert`/`WriteImage`/`EncodeImage`
  to `FormatWebP`, the CLI's `rasterize --format webp` and any `.webp` output path (which also
  infers the `rasterize` subcommand). Round-trips are verified **pixel-exact** against
  `x/image`'s independent decoder, including alpha, at sizes down to 1×1 and up to the format's
  16384-pixel ceiling (`webp.MaxDimension`, VP8L's 14-bit dimension field) — one pixel over is a
  clean error, not a truncated file. The showcase pairs `img/quad.webp` with `img/quad.png`, and
  the two decode to 0 differing pixels across all 4096.
- **WebP output is a PNG-class target, not a JPEG-class one.** There is no pure-Go lossy VP8
  encoder and the toolkit takes no CGo, so a photographic page encoded to WebP comes out much
  larger than a lossy encoder would produce, possibly larger than the equivalent JPEG — ask for
  JPEG when the output needs to be small. `ImageOptions.Quality` is a **no-op** for WebP, pinned
  by a test: two quality values produce byte-identical output.
- `webp.Encode` buffers and writes once, checking the error. `nativewebp.Encode` ignores the
  error from every write it makes to the destination, so passing it the caller's `io.Writer`
  would report a failed write — full disk, closed pipe — as a successful encode. A test pins that
  a failing writer surfaces the error.
- No animated output: the toolkit rasterizes pages to still images, so a multi-page document
  becomes one file per page (the `%d` fan-out), never one animation.
- Bug fixed on the way in: `rasterize` took its encoding from `--format` alone, whose `png`
  default always won, so `--out page.jpg` wrote a **PNG under a .jpg name** with no diagnostic
  (`.webp` would have done the same). The output extension now picks the format when `--format`
  is absent; an explicit `--format` still wins. Regression test in
  `cmd/omnidoc/rasterize_test.go`.

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

**Page geometry + fit-within raster sizing** (`pkg/omnidoc`, CLI `--max-width/--max-height`):

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
  primitive, implemented by BOTH backends: overlapping children inside an opacity group blend once, at
  the flattened result, instead of each child's own paint alpha double-darkening the overlap. The same
  primitive fixes the analogous double-paint case on a single shape carrying both a fill AND a stroke
  at element `opacity` < 1 (the stroke's inner edge overlaps the fill) — routed through a group only
  when both a fill and a stroke are present and opacity < 1, so the common opaque/single-paint shape
  stays on the cheap per-paint path with no offscreen allocation. In PDF output, `BeginGroup`/`EndGroup`
  emit a real `/Group << /S /Transparency /CS /DeviceRGB /I true >>` Form XObject: children paint into
  their own content stream/resources, and the group composites back with one `/GSn gs /Fmn Do`
  referencing an ExtGState carrying the group's `/ca` and `/BM`. A fully-opaque, ungrouped document's
  PDF output stays byte-identical to before this feature (verified: `cmp` on a rendered fixture matches
  the pre-groups commit exactly).
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
  unresolved reference (no clipping at all). `display:none` and `visibility:hidden` both remove a clip
  child from the union (verified against the resvg corpus's reference renders for both). fill/stroke/
  opacity/filter/mask on a clip child have no effect — only geometry, transform,
  clip-rule, and clip-path matter. Resolved during `Parse` (like paint servers), with a
  `buildingClip`-style recursion guard so a self-referencing or mutually-cyclic clipPath terminates.
  raster implements `BuildClipMask` exactly (per-child rasterize + max union); pdfwrite — which has no
  offscreen surface to rasterize a pixel-exact per-child-rule union into — returns a documented
  rectangular bounding-box approximation, now applied for real: it feeds the same `/SMask` machinery a
  luminance mask uses (a DeviceGray coverage image behind a luminosity soft mask), so a PDF clip-path
  is a real (if rectangular-approximate) restriction rather than an inert no-op.
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
  pixel); pdfwrite renders the mask's content into a SECOND, nested Form XObject
  (`/Group << /S /Transparency /CS /DeviceGray >>`) and wires it into the group's own ExtGState as
  `/SMask << /S /Luminosity /G <form> /BC [0] >>` — a real PDF luminosity soft mask, not an
  approximation. `/BC [0]` (a black backdrop) is mandatory: without it, the area outside the mask form's
  own content is undefined where SVG requires fully transparent. `pkg/pdf/content` (the PDF *reader*)
  was taught `/SMask` too (an ExtGState soft mask now renders through `Device.BuildLuminanceMask`
  exactly like the writer produces it, scoped per paint operator: fills, strokes, `sh`, image/inline-
  image `Do`, and a form XObject's entire nested content — text glyph fill/stroke is the one documented
  gap, since masking each glyph individually would reintroduce the per-child compositing seam this
  whole feature exists to avoid), so the SVG → PDF → reopen → raster round trip is genuine end-to-end
  proof for masks, independently cross-checked against Poppler's own renderer.
- Known gaps in the group/clip/mask feature, each verified by rendering rather than merely inferred:
  **`BuildClipMask`'s pdfwrite approximation is rectangular, not exact** — a non-rectangular
  `<clipPath>` union (e.g. two non-overlapping circles) clips to the union's BOUNDING BOX in PDF
  output, not its true shape (raster remains pixel-exact via the per-child rasterize + max-union
  primitive; only the PDF writer, which has no offscreen surface to rasterize a pixel-exact union
  into, degrades). **Glyph fill/stroke is not soft-mask-wrapped** — `pkg/pdf/content`'s ExtGState
  soft-mask support scopes to fills, strokes, `sh`, images, and nested form XObjects, but not
  per-glyph text painting, to avoid reintroducing the per-child compositing seam this feature exists
  to eliminate. **`reflect`/`repeat` gradient spreads still rasterize** in PDF output (see the
  alpha-gradient shading lift above — only `pad` gets a native `/Shading`/`/Extend`). **`objectBoundingBox`
  clip-path/mask units on a `<g>` target degrade to `userSpaceOnUse`** (Identity mapping): a `<g>` has
  no single `Path` to measure a bounding box from the way a `Shape` does, so `pkg/svg/draw` passes a
  nil `boundsFunc` for a Group target and `clipUnitsMatrix`/mask region resolution falls back to
  Identity rather than resolving the group's real (post-layout) bbox — verified against the resvg
  corpus's `mask/on-group-with-transform.svg` and `mask/half-width-region-with-rotation.svg`, both of
  which render blank under this engine (a graceful degradation, not a crash) versus resvg's correctly
  bbox-relative result.
- **SVG `<filter>` — the primitives real documents use** (`pkg/svg/filter.go` resolves the
  graph, `pkg/svg/filter` holds the pixel math, `pkg/svg/draw/filter.go` drives it). The `filter`
  property is wired through the same presentation-attribute/cascade path as `clip-path`/`mask`, and
  the `<filter>` element resolves at PARSE time (the document index is gone once `Parse` returns):
  `filterUnits`/`primitiveUnits` (opposite defaults, `objectBoundingBox`/`userSpaceOnUse`), the
  `x`/`y`/`width`/`height` region defaulting to **-10%,-10%,120%,120%** so a filter has room to bleed
  past its source, per-primitive subregions, and the `result`/`in`/`in2` wiring with the implicit
  `SourceGraphic`/`SourceAlpha` inputs. A cycle is impossible BY CONSTRUCTION rather than by a
  runtime guard: `in` may only name an EARLIER `result`, so the resolved graph is a strictly backward
  DAG the renderer evaluates in one forward pass. An `in` naming an undefined result falls back to
  the previous primitive's output, per spec.
- **Filters run in linearRGB by default** — the one place this engine departs from sRGB, and the
  likeliest source of subtly-wrong output. `color-interpolation-filters` defaults to `linearRGB`
  (with the `sRGB` opt-out supported), so a filter converts sRGB → linear, operates, and converts
  back, using the exact IEC 61966-2-1 transfer function INCLUDING its linear segment near zero — not
  a `pow(2.2)` approximation, which is wrong across the range and worst in the near-black values a
  shadow's falloff is made of. The inverse is computed rather than table-driven, since a 256-entry
  inverse table quantizes the dark end enough to band exactly those gradients.
- **Filter error handling is the OPPOSITE of clip-path/mask's**, and the corpus pins each case: an
  unresolvable `filter="url(#missing)"` means the element is **not rendered at all** (not "no
  filtering"); an empty `<filter>` outputs transparent black, so the element disappears rather than
  passing through; a zero/negative region or primitive subregion likewise disables the element; and
  an `objectBoundingBox` region on an element with no bounding box (an empty group) disables it,
  while a `userSpaceOnUse` region on the same group still paints.
- **`feGaussianBlur` uses the spec's own three-box approximation**, not a hand-rolled Gaussian
  convolution: `d = floor(s·3·√(2π)/4 + 0.5)`, three boxes of `d` when `d` is odd, and for an even
  `d` two boxes centred on OPPOSITE pixel boundaries (so their half-pixel shifts cancel) plus one of
  `d+1`. Ignoring that odd/even split yields a blur that is correct in shape but visibly
  TRANSLATED. `stdDeviation` takes one or two numbers, each axis independent; a negative value, an
  empty value, or more than two values is an error that disables the PRIMITIVE, so the element
  renders **unblurred rather than blank**. **The blur operates on PREMULTIPLIED values** — the one
  primitive where that is most visible, since averaging straight colour across a transparent edge
  weights the transparent pixels' meaningless black equally and darkens every blurred edge. A blur
  is O(pixels) per pass whatever the box width, and the deviation is additionally clamped so the box
  never spans more than half the extent it blurs across: past that the window is almost entirely
  off-buffer and the element decays toward NOTHING rather than becoming more blurred.
- **`feComposite`** — the five Porter-Duff operators (`over`, `in`, `out`, `atop`, `xor`) plus
  `arithmetic` with its `k1..k4`. Both run on premultiplied values, with the result (not the
  coefficients) clamped to [0,1]: the corpus's `k4="100"` fixture renders opaque white, proving the
  reference feeds the coefficients through verbatim despite its own `<desc>` claiming otherwise.
  An unrecognized `operator` falls back to `over` rather than disabling the primitive.
- **`feMerge`/`feMergeNode`** — its inputs composited in DOCUMENT order, which is painting order:
  the first node is the bottom of the stack. It is exactly a fold of `over`, asserted against
  `Composite` directly so an `feMerge` cannot drift from the equivalent `feComposite` chain.
- **`feBlend`** — `normal` plus the fifteen CSS/PDF blend modes (`multiply`, `screen`, `overlay`,
  `darken`, `lighten`, `color-dodge`, `color-burn`, `hard-light`, `soft-light`, `difference`,
  `exclusion`, and the four non-separable `hue`/`saturation`/`color`/`luminosity`). The blend
  FUNCTIONS moved to `pkg/render/blend.go` and are now **shared with the raster backend's PDF `/BM`
  compositing** rather than reimplemented — one `colorBurn`, two consumers. Compositing follows the
  full CSS formula, so a source over a TRANSPARENT backdrop comes through unblended rather than
  multiplied against the backdrop's meaningless colour.
- **`feColorMatrix`** — `matrix` (5x4), `saturate`, `hueRotate` and `luminanceToAlpha`, with each
  shorthand expanded into a matrix at PARSE time so the renderer implements one operation.
  **It operates UN-premultiplied**, the exact opposite of blur and composite; both directions are
  mutation-proven by tests, since getting either backwards is the classic bug. `luminanceToAlpha`
  uses SVG's BT.709 filter weights (0.2125/0.7154/0.0721), deliberately not the 0.3/0.59/0.11 set
  PDF's blend functions use for the same-sounding quantity. An unrecognized `type` is treated as
  `matrix` (so `type="qwe"` with a full `values` list still applies it), while a `values` list that
  is not exactly 20 numbers falls back to the identity.
- **`feDropShadow` is EXPANDED into its five-primitive chain**, not special-cased: blur → offset →
  flood → composite(`in`) → merge. A test asserts the shorthand and the hand-written chain produce
  **byte-identical** pixels, so it inherits the chain's premultiplication, fractional resampling and
  colour-space handling instead of re-deriving them.
- **The CSS `filter:` shorthand** — `blur()`, `drop-shadow()`, `brightness()`, `contrast()`,
  `grayscale()`, `sepia()`, `saturate()`, `hue-rotate()`, `invert()`, `opacity()`, and `url()`, in
  any combination and composing in sequence. Each lowers to its spec-defined primitive chain rather
  than to separate pixel code. The parser lives in **`pkg/filtereffects`, deliberately shared** —
  `filter` is one property with one grammar, and the HTML/CSS side consumes this parser rather than
  growing a second one. Error handling is CSS's, and the corpus tests it hard: **one invalid
  function invalidates the WHOLE declaration** (the element renders completely unfiltered, not with
  the functions that did parse), while an unresolvable `url()` inside a list is merely dropped;
  `blur(50%)` and `hue-rotate(45)` are both invalid (a percentage is not a `<length>`; a CSS
  `<angle>` requires a unit) even though `blur(1mm)` and `hue-rotate(45deg)` are fine. A filter
  function has **no filter region** — unlike the `<filter>` element — so a large `drop-shadow()`
  spreads across the canvas instead of being clipped to a box barely larger than its element.
- **Filters apply BEFORE clip-path, mask and opacity**, per SVG's rendering model. All three are
  stripped from the filter's source pass and applied to the filtered RESULT, so a blur spreads past
  a clip's edge and is then cut off hard by it. Clipping the filter's INPUT instead removes the
  content the blur would have spread from, which reads as a too-soft blur rather than as a
  mis-ordered clip.
- **The remaining nine primitives degrade to the UNFILTERED element with a warn-once log naming
  each** (`feTurbulence`, `feConvolveMatrix`, `feDiffuseLighting`/`feSpecularLighting`,
  `feMorphology`, `feImage`, `feTile`, `feComponentTransfer`, `feDisplacementMap`). A visible
  approximation beats a blank, and an unknown primitive never silently yields an empty result.
  `enable-background` is DROPPED outright rather than deferred — it was removed from the spec and no
  browser implements it — so its `BackgroundImage`/`BackgroundAlpha` inputs resolve like any other
  unknown name.
- **Filters rasterize, including in PDF output — stated plainly rather than discovered later.** This
  is the one place the series' vector-native principle does not apply: a blur has no vector
  representation and PDF has no filter operator, so **any filtered element is rasterized**, at a
  resolution taken from the filter region and the current transform. That is what every PDF producer
  does with SVG filters, but it is a real trade-off rather than a free lunch. The seam is
  `render.Device.RenderOffscreen`, a third member of the `BuildClipMask`/`BuildLuminanceMask` family
  that hands back a group's rasterized PIXELS (`*image.RGBA`) instead of a coverage mask, keeping
  rasterization in the backend and `pkg/svg/draw` backend-agnostic. `pkg/render/pdfwrite` returns nil
  from it — the documented "cannot rasterize offscreen" degradation — and the caller then paints the
  element unfiltered, so PDF output keeps the content visible and correctly placed, minus the
  filter's visual effect.
- **A filter on `<text>` uses REAL placed-glyph bounds** (`textUserBounds`, computed from the shaped
  glyphs), never `pkg/svg`'s build-time `textBBox` estimate. That estimate assumes a half em per
  character and measures 0.53x–2.25x off; an `objectBoundingBox` filter region built on it would
  visibly clip the filtered result rather than merely shifting a gradient.
- **Filters are bounded against a build-time DoS.** The region is intersected with the part of the
  canvas it could actually reach BEFORE any buffer is allocated, so a crafted `width="400000"` costs
  the same pixels a sane region would; on top of that a hard per-region pixel cap, a primitive-count
  cap, and a filter-nesting-depth cap each degrade to the unfiltered element with a log rather than
  allocating unboundedly.
- **`<use>` and `<symbol>` instantiation.** A `<use>` instantiates its href target as if it were a
  deep clone spliced in at the `<use>`'s own position: the clone inherits from the USE SITE, not from
  the target's document parent, so the target's own attributes win where it sets them and the
  `<use>`'s cascade shows through everywhere else (`style-inheritance-1.svg` and
  `complex-style-resolving-order.svg` pin this down). Instantiation is therefore deliberately NEVER
  memoized by target id — two `<use>`s of one target under different inherited style must produce
  genuinely different `Shape`s, the opposite of `clipMemo`/`maskMemo`'s idempotent by-id caching. The
  `<use>`'s `x`/`y` and its own `transform` compose under the target's, and its element `opacity`
  composites on the wrapper Group exactly like a `<g opacity>`'s does. A `<symbol>` target
  additionally establishes a real second viewport (SVG2 §5.6): sized from the `<use>`'s own
  `width`/`height` (lacuna 100% of the current viewport), mapped through the symbol's `viewBox`/
  `preserveAspectRatio` with the same machinery as the root `<svg>`, with `userSpaceOnUse`
  percentages inside resolving against the symbol's extent. Its default `overflow:hidden` clips to
  the viewport rect via a cheap axis-aligned `Group.ViewportClip` (a plain `PushClip`, no offscreen
  compositing pass) — resolved through the cascade, so `style="overflow:visible"` disables it. A
  `<symbol>`'s own `transform` is ignored per SVG 1.1. `<use>` also works as a `<clipPath>` child.
  Both cycle shapes terminate: an href chain (`#u1` → `#u2` → `#u1`) and tree recursion (a `<use>`
  targeting its own DOM ancestor, or a descendant `<use>` targeting an enclosing one) — the latter is
  unreachable by href-following alone and is caught by a `buildingUse` "id currently on the call
  stack" guard keyed on BOTH the `<use>`'s own id and its target's. A long ACYCLIC chain of distinct
  targets is bounded separately by `maxUseDepth` (64).
- **`<marker>` painting** on `path`, `line`, `polyline`, and `polygon` — the SVG 1.1 markerable set.
  `marker-start`/`-mid`/`-end` and the `marker` shorthand all resolve through the cascade and
  inherit, so a marker set on a `<g>` reaches its shapes. Markers place at every vertex with correct
  per-vertex tangents (including a synthesized vertex for a closed subpath, per SVG 1.1 §11.6.3),
  honor `orient="auto"`/`orient="auto-start-reverse"`/an explicit angle, `refX`/`refY`, `markerWidth`/
  `markerHeight`, `markerUnits` (`strokeWidth` default vs. `userSpaceOnUse`), and their own
  `viewBox`/`preserveAspectRatio`. A marker clips to its viewport BY DEFAULT (`overflow:hidden` is
  the initial value for a marker, the opposite of most SVG elements). Markers are memoized by id
  (`markerMemo`, like `clipMemo`/`maskMemo` — resolution is idempotent here), and a marker whose own
  content carries a `marker-*` property is guarded against self-reference by `buildingMarker` and
  against a long acyclic chain by `maxMarkerChainDepth` (64).
- Known scope limits of `<use>`/`<symbol>`/`<marker>`, each degrading rather than failing: **a nested
  `<svg>` as a `<use>` target is not supported** — it establishes its own viewport, which this slice
  does not implement, so the reference resolves to nothing SILENTLY (no `WithLogf` line, unlike most
  degradations here), deferring the corpus's seven `xlink-to-svg-element*.svg` fixtures. **Markers
  paint only on `path`/`line`/`polyline`/`polygon`** — SVG 2 extends the markerable set to the
  remaining shapes and this engine does not follow it, so a `marker-*` property reaching a
  `<circle>`/`<rect>`/`<ellipse>` by inheritance paints nothing (the corpus's `marker-on-circle.svg`,
  `marker-on-rect.svg`, and `marker-on-rounded-rect.svg` assert exactly that). **A `<use>` inside a
  `<clipPath>` may not itself target another `<use>`.** **Total `<use>`/`<symbol>` instantiations per
  document are capped at `maxUseNodes` (100,000)**, logged once via `WithLogf` when exhausted: this
  is a build-time DoS bound, since `maxUseDepth` limits recursion depth but not breadth, and a graph
  where each level references the previous level twice expands ~4× per level entirely inside `Parse`
  — where `pkg/svg/draw`'s draw-time `maxDrawCalls` can never fire. The budget is a monotonic
  whole-document total (a per-subtree counter would reset on every sibling and let such a graph
  through) and sits about an order of magnitude above the largest realistic icon sprite sheet, so
  legitimate documents are never truncated.
- **`<text>` and `<tspan>` as vector outlines** — the core text pipeline. SVG text is shaped by the
  SAME `inline.Shape` the CSS reflow engine uses, not a second shaper: `Shape` is a pure function
  that is not fused to line-breaking, so SVG calls it and walks the flat `[]Glyph` accumulating
  advances, skipping `Break`/`MakeLine`/`Place` entirely (those need a width to wrap against, and
  SVG text does not wrap). Arabic harfbuzz shaping and per-rune script fallback happen inside
  `Shape`, so SVG gets them for free, and `inline.Reorder` supplies UAX#9 bidi on the same flat
  slice. Supported: the per-character `x`/`y`/`dx`/`dy`/`rotate` LISTS with SVG's full rule set
  (absolute resets start a new text chunk, relative offsets accumulate, a short list stops applying
  — except `rotate`, whose last value persists), `text-anchor` (`start`/`middle`/`end`, resolved per
  CHUNK and DIRECTION-RELATIVE, so `start` anchors the right edge in an rtl chunk), `<tspan>` nesting
  with an inherited position cursor and per-tspan style, `font-family`/`font-size`
  (incl. `em`/`ex`/percentage against the parent, and the CSS size keywords)/`font-weight`
  (incl. `bolder`/`lighter`)/`font-style`, `direction`, `unicode-bidi: bidi-override`, both
  `xml:space` modes, and the SVG 2 `clip-path`/`mask`/`opacity` properties on a `<tspan>`.
  `<text>` also works as `<clipPath>` and `<mask>` geometry.
- SVG text paints through `Device.FillGlyph`/`Stroke`, **never `DrawGlyph`** — so it routes through
  the same `paintFill`/`paintStroke` helpers a `<path>` uses and gets gradients, patterns, and
  independent fill+stroke for free. `DrawGlyph` emits PDF text-showing operators, which cannot
  express a per-glyph transform (SVG's `rotate`), independent fill and stroke on one glyph, or a
  glyph acting as clip/mask geometry. **The cost, stated plainly: SVG text in PDF output is vector
  outlines, not selectable or searchable text.**
- Known scope limits of SVG text, each degrading with a log: **`<textPath>`** renders its text on a
  straight baseline (arc-length parameterization of a `render.Path` is a subsystem of its own) and
  **`writing-mode`** renders horizontally (the layout path has no vertical advance model; the
  metrics themselves now ship — see "Vertical font metrics"). **`<tref>` is dropped, not deferred** — it was removed from SVG 2 and is
  unimplemented in every current browser. A ligature or
  cursive join spanning a `<tspan>` boundary does not form, since the two sides reach the shaper as
  separate runs. An `objectBoundingBox` paint server on text resolves against an approximated box (a
  text chunk's true box needs shaping, which happens a layer away from where paint servers resolve);
  `userSpaceOnUse` is exact. Text is bounded against hostile input by a `<tspan>` nesting cap and a
  whole-document character budget (`maxTextChars`, 200,000), both logged once.
- **`letter-spacing` and `word-spacing` on SVG text** — applied as a post-shaping advance adjustment
  on the flat glyph slice, resolved per SOURCE CHARACTER (so a `<tspan letter-spacing="10">` inside a
  `<text letter-spacing="3">` widens only its own gaps). Values may be a bare number, any absolute
  unit, `em`/`ex`/`%` (against the element's own `font-size`), `normal`, or negative. `letter-spacing`
  widens the gap AFTER each character except the last in the whole `<text>` — CSS Text 3's rule, not
  SVG 1.1's literal wording, matching resvg (whose `letter-spacing/filter-bbox.svg` asserts the flush
  trailing edge with a filter region and states the rule in its own `<desc>`). `word-spacing` adds at
  each space character. **Deliberate asymmetry: neither property exists anywhere else in this engine
  — not in `pkg/css`, not in `pkg/layout/css` — so both work in SVG and are inert in HTML/DOCX.**
  Wiring them into CSS reflow means threading them through line-breaking and justification, which is
  a materially larger job; the asymmetry is recorded on the `Style` fields so it does not read as a
  bug.
- **`textLength` and `lengthAdjust`** — a range's advance is forced to an exact width.
  `lengthAdjust="spacing"` (the default) distributes the difference into the interior gaps only, so
  the leading and trailing edges are untouched; `lengthAdjust="spacingAndGlyphs"` scales the glyph
  OUTLINES horizontally as well. Both work on `<text>` and on `<tspan>`, and nest (innermost wins for
  the characters they share, and a nested `spacingAndGlyphs` COMPOUNDS its outline scale onto the
  outer one so the outline and the advance never drift apart). `textLength` is an XML attribute, not
  a presentation attribute: it neither cascades nor inherits. Edge cases handled without a special case: a target smaller than the
  natural width (the glyphs overlap), exactly zero (they collapse onto a point), negative (invalid,
  ignored), and a single-character range, which has no interior gap for `"spacing"` to use and is
  left at its natural width rather than dividing by zero.
- **`dominant-baseline` and `alignment-baseline`** — `auto`, `alphabetic`, `middle`, `central`,
  `hanging`, `text-before-edge`/`before-edge`, and `text-after-edge`/`after-edge`, each resolved
  against the GLYPH's own `Face.Metrics()` so a per-rune script fallback hangs each character from
  its own font. `alignment-baseline` wins when set and defers at `auto`; its `baseline` keyword
  DEFERS to the parent's dominant baseline rather than resetting to alphabetic. Scoping matches
  resvg: `dominant-baseline` propagates inside a `<text>` subtree but does not arrive from a `<g>`
  above it, while `alignment-baseline` is non-inherited with an explicit `inherit` that reaches back
  for the parent's value. `ideographic`, `mathematical`, `use-script`, `no-change`, and `reset-size`
  **degrade to the alphabetic baseline with a warn-once** — they need OS/2 and BASE table metrics
  `pkg/font` does not parse. `middle` uses the conventional 0.5 em x-height substitute for the same
  reason (no OS/2 `sxHeight`).
- **`baseline-shift`** — `sub`, `super`, lengths, and percentages (against the element's own
  `font-size`), CUMULATIVE through nested `<tspan>`s per SVG2 §11.10.2, with the `baseline` keyword
  contributing zero WITHOUT resetting the accumulation. A shift written on the `<text>` element
  itself, or inherited from above it, is inert: the accumulation starts at zero there and only a
  `<tspan>` inward can add to it (resvg asserts this four ways, each overlaying an unshifted
  reference the shifted text must exactly cover). `sub`/`super` use the conventional ∓0.2 em / ±0.4
  em offsets, since the font's OS/2 sub/superscript offsets are not parsed.
- **`text-decoration`** — `underline`, `overline`, and `line-through`, in any combination and either
  separator, drawn as filled (and, when the declaring element has one, stroked) rectangles.
  **A rule takes the paint AND the font metrics of the element that DECLARED it, not of the
  descendant characters it covers**, so `<text fill="red" text-decoration="underline"><tspan
  fill="blue">x</tspan></text>` underlines in RED and a decoration declared at `font-size:200` draws
  a thick rule over 48 px text. The one bend: a decoration inherited from ABOVE the `<text>` keeps
  its line but adopts the `<text>`'s paint, which is what resvg's `outside-the-text-element.svg` and
  `style-resolving-2.svg` both assert. A rule spans the whole run that inherited the same
  declaration, crossing `<tspan>` boundaries, and is emitted once per baseline FRAME rather than once
  per run — so it staircases with a `dy`/`y` list and tilts per glyph with a `rotate` list, matching
  the references. Underline and overline paint under the glyphs, line-through over them. Position and
  thickness use conventional em fractions: `pkg/font` parses no `post` table, so a face's own
  `underlinePosition`/`underlineThickness` are unavailable.
- **The `font` shorthand** — expands to `font-style`, `font-weight`, `font-size` (with an optional
  `/line-height` that is discarded, since SVG text does not wrap), and `font-family`, and **RESETS
  every longhand it covers to that longhand's initial value whether or not the value names it**
  (CSS Cascade §3) — so `font="40px X"` inside a `<g font-weight="bold">` renders REGULAR, and
  `font-variant`/`font-stretch` (tracked here only as degradation flags) clear alongside, so a
  shorthand cannot leave an ancestor's stale diagnostic attached. `bolder`/`lighter` still step from
  the inherited weight: the reset governs where a slot starts, not what an explicitly-named relative
  keyword measures against. A value that does not yield BOTH a size and a family is invalid per CSS
  and applies nothing rather than half of itself — not even the reset. The system-font keywords
  (`caption`, `icon`, …) name platform UI fonts this engine cannot resolve and are logged and
  ignored.
- **`font-stretch`, `font-variant`, `kerning`, and `font-kerning` ship as honest no-ops**, each
  logged once. Stated plainly rather than approximated: the bundled families (`pkg/font/standard`)
  have no condensed, expanded, or small-caps variant, no synthetic stretching or obliquing exists
  anywhere in the engine, OpenType feature settings are not plumbed through the shaper, and no GPOS
  kerning-pair pass runs for simple scripts — so there is no kerning to disable and no length that
  could replace it. A synthetic squeeze or a fabricated small-caps would change advances and glyph
  shapes in ways no font designer sanctioned and no other renderer reproduces.
- Not yet, each degrading with a `WithLogf` debug line rather than failing: filters,
  `<image>`, and inline `<svg>` inside HTML/`<img src=*.svg>` — tracked as the PR 7–8 slices in
  `docs/superpowers/specs/2026-08-25-svg-support-design.md`.
- 100 curated fixtures from the resvg suite's `text/**` tranche land with SVG text, with committed
  goldens: `text/` (25), `tspan/` (25), `text-anchor/` (11), `font-size/` (16), `font-weight/` (12),
  `font-family/` (6), `font-style/` (3), `direction/` (1), `unicode-bidi/` (1). Most of that suite
  names a font this repo does not bundle, so every golden differs from resvg's reference in glyph
  SHAPE — each file was vendored only because its claim is GEOMETRIC (anchoring, per-character
  positioning, whitespace, clipping) and survives substitution, and each was compared against the
  reference by eye first. Fixtures whose reference cannot be matched for font reasons were SKIPPED
  with the reason recorded rather than committing a golden that locks in a substituted rendering as
  if it were correct; see `testdata/svg/resvg/README.md`. The sweep found five real bugs in SVG text
  (`clip-path`/`mask` ignored on a `<tspan>`; bidi reordered before the pen walk, so an absolute `x`
  landed on the wrong glyph; `text-anchor` not direction-relative; `unicode-bidi: bidi-override`
  doing nothing to Latin; and source indentation stealing the following `<tspan>`'s `x`) plus two
  outside `pkg/svg`: `pkg/font` glyph outlines carried a leading drawing op before any move-to, so
  every glyph's `Bounds` stretched back to the origin, and `inline.Reorder` emitted a multi-rune
  cluster glyph once per RUNE, returning more glyphs than it was given.
- 98 further `text/**` fixtures land with the spacing, length, baseline, and decoration properties
  above, all with committed goldens: `baseline-shift/` (22), `text-decoration/` (20),
  `dominant-baseline/` (17), `alignment-baseline/` (11), `textLength/` (10), `letter-spacing/` (7),
  `word-spacing/` (6), `lengthAdjust/` (2), `font/` (2), `font-kerning/` (1). Every one was compared
  against resvg's reference PNG by eye before vendoring. The sweep settled four behaviours the spec
  leaves ambiguous (`letter-spacing`'s trailing gap, whose paint an inherited decoration takes,
  `baseline-shift`'s inertness on `<text>` itself, and the opposite `inherit` scoping of the two
  baseline-selection properties) and caught two outright bugs before any golden was committed:
  `alignment-baseline="baseline"` was resetting to alphabetic instead of deferring, and a decoration
  was one rectangle across the whole run instead of one per baseline frame, which flattened the
  `dy`-list staircase and merged the `rotate`-list's four tilted segments. Also fixed at the source:
  `applyFontSize` silently dropped every ABSOLUTE-unit `font-size` (`font-size="40px"` fell back to
  the inherited value with an "unparseable" log) because the branch delegating px/pt/pc/mm/cm/in to
  `parseLength` was unreachable; no vendored fixture caught it, since the resvg text corpus writes
  bare numbers throughout. Fixtures whose reference cannot be matched — the `(UB)` cases, the ones
  needing `<textPath>`/`writing-mode`/filters, unbundled fonts, real GPOS kerning, or the OS/2 and
  BASE metrics the two degraded baselines want — were SKIPPED with the reason recorded in
  `testdata/svg/resvg/README.md`.
- 74 curated fixtures from the resvg test suite's `masking/**` tranche (MIT, commit
  `d8e064337faf01bc5a9579187a56dbdbe3eacc72`; see `testdata/svg/resvg/README.md` for the earlier
  tranches' counts) land with this feature: `clipPath/` (37) and `mask/` (37), all with committed
  goldens. This tranche's goldens were additionally cross-checked pixel-for-pixel against resvg's own
  reference PNGs (not just visually inspected against fixture intent), the strongest verification
  available — the sweep found and fixed three real bugs (a `visibility:hidden` clipPath child wrongly
  kept in the union, nested/self-referencing masks composing via `min` instead of multiplication, and
  a clipPath child's own nested `clip-path` resolving in the wrong coordinate space when the child
  carried its own transform).

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
  and are tracked as planned work — see [docs/CSS-LAYOUT.md](docs/CSS-LAYOUT.md), "Selectors".
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
  A gradient with `stop-opacity` < 1 ALSO emits vector output now that luminosity soft masks exist: the
  color ramp still emits as the native `/DeviceRGB` `/Shading` above, paired with a second, parallel
  `/DeviceGray` shading (one gray component per stop, equal to that stop's own alpha) painted into a
  Form XObject and wired in as the `sh` operator's ExtGState `/SMask` — same `/Coords`/`/Extend`/offset
  segmentation as the color shading, so the two agree pixel-for-pixel. This is a lift of a real,
  previously-shipped fallback, not new scope: **only the alpha half lifts** — a `reflect`/`repeat`
  spread still has no native `/Extend` equivalent and still rasterizes into an image XObject, logged
  once with the reason, exactly as before.
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

**SVG in HTML** (`pkg/html` foreign-content capture, `pkg/layout/css` vector carrier,
`pkg/svg` intrinsic sizing):

- **`<img src="*.svg">` and inline `<svg>` render as VECTORS end to end — never rasterized.** Both
  route through the pre-existing `layout.VectorItem` / `layout.VectorScene` seam
  (`pkg/layout/page.go`), which `paint.paintVector` hands straight to the `render.Device`. On
  `pkg/render/pdfwrite` that emits real path operators, so a PDF built from an HTML page with an SVG
  image contains **no image XObject at all** — asserted structurally on the emitted PDF bytes rather
  than by a golden, since a golden alone would pass with a rasterized round trip. Deliberately NOT
  routed through `imageCache`/`decodeImageBytes`/`ImageContent`, all of which carry an `image.Image`
  and would force a bitmap round trip; `Fragment.Vector *VectorContent` is a parallel carrier that
  keeps the scene itself, and `svgCache` is the parallel document cache.
- **Intrinsic sizing takes the SVG's UN-DEFAULTED size** via the new `svg.Document.Intrinsic()`
  (`svg.IntrinsicSize`: has-width / has-height / has-ratio). `resolveSize` has already applied CSS's
  300×150 default and the viewBox-extent fallback by the time a `Document` exists — correct for a
  standalone SVG, which is its own sizing authority, and wrong for an embedded one, where the outer
  `<img>`'s CSS supplies an axis and the SVG must contribute only a ratio. All four cases ship:
  explicit `width`/`height` honored; viewBox-only plus one CSS axis derives the other from the ratio
  (`<img src="ratio.svg" style="width:600px">` on a 2:1 viewBox is 600×300, not 600×150); a `width`
  attribute alone derives the height from the ratio; neither a size nor a ratio falls back to
  300×150. The used size then drives the DRAWING as well as the box — a scene whose box differs from
  its own viewport is scaled through the ctm, which stays vector (a coordinate transform, not a
  resample), and is left entirely unwrapped when the box already matches.
- **Inline `<svg>` re-serializes rather than bridging DOM to DOM.** `x/net/html` fully implements
  HTML5 foreign content: the subtree arrives with `Namespace: "svg"` and its camelCase names already
  REPAIRED by `svgTagNameAdjustments` (`clippath`→`clipPath`, `lineargradient`→`linearGradient`,
  `gradientunits`→`gradientUnits`, …). `pkg/html` captures that subtree as markup
  (`Element.ForeignSource`) and `pkg/svg` re-parses it, so the SVG parser stays the single source of
  truth — a `x/net/html.Node` → `pkg/svg` AST bridge would duplicate that package's whole
  element/attribute construction against a second node type and every future parser fix would have
  to land twice. The camelCase round trip is load-bearing (losing it silently kills every gradient
  and clip) and is pinned by a test. The serializer reinstates the `xmlns` declaration inline SVG is
  allowed to omit, and `xmlns:xlink` when the subtree actually uses the legacy prefix — without
  either, `pkg/svg`'s XML parser rejects the markup outright.
- **`<svg>` is replaced content**, so box generation stops there: `<circle>`/`<path>` no longer
  generate meaningless HTML block boxes, and an SVG-internal `<style>` no longer leaks into the HOST
  document's cascade (its rules stay scoped to the SVG, where `pkg/svg` runs its own cascade). An
  `<svg>` behaves as a normal atomic inline: it sits on a line with text, stacks in block flow, and
  carries backgrounds/borders/margins like any replaced element.
- **Untrusted-input bounds.** An SVG reached from an `<img>` is untrusted, and `pkg/svg`'s own
  budgets (the `<use>` instantiation budget, the `<text>` character budget) bound EXPANSION, not the
  size of the source document — so a 32 MB source cap is applied before parsing, logged when it
  fires. Every degradation path (unfetchable, wrong content type, unparseable, over-cap, malformed
  inline markup) reserves the box and paints nothing rather than panicking, and is covered by tests.
- **The vector path is chosen by CONTENT TYPE, never by sniffing bytes**: an unknown/empty content
  type falls through to the raster path, so an unrecognized binary blob is not fed to an XML parser.
  A `data:` URI SVG works, carrying its own type and bypassing the loader entirely.
- **`background-image: url(*.svg)` is vector too.** `resolveBackgroundImage` previously called the
  same `imageCache` as `<img>`, so an SVG background dead-ended there — invisibly, since a
  background that never paints looks like a styling mistake. It now resolves through the SVG cache
  first (`backgroundSource`), and `layout.BackgroundImageItem` carries a `Scene` alongside `Img`,
  exactly one of which is set. The geometry model is shared verbatim between the two: tile size,
  position, origin box and clip box are computed identically (`BackgroundImageItem.TileSize`), and
  only the final draw call differs — `DrawImage` for a raster source, a scaled ctm handed to the
  scene for a vector one. Asserted the same structural way as `<img>`: an SVG background emits path
  operators and **no image XObject**, with a PNG-background control keeping that falsifiable.
- **`background-size` uses the SVG's real intrinsic ratio.** `cover`/`contain` (and a single-axis
  explicit size) go through `svgIntrinsic` — the same un-defaulted accessor the replaced path uses —
  so a viewBox-only SVG contains and covers by its viewBox ratio rather than by the 300×150 default
  `Document.WidthPt`/`HeightPt` already carry. All four size modes ship (`cover`, `contain`,
  explicit lengths with either axis auto, and the initial `auto`).
- **`background-repeat` TILING of an SVG is deliberately NOT implemented, and degrades visibly.**
  Repeating a vector source interacts with the SVG's own viewBox/`preserveAspectRatio` mapping in a
  corner most engines special-case, and a subtly wrong tile grid would be a silent fidelity bug. A
  tiling declaration paints the image ONCE — correctly sized, positioned, and clipped, never blank —
  and logs a warn-once naming the ref. The raster path is untouched and still tiles. Both halves are
  covered by tests, including a control proving the suppression does not leak onto raster
  backgrounds.

**EPUB cover images** (`pkg/epub` manifest, `pkg/omnidoc` frontend):

- **The OPF manifest's cover image is surfaced and rendered.** It was previously parsed and then
  discarded — `parseBook` read `Manifest.Items` only to build `hrefByID` for spine resolution — so a
  cover appeared only when some chapter happened to `<img>` it, which is a minority of real books.
  `Book.CoverHref`/`CoverMediaType` now report it.
- **Both real-world conventions resolve**: the EPUB 3 manifest property `properties="cover-image"`,
  and the EPUB 2 de-facto `<meta name="cover" content="itemID">`. Plenty of EPUB 3 files ship both
  for reader compatibility; both are read, and the normative EPUB 3 property wins when they disagree.
- **Any image format works.** An SVG cover reaches the page through the same vector seam as any
  other `<img src="*.svg">` (so it stays vector and stays sharp at any zoom); a JPEG/PNG cover takes
  the raster path. Neither is privileged, and both are tested.
- **The cover renders alone on the first page, ahead of the spine** — what a cover is, and the only
  position available, since a cover-image manifest item is not part of the reading order and has no
  place within the spine to occupy. It is constrained with `max-width: 100%` so a typical oversized
  cover scales down whole rather than being clipped to a corner crop. Only the width is bounded: the
  engine has no `vh` unit, and a percentage height on a replaced element has no basis in its
  single-axis model, so a height bound would be dropped — one of the two silently.
- **`Book.CoverInSpine` guards the duplicate.** Many EPUB 3 books put a cover XHTML document in the
  spine that `<img>`s the same manifest item; prepending the image would then show it twice. Detected
  both ways (the cover item IS a spine document, or a spine document references it), and the prepend
  is skipped. A book with **no** declared cover is byte-for-byte unchanged — no section, and no
  `break-before` on the first chapter, so it gains no leading blank page. Covered by a test.

**Unsupported-selector diagnostic** (`pkg/css`, shared by HTML/DOCX/SVG):

- **A selector dropped for an unimplemented construct now says so, once.** `pkg/css` supports type,
  class, id, universal, descendant, grouping and the structural pseudo-classes, and has no child
  (`>`), sibling (`+`, `~`), attribute, or namespace selectors. Those already failed SAFE — the rule
  is inert, never mis-matched — but they failed SILENTLY. Design-tool SVG exports lean on
  `[class^="cls-"]` and `.icon > path`, so an inline `<svg>` carrying its own `<style>` lost those
  rules with no hint why. **The selector ENGINE is unchanged** (see docs/CSS-LAYOUT.md, "Selectors"); only the
  diagnostic ships here, the cheap half that item already identifies as worth doing first.
- `Parse` cannot log — `html.UAStylesheet` is a package-level var initialized by `Parse`, so there is
  no caller at that point to hold a logger. The records ride on `Stylesheet.Unsupported` as data
  (deduplicated, capped) and are drained by the two places that already hold both a logger and every
  sheet: `NewResolver` for HTML/DOCX and `pkg/svg`'s index for SVG-internal `<style>`.
- **Warn-once per CONSTRUCT, not per selector**, and never for a UA sheet: a framework stylesheet can
  hold hundreds of `>` rules, and blaming the author for the engine's own UA sheet would fire on
  every document ever rendered. The negative half is tested as hard as the positive — every supported
  selector form records nothing, a drop for a non-construct reason (a pseudo-element, `:not`, `:is`)
  records nothing, and valid `An+B` syntax (`:nth-child(2n+1)`, and the spaced `:nth-last-child(2n +
  1)` the parser already could not handle) is never mis-reported as a sibling combinator.
- Known scope limits, each recorded rather than silently missing: `letter-spacing`/`word-spacing` do
  not inherit across the HTML→SVG boundary, since `ComputedStyle` has no such fields; SVG background
  tiling degrades to one paint (above); and the selector engine itself is NOT fixed — only the
  diagnostic. (CSS `filter:` on HTML boxes was listed here as deferred; it has since shipped —
  see below.)

**CSS `filter:` on HTML boxes** (`pkg/css` cascade, `pkg/layout/css` bracket emission,
`pkg/layout/paint` the pixel chain; showcase section 18, `htmldoc-p18.png`):

- **All ten shorthand functions render**: `blur()`, `drop-shadow()`, `grayscale()`, `sepia()`,
  `saturate()`, `hue-rotate()`, `invert()`, `brightness()`, `contrast()`, `opacity()`. A list
  composes **left to right**, each function consuming the previous one's output. The effect applies
  to the element's WHOLE rendering — its background and border as well as its contents and
  descendants — which is the one structural difference from the clip bracket, whose pair deliberately
  excludes the box's own border box.
- **No new pixel math.** Every function is lowered to the Filter Effects specification's own
  equivalent primitive and run through **`pkg/svg/filter`**, the same corpus-tested code the
  `<filter>` element uses, so the blur premultiplication, the colour-matrix arithmetic and the
  colour-space handling are inherited rather than reimplemented and the two spellings of
  `filter: invert(1)` cannot drift apart. The parser is the already-shared `pkg/filtereffects`.
- `invert`/`brightness`/`contrast`/`opacity` are the four the spec expresses as an
  **`feComponentTransfer` with LINEAR transfer functions** — a primitive the SVG series deliberately
  deferred. Rather than reviving it, each is written as the equivalent affine per-channel map and
  evaluated through `feColorMatrix`, which computes exactly `slope·v + intercept` per channel. A
  linear transfer function IS an affine map, so this is an exact reformulation, not an approximation.
- **The CSS functions run in sRGB**, not the linearRGB SVG's own primitives default to. Getting this
  backwards makes every `blur()` and `drop-shadow()` visibly lighter than a browser's.
- **`drop-shadow()`'s colour is resolved at LAYOUT time**, because an omitted colour (and
  `currentColor`) means the element's own `color` property, which only the cascade knows. It rides on
  the item as `layout.FilterItem.ShadowColors`.
- **CSS does not clip a filter to a region** (unlike SVG's `filterUnits`/`x`/`y`/`width`/`height`), so
  the offscreen surface covers the border box UNIONED with what the bracketed items actually paint,
  grown by three standard deviations for a blur plus a drop-shadow's own offset. A border-box-sized
  surface would crop a blur dead at the box edge and drop overflowing content outright.
- **A filtered box establishes a BFC and a stacking context**, both spec-required and both
  load-bearing: the BFC makes the box flatten through ONE `AppendItems` call (so one balanced bracket
  wraps decorations and content together), and the stacking context stops a positioned descendant
  bubbling past the bracket and rendering unfiltered.
- **`rem` in a filter length resolves against the ROOT font size**, per CSS Values, via a root size
  recorded once per layout at `layoutTree`. (The cascade's own `parseLength` still folds `rem` into
  `em` for every other property — a separate, pre-existing approximation this does not change.)
- Error handling matches CSS's: `filter: none` and **any** invalid value leave rendering
  **byte-identical** to a document with no declaration, since an invalid declaration is ignored
  ENTIRELY rather than applying the entries that did parse.

Honest degradations. Every one of them logs on the raster path; the one place a
cap stays silent is named explicitly below rather than glossed. `pkg/layout/paint`
carries the same optional `Logf` the rest of the engine uses — see the painter's
own entry further down:

- **PDF output paints filtered content UNFILTERED.** `pkg/render/pdfwrite`'s `RenderOffscreen`
  declines by design — PDF has no filter operator and a blur has no vector representation — so the
  bracket degrades to a plain transparency group: the content is present, correctly placed, and
  still **vector** (no image XObject is emitted, asserted directly on the operators). Rasterizing a
  page region to fake the effect is deliberately refused. Logged once per document.
- **The page-break approximation.** Brackets are page-local: pagination splits the fragment tree, not
  the item list, so a box straddling a break emits its own balanced pair on each page. That is
  **exact** for the eight per-pixel colour adjustments and an **approximation** for the two spatial
  ones — a blur cannot sample content that fell on the other page, so the seam differs from an
  unbroken render. Logged once per document for a split `blur()`/`drop-shadow()` only; a split
  `grayscale()` stays silent.
- **`filter: url(#id)` is dropped** with a warn-once: it references an SVG `<filter>` element, which
  an HTML box tree cannot resolve. The surrounding shorthand functions still apply.
- A degenerate, off-device, or over-cap region (`maxCSSFilterPixels`, 4M pixels — the same bound the
  SVG side uses, and meaningful for the same reason: the surface is clipped to the device and its
  origin shifted to (0,0) before allocating) degrades to painting the content unfiltered, **logged
  once per page** with the specific cause named (over-cap, off-device, and degenerate-box are three
  different problems and read as three different lines). Note 4M is NOT above every legitimate page:
  a 300 DPI A4 page is ~8.7M pixels, so a full-page filter renders filtered at 72 and 150 DPI and
  unfiltered at 300 — the over-cap line names the cap and points at the DPI, since that outcome is
  otherwise unexplainable from the output. The surface also covers the border box UNIONED with the
  bracketed content's extents (CSS does not clip a filter's input), so one far-flung positioned
  descendant inflates the hull and can reach the cap on an otherwise modest box.
- Filters nested more than 4 deep degrade to unfiltered, **logged once per page**, matching the SVG
  side's nesting bound — each live level holds its own offscreen surface, so depth bounds concurrent
  memory rather than just CPU.
- **Still silent, deliberately:** the two caps above do NOT log on the **PDF** path. `pkg/render/
  pdfwrite` calls plain `PaintPage`, because its `RenderOffscreen` always declines and it already
  reports once per document that every filter in the file paints unfiltered — a second, narrower
  reason for a subset of brackets would annotate an outcome already stated for all of them, and it
  would have to fire from the concurrent per-band render phase, where the once-per-page flags are
  per-band and so could repeat. A PDF caller therefore learns THAT its filters were not applied, but
  not that a particular one would also have exceeded a cap.
- Not implemented: `backdrop-filter` (it needs the backdrop, not the element's own pixels — a
  different mechanism entirely), and native PDF filter emulation via soft masks.

**`paint.PaintPageWithOptions` — an optional diagnostics logger on the painter.** `PaintPage` gained
a sibling entry point taking `paint.Options{Logf: ...}`, rather than a widened signature, so all
existing callers stay source-compatible and byte-identical; the zero `Options` is exactly the old
behavior. The `Logf func(string, ...any)` signature matches every other degradation logger in the
engine (`svg/draw.Renderer.Logf`, `raster.Options.Logf`, `pdfwrite.Options.Logf`), so one func
threads through the whole pipeline. With no logger the warn-once state is a **nil pointer** — nothing
is allocated and nothing is captured on the per-page hot path, and the logger-less path is pinned by
a test. Notices are warn-once **per cause, per page**, allocated per call and never stored, so the
concurrent page fan-out cannot race on them or suppress each other's first line. The raster/reflow
backend passes the caller's `Logf` through automatically. A test asserts the *captured output* of
each degradation (not merely that the branch runs), and another pins that attaching a logger moves
no pixels.
