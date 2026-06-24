# Sub-project 2 — HTML frontend + box generation

**Status:** Approved design
**Date:** 2026-06-23
**Branch:** `feat/html-box-generation`
**Parent design:** `docs/superpowers/specs/2026-06-23-html-rendering-design.md` (overarching HTML-rendering
roadmap; this is sub-project 2 of that program, roadmap §5 row 2).
**Predecessor:** sub-project 1 (CSS parse + cascade), shipped as `pkg/css` (PR #2).

---

## 1. Goal and deliverable

Build the HTML frontend and box-generation stage of the HTML-rendering pipeline: turn HTML + CSS
bytes into a **correct, well-formed recursive box tree** (`cssbox`), ready for the layout engine that
arrives in sub-project 3.

**This sub-project produces NO pixels.** There is no layout, no paint, no rasterization. The
deliverable is a correct `cssbox` tree, verified by **structural assertions** on the tree shape — not
golden images. (Golden images and the first WPT reftest slice belong to sub-project 3, which is the
first stage that produces pixels.)

### In scope
- `pkg/html` — wrap `golang.org/x/net/html` into an owned DOM that implements the `pkg/css` `Node`
  interface; collect `<style>`, `<link rel=stylesheet>`, and inline `style=""`; ship a minimal
  user-agent (UA) default stylesheet.
- `pkg/resource` — the `ResourceLoader` seam plus hermetic in-memory / testdata loaders. **No HTTP**
  (HTTP fetching is sub-project 7).
- `pkg/layout/cssbox` — the recursive, format-neutral box tree (the long-lived contract that the
  layout engine and the eventual DOCX convergence both consume).
- `pkg/layout/css` — box generation: walk the DOM, drive the `pkg/css` cascade per node, emit the
  `cssbox` tree, including anonymous-box fixups (inline-in-block and block-in-inline).
- Two small additive changes to the existing `pkg/css`: nil-parent root handling, and origin-aware
  cascading (UA below author). No behavior change for the normal-flow subset; the existing in-package
  tests are updated for the new `NewResolver` signature.

### Out of scope (deferred; must degrade gracefully)
- Any layout or positioning (sub-project 3+).
- Image **decoding** — an `<img>` becomes a replaced *box* carrying its attributes, with no pixels
  (decoding + intrinsic sizing is sub-project 4).
- HTTP resource fetching (sub-project 7).
- `@media` / `@font-face` / `@page` handling beyond what `pkg/css` already skips.
- `<table>` / flexbox / grid **layout** — these elements generate boxes with the correct display
  kind, but laying them out is sub-projects 6 / 8 / 9.

### Non-negotiables inherited from the project (CLAUDE.md)
Pure Go, no CGo/WASM, MIT/BSD/Apache deps only; no panics on malformed input; every layer
independently unit-tested; new feature ⇒ new fixture + test in the same PR; unsupported constructs
degrade gracefully (skip + debug log / typed error).

---

## 2. Package layout

| Package | Responsibility | Key public surface |
|---|---|---|
| `pkg/html` | Wrap `x/net/html` → an owned DOM implementing `css.Node`; collect `<style>` / `<link>` / inline `style=""`; ship the UA stylesheet. | `Parse([]byte) (*Document, error)`; DOM node types `*Element` / `*Text`; `UAStylesheet` |
| `pkg/resource` | `ResourceLoader` seam + hermetic loaders. **No HTTP.** | `ResourceLoader` interface; `MapLoader`, `DirLoader`; `ErrNotFound` |
| `pkg/layout/cssbox` | The recursive neutral box tree (long-lived contract). | `Box`, `BoxKind`, `DisplayKind`, `FormattingContext`, `FloatKind`, `PositionKind`, `ReplacedContent` |
| `pkg/layout/css` | Box generation: DOM + cascade → `cssbox` tree, incl. anonymous boxes. | `Build(ctx, *html.Document, resource.ResourceLoader, logf) (*cssbox.Box, error)` |

These mirror the `pkg/pdf` / `pkg/pdf/filter` / `pkg/pdf/content` layering: each package is cohesive,
has one clear purpose, communicates through a small interface, and is independently testable. No
cyclic dependencies: `pkg/css` imports nothing from `pkg/html` (the `Node` interface keeps the
dependency one-directional); `pkg/layout/css` imports `pkg/html`, `pkg/css`, `pkg/resource`, and
`pkg/layout/cssbox`.

### Dependency

`golang.org/x/net/html` — BSD (The Go Authors), pure Go, already in the module cache at v0.43.0 (the
version matching the existing `golang.org/x/image v0.43.0` / `golang.org/x/text v0.38.0` line). It is
the one new dependency this sub-project adds; the reason is recorded in the PR per CLAUDE.md. It is a
tokenizer + HTML5 tree-builder we read **once** — no other use.

---

## 3. Data flow (the chosen architecture)

Box generation **drives the cascade** in a single recursive descent, and `ComputedStyle` lives **only
on the box** — never on the DOM node, never lazily re-derived from a back-reference. The DOM node
stays a pure structural / selector-matching view; the `cssbox.Box` is self-contained and
format-neutral. This is the only data-flow shape in which the box model is genuinely independent of
its frontend, which is the whole point of the model: in sub-project 10 the DOCX frontend converges
onto `cssbox`, and DOCX has no DOM to annotate or point back to.

```
bytes ─▶ pkg/html.Parse ─▶ owned DOM (*Element/*Text), + collected <style>/<link>/style=""
                                  │
   UA stylesheet (pkg/html) ──────┤  assemble sheets in origin order:
   <link> sheets (pkg/resource) ──┤    [UA: OriginUA] ++ [<style> + <link>: OriginAuthor]
                                  ▼
                          css.Resolver over the origin-tagged sheets
                                  │
                                  ▼  recursive descent (pkg/layout/css/build.go):
                                     per node: Compute(style) ─ thread parentStyle down
                                     map display → BoxKind/DisplayKind; <img> → replaced leaf
                                     text nodes → text runs (whitespace collapsed in context)
                                  │
                                  ▼  anonymous-box normalization (anon.go):
                                     inline-in-block wrap · block-in-inline split · whitespace
                                  ▼
                          cssbox tree (root *Box) — READ-ONLY after Build
```

Rejected alternatives: (B) two passes annotating the DOM with `ComputedStyle` then lowering — splits
style across two places, makes the DOM no longer a pure selector view, and the "annotate the DOM"
pattern does not generalize to DOCX. (C) boxes that reference DOM nodes with lazy style — breaks box
format-neutrality, ties box lifetime to the DOM, and muddies the read-only-after-build concurrency
invariant.

---

## 4. `pkg/html` — the owned DOM

`x/net/html` parses bytes into its own `*html.Node` tree (a spec-compliant HTML5 tokenizer +
tree-builder). We walk that tree **once** into our own owned DOM, then never touch `x/net/html`
again. The owned tree pre-computes everything the cascade needs, so the cascade tree-walk does zero
allocation and zero re-splitting per call — and it gives box generation an ordered, typed structure
to traverse.

### Node types

```go
// DOMNode is the common interface over the owned tree (Element and Text).
type DOMNode interface {
    Parent() *Element
}

// Element is an HTML element. It implements css.Node.
type Element struct {
    tag      string            // lowercased element name, e.g. "div"
    id       string            // id attribute, pre-extracted ("" if absent)
    classes  []string          // class list, pre-split once at build time
    attrs    map[string]string // attribute map, lowercased keys
    parent   *Element          // nil at the root
    children []DOMNode         // Elements and Texts, in document order
}

// Text is a character-data node.
type Text struct {
    Data   string
    parent *Element
}
```

`*Element` satisfies `css.Node` directly:
- `Tag() string` → `tag`
- `ID() string` → `id`
- `Classes() []string` → `classes`
- `Parent() css.Node` → `parent` (returns `nil` at the root, which the cascade treats as the
  initial-values base — see §6)
- `Attr(key string) (string, bool)` → `attrs` lookup

All four are pre-computed at build time; nothing is derived per call. This is the payoff of choosing
an owned tree over a live wrapper over `*html.Node`.

### `Document` (the parse result)

```go
type Document struct {
    Root        *Element         // the <html> element
    StyleSheets []css.Stylesheet // parsed <style> contents, in document order
    LinkRefs    []string         // hrefs of <link rel=stylesheet>, unresolved
}
```

Inline `style=""` needs no field: the cascade reads it directly via `Attr("style")`, which is already
how `pkg/css` consumes inline styles.

### Collection during the walk
- `<style>` → take its text child, `css.Parse` it, append to `StyleSheets` in document order (order
  is a cascade tie-breaker, so it must be preserved).
- `<link rel="stylesheet" href="...">` → record `href` in `LinkRefs`. **Not fetched here**; resolution
  goes through `pkg/resource` in box generation (§7). An unresolved/absent link degrades to "no
  stylesheet" + debug log.
- `style="..."` → left on the element's `attrs`; no special handling.

### Whitespace text nodes
The DOM **keeps all text nodes**, faithfully mirroring the document, including whitespace-only text
between block elements (`</p>\n  <p>`). Whitespace collapsing and the stripping of
collapsible-whitespace runs adjacent to block boundaries happen in **box generation** (§7), which is
the layer that knows the block/inline context and can apply the rule correctly. Stripping at
DOM-build time would lack that context and risks discarding significant whitespace.

### Malformed input
`x/net/html`'s tree-builder is lenient (inserts implied `<html>`/`<head>`/`<body>`, closes tags,
recovers from bad nesting), so hard parse errors are rare. `Parse` returns `error` only for truly
unreadable input; structural oddities yield a valid-but-quirky tree, never a panic.

