# HTML Rendering — Overarching Design

**Status:** Approved design / program roadmap
**Date:** 2026-06-23
**Scope:** This is the *overarching* design for HTML rendering in Doctaculous. HTML rendering is
not a single project — it is a program of sub-projects, each of which gets its own
spec → plan → implementation cycle. This document fixes the architecture, the package layout, the
decomposition into sub-projects, the ordering, and the cross-cutting contracts (degradation, error
model, resource loading, concurrency, testing). Each sub-project's detailed design is written when
that sub-project is reached.

---

## 1. Goal and scope

Render **modern web pages** to images with high fidelity, as a pure-Go, MIT-licensed feature of the
existing toolkit. "Modern" means the full layout stack: CSS2.1 normal flow, floats/positioning,
tables, **flexbox and grid**, web fonts — effectively a browser layout engine minus JavaScript,
navigation, and user input.

**In scope**
- Parsing HTML (via `golang.org/x/net/html`) and CSS (hand-written, in-tree).
- The CSS cascade, computed values, and inheritance.
- A recursive CSS box model and a layout engine implementing it, built up in layers.
- Replaced content (images), web fonts (`@font-face`, WOFF/WOFF2).
- Rasterizing the laid-out page through the existing `render.Device` / `pkg/render/raster` stack.
- Built-in HTTP resource fetching (behind a seam), so callers can "point at a URL".

