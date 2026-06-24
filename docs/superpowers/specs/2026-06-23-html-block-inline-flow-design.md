# Sub-project 3 — Block + inline normal flow (first pixels ★)

**Status:** Implemented (branch `feat/html-block-inline-flow`)
**Date:** 2026-06-23
**Parent design:** `docs/superpowers/specs/2026-06-23-html-rendering-design.md` (overarching HTML-rendering
roadmap; this is sub-project 3 of that program, roadmap §5 row 3 — the first-pixels milestone).
**Predecessors:** sub-project 1 (CSS parse + cascade, `pkg/css`); sub-project 2 (HTML frontend + box
generation, `pkg/html` + `pkg/layout/cssbox` + `pkg/layout/css`).

---

## 1. Goal and deliverable

Build the CSS layout engine that turns the **normalized `cssbox` tree** (produced by sub-project 2)
into **positioned fragments** and **paints them**, so the public `OpenHTML` / `OpenHTMLBytes` API
produces a real rendered image. This is the first stage of the HTML pipeline that emits **pixels**.

The pipeline is now complete end-to-end:

```
bytes → pkg/html.Parse → owned DOM
      → pkg/layout/css.Build → cssbox tree (read-only)
      → pkg/layout/css.Engine.Layout → fragment tree (read-only)
      → Fragment.AppendItems → []layout.Item (flat, page space)
      → pkg/layout/paint.PaintPage → render.Device
      → pkg/render/raster → image
```

### In scope (the "core + extras" coverage delivered)
- **Block formatting context** (`pkg/layout/css/block.go`): the CSS box model — width (incl. `auto`),
  fixed and `%` widths, `box-sizing` (content-box default + border-box), `min/max-width` (and
  `min/max-height`) clamping, padding, borders, backgrounds; **vertical margin collapsing** (adjacent
  siblings + parent/first-child + parent/last-child through zero border/padding); em→pt and %→pt
  resolution; auto/fixed height; recover at the page boundary; degradation of unsupported formatting
  contexts (table/flex/grid) to block normal flow.
- **Inline formatting context** (`pkg/layout/css/inline.go`): text shaping, line breaking, and
  alignment via the shared inline core; line-box positioning; `text-align` (left/right/center/justify);
  `line-height` (auto×1.15 / px / pt / em); inline-block and replaced atoms.
- **The shared inline-layout core** (`pkg/layout/inline`): extracted from the flat DOCX engine so one
  shaper / line-breaker / alignment-math implementation serves both the flat engine and the new IFC.
- **The positioned fragment tree** (`pkg/layout/css/fragment.go`): the read-only paint input, flattened
  to the existing flat `layout.Item` slice.
- **Paint extension** (`pkg/layout/paint` + `pkg/layout/page.go`): backgrounds and 4-side styled
  borders (solid / dashed / dotted / double) on `render.Device`.
- **Public API** (`pkg/doctaculous/html_backend.go`): `OpenHTML(path)` / `OpenHTMLBytes(data, opts...)`
  with `WithViewportWidth` (default 1280) / `WithResourceLoader` / `WithLogf`, returning the same
  `*Document` the toolkit rasterizes (no new rasterization surface).
- **Two scope extensions** that make *real* HTML render:
  - **CSS shorthand expansion** (`pkg/css/shorthand.go`): `margin` / `padding` / `border` /
    `border-{top,right,bottom,left}` / `border-{width,style,color}` / `background`, expanded into the
    existing longhand `ComputedStyle` fields (real pages author boxes almost entirely via shorthands).
  - **inline-block participates inline** (`pkg/layout/css/anon.go`): box generation now treats an
    inline-block as inline-level *outer* (so a container of inline-blocks establishes an inline FC and
    they flow horizontally) while keeping it a block container *inner*.
- **Tests** (project non-negotiable): unit tests for every helper, numeric box/fragment-position
  assertions, **committed golden PNGs** (eyeballed), and **WPT-style reference-comparison reftests**
  for CSS2.1 normal-flow equivalences. The DOCX golden images are the regression oracle for the
  inline-core extraction and stay byte-identical.

