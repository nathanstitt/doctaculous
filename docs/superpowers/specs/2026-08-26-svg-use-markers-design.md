# SVG `<use>`, `<symbol>`, and `<marker>` (PR 5 of 8) — Design

**Date:** 2026-08-26
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-use-markers`, stacked on `feat/svg-groups-clip-mask` (PR #106)

## Goal

Three reference-and-instantiate features:

- **`<use>`** instantiates a referenced subtree with the *use site's* inherited style.
- **`<symbol>`** is never rendered directly; instantiated through `<use>` it
  establishes a viewport with `viewBox`/`preserveAspectRatio`.
- **`<marker>`** paints a referenced subtree at a path's vertices, oriented to the
  path's tangent.

## Decisions taken (autonomous, with rationale)

### 1. `<use>` instantiations are NOT memoized

`clipMemo`/`maskMemo` can cache by id because their resolution is idempotent —
no per-referencer state. `<use>` is the opposite: `Shape.Style` is a by-value
field baked in at build time, and the whole point of `<use>` is that the target
inherits from the *use site*. Two `<use>`s of the same target with different
`fill` must produce different shapes. Memoizing by id would break
`style-inheritance-*.svg` and `complex-style-resolving-order.svg`.

The cost is duplicated geometry in the scene graph, which is harmless — nothing
mutates a `*Shape` after `Parse`, and the existing `maxDrawCalls` backstop was
explicitly sized with `<use>` in mind.

### 2. A new `buildingUse` guard — `followHrefChain` is NOT sufficient

`<use>` has **two** distinct cycle shapes, and the existing helper catches only
one:

- **href chain** — `<use href="#u2">` where `#u2` is itself a `<use>`.
  `followHrefChain` handles this.
- **Tree recursion** — `<use id="u1" href="#u2"><use id="u2" href="#u1"/></use>`,
  or a `<use>` targeting its own ancestor. The second `<use>` is a *descendant of
  the target*, reached by tree recursion and never by reading the first's `href`.
  `followHrefChain` would visit one element, find no `href`, and report no cycle.

So `<use>` needs a `buildingUse` set on `sceneBuilder`, mirroring
`buildingPattern`/`buildingClip`/`buildingMask` exactly, **plus** a depth cap for
long acyclic chains (a repeat-only guard does not bound `#a`→`#b`→`#c`→…).

### 3. Nested `<svg>` as a `<use>` target is OUT of scope

Seven `structure/use/xlink-to-svg-element*.svg` fixtures target a nested `<svg>`,
which is still in `unsupportedElements`. Shipping `<use>` does not ship nested
`<svg>`, and pretending otherwise would vendor fixtures whose goldens lock in
wrong output. Those seven defer with a recorded reason.

### 4. The viewport becomes a stack — and the id-keyed memos are a latent bug

`sceneBuilder.vp` is documented as single-instance: *"exactly one viewport in
play at any time."* `<symbol>` invalidates that. Saving and restoring `b.vp`
around instantiation (the same set/clear discipline `buildingPattern` uses) is
sufficient for correctness of the build itself.

**But the paint-server, clip, and mask memos are keyed by id alone.** A
`userSpaceOnUse` percentage resolved under one viewport would be cached and
reused under another. That is a real correctness bug the moment a second viewport
exists. Either key those memos by `(id, viewport)` or document the divergence
explicitly — decided during implementation, recorded either way.

### 5. `<use>` inside `clipPath` requires a structural change, not an additive one

PR 4's design promised the clip-child walk would be "structured as a small
dispatch function so PR 5 can slot `<use>` in additively." **It was not** —
`buildClipChild` is a single linear function that calls `shapePath` and builds
one `ClipPathChild`.

Worse, `ClipPathChild` holds exactly **one** path, while a `<use>` in a clipPath
resolves to a subtree that may contribute several. So this needs either
`buildClipChild` returning a slice, or a nested-children variant on
`ClipPathChild`. That is genuine restructuring and is scoped into this PR
explicitly rather than discovered mid-implementation.

## Architecture

**`<use>`** resolves at build time into a `Group` whose `M` is
`translate(x,y) ∘ transform`, containing `buildNode(target, useSiteStyle, ctx)`.
The re-entrant call is the same shape the pattern-tile and mask-content code
already use — except it threads the *use site's* resolved style rather than
`defaultStyle()`.

**`<symbol>`** maps onto a `Group` with `M = viewBoxMatrix(...)`, sized by the
`<use>`'s `width`/`height`. Its default `overflow: hidden` needs a viewport clip;
a synthesized rect `ClipPath` works with existing machinery, at the cost of an
offscreen group per symbol. A cheaper viewport-rect fast path on `Group` is worth
considering during implementation.

Both `<symbol>` and `<marker>` **move from `unsupportedElements` to
`skippedElements`** — they render nothing where they appear, but they are no
longer unsupported, so the "not yet supported" log must stop.

**`<marker>`** needs machinery that does not exist yet:

- **A vertex+tangent extractor** over `*render.Path`. Nothing computes tangents
  today. `Path.Bounds` already solves cubic derivative roots for axis extrema, so
  the derivative math exists but returns only `t` values. Degenerate cases matter:
  coincident control points need documented fallbacks, and `Close` contributes
  both a vertex and the start vertex's in-tangent.
- **Bisector angles** at mid vertices — `orient="auto"` there uses the bisector of
  the in- and out-tangents, not either alone.
- **An angle parser** for `orient` (`deg`/`grad`/`rad`/`turn`). None exists;
  `parseTransform` hardcodes degrees.
- `markerUnits` (`strokeWidth` default vs `userSpaceOnUse`), `refX`/`refY`,
  `markerWidth`/`markerHeight`, marker `viewBox`, and **clip-to-viewport by
  default** (the opposite of most SVG elements).
- A `buildingMarker` recursion guard.

**Markers apply only to `path`, `line`, `polyline`, `polygon`.** The corpus
asserts the negatives (`marker-on-circle`, `-rect`, `-rounded-rect`), and since
`shapePath` has already flattened everything to a `*render.Path` by then, this
needs a separate element-name set.

**Property plumbing** for `marker-start`/`-mid`/`-end`/`marker` follows the
`clip-path` template exactly: `hints.go` list + `style.go` applier + a resolved
field on the scene node. One difference: markers **are inherited**, so they skip
the non-inherited reset that `clip-path` and `mask` perform.

The `hints.go`/`style.go` sync is comment-enforced with no automated test —
adding a property to one list and not the other fails silently. Adding four
properties at once is exactly when that bites, so this PR adds a test asserting
the two lists agree.

## Out of scope

- Nested `<svg>` as a `<use>` target (7 fixtures, decision 3).
- `<text>` in markers or as a marker target (PR 6); `<image>` in a marker (PR 8).
- External-document references (`href="../other.svg#id"`).

## Testing

- `<use>` style inheritance: the target's own attributes beat the use site's
  inherited ones, but the use site's reach through where the target is silent.
- Both cycle shapes terminate: href-chain and tree recursion, including a `<use>`
  targeting its own ancestor.
- `<symbol>` viewport mapping, `overflow` clipping, and the four `opacity-on-*`
  fixtures (good regression value for PR 4's group compositing).
- Marker tangents at every degenerate case the corpus names, bisectors at mid
  vertices, all `orient` unit forms, and the negative applicability set.
- Vendor the deferred fixtures owed from earlier PRs (4 `<use>`, 3 `<marker>`).
- Every golden compared against **resvg's reference PNGs**, not just eyeballed —
  that sweep found three bugs in the previous PR.
- No pre-existing golden may move.
