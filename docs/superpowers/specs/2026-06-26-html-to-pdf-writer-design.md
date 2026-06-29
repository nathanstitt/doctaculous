# HTML → PDF via a PDF-writer device (sub-project A)

**Status:** design approved, ready for implementation plan
**Date:** 2026-06-26
**Author:** Nathan Stitt

## Goal

Produce real PDF files from HTML (and, as a free bonus, DOCX) by adding a second
`render.Device` implementation that emits a PDF document instead of pixels. The toolkit
already has the input pipelines (HTML/DOCX parse → CSS cascade/lower → layout engine →
positioned fragment stream) and one output backend (`pkg/render/raster`). This sub-project
adds a sibling output backend, `pkg/render/pdfwrite`, plus the small seam and plumbing
changes needed to emit selectable, embedded-font PDF text.

This is the first step toward the toolkit's ultimate goal of translating between formats.
The chosen first direction is **HTML → PDF**, because the entire HTML layout engine is
reused unchanged and the work is a clean, well-bounded output backend at an existing seam.

## Scope

### In scope (sub-project A)

- A new `pkg/render/pdfwrite` package implementing `render.Device` by emitting PDF objects
  and content streams to an `io.Writer`.
- A writer-side PDF object model + serializer (classic xref table + trailer, Flate-compressed
  content streams), independent of the parse-oriented `pkg/pdf`.
- Embedded fonts with **real selectable text**: CIDFontType2/Type0 subset with `Identity-H`
  encoding and a `/ToUnicode` CMap for searchability.
- A text-aware glyph seam: extend `render.Device` with `DrawGlyph(GlyphRef)` carrying font
  identity, glyph id, source runes, transform, and advance.
- **Simple page fragmentation**: slice the laid-out content into fixed-size pages (default
  US Letter), breaking between line boxes / block boundaries — never mid-line. Respect
  `break-inside: avoid` on a block that fits on a page by itself.
- Optional **print CSS**: a `PDFOptions.Print` flag that makes the cascade honor
  `@media print` (and exclude screen-only rules). Requires `pkg/css` to stop discarding
  `@media` blocks.
- Public API: `doctaculous.ConvertHTMLToPDF(...)` and `(*Document).WritePDF(...)`.
- DOCX → PDF falls out for free (same reflow engine → same `Device`); covered by a fixture.

### Out of scope (explicitly deferred)

- **Full CSS paged media** — `@page` rules, page margin boxes (running headers/footers),
  page counters, orphans/widows, named pages. This is **sub-project B**, designed to layer
  on top of the simple fragmentation here.
- **PDF → HTML** (semantic reconstruction) — a separate, much harder later sub-project.
- A CLI subcommand (`doctaculous convert --to pdf`) — thin wrapper to add later; not required
  for this spec.

### Non-goal but designed-for: the text-extraction backend

The **next target** after this is a **text backend** — another `render.Device` implementation
that extracts text positioned roughly like the source document (tables/columns keep their
shape). It is NOT built here, but the glyph seam introduced here is deliberately shaped to
serve it. `GlyphRef` carries the union of what all three output backends need (raster,
pdfwrite, text), and is format-neutral so the PDF content interpreter can populate it too
(enabling PDF → text later). Nothing in this sub-project may make the seam HTML-specific.

## Architecture & data flow

```
HTML / DOCX ──► layout fragment/Item stream ──┐
PDF ──► content interpreter ───────────────────┼──► render.Device ──┬──► raster   (pixels)         [today]
                                                │                   ├──► pdfwrite (PDF bytes)       [this sub-project]
                                                │                   └──► text     (positioned text) [next target — designed-for]
```

The `render.Device` interface remains the single seam. Input pipelines and the layout engine
are untouched. The text-aware glyph seam (`DrawGlyph(GlyphRef)`) is the shared abstraction for
all output backends.

### Components

1. **`pkg/render/pdfwrite`** (new package) — implements `render.Device`; four files (§ below).
2. **PDF object serializer** — `pkg/render/pdfwrite/object.go`; writer-side object model +
   serializer. Independent of `pkg/pdf`.