### UA stylesheet
`pkg/html` ships a minimal UA default stylesheet as a `const` source string run through `css.Parse`,
exposed as `UAStylesheet` (a `css.Stylesheet`). It is cascaded at the UA origin (below author rules;
see §6). It is the minimum needed to make HTML render as HTML — without it every element would
compute as `display:inline` (the CSS initial value) and box generation would produce a nonsensical
all-inline tree.

Initial coverage (scoped to this sub-project's needs; grown by later sub-projects as required):
- **`display:block`**: `html, body, div, p, h1–h6, section, article, header, footer, nav, main,
  aside, ul, ol, li, blockquote, pre, table, form, figure, figcaption, hr`. (`li` is `display:list-item`.)
- **`display:none`**: `head, title, meta, link, style, script`.
- **block-level table parts**: `tr` (`table-row`), `td`/`th` (`table-cell`), etc., recorded with the
  appropriate `DisplayKind` even though table *layout* is sub-project 6.
- **Heading sizes/margins**: the conventional `h1`–`h6` `font-size` and top/bottom `margin` values, so
  generated trees carry browser-like defaults.

The exact rule text is finalized during implementation; it is ordinary CSS the existing parser
already handles.

---

## 5. `pkg/resource` — the loader seam

```go
// ResourceLoader resolves a URL/ref referenced by a document (a <link>
// stylesheet today; images and fonts later) to its bytes and content type. The
// HTTP-backed default ships in a later sub-project; this sub-project ships only
// hermetic loaders so no layer below the public API touches the network. Honors
// ctx cancellation.
type ResourceLoader interface {
    Load(ctx context.Context, ref string) (data []byte, contentType string, err error)
}
```

This matches the overarching spec §7.1. Network access lives **only** in the future HTTP loader;
`pkg/css`, `pkg/layout/css`, and (later) `pkg/font` see only a `ResourceLoader`, keeping them pure and
unit-testable and satisfying the project's "no network in tests" rule.

### Hermetic implementations (this sub-project)
- `MapLoader` — an in-memory `map[string]Resource` (`{Data []byte; ContentType string}`). The
  workhorse for unit tests: register `"theme.css" → (bytes, "text/css")` and point a
  `<link href="theme.css">` at it.
- `DirLoader` — serves files from a base `testdata/` directory; content type inferred from the file
  extension. For fixtures more natural as on-disk files.

### Sentinel error
`ErrNotFound` so callers distinguish "missing" from "broken" and apply the degradation rule (a
`<link>` that 404s → treat the resource as absent + log; render without it).

### Deferred to the sub-project-7 HTTP loader
Timeouts, redirects, TLS, size caps, content-type sniffing, and URL resolution against a base href.
In this sub-project, `ref` is an opaque key the loader resolves however it chooses.

---

## 6. `pkg/css` changes (additive)

Two small changes to the existing, shipping `pkg/css`. Neither changes the computed result for the
normal-flow property subset; the only callers are `pkg/css`'s own in-package tests, updated in the
same PR.

### 6.1 Nil-parent root handling
Today `Compute(n, parentStyle)` requires the caller to pass `initialStyle()` for the root, but
`initialStyle()` is unexported — a day-one blocker for box generation, which lives in a different
package. Fix: make the root case `pkg/css`'s own concern so callers never touch CSS initial values.

The chosen shape keeps `initialStyle()` **unexported**: add a thin `ComputeRoot(n Node) ComputedStyle`
wrapper (equivalent to `Compute(n, initialStyle())`) that box generation uses as the recursion's
entry point. `Compute(n, parentStyle)` is unchanged for the non-root case. (The existing integration
test, which passes `initialStyle()` explicitly for the root, continues to compute identical values.)

### 6.2 Origin-aware cascade (UA below author)
The UA stylesheet must lose to author rules of **any** specificity, yet UA `!important` must still
beat author normal. Real CSS origin order is:

```
UA-normal  <  author-normal  <  author-important  <  UA-important
```

The current `Resolver` holds a single `Stylesheet` and sorts only by (specificity, source order) —
origin is not represented. A naive "prepend the UA sheet as earliest source order" hack is **wrong**:
it would let a high-specificity UA rule (e.g. a UA `a {…}` type selector) beat a low-specificity
author rule (e.g. `.foo {…}`), because specificity, not source order, decides between them. Origin
must be a first-class, outermost cascade key.

Mechanism — tag each sheet with an origin and replace the constructor:

```go
type Origin int
const (
    OriginUA     Origin = iota // user-agent default sheet
    OriginAuthor               // <style>, <link>, style=""
)

type OriginSheet struct {
    Sheet  Stylesheet
    Origin Origin
}

// NewResolver builds a Resolver over origin-tagged sheets. (Replaces the prior
// single-Stylesheet constructor; the in-package tests are updated.)
func NewResolver(sheets []OriginSheet, logf func(string, ...any)) *Resolver
```

The cascade gains origin as the outermost comparison in its sort key, ordered to encode the origin
sequence above. Inline `style=""` keeps its current treatment (stronger than any author rule), and
inline-`!important` keeps the existing `inlineImportantIDs` mechanism — both are author origin, so the
origin layer slots cleanly above the existing specificity/source-order logic without disturbing it.

`NewResolver` is replaced (not supplemented) by the slice/origin form: one clear constructor, origin
is a first-class input, and there are no external consumers yet.

---

## 7. `pkg/layout/cssbox` and box generation

### 7.1 The `cssbox.Box` type (rich, anticipating layout needs)

The box tree is the long-lived contract that the layout engine (sub-project 3+) and the DOCX
convergence (sub-project 10) both consume. To minimize churn on that contract, the box carries the
**structural / layout-intent vocabulary** that box generation can populate correctly today — *not*
half-implemented layout state.

```go
package cssbox

type BoxKind int
const (
    BoxBlock      BoxKind = iota // block-level box (display:block/list-item/table/flex/grid/…)
    BoxInline                    // inline-level box (display:inline)
    BoxAnonBlock                 // anonymous block wrapping inline runs in a block container
    BoxAnonInline                // anonymous inline (block-in-inline split remainder)
    BoxReplaced                  // replaced element (<img>): a leaf, intrinsically sized later
    BoxText                      // a text run (leaf)
)

// Box is the recursive, format-neutral layout input. Read-only after Build, so it
// is shared across the render fan-out without locks (same invariant as
// box.Document and *Document today).
type Box struct {
    Kind     BoxKind
    Style    css.ComputedStyle // resolved style; inherited/zero for anonymous boxes
    Children []*Box

    Text     string            // set only for Kind==BoxText
    Replaced *ReplacedContent  // set only for Kind==BoxReplaced

    // Layout-intent hints, derived from Style at generation time:
    Display    DisplayKind       // normalized outer/inner display of this box
    Formatting FormattingContext // the formatting context this box ESTABLISHES for its children
    Float      FloatKind         // none/left/right (read by sub-project 5)
    Position   PositionKind      // static/relative/absolute/fixed (read by sub-project 5)
}

type DisplayKind int        // Block, Inline, InlineBlock, ListItem, Table, TableRow, TableCell, Flex, Grid, …
type FormattingContext int  // BlockFC, InlineFC, TableFC, FlexFC, GridFC
type FloatKind int          // FloatNone, FloatLeft, FloatRight
type PositionKind int       // PosStatic, PosRelative, PosAbsolute, PosFixed

// ReplacedContent holds replaced-element facts. In this sub-project it carries
// only the source facts (no decoded image); sub-project 4 adds intrinsic size.
type ReplacedContent struct {
    Tag   string            // "img"
    Attrs map[string]string // src, width, height, alt, …
}
```

**What box generation populates correctly now:** `Kind`, `Style`, `Children`, `Text`, `Display`,
`Float`, `Position`, `Replaced` facts, and `Formatting` (derivable from display and child
composition: a block box whose children are all inline establishes an *inline* FC, otherwise a *block*
FC; `display:flex` establishes a flex FC; etc.). These follow deterministically from the computed
style plus child composition. The flow-context determination that depends on child composition
(block-vs-inline FC) is finalized in the normalization post-pass — after anonymous-box fixups settle
the child list — so a real block with all-inline content and an anonymous block holding inline runs
report the same `InlineFC`; non-flow contexts (table/flex/grid) keep their keyword-derived value.

**What is deliberately NOT on the box yet** (it would be speculative dead weight, and sub-project 3+
will design it as layout *output*, not generation *input*): computed/used sizes, margin-collapse
state, line boxes, and the positioned fragment tree. "Rich" here means *structural and intent
vocabulary*, not pre-computed layout results.

### 7.2 Box generation (`pkg/layout/css/build.go`)

```go
func Build(
    ctx context.Context,
    doc *html.Document,
    loader resource.ResourceLoader, // may be nil; <link>s then skipped + logged
    logf func(string, ...any),       // may be nil
) (*cssbox.Box, error)
```

Steps:

1. **Assemble sheets in origin order:** `[{html.UAStylesheet, OriginUA}]` followed by each
   `doc.StyleSheets` entry and each successfully resolved `<link>` sheet, all `OriginAuthor` (document
   order preserved). Build one `css.Resolver` over them.
2. **Resolve `<link>` refs** via `loader.Load`; on success `css.Parse` the bytes into an author sheet;
   on `ErrNotFound`/error/nil-loader, log and skip (the page still has its `<style>` sheets + UA
   defaults). Honors `ctx` cancellation.
3. **Recursive generate** from `doc.Root`:
   - Compute the element's `ComputedStyle` — the root via `ComputeRoot`, children threading the
     parent's computed style down.
   - `display:none` → generate nothing (prune the subtree).
   - Map computed `display` → `BoxKind` + `DisplayKind`; set `Float`/`Position` from the style.
   - Recognized-but-not-yet-laid-out displays (`flex`, `grid`, table parts) are **preserved** as their
     true `DisplayKind` / `FormattingContext` — the block fallback for them happens later, at the
     **layout** stage (sub-project 3's rule), not here. Only **genuinely unknown** display values
     (typos, unsupported keywords) are normalized to block at generation time.
   - `<img>` (and other replaced tags) → a `BoxReplaced` leaf carrying its attribute facts.
   - Text nodes → `BoxText` runs, with whitespace collapsed in context (step 4).
4. **Anonymous-box normalization** (its own scoped step, `anon.go`), applied to each box's child list:
   - **inline-in-block:** if a block container has any block-level child, wrap each maximal run of
     inline-level children (and text) in a `BoxAnonBlock`.
   - **block-in-inline:** if an inline box contains a block-level box, split the inline around the
     block, producing anonymous boxes so the block breaks out of the inline.
   - **whitespace:** drop inline runs that are entirely collapsible whitespace adjacent to a block
     boundary (so `</p>\n<p>` does not create a spurious anonymous block); collapse internal
     whitespace runs to a single space.
   The post-condition is the invariant sub-project 3's formatting contexts assume: **a block
   container's children are either all block-level or all inline-level.**
5. Return the root `*Box`. Read-only thereafter.

---

## 8. Error handling and concurrency

**Error model** (mirrors the PDF side and the overarching spec's degradation table):
- `pkg/html.Parse` returns `error` only on truly unreadable input.
- Box generation **never panics** on malformed input. Degradations: unknown display → block;
  `display:none` → pruned; unresolved `<link>` → skipped + `logf`; unknown replaced tag → treated as a
  normal element. A **recover at the `Build` entry boundary** ensures one pathological subtree cannot
  kill the build (it returns a partial tree + logs), matching the project's "recover at the page
  boundary" rule.
- Sentinel errors: `resource.ErrNotFound`. `pkg/css` keeps its existing total-parse behavior (bad
  rules/declarations skipped, sheet preserved).

**Concurrency:** the `cssbox` tree and the owned DOM are **read-only after their respective build
calls**, so they are shared across the future render fan-out without locks — the same invariant as
`*box.Document`, `*Document`, and `*layout.Pages` today. `Build` itself is a single-threaded tree
walk; parallelism lives in layout/raster (overarching spec §7.2), which this sub-project does not
touch.

---

## 9. Testing

The project lives or dies on its test corpus. This sub-project is verified by **structural assertions
only** — there are no pixels yet, so there are **no golden images and no WPT slice here** (golden
images and the first WPT reftest slice are sub-project 3, the first pixel-producing stage). Structural
assertions are this sub-project's fidelity bar: they walk the produced tree and assert kinds,
display, nesting, and anonymous-box insertion, so a failure localizes to a specific box.

Per-package coverage:
- **`pkg/html`**: parse fixtures → assert DOM shape; `<style>` / `<link>` / `style=""` collection;
  `css.Node` conformance (tag/id/classes/parent/attr); `UAStylesheet` parses; lenient handling of
  malformed/odd HTML (no panic, sensible tree).
- **`pkg/resource`**: `MapLoader` / `DirLoader` round-trips; `ErrNotFound`; `ctx` cancellation.
- **`pkg/layout/cssbox`**: type-level invariants (anonymous-box zero values, kind predicates).
- **`pkg/layout/css`** (the substantive suite): hand-authored HTML fixtures with **box-tree
  assertions** — block/inline mapping; UA defaults applied; author-overrides-UA and origin ordering
  (UA `!important` vs. author normal); `display:none` pruning; **both** anonymous-box fixups;
  whitespace handling; flex/grid preserved as their true display; `<img>` → replaced leaf; `<link>`
  resolution via `MapLoader`; malformed-HTML no-panic / partial-tree recovery.
- **Project gates**: `go vet ./...`, `golangci-lint run`, and `go test -race ./...` all green. New
  feature ⇒ new fixture + test in this PR.

All tests are hermetic: the `ResourceLoader` test loaders serve fixtures from memory/`testdata`; no
network.

---

## 10. CLAUDE.md update (lands with this PR)

Per the overarching spec §2.2, the architecture note that "a new reflow format is just a parse+lower
frontend producing `box.Document`" is revised when this sub-project lands, to reflect the two-tier
reality: the flat `box.Document` model continues to serve DOCX *during the program*, while the
recursive `cssbox` tree is the convergence target that one shared layout engine will consume (DOCX
re-points onto it in sub-project 10). A short, factual note is added to the Architecture section, and a
"Done" roadmap entry records the HTML frontend's box-generation slice (mirroring how sub-project 1's
`pkg/css` parse+cascade slice is recorded). This is a deliberate architectural evolution, recorded as
such — not a contradiction.

The PR also records the `golang.org/x/net/html` dependency reason (BSD, pure Go) and notes that
`pkg/css`'s `NewResolver` / `Compute` gained origin awareness and root handling.

---

## 11. Decisions locked during brainstorming

1. **Rich `cssbox.Box`** — carry structural / layout-intent vocabulary box generation can populate
   today (kind, display, formatting context, float, position, replaced facts); defer layout *output*
   (sizes, fragments) to the layout engine.
2. **Owned DOM tree** — parse `x/net/html` once into our own nodes that pre-compute tag/id/classes and
   hold typed parent/children; serves both the cascade and box generation with zero per-call cost.
3. **Both anonymous-box fixups, fully** — inline-in-block wrapping and block-in-inline splitting, with
   structural tests, so the tree handed to sub-project 3 satisfies the all-block-or-all-inline
   invariant.
4. **`pkg/css` handles the nil-parent root internally** (`ComputeRoot`) — CSS initial values stay
   unexported.
5. **Minimal UA stylesheet in `pkg/html`, cascaded at the UA origin** (below author rules), the
   minimum to make HTML render as HTML.
6. **Box generation drives the cascade; `ComputedStyle` lives only on the box** (data-flow Approach
   A) — the box is self-contained and format-neutral, which is what the DOCX convergence requires.
7. **Real origin tracking in the cascade** (UA/Author), origin as the outermost sort key; the naive
   prepend-as-earliest-source-order hack is rejected as incorrect across specificities.
8. **`NewResolver` replaced** with the origin-aware slice form; the in-package tests are updated.
9. **Flex/grid display preserved at generation time**; the block fallback for recognized-but-not-yet-
   laid-out display happens at the layout stage (sub-project 3), not during generation. Only genuinely
   unknown display values normalize to block in generation.
