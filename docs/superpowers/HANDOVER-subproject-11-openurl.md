# Handover — after sub-project 11 (`OpenURL` + HTTP `ResourceLoader`): the remaining HTML roadmap

**Status:** Sub-project 11 (**`OpenURL` + the HTTP `ResourceLoader`**) is **DONE** and shipped as **PR #15
(`feat/html-openurl`), stacked on `feat/html-grid` (#14)**, 17 commits, whole repo green / race-clean / lint-0.
A document can now be fetched **over the network**: `OpenURL(rawURL, opts...)` fetches the HTML over HTTP(S)
and renders it, resolving relative `<link>`/`<img>`/`@font-face` refs against the document URL; `data:` URIs
decode inline; URL-userinfo becomes Basic auth (redacted in logs); it degrades gracefully and is
byte-identical to the existing `MapLoader` path (no new golden). See CLAUDE.md "Done" (the `OpenURL` bullet)
and `docs/superpowers/specs/2026-06-28-html-openurl-design.md`.

This handover surveys **what's left** so the next session can pick where to go. With `OpenURL` done, the HTML
engine has every major **layout mode** (block, inline, float, positioned, overflow/z-index, table, flexbox,
grid), replaced images, web fonts, **and** network/`data:` resource loading. **What remains is NOT new layout
modes** — it is: (A) **pagination** (the one remaining engine-shaped feature), (B) **EPUB** (a new container
frontend that reuses the HTML pipeline, and wants pagination first), and (C) a **backlog of fidelity
follow-ups** within the existing modes. Pick a slice, then run the usual flow.

**Recommended next slice:** **pagination / CSS paged media** (sub-project 12). It is the last engine-shaped
feature, EPUB depends on it, and the *output* shape (a multi-page document) already exists — only the
*fragmentation* logic is missing. See "Slice 12" below for the exact foundation and a bounded first scope.

**Next action (whichever slice you pick):** the same flow as every prior slice — brainstorm → spec
(`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) → subagent-driven execution (per task:
implement → spec-review → code-quality-review → fix) → holistic final review → finish branch / stacked PR.

---

## ⚠️ Read this first: the PR stack + the still-pending sub-project A split

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats ← #7 positioning ← #8 overflow ← #9 z-index ← #10 zindex-6b
     ← #11 tables ← #12 web-fonts ← #13 flexbox ← #14 grid ← #15 openurl   ← (you branch here)
```

- **`OpenURL` is PR #15**, stacked on `feat/html-grid` (#14). **The whole stack #2–#15 is still UNMERGED to
  `main`** (verified: `feat/html-openurl` is not an ancestor of `main`). If the stack has merged to `main` by
  the time you start, branch your next slice off `main`. Otherwise branch off **`feat/html-openurl`** (the tip).
  Confirm with `git merge-base --is-ancestor feat/html-openurl main && echo merged || echo "still stacked"`.
- **The pending sub-project A split is STILL pending (confirm before branching):** `feat/html-flexbox` (deep in
  the stack's base) still carries **out-of-band commits for a separate "HTML→PDF writer" sub-project A**
  (`docs/superpowers/specs/2026-06-26-html-to-pdf-writer-design.md` + `…/plans/2026-06-26-html-to-pdf-writer.md`,
  commits `d1cb18e`/`8ae2b2c`/`7c67bad`). These are **docs-only** and are inherited by every branch above them
  (grid, openurl, and your next slice). The user owns the split. **Sub-project 11 did NOT touch them** (verified
  at finish: `git diff <base>..HEAD` shows zero `pdf-writer`/`html-to-pdf` files). **Before you branch**, check
  `git log`/`git status` — if those commits are still inline, confirm with the user whether they've been moved,
  and keep your commits scoped so you don't add to or modify them. (They are still present as of this handover:
  `git log --oneline | grep -ci pdf-writer` → 3.)
- **Tell every subagent (carried from #1–#11, every one earned its keep):** you are on your slice's branch; do
  NOT checkout/stash/switch branches; do NOT commit unless asked; scope every `git add` to only your files
  (NEVER `git add -A`/`.` — the sub-project A docs may still be dirty); delete any `zz_*`/`*probe*` scratch
  before finishing (confirm `git status` clean + `find . -name 'zz_*' -o -name '*probe*'` empty). **The editor
  panel showed phantom `zz_*`/`*probe*` files AND phantom "undefined: X" errors throughout sub-projects 9, 10,
  AND 11 even after deletion/definition — trust `go build`/`go test`/`golangci-lint`/`find`, NOT the panel.**
  In #11 the panel showed phantom `zz_probe_test.go`/`zz_diag_test.go` in `pkg/layout/css` (a package the slice
  never touched) and a phantom "undefined: HTTPLoader" in a file that defined it — all phantom; `go test` was
  green every time.

---

## The remaining slices (roughly priority order)

### Slice 12 (recommended next): pagination / CSS paged media

**What:** Today `engine.Layout(ctx, root, viewportW)` produces a **single tall page** (the full-page-capture
model). Pagination breaks the laid-out content into multiple fixed-height pages, and ideally honors CSS paged
media: `@page` size/margins, `page-break-before/after/inside`, and widow/orphan control. This is the **one
remaining engine-shaped feature** — it touches the fragment tree / the `Layout`→pages step, not just a frontend.

**Why it's the best next step despite being larger:** it is the last feature that changes the *engine*, EPUB
depends on it (a book is inherently paginated), and the **output shape already exists** — the work is purely the
fragmentation logic.

**What already exists (verify against the code — these are the load-bearing facts):**
- `engine.Layout(ctx, root, viewportW float64) (*layout.Pages, error)` (`pkg/layout/css/block.go:47`). It
  **already returns a `*layout.Pages`** — a multi-page container (`type Pages struct { Pages []Page }`,
  `pkg/layout/page.go:13`).
- **But it always emits exactly ONE page today.** Every return in `Layout` is `&layout.Pages{Pages:
  []layout.Page{page}}` (`block.go:58`/`:65`, and the empty case `:51`). So the *container* is plural but the
  *content* is one tall page. **Pagination is the work of populating `Pages.Pages` with N page-height slices.**
- `pkg/doctaculous/reflow_backend.go` already wraps a `*layout.Pages`: `reflowRenderer{pages}`,
  `pageCount() = len(r.pages.Pages)` (`:61`), `renderPage(ctx, index, opts)` rasterizes page `index`. So a
  multi-page `Document` **already rasterizes correctly** (and the PDF side already fans multi-page renders out
  across goroutines). The *output* path is done; only the *split* is missing.
- **No `@page`/`page-break-*`/`break-*` handling exists in `pkg/css` yet** (verified: grep is empty). If you
  honor break properties, you add them to the cascade (`ComputedStyle`) like every prior property slice.
- The single-tall-page model is wired in `pkg/doctaculous/html_backend.go` (`defaultViewportPt = 1280`,
  `viewportPt`, and the "single tall page" comments at `:29`/`:71`/`:132`). The page *height* is currently
  unbounded (`HeightPt: 0` grows to content). Pagination introduces a page **height** (from `@page`, an option,
  or a fixed default) and slices at it.

**The real design decisions (the brainstorm):**
1. **The fragmentation model — pick a BOUNDED first scope.** Full CSS fragmentation is genuinely hard: a break
   can fall inside a line box, a table row, a flex/grid item, across a float/positioned element. A first slice
   should break **between block-level boxes only** (no mid-line/mid-row/mid-atom breaks), honoring
   `page-break-before/after: always` (and `avoid` if cheap), and **defer** widows/orphans and mid-box
   fragmentation. State the cut explicitly and test the deferrals degrade (a too-tall single box overflows its
   page rather than splitting — logged).
2. **Page size source** — `@page` size/margins, an `OpenHTML`/`OpenURL` option (e.g. `WithPageSize`), or a
   fixed default (e.g. US-Letter/A4 at 96dpi)? The viewport *width* already exists; you add a *height*.
3. **Where the split happens** — operate on the **fragment tree** after layout (walk top-level block fragments,
   accumulate height, start a new `layout.Page` when the next block would overflow, translating subsequent
   fragments up by the page top). This reuses the existing fragment/paint path per page. Decide whether the
   split is a post-pass over the single tall layout (simpler) or threaded into the block stacker (more correct
   for `break-inside`).
4. **Default behavior must stay backwards-compatible** — `OpenHTML`/`OpenURL` default to the single tall page
   (the current full-page-capture model) **unless** pagination is requested (an option / a non-zero page
   height / an `@page` rule). The existing golden/reftest corpus must stay byte-identical (it renders one page).

**Scope-cut suggestion:** land fixed-or-`@page` page height + **between-block** fragmentation + `page-break-
before/after: always`, defaulting to the current single-page behavior, tested with new fixtures (a multi-page
document; a forced break; an oversized-block-overflows degradation). Defer widows/orphans, `break-inside:
avoid`, mid-line/mid-row/mid-atom splitting, and running headers/footers.

**Tests:** unit tests on the fragmentation pass asserting **which fragments land on which page** (actual page
index + y-offset), a forced-`page-break-before` test, an oversized-block degradation test (overflows, no
panic), and a `pageCount()` end-to-end. A golden per page is eyeball-able (controller reads the PNGs). **Byte-
identical guard:** non-paginated pages unchanged.

**Architecture fit:** touches `pkg/layout/css` (the `Layout`→pages fragmentation) + possibly `pkg/css`
(`@page`/break properties on `ComputedStyle`) + maybe `pkg/doctaculous` (a page-size option). The
`render.Device` seam, PDF pipeline, DOCX pipeline, and shared inline core stay **untouched**. **No new dep.**

### Slice 13: EPUB (`OpenEPUB`)

**What:** `OpenEPUB(path)` — open an `.epub` (a ZIP + an OPF "spine" of XHTML chapters) and render each chapter
through the **existing HTML frontend**, concatenated/paginated into one `*Document`.

**Foundation:** DOCX already proves the ZIP/OPC container pattern (`pkg/docx` parse). The HTML pipeline
(`html.Parse` → `BuildWithFonts` → `engine.Layout`) is the per-chapter engine. EPUB's new work: OPF/spine
parsing, the container `META-INF/container.xml` → root file, a **ZIP-backed `ResourceLoader`** (mirrors
`DirLoader` but reads from the zip — a fourth loader alongside `MapLoader`/`DirLoader`/`HTTPLoader`), and
stitching chapters. **EPUB wants pagination (Slice 12) first** (a book is inherently paginated). Lives in a new
`pkg/epub` + `pkg/doctaculous` (`OpenEPUB`); reuses the HTML layout (no new layout). Needs only stdlib
(`archive/zip` + `encoding/xml`) — **no new dep.**

### Slice 14+: the RTL/bidi cross-cutting sub-project

The engine has **NO `direction`/bidi support anywhere**. It is the single deferred item in tables, flexbox,
AND grid (each logs "RTL … laying out LTR"). A dedicated **bidi/`direction` sub-project would unblock all three
at once** and is the highest-leverage cross-cutting fidelity item — but it is larger than the per-mode
follow-ups (it needs real bidi reordering in the shared inline core). Consider it if you'd rather deepen than
broaden.

---

## Fidelity follow-ups within the existing engine (a backlog, not a roadmap)

Smaller, self-contained improvements to already-shipped modes — good "pick one when you want a bounded task"
items. The authoritative list is CLAUDE.md §6 (the per-mode follow-up paragraphs); summarized by mode:

- **`OpenURL`/HTTP (just shipped — see CLAUDE.md's `OpenURL` Done bullet, "Deferred"):** **`<base href>`**; a
  **content-addressed fetch cache** (the `FaceCache` is keyed `(family, style)` so one font file is fetched once
  per style — now worth a shared fetch cache since HTTP fetches are real); **caller-controlled context**
  (`OpenURLContext` — the document fetch uses a background context today); **cookies / richer auth** (beyond
  URL-userinfo Basic, via an injected `Client`); **SSRF/proxy/redirect hardening**; and the
  **redirected-document base URL** — when the *document* fetch follows a redirect, relative sub-resource refs
  resolve against the *original* URL, not the final response URL a browser would use (the page still renders;
  sub-refs that live only at the post-redirect path degrade to placeholder/skipped). The holistic review
  flagged this as the one browser-divergent gap; fixing it is small (surface the loader's final
  `resp.Request.URL` and re-root `HTTPLoader.Base`).
- **Grid** (CLAUDE.md "Grid fidelity follow-ups"): named-LINE placement (`[name]`s parsed-and-ignored);
  flow-axis-locked auto-placement; the row-track content-height width-proxy; `subgrid` (→ `none`); auto-fit
  empty-track collapse; a rowspan cell whose spanned-into row grows from baseline.
- **Flexbox** (CLAUDE.md "Flexbox fidelity follow-ups"): **multi-line flex** (`flex-wrap: wrap`/`wrap-reverse` +
  `align-content` — the big one; today single-line `nowrap` with overflow); line cross size clamped to a
  definite container cross size; the column `flex-basis: auto`/`content` height (max-content width proxy).
- **Tables** (CLAUDE.md "Table fidelity follow-ups"): the six table background layers (`<col>`/row-group
  layering not modeled); `empty-cells`; a percentage `<col>` width with no cells; 3D collapse border styles
  (`ridge`/`groove`/`outset`/`inset` → `solid`); `buildCollapsedBorders` is O(cells²); RTL.
- **Positioning**: precise static-position solve for an all-`auto`-offset abs box; abs `width:auto`
  shrink-to-fit; abs `margin:auto`; percentage `top`/`bottom` against an auto-height CB; a `bottom`-only
  auto-height abs box; `position:relative` on a **text-only inline box** (needs inline-box fragments).
- **Replaced content**: `object-position`; the ratio-preserving min/max sizing step (CSS 10.4); a percentage
  `height` basis on replaced elements; CSS **`background-image`** decode.
- **General inline/flow**: the full `vertical-align` keyword set (only atom-baseline mechanics landed);
  `margin:auto` centering; deferred margin-collapse edge cases (empty-block collapse-through, clearance,
  `min-height` interaction).
- **Web fonts**: synthetic bold/oblique; `unicode-range` subsetting; `font-display`; variable-font axes;
  `local()` beyond the `DiskFontProvider`; (the content-addressed fetch cache is now shared with the `OpenURL`
  follow-up above).
- **RTL / bidi** — see Slice 14+ above; the single highest-leverage cross-cutting item.

---

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

The seam map, unchanged by `OpenURL`:

`pkg/pdf` parse · `pkg/pdf/content` interpret · `pkg/render` device ops (`Device` interface) ·
`pkg/render/raster` bitmap backend · `pkg/doctaculous` public API · the reflow side: `pkg/html` + `pkg/css`
(parse/cascade) → `pkg/layout/cssbox` (box tree) → `pkg/layout/css` (the layout engine:
block/inline/float/positioned/overflow/table/flex/grid + the shared `pkg/layout/inline` core + `baseline.go`)
→ `pkg/layout/paint` → `render.Device`. Resource loading: `pkg/resource` (`MapLoader`/`DirLoader`/`HTTPLoader`)
feeds bytes through the one `ResourceLoader` seam; `pkg/doctaculous` wires `OpenHTML`/`OpenURL`.

- **Pagination** touches `pkg/layout/css` (the `Layout`→pages fragmentation) + possibly `pkg/css` (`@page`/break
  properties) + maybe `pkg/doctaculous` (a page-size option). It is the only remaining slice that changes the
  **engine**.
- **EPUB** lives in a new `pkg/epub` (container/OPF parse) + `pkg/doctaculous` (`OpenEPUB`) + a **ZIP-backed
  `ResourceLoader`** (a fourth loader); it **reuses** the HTML pipeline per chapter (no new layout).
- **The `render.Device` seam, the PDF pipeline, the DOCX pipeline, and the shared inline core** stay untouched by
  both (pagination is the only one that touches the CSS engine, and only at the `Layout`/fragment-tree level).
- **No new dependencies** for pagination (stdlib) or EPUB (`archive/zip` + `encoding/xml`, stdlib). Keep it that
  way; a dep needs a PR-recorded reason (CLAUDE.md "Non-negotiable constraints").

---

## Process reminders (carried across #1–#11 — these earned their keep, and #11 reaffirmed every one)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`, `git
  push` over HTTPS) with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with
  cache/permission/"no go files to analyze" errors that are NOT real failures; re-run disabled. **Hermetic tests
  stay offline** — in #11 the HTTP-loader and `OpenURL` tests used `net/http/httptest` (loopback) and a fake
  `http.RoundTripper`, never a real outbound request; do the same for any network-shaped slice. EPUB/pagination
  tests are pure local I/O, fine sandboxed for the *test* but the build cache still needs the flag.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale "undefined"/"unused"/
  "redeclared" errors AND phantom `zz_*`/`*probe*` scratch files that no longer exist (often in packages you
  never touched). Trust `go build`/`go test`/`golangci-lint`/`find . -name 'zz_*'`, not the panel.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific packages
  (`./pkg/css/... ./pkg/layout/... ./pkg/doctaculous/... ./pkg/resource/...`), not the repo root. **NO
  `//nolint`**; the repo **declines all "modernize" hints** (`max()`/`min()` builtins, `slices.*`, `maps.*`,
  range-over-int, `SplitSeq`) — keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++`
  loops, `sort.SliceStable`. (Note: in #11 the `max` *local variable* in `HTTPLoader.fetch` shadows the builtin
  `max` — that's ACCEPTED here precisely because the repo declines the builtin; a reviewer flagged it and it was
  kept.) golangci-lint flags `if !(a && b)` (QF1001), bare `x.Close()` (errcheck — `_ = x.Close()` / `_, _ =
  w.Write(...)` in httptest handlers), `S1011`, and `ineffassign`. The `unused` linter IS enforced — a
  field/const/**exported** symbol you add must be *read* by code in the same PR (in #11 the `ErrUnsupportedScheme`
  sentinel had to be exercised by a test in the same commit, and exported track/line constants in #10 the moment
  another package read them).
- **Verify against the spec + the actual code, don't trust the handover/plan blindly.** In #11 the `fetch` method
  changed between the plan being written and Task 3 running (a `math.MaxInt64` overflow clamp landed in Task 2),
  so Task 3's implementer was told to read the CURRENT `fetch` and modify only the top, NOT paste the plan's stale
  snippet — and did. For a network/IO slice the stdlib behavior is the arbiter: **two spec items were marked
  "verify against stdlib, don't assume"** (whether `http.Client` auto-sends Basic auth from URL userinfo — it
  does, but we set it explicitly anyway; and that `Base.ResolveReference(url.Parse(""))` returns `Base` — it does)
  and both were confirmed empirically, not assumed. **A change that forces inverting a passing test is a red
  flag** — re-verify the algorithm, don't edit the test.
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review earn their keep —
  hugely.** In #11 the **spec review caught a vacuous assertion**: the auth test checked `req.URL.User` *server-
  side*, but Go's `http.Client` always hides userinfo from the server, so the assertion could never fail — it was
  replaced with a client-side `captureTransport` that genuinely bites (mutation-verified: removing the strip makes
  it fail). The **code-quality review caught** a `MaxBytes` overflow cliff, a missing `%w` sentinel
  (`ErrUnsupportedScheme`), and a credential-bearing-query redaction leak. The **holistic reviewer ran 14
  adversarial probes** that combined what per-task tests held fixed (a `data:` image end-to-end, a redirect during
  the *document* fetch, a same-origin authed *sub-ref*, an oversize document, query-string refs, `..`-past-root,
  scheme-relative cross-origin auth non-leak, a byte-equality *oracle* check) and surfaced the redirected-document
  base-URL gap. **Have reviewers write throwaway probes that vary the conditions unit tests hold fixed — and
  DELETE every probe** (confirm `find` empty + `git status` clean). **Render real pages at milestones** (the #11
  holistic reviewer rendered an `OpenURL` page with a remote `<link>` + an inline `data:` PNG to `$TMPDIR` and the
  controller eyeballed it — confirming correct pixels). Every visible bug across the project was caught by
  rendering.
- **Strengthen weak assertions.** In #11: a "some error" document-404 test was upgraded to assert `errors.Is(err,
  ErrNotFound)` (the error *chain* propagates); a `PageCount()==1` degradation test was upgraded to also assert
  the rendered page has **non-zero area** (a blank page would also be 1 page). A test that doesn't test what it
  claims is worse than no test.
- **Prefer the simpler mechanism / reuse.** #11 reused the `ResourceLoader` seam (HTTPLoader is just a third
  implementation — no interface change, no call-site change), the `htmlConfig`/options wiring (`OpenURL` mirrors
  `OpenHTML`), and `quadPNG`/`goldenDPI`/`RasterizePage` from the existing golden test for the byte-equality proof
  — inventing only `HTTPLoader` + base-URL resolution. For pagination, reuse the existing `layout.Pages`
  container, the `reflowRenderer`/`renderPage` multi-page output, and the fragment/paint path per page; invent
  only the fragmentation pass. For EPUB, reuse the `pkg/docx` ZIP/OPC *pattern* and the HTML pipeline per chapter;
  invent only OPF/spine parse + a ZIP-backed loader.
- **Test strategy when output is non-visual (a #11 lesson worth keeping).** `OpenURL` changes *where bytes come
  from*, not *what pixels result*, so the strongest honest proof was a **`bytes.Equal` raster-equality test**
  (HTTPLoader render == MapLoader render) rather than a new golden — and it kept the byte-identical guard
  trivially clean (no `testdata` changes). For pagination the output *is* visual (new page layouts) → use new
  goldens + page-index/y-offset geometry assertions. Match the proof to the change.
- **Scope every commit** (`git add <specific files>`, never `git add -A`/`.`; `git show --stat` to confirm). The
  sub-project A docs sat dirty/inline on the base the whole time; #11's every commit was explicitly scoped and the
  sub-project A docs were confirmed untouched at the end (`git diff <base>..HEAD | grep pdf-writer` empty).
- **Update CLAUDE.md when the PR lands** — move the slice from §6 remaining-slices into a new "Done" bullet, update
  the §6 done-slices parenthetical, and add/curate the fidelity-follow-ups note. Keep Done/TODO the honest source
  of truth. When a slice resolves a previously-deferred item elsewhere, update that note too.

## Recommendation

**Do pagination / CSS paged media (Slice 12) next.** It is the last engine-shaped feature, EPUB depends on it, and
the foundation is mostly built: `engine.Layout` already returns a `*layout.Pages` container and the
`reflowRenderer` already rasterizes multi-page documents (the PDF side fans them out) — the only missing piece is
the **fragmentation pass** that splits the single tall layout into page-height slices. Pick a **bounded** first
scope (fixed/`@page` page height + between-block breaks + `page-break-before/after: always`; defer
widows/orphans, `break-inside`, and mid-line/row/atom splitting), keep non-paginated pages byte-identical, and run
the usual brainstorm → spec → plan → subagent-driven execution → holistic review → stacked PR flow. EPUB (Slice 13,
wants pagination first) and the RTL/bidi cross-cutting sub-project (Slice 14+, the highest-leverage fidelity item)
are the larger follow-ons.
