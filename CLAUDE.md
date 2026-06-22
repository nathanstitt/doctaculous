# Doctaculous

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/EPUB/HTML, and rasterize pages to images. **Current focus: rasterize a PDF
page to an image.**

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

The `Device` interface is the seam: the interpreter must stay backend-agnostic so we can add an
SVG/other backend later without touching parsing or interpretation.

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
  fixtures that produce them are generated, so the whole chain stays hermetic. Text fixtures
  currently render blank (no font backend yet) — that is the expected baseline and will change
  when glyph rendering lands, at which point those goldens get regenerated.
- **Benchmarks**: `BenchmarkRasterizePages` proves goroutine speedup vs. `--workers 1`. Add a
  race-detector run (`go test -race ./...`) since concurrency is core.
- Tests must be hermetic and fast: no network. Generated fixtures are preferred; committed PDFs
  are allowed where generation is impractical (see above) — keep them small and provenance-noted.
- New feature ⇒ new fixture + test in the same PR. Unsupported PDF features must degrade
  gracefully (skip + debug log), and that behavior must be covered by a test.

## Out of scope for v1 (don't gold-plate)

Encryption, shadings/gradients, blend modes, transparency groups, exact base-14 fonts, full color
management. Stub with clear typed errors and move on; these are tracked follow-ups.
