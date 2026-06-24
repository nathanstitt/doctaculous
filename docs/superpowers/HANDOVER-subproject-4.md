# Handover — Sub-project 4: Replaced content + images

**Status:** Not started. Sub-project 3 (block + inline normal flow — first pixels ★) is DONE and in
**PR #4**.
**Next action:** Resume the same flow used for sub-projects 1–3 — `superpowers:brainstorming` for
sub-project 4, then `writing-plans`, then `subagent-driven-development` (implement → spec-review →
code-quality-review → fix per task, then a holistic final review, then finish the branch / open a PR).

---

## Where we are (the PR stack)

- **Sub-project 1 (CSS parse + cascade)** — `pkg/css`. **PR #2** (`feat/css-parse-cascade` → `main`).
- **Sub-project 2 (HTML frontend + box generation)** — **PR #3** (`feat/html-box-generation` →
  `feat/css-parse-cascade`).
- **Sub-project 3 (block + inline normal flow)** — DONE, **PR #4** (`feat/html-block-inline-flow` →
  `feat/html-box-generation`). The PR is intentionally based on its predecessor so its diff is only
  sub-project 3; **retarget PR #4 to `main` once #2/#3 merge** (each PR retargets up the chain as the
  one below it merges).
- **Stacking note:** when you start sub-project 4, branch off `feat/html-block-inline-flow` (or off
  `main` after #2–#4 merge). Decide the base when you get there; if the stack has merged to `main`,
  just branch from `main`.

## What sub-project 3 delivered (the foundation #4 builds on)

Design: `docs/superpowers/specs/2026-06-23-html-block-inline-flow-design.md`.
Overarching roadmap: `docs/superpowers/specs/2026-06-23-html-rendering-design.md` (this is §5 row 4).

Sub-project 3 made **HTML render to pixels end-to-end**. `OpenHTML(path)` / `OpenHTMLBytes(data,
opts...)` now produce a real `*Document` the toolkit rasterizes (single tall image at a fixed viewport,
default 1280px). New/changed since sub-project 2:

- **`pkg/layout/inline`** — the shared inline-layout core (`Shape` styled runs → shaped glyphs;
  `Break` greedy line-breaker; `Place` alignment/justification math; `Line`/`Run`/`Glyph`/`AtomicItem`).
  Both the flat DOCX engine and the new CSS inline FC call it. **DOCX goldens are unchanged** (the
  extraction is behavior-preserving — keep them that way).
- **`pkg/layout/css/block.go`** — the CSS `Engine` (`New`, `Layout(ctx, root, viewportW)`) and the
  block formatting context: width/`auto`/`%`, `box-sizing`, `min/max-width`, padding, borders,
  backgrounds, em→pt and %→pt resolution (`resolveLen`), vertical margin collapsing, auto/fixed height,
  `Formatting` dispatch with flex/grid/table→block fallback, page-boundary recover, the `isAnonymous`
  guard for zero-value anon-box styles.
- **`pkg/layout/css/inline.go`** — the inline FC: run gathering, `effectiveTextAlign`,
  `effectiveLineHeight` (with inherited line-height on anon blocks), shape→break→place, and the
  **atomic path for inline-block and replaced boxes** (`gatherInlineRuns` → `inline.AtomicItem` →
  `translateFragment` into `Fragment.Children`). **This is the seam #4 extends** (see below).
- **`pkg/layout/css/fragment.go`** — the positioned `Fragment` tree (read-only) + `AppendItems`
  flatten to `layout.Item` + `Page`.
- **`pkg/layout/paint` + `pkg/layout/page.go`** — backgrounds + 4-side styled borders
  (solid/dashed/dotted/double); `BackgroundKind`/`BorderKind` Item kinds; `BorderItem`;
  `layout.BorderStyle`/`EdgeSide`.
- **`pkg/css`** — additive: `min/max-width`/`-height` + `box-sizing` on `ComputedStyle`; CSS shorthand
  expansion (`margin`/`padding`/`border`/`background` → longhands, `pkg/css/shorthand.go`).
- **`pkg/layout/css/anon.go`** — `isBlockLevelOuter` so inline-block participates inline (reaches the
  IFC atomic path) while keeping its block-container interior.
- **`pkg/font/standard`** — generic CSS family keywords (`serif`/`sans-serif`/`monospace`/…) map to
  bundled faces.
- **`pkg/doctaculous/html_backend.go`** — `OpenHTML`/`OpenHTMLBytes` + `WithViewportWidth`(1280)/
  `WithResourceLoader`/`WithLogf`, reusing `reflowRenderer`.
- **Tests** — box/fragment-position assertions; `html-*` golden PNGs; WPT-style normal-flow reftests
  (`pkg/doctaculous/testdata/wpt/css21-normal-flow/`). DOCX goldens preserved.

## What sub-project 4 must build (roadmap §5 row 4)

From the overarching design, sub-project 4 is **Replaced content + images**:

> `<img>`, replaced-element sizing, basic `object-fit`, PNG/JPEG/GIF decode → `dev.DrawImage`.
> Intrinsic sizing feeds layout.

Concretely, the work is:

1. **Image decode** — decode the bytes of an `<img src=…>` (and later CSS `background-image`) into an
   `image.Image`. PNG and JPEG are in the Go standard library (`image/png`, `image/jpeg`); GIF too
   (`image/gif`). **This is pure-Go and needs NO new dependency** — just blank-import the decoders.
   (Confirm: the PDF side already uses `image/jpeg` for DCTDecode, so the decoders are precedented.)
2. **Resource loading for images** — `<img src>` is a ref the existing `pkg/resource.ResourceLoader`
   resolves to bytes + content type (the seam already exists; `DirLoader`/`MapLoader` work; HTTP is
   sub-project 7). Box generation already records `<img>` as a `cssbox.BoxReplaced` carrying
   `Replaced{Tag, Attrs}` (with `src`/`width`/`height`/`alt`) — **decode happens at layout time** (the
   engine has the loader and ctx), not at box-gen time. Decide where the decoded image is cached on the
   box/fragment.
3. **Intrinsic sizing + the CSS replaced-element sizing algorithm** — today the IFC sizes a replaced
   atom only from its `width`/`height` (attrs or style) or a zero placeholder
   (`pkg/layout/css/inline.go` `replacedSize`/`gatherInlineRuns`; `ReplacedContent` has **no decoded
   image / no intrinsic size** — handover-note-3 from sub-project 3). Implement the CSS algorithm:
   intrinsic width/height come from the decoded image; if only one of width/height is specified, the
   other is derived from the intrinsic aspect ratio; if neither, use the intrinsic size; clamp by
   `min/max-width`/`-height`. This **feeds layout** — a sized `<img>` participates as an atom of its
   used size (inline) or as a block/inline-block.
4. **Paint the image** — the fragment tree needs an image item: extend `pkg/layout/css/fragment.go`
   (a `GlyphFragment`-like image fragment, or an image field on `Fragment`) and the flatten
   (`AppendItems`) to emit a new `layout.Item` image kind, and `pkg/layout/paint` to call
   `dev.DrawImage(img, ctm, alpha, blendMode)` (the `render.Device` already has `DrawImage` — the PDF
   side uses it). Map the image's unit square into the fragment's content box. **Basic `object-fit`**
   (`fill`/`contain`/`cover`/`none`/`scale-down`) controls how the image maps into the box; start with
   `fill` (stretch) as the default and add the others.
5. **Degradation (overarching §6):** an undecodable / 404 / missing-`src` image draws nothing (or an
   alt-text/placeholder box) + logs — never aborts the page. A format we can't decode (e.g. SVG, WebP,
   or — until sub-project 7 — anything needing HTTP) degrades the same way. Recover at the page
   boundary; never panic on a malformed image.