### Out of scope (deferred; degrade gracefully, never panic)
Floats / positioning (sub-project 5); real replaced-element intrinsic sizing + image decode (4);
table / flex / grid *layout* (6/8/9 — block-fallback for now); web fonts (7); DOCX→cssbox convergence
(10); pagination / `@page` (11); inline-box decoration (background/border/padding on non-atomic inline
boxes); `margin:auto` centering; inline-block horizontal margins in the IFC; collapse-through of empty
blocks, clearance, `min-height` × collapse-through; overflow clipping; full inline-box `vertical-align`
(atoms are bottom-aligned for now); body-margin propagation to the viewport.

---

## 2. Architecture — one inline core, two engines, convergence later

The toolkit has two reflow box models during the HTML-rendering program (per CLAUDE.md): the **flat**
`box.Document` (DOCX today) and the **recursive** `cssbox` (HTML). They now **share one inline-layout
core**, `pkg/layout/inline`:

```
   pkg/layout (flat DOCX engine)  ─┐
                                   ├─▶ pkg/layout/inline ─▶ pkg/layout/font ─▶ pkg/font ─▶ pkg/render
   pkg/layout/css (CSS engine)  ───┘   shape · break · line     (face cache)
```

`pkg/layout/inline` depends only on `pkg/layout/font`, `pkg/font`, `pkg/render`, `image/color` — none
of which import `pkg/layout`, so the back-edges from both engines are clean downward edges (no cycle).
`pkg/layout` does **not** import `pkg/layout/css`, so the two box models coexist without a cycle. The
CSS engine (`pkg/layout/css`) is imported by `pkg/doctaculous`. The `render.Device` seam is preserved:
the engine produces device-independent fragments; only `pkg/layout/paint` touches the Device.

The flat DOCX path stays **untouched and green** (its golden images are the regression oracle proving
the inline-core extraction is behavior-preserving). DOCX→cssbox convergence and the deletion of the
flat engine remain sub-project 10.

---

## 3. The shared inline core (`pkg/layout/inline`)

Extracted from `pkg/layout/flow.go` + `linebreak.go`. Three files:
- **`shape.go`** — the neutral `Run` (styled text, hard-break, or atomic), `AtomicItem`, the shaped
  `Glyph`, the compact `Color`, and `Shape(faces, runs, logf) []Glyph` (face resolution + per-rune
  measurement; missing family skipped+logged; zero-alpha color → opaque).
- **`break.go`** — `Break(glyphs, maxWidthPt, firstWidthPt) []Line`, the greedy first-fit breaker
  (a hard-break or over-wide unit forces/owns a line).
- **`line.go`** — `Line` (glyphs + ascent/descent/lineGap/width metrics), `Align`, `Placement`,
  `Place(align, originX, availWidth, widthPt, spaceCount, last)` (alignment + justification math),
  `MakeLine` / `VisibleWidth` / `CountSpaces`.

`Run` is genuinely format-neutral: it carries no DOCX (`box.Inline`/`box.Align`) or CSS
(`css.ComputedStyle`) concept. The flat engine adapts `box.Inline` → `inline.Run` and translates
`box.Align` → `inline.Align`; the IFC adapts `cssbox` text/inline-block/replaced → `inline.Run` and
`css` `text-align` → `inline.Align`. The flat engine's residual `emitLine` (which appends flat
`layout.Item`s) stays flat-specific; the shared math (`Place`) is what both reuse.

`AtomicItem`/`Atomic` plumbing lets inline-block and replaced boxes participate as unbreakable units;
the flat engine never emits atoms, so DOCX output is unchanged.

**Behavior preservation:** the extraction is byte-faithful to the old code (an 11-point "preserve
exactly" checklist covered advance accumulation, trailing-space trimming, the first-line-width clamp,
justification rounding, the zero-alpha fixup, baseline/natural-height formulas, empty-line guarantees,
and the space set). The DOCX golden test passes **without regeneration**.