3. **Font subsetting/embedding** — `pkg/render/pdfwrite/font.go`; CIDFontType2/Type0 subset,
   Identity-H, `/ToUnicode`.
4. **`pkg/font` additions** — expose raw program bytes, per-GID advance, units-per-em.
5. **`render.Device` extension** — `DrawGlyph(GlyphRef)`.
6. **`pkg/layout` `GlyphItem` extension** — carry face + GID + runes + advance.
7. **Public API** — `ConvertHTMLToPDF`, `(*Document).WritePDF`, `PDFOptions`.
8. **`pkg/css` `@media` support** — parse `@media print/screen/all` (today discarded), tag
   rules by media, filter at cascade time by the active media type.
9. **Page fragmentation** — `pkg/render/pdfwrite/page.go`; simple vertical slicing.

## Section 2 — The `render.Device` extension & `GlyphRef` contract

Add **one method** to `render.Device`:

```go
// DrawGlyph paints one shaped glyph already placed in device space.
// Backends that only rasterize may render g.Face's outline for g.GID and
// ignore Runes/Advance; backends that emit text (PDF, text extraction)
// use Runes and Advance.
DrawGlyph(g GlyphRef)

type GlyphRef struct {
    Face      *font.Face   // font identity: program bytes + GID space (for embed/subset)
    GID       uint16       // glyph id within Face
    Runes     []rune       // source characters this glyph represents (the cluster);
                           // used for ToUnicode + text extraction (handles ligatures)
    Transform Matrix       // em-space (Y-up, 1 em = 1 unit) → device space;
                           // carries position, size, rotation, skew
    Advance   float64      // horizontal advance in device units
    Color     FillColor
    Blend     string       // /BM blend mode ("" = Normal), matching FillGlyph today
}
```

**Rationale**

- `Transform` (not a bare x,y) inherits the existing matrix discipline; rotation/scale/skew
  come along free for both raster and the PDF text matrix.
- `Runes []rune` handles ligature clusters (one GID ↔ "ffi" and the inverse); pdfwrite folds
  it into `/ToUnicode`, the future text backend uses it directly.
- `Face` is a stable identity the writer keys its subset table on (one subset per face) and
  from which it pulls program bytes.
- `GlyphRef` is format-neutral: the reflow paint layer and (later) the PDF content interpreter
  can both populate it.

**Migration of the existing seam.** `FillGlyph(outline, color, blend)` **stays** — the PDF
*interpreter* still uses it (interpreted PDF glyphs may lack a clean face/GID/runes, e.g.
Type3 fonts or odd encodings). The **reflow paint layer** switches from `FillGlyph` to
`DrawGlyph` because it knows face + GID + runes.

- **raster** implements `DrawGlyph` by looking up the outline for `Face`+`GID` and filling it
  — identical pixels to today, so the existing goldens are unchanged.
- **pdfwrite** implements both: `DrawGlyph` → real text; `FillGlyph` → vector-outline fallback
  (not exercised by HTML→PDF, present for completeness/robustness).

**Layout-side plumbing.** `layout.GlyphItem` gains `Face *font.Face`, `GID uint16`,
`Runes []rune`, `Advance float64` (it already carries the outline + transform). The inline
core already has all of this at shape time; it is currently dropped when the item is built.
The paint layer reads them into `GlyphRef`.

**`pkg/font` additions** (consumed by pdfwrite, ignored by raster):

```go
func (f *Face) ProgramBytes() (data []byte, kind ProgramKind) // raw sfnt/CFF for embedding
func (f *Face) GlyphAdvance(gid uint16) float64               // for the /W widths array
func (f *Face) UnitsPerEm() float64
```

## Section 3 — `pkg/render/pdfwrite` internals

Four files, each one bounded responsibility.

### 3a. `object.go` — writer-side PDF object model + serializer

A write-only object model, deliberately separate from the parse-oriented `pkg/pdf`:

```go
type objID int
type writer struct { objs []object; /* ... */ }
func (w *writer) alloc() objID
func (w *writer) put(id objID, obj object)
func (w *writer) addStream(dict Dict, data []byte) objID // Flate-compress, set /Length + /Filter
func (w *writer) serialize(out io.Writer) error          // header, body, xref table, trailer
```

Values: `Dict`, `Array`, `Name`, `Ref`, `Int`/`Real`, `String`, `Bool`, `Null`, `Stream`.
Classic xref **table** + trailer (not xref streams — simpler, universally readable). Content
streams Flate-compressed via stdlib `compress/zlib`. Proper escaping for strings/names.

### 3b. `device.go` — the `render.Device` implementation

Holds the `writer`, a list of per-page content buffers, a graphics-state stack mirroring
`Save`/`Restore`, the current page's resource sets (fonts, xobjects, extgstates, shadings),
and the subset collector. Each method appends operators to the current content buffer:

- `Fill` / `Stroke` → path ops (`m`/`l`/`c`/`re`/`h`) + `f`/`f*`/`S`; color via `rg`/`g`/`k`;
  line attrs via `w`/`J`/`j`/`M`/`d`.
- `PushClip` → path + `W`/`W*` then `n`.
- `Save` / `Restore` → `q` / `Q`.
- `DrawImage` → register an image XObject (see below), emit `cm` + `Do`; `alpha`/`blend` →
  an ExtGState (`/ca`/`/CA`/`/BM`) referenced via `gs`.
- `DrawGlyph` → text mode: `BT` … `Tf`/`Tm`/`Tj` … `ET`, recording (face, GID, runes) in the
  subset collector; consecutive glyphs in the same face/size coalesce into one `BT…ET` run
  using positioned `Tj`/`TJ` where possible.
- `FillGlyph` → vector-outline fallback (path fill); not exercised by HTML→PDF.
- `FillShading` → emit a `/Shading` resource + `sh` (axial/radial/function). The HTML layout
  engine does not currently emit gradients, so this is a thin path; mesh/pattern shadings are
  not produced by the engine.

Images reach the device as a decoded `image.Image` (the reflow engine decodes `<img>` at layout
time). The writer therefore embeds them as image XObjects from decoded samples: RGB or gray
samples, Flate-encoded; soft masks via a separate `/SMask` image XObject when the source has an
alpha channel. (JPEG `/DCTDecode` passthrough is a later size optimization — the device sees the
decoded image, not the original bytes — and is out of scope here.)

### 3c. `font.go` — CIDFontType2/Type0 subset embedding

Per face used in the document:

- Collect the set of GIDs actually drawn.
- Build a glyph subset: for TrueType, retain used glyphs **plus composite-glyph dependencies**,
  remap to a compact GID range, and rewrite `loca`/`glyf`/`hmtx`/`maxp`/`head` (+ a minimal
  `cmap`); for CFF, embed the CFF program (subset if practical, else embed whole + log).
- Emit a `Type0` font with `Identity-H` encoding, a `CIDFontType2` (or `CIDFontType0` for CFF)
  descendant, a `/W` widths array from `Face.GlyphAdvance`, a `FontDescriptor` with
  `/FontFile2` (or `/FontFile3`) = subset bytes.
- Emit a `/ToUnicode` CMap built from the recorded `Runes`, so text is searchable and
  copy-paste yields the source characters (including ligature clusters).

This is the single hardest unit and gets the most isolated tests (§5b).

### 3d. `page.go` — page assembly + simple fragmentation

Receives the laid-out content for the whole document plus `PDFOptions` (page size, margins).
**Simple fragmentation:** walk the fragment/Item stream in vertical order; start a new page
when the next block/line box would cross the page content-box bottom; translate each page's
items so the page-top maps to the PDF page's top-of-content; **never split a single line box**.
`break-inside: avoid` on a block keeps it whole if it fits on a page by itself (else it splits
as normal). Each page = one content stream + shared resources.

