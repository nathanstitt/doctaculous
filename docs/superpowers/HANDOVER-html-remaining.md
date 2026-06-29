# Handover — HTML rendering: remaining roadmap + fidelity backlog

**Status:** Sub-project 10 (**CSS Grid, explicit grid**) is **DONE** — `display:grid`/`inline-grid` lays out as a real
CSS Grid Level 1 explicit grid (track sizing incl. `fr`/`minmax()`/`repeat(auto-fill|auto-fit)`, `grid-template-areas`,
line/span/auto placement with sparse+dense and row+column flow, implicit tracks, item + content-distribution alignment,
gaps, inline-grid), plus a cross-cutting **baseline backport** (real `align-items: baseline` in grid AND flexbox-row AND
table cells, via a shared `pkg/layout/css/baseline.go`). See CLAUDE.md "Done" (the Grid bullet) and
`docs/superpowers/specs/2026-06-26-html-grid-design.md`. It shipped as **PR #14 (`feat/html-grid`) stacked on
`feat/html-flexbox` (#13)**, 25 commits, 22 packages green / race-clean / lint-0.

This is a **broad** handover (requested): it surveys ALL the remaining HTML-rendering slices and the open fidelity
follow-ups so the next session can pick where to go, rather than scoping a single slice in depth. With grid done, the
HTML engine now has every major **layout mode** (block, inline, float, positioned, overflow/z-index, table, flexbox,
grid) plus replaced images and web fonts. **What's left is mostly NOT new layout modes** — it's (A) the **network/IO
frontends** (`OpenURL`, EPUB), (B) **pagination** (the one remaining engine-shaped feature), and (C) a **backlog of
fidelity follow-ups** within the existing modes. Pick a slice, then run the usual flow.

**Next action (whichever slice you pick):** the same flow as every prior slice — brainstorm → spec
(`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) → subagent-driven execution (per task:
implement → spec-review → code-quality-review → fix) → holistic final review → finish branch / stacked PR. The
recommended next slice is **`OpenURL` + the HTTP `ResourceLoader`** (sub-project 11): it's the smallest, unblocks remote
`<img>`/`<link>`/`@font-face`, and the seam is already built for it (see below). Pagination and EPUB are larger.

---

## ⚠️ Read this first: the PR stack + a pending split

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats ← #7 positioning ← #8 overflow ← #9 z-index ← #10 zindex-6b
     ← #11 tables ← #12 web-fonts ← #13 flexbox ← #14 grid   ← (you branch here)
```

- **Grid is PR #14**, stacked on `feat/html-flexbox` (#13). If the whole stack has merged to `main` by the time you
  start, branch your next slice off `main`. Otherwise branch off **`feat/html-grid`** (the tip).
- **A pending split (confirm before branching):** `feat/html-flexbox` (grid's base) still carries **out-of-band commits
  for a separate "HTML→PDF writer" sub-project A** (`docs/superpowers/specs/2026-06-26-html-to-pdf-writer-design.md` +
  `…/plans/2026-06-26-html-to-pdf-writer.md`). The user confirmed (this session) those are **still pending a split onto
  their own branch** and that they will handle it. Grid never touched them. **Before you branch, check `git log`/`git
  status`** — if those sub-project A commits are still inline on the base, confirm with the user whether they've been
  moved, so your slice doesn't inherit unrelated work. PR #14's description notes this dependency.
- **Tell every subagent (carried from #1–#10, every one earned its keep):** you are on your slice's branch; do NOT
  checkout/stash/switch branches; do NOT commit unless asked; scope every `git add` to only your files (NEVER `git add
  -A`/`.` — the sub-project A docs may still be dirty); delete any `zz_*`/`*probe*` scratch before finishing (confirm
  `git status` clean + `find . -name 'zz_*' -o -name '*probe*'` empty). **The editor panel showed phantom `zz_*` files
  AND phantom "undefined: X"/"redeclared" errors all through sub-projects 9 AND 10 even after deletion/definition — trust
  `go build`/`go test`/`golangci-lint`/`find`, NOT the panel.**

---

## The remaining slices (roughly priority order)

### Slice 11 (recommended next): `OpenURL` + the HTTP `ResourceLoader`

**What:** A public `OpenURL(url string) (*Document, error)` (+ likely `OpenURLBytes`/an option) and an HTTP-backed
`resource.ResourceLoader` so external refs — `<link>` stylesheets, `<img src>`, and `@font-face` `url(...)` — can be
fetched over the network. Today every loader is hermetic (in-memory `MapLoader`, on-disk `DirLoader`); nothing below the
public API touches the network.

**Why it's the smallest/best next step:** the **seam already exists and was built for exactly this.**
`pkg/resource/loader.go`'s package doc literally says "The library will ship an HTTP-backed loader for the public URL
path in a later sub-project." The `ResourceLoader` interface is one method: `Load(ctx, ref) (data []byte, contentType
string, err error)`. You add a third implementation (`HTTPLoader`) alongside `MapLoader`/`DirLoader`, plus base-URL
**ref resolution** (relative `<img src="foo.png">` against the document URL) which is the real design work.

**What already exists (verify against the code):**
- `pkg/resource/loader.go` — the `ResourceLoader` interface, `Resource{Data, ContentType}`, `ErrNotFound`, and the two
  hermetic loaders. Add `HTTPLoader` here (or a sibling file).
- `pkg/doctaculous/html_backend.go` — `htmlConfig{viewportPt, loader, sys, logf}`, the `HTMLOption` functional options
  (`WithResourceLoader`, `WithSystemFontProvider`, `WithViewportWidth`, `WithLogf`), and `OpenHTML(path)` which supplies
  a `DirLoader{Base: dir}` + `DiskFontProvider{Dir: dir}` rooted at the file's directory. `OpenURL` mirrors this:
  fetch the document bytes over HTTP, then call `OpenHTMLBytes(data, WithResourceLoader(httpLoaderRootedAtURL), …)`.
- The whole pipeline already routes refs through `cfg.loader` (`BuildWithFonts` for `<link>`/`@font-face`; image decode
  for `<img>`), so once the loader resolves URLs, remote resources "just work."

**The real design decisions (the brainstorm):**
1. **Ref resolution / base URL** — relative refs (`href="../style.css"`, `src="img/a.png"`) must resolve against the
   *document's* URL (and `<base href>` if you support it). This is the load-bearing piece; `net/url`'s `ResolveReference`
   is the tool. The `ResourceLoader` takes a bare `ref` string today — decide whether the loader carries the base URL
   (an `HTTPLoader{Base: *url.URL}`) or the ref arrives pre-resolved.
2. **Hermetic tests with no network** — the project's non-negotiable: tests are offline. Options: an `http.Client` with
   a custom `Transport`/`RoundTripper` that serves from an in-memory map (the cleanest — test the HTTP loader against an
   `httptest.Server` or a fake transport), so the `HTTPLoader` is real but the bytes are local. Do NOT add a test that
   hits the real network.
3. **Robustness/safety** — timeouts (honor `ctx`), redirect handling, non-2xx → `ErrNotFound` (so it degrades like a
   missing local ref), content-type from the response header (the loader returns `contentType`), size limits / a
   sane default `http.Client`. Decide how much hardening is in scope (this is a public, potentially-untrusted-URL path).
4. **`std lib only`?** — `net/http` + `net/url` are stdlib; no new dep needed. Keep it that way (the project prefers
   stdlib; a dep needs a PR-recorded reason).

**Scope cut suggestion:** land `OpenURL` + `HTTPLoader` + base-URL resolution + ctx/timeout/non-2xx degradation, tested
hermetically via a fake transport/`httptest`. Defer: `<base href>`, caching/content-addressed fetch (the webfont
follow-up notes the FaceCache is keyed by `(family, style)` and would benefit from a shared fetch cache once HTTP
lands — see the web-font follow-ups), cookies/auth, `data:` URIs (small, could include).

**Tests:** an `httptest.Server` (or fake `RoundTripper`) serving a small HTML doc + a CSS `<link>` + an `<img>` + an
`@font-face` `url(...)`; assert the page renders with the remote resources (a golden or a fragment-geometry assertion);
assert a 404 ref degrades gracefully (placeholder image / skipped stylesheet, no panic); assert ctx-cancel/timeout is
honored. **No real network in any test.**

### Slice 12: Pagination / CSS paged media

**What:** Today `engine.Layout(ctx, root, viewportPt)` produces a **single tall page** (the full-page-capture model —
see `html_backend.go` `htmlDocument`, and `defaultViewportPt = 1280`). Pagination breaks the laid-out content into
multiple fixed-height pages (and ideally honors CSS paged media: `@page` size/margins, `page-break-before/after/inside`,
widow/orphan control). This is the **one remaining engine-shaped feature** — it touches the fragment tree / the
`Layout` → pages step, not just a frontend.

**Why it's larger:** fragmentation is genuinely hard — a page break can fall inside a line box, inside a table row,
across a positioned/float element, inside a grid/flex container. The CSS Fragmentation spec is intricate. A first slice
should pick a **bounded model**: e.g. fixed page height + break **between** block-level boxes only (no mid-line/mid-row
breaks), honoring `page-break-before/after: always`, deferring widows/orphans and mid-box fragmentation. Decide the
page-size source (`@page`, an option, or a fixed default).

**Foundation:** `Layout` already returns `[]page` (the reflow renderer wraps them — `reflowRenderer{pages}`); the PDF
side already rasterizes multi-page documents (the parallel render fan-out). So the *output* shape (multiple pages)
exists; the work is the *fragmentation* logic that splits one tall layout into page-height slices at legal break points.

### Slice 13: EPUB (`OpenEPUB`)

**What:** `OpenEPUB(path)` — open an `.epub` (a ZIP + an OPF "spine" of XHTML chapters) and render each chapter through
the **existing HTML frontend**, concatenated/paginated into one `*Document`. EPUB is "ZIP/OPC container parsing + the
HTML pipeline per spine item" — conceptually like DOCX's `OpenDOCX` (ZIP/OPC) feeding the reflow engine, but each spine
item is XHTML so it reuses `pkg/html` + the CSS layout engine directly.

**Foundation:** DOCX already proves the ZIP/OPC container pattern (`pkg/docx` parse). The HTML pipeline
(`html.Parse` → `BuildWithFonts` → `engine.Layout`) is the per-chapter engine. EPUB's new work: OPF/spine parsing, the
container `META-INF/container.xml` → root file, resolving inter-chapter resource refs (a `ResourceLoader` over the ZIP —
mirrors `DirLoader` but ZIP-backed), and stitching chapters. **EPUB likely wants pagination (Slice 12) first** (a book
is inherently paginated), or at least the multi-page output shape.

---

## Fidelity follow-ups within the existing engine (a backlog, not a roadmap)

These are smaller, self-contained improvements to already-shipped modes — good "pick one when you want a bounded task"
items. The authoritative list is CLAUDE.md §6 (the per-mode follow-up paragraphs); summarized here by mode:

- **Grid** (just shipped — see CLAUDE.md "Grid fidelity follow-ups"): **named-line placement** (`grid-column: start /
  end` referencing `[name]`s — today `[name]` tokens are parsed-and-ignored, named-line placement falls to
  auto-placement); the **flow-axis-locked auto-placement** case (a definite line on the flow axis + auto cross axis
  honors the span but ignores the start line — graceful, non-overlapping approximation); the **row-track content-height
  width-proxy** (an `auto`/min/max-content ROW track sizes via `measureMaxContent`, which returns a WIDTH — shared with
  flex/table); **`subgrid`** (→ `none`); auto-fit empty-track collapse is approximate; a rowspan cell whose spanned-into
  row grows from baseline doesn't re-grow (localized approximation).
- **Flexbox** (CLAUDE.md "Flexbox fidelity follow-ups"): **multi-line flex** (`flex-wrap: wrap`/`wrap-reverse` +
  `align-content` — the big one; today single-line `nowrap` with overflow); the **line cross size clamped to a definite
  container cross size** (today it's the max item's cross size); the **column `flex-basis: auto`/`content` height**
  (max-content width proxy). (`align-items: baseline` on a row is now real — done in slice 10; column still →
  `flex-start`.)
- **Tables** (CLAUDE.md "Table fidelity follow-ups"): the **six table background layers** (`<col>`/row-group background
  layering not modeled); **`empty-cells`** (always `show`); a **percentage `<col>` width with no cells in its column**;
  3D collapse border styles (`ridge`/`groove`/`outset`/`inset` → `solid`); `buildCollapsedBorders` is O(cells²).
  (`vertical-align: baseline` is now real — done in slice 10.)
- **Positioning**: precise static-position solve for an all-`auto`-offset abs box; abs `width:auto` shrink-to-fit; abs
  `margin:auto` centering; percentage `top`/`bottom` against an auto-height CB; a `bottom`-only auto-height abs box;
  `position:relative` on a **text-only inline box** (needs inline-box fragments).
- **Replaced content**: `object-position`; the ratio-preserving min/max sizing step (CSS 10.4); a percentage `height`
  basis on replaced elements; CSS **`background-image`** decode.
- **General inline/flow**: the full `vertical-align` keyword set (only atom-baseline mechanics landed); `margin:auto`
  centering; deferred margin-collapse edge cases (empty-block collapse-through, clearance, `min-height` interaction).
- **Web fonts**: synthetic bold/oblique; `unicode-range` subsetting; `font-display`; variable-font axes; `local()`
  beyond the `DiskFontProvider`; a **content-addressed fetch cache** (the `FaceCache` is keyed `(family, style)` so one
  file is fetched once per style — harmless with hermetic loaders, worth fixing once Slice 11's HTTP loader lands).
- **RTL / bidi** — the engine has **NO `direction`/bidi support anywhere**. It is the deferred item in tables, flexbox,
  AND grid (each logs "RTL … laying out LTR"). A dedicated **bidi/`direction` sub-project would unblock all three at
  once** and is the single highest-leverage cross-cutting fidelity item. Larger than the per-mode follow-ups.

---

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

The seam map, unchanged by grid:

`pkg/pdf` parse · `pkg/pdf/content` interpret · `pkg/render` device ops (`Device` interface) · `pkg/render/raster`
bitmap backend · `pkg/doctaculous` public API · the reflow side: `pkg/html` + `pkg/css` (parse/cascade) →
`pkg/layout/cssbox` (box tree) → `pkg/layout/css` (the layout engine: block/inline/float/positioned/overflow/table/
flex/grid + the shared `pkg/layout/inline` core + `baseline.go`) → `pkg/layout/paint` → `render.Device`.

- **`OpenURL`/HTTP loader** lives in `pkg/resource` (the loader) + `pkg/doctaculous` (the `OpenURL` entry + base-URL
  wiring). **No layout/engine change** — it feeds bytes through the existing `ResourceLoader` seam. The cleanest of the
  three slices, architecturally.
- **Pagination** touches `pkg/layout/css` (the `Layout` → pages fragmentation) + possibly `pkg/css` (`@page`/break
  properties on `ComputedStyle`). It is the only remaining slice that changes the **engine**.
- **EPUB** lives in a new `pkg/epub` (container/OPF parse) + `pkg/doctaculous` (`OpenEPUB`) + a ZIP-backed
  `ResourceLoader`; it **reuses** the HTML pipeline per chapter (no new layout).
- **The `render.Device` seam, the PDF pipeline, the DOCX pipeline, and the shared inline core stay untouched** by all
  three (pagination is the only one that even touches the CSS engine, and only at the `Layout`/fragment-tree level).
- **No new dependencies** for Slice 11 (stdlib `net/http`+`net/url`) or pagination. EPUB needs ZIP (stdlib
  `archive/zip`) + XML (stdlib) — also no new dep. Keep it that way; a dep needs a PR-recorded reason (CLAUDE.md
  "Non-negotiable constraints").

---

## Testing (this project lives or dies on its test corpus — see CLAUDE.md "Testing")

Every layer gets tests **in the same PR**; **hermetic, NO network** (the hard rule — even the HTTP-loader slice tests
offline via a fake transport/`httptest`). The patterns that worked across #1–#10:
- **Unit tests per layer** asserting **actual values** (not "it exists") — for layout, fragment rects (x/y/w/h);
  for parsers, the parsed struct.
- **Golden images** (`pkg/doctaculous`, `htmlGoldens` + committed PNGs at `testdata/golden/html-<name>.png`) for
  eyeball-able output. **The implementer has no image vision — they STOP after `-update` and hand back PNG paths; the
  controller eyeballs each via the Read tool.** This caught visible bugs in every slice (in #10 it confirmed the 2×2/
  fr/span/areas/auto layouts and the +3px table-collapse baseline fix).
- **WPT-style reftests** (`pkg/doctaculous`, `wptReftests` + `testdata/wpt/css21-normal-flow/NAME.html` + `-ref.html`) —
  a feature page == the same boxes built another way (abs-positioned), rasterized identically. The strongest layout
  correctness proof; grid's 4 reftests passed first-run.
- **Byte-identical guard** — an additive feature must not change existing pages. After each task, `git status --short
  pkg/doctaculous/testdata pkg/render/raster/testdata` should show only NEW files (plus any *intentionally* regenerated
  golden, which the controller eyeballs — in #10 only `html-table-collapse.png` changed, from the baseline backport, and
  was eyeballed). Run it as a dedicated checkpoint task.
- **Degradation tests** — every deferral degrades gracefully (no panic + documented fallback + log) and is **tested**.
  The flexbox holistic review caught an untested RTL→LTR; #10 covered every deferral. For Slice 11: a 404/timeout/
  cancelled-ctx must degrade (skipped stylesheet / placeholder image), tested.

---

## Process reminders (carried across #1–#10 — these earned their keep, and #10 reaffirmed every one)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`, `git push`
  over HTTPS) with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with cache/permission/"no go
  files to analyze" errors that are NOT real failures; re-run disabled. **Note for Slice 11:** the HTTP-loader tests
  use a fake transport / `httptest` (loopback), so they're fine sandboxed; do NOT add a test that needs real outbound
  TLS.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale "undefined"/"unused"/
  "redeclared"/"not in go.mod" errors AND phantom `zz_*`/`*probe*` scratch files that no longer exist. Trust
  `go build`/`go test`/`golangci-lint`/`find . -name 'zz_*'`, not the panel. (Across #10 the panel showed phantom
  `undefined: layoutGrid`/`fixupGrid`/`firstBaselineOffset`, `gridContainer`/`absf`/`spanLine`/`spanOf` "redeclared",
  and a dozen deleted `zz_probe*` files — ALL phantom; the real `go test`/lint were green every time.)
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific packages
  (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/... ./pkg/resource/...`), not the repo root. **NO `//nolint`**; the
  repo **declines all "modernize" hints** (`max()`/`min()` builtins, `slices.*`, `maps.*`, range-over-int, `SplitSeq`) —
  keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`/`sort.Ints`.
  golangci-lint flags `if !(a && b)` (QF1001 — De Morgan form), bare `x.Close()` (errcheck — `_ = x.Close()`), `S1011`
  (use `append(dst, src...)`), and `ineffassign` (a value assigned then never read). The `unused` linter IS enforced —
  a struct field/const/exported constructor you add must be *read* by code in the same PR; defer adding it until the
  consuming task. (In #10 the track-kind and line-kind constants had to be **exported** the moment `pkg/layout/css`
  read them as `gcss.*` — a cross-package version of the same rule.)
- **Verify against the spec + the actual code, don't trust the handover/plan blindly.** Across #10 the plan referenced
  helper names that collided with existing code (`spanOf`, `gridContainer`, `absf`) — the implementers renamed/reused by
  reading the real code. **For an intricate algorithm (the next equivalent of grid §11 / flexbox §9.7), confirm the spec
  steps against the actual W3C text before encoding** — but note `WebFetch`'s summarizer **truncates before the deep
  algorithm sections** (it did for grid §11 and flexbox §9.7); encode from known-good knowledge and rely on
  **hand-computed unit tests as the arbiter**. **A change that forces inverting a passing test is a red flag — re-verify
  the algorithm, don't edit the test.** (This rule found TWO real Critical bugs in grid's §11 resolver that all 13 unit
  vectors initially missed — see the next point.)
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review earn their keep — hugely.**
  In #10 the **adversarial code-quality review of the pure §11 track resolver found two Critical bugs the unit vectors
  missed** (a growth-limit collapse that mis-sized every `auto` column with wrapping text, and a fixed-max overflow) —
  both invisible because every vector held `minContent == maxContent`. The **holistic reviewer ran 29 adversarial probes**
  that varied what the per-task tests held fixed (container off page-origin, grid-in-grid, grid-in-flex/table,
  column-flow+dense+spans combined, all alignment sources stacked, multi-row baseline cascade, an overlap oracle) and
  confirmed the flexbox-class "collapse-to-0 off-origin" bug does NOT exist in grid. **Have reviewers write throwaway
  probes that vary the conditions unit tests hold fixed — and DELETE every probe** (confirm `find` empty). **Render real
  pages at milestones** (controller, via Read) — every visible bug across the project was caught by rendering.
- **Strengthen weak assertions.** Geometry tests compare actual rects, not "an item exists." In #10 a degradation test
  that *claimed* to test unknown-value handling actually re-tested the default (the harness overrode it) — the reviewer
  caught it and it was moved to a cascade test that genuinely exercises the deferral. A test that doesn't test what it
  claims is worse than no test.
- **Prefer the simpler mechanism.** #10 reused `measure.go` for track content sizing, `layoutBlock`/the fragment/paint
  path for item contents, the table `intrinsicWidth` shrink-to-fit path for `inline-grid`, the existing
  `shiftCellContent` for table baseline, and the table occupancy-scan *pattern* (not code) for placement — inventing
  only the §11 track-sizing + §8 placement algorithms themselves. For Slice 11, reuse the `ResourceLoader` seam and the
  `htmlConfig`/options wiring; invent only `HTTPLoader` + base-URL resolution.
- **Scope every commit** (`git add <specific files>`, never `git add -A`/`.`; `git show --stat` to confirm). The
  sub-project A docs sat dirty/committed on the base the whole time; #10's every commit was explicitly scoped and the
  sub-project A docs were confirmed untouched at the end.
- **Update CLAUDE.md when the PR lands** — move the slice from §6 remaining-slices into a new "Done" bullet, update the
  §6 done-slices parenthetical, and add/curate the fidelity-follow-ups note. Keep Done/TODO the honest source of truth,
  as #7–#10 did (and #10 also marked the flex/table baseline deferrals *resolved* in their follow-up notes when the
  backport landed — when a slice resolves a previously-deferred item elsewhere, update that note too).

## Recommendation

**Do `OpenURL` + the HTTP `ResourceLoader` (Slice 11) next.** It is the smallest remaining slice, the seam was built for
it (`pkg/resource/loader.go`'s own doc promises it), it unblocks remote `<img>`/`<link>`/`@font-face` in one stroke, and
it needs no engine change — so it's a clean, bounded sub-project. The load-bearing brainstorm decisions are **base-URL
ref resolution** and **how to test HTTP hermetically** (fake transport / `httptest`, never the real network). Pagination
(the only remaining engine-shaped feature) and EPUB (which probably wants pagination first) are the larger follow-ons;
the RTL/bidi cross-cutting sub-project is the highest-leverage fidelity item if you'd rather deepen than broaden.
