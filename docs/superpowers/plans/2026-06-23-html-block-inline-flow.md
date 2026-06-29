# Plan — Sub-project 3: Block + inline normal flow (first pixels ★)

**Design:** `docs/superpowers/specs/2026-06-23-html-block-inline-flow-design.md`
**Branch:** `feat/html-block-inline-flow` (off `feat/html-box-generation`)
**Status:** Implemented. 11 commits (`d3be07b`…`3fcb540`), all gates green, holistic review passed.

This plan was executed with subagent-driven development: per task → implement → spec review → code-quality
review → fix → commit. The DOCX golden images are the regression oracle for the inline-core extraction
and stayed byte-identical throughout.

---

## Tasks (in execution order)

### Task 1 — Inline-core extraction (`pkg/layout/inline`) — DO FIRST · commit `d3be07b`
Extract `shape.go` (neutral `Run`/`AtomicItem`/`Glyph`/`Color` + `Shape`), `break.go` (greedy `Break`),
`line.go` (`Line`/`Align`/`Placement`/`Place`/`MakeLine`/`VisibleWidth`/`CountSpaces`) from the flat
engine. Refactor `pkg/layout/flow.go` to delegate (`shapeBlock` → `inline.Run` adapter; `layoutBlock`
→ `inline.Break`; `emitLine` → `inline.Place`; `toInlineAlign`). Delete `linebreak.go`.
**Gate:** `TestDOCXGolden` green **without** `-update`. Move the breaker tests to the new package; add
`Place`/`Shape`/metrics unit tests.

### Task 2 — `pkg/css` sizing properties · commit `01c2959`
Add `MinWidth`/`MaxWidth`/`MinHeight`/`MaxHeight Length` + `BoxSizing string` to `ComputedStyle` (not
inherited; `MaxWidth`/`MaxHeight` model `none`→`UnitAuto`; `BoxSizing` default `content-box`). New
`setMaxLength` helper. A discriminating not-inherited test.

### Task 4 — Paint extension · commit `e4664c7`
Extend `pkg/layout/page.go` with `BackgroundKind`/`BorderKind` Item kinds, `BorderItem`, and the
`layout.BorderStyle`/`layout.EdgeSide` enums. Extend `pkg/layout/paint` with `paintBorder` (solid /
double / dashed / dotted) reusing a `fillRect` helper; backgrounds reuse `paintRule`. Pixel tests
(incl. the double-gap and dashed-alternation assertions). DOCX goldens unchanged.

### Task 3 — Fragment tree · commit `8d0de8d`
`pkg/layout/css/fragment.go`: `Fragment`/`BorderEdge`/`LineFragment`/`GlyphFragment` (reusing
`layout.BorderStyle`/`EdgeSide`), `AppendItems` flatten (background → 4 border strips → glyphs →
children, paint order), `Page`. Flatten/paint-order unit tests.

### Task 5 — Block formatting context · commit `aeeeba1`
`pkg/layout/css/block.go`: `Engine`/`New`/`Layout`; `resolveLen` (em/%), `usedEdges`,
`resolveContentWidth` (auto/fixed/border-box + min/max clamp), height resolution; vertical **margin
collapsing** (siblings + parent/first-child + parent/last-child through zero border/padding, via a
subtree-shift model); `Formatting` dispatch with flex/grid/table→block fallback; `isAnonymous` guard;
page-boundary + per-child recover. Box-assertion fixtures for every feature.

### Task 5b — CSS shorthand expansion · commit `66b5a80`
`pkg/css/shorthand.go`: `margin`/`padding` (1–4 clockwise), `border-{width,style,color}` (1–4),
`border` + per-side (width‖style‖color any order), `background` (color). Whole-declaration-drop for box
lists, per-component-skip for the border triple; expansion into longhands preserves cascade order.

### Task 6 — Inline formatting context · commit `9aa3dcb` (+ fix `9aa3dcb`)
`pkg/layout/css/inline.go`: implement the `layoutInline` hook — run gathering, `effectiveTextAlign`
(anon-block reads inherited align), `effectiveLineHeight`, shape→break→place, atomic inline-block /
replaced threaded into `Fragment.Children`. Maps generic CSS family keywords in `pkg/font/standard`
(required for any text to render). **Review found and fixed** an anon-block zero-line-height bug (anon
boxes carry zero-value Style → forced auto via `isAnonymous`).

### Task 6b — inline-block flows inline · commit `bc27f29`
`pkg/layout/css/anon.go`: add `isBlockLevelOuter` and use it at the partitioning call sites that
classify a child for the parent's FC (so inline-block is inline-level outer and reaches the IFC),
leaving interior-role checks `Kind`-based. End-to-end test through the public path.

### Task 7 — Public API · commit `c52612f`
`pkg/doctaculous/html_backend.go`: `OpenHTML`/`OpenHTMLBytes` + `WithViewportWidth`(1280)/
`WithResourceLoader`/`WithLogf`; pipeline `html.Parse`→`layoutcss.Build`→`Engine.Layout`→reuse
`reflowRenderer`. Tests assert pixels are actually painted.

### Task 8 — Test corpus · commit `303b832`
`TestHTMLGolden` + 4 committed `html-*.png` goldens (eyeballed); `TestWPTReftests` + 6 WPT-style
reference-comparison pairs under `testdata/wpt/css21-normal-flow/` (provenance noted). Reuses the
existing `compareImages`/`writePNG`/`readPNG`/`-update` helpers. DOCX goldens confirmed green.

### Task 9 — Holistic review + docs + polish · commit `3fcb540`
Full-branch gate sweep (suite + `-race` + vet + lint + gofmt, all green). Holistic review: no
Critical/Important issues. Folded in the M1 fidelity fix (anon block honors **inherited** explicit
line-height) + M3 documentation note (tall mixed-line atom placement). This spec + plan written; CLAUDE.md
updated.

---

## Verification (end-to-end)
- **Extraction faithful:** `go test ./pkg/doctaculous -run TestDOCXGolden` green without `-update`.
- **Engine + corpus:** `go test ./pkg/layout/inline/... ./pkg/layout/css/... ./pkg/css/...
  ./pkg/doctaculous -run 'TestHTMLGolden|TestWPTReftests'` green; golden PNGs eyeballed.
- **Whole suite + race:** `go test -race ./...` green across all 21 packages.
- **Lint/format:** `gofmt -l`, `go vet ./...`, `golangci-lint run` clean on the touched packages.
- All `go`/lint commands run with the command sandbox disabled (the Go build cache and module proxy
  are blocked under the sandbox).

## Notes carried forward to later sub-projects
- Real replaced-element intrinsic sizing + image decode (sub-project 4): atoms currently size from
  attrs/style or are zero placeholders.
- Inline-block horizontal margins, full `vertical-align`/line-box ascent including atoms (the tall
  mixed-line case is documented), `margin:auto` centering — fidelity follow-ups.
- Floats/positioning (5); table/flex/grid layout (6/8/9 — block-fallback today); web fonts (7);
  DOCX→cssbox convergence (10); pagination/`@page` (11).
