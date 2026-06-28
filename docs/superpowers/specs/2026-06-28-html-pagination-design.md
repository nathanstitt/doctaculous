# HTML rendering — pagination / fixed-height page fragmentation (sub-project 12)

**Date:** 2026-06-28
**Status:** Design approved; ready for implementation plan.
**Branch:** `feat/html-pagination` (off `feat/html-openurl`, PR #15 tip; rebase onto `main` if the stack has
merged — confirm with `git merge-base --is-ancestor feat/html-openurl main`).
**Spec references:** CSS Fragmentation Module Level 3 (the *vocabulary* — fragmentation container, fragment,
forced/unforced break, `break-before`/`break-after`); CSS Paged Media `@page` (the *eventual* size/margin
source — **explicitly deferred** this slice). No full spec algorithm is encoded: this slice implements a
**bounded between-block fragmentation pass**, not full CSS fragmentation.

## Summary

Today the HTML engine lays a document out into a **single tall page**: `engine.Layout(ctx, root, viewportW)`
builds a fragment tree, computes `contentH = frag.Y + frag.H`, and returns
`&layout.Pages{Pages: []layout.Page{frag.Page(viewportW, contentH)}}` — one page, height = full content
(`pkg/layout/css/block.go:47-66`). The output *container* is already plural (`type Pages struct { Pages
[]Page }`, `pkg/layout/page.go:13`) and the rasterizer already fans **N** pages out across goroutines
(`reflowRenderer.pageCount()`/`renderPage(index)`, `pkg/doctaculous/reflow_backend.go:61-90`) — so a
multi-page `Document` already rasterizes correctly. **The only missing piece is the fragmentation pass** that
splits the one tall layout into page-height slices.

This slice adds:
1. A **page-height trigger**: a new `WithPageSize(widthPt, heightPt float64)` option on
   `OpenHTML`/`OpenHTMLBytes`/`OpenURL`. With no option the engine emits the current single tall page
   (**byte-identical** to today). With it, the engine paginates to that width × height.
2. A **between-block fragmentation pass** (`paginate`, `pkg/layout/css/paginate.go`, new) that walks the
   **top-level in-flow block fragments** of the root, accumulates their heights, and starts a new
   `layout.Page` whenever the next block would overflow the page's content height — translating each block
   into its page's local space.
3. **Forced breaks** between top-level blocks: `break-before`/`break-after: page|always` (and the legacy
   `page-break-before`/`page-break-after: always`) force the next/current block onto a fresh page.

It explicitly **defers** (each degrades gracefully, logged): mid-box fragmentation (a single block taller
than a page **overflows its page**, it is not split), mid-line / mid-table-row / mid-flex-or-grid-item
breaks, widows/orphans, `break-inside: avoid`, `break-*: avoid`, distributing **positioned/abs/fixed**
descendants across pages (they ride the root fragment and paint relative to the root — see "Deferrals" for
the exact, safe behavior), `@page` size/margins, and running headers/footers.

**No `render.Device`, PDF, DOCX, or shared-inline-core change.** The PDF and DOCX pipelines already produce
`*layout.Pages`; this slice only changes how the **CSS** engine's single fragment tree becomes one-or-many
`layout.Page`s. **No new dependency** (stdlib only).

This is sub-project 12 of the HTML-rendering roadmap — the **last engine-shaped feature** (EPUB, sub-project
13, depends on it).

## Architecture / seam fit

The seam map (CLAUDE.md "Architecture") is unchanged. Three touch points, all on the reflow side at or above
the `Layout`→pages boundary:

- **`pkg/css`** — add `BreakBefore` / `BreakAfter` string fields to `ComputedStyle` and cascade them
  (`break-before`/`break-after` + the legacy `page-break-before`/`page-break-after` aliases), exactly like
  every prior property slice. These are inherited=`no`, initial=`auto`. **No layout property is added** —
  break values are read only by the fragmentation pass, never by the box model.