**Explicitly out of scope** (no JS engine, by the project's constraints)
- JavaScript execution, the DOM API, event handling, navigation, history, user input.
- We render HTML/CSS **as delivered** (e.g. a server-rendered/SSR snapshot). We never execute
  scripts, so dynamic post-load mutation is not represented.
- Form interactivity, scrolling behavior, animations/transitions as motion (their *static*
  computed state is rendered; they do not animate).

**Non-negotiables inherited from the project (CLAUDE.md):** pure Go, no CGo/WASM, MIT/BSD/Apache
deps only, concurrency-first, no panics on malformed input, every layer independently unit-tested,
new feature ⇒ new fixture + test in the same PR, unsupported constructs degrade gracefully.

---

## 2. Architecture and the convergence strategy

### 2.1 The end state: one engine

The toolkit has two pipelines that meet at the `render.Device` seam: the PDF pipeline and the
reflowable-document pipeline. Today the reflow pipeline uses a **flat** box model
(`pkg/layout/box.Document` — a linear list of paragraph blocks with inline spans) that is
deliberately simple and presentational. It is sufficient for DOCX but **cannot** express the CSS box
model (recursive boxes, the margin/border/padding/background box, floats, flex, grid).

The target architecture is **one recursive box model and one layout engine** shared by HTML and
DOCX, converging with the PDF pipeline at `render.Device`:

```
                         ┌─────────────────────────────────────┐
                         │   render.Device  (the shared seam)   │
                         └───────────────▲─────────────────────┘
            ┌────────────────────────────┼────────────────────────────┐
   ┌────────┴────────┐                   │                  ┌──────────┴──────────┐
   │   PDF pipeline  │                   │                  │  ONE CSS layout     │
   │  (unchanged)    │                   │                  │  engine             │
   └─────────────────┘                   │                  └──────────▲──────────┘
                                         │                  ┌──────────┴──────────┐
                                         │                  │ recursive cssbox    │
                                         │                  │ tree                │
                                         │                  └──────▲───────▲──────┘
                                         │                   DOCX ─┘       └─ HTML
                          shared: pkg/font, pkg/layout/font (face cache),
                                  pkg/render, pkg/render/raster, pkg/layout/inline
```

### 2.2 The path to it: converge later (deliberate, gated, golden-verified)

Migrating DOCX onto the new engine on day one would tear out a **working, shipping** code path and
bet its passing golden images on brand-new, unproven engine code. Instead we converge *late*:

```
   DURING THE PROGRAM                         AFTER CONVERGENCE (sub-project 10)
   ─────────────────                          ──────────────────────────────────
   DOCX → flat box.Document → flat engine ┐   DOCX ┐
                                          ├─▶dev    ├─▶ cssbox → CSS engine → dev
   HTML → cssbox → CSS engine ────────────┘   HTML ┘
        flat path kept GREEN as a control      flat engine + flat box DELETED;
                                               DOCX goldens are the regression oracle
```

- The DOCX flat path stays **untouched and green** while the CSS engine is built and hardened
  against HTML + WPT.
- A dedicated late sub-project (10) re-points **DOCX lowering** from `box.Document` to `cssbox`,
  then **deletes** the flat engine and flat box model. It is gated on the CSS engine matching the
  flat engine on normal flow + tables + lists. **The existing DOCX golden images are the exact
  regression oracle** that proves the migration is faithful.
- The PDF pipeline is never touched. `pkg/font`, `pkg/layout/font`, `pkg/render`,
  `pkg/render/raster` are shared by all paths throughout.

**CLAUDE.md update lands with sub-project 2:** the existing line — "a new reflow format is just a
parse+lower frontend producing `box.Document`" — is revised to reflect the two-tier reality during
the program and the single recursive `cssbox` engine at the end. This is recorded as a deliberate
architectural evolution, not a contradiction.

### 2.3 Why not the alternatives

- **Keep two engines forever:** permanently duplicates flow/pagination and forces tables/lists to be
  built twice. Rejected — the duplication was only ever risk-avoidance, and converging late avoids
  the same risk without it.
- **Migrate DOCX first:** front-loads risk onto a shipping feature for the benefit of a future one.
  Rejected in favor of late convergence with a golden safety net.

---

## 3. Package layout

New packages, each cohesive and independently testable (mirroring `pkg/pdf` / `pkg/pdf/filter` /
`pkg/pdf/content` separation):

```
pkg/css                 ── hand-written CSS (format-agnostic; no layout, no rendering)
   css.go        tokenizer (CSS Syntax spec) + parser → stylesheet AST
   selector.go   selector parsing, specificity, matching against the DOM
   value.go      typed property values (lengths, %, colors, keywords, shorthands)
   cascade.go    cascade + inheritance + computed values → ComputedStyle per node

pkg/html                ── HTML frontend (thin; x/net/html does the heavy lifting)
   html.go       wrap x/net/html → DOM; collect <style>, <link>, inline style=""
   dom.go        the DOM node view the cascade matches selectors against

pkg/layout/inline       ── shared inline-layout core (EXTRACTED from pkg/layout, sub-project 0)
   shape.go      styled runs → shaped glyphs (face resolution + measurement)
   break.go      shaped glyphs + available widths → lines (the salvaged greedy breaker)
   line.go       line metrics, justification, alignment math

pkg/layout/cssbox       ── the recursive neutral box tree (NEW shared model; the engine's input)
   box.go        Box: display type, ComputedStyle, children, content
                 (text runs, replaced images); anonymous-box vocabulary

pkg/layout/css          ── the CSS layout engine (consumes cssbox → positioned fragments)
   build.go      DOM + computed styles → box tree (box generation, anonymous boxes)
   block.go      block formatting context (normal flow, margin collapsing)
   inline.go     inline formatting context (line boxes; calls pkg/layout/inline)
   float.go      floats + clear (sub-project 5)
   position.go   relative/absolute/fixed, z-order, overflow (sub-project 5)
   table.go      table layout (sub-project 6)
   flex.go       flexbox (sub-project 8)
   grid.go       grid (sub-project 9)
   fragment.go   engine output: the positioned fragment tree (paint input)

pkg/resource            ── ResourceLoader seam + HTTP-backed default loader + test loader
pkg/font  (extended)    ── WOFF/WOFF2 unwrap + @font-face matching (sub-project 7)
```

`pkg/layout/paint` is **extended** (not forked): it already paints glyphs and rules; it grows
background, border (4 sides + styles), and image item kinds — all still fills/images on
`render.Device`. The public API additions live in `pkg/doctaculous` alongside `OpenDOCX`.

---

## 4. Stage pipeline

```
bytes ─▶ pkg/html ─────▶ DOM tree
                         │
pkg/css ◀── <style> / <link> / style="" ──┘   (external refs via pkg/resource)
   │
   ▼  parse stylesheets → Stylesheet AST
   ▼  cascade + inherit  (pkg/css/cascade.go)
DOM annotated with ComputedStyle per node
   │
   ▼  box generation     (pkg/layout/css/build.go)
cssbox tree  ◀───────────────── DOCX lowering re-points here at sub-project 10
   │
   ▼  layout (BFC / IFC / float / table / flex / grid)
positioned fragment tree   (read-only after layout → shared without locks)
   │
   ▼  fragmentation (OPTIONAL: paginate to sheets / CSS @page)
page(s) of fragments
   │
   ▼  paint  (extend pkg/layout/paint)
render.Device ─▶ pkg/render/raster ─▶ image(s)
```

Each arrow is a unit-tested boundary. The line-break core (`pkg/layout/inline`) is shared by the
IFC and, during the program, by the flat engine.

---

## 5. Sub-project roadmap

Each row is a self-contained spec → plan → build cycle and lands its own fixtures / WPT slice +
goldens **in the same PR** (project non-negotiable). Everything not yet built degrades gracefully
(see §6). "★" marks the first-pixels milestone; "◆" marks shared/convergence work; "‖" marks a
parallelizable track.

| #  | Sub-project | What lands | Gated on |
|----|-------------|-----------|----------|
| 0  | **Inline-core extraction** (pure refactor) | `pkg/layout/inline`; flat engine refactored to call it. **No new feature.** DOCX goldens + `pkg/layout` tests unchanged = proof. De-risks everything after and pre-neutralizes the inline path for convergence. | — |
| 1  | **CSS parse + cascade** (no rendering) | `pkg/css`: tokenizer, parser, selectors, specificity, cascade, inheritance, computed values. Unit-tested in isolation. Output: `ComputedStyle` per node. Pure machinery, no layout. | 0 |
| 2  | **HTML frontend + box generation** | `pkg/html` (wrap `x/net/html`), `pkg/layout/cssbox` tree, box generation incl. anonymous boxes. `pkg/resource` seam + hermetic test loader (no HTTP yet). | 1 |
| 3  | **Block + inline normal flow** ★ | `pkg/layout/css`: BFC + IFC. Block boxes, margins (+ collapsing), padding, borders, backgrounds, text via the inline core. `OpenHTML` returns a real rendered page. First WPT slice (CSS2.1 normal flow) + goldens. | 2 |
| 4  | **Replaced content + images** | `<img>`, replaced-element sizing, basic object-fit, PNG/JPEG/GIF decode → `dev.DrawImage`. Intrinsic sizing feeds layout. | 3 |
| 5  | **Floats + positioning** | float/clear + BFC interaction; relative/absolute/fixed positioning; z-order; overflow clipping. (The "classic CSS2 web" milestone.) | 3 |
| 6  | **Tables** ◆ | `display:table`, auto/fixed table layout, column-width solve, row/col spans, borders. Built in the CSS engine; DOCX tables inherit it at convergence. | 5 |
| 7  | **Web fonts** ‖ | `pkg/font`: WOFF unwrap (zlib) + WOFF2 (Brotli dep), `@font-face` matching (family/weight/style/unicode-range). Real page fonts replace substitutes. Needs only the inline/paint path (step 3) → parallel track. | 3 |
| 8  | **Flexbox** | `display:flex`: main/cross sizing, grow/shrink/basis, alignment, wrapping. Large self-contained algorithm on the proven box model. | 5, 6 |
| 9  | **Grid** | `display:grid`: track sizing (fr/min/max/auto), placement, spanning. Largest single algorithm; last, because it assumes everything beneath is solid. | 8 |
| 10 | **DOCX → cssbox convergence** ◆ | Re-point DOCX lowering at `cssbox`; **delete** the flat engine + flat box model. DOCX goldens are the regression oracle. Schedulable any time after the engine covers DOCX's needs (flow + tables); **does not block 8/9.** | 3, 6 |
| 11 | **Pagination + CSS paged media** (optional output modes) | Post-layout slicing into uniform sheets; `@page` / `break-*` / paged-media. Default output stays a single tall image. Layered on, not core. | 3 (+ per feature) |

**Capability milestones the user sees:** step 3 = document-style pages render; step 5 = classic
float-layout sites render; steps 8–9 = modern flex/grid pages render. Fidelity climbs monotonically;
there is never a "renders nothing" cliff (see §6).

**Ordering notes:** images (4) precede floats (5) because float layout interacts with
replaced-element sizing, so float fixtures are only meaningful once images are real. Web fonts (7)
and DOCX convergence (10) float relative to the main spine.

---

## 6. Degradation and error model

**Golden rule:** every layer produces *something renderable* even when it meets a construct it does
not yet support. A page using grid before sub-project 9 still renders its text. This is what makes
each milestone shippable rather than all-or-nothing.

Per-stage degradation contract:

| Stage | Unsupported construct | Degraded behavior |
|-------|----------------------|-------------------|
| CSS parse | unknown property/value | skip the declaration, keep the rule |
| CSS parse | unknown selector token | drop that selector, keep the stylesheet |
| Box generation | unknown `display` value | treat as nearest known mode (**flex/grid → block**) + debug log |
| Layout | unsupported layout mode | **fall back to block normal flow**, so children still lay out and paint |
| Replaced/images | undecodable image | draw nothing (or an alt box) + log; never abort the page |
| Fonts | no face / WOFF2 before sub-project 7 | bundled substitute + log (today's DOCX behavior) |
| Resource load | fetch failure / 404 | treat the resource as absent + log; render without it |

**Mechanism (identical to the PDF side):** a typed/sentinel error or a skip + `logf` at the
narrowest scope, plus a **recover at the page boundary** so one pathological subtree cannot kill the
render. No `panic` escapes to the caller on malformed input.

**Explicit consequence:** because unsupported display modes fall back to **block normal flow**, a
flex/grid page rendered before sub-projects 8–9 renders its content stacked top-to-bottom and
legible — not in rows/tracks, but complete. This is the *intended* degradation curve and is
documented so such output reads as expected, not as a bug.

---

## 7. Cross-cutting contracts

### 7.1 Resource loading (built-in HTTP, behind a seam)

```go
// ResourceLoader resolves a URL referenced by a document (stylesheet, image,
// font) to its bytes and content type. The library ships an HTTP-backed default
// for the public OpenURL path; tests inject a hermetic in-memory/testdata loader
// so no layer below the public API touches the network. Honors ctx cancellation.
type ResourceLoader interface {
    Load(ctx context.Context, url string) (data []byte, contentType string, err error)
}
```

Network access lives **only** in the HTTP loader. `pkg/css`, `pkg/layout/css`, and `pkg/font` see
only a `ResourceLoader`, keeping them pure and unit-testable and satisfying the project's "no
network in tests" rule. The HTTP loader handles its own concerns (timeouts, redirects, TLS, size
caps) and is not depended on by any layout/parse code.

### 7.2 Concurrency

The single tall page lays out **once** — CSS layout is inherently whole-document (floats, flow,
flex, grid cannot be safely parallelized per-region), mirroring how DOCX pagination already runs
once. Parallelism lives where it already does: the **rasterization fan-out** (and, in the
paginate / `@page` modes, per-sheet rendering). The laid-out fragment tree is **read-only after
layout**, so it is shared across the render fan-out without locks — the same invariant as
`*Document` and `*layout.Pages` today.

### 7.3 Public API (mirrors `OpenDOCX`, returns the same `*Document`)

```go
// Local file; relative refs resolve through a filesystem-backed loader.
func OpenHTML(path string) (*Document, error)

// In-memory HTML; options set viewport, loader, pagination/@page mode, DPI.
func OpenHTMLBytes(data []byte, opts ...HTMLOption) (*Document, error)

// Fetch and render a live URL; the HTTP ResourceLoader is the default.
func OpenURL(ctx context.Context, url string, opts ...HTMLOption) (*Document, error)
```

`HTMLOption` covers: viewport width (default a fixed desktop width, e.g. 1280px), `ResourceLoader`
override, output mode (single tall image **default** / paginate to sheets / CSS paged media), and
DPI passthrough. HTML returns the **same `*Document`** the rest of the toolkit rasterizes, so it
reuses the existing `RasterOptions` / `renderPage` machinery with **no new public rasterization
surface**.

### 7.4 Viewport and output model

A webpage is a continuous canvas at a viewport width, not intrinsic pages. Therefore:
- **Default:** lay out at a fixed viewport width; flow to whatever height the content needs;
  rasterize as a **single tall image** (a full-page capture). This matches how CSS layout and
  page-capture actually work.
- **Optional (sub-project 11):** slice the tall layout into uniform fixed-height **sheets**
  (print-style), and honor CSS **paged media** (`@page` size, `break-*`). These are *post-layout
  slicing modes* layered on the continuous layout — the continuous layout is always the primary
  product.

---

## 8. Testing strategy

Anchored on the project's "lives or dies on its test corpus" discipline, with two complementary
mechanisms per layer:

1. **W3C Web Platform Tests (WPT) reftests as the fidelity bar.** WPT is organized by CSS feature,
   so each sub-project adopts the relevant WPT slice as its acceptance bar. WPT reftests are
   **reference-comparison** (render a *test* page and a *reference* page, assert they rasterize
   identically) rather than committed-golden — which neatly sidesteps "match a real browser
   pixel-for-pixel": we only assert **self-consistency between two of our own renders**, reusing the
   existing golden comparator's tolerance (±4/channel + small differing-pixel budget). WPT is
   **BSD-3-licensed**; we vendor a **curated per-feature subset** (not the whole suite, which is
   enormous and assumes features far beyond scope), with provenance + license noted in the PR.
2. **Hand-authored generated fixtures + box-tree assertions** alongside WPT, per layer. These assert
   the computed position/size of *named* boxes numerically, so a failure localizes to a specific box
   — WPT tells you "the two pages differ," the box-tree assertion tells you *which box is wrong*.
   Some fixtures also produce committed golden PNGs, eyeballed in the PR, per existing discipline.

All tests are **hermetic**: the `ResourceLoader` test loader serves fixtures from `testdata/`; no
network in CI. A race-detector run covers the rasterization fan-out, as with the existing pipeline.
Each sub-project lands its WPT slice + fixtures + goldens in the **same PR** as the feature.

---

## 9. Dependencies (all verified pure-Go + permissive)

| Dependency | License | Role | Notes |
|-----------|---------|------|-------|
| `golang.org/x/net/html` | BSD (The Go Authors) | HTML5 tokenizer + tree builder | Already in module cache. |
| `github.com/andybalholm/brotli` | MIT (Brotli Authors) | WOFF2 (Brotli) decompression | Added at sub-project 7 only. Pure Go. |

CSS parsing is **hand-written in-tree** (`pkg/css`), consistent with the project's "own the hard
parts" ethos (the even-odd rasterizer, CCITT, and PDF function evaluator are all hand-rolled) — no
CSS-parser dependency. WOFF (zlib) uses the standard library; only WOFF2's Brotli needs the one new
dep, and only when sub-project 7 lands. Each dependency's reason is recorded in the PR that
introduces it, per CLAUDE.md.

---

## 10. Open questions deferred to sub-project specs

These are intentionally *not* resolved here; each is decided in the relevant sub-project's own
brainstorm/spec, where the detail is in scope:

- The exact CSS property subset per layer (which properties land in sub-project 3 vs. later).
- `ComputedStyle`'s concrete shape and how inheritance is represented (sub-project 1).
- The `cssbox.Box` node's exact fields and anonymous-box rules (sub-project 2).
- Margin-collapsing edge cases and the BFC algorithm details (sub-project 3).
- Image format coverage and `object-fit` fidelity (sub-project 4).
- Float/positioning corner cases and stacking-context rules (sub-project 5).
- Table width-distribution algorithm choice (sub-project 6).
- `@font-face` matching precedence and `unicode-range` subsetting (sub-project 7).
- The flex and grid sizing algorithms in detail (sub-projects 8–9).
- DOCX→cssbox lowering parity checklist and the cutover gate (sub-project 10).
- Paged-media `@page` model (sub-project 11).
```