![doctaculous — any document to any other document, in pure Go](docs/assets/doctaculous-banner.svg)

[![CI](https://github.com/nathanstitt/doctaculous/actions/workflows/ci.yml/badge.svg)](https://github.com/nathanstitt/doctaculous/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nathanstitt/doctaculous.svg)](https://pkg.go.dev/github.com/nathanstitt/doctaculous/pkg/doctaculous)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00758d?labelColor=211c17)](go.mod)
[![CGo free](https://img.shields.io/badge/CGo-none-c8401a?labelColor=211c17)](#why)
[![MIT](https://img.shields.io/badge/license-MIT-c8401a?labelColor=211c17)](LICENSE)

A pure-Go document toolkit: parse, lay out, rasterize, extract, convert, and edit
documents. It brings its own PDF interpreter and its own CSS layout engine. No
CGo, no native bindings, no copyleft.

## Read/Write fourteen formats and convert between them

Every supported format is both an input **and** an output. All 182 ordered pairs
convert (a format to itself is a deliberate `ErrSameFormat`):

> `pdf` · `docx` · `xlsx` · `pptx` · `epub` · `rtf` · `html` · `md` · `txt` · `csv` · `tsv` · `png` · `jpeg` · `webp`

Plus two input-only formats: **`heic`** and **`svg`**. HEIF/HEIC stills decode
through an in-tree pure-Go HEVC intra decoder (no libheif, no CGo), so an
iPhone photo converts to any of the fourteen outputs and drops into HTML/EPUB
`<img>` unchanged. Standalone `.svg`/`.svgz` documents open as a vector page —
paths, shapes, transforms, solid fill/stroke — and convert to PDF as real
vectors, not a rasterized image. WebP reads lossy VP8, lossless VP8L, and
alpha; it writes lossless VP8L, so it replaces PNG rather than JPEG.

```sh
doctaculous convert report.docx report.pdf         # typeset through the CSS engine
doctaculous convert https://example.com page.png   # fetch, lay out, rasterize
doctaculous convert statement.pdf tables.xlsx      # tables recovered from ruling lines & whitespace
doctaculous convert book.epub book.docx            # ebook → Word, images and all
doctaculous convert notes.md deck.pptx             # each heading becomes a slide
doctaculous convert photo.heic photo.webp          # iPhone photo → lossless WebP
doctaculous rasterize input.pdf --page 1 --out page1.png --dpi 150
```

`convert` sniffs the input format from **content first** (magic bytes, OPC/zip
classification, HTML sniffing), then the extension; the output format comes from
the output extension. `--from`/`--to` override both. HTML input can also be an
`http(s)` URL, with relative resources, `data:` URIs, and web fonts resolved.
Image output writes one page by default, or many with
`--pages all` and a `%d` in the output name (`page-%d.png`);
`--max-width`/`--max-height` produce fit-within thumbnails without knowing page
sizes up front, and `--crop <gravity|saliency> --crop-size WxH` fills an exact
pixel box rather than fitting within one — the difference between a 720×720
square and a 720×540 fit. `--crop saliency` picks the window from image content
(edge energy, saturation, skin likelihood, centre bias; no model, pure Go).

**Demo:** [`testdata/htmldoc/index.html`](testdata/htmldoc/index.html) is the
rendering specimen, one document exercising every implemented HTML/CSS/image
slice. [`docs/assets/htmldoc-specimen.pdf`](docs/assets/htmldoc-specimen.pdf)
is the PDF `doctaculous convert` typesets from it: 18 Letter pages with running
headers, page counters, a WOFF2 script wordmark, floats, flexbox, grid, and
tables, all as selectable text.

## Quick start

```sh
go install github.com/nathanstitt/doctaculous/cmd/doctaculous@latest
```

Or as a library:

```go
import "github.com/nathanstitt/doctaculous/pkg/doctaculous"

// Open sniffs the format from content — PDF, DOCX, XLSX, EPUB, RTF, HTML, …
doc, err := doctaculous.Open("input.pdf")

// Rasterize a page (RasterizePages renders many pages concurrently;
// a parsed document is read-only and goroutine-safe).
img, err := doc.RasterizePage(ctx, 0, doctaculous.RasterOptions{DPI: 150})

// Or convert in one call — any input format to any output format.
err = doctaculous.ConvertFile(ctx, "report.docx", "report.pdf", doctaculous.ConvertOptions{})

// Streams work too, with explicit formats when there's no filename to sniff.
err = doctaculous.Convert(ctx, in, out, doctaculous.ConvertOptions{
    From: doctaculous.FormatPDF,
    To:   doctaculous.FormatMarkdown,
})
```

For hosts routing uploads: `OpenReader(ctx, r)` / `OpenReaderAs` accept plain
`io.Reader`s, and `FormatFromMIME` maps content types onto the capability table
(`Format.ValidInput()` is the gate). Full API reference:
[pkg.go.dev/github.com/nathanstitt/doctaculous/pkg/doctaculous](https://pkg.go.dev/github.com/nathanstitt/doctaculous/pkg/doctaculous).

## How it works

There are three routes through the code, and everything meets at a single
format-neutral CSS box tree and a single backend-agnostic paint interface.

```text
 DOCX · HTML · Markdown · text          ┌────────────────────────┐      render.Device
 CSV/TSV · XLSX · RTF · PPTX     ─────▶ │  one CSS layout engine │ ───▶  ├─ raster    → PNG · JPEG
 EPUB · PNG/JPEG/HEIC · http(s) URLs    │  (pkg/layout/css)      │       └─ pdfwrite  → PDF
   frontends lower to a shared          │  blocks · inlines ·    │          (selectable text)
   box tree (pkg/layout/cssbox)         │  floats · tables ·     │
        │                               │  flex · grid ·         │
        │                               │  paged media           │
        │                               └────────────────────────┘
        └───▶ structure writers walk the box tree, not pixels:
              Markdown · text · HTML · DOCX · RTF · PPTX · EPUB · CSV/TSV · XLSX

 PDF ──▶ parse (pkg/pdf) ──▶ filters ──▶ interpret (pkg/pdf/content) ──▶ render.Device
              └───▶ extraction (pkg/pdf/extract): positioned glyphs + ruling lines
                    → reading order (XY-cut) + table recognition → the same box tree
```

- **The PDF pipeline** parses and interprets real-world PDFs itself: classic and
  stream xrefs, object streams, broken-file repair, RC4/AES-128/AES-256
  encryption, the full filter set (Flate, LZW, CCITT G3/G4, DCT, JBIG2, …),
  embedded TrueType/CFF/Type1/CID fonts, blend modes, shadings, clipping,
  inline images.
- **The reflow engine** is a from-scratch CSS 2.1+ layout engine: cascade,
  floats, absolute positioning, Appendix E stacking, both table border models,
  multi-line flexbox, grid, web fonts (WOFF1/WOFF2), CSS counters, bidirectional
  text (UAX#9) with Arabic shaping, and CSS Paged Media (`@page`, margin boxes,
  named pages, running headers/footers). DOCX lowers into this same engine rather
  than getting a bespoke renderer.
- **The structure writers** convert by walking the box tree, so a PDF's
  recovered headings and tables come out as real Markdown headings and pipe
  tables, and `pdf → xlsx` extracts spreadsheet-ready tables.

Conversions are pinned by round-trip parity tests (e.g. `html → docx → md` must
equal `html → md`), golden images, and WPT-style reftests.

## Beyond conversion: native office models

Two packages are supported public surfaces of their own, built for programs that
edit files rather than convert them:

- **`pkg/xlsx`** is a preservation-first spreadsheet editor. `Edit` + `Save` of an
  untouched workbook is **part-for-part byte-identical**; edits rewrite only the
  dirty XML parts, keeping unknown elements, attributes, and prefixes intact.
  Typed cell writes, style patches (patch-not-replace), conditional formats,
  comments, pivot tables, defined names, frozen panes, merges.
- **`pkg/docx`** is a full document model with a deterministic writer.
  `Parse ∘ Write` is a fixed point over both generated and real Word/LibreOffice
  corpora: tracked changes, comments, footnotes/endnotes, numbering, sections,
  drawings, and unmodeled parts all survive the round trip.

## Why?

The high-fidelity incumbents (PDFium, MuPDF, Poppler) require CGo and/or carry
copyleft licenses. doctaculous implements the whole stack in Go: PDF
interpretation, font parsing, CSS layout, rasterization, OOXML. It cross-compiles
freely, builds as a single static binary, and stays MIT. The few dependencies
are pure-Go and permissively licensed (see `go.mod`); the one vendored decoder
is Apache-2.0.

Built for [tinycld](https://github.com/tinycld), where it powers document
thumbnails, text extraction, and editing of xlsx/docx with the
[calc](https://github.com/tinycld/calc) and [text](https://github.com/tinycld/text)
packages respectively.

## Limitations

Unsupported constructs degrade gracefully. You get a skip and a debug log, or a
typed error (`ErrEncryptedNeedsPassword`, `ErrUnsupportedFormat`, …), never a
panic, and one bad page can't kill a batch. The notable gaps today:

- **Bidi/RTL is implemented but not complete.** `direction: rtl` mirrors tables,
  flex, and grid; text reorders per UAX#9 (with bracket mirroring); Arabic shapes
  with connected letterforms through the bundled Noto faces; and RTL PDFs extract
  in reading order. Remaining: OpenType mark positioning (Arabic diacritics sit on
  the baseline), nested bidi embeddings deeper than one level, and `dir="auto"`.
- **CJK text extraction from PDFs.** ToUnicode CMap parsing is pending, so
  Type0/CID text can extract as unknown runes (it still *renders* correctly).
- **No OCR.** Scanned PDFs rasterize fine but extract no text.
- Grid lacks named-line placement and subgrid; JPEG2000 images and PDF tiling
  patterns are skipped; password-protected PDFs open only with an empty user
  password.
- **Pagination splits blocks, tables, and lines, but not flex or grid items.** A
  flex or grid item taller than a page moves whole rather than fragmenting, because
  those size their items collectively and would need re-solving per page.
- **HEIC is stills-only and read-only.** HEIF image *sequences* (`msf1`) and
  AVIF are refused with a typed error, and nothing writes HEIC. Decoding is
  intra-only 4:2:0 at 8/10-bit (Main/Main 10/Main Still); P/B slices, range
  extensions, and >10-bit are rejected rather than approximated.
- **WebP is stills-only, and writes lossless.** Animated WebP is refused with a
  typed `ErrAnimated`. Output is VP8L, since no pure-Go lossy VP8 encoder
  exists — expect PNG-sized files, not JPEG-sized ones.

The complete feature inventory lives in [FEATURES.md](FEATURES.md).

## Layout of the codebase

| Area | Packages | Responsibility |
|------|----------|----------------|
| PDF | `pkg/pdf`, `pkg/pdf/filter`, `pkg/pdf/content`, `pkg/pdf/extract` | Parse, decode streams, interpret content, recover structure |
| Frontends | `pkg/html` + `pkg/css`, `pkg/docx`, `pkg/xlsx`, `pkg/pptx`, `pkg/epub`, `pkg/rtf`, `pkg/markdown` | Parse each format, lower to the shared box tree |
| Layout | `pkg/layout/cssbox`, `pkg/layout/css`, `pkg/layout/inline` | The box model, the CSS engine, shaping & line breaking |
| Fonts | `pkg/font`, `pkg/layout/font` | SFNT/WOFF/WOFF2 parsing, system + bundled resolution, per-rune script fallback |
| Backends | `pkg/render/raster`, `pkg/render/pdfwrite`, `pkg/render/{markdown,htmlwrite,docxwrite,rtfwrite,pptxwrite,epubwrite,csvwrite,xlsxwrite}` | Pixels, PDFs, and structure output |
| API / CLI | `pkg/doctaculous`, `cmd/doctaculous` | Public entry points, format detection, the conversion matrix |

The `render.Device` interface is the seam. Parsing, interpretation, and layout
never know which backend they're painting into, so new backends bolt on without
touching them.

## Testing

Fidelity work is only as good as what it's tested against, so the corpus carries
a lot of weight here:

- **Generated fixtures.** Test PDFs, DOCX, and XLSX are built deterministically
  in `testdata/gen` (readable Go, not opaque blobs). Materialize them with
  `go run ./cmd/dumpfixtures`.
- **Golden images.** Rendered pages are compared to committed PNGs with a
  per-pixel tolerance; every intentional change is regenerated and eyeballed.
- **Real-world corpus.** `testdata/external/` holds third-party PDFs, DOCX, and
  XLSX files that must parse, render, convert, and (for the editors) round-trip
  byte-identically.
- **Round-trip parity.** Structure writers are pinned so converting through an
  intermediate format equals converting directly.

```sh
make build   # build the CLI
make test    # go test ./... (race detector on)
make lint    # go vet + golangci-lint
```

## License

MIT — see [LICENSE](LICENSE).

Everything compiled into the module is MIT-compatible.

**Bundled fonts are embedded in the binary** (`go:embed`) and ship with anything
built from it. All are permissively licensed and may be redistributed:

- TeX Gyre Heros and TeX Gyre Termes — GUST Font License (an LPPL instance).
- Inconsolata, Noto Sans Hebrew, and Noto Naskh Arabic — SIL Open Font License
  1.1. None declares a Reserved Font Name, so they ship under their original
  names. See the `LICENSE-*.txt` files in
  [`pkg/font/standard/fonts/`](pkg/font/standard/fonts/).

Two further carve-outs, both isolated and NOT shipped with the library:

- [`pkg/pdf/filter/jbig2/`](pkg/pdf/filter/jbig2/) vendors
  [xiaoqidun/jbig2](https://github.com/xiaoqidun/jbig2) (pure-Go JBIG2 decoding,
  **Apache-2.0**, MIT-compatible) with its upstream `LICENSE` and `NOTICE`.
- [`testdata/external/`](testdata/external/) holds third-party **test inputs
  only** under their own licenses: PDFs (CC-BY-SA-4.0, from
  [py-pdf/sample-files](https://github.com/py-pdf/sample-files)) and DOCX/XLSX
  files (Apache-2.0 / MPL-2.0 / MIT, from Apache POI, LibreOffice, and
  Open-XML-SDK). Each directory's README carries per-file provenance.