---

## 4. The fragment tree (`pkg/layout/css/fragment.go`)

The engine emits a recursive **fragment tree** (boxes contain boxes; tree order = paint order:
background → border → content, parent before child), then a flatten step walks it into the flat
`layout.Page.Items` slice — keeping `layout.Item`'s contiguous-slice contract and **paint
format-neutral** (DOCX is untouched).

- `Fragment{X, Y, W, H float64 (border box, page space); Background color.RGBA; Border [4]BorderEdge
  (indexed by layout.EdgeSide); Lines []LineFragment; Children []*Fragment; DebugTag string}`.
- `BorderEdge{Width float64; Color color.RGBA; Style layout.BorderStyle}`.
- `LineFragment{BaselineY float64; Glyphs []GlyphFragment}`; `GlyphFragment` mirrors `layout.GlyphItem`.
- `AppendItems(dst) []layout.Item` appends, per fragment: the background (if opaque) → each non-none,
  non-zero-width border edge as a strip rect → each line's glyphs → recurse into children.
- `Page(widthPt, heightPt) layout.Page` builds the single tall page.

Read-only after layout, so it is shared across the render fan-out without locks (the same invariant as
`box.Document` / `*Document` / `*layout.Pages`).

---

## 5. Block formatting context (`pkg/layout/css/block.go`)

`Engine{faces, logf}` with `New` and `Layout(ctx, root, viewportW) (*layout.Pages, error)`.

- **Initial containing block:** width = `viewportW` (px:pt 1:1), origin (0,0), height auto. The output
  is a single tall page sized `viewportW × content-height`.
- **Length resolution (the engine owns em/%):** `resolveLen(l, fontSizePt, pctBasis)` → px/pt 1:1; em ×
  font size; percent ÷100 × `pctBasis` (the containing-block **width** for width/height/margin/padding);
  auto flagged.
- **Box model:** padding/border widths clamp negative→0; a border edge counts only when its style is
  not `none`/`""`. Width auto fills the containing block minus horizontal margins/border/padding; fixed/%
  resolves, adjusted for `box-sizing` (border-box subtracts padding+border to get content width); then
  `max-width` then `min-width` clamp, then `≥0`. Auto margins compute to 0 (centering deferred).
- **Vertical margin collapsing (implemented scope):** adjacent siblings (collapsed = max-positive +
  min-negative); parent/first-child through zero top border+padding; parent/last-child through zero
  bottom border+padding when height is auto; an inline-block establishes a new BFC so its child margins
  do not collapse with it. The layout is computed in a local content-top-0 frame and a subtree shift is
  applied, so multi-level collapse-through composes.
- **Formatting-context dispatch on `b.Formatting`** (authoritative): `BlockFC` stacks block children;
  `InlineFC` runs the IFC; `TableFC`/`FlexFC`/`GridFC` **fall back to block normal flow** with a logged
  message (they arrive with their true formatting; the fallback lives at the layout stage).
- **Anonymous-box guard:** anonymous boxes carry a zero-value `ComputedStyle` (whose zero `Length` would
  read as `width:0`/`max-width:0`); `isAnonymous(b)` makes them resolve to auto width/height with no
  min/max constraint. (The IFC applies the same reasoning to `text-align` and `line-height`.)
- **Degradation:** a top-level recover in `Layout` is the page boundary (panic → a renderable partial
  page, never propagated), and a per-child recover skips one pathological subtree.

---

## 6. Inline formatting context (`pkg/layout/css/inline.go`)

When `b.Formatting == InlineFC`, `layoutInline(ctx, b, contentW, contentTopY, contentX)` returns
positioned `LineFragment`s, the total inline height, and any atomic child fragments (threaded into the
fragment's `Children` so they paint).

- **Run gathering:** depth-first over the inline subtree → `[]inline.Run`. A `BoxText` becomes a styled
  run reading the text leaf's inherited font/color/size (only the inherited fields are meaningful on a
  text box). Inline element boxes recurse (inline-box decoration deferred). A replaced box becomes an
  atom sized from style or `width`/`height` attrs (zero placeholder otherwise; intrinsic sizing is
  sub-project 4). An inline-block is laid out via `e.layoutBlock` and carried as an atom holding its
  fragment.