- **`pkg/layout/css`** — the new `paginate.go` fragmentation pass and a new `Layout` entry point that takes a
  page height. `block.go`'s existing `Layout` stays (the single-tall-page path) and gains a sibling that
  paginates; both share `layoutTree` (the fragment-tree builder is **untouched** — pagination is a *post-pass*
  over the finished tree, the simpler of the two models the handover floated).
- **`pkg/doctaculous`** — `WithPageSize` option + threading a page height through `htmlConfig` into the engine.

The `render.Device` seam, `pkg/render/raster`, the PDF pipeline (`pkg/pdf*`), the DOCX pipeline
(`pkg/docx*`, `pkg/layout` flat engine), and the shared inline core (`pkg/layout/inline`) are **untouched**.
The flat (DOCX) engine already paginates its own way (`pkg/layout` `Layout`); this slice does not touch it.

## The fragmentation model — between top-level blocks, post-layout

### Why a post-pass (not threaded into the stacker)

The handover floats two models: a **post-pass over the single tall layout** (simpler) or **threading the
split into the block stacker** (more correct for `break-inside`). This slice takes the **post-pass**, because:

- The bounded scope **defers `break-inside`** — the only thing threading buys — so the post-pass loses
  nothing it needs.
- `layoutTree` already produces a fully-positioned fragment tree in page space; the top-level in-flow block
  fragments (`root → body → block children`) each carry an absolute `Y` and border-box height `H`. Walking
  them and re-bucketing by accumulated height is a pure tree transform — no relayout, no engine change, no
  risk to the byte-identical guard (the un-paginated path is literally unchanged).
- It reuses the existing per-fragment flatten (`Fragment.AppendItems`) **once per page**: each page is an
  independent `layout.Page` whose `Items` come from flattening that page's fragment subtree. The clip-push/pop
  balance, float layer, z-index bands, and collapsed-border emission all keep working **within a page**
  because each page flattens a self-contained fragment.

### The unit of fragmentation: top-level in-flow block fragments

