# doctaculous

A pure-Go, MIT-licensed document toolkit. No CGo, no native bindings — everything is Go.

The long-term goal is an "everything" document tool: convert between formats, author and sign
PDF/DOCX/HTML, and rasterize pages to images.

Built for [tinycld](https://github.com/tinycld), where it powers document thumbnails and other
format conversions.

## Status

The PDF pipeline (parse → interpret → rasterize) and the reflow engine (HTML and DOCX through a
shared CSS layout engine) work end-to-end and render real-world documents faithfully. Any
supported input converts to any supported output — inputs: PDF, DOCX, XLSX, HTML, Markdown,
plain text, CSV/TSV, http(s) URLs; outputs: PDF, DOCX, XLSX, HTML, Markdown, plain text,
CSV/TSV (spreadsheet outputs carry the document's tables — including tables recovered from
PDFs), PNG, JPEG:

```sh
doctaculous convert report.docx report.pdf
doctaculous convert https://example.com page.png
doctaculous convert input.pdf notes.md            # structure recovered by extraction
doctaculous rasterize input.pdf --page 1 --out page1.png --dpi 150
```

`convert` detects the input format from content and extension (`--from` overrides) and takes the
output format from the output extension (`--to` overrides). Converting a document to its own
format is not supported. The focused subcommands (`rasterize`, `topdf`, `tomd`, `tohtml`) remain.

See the "Status & roadmap" section of [CLAUDE.md](CLAUDE.md#status--roadmap) for the full,
current feature list (filters, fonts, encryption, shadings, CSS coverage, …) and what's next.
Unsupported features degrade gracefully (skipped with a debug log, or a typed error) rather than
failing the render.

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

### Third-party / vendored code

JBIG2 image decoding is provided by
[xiaoqidun/jbig2](https://github.com/xiaoqidun/jbig2) — a pure-Go JBIG2 (ITU T.88)
decoder under the **Apache License 2.0** — vendored into
[`pkg/pdf/filter/jbig2/`](pkg/pdf/filter/jbig2/) (a copy in-tree, not a module
dependency). Apache-2.0 is MIT-compatible; the vendored directory retains the
upstream `LICENSE` and `NOTICE`, and its `README.md` records the exact source and
version. See that directory for full attribution.