6. **CSS for replaced elements** — `object-fit` (and maybe `object-position`) are new `ComputedStyle`
   properties; add them additively in `pkg/css` (mirror how sub-project 3 added `box-sizing`). The
   `width`/`height` HTML *attributes* on `<img>` are already captured in `Replaced.Attrs`; CSS
   `width`/`height` already resolve in the engine — reconcile the precedence (CSS wins over the
   presentational attribute).
7. **Tests (project non-negotiable):** new fixtures + tests in the same PR. Generate small images
   deterministically in-test (a few-pixel PNG/JPEG/GIF encoded with the stdlib, served via a
   `MapLoader`) so the corpus stays hermetic — do NOT commit large binary images. Cover: an `<img>`
   with explicit width/height; intrinsic sizing (no width/height → image's own size); aspect-ratio
   derivation (one dimension given); `object-fit` variants; a 404/undecodable image degrades (no panic,
   page still renders); an `<img>` as a block vs inline vs inline-block. Add a committed `html-*` golden
   PNG (eyeballed) showing a decoded image rendered, and consider a WPT-style reftest (e.g. an `<img>`
   sized to a box == a `<div>` of the same size/background). Keep `TestDOCXGolden` green.

## The exact seams #4 extends (read these)

- **`pkg/layout/cssbox/box.go`** — `ReplacedContent{Tag string; Attrs map[string]string}`. #4 likely
  adds a decoded-image field here (or carries it on the fragment). `BoxReplaced` is a leaf; box-gen
  already produces it (`pkg/layout/css/build.go` `replacedTags`/`attrSnapshot`).
- **`pkg/layout/css/inline.go`** — `replacedSize` (currently style/attr-or-zero) and the atomic-run
  construction in `gatherInlineRuns`. This is where intrinsic size + the replaced-sizing algorithm
  plug in. Note: a replaced box can also be **block-level** (`<img style="display:block">`) — handle it
  in `block.go` too, not only as an inline atom. Today a block-level replaced box falls through the
  block path; make sure it gets sized + painted.
- **`pkg/layout/css/fragment.go`** + **`pkg/layout/paint/paint.go`** + **`pkg/layout/page.go`** — add
  the image fragment/item + the `DrawImage` paint path (analogous to how Task 4 added borders).
- **`pkg/render/device.go`** — `DrawImage(img image.Image, ctm Matrix, alpha float64, blendMode string)`
  already exists; the raster backend implements it (`pkg/render/raster/image.go`). The reflow paint
  matrix is page→device (`render.Scale(scale,scale)`, no Y-flip) — compose the image's unit-square→
  content-box mapping with it.
- **`pkg/resource`** — `Load(ctx, ref) ([]byte, string, error)`; `ErrNotFound`. Use the content type
  (or sniff/extension) to pick the decoder.

## Carried-forward notes from sub-project 3's reviews (things #4 should know)

1. **Replaced atoms are bottom-aligned on the baseline** and their **horizontal margins are ignored**
   in the IFC today (sub-project-3 deferrals). #4 should at least size/paint images correctly; full
   inline `vertical-align` and inline-block/replaced margins remain fidelity follow-ups (can be
   deferred again or picked up here — your call in the spec).
2. **The replaced-sizing math must clamp by `min/max-width`/`-height`** (those exist on `ComputedStyle`
   as of sub-project 3) — reuse the BFC's clamp logic where possible (`resolveContentWidth` in
   `block.go` already does max-then-min-then-≥0).