(Full `@page` / margin boxes / counters / orphans+widows = sub-project B, layered on this.)

### Concurrency

PDF generation is split into a **parallel render phase** and a **sequential assembly phase**,
matching the project's "multi-page work fans out across goroutines, bounded pool sized to
`GOMAXPROCS`" rule:

- **Parallel (per page, no shared state):** each page-band is rendered on a worker into its
  **own `pageDevice`** with its **own local `fontEmbedder`/image list**. The output of a worker
  is a pure value — the content-stream bytes plus the set of faces/images that page used. No two
  workers touch shared mutable state, so there is no lock contention and `go test -race` is clean.
  Font subsetting (the CPU-bound glyf rewrite) also runs in this phase, keyed per face.
- **Sequential (assembly):** a single goroutine folds the per-page results into the shared object
  table and xref, **de-duplicating faces across pages** (one embedded subset per face even when
  many pages use it) and assigning final object ids. The object table / xref is never shared
  across goroutines, so it stays lock-free.

The fan-out is a bounded worker pool sized to `GOMAXPROCS` (overridable). Results are reassembled
in page order so output is deterministic. Context cancellation is honored between dispatched pages.
This is the *write* path; it does not conflict with the "parsed `*Document` is read-only and
shared without locks" rule, which governs the *read*/raster path.

## Section 4 — Public API & error handling

### Public API (`pkg/doctaculous`)

```go
type PDFOptions struct {
    PageSize Size     // default US Letter (612×792 pt); presets: A4, Letter, Legal
    Margins  Margins  // default 0.5in all sides
    Print    bool     // honor @media print in the cascade (default false → screen context)
    Title    string   // optional /Info metadata
}

// ConvertHTMLToPDF converts an HTML document to PDF, writing to w.
func ConvertHTMLToPDF(ctx context.Context, in io.Reader, w io.Writer, opts PDFOptions) error

// WritePDF writes an opened reflow Document (HTML or DOCX) to w as PDF.
func (d *Document) WritePDF(ctx context.Context, w io.Writer, opts PDFOptions) error
```

`ConvertHTMLToPDF` is sugar over `OpenHTML` + `WritePDF`. Because the seam is the reflow
engine, **`WritePDF` works for DOCX-opened documents too**, so DOCX → PDF falls out for free
(still requires its own fixture/test).

### `PDFOptions.Print` semantics

When `true`, the cascade includes `@media print` rules and excludes `@media screen`-only rules;
when `false`, the reverse. This requires the `pkg/css` `@media` work (component 8): parse the
block (today discarded), tag each rule with its media, and filter at cascade time by the active
media type. Only media **types** (`print`/`screen`/`all`) are honored; full media **queries**
(width/resolution features) degrade gracefully to "matches if the type matches" + debug log.

### Error handling

Per project conventions (never panic on bad input; wrap with `%w`; sentinels for branchable
conditions):

- `io.Writer` failures propagate wrapped: `fmt.Errorf("write pdf: %w", err)`.
- A face whose program bytes cannot be extracted/subset **degrades**: draw that face's glyphs
  as vector outlines (the `FillGlyph` path) + debug log, rather than failing the document. Text
  stays visible, just not selectable for that face. Sentinel `ErrFontEmbed` (wrapped, non-fatal).
- An unsupported device op from an exotic input (e.g. a mesh `FillShading` the HTML engine never
  emits) → skip + debug log, no panic.
- Page assembly **recovers per page** so one malformed page cannot abort the whole PDF; the page
  is logged and emitted blank-or-partial.
- Context cancellation honored between pages.

## Section 5 — Testing strategy

Every unit gets isolated tests; the chain stays hermetic (no network; generated fixtures or
tiny in-repo faces).

### 5a. Object serializer (`object.go`)

Round-trip: build objects → serialize → **re-parse with the project's own `pkg/pdf`** and
assert structure. Uses the existing parser as the oracle. Covers xref correctness, stream
`/Length`, the Flate filter, nested dicts/arrays, and escaping in strings/names.

