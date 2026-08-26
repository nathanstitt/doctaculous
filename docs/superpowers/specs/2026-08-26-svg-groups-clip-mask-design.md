# SVG Groups, Clip, and Mask (PR 4 of 8) — Design

**Date:** 2026-08-26
**Status:** Approved design (autonomous), pending implementation
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-groups-clip-mask`, stacked on `feat/shader-describe` (PR #105)

## Goal

Three related features that all need the same missing primitive — offscreen
composition:

- **Group opacity** — `<g opacity="0.5">` currently logs a warn-once and is
  silently ignored, because per-paint alpha through children would double-darken
  every overlap.
- **`clip-path`** — clip an element to the union of a `<clipPath>`'s children.
- **`mask`** — modulate an element's alpha by the luminance of a `<mask>`'s
  rendered content.

## The missing primitive

`render.Device` has ten methods and no grouping concept. The PR-1 spec promised a
`BeginGroup`/`EndGroup` pair and it was never built; this PR builds it. Every
paint method in the raster backend already funnels through one `d.img` field and
one `activeClip()` accessor, so the addition is a buffer swap rather than a
rewrite.

```go
// on render.Device, between PushClip and Save:
BeginGroup()
EndGroup(alpha float64, blendMode string, mask GroupMask)
```

`GroupMask` is nil for a plain opacity group, or carries a per-pixel alpha map
for a clip union or a luminance mask. Six implementations must gain the methods:
two real backends, one null device, and three test doubles.

## Decisions taken (autonomous, with rationale)

### 1. The clipPath union MUST flatten to a single mask — not repeated PushClip

This is the sharpest correctness question in the PR. `Device.PushClip` takes one
path and **intersects**; a `<clipPath>`'s children form a **union**. Two
non-overlapping circles pushed separately would intersect to *empty*.

Three designs were weighed:

- **(a) Rasterize each child with its own rule, then `max()` the coverage masks.**
  Exact. Handles mixed `clip-rule` (a nonzero child beside an evenodd one) and
  overlapping children correctly. Reuses both existing rasterizers.
- **(b) A real path boolean union.** Correct and vector-preserving in both
  backends, but `pkg/render/path.go` has no boolean ops and no exported
  flattening. A whole new subsystem — not proportionate here.
- **(c) Concatenate the children into one path and force nonzero.** Cheap and
  vector-preserving, but wrong for mixed `clip-rule` and for overlapping children
  with opposing winding, which the corpus tests directly.

**Chosen: (a) for raster.** pdfwrite takes (c) as a documented, logged
approximation when the clip is expressible that way, and otherwise falls back to
a soft mask. Naive concatenation is never used where the corpus proves it wrong.

### 2. Mask luminance uses sRGB coefficients on sRGB values by default

SVG 1.1 specifies linearRGB luminance. Browsers, SVG2, and resvg all default to
sRGB, and the corpus makes this explicit by shipping `color-interpolation=linearRGB.svg`
as an *opt-in* fixture — which only makes sense if sRGB is the default. Following
the spec letter here would make every mask golden visibly wrong.

`lum = 0.2126·R + 0.7152·G + 0.0722·B` on sRGB values, multiplied by the pixel's
own alpha. Reading `color-interpolation` to switch to the linearized path is
in scope if cheap, deferred with a log if not.

### 3. pdfwrite gets `/ExtGState` — and that fixes a live bug on the way

The writer emits no ExtGState at all today, which means **`Fill` discards
`Color.A` entirely**: a 50%-alpha SVG fill renders fully opaque in PDF output.
That is a real, currently-shipping bug independent of groups, and the resource
plumbing this PR needs to add fixes it. It lands as its own commit ahead of the
group work.

Then: Form XObjects with `/Group << /S /Transparency >>` for groups, and
`/SMask << /S /Luminosity /G ... /BC [0] >>` for masks. The `/BC [0]` black
backdrop is mandatory — without it the area outside the mask is undefined where
SVG requires transparent.

**The Y-flip trap:** the device emits top-left/Y-down coordinates and the page
assembler prepends one flip matrix. A Form XObject's `/BBox` lives inside that
already-flipped space, so BBox coordinates must be computed in the raw space the
device emits. Getting this wrong lands forms mirrored.

### 4. Lift the alpha-gradient rasterization fallback

The shader PR falls back to rasterizing any gradient with a transparent stop,
because PDF `/Shading` has no alpha channel. With soft masks available, that
lifts: emit the color shading in `/DeviceRGB` and a parallel alpha shading in
`/DeviceGray` sharing identical geometry, wrapped in a luminosity soft mask. The
existing dictionary builder needs no change for the color half.

This compounds with group opacity: a gradient under `<g opacity>` currently
forces the raster path through `alphaShader`, so both fixes together turn a
common real-world case vector.

**Scope guard:** this lifts only the *alpha* half. `reflect`/`repeat` spreads
still rasterize — `/Extend` cannot express them. FEATURES.md must not over-claim.

### 5. Teach `pkg/pdf/content` soft masks

The engine's own PDF *reader* ignores ExtGState soft masks and flags them
unsupported. That matters beyond fidelity: the SVG→PDF→reopen→raster equivalence
test — the technique that proved the shading dictionaries correct — would become
structural-only for every mask this PR emits. Teaching the reader keeps the
strongest verification technique available for masks and for PR 7's filters.

If this proves larger than expected, it may be split into its own PR; the
decision and reason get recorded either way.

## Scene-graph changes

`svg.Group` has only `M` and `Kids` — no style at all, which is why group
opacity is dropped. It gains `Opacity float64`, plus resolved clip and mask
references. Not a whole `Style`: that type is shape-paint-shaped and would be
misleading on a container.

`clip-path`, `mask`, `clip-rule`, and `mask-type` must be added to **both**
`hints.go`'s presentation-attribute list and `style.go`'s appliers. Those two
lists carry an explicit in-code warning that they must stay in sync; adding to
one only would make the property work from CSS and silently not from an
attribute.

Clip and mask resolve **during `Parse`**, like paint servers, because `docIndex`
is discarded when `Parse` returns and `Document` must stay lock-free-shareable.
The existing `fragmentID`, `docIndex.ids`/`defs`, `followHrefChain`, and the
`buildingPattern` recursion-guard shape all generalize; `docIndex.defs` was
explicitly built for a consumer like `clipPath`.

`<clipPath>` accepts only shapes, `<text>`, and `<use>` as children — the
forgiving-container default would happily recurse into a `<g>`, so the clip walk
needs its own explicit allowlist, structured as a small dispatch function so PR 5
can slot `<use>` in additively.

## Out of scope

- `<use>` inside a clipPath (3 fixtures) — PR 5.
- `<image>` inside a mask (2 fixtures) — PR 8.
- Text in a clipPath (4 fixtures) — PR 6.
- Markers on a clip (1 fixture) — PR 5.
- The deprecated SVG 1.1 `clip` property (2 fixtures) — obsolete, skipped with a note.
- `reflect`/`repeat` spreads in PDF (unchanged).

## Testing

- Group opacity composites once, not per child: two overlapping opaque shapes in
  a 50%-opacity group must show **no seam** at the overlap. That is the exact
  artifact per-paint alpha produces, so it is the discriminating test.
- clipPath union: two non-overlapping circles clip to both (the case repeated
  `PushClip` would render empty); mixed `clip-rule` children; overlapping
  children under evenodd.
- Mask luminance against known values; `mask-type=alpha`; nested masks.
- Recursion: self-referencing clipPath and mask terminate.
- PDF: fill alpha now survives (the pre-existing bug); groups emit transparency
  Form XObjects; masks emit luminosity soft masks; alpha gradients emit vector.
- SVG→PDF→reopen→raster equivalence, if the reader work lands.
- ~78 curated fixtures from resvg's 93-file `masking/**` tranche, every golden
  visually inspected. No pre-existing golden may move.
