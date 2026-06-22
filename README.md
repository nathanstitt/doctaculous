# doctaculous

A pure-Go, MIT-licensed document toolkit. No CGo, no native bindings — everything is Go.

The long-term goal is an "everything" document tool: convert between formats, author and sign
PDF/DOCX/EPUB/HTML, and rasterize pages to images. The current focus is **rasterizing PDF pages
to images**.

Built for [tinycld](https://github.com/tinycld), where it powers document thumbnails and other
format conversions.

## Status

The core pipeline — parse → interpret → rasterize — works end-to-end and renders real-world PDFs
faithfully (multi-column text, tables, images, rotated/cropped pages).

```sh
doctaculous rasterize input.pdf --page 1 --out page1.png --dpi 150
```

**Working:** xref tables / xref streams / object streams, Flate/LZW/ASCII/RunLength filters, vector
fills (nonzero + even-odd), clipping, form XObjects, page rotation, ExtGState constant alpha
(`/ca`/`/CA`), embedded fonts (TrueType, CFF, classic Type 1, Type0/CID, symbolic subsets), images
(Gray/RGB/CMYK/Indexed/ICCBased at 1–16 bpc, JPEG, and `/SMask` soft masks), and a concurrent
multi-page render path.

**Not yet** (see the roadmap in [CLAUDE.md](CLAUDE.md#status--roadmap) for the prioritized list):
ImageMask stencils, CCITT/JBIG2/JPX image filters, inline images, non-embedded base-14 fonts, full
stroke joins/caps, shadings/gradients, blend modes, and encryption. Unsupported features degrade
gracefully (skipped with a debug log, or a typed error) rather than failing the render.

## Why pure Go?

Existing high-fidelity renderers (PDFium, MuPDF, Poppler) require CGo and/or carry copyleft
licenses. doctaculous renders PDF content streams itself in pure Go, so it builds as a single
static binary, cross-compiles freely, and stays MIT-licensed.

## Architecture

A layered pipeline; each layer is independently testable:

| Layer | Package | Responsibility |
|-------|---------|----------------|
| Parse | `pkg/pdf` | Tokenizer, objects, xref, page tree |
| Filter | `pkg/pdf/filter` | Decode stream bytes (Flate, ASCII85, …) |
| Interpret | `pkg/pdf/content` | Content-stream tokenizer + graphics state |
| Device | `pkg/render` | Backend-agnostic paint ops (`Device` interface) |
| Raster | `pkg/render/raster` | Bitmap backend → `image.Image` |
| API | `pkg/doctaculous` | Public library entry point |
| CLI | `cmd/doctaculous` | Thin command-line wrapper |

The `Device` interface is the seam that will let us add other backends (e.g. SVG) later.

## Test fixtures

Test PDFs aren't committed — they're generated deterministically in `testdata/gen`, so each
fixture is readable Go rather than an opaque blob. To materialize them as real `.pdf` files for
inspection:

```sh
go run ./cmd/dumpfixtures -list           # show available fixtures
go run ./cmd/dumpfixtures                 # write the core set to ./fixtures-out
go run ./cmd/dumpfixtures -all -o /tmp    # include malformed fixtures too
go run ./cmd/dumpfixtures text objstm     # write only named fixtures
```

## Development

```sh
make build   # build the CLI
make test    # go test ./... (with race detector)
make lint    # go vet + golangci-lint
```

## License

MIT — see [LICENSE](LICENSE).

The library and all its source code are MIT-licensed. One exception: the
real-world PDF samples under [`testdata/external/`](testdata/external/) are
third-party test inputs licensed **CC-BY-SA-4.0** (from
[py-pdf/sample-files](https://github.com/py-pdf/sample-files)), kept isolated
there with their own license text. They are test fixtures only — never compiled
into or shipped with the module — so they don't affect the MIT licensing of the
library. See that directory's README for details and attribution.
