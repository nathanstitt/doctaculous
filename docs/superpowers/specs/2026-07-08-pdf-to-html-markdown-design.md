# PDF → HTML & Markdown (with automatic table recognition)

**Status:** implemented
**Date:** 2026-07-08
**Author:** Nathan Stitt

## Goal

Convert a PDF to HTML or Markdown — the reverse of the reflow → PDF/Markdown paths, and
the hardest direction. PDF has no semantic structure: a page is a bag of positioned
glyphs and vector paths. Paragraphs, headings, lists, and tables must be *reconstructed*
from geometry (document layout analysis). The headline requirement is **automatic table
recognition**.

## Prior art (adopted)

All algorithmic (non-ML), implementable in pure Go on what the content interpreter
exposes:

- **Text reconstruction** — cluster glyphs into words (horizontal gap threshold), words
  into lines (shared baseline), lines into blocks. This is pdfplumber's char→word→line
  grouping (Nurminen thesis).
- **Reading order / columns** — the **XY-cut** recursive algorithm: split at the widest
  vertical whitespace valley (columns) then horizontal valleys (blocks), emit in
  column-then-top-to-bottom order.
- **Tables, two auto-selected strategies** (pdfplumber / Camelot):
  - **Lattice** — ruling lines from the page's vector graphics (thin strokes + rectangle
    edges) → snap/merge → intersections → cell grid → assign words to cells.
  - **Stream** — infer columns from vertical whitespace gaps consistent across rows when
    there are no rules.

## Architecture

```
pdf.Page ─► content.Interpreter (paints AS TODAY) ─► raster/pdfwrite   (unchanged, byte-identical)
                    │
                    └─(opt-in sinks)─► pkg/pdf/extract ─► synthetic *cssbox.Box ─┬─► pkg/render/markdown (reuse)
                                       glyphs + rules                            └─► pkg/render/htmlwrite (new)
```

The extractor builds the **same `cssbox` tree** the reflow frontends produce, so the
Markdown writer is reused unchanged and the new HTML writer serves any future
`cssbox`-producing path.

## Design

### 1. Interpreter capture sinks (`pkg/pdf/content/sink.go`)

Two optional, paint-neutral callbacks on `content.Options`, nil by default (→
byte-identical paint, verified by the unchanged raster/PDF golden suite):

- `TextSink func(TextGlyph)` — emitted from `drawGlyph` with the glyph's rune and its
  device-space origin/size/advance (derived from the same text-rendering matrix that
  places the outline). Emitted even for render mode 3 (invisible OCR text layers are
  exactly what an extractor wants).
- `GraphicsSink func(VectorOp)` — emitted from `fillPath`/`strokePath` with the
  device-space path and (for strokes) width, so a table detector can recover rules.

A literal `DrawGlyph` reuse was rejected: `raster.DrawGlyph` needs a `render.GlyphFace`
the PDF path has no equivalent for, so switching the paint path would break raster. The
sink is the seam the `render.Device` comment already anticipated ("text extraction").

### 2. Extraction core (`pkg/pdf/extract`)

- `collect.go` — drives the interpreter per page with the two sinks (a no-op device +
  a minimal `content.Resources`), accumulating glyphs and rules (rules recovered from
  thin axis-aligned strokes and thin filled rectangular bars). Recovers per page.
- `words.go` — glyph→word→line grouping. A word breaks when the horizontal gap exceeds
  `0.15 × font-size` (measured: intra-word gaps ≈ 0em, inter-word/space gaps ≈ 0.25em, so
  0.15 sits safely between — the pdfwrite path emits no space glyphs, only position gaps).
- `blocks.go` — **XY-cut** reading order + block classification (heading by relative
  size, list item by leading bullet/number, else paragraph). Gutter-straddling lines are
  split before the cut so multi-column pages read down each column.
- `tables.go` — lattice + stream detection, auto-selected (lattice when a ≥2×2 ruling
  grid is found, else stream, else prose), logging which fired.
- `lower.go` — `Lower(doc, logf) (*cssbox.Box, error)` builds the synthetic tree
  (root→body, headings/paragraphs/list-items/tables) using the DOCX hand-construction
  patterns, so downstream writers consume it unchanged.

### 3. HTML writer (`pkg/render/htmlwrite`)

`cssbox → HTML`, structured like the Markdown writer. Headings→`<h1..6>`,
paragraphs→`<p>`, lists→`<ul>/<ol>`, emphasis→`<strong>/<em>/<s>/<code>`, links→`<a>`,
images→`<img>`, and **tables→`<table>` with native `colspan`/`rowspan`** (HTML expresses
spans natively, unlike GFM — so extracted tables round-trip merges losslessly). Full
document (default) or `Fragment`. Format-neutral: also enables HTML/DOCX → HTML later.

### 4. Public API + CLI

- `pdfRenderer` implements the existing `reflowTree` interface via lazy, cached
  extraction (`cssboxRoot()`), so `(*Document).WriteMarkdown`/`WriteText`/`WriteHTML` all
  work on PDF inputs.
- `WriteHTML` + `ConvertPDFToMarkdown` / `ConvertPDFToHTML`.
- CLI: `tomd` accepts `.pdf`; new `tohtml` subcommand; inference maps `.html`→tohtml.

## Text quality & known limits (this PR)

- **ToUnicode CMaps are out of scope.** Type0/CID fonts (many CJK / subsetted PDFs) yield
  `Rune==0`; those runs degrade to a placeholder + are logged. Simple-font (common
  Western) PDFs extract real text. ToUnicode is the top follow-up.
- **Emphasis** (bold/italic) is reserved: `GlyphSource` does not yet surface font
  weight/slant, so headings key on relative *size* (populated) not weight.
- **Heading level** is size-bucketed (an `h2` may map to `###`).
- **Lists**: ordered/unordered boundary detection is approximate (HTML output may merge
  adjacent lists).
- **Empty-body PDFs**: `ConvertHTMLToPDF` emits a 0-page PDF the parser rejects — a
  pre-existing `pdfwrite`/parser round-trip inconsistency, not an extractor bug; flagged.

## Testing

- Interpreter sink unit tests + nil-sink byte-identical guard; full pre-existing
  raster/PDF golden suite passes unchanged.
- `pkg/pdf/extract` per-stage unit tests (word grouping, XY-cut incl. fused-column split,
  lattice cell grid + spans, stream columns, block classification).
- `pkg/render/htmlwrite` round-trip tests (tags, native colspan/rowspan single-emit).
- End-to-end HTML→PDF→extract round trips (`pdf_extract_test.go`): heading/paragraph
  survival, **ruled-table → GFM pipe table**, PDF `Document` satisfies the write methods.
- Extraction goldens (`pdfx-*.md`/`.html`): headings, list, ruled table, two-column.
- CLI tests for `tomd` (PDF input) and `tohtml`; command inference.
- Degradation: invalid-PDF error path, vector-only empty page.
- `go test -race`, `go vet`, `golangci-lint` clean.

## Follow-ups

- **ToUnicode CMap parsing** (`pkg/pdf/font`) — the biggest quality lift; the extractor
  consumes it transparently.
- Font weight/slant through `GlyphSource` → emphasis + weight-based heading detection.
- ML/vision table detection, scanned-PDF OCR, RTL/bidi order, rotated/nested tables,
  figure/caption association, the empty-PDF writer/parser round-trip fix.
```
