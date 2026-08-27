# Testing

This project lives or dies on its test corpus.

- **Every layer has unit tests**: parser (objects, xref tables AND streams, object streams), filters
  (round-trip + predictors), interpreter (per-operator behavior), rasterizer (shapes), and the CSS
  engine (box-gen, per-algorithm unit suites, fragment-geometry).
- **Prefer generating test PDFs deterministically** with the hermetic Go generator (`testdata/gen`),
  one fixture per feature so failures localize. **Committing real PDFs is fine** when a fixture is
  impractical to generate (complex real-world files, specific producers, fidelity/integration cases)
  — keep them small and note provenance/license in the PR. `cmd/dumpfixtures` materializes generated
  fixtures for inspection.
- **Core corpus (`gen.Core` in `testdata/gen/core.go`)**: ~10 fixtures (`text`, `vector`, `flate`,
  `multipage`, `rotated`, `image-flate`, `image-jpeg`, `xref-stream`, `objstm`, `bad-xref`) each
  locking one must-always-work path from parse through raster. Range over it where a uniform sweep
  fits (parser round-trip, golden rendering, the parallel-render benchmark). When you add a fixture
  for a new core path, add it to `gen.Core`.
- **SVG corpus (`testdata/svg/resvg`, 848 fixtures)**: curated from resvg-test-suite (MIT, pinned
  commit), one feature per file, with our own committed goldens under
  `pkg/doctaculous/testdata/golden/svg-resvg/`. Provenance and every curation/exclusion decision live
  in that directory's README — read it before adding or excluding a fixture.
  - **The `.png` beside each fixture is NOT resvg's output.** The suite's README says `results.csv`
    holds "results of manual testing via `tools/vdiff`", and `vdiff` renders each SVG *live* across
    Chrome, QtSvg, Inkscape, librsvg and Batik for a human to compare — so those PNGs are curated
    expected-result images. Verified: `shapes/circle/simple-case.png`, a plain circle, differs from a
    fresh resvg render by 0.66% along the antialiased edge alone. **Do not try to "refresh" them from
    a resvg build** — that is not a meaningful operation, and one attempt reported 45% of the corpus
    as stale before the premise was caught.
  - They are still the right thing to eyeball against: compare *intent and geometry*, not pixels,
    since our bundled fonts and rasterizer differ. Vendor a fixture only when its geometric claim
    stays verifiable under font substitution; otherwise skip it with a recorded reason rather than
    committing a golden that locks in a substituted rendering as correct.
  - When a fidelity question turns on what resvg *actually does*, build it and look
    (`~/code/vendor/resvg`, `cargo` works out of the box). Reading `usvg`'s resolved tree settled in
    minutes a cyclic-mask question that days of pixel archaeology could not — and revealed that one
    committed reference was simply out of step with current resvg.
- **Golden-image tests** (`pkg/render/raster/golden_test.go`, plus the `pkg/doctaculous` `docx-*` /
  `html-*` / `htmldoc-*` goldens): render at 72 DPI, compare to committed PNGs with a per-pixel
  tolerance (±4/channel) + 0.2% differing-pixel budget. Regenerate an intentional render change with
  `go test ./pkg/render/raster -run TestGolden -update`, then **eyeball every changed PNG in the PR**
  — an unexplained golden diff is a regression. Goldens are committed; the fixtures that produce them
  are generated, so the chain stays hermetic. HTML/DOCX also carry WPT-style reftests.
  - **`-update` rewriting a golden does NOT mean it was stale.** A handful of goldens
    (`docx-model-specimen`, `md-specimen`, and some masking ones) get new BYTES on every
    `-update` while remaining pixel-identical or well inside tolerance — PNG encoding is not
    byte-stable across runs even when the rendered pixels are. Rendering itself IS
    deterministic (verified: repeated renders hash identically). Judge staleness by the
    tolerance check, never by `git status`.
  - **Compare PNGs by decoded pixels, not raw bytes.** These files use per-row PNG filters
    (Paeth/Average/Sub/Up), so a byte-level diff reads filter RESIDUALS, where one changed
    pixel perturbs every byte after it — a 1/255 difference can look like 7% of the file.
    Use the harness's own `compareImages`, or decode properly, before concluding anything.
- **Benchmarks**: `BenchmarkRasterizePages` proves goroutine speedup vs. `--workers 1`. Run the
  race detector (`go test -race ./...`) since concurrency is core.
- Tests must be hermetic and fast: no network (HTTP paths use `net/http/httptest` loopback).
- New feature ⇒ new fixture + test + showcase entry in the same PR. Unsupported features must
  degrade gracefully (skip + debug log / typed error), and that behavior must be covered by a test.