- **`text-align` (the anonymous-block trap):** `effectiveTextAlign(b)` reads `b.Style.TextAlign` for a
  real element box, but for a `BoxAnonBlock` (zero Style) reads the inherited align from the first
  inline child.
- **`line-height`:** `effectiveLineHeight(b, line)` resolves `LineHeight` (auto → metrics × 1.15,
  matching the flat engine's `defaultLineMult`; px/pt → exact; em → × font size). An anonymous block
  honors the **inherited** explicit line-height (read from the first text leaf, mirroring text-align),
  falling back to auto only when there is no inline content.
- **Positioning:** shape → break at `contentW` → place each line top-to-bottom; the baseline is `penY +
  line ascent`, glyph X comes from `inline.Place(align, contentX, …)` so glyph X is page-space (the
  block engine shifts only Y afterward). Atoms are bottom-aligned on the baseline (full `vertical-align`
  deferred; a tall atom on a mixed line extends above the line top, but the line *height* is floored to
  the atom so the next line does not overlap).

A required out-of-package fix landed here: `pkg/font/standard` now maps the **generic CSS family
keywords** (`serif`/`sans-serif`/`monospace`/`cursive`/`fantasy`/`system-ui`/…) to bundled faces.
The CSS default family is the generic `serif`, which previously resolved to nothing, so no HTML text
rendered; the mapping is checked *before* the named-family aliases so a concrete family is never
shadowed.

---

## 7. Additive `pkg/css` changes

- **Sizing properties** (`cascade.go`): `MinWidth`/`MaxWidth`/`MinHeight`/`MaxHeight Length` and
  `BoxSizing string` on `ComputedStyle` (not inherited; `MaxWidth`/`MaxHeight` model `none` as
  `UnitAuto`; `BoxSizing` defaults to `content-box`).
- **Shorthand expansion** (`shorthand.go`): the box shorthands use the 1–4-value clockwise rule; the
  `border` triple parses width‖style‖color in any order. Box-list shorthands drop the whole declaration
  on an invalid component (CSS-correct); the border triple skips an unrecognized component. Expansion
  happens immediately into the longhand fields, so cascade source-order/override semantics hold for
  free. `background` is color-only for now.

These ride along on `cssbox.Box` (which carries `Style` by value) with no box-gen change.

---

## 8. inline-block participates inline (`pkg/layout/css/anon.go`)

`BoxKind` conflates a box's structural/interior role with its outer level. An inline-block is
`BoxBlock` (a block container interior — CSS-correct) but is **inline-level outer** (it flows in its
parent's inline FC). Box generation now uses an outer-level predicate `isBlockLevelOuter(b)` (false for
`Display == DisplayInlineBlock`, else `b.Kind.IsBlockLevel()`) at the normalization call sites that
classify a *child* for the parent's formatting context (`reconcileFormatting`, the block-in-inline
split, `wrapInlineRuns`, whitespace-adjacency), while the call sites that ask a box's *own interior*
role stay `Kind`-based (`normalize`'s own-box check; `handleWhitespace`'s parent-is-block-container).

`isBlockLevelOuter` equals `Kind.IsBlockLevel()` for everything except inline-block, so non-inline-block
trees are bit-for-bit unaffected. The result: a container of inline-blocks becomes `InlineFC` and they
reach the IFC's atomic path, flowing horizontally (proven end-to-end via the public `Build`→`Layout`).

`cssbox.BoxKind.IsBlockLevel()`/`IsInlineLevel()` and `build.go`'s `classifyDisplay` mapping are
unchanged.

---

## 9. Public API (`pkg/doctaculous/html_backend.go`)

