# Omnidoc

Pure-Go, MIT-licensed document toolkit. Long-term goal: convert any document to any other format,
author/sign PDF/DOCX/HTML, and rasterize pages to images. The core pipeline (parse → interpret →
raster) works end to end and renders real-world PDF, DOCX, HTML, EPUB, and SVG faithfully.

## Where things are written down

This file holds only how to WORK here. The technical reference lives in `docs/`:

| File | What it covers |
| --- | --- |
| [FEATURES.md](FEATURES.md) | The inventory of everything that has SHIPPED. Keep it current — every feature that lands gets a bullet in the same PR. It is NOT a CHANGELOG, keep it grounded in current state, not listing the history of each feature.|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Layers, the pipelines, and the `Device` seam. Read before adding a package or crossing a boundary. |
| [docs/DEPENDENCIES.md](docs/DEPENDENCIES.md) | Pure-Go / licensing constraints and the approved dependency list. Non-negotiable. |
| [docs/TESTING.md](docs/TESTING.md) | Corpora, golden-image rules, and the traps that have cost real time. |
| [docs/GO-PRACTICES.md](docs/GO-PRACTICES.md) | Error handling, API shape, concurrency conventions. |
| [docs/SCOPE.md](docs/SCOPE.md) | What is deliberately in/out of scope. |

Per-subsystem open work — what is NOT done, and why each item was deferred:

[docs/PDF.md](docs/PDF.md) · [docs/DOCX.md](docs/DOCX.md) · [docs/CSS-LAYOUT.md](docs/CSS-LAYOUT.md) ·
[docs/SVG.md](docs/SVG.md) · [docs/FIDELITY-BACKLOG.md](docs/FIDELITY-BACKLOG.md) (the detailed
per-item checklist behind the CSS/layout entries).

Those files hold **only outstanding work**. Shipped features belong in FEATURES.md — if you find a
"DONE" entry in a subsystem doc, move it.

## Working directives

- **Always implement the maximal, most browser-faithful option.** For any feature with a
  fidelity/scope choice (which CSS values, how complete the algorithm, edge cases), pick the fullest
  spec-compliant behavior — do NOT ask which subset to do. Lean to thoroughness; only surface a
  question for a genuine product decision that cannot be inferred, and even then default to maximal.
- **Every feature lands with tests AND a visual entry.** Add unit/golden tests for each part, and
  add the feature to the `testdata/htmldoc/` showcase (a new section + regenerated, eyeballed
  goldens) so it is visually exercised end to end.
- **Each engine feature/sub-project is its own branch → PR off `main`**, merged when CI is green.
  Keep changes additive and byte-identical for untouched callers (DOCX/PDF, pages not using the
  feature). Design rationale lives in the commit and PR history — read `git log` for the area you
  are extending; each sub-project's PR body carries the reasoning and the alternatives rejected.
- **Degrade honestly.** An unsupported case skips with a debug log and a test covering that
  behavior. If a degradation cannot log (no logger on that path), say so in FEATURES.md rather than
  implying it does — a claim that something "degrades with a log" when it is silent is worse than
  admitting the gap.
- **Verify claims before repeating them.** Measure rather than infer, and re-check a result that
  came from somewhere else before acting on it. Several bugs in this repo's history were found only
  because a passing test or a confident report was checked against the actual pixels.