### 5b. Font subsetting (`font.go`) — the riskiest unit, tested hardest

- Subset a known face to a GID set → re-parse the embedded `FontFile2`/`3` and assert the
  retained glyphs (and composite-glyph deps) are present, dropped ones absent, and
  `loca`/`hmtx`/`maxp` are internally consistent.
- `/ToUnicode` maps the emitted CIDs back to the original runes, including a ligature cluster.
- Degradation: a face that cannot be subset falls back to outlines (no error; glyphs still
  painted).

### 5c. Device ops (`device.go`)

Per-operator: feed a tiny synthetic Item/fragment stream and assert the emitted content-stream
operators (fill → `f`; clip → `W n`; image → `Do` + ExtGState; glyph → `BT…Tj…ET`). String-match
operators in the decompressed stream.

### 5d. Fragmentation (`page.go`)

Synthetic tall content → assert the page count for a given page size, that no line box is split
across a boundary, and that `break-inside: avoid` keeps a block whole.

### 5e. End-to-end golden / round-trip (the keystone)

Leverage the existing trusted raster path as the correctness oracle:

- Convert a set of HTML fixtures → PDF, then **parse + rasterize that PDF with the project's own
  pipeline** and compare to the **direct HTML raster golden** for the same fixture (same
  per-pixel ±4 / 0.2%-differing-pixel budget already used). This proves
  HTML → PDF → pixels ≈ HTML → pixels, i.e. the writer is faithful, using machinery already
  trusted.
- New fixtures: `pdf-text`, `pdf-paragraphs`, `pdf-image`, `pdf-borders`, `pdf-multipage`,
  `pdf-print-media` (asserts a `@media print` rule changed the output), and a **`docx→pdf`**
  fixture for the free bonus.
- A searchable-text assertion: parse the generated PDF, extract text for a fixture, and assert
  the words are present — proving real text, not outlines.

### 5f. `@media` cascade (`pkg/css`)

Unit tests: a sheet with `@media print` + `@media screen` rules cascades differently under
`Print:true` vs `false`; unsupported media queries degrade gracefully.

### 5g. Race + hermetic

`go test -race ./...`; no network; all fixtures generated or tiny committed faces already in-repo.

## File-level summary

| File / package | Responsibility | New? |
|---|---|---|
| `pkg/render/device.go` | Add `DrawGlyph` + `GlyphRef` | edit |
| `pkg/render/raster/*` | Implement `DrawGlyph` via outline (pixels unchanged) | edit |
| `pkg/font/family.go` | `ProgramBytes`, `GlyphAdvance`, `UnitsPerEm` | edit |
| `pkg/layout` `GlyphItem` | Carry `Face`/`GID`/`Runes`/`Advance` | edit |
| `pkg/layout/paint/paint.go` | Call `DrawGlyph` instead of `FillGlyph` | edit |
| `pkg/css` | Parse + tag `@media`; filter by active media type | edit |
| `pkg/render/pdfwrite/object.go` | Writer-side PDF object model + serializer | new |
| `pkg/render/pdfwrite/device.go` | `render.Device` impl → content streams | new |
| `pkg/render/pdfwrite/font.go` | CIDFontType2/Type0 subset + ToUnicode | new |
| `pkg/render/pdfwrite/page.go` | Page assembly + simple fragmentation | new |
| `pkg/doctaculous/*` | `ConvertHTMLToPDF`, `WritePDF`, `PDFOptions` | new/edit |

## Open follow-ups (out of scope, recorded for later)

- **Sub-project B:** full CSS paged media — `@page` (size/margins), page margin boxes (running
  headers/footers), page counters, orphans/widows, named pages; layered on this fragmentation.
- **Text backend (next target):** a `render.Device` that extracts positioned text (tables keep
  shape), consuming the `GlyphRef` seam introduced here; also fed by the PDF content interpreter
  for PDF → text.
- CLI `convert --to pdf` subcommand.
- CFF subsetting (vs whole-program embed) if file size warrants.
- Full media queries (width/resolution features) beyond media types.