3. **Decode at layout time, not box-gen time.** Box generation stays pure/structural (no I/O, no
   image bytes); the engine has the `ResourceLoader` and `ctx`. Cache the decoded image so a repeated
   `src` isn't re-decoded (mirror the face cache pattern).
4. **`<img>` width/height attributes are unitless pixels** (e.g. `width="200"`) — `attrPx` in
   `inline.go` already parses them; CSS `width`/`height` (with units) take precedence over the
   presentational attribute per CSS.
5. **No new dependency.** PNG/JPEG/GIF are stdlib. If you want APNG/WebP/AVIF, that's a pure-Go
   permissive dep decision to record in the PR per CLAUDE.md — but the roadmap only asks for
   PNG/JPEG/GIF, so prefer stdlib-only.

## Dependencies note

No new dependency is required for #4 (PNG/JPEG/GIF decode is stdlib; `render.Device.DrawImage` and the
raster backend already exist). If a later format needs a pure-Go permissive dep, record the reason in
the PR (CLAUDE.md).

## Process reminder (what worked for #1–#3)

Brainstorm → spec (`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) → subagent-driven
execution (per task: implement → spec-review → code-quality-review → fix) → holistic final review →
finish branch (PR). Notes that held across #1–#3:

- **The command sandbox blocks the Go build cache** (`operation not permitted` on
  `~/Library/Caches/go-build`) and TLS to the proxy — run `go`/`golangci-lint`/`gofmt` with the
  sandbox disabled. The **editor diagnostics panel lags** (shows stale "undefined"/"unused" errors and
  phantom scratch files after subagents write/delete files) — trust `go build`/`go test`, not the panel.
- **`golangci-lint` here does NOT enable a gofmt linter** — run `gofmt -l <pkgs>` separately. Lint
  specific packages (not the repo root). The editor's "modernize" hints (`max()`/`min()`/`slices.*`/
  range-over-int) are NOT enabled by the project lint and the codebase intentionally uses explicit
  clamps — decline them.
- **The two-stage review earns its keep.** In #3 it caught an anonymous-block zero-line-height bug (a
  real rendering defect), and the holistic pass surfaced the inherited-line-height fidelity fix. Have
  spec reviewers verify the load-bearing math (here: the replaced-sizing aspect-ratio algorithm and the
  image→content-box mapping) with throwaway adversarial checks.
- **Eyeball every changed/new golden PNG in the PR** (the controller, not just the implementer). For
  #4, generated test images keep the corpus hermetic — the golden then proves the decode+sizing+paint
  chain visually.
- **Propagate review fixes back into the spec/plan** so they stay authoritative, and update CLAUDE.md's
  "Done" roadmap when the PR lands (move the image work out of the §6 TODO list).