The fragmentation walks the **children of the *body* fragment** (the document's top-level block boxes), the
natural break points. Concretely, the root fragment is the ICB; its child is `<html>`; whose child is
`<body>`; whose `Children` are the page's top-level blocks. The pass:

1. Resolves the **body fragment** (`root.Children[last]` — `x/net/html` always synthesizes `<html><body>`;
   `bodyOf` in the tests encodes this) and its **in-flow block children** `blocks := body.Children`.
   (Floats and positioned descendants are **not** in `body.Children` for the cases this slice paginates — see
   Deferrals; if `body` itself is the only block, the whole document is one block and pagination reduces to
   "does it fit in one page".)
2. Walks `blocks` in order, maintaining `pageTop` (the page-space Y where the current page's content begins)
   and `used` (height consumed on the current page). For each block `b` with top `b.Y` and bottom `b.Y+b.H`:
   - **Forced break before** (`b`'s `break-before`/`page-break-before` is a forced value): close the current
     page, start a new one whose `pageTop = b.Y` (so `b` lands at the new page's top).
   - **Overflow break**: if the block does not fit — `(b.Y + b.H) - pageTop > pageH` **and** the current page
     already has content (`used > 0`) — close the current page and start a new one at `pageTop = b.Y`. (If the
     page is empty and the block still overflows, the block is **taller than a page**: it stays on this page
     and overflows — the deferred mid-box-split case, logged once.)
   - Assign `b` to the current page.
   - **Forced break after** (`b`'s `break-after`/`page-break-after` is forced): close the current page after
     `b`; the next block starts a new page.
3. For each page, collect its assigned block fragments, **shift them up by that page's `pageTop`** (so the
   page's content starts at local Y 0), wrap them under a **page-root fragment** (a copy of the body/root
   chain carrying the page's blocks as `Children`), flatten to `layout.Page{WidthPt, HeightPt, Items}`.

### How a block is moved onto its page

Each page's blocks were laid out in the **single tall** page space (Y grows down across the whole document).
To paint a page, its blocks must be translated so the page's first content sits at local Y 0. This reuses the
existing **`shiftFragment`** helper (`block.go:1178`), which translates a fragment **and its descendants**
(Children, Floats, Lines, Image) by `dy`. For page *p* with top `pageTop_p`, every block on the page is
shifted by `dy = -pageTop_p`.

**The body/html wrapper:** the engine flattens from the root fragment (`<html>`), whose own background/border
and the body's are part of the page. For a faithful per-page paint, each page gets a **shallow-cloned root
chain**: `pageRoot := *root` (value copy), `pageBody := *body` (value copy), `pageBody.Children = <this page's
shifted blocks>`, `pageRoot.Children = []*Fragment{…, &pageBody}` (preserving any non-body children, though
for these fixtures body is the sole child). The clones share everything except `Children`; the root/body
border boxes are **not** re-sized per page (the body background, if any, paints at the body's original
geometry shifted by `dy` — a documented approximation; a full per-page background-painting model is a
follow-up). The page height is the **page height** (`pageH`), not the body height, so a partially-filled last
page is still a full page tall (correct for print).

### Page size source

- The page **width** continues to be the layout viewport width (`cfg.viewportPt`, default 1280pt). Pagination
  does **not** change layout width — the document still lays out at the viewport width, so existing geometry
  is preserved; only the *height slicing* is new. **Exception:** `WithPageSize(w, h)` sets **both** the
  viewport width `w` (so the caller controls the layout width) **and** the page height `h`. (Rationale: a
  caller asking for US-Letter pages wants 816pt-wide layout, not 1280pt sliced to 816 — the page width *is*
  the layout width when paginating.)
- The page **height** comes only from `WithPageSize`. With no `WithPageSize`, `pageH == 0` ⇒ **no
  pagination** ⇒ the current single tall page (the byte-identical path).
- **Default size constant** for callers who want "paginate with a sensible page": `WithPageSize` is explicit
  (caller passes w,h). The documented default for US-Letter is **816 × 1056 pt** (8.5in × 11in at 96dpi,
  px:pt 1:1 — consistent with the existing 1280px desktop-width convention). We expose it as exported
  constants `LetterWidthPt = 816`, `LetterHeightPt = 1056` in `pkg/doctaculous` so callers can write
  `WithPageSize(doctaculous.LetterWidthPt, doctaculous.LetterHeightPt)` without magic numbers. (A4 —
  794 × 1123 — and a convenience `WithLetterPages()`/`WithA4Pages()` are easy follow-ups; this slice ships the
  primitive `WithPageSize` + the Letter constants.)

## Component 1 — break properties on the cascade (`pkg/css`)

Add to `ComputedStyle` (`pkg/css/cascade.go`):

```go
// BreakBefore / BreakAfter are the CSS fragmentation break hints (break-before /
// break-after, plus the legacy page-break-before / page-break-after aliases),
// lowercased. Read only by the pagination pass (never by layout): a forced value
// ("page"/"always"/"left"/"right"/"recto"/"verso") starts the box on a new page.
// Initial "" (== auto, no forced break). Not inherited.
BreakBefore string
BreakAfter  string
```

Cascade (in the property-application switch, mirroring an existing string property like `Overflow`):

- `break-before` → `BreakBefore`; `break-after` → `BreakAfter`.
- **Legacy aliases** `page-break-before` → `BreakBefore`; `page-break-after` → `BreakAfter` (still the common
  authoring form). When both the modern and legacy form are present, normal cascade order wins (last wins at
  equal specificity) — no special-casing needed; they map to the same field.
- Values are stored lowercased and otherwise unvalidated (an unknown value reads as non-forced — graceful).

A tiny helper in the pagination pass classifies a value as forced:

```go
// isForcedBreak reports whether a break-before/after value forces a page break.
// "page"/"always" (and the named page-side values left/right/recto/verso, which we
// treat as a plain forced break — no left/right page distinction in this model) are
// forced; "auto"/"avoid"/"avoid-page"/""/anything else is not.
func isForcedBreak(v string) bool {
	switch v {
	case "page", "always", "left", "right", "recto", "verso":
		return true
	}
	return false
}
```

**Why not a layout-affecting property:** breaks change *which page* a box paints on, not its size or
position within the flow. Keeping them out of the box model means the un-paginated path ignores them entirely
(no behavior change without `WithPageSize`) and the fragmentation pass is the single reader.

## Component 2 — the pagination pass (`pkg/layout/css/paginate.go`, new)

A new file holding the pass + a new `Layout` entry point. The existing `Layout` (single tall page) is
**unchanged**; a new method paginates when given a page height:

```go
// LayoutPaged lays out root at viewportW × (paginating to pageH-tall pages) and
// returns one-or-more pages. pageH <= 0 means no pagination: it returns exactly
// what Layout returns (a single tall page) — the byte-identical path. Otherwise it
// builds the same fragment tree as Layout, then fragments the top-level in-flow
// block fragments into pageH-tall pages between block boundaries, honoring forced
// page breaks (break-before/after: page|always and the page-break-* aliases).
//
// It never panics on malformed input (the same page-boundary recover as Layout) and
// degrades gracefully: a single block taller than a page overflows its page rather
// than splitting (logged once); positioned/abs/fixed descendants ride the page that
// owns their in-flow position (see design doc "Deferrals").
func (e *Engine) LayoutPaged(ctx context.Context, root *cssbox.Box, viewportW, pageH float64) (pages *layout.Pages, err error)
```

Implementation outline:

```go
func (e *Engine) LayoutPaged(ctx, root, viewportW, pageH) (*layout.Pages, error) {
	if pageH <= 0 {
		return e.Layout(ctx, root, viewportW) // unchanged single-tall-page path
	}
	// same recover() as Layout, returning a single empty pageH-tall page on panic.
	frag := e.layoutTree(ctx, root, viewportW)
	if frag == nil {
		return &layout.Pages{Pages: []layout.Page{{WidthPt: viewportW, HeightPt: pageH}}}, nil
	}
	return e.paginate(frag, viewportW, pageH), nil
}
```

```go
// paginate fragments the laid-out root fragment into pageH-tall pages, breaking
// only between the document's top-level in-flow block fragments.
func (e *Engine) paginate(root *Fragment, viewportW, pageH float64) *layout.Pages {
	body := bodyFragment(root) // root.Children[last]; nil-safe
	if body == nil || len(body.Children) == 0 {
		// no top-level blocks: the whole doc is one page (its content height, capped).
		return &layout.Pages{Pages: []layout.Page{root.Page(viewportW, pageH)}}
	}
	blocks := body.Children

	// 1. Bucket blocks into pages by accumulated height + forced breaks.
	type pageBucket struct{ top float64; blocks []*Fragment }
	var buckets []pageBucket
	var cur pageBucket
	for _, b := range blocks {
		forcedBefore := isForcedBreak(breakBefore(b))
		overflow := len(cur.blocks) > 0 && (b.Y+b.H)-cur.top > pageH
		// Close the current page only if it has content (a forced-before on the first
		// block is a no-op, not a leading empty page).
		if (forcedBefore || overflow) && len(cur.blocks) > 0 {
			buckets = append(buckets, cur)
			cur = pageBucket{}
		}
		// The page top is the FIRST block's own page-space Y, captured when the block
		// joins a fresh page — NOT a provisional previous-block bottom (which would
		// leave the next page's content mis-shifted by any margin gap between blocks).
		if len(cur.blocks) == 0 {
			cur.top = b.Y
			// too-tall-for-a-page block on an otherwise-empty page: keep it, it overflows.
			if b.H > pageH {
				e.logf("css pagination: block taller than page (%.0fpt > %.0fpt); overflowing, not splitting", b.H, pageH)
			}
		}
		cur.blocks = append(cur.blocks, b)
		if isForcedBreak(breakAfter(b)) {
			buckets = append(buckets, cur)
			cur = pageBucket{} // next page's top is set from its own first block above
		}
	}
	if len(cur.blocks) > 0 {
		buckets = append(buckets, cur)
	}
	if len(buckets) == 0 {
		buckets = append(buckets, pageBucket{top: 0})
	}

	// 2. Build one layout.Page per bucket: shift its blocks to local Y 0, wrap in a
	//    shallow-cloned root/body, flatten. On every page AFTER the first, null the
	//    out-of-flow layers on the clone (Positioned/PositionedInfo/Floats) so an
	//    absolute/fixed box or a top-level float — which rides the root/body wrapper,
	//    not the per-page block list — is not duplicated onto every page (this slice
	//    keeps out-of-flow content on the first page only; see Deferrals).
	pages := make([]layout.Page, 0, len(buckets))
	for i, bk := range buckets {
		pageRoot := clonePageRoot(root, body, bk.blocks) // value-copies root+body, sets body.Children
		if i > 0 {
			pageRoot.Positioned, pageRoot.PositionedInfo, pageRoot.Floats = nil, nil, nil
			// (and likewise on the body clone, if body owns any)
		}
		shiftFragments(bk.blocks, -bk.top) // translate the page's blocks up
		pages = append(pages, pageRoot.Page(viewportW, pageH))
	}
	return &layout.Pages{Pages: pages}
}
```

Helpers (small, in `paginate.go`):
- `bodyFragment(root)` — `root.Children[len-1]` with nil/empty guards (mirrors the test `bodyOf`).
- `breakBefore(f)/breakAfter(f)` — read `f.Box.Style.BreakBefore/BreakAfter` (nil-Box ⇒ `""`).
- `clonePageRoot(root, body, blocks)` — value-copy `root` and `body`; set the body clone's `Children` to
  `blocks`; point the root clone's `Children` at the body clone (preserving any other root children); return
  it. **Critically a shallow copy:** the *blocks themselves* are the original fragment pointers (we shift them
  in place — and a fragment lands on exactly one page, so no aliasing across pages). Only the root/body
  wrapper structs are copied so each page can carry a different child list without mutating the shared tree.
  The original `root`/`body` are not reused after this (the function owns the tree post-layout).

**Mutation note:** `shiftFragments(blocks, -top)` mutates the block fragments in place. This is safe because
(a) each block belongs to exactly one page, and (b) the fragment tree is single-owner after `layoutTree`
returns and before any flatten — there is no concurrent reader (the render fan-out runs only after
`LayoutPaged` returns the finished `*layout.Pages`). This mirrors how the single-tall path already mutates
during layout. **Order matters:** shift *before* `Page(...)` flattens (the flatten reads the shifted Ys).

## Component 3 — `WithPageSize` option + wiring (`pkg/doctaculous`)

`html_backend.go`:

```go
// LetterWidthPt / LetterHeightPt are US-Letter (8.5in × 11in) at 96dpi (px:pt 1:1),
// the conventional default page for WithPageSize.
const (
	LetterWidthPt  = 816
	LetterHeightPt = 1056
)

// WithPageSize paginates output into fixed pages widthPt × heightPt (points). It
// sets BOTH the layout viewport width (widthPt) and the page height (heightPt): the
// document lays out at widthPt and is sliced into heightPt-tall pages, breaking
// between top-level blocks and at forced page breaks. Without WithPageSize the
// document renders as a single tall page (the default). widthPt/heightPt <= 0 are
// ignored (no pagination).
func WithPageSize(widthPt, heightPt float64) HTMLOption {
	return func(c *htmlConfig) {
		if widthPt > 0 && heightPt > 0 {
			c.viewportPt = widthPt
			c.pageHeightPt = heightPt
		}
	}
}
```

`htmlConfig` gains `pageHeightPt float64` (default 0 ⇒ no pagination). `htmlDocument` calls the paged entry:

```go
pages, err := engine.LayoutPaged(ctx, root, cfg.viewportPt, cfg.pageHeightPt)
```

`LayoutPaged` with `pageHeightPt == 0` delegates to `Layout`, so **the default path is byte-identical** —
every existing call (`OpenHTML`, `OpenURL`, the golden/reftest corpus) takes the `pageH <= 0` branch and gets
the same single tall page. `WithPageSize` is the only way to opt in. `OpenURL` gets pagination "for free" (it
already funnels through `OpenHTMLBytes` options).

## Deferrals (each degrades gracefully — tested)

| Deferred | Behavior this slice | Follow-up |
|---|---|---|
| **Mid-box fragmentation** (a block taller than a page) | The block stays on its page and **overflows** the page's bottom (painted; the page is still `pageH` tall, so the overflow is clipped only by the raster bitmap size — actually it renders past the page if `renderPage` sizes to `pageH`; **it is clipped to `pageH` because the bitmap is `pageH` tall**). Logged once. | Real block/line/row splitting. |
| **Mid-line / mid-table-row / mid-flex-or-grid-item** breaks | Never split — the whole atomic box rides one page (it is a single `Children` entry under body only if it *is* a top-level block; nested content never independently breaks). | Fragmentation inside formatting contexts. |
| **Positioned / abs / fixed descendants** | They live in the **root fragment's** `Positioned` slice (not `body.Children`), so the pagination walk does not see them; they are flattened **with the root wrapper of page 0** (the first bucket's `clonePageRoot` carries `root.Positioned`; subsequent page roots get an empty `Positioned`). Net: **absolutely/fixed-positioned content paints on the first page only**, at its original page-space Y (so it may sit off a short first page). This is a documented, non-panicking approximation; a relative box's *in-flow* block still paginates normally (relative only offsets paint). | Distribute positioned layers per page; `fixed` repeating on every page. |
| **Floats spanning a page boundary** | A float is in its BFC owner's `Floats`; a top-level float owned by body rides whichever block-bucket logic places it — but top-level floats are **not** in `body.Children`, so like positioned content they flatten with page 0's root wrapper. A float **inside** a block rides that block's page (it is in the block's subtree, moved by `shiftFragment`). | Per-page float distribution. |
| **`break-inside: avoid`, `break-*: avoid`** | Parsed onto `ComputedStyle` but **not acted on** (a block that would split still breaks at the page boundary as if `auto`). Logged is unnecessary (no behavior change); documented. | Keep-together. |
| **Widows / orphans** | Not modeled (no minimum line counts). | Line-count control. |
| **`@page` size / margins, named pages, running headers/footers** | Not parsed. Page size comes only from `WithPageSize`; margins are zero (content fills the page). | `@page` cascade + margin boxes. |

The first row's parenthetical resolves to: **the per-page bitmap is `pageH` tall** (`renderPage` sizes
`pxH = ceil(pageH × scale)`), so anything painted below `pageH` is **clipped by the bitmap bounds** — an
over-tall block is visibly cut at the page bottom, exactly the graceful degradation we want (no panic, no
infinite page). A test asserts the over-tall case yields the expected page count (it does **not** spill into a
second page) and a non-blank page.

## Testing (CLAUDE.md "Testing" — new fixtures + tests in this PR)

Match the proof to the change: pagination changes **page structure**, so the strongest tests assert **which
fragment lands on which page** (page index + local Y) and **page count**, plus goldens for eyeballing.

**Unit (pagination pass, `pkg/layout/css/paginate_test.go`):**
1. **Multi-page by height** — three stacked blocks each ~0.5×pageH ⇒ assert 2 pages, blocks 1–2 on page 0,
   block 3 on page 1 at local Y ≈ 0 (`top` subtracted). Assert page 0's block Ys are unchanged, page 1's
   block Y is its original Y minus page 0's height.
2. **Forced `break-before`** — two blocks that *fit* on one page, second has `page-break-before: always` ⇒
   2 pages (block 2 alone on page 1 despite fitting).
3. **Forced `break-after`** — block 1 has `break-after: page` ⇒ block 2 on page 1.
4. **Single block, fits** — one short block, `WithPageSize` taller ⇒ exactly 1 page (the trivial case).
5. **Over-tall block degradation** — one block taller than `pageH` ⇒ exactly **1** page (it does not spill to
   a phantom page 2), the degradation logged, the page flattens (non-empty `Items`).
6. **`pageH <= 0` ⇒ single tall page** — `LayoutPaged(…, 0)` returns the **same** `Pages` as `Layout` (assert
   `len == 1` and the page height equals content height, not a fixed page height).
7. **`isForcedBreak` table** — `page`/`always`/`left`/`right` true; `auto`/`avoid`/`""`/`junk` false.
8. **Break-property cascade** (`pkg/css`) — `break-before:page`, `page-break-after:always`, and the modern
   vs legacy precedence resolve onto `ComputedStyle.BreakBefore/BreakAfter` correctly.

**End-to-end (`pkg/doctaculous`):**
9. **`pageCount()` via `WithPageSize`** — an `OpenHTMLBytes(multiBlockDoc, WithPageSize(W, H))` Document
   reports `PageCount() == N` (N>1), each page rasterizes without error and has **non-zero area / non-blank**
   content (a stronger assertion than count alone — a blank page is also a page).
10. **Byte-identical default guard** — `OpenHTMLBytes(doc)` (no `WithPageSize`) renders **identically** to
    before: assert the single page's height equals the content height (the un-paginated invariant). Combined
    with leaving the existing golden corpus untouched, this proves the default path is unchanged.

**Golden images (`pkg/render/raster/.../testdata/golden/` or the doctaculous reflow goldens — match the
existing `html-*` convention):**
11. A `html-paginate-*` fixture rendered with a small `WithPageSize`, producing **multiple page PNGs**
    (`html-paginate-p0.png`, `-p1.png`, …) — eyeball each in the PR. Per CLAUDE.md, regenerate with the
    golden `-update` flow and eyeball every changed PNG.

**Byte-identical guard (critical):** the **entire existing golden/reftest corpus is unchanged** — no existing
test passes `WithPageSize`, so every existing page takes the `pageH <= 0` branch. Adding `paginate.go` and the
two `ComputedStyle` fields must not perturb any existing golden (the new fields default `""`, read only by the
new pass). CI proves this: existing goldens stay byte-identical; only **new** `html-paginate-*` goldens are
added.

**Hermetic / fast:** all fixtures are inline HTML strings or small generated docs; no network, no committed
binaries beyond the new goldens (small). Race-clean (`go test -race ./pkg/layout/css/... ./pkg/doctaculous/...`).

## What this slice deliberately does NOT change

- **The fragment-tree builder** (`layoutTree` and everything it calls) — pagination is a *post-pass*. A box's
  size/position within the flow is identical paginated or not.
- **`Fragment.AppendItems` / the flatten** — reused verbatim, once per page.
- **`render.Device`, `pkg/render/raster`, `pkg/layout/paint`** — a `layout.Page` is a `layout.Page`; the
  painter already handles any `WidthPt × HeightPt`.
- **The PDF and DOCX pipelines** — untouched (DOCX has its own pagination in the flat engine).
- **The shared inline core** (`pkg/layout/inline`) — untouched (no mid-line breaking).
- **Dependencies** — none added.

## Open questions resolved (from the brainstorm decisions)

- **Trigger:** opt-in `WithPageSize` option only. No `@page` this slice (deferred). Default = single tall page.
- **Default page size when paginating:** US-Letter, **816 × 1056 pt** (exposed as `LetterWidthPt`/
  `LetterHeightPt`; `WithPageSize` is explicit so the caller passes the size).
- **Fragmentation model:** between top-level in-flow **blocks only**, **post-pass** over the finished tree.
- **Backwards compat:** byte-identical default — proven by the un-paginated invariant test + the untouched
  existing corpus.