Mirrors `OpenDOCX`/`docxDocument` and **reuses `reflowRenderer`** (it only holds `*layout.Pages`; its
`renderPage` already does `raster.New` + `paint.PaintPage` with a uniform scale, no Y-flip — exactly
right for the single tall page). `OpenHTML(path)` reads the file and defaults the loader to a
`DirLoader` rooted at the file's directory so relative `<link>` refs resolve. Output is the same
`*Document` the rest of the toolkit rasterizes; HTML produces one page. The single-tall-image / fixed
1280px viewport model is the overarching spec §7.4 default; pagination/`@page` is sub-project 11.

---

## 10. Testing

Three complementary mechanisms (project non-negotiable, all in the same PR), all hermetic (no network):
1. **Numeric box/fragment-position assertions** (`pkg/layout/css/*_test.go`): a fixture →
   `Build`→`Layout`→navigate the fragment tree → assert named-box X/Y/W/H, margin-collapse results,
   border boxes, line positions, glyph positions. Covers every "core + extras" feature, including the
   anonymous-block text-align and line-height traps.
2. **Committed golden PNGs** (`pkg/doctaculous/html_golden_test.go` + `testdata/golden/html-*.png`):
   mirror the DOCX golden test (`-update` to regenerate); eyeballed in review. Fixtures: a styled box
   (background + solid border + padding + centered text), a multi-paragraph wrapping flow, an
   inline-block row, and the four border styles.
3. **WPT-style reftests** (`pkg/doctaculous/wpt_reftest_test.go` + `testdata/wpt/css21-normal-flow/`):
   reference-comparison pairs (a *test* page and a *reference* page asserted to rasterize identically
   within the existing ±4/channel + 0.2% tolerance) for margin-collapse, shorthand==longhand,
   box-sizing, auto-width, percent-width, and the padding shorthand. Authored in-house in the WPT
   `<link rel=match>` style (provenance noted; the W3C WPT suite is BSD-3 — no files vendored).

The **DOCX golden images** stay byte-identical (the inline-core extraction oracle). `go test -race
./...`, `go vet ./...`, `gofmt`, and `golangci-lint` are clean across the new packages.

---

## 11. Decisions locked during implementation

1. **Inline core extracted first**, with the DOCX goldens as the behavior-preserving oracle (do it
   before building the IFC on top of the same core).
2. **The CSS engine emits a fragment tree, then flattens** to `layout.Item`s — keeping paint
   format-neutral and DOCX untouched, rather than teaching paint about fragments.
3. **The engine owns em→pt and %→pt resolution** (`pkg/css` keeps lengths in their CSS unit).
4. **`box-sizing` content-box default**, min/max clamp as `max` then `min` then `≥0`.
5. **Margin collapsing scope** limited to adjacent siblings + parent/first-child + parent/last-child
   through zero border/padding; the rest is explicitly deferred and commented.
6. **Anonymous boxes are treated as auto/inherited** wherever their zero-value `Style` would otherwise
   be misread (width/height/min/max in the BFC; text-align and inherited line-height in the IFC).
7. **Generic CSS family keywords mapped** in `pkg/font/standard` (required for any HTML text to render).
8. **CSS shorthand expansion added** (scope extension) so real pages render styled boxes; expansion is
   into longhands so cascade order is preserved.
9. **inline-block made inline-level outer** in box generation (scope extension) so it flows inline via
   the IFC, while keeping its block-container interior; `BoxKind` semantics unchanged.
10. **`OpenHTML` reuses `reflowRenderer`** — no new rasterization surface; single tall page at a 1280px
    default viewport.

---

## 12. CLAUDE.md update (lands with this PR)

The "Done" roadmap gains a sub-project-3 entry (the CSS block+inline layout engine, paint extension,
and `OpenHTML` API; the shared `pkg/layout/inline` core; the new `pkg/css` sizing properties + shorthand
expansion; inline-block flowing inline). The architecture section records the shared inline-layout core
seam. No new runtime dependency is added (WPT testdata is BSD-3 in-house-authored reftests, not vendored
files).
