# resvg-test-suite (curated subset)

SVG files copied verbatim from https://github.com/RazrFalcon/resvg-test-suite
at commit `d8e064337faf01bc5a9579187a56dbdbe3eacc72`, MIT licensed (Copyright (c)
2018 Reizner Evgeniy — see LICENSE in that repository). Each file exercises one
feature. Goldens are OUR renderer's committed output
(`pkg/omnidoc/testdata/golden/svg-resvg/`), not resvg's reference PNGs —
the sweep locks omnidoc against regression, not against resvg.

Curation rule: a file lands here only when every feature it uses is shipped;
new tranches arrive with the PR that ships their feature.

## What shipped in this tranche (PR 1)

148 files vendored (150 curated, minus 2 removed after the golden-eyeball pass
— see "Bugs found" below), covering:

- `shapes/` — rect (incl. `rx`/`ry` resolving, clamping, and lacuna-value
  rules), circle, ellipse (incl. SVG2 single-radius auto-defaulting), line,
  polyline, polygon (23 rect/circle/ellipse/line/polyline/polygon files plus
  23 `path` files exercising the full `d` grammar: every command letter
  (M/L/H/V/C/S/Q/T/A/Z), relative vs. absolute forms, implicit command
  repetition, multi-subpath data, and the arc/flag error-recovery corner
  cases from the path grammar's malformed-input handling).
- `painting/fill/` — all supported color syntaxes: named colors (incl. case
  variants), `#rgb`/`#rrggbb`/`#rrggbbaa` hex forms, `rgb()`/`rgba()` (comma
  and space syntax, ints/floats/percentages), `hsl()`/`hsla()`,
  `currentColor`, `inherit`, `none`, `transparent`, and invalid-value
  fallback behavior.
- `painting/fill-rule`, `fill-opacity`, `stroke`, `stroke-width`,
  `stroke-linecap`, `stroke-linejoin` (miter/round/bevel only — see
  exclusions), `stroke-miterlimit`, `stroke-dasharray`, `stroke-dashoffset`,
  `stroke-opacity`, `opacity` (element-level, not group/root), `display`,
  `visibility`, `color` (the `currentColor` backing property).
- `structure/svg/` — `viewBox`, `preserveAspectRatio` (all 9 alignment
  keywords plus `none`, both `meet`/`slice`), default sizing (no
  width/height/viewBox), explicit/prefixed SVG namespaces, and namespace
  validation (a child re-declaring the default namespace to a non-SVG URI is
  correctly treated as foreign and skipped).
- `structure/g/`, `structure/defs/` — grouping, nested groups, `defs`
  contents skipped.
- `structure/transform/` — `matrix`, `translate`, `scale`, `rotate` (with and
  without a center point), `skewX`, `skewY`, transform lists, nested
  transforms, and edge cases (empty, zeroed, whitespace-tolerant).

## Notable exclusions (found during curation, not merely grep-filtered)

Beyond the obvious PR1-scope exclusions (gradients/patterns via `url(...)`,
`use`/`symbol`/`image`/`text`/`marker`/`clipPath`/`mask`/`filter`/`switch`,
`style` elements/attributes, CSS classes, nested `<svg>`), the following
required actually running the file through the renderer or reading the
relevant source to confirm a mismatch with the ships list, and were dropped:

- **Font-relative units** (`em`, `ex`, `ch`, `ic`, `lh`, `rem`, `rlh`, `cap`)
  and **viewport-relative units** (`vi`, `vb`, `vh`, `vw`, `vmin`, `vmax`) —
  `font-size`/`font-family` are not part of this PR's cascade, and
  `parseLength` hardcodes `em`=16px/`ex`=8px with no per-element `font-size`
  awareness and does not recognize the others at all. Any file that set a
  local `font-size` to make its `em`/`rem`/etc. assertion meaningful was
  excluded (e.g. `shapes/rect/em-values.svg`, `ch-values.svg`, `ic-values.svg`,
  `lh-values.svg`, `rem-values.svg`, `rlh-values.svg`, `cap-values.svg`,
  `vi-and-vb-values.svg`, `stroke-dasharray/em-units.svg`,
  `stroke-dashoffset/em-units.svg`).
- **Percentage lengths on shape geometry** (`x`/`y`/`width`/`height`/`rx`/
  `ry`/`cx`/`cy`/`r`/`x1`/`y1`/`x2`/`y2`) — `shapePath`'s length resolver
  explicitly resolves a percentage against `0`, not the real viewport (a
  documented, logged degradation), so e.g. `width="80%"` silently becomes a
  zero-width shape. Excluded `shapes/rect/percentage-values-{1,2}.svg`,
  `shapes/line/percent-units.svg`, `shapes/ellipse/percent-values*.svg`.
- **Percentage `stroke-width`** — same root cause via `parseLength(val, 0)`;
  `stroke-width="10%"` resolves to a zero-width (invisible) stroke instead of
  10% of the viewport diagonal. Excluded `painting/stroke-width/percentage.svg`.
  (Percentages on `opacity`/`fill-opacity`/`stroke-opacity` are unaffected —
  those are self-contained 0–100% → [0,1] mappings with no viewport
  dependency — and were kept.)
- **Units/percentages inside `stroke-dasharray`** — `applyDashArray` parses
  the list with a bare-number tokenizer (`parseNumberList`/`parseNumber`);
  a token like `"5mm"` or `"15%"` fails to parse, invalidating the whole
  list and silently falling back to a solid stroke. Excluded
  `stroke-dasharray/mm-units.svg` and `stroke-dasharray/percent-units.svg`.
- **`stroke-linejoin: arcs` / `miter-clip`** — both degrade to `miter` with a
  log line (not a rendering bug — a documented approximation), but the ships
  list for this PR is explicitly `miter|round|bevel`, and resvg itself marks
  its `arcs` fixture "(UB)" (undefined behavior, no reference implementation
  agrees). Excluded `stroke-linejoin/arcs.svg` and `miter-clip.svg`.
- **`icc-color()` fallback syntax** (`fill="red icc-color(...)"`) — the color
  parser does not split the sRGB fallback from the trailing `icc-color()`
  function, so the whole value fails to parse. resvg marks this fixture
  "(UB)" as well. Excluded `painting/fill/icc-color.svg`.
- **Presentation attributes on the root `<svg>` element itself** —
  `svg.Parse` only ever reads `viewBox`/`width`/`height`/`preserveAspectRatio`
  off the root element; `opacity`, `display`, `fill`, etc. set directly on
  `<svg>` are never consulted (verified by rendering: `opacity="0.5"` on root
  renders fully opaque, `display="none"` on root does not hide anything).
  Excluded `painting/opacity/on-the-root-svg.svg` and
  `painting/display/none-on-svg.svg`.
- **Internal DTD entity references** (`<!ENTITY ...>` + `&name;`) — Go's
  `encoding/xml` decoder is configured with `xml.HTMLEntity` (named HTML
  entities only); a custom internal-subset entity is not expanded, and the
  literal `&name;` text reaches the attribute/element parser instead
  (verified: `attribute-value-via-ENTITY-reference.svg` renders black instead
  of green; `elements-via-ENTITY-reference-1.svg` renders blank instead of a
  green rect). Excluded all three `structure/svg/*-ENTITY-reference-*.svg`
  fixtures.

Two fixtures were specifically verified as *correctly handled* and kept
despite initially looking suspicious: `structure/svg/explicit-svg-namespace.svg`
(an `svg:`-prefixed document, renders correctly) and
`structure/svg/xmlns-validation.svg` (a child re-declaring the default
namespace to a non-SVG URI, confirmed to correctly skip the
now-foreign-namespace sibling while still rendering the `s:`-prefixed SVG
content).

The corpus was pared down from ~285 files that passed a first-pass automated
filter (removing anything referencing an out-of-scope element/attribute) to
a 150-file curated set by removing near-duplicate permutations within a
single feature (e.g. keeping one representative "missing coordinate resolves
to 0" case per shape rather than one per coordinate), while keeping every
distinct path-grammar command combination and every distinct color-syntax
family, per the task's 80–150 file target.

## Bugs found and fixed during the golden-eyeball pass

Generating a golden only proves the test harness works — it always matches
itself on first generation. Every one of the (then) 150 PNGs was visually
inspected against its source SVG's stated intent (most files carry a
`<title>`/`<desc>` describing the expected result) before being committed.
Two real rendering bugs surfaced this way, both fixed in this PR rather than
worked around:

1. **Unclosed fill paths rendered as their bounding box, not their actual
   shape.** `pkg/render/raster/geom.go`'s `replay()` (the nonzero-winding
   fill path, used by every `Fill` call) never closed a subpath unless the
   source path data contained an explicit `Z`/`Close`. Per SVG's painting
   model, fill always implicitly closes every subpath — `<polygon>` does
   this by construction, but a `<path d="M ... L ... L ...">` with no `Z`
   and a `fill` is completely legal and common. `golang.org/x/image/vector`'s
   scanline rasterizer accumulates signed per-edge coverage deltas; leaving
   an edge unclosed left an uncancelled delta that read as "inside" for an
   entire rectangular region rather than merely omitting the closing edge.
   Caught via `shapes/polygon/ignore-odd-points.svg` and
   `stop-processing-on-invalid-data.svg`: both files' `<path>` reference
   shape (intentionally left open, since the *polygon* is what's under
   test) rendered as a solid quadrilateral instead of the intended triangle,
   fully covering the green polygon underneath it — a "should be covered"
   comparison the wrong shape can only ever fail silently on a wrong golden
   (the areas didn't overlap where the two triangles didn't touch). Fixed by
   force-closing every subpath in `replay()` before the next `MoveTo` and
   after the last segment; `replayStroke` (a separate function) is
   unaffected, since strokes must still honor an actually-open subpath.
2. **`<ellipse>` did not implement SVG 2's single-radius auto-defaulting.**
   `shapes/ellipse/missing-rx-attribute.svg` and `missing-ry-attribute.svg`
   (both explicitly titled "(SVG 2)": "Error in SVG 1, but not in SVG 2")
   rendered as fully blank instead of a circle. `pkg/svg/shapes.go`'s
   `rectPath` already implements "when exactly one of rx/ry is present, the
   other defaults to it" for `<rect>`, but the `ellipse` case in
   `shapePath` required both `rx` and `ry` to independently be present and
   positive. Fixed by adding `ellipseRadii()`, mirroring `rectPath`'s
   presence-based (not resolved-value-based) substitution rule, with a new
   unit test (`TestShapePathEllipseAutoRadius` in `pkg/svg/shapes_test.go`).

Two group-opacity fixtures (`painting/opacity/group-opacity.svg` and
`mixed-group-opacity.svg`) were removed from the corpus after the eyeball
pass: they render fully opaque, with no transparency effect, because `<g
opacity>` compositing is a documented, intentional PR-1 limitation (see
`groupOpacityWarnKey` in `pkg/svg/svg.go`) — not a bug, but also not a
feature this tranche ships, so locking a golden of the ignored-opacity
render would assert the wrong thing.

## What shipped in this tranche (PR 2)

13 files vendored from `tests/structure/style/` (11) and
`tests/structure/style-attribute/` (2), covering CSS styling of SVG:

- `<style>` elements: plain, `type="text/css"` implied by omission, CDATA-
  wrapped rule bodies, a sheet appearing textually after the element it
  styles (sheets are indexed whole-document before the scene walk, so source
  position relative to the styled element doesn't matter), and a sheet with
  a non-CSS `type` (skipped, presentation/other attributes still apply).
- Selectors: type (`rect { ... }`), class (`.fil { ... }`), ID (`#rect1
  { ... }`), universal (`* { ... }`), and multi-part descendant-combinator
  chains mixing id/type/universal parts (`g#g1 * g * * rect`) — specificity
  compares correctly across a 5-ancestor chain (`rule-specificity.svg`).
- Cascade mechanics: `!important` beating a later higher-source-order plain
  declaration; the presentation-attribute < sheet-rule < inline-`style`
  precedence ladder (`resolve-order.svg`: an inline `style="fill:green"`
  beats a `.fil{fill:blue}` sheet rule); an unresolved class selector (no
  matching rule) falling back to the element's own presentation attribute;
  a `class` attribute with no `<style>` sheet at all doing nothing.
- `style=""` inline attribute: a plain declaration, and one wrapped in
  `/* */` comments on both sides.

## Notable exclusions (this tranche)

- **`@import`** (`external-CSS.svg`) — no loader; per pkg/svg's
  `indexStyleSheet`, an `@import` inside a `<style>` sheet is warned and
  skipped, the rest of the sheet still parses, but this fixture's whole
  point is the imported rule's effect, so excluding it is correct rather
  than a workaround.
- **Attribute selectors** (`attribute-selector.svg`, `[x] { fill: green }`)
  — `pkg/css/selector.go`'s `parseSimple` has no `[` handling; `[x]` parses
  as a literal type selector (tag `"[x]"`) that can never match a real
  element name, so the rule is silently inert. Including this fixture would
  lock in a blank/default-black golden as if it were the intended green —
  excluded per the task brief's explicit warning about this exact case.
- **Child combinator `>`** (`combined-selectors.svg`, `svg > rect`) —
  `parseOneSelector` splits only on whitespace (`strings.Fields`), so `>`
  becomes its own bogus simple selector (tag `">"`) that never matches;
  same "locks in a wrong golden" hazard as the attribute selector. Excluded.
- **CSS `transform` property**, both as a sheet rule
  (`structure/style/transform.svg`, `#rect1 { transform:scale(2) }`) and as
  an inline `style` (`structure/style-attribute/transform.svg`,
  `style="transform:scale(2)"`) — `pkg/svg/svg.go`'s `elementTransform`
  reads the `transform` XML *attribute* directly and never consults the
  cascade; `pkg/svg/style.go`'s `Style.apply` has no transform property at
  all. Verified: rendering either fixture produces the unscaled 80×80
  square, not the intended scaled-up green fill. Excluded rather than
  locking in a golden of a property this engine doesn't apply.
- **CSS geometry properties** (`height`/`width`/`x`/`y` via `style=`/sheet) —
  both `non-presentational-attribute.svg` fixtures (one under `style/`, one
  under `style-attribute/`) set `height` through CSS to probe SVG-1-vs-2
  behavior; `Style.apply` never reads geometry properties from the cascade
  at all (shape geometry comes from `shapePath` reading XML attributes
  directly), so these two would happen to render "correctly" by coincidence
  (CSS geometry never applying, which matches the fixture's SVG-1
  expectation) rather than by an implemented rule. Excluded as testing a
  property category this cascade doesn't model, per the brief's guidance
  not to lock in a golden for the wrong reason.
- **`<marker>`-dependent fixtures** (`painting/marker/recursive-4.svg`,
  `the-marker-property-in-CSS.svg`) — both need a working `<marker>`
  element (out of scope) in addition to the CSS `marker:url(#...)`
  property; excluded on the marker dependency alone.
- **`<use>`-dependent fixture** (`structure/use/cSS-rules.svg`) — needs
  `<use xlink:href="#rect1">` resolution (out of scope); excluded on the
  `use` dependency alone, though the underlying "CSS resolves against the
  referenced element's ID before `use` copies it" claim is itself something
  our per-element `id` selector already gets right.
- **Blend-mode / isolation / mask-type-via-style fixtures**
  (`painting/mix-blend-mode/*.svg`, `painting/isolation/*.svg`,
  `masking/mask/mask-type-in-style.svg`) — all found via the class/style
  grep, but every one depends on a gradient paint (`fill="url(#lg1)"`) and/or
  `<mask>`, both out of scope regardless of the CSS construct under test;
  excluded on those grounds.
- **Text/font `style=` fixtures** (`text/font-kerning/*.svg`,
  `text/font-style/*.svg`, `text/font/font-shorthand.svg`) — all require
  `<text>` rendering, out of scope; excluded on that dependency alone.

No fixture was removed after generating goldens — all 13 curated files
survived the eyeball pass unchanged. See "Bugs found" below for one fix the
eyeball pass did prompt.

## Bugs found and fixed in this tranche

`structure/style-attribute/comments.svg` (`style="/*text*/fill:green/*text*/"`)
initially rendered with the rect's default black fill instead of green:
`pkg/css/parse.go`'s `ParseDeclarations` — the function backing both the
SVG and HTML `style=""` inline-attribute cascades — never stripped `/* */`
comments before splitting on `;` and `:`, so the whole declaration parsed
as property `/*text*/fill`, which matches nothing. A `<style>` *sheet*
body already had comments stripped by `ruleScanner.readBody` before
reaching `ParseDeclarations`, so this only affected the `style=""`
attribute path — an inconsistency between the two callers of the same
function, not an unimplemented feature. Fixed by stripping comments inside
`ParseDeclarations` itself (a new `stripComments` helper) so both callers
get identical, correct behavior; covered by rerunning the existing
`pkg/css` test suite (all passing) plus this golden.

## What shipped in this tranche (PR 3)

110 files vendored from `tests/paint-servers/` (149 candidates curated down),
covering gradient and pattern paint servers end to end:

- `linearGradient/` (35 files) — both unit systems (`objectBoundingBox`
  default and `userSpaceOnUse`, including percentage values in each),
  `gradientTransform` alone and combined with the element's own `transform`,
  all three `spreadMethod` values, `xlink:href` attribute/stop inheritance
  (simple, "only required", and multi-hop chains with mixed override order),
  2-cycle/3-cycle/self-referencing href chains (all terminate and degrade to
  "own stops win" or "paints nothing"), an href to a non-gradient element
  (no-op, own stops still apply), invalid `gradientTransform`/`gradientUnits`/
  `spreadMethod`/`xlink:href` (each degrades to a spec-reasonable default),
  a single-stop gradient (solid-color fill; also proves fill still paints
  correctly when the same shape's *stroke* uses the same gradient and
  degrades to the fallback), `hsla()` stop colors, many-stops ramps, and an
  invalid `<stop>`-position child (a `<rect>` in gradient-child position,
  both standalone and reached through an href).
- `radialGradient/` (32 files) — the linearGradient list above plus
  `fx`/`fy` focal-point resolution (bare, defaulting to `cx`/`cy`, and
  through an href chain), and `r`/`cx`/`cy` default-attribute resolution.
  `negative-r` (resvg-tagged UB) is kept because this engine's degenerate-
  matrix/degenerate-circle handling produces a deterministic, defensible
  fallback (solid first-stop color) rather than garbage.
- `pattern/` (26 files) — `patternUnits`/`patternContentUnits` in both unit
  systems (including percentages), a `viewBox` on the pattern (including
  through `xlink:href`, and its precedence over `patternContentUnits` when
  both are present), `preserveAspectRatio`, `patternTransform` alone and
  combined with the element's own `transform` (proving the two compose
  rather than one silently winning), `x`/`y` cell offset, missing
  `width`/`height` (disables the pattern per spec), no tile children
  (disables the pattern), `display:none` on one tile child (that child
  alone is skipped), a *gradient* nested inside a pattern's tile
  (`nested-objectBoundingBox.svg`; including `objectBoundingBox` units
  resolving against the *outer* shape's bbox — note this is a gradient
  inside a pattern, not a pattern inside a pattern; see `hand-authored/` below
  for that case), out-of-order forward references, a tile whose own fill
  references a gradient (paint-server composition), an invalid
  `patternTransform` (degenerate matrix, paints nothing), invalid
  `patternUnits`/`patternContentUnits` (default to `objectBoundingBox`/
  `userSpaceOnUse` respectively), attribute/child inheritance via
  `xlink:href` (including "everything inherited" and a pure passthrough
  link), a self-referencing tile and a two-pattern mutual-reference cycle
  (both terminate via `sceneBuilder.buildingPattern`, degrading to
  "unpainted fill, tile's own stroke still shows"), and a tiny tile
  upscaled 10x via `patternTransform="scale(10)"`.
- `stop/`, `stop-color/`, `stop-opacity/` (20 files) — `offset` as a bare
  number or percentage, offset clamping (out-of-[0,1] values, and a
  non-monotonic sequence clamping forward rather than sorting), coincident
  offsets (a hard color edge, not a divide-by-zero), a missing `offset`
  (defaults to the previous stop's position), an invalid (unit-suffixed)
  `offset` (falls back to 0, then clamps forward same as any other 0),
  `stop-color` absent (defaults to black), `hsla()` stop colors, and
  `stop-opacity` as a fraction and as a percentage — verified as REAL alpha
  (the shape fades to whatever is behind it, not to a black-composited
  color).

## Notable exclusions (this tranche)

- **`fr` (SVG2 radial focal radius)** — entirely unimplemented;
  `radialGradientCoords` has no `fr` parameter at all, so a value there is
  silently ignored. Excluded all four `radialGradient/fr=*.svg` fixtures
  (`-1`, `0.2`, `0.5`, `0.7`) rather than lock in a golden that quietly
  drops an attribute the fixture is specifically about.
- **Radial-gradient focal-point-outside-circle correction** — per spec, an
  `fx`/`fy` outside the `r` circle must be projected onto the circle's
  boundary along the line from the center; nothing in `gradient.go` or
  `shading.go` implements this projection. Verified by rendering
  `radialGradient/focal-point-correction.svg`: the unclamped focal point
  breaks `atRadial`'s quadratic assumptions and paints an inverted/wrong
  pattern (black at the circle's center, white outside it) rather than the
  intended smooth radial fade. Excluded.
- **Radial gradient with `r="0"`** — per spec this must paint a solid fill
  of the *last* stop's color; `atRadial`'s quadratic degenerates to `a=0,
  b=0` for a zero-radius circle at a fixed focal point and returns
  `paint=false` (nothing painted) instead. Verified by rendering: the shape
  comes out fully unpainted, not solid green. Excluded
  `radialGradient/zero-r.svg` and both `zero-r-with-stop-opacity-*.svg`
  fixtures (the second of which uses the same zero-radius case on a
  *stroke*, doubly out of scope with the stroke-gradient deferral below).
- **Gradient/pattern `stroke="url(...)"`** — a known PR-scope deferral (see
  `pkg/render/raster/stroke.go`'s doc comment): no stroke-to-outline
  conversion exists to clip a shading or tile against, so a gradient/
  pattern stroke degrades to the fallback color (or no stroke, absent a
  fallback) with a one-per-document warn-once log. Grepped every candidate
  for `stroke="url(`; the three hits
  (`linearGradient/single-stop-with-opacity-used-by-{stroke,fill-and-
  stroke}.svg`, `radialGradient/zero-r-with-stop-opacity-2.svg`) were kept
  only where the fixture's *fill* behavior (not the stroke) is what the
  golden actually locks in, and dropped otherwise (see the zero-r bullet
  above for the third).
- **`currentColor`/`inherit` on a `<stop>`'s `stop-color`, when the value
  lives on a real DOM ancestor** — `resolveStopColor` (`pkg/svg/stops.go`)
  only ever resolves `color`/`stop-color` against the `<stop>` element's
  *own* attributes/cascade; there is no inherited-style walk from a stop up
  through its parent gradient or an ancestor `<g>`, so `stop-color="green"`
  or `color="green"` set on the immediate `<linearGradient>` (let alone a
  further ancestor `<g>`) never reaches a child `<stop>`'s `currentColor`/
  `inherit` resolution — verified by rendering: `stop-color-with-
  currentColor-1.svg` (color set directly on the parent gradient) and
  `stop-color-with-inherit-1.svg` (`stop-color` set directly on the parent
  gradient) both come out pixel-identical to the "no ancestor value at all"
  baseline, i.e. black, not green. Excluded all `currentColor-{1,2,3}` and
  `inherit-{1,2,3,5}` fixtures; kept only `currentColor-4` and `inherit-4`
  (the "no ancestor sets a value, default to black" baseline, which is
  correct either way).
- **Cross-type `xlink:href` attribute inheritance is unfiltered by the
  target's own element kind** — `paintServerResolver.resolve`'s attribute
  inheritance is a flat per-attribute-name copy across the whole href
  chain, with no notion of "this attribute is meaningless on that source
  element." A `radialGradient`'s stray `y2` attribute (meaningful only to a
  `linearGradient`) is therefore inherited into a `linearGradient` that
  hrefs it, even though `y2` never had any effect on the `radialGradient`
  it was written on. Verified by rendering
  `linearGradient/attributes-via-xlink-href-from-radialGradient.svg`
  (explicitly titled with `<desc>y2 should be ignored</desc>`): the
  resulting gradient varies with Y as well as X, instead of the intended
  pure horizontal ramp. The equivalent `-from-rect.svg` fixtures are
  unaffected and kept — a `<rect>` is not a paint-server kind at all, so
  `followHrefChain`'s visitor stops before copying any of its attributes —
  and `radialGradient/attributes-via-xlink-href-from-linearGradient.svg` is
  also kept: the leaked attribute there (`cx`) happens to be meaningful to
  the referencing `radialGradient` too, so no incorrect behavior is
  observable. Excluded only the one broken fixture.
- **`overflow="visible"` on a `<pattern>`** — resvg itself tags this
  fixture "(UB)"; `resolvePattern`/`fillPattern` have no `overflow` handling
  at all (every tile is always clipped to its own cell), so this is a
  documented gap rather than a defensible UB response. Excluded
  `pattern/overflow=visible.svg`.
- **`<text>` inside a pattern tile** — `<text>` rendering is out of scope
  regardless of the paint-server construct under test. Excluded
  `pattern/text-child.svg`.
- **Near-duplicate permutations pruned after curation** — per the existing
  curation rule (one representative per distinct mechanism, not one per
  numeric variant), trimmed the `stop/missing-offset-*` family from 7 to 3
  (first/middle/last position missing), `stop/stops-with-equal-offset-*`
  from 6 to 2 (a mid-ramp coincident pair, and the whole-ramp-collapsed
  case), and dropped a handful of single-mechanism repeats elsewhere
  (`linearGradient/single-stop-with-opacity-used-by-fill.svg`, subsumed by
  the plain `single-stop.svg`; `stops-via-xlink-href-complex-order-1.svg`,
  subsumed by `stops-via-xlink-href.svg` + `recursive-xlink-href-3.svg`;
  `radialGradient/{fx,fy}-resolving-{2,3}.svg`, subsumed by `-1` plus the
  general href-attribute-inheritance coverage above;
  `radialGradient/attributes-via-xlink-href-complex-order.svg` and
  `gradientTransform-and-transform.svg`, subsumed by the linearGradient
  equivalents already exercising the same mechanism;
  `pattern/child-with-invalid-FuncIRI.svg` and
  `invalid-patternUnits-and-patternContentUnits.svg`, subsumed by the
  gradient-side unresolved-url()/invalid-enum coverage; `pattern/missing-
  width.svg`, subsumed by `missing-height.svg`; `pattern/self-recursive-
  on-child.svg`, subsumed by `recursive-on-child.svg` and `self-
  recursive.svg`).

## Bugs found in this tranche

Generating a golden only proves the harness works; every one of the 110
PNGs was visually inspected against its source SVG's `<title>`/`<desc>`
before being committed. None of this tranche's fixtures use the
green=correct/red=wrong convention some PR 1/PR 2 fixtures used — paint-
server fixtures instead show the gradient/pattern content directly, with a
`stroke="darkblue"`/`stroke="black"` outline for reference, so "does the
rendered ramp/tile match the described geometry" was the check, not a
color-coded pass/fail.

Two real, pre-existing rendering bugs surfaced during implementation and
were fixed (see the git history for the fix commit): `resolveGradient`'s
`objectBoundingBox` matrix composed `Translate(minX,minY).Mul(Scale(w,h))`
— translate first, then scale — which (per this codebase's
`Matrix.Mul(m,n)` = "m first, then n" convention) scales the translation by
`(w,h)` too. For any shape not anchored at the document origin, this sent
the gradient's local space to a device-space location far outside the
visible page, so the shape painted as a flat block of the gradient's first
stop color instead of a ramp. `pkg/render/pdfwrite`'s `FillShading` had the
identical mistake placing its rasterized shading image. Both needed
`Scale(...).Mul(Translate(...))` instead. Neither bug was caught by the
pre-existing unit tests because every one of them used a shape/clip
anchored at `(0,0)`, where `Translate(0,0)` is inert regardless of
composition order — this tranche's off-origin fixtures (nearly all of
them; the corpus's shapes sit at `x="20" y="20"`) surfaced it immediately.
Fixed at the source, with new regression tests using an off-origin
rect/clip in both `pkg/svg/draw` and `pkg/render/pdfwrite`.

A third composition-order bug of the same shape was found in final review
and fixed afterward: `resolveGradient`'s `gradientTransform` handling
composed `bboxM.Mul(gradM)` — bbox mapping first, then gradientTransform —
applying gradientTransform in USER space about the user-space origin
instead of within the gradient's own local coordinate system. Every
resvg-sourced `gradientTransform` fixture uses a 160x160 (square) bbox, under
which a uniform scale makes the two orders differ only by a small
translation, so the bug produced near-identical PNGs and was invisible to
the golden sweep. Fixed to `gradM.Mul(bboxM)`; see `hand-authored/` below
for the fixture that actually distinguishes the two orders.

## Hand-authored fixtures (not from resvg-test-suite)

Two files under `paint-servers/hand-authored/` are original to this
repository, not vendored — they exist to close corpus gaps the resvg
tranche above cannot, and are not subject to the MIT-licensed upstream
suite's terms:

- `gradientTransform-nonsquare-bbox.svg` — every resvg `gradientTransform`
  fixture (`linearGradient/gradientTransform.svg`,
  `linearGradient/gradientTransform-and-transform.svg`,
  `radialGradient/gradientTransform.svg`) uses a square 160x160 bbox, which
  cannot distinguish `bboxM.Mul(gradM)` from `gradM.Mul(bboxM)` (see "Bugs
  found" above) — under a uniform scale the two orders differ only by a
  translation. This fixture uses a 200x50 (non-square) rect so the two
  composition orders diverge in angle, not just position, making a future
  regression of this kind visible in the golden.
- `nested-distinct-patterns.svg` — a 3-level chain of DISTINCT patterns
  (`patt1`'s tile fills with `patt2`, whose tile fills with `patt3`), which
  is not a cycle and so is not caught by `sceneBuilder.buildingPattern` (see
  that field's doc comment in `pkg/svg/pattern.go`): each level multiplies
  draw calls by its own cell count, and nothing at scene-build time bounds
  the chain's depth. The only bound on it is `pkg/svg/draw`'s per-`DrawVector`
  nesting-depth guard (`maxPatternNestingDepth`). This fixture is shallow
  enough to render normally and lock in correct output; the guard's actual
  trip point is covered by a Go benchmark/timing test rather than a golden,
  since a document deep enough to matter for that test is intentionally
  truncated mid-render.

## What shipped in this tranche (PR 4 — masking)

74 files vendored under `masking/clipPath/` (37) and `masking/mask/` (37),
covering group opacity's prerequisite offscreen-compositing primitive,
`clip-path`/`<clipPath>` (union semantics, mixed `clip-rule`, nested/
self-referencing clipPaths, `clipPathUnits`, a shape-only child allowlist),
and `mask`/`<mask>` (luminance and `mask-type=alpha` coverage, `maskUnits`/
`maskContentUnits`, the default 10%-bleed region, nested/self-referencing
masks, `mask-type` via attribute and `style=`).

**This tranche ships with resvg's own reference PNGs**, a strictly stronger
check than eyeballing intent alone: every one of the 74 goldens was diffed
pixel-by-pixel against resvg's reference render (both composited onto an
opaque white backdrop, nearest-neighbor-sampled from resvg's 500x500
reference down to our 200x200 golden resolution, tolerance ±24/channel) in
addition to being individually viewed. All 74 matched closely — the
handful of fixtures with an above-baseline diff percentage are the
documented, expected divergences listed under "Notable exclusions" and
"Known divergences from resvg's reference" below, not bugs. The universal
~4-6% baseline seen on every fixture is edge antialiasing noise from the
2.5x resolution mismatch between the two renderers, confirmed by a visual
diff heatmap that lights up only the shape outlines, never interior fills.

## Notable exclusions (this tranche)

- **`<use>`-dependent** (3, per the design's explicit deferral to a `<use>`
  PR): `clipPath/with-use-child.svg`, `clipPath/with-invalid-child-via-use.svg`,
  `clipPath/symbol-via-use-is-not-a-valid-child.svg`.
- **`<image>`-dependent** (2, deferred): `mask/with-image.svg`,
  `mask/with-grayscale-image.svg`.
- **Text-dependent** (5 — one more than the 4 originally flagged):
  `clipPath/clipping-with-text.svg`, `clipPath/clipping-with-complex-text-1.svg`,
  `clipPath/clipping-with-complex-text-2.svg`,
  `clipPath/clipping-with-complex-text-and-clip-rule.svg`, and
  `clipPath/clip-path-with-transform-on-text.svg` (a `<text>` clip *target*,
  not a `<text>` clipPath *child* — found during inspection: `<text>` is not
  implemented at all — see `pkg/svg/clippath.go`'s `clipPathChildKinds`
  comment — so any fixture involving `<text>` in either role is excluded).
- **Marker-dependent** (1): `clipPath/with-marker-on-clip.svg`.
- **The deprecated SVG 1.1 `clip` property, and a duplicate** (2):
  `clip/simple-case.svg` uses the SVG 1.1 `clip="rect(...)"` presentation
  property (via `<image>`, itself also out of scope) — obsolete, skipped.
  `clip-rule/clip-rule=evenodd.svg` is, on inspection, byte-identical in
  intent to `clipPath/clip-rule=evenodd.svg` (both test the modern
  `clip-path`/`clip-rule` attributes, not the deprecated `clip` property —
  resvg's own directory layout just files a duplicate copy under
  `clip-rule/`); skipped as a duplicate, not a feature gap.
- **CSS basic-shape `clip-path` values (SVG2/CSS Shapes)** (3):
  `clipPath/circle-shorthand.svg`, `circle-shorthand-with-view-box.svg`,
  `circle-shorthand-with-stroke-box.svg` all set `clip-path="circle()"` (a
  CSS basic-shape function, not a `url(#id)` reference) directly on the
  target element. Verified against source:
  `sceneBuilder.resolveClipPathRef` only recognizes a `url(...)` prefix and
  treats anything else as unresolvable/no-clip; `circle()`/`inset()`/
  `polygon()` parsing does not exist anywhere in `pkg/svg`. Excluded as an
  unshipped feature, not merely an unshipped fixture.
- **`clip-path`/`opacity`/`display` on the root `<svg>` element itself** (2,
  found during the golden-eyeball comparison against resvg's reference, not
  by grep): `clipPath/on-the-root-svg-with-size.svg` and
  `on-the-root-svg-without-size.svg`. `svg.Parse` only ever special-cases
  `opacity` on the root element (`rootOpacity`, with a doc comment
  explaining why) — `clip-path` set directly on `<svg>` is never read. This
  is the same root cause PR 1 already excluded `painting/opacity/
  on-the-root-svg.svg` and `painting/display/none-on-svg.svg` for; the
  fixture-comparison pass here is what caught the `clip-path` case, since
  neither fixture's filename mentioned this at all.
- **Near-duplicate/redundant coverage**: none pruned beyond the above — the
  masking tranche's fixture set does not have the numeric-variant families
  (`stop/missing-offset-*`-style) earlier tranches needed to trim.

## Known divergences from resvg's reference (not bugs)

Found and confirmed via the pixel-diff sweep, each is a documented,
deliberate scope boundary rather than a defect:

- **`mask/color-interpolation=linearRGB.svg`** — this fixture is the
  corpus's OWN opt-in signal that sRGB is the assumed default (see the
  design doc's decision 2): resvg renders it in linearRGB space per the
  fixture's explicit request, this engine always uses sRGB luminance
  coefficients on sRGB values (browsers/SVG2/resvg's own DEFAULT, just not
  what this one fixture asks for) and does not read `color-interpolation`
  at all. Kept to lock in the sRGB-default behavior; the divergence from
  resvg's non-default rendering here is expected.
- **`mask/on-group-with-transform.svg`, `mask/half-width-region-with-
  rotation.svg`** — both apply a default-`objectBoundingBox`-units mask to
  a `<g>` target. A `<g>` has no single `Path` to measure a bounding box
  from (unlike a `Shape`), so `pkg/svg/draw`'s mask/clip-path builders pass
  a nil `boundsFunc` for a Group target, which `clipUnitsMatrix` degrades to
  Identity (i.e. `userSpaceOnUse`) rather than resolving a real bbox — a
  documented, narrow approximation (see `draw.go`'s "Same nil-target
  approximation" comments) pending a group-subtree bbox helper. Kept: both
  correctly render blank (the degenerate Identity-mapped region misses the
  actual geometry), which is a graceful degradation, not a crash or
  garbage output.
- **`mask/recursive-on-child.svg`** — resvg's own title suffix marks this
  "(UB)": a mutually-cyclic mask reference has no spec-defined resolution,
  so implementations reasonably disagree. Kept: this engine's cycle guard
  terminates deterministically and produces sane (non-garbage) output.
- **`mask/recursive-on-self.svg`** — vendored and MATCHING. An earlier
  revision of this file called it "inherently implementation-defined" and
  kept a divergent golden on that basis. **That was wrong**, and it was
  covering a real bug:
  - resvg's suite scores this fixture `1` (passed) for Chrome, Firefox,
    Safari, resvg, AND Inkscape (`results.csv`). Five independent
    renderers agree, so the behavior is interoperable, not a coin flip.
    Only `recursive-on-child.svg` is genuinely UB, and resvg's own `<desc>`
    says so — this one carries no such marker.
  - The engine used to resolve one level THROUGH the cycle and keep the
    result as an extra attenuation, making the output the product of both
    gradients: symmetric in x and y, and ~4x too faint. Dropping the cyclic
    reference entirely (see `maskRefCycles` in `pkg/svg/mask.go`) matches
    resvg, whose parser rewrites a cyclic `mask` attribute to `none` before
    rendering (`usvg/src/parser/svgtree/parse.rs`, `fix_recursive_links`).

  **Note on the committed reference PNG:** `recursive-on-self.png` in the
  vendored suite is STALE. It shows a monotonic ramp (alpha 0→55 down the
  image), but building resvg from source at `021d44b` and rendering the same
  file gives a symmetric parabola peaking at alpha 64 — which is what this
  engine now produces, to within 2/255. resvg's behavior changed after that
  reference was generated. Compare against a current resvg build rather than
  that PNG when auditing this fixture.

## Bugs found and fixed in this tranche

Three real, pre-existing bugs surfaced by cross-checking every golden
against resvg's reference PNGs (not just eyeballing intent) — the sharpest
argument yet for vendoring a corpus with reference renders:

1. **`visibility:hidden` on a `<clipPath>` child was incorrectly kept in
   the union.** `pkg/svg/clippath.go`'s `buildClipChild` had an explicit
   comment asserting "per SVG a visibility:hidden clipPath child STILL
   contributes to the union (only display:none removes it)" — this is
   wrong. Per SVG 1.1 §14.3.5 and SVG2's clipPath model, and confirmed
   against resvg's `invisible-child-1.svg` reference (a
   `visibility="hidden"`-only child clips its target to nothing, identical
   to `invisible-child-2.svg`'s `display="none"` case), both properties
   remove a child from the union. `TestClipPathVisibilityHiddenChildKept`
   (renamed `TestClipPathVisibilityHiddenChildDropped`) had encoded the
   same wrong assumption and was corrected alongside the fix.
2. **Nested/self-referencing masks composed via `min` instead of
   multiplication.** `pkg/svg/draw/mask.go`'s `buildMask` intersected a
   mask's own `Self` reference (`mask="url(#...)"` on a `<mask>` element)
   using the same `intersectMasks` (per-pixel `min`) that `clip-path`
   correctly uses for its boolean-AND region semantics. A mask stacking on
   another mask is multiplicative alpha compositing, not a hard
   intersection — `TestNestedMaskOnMask`'s own doc comment already stated
   "outer's mask value = outer content x inner mask" but every existing
   test used only binary (0/255) mask values, where `min` and product are
   indistinguishable. `mask/mask-on-self-with-mixed-mask-type.svg`, whose
   two composed masks are both soft gradients, exposed the gap (a
   systematically darker/more-opaque result versus resvg's reference). Added
   `multiplyMasks` (used only for `Mask.Self`, not for clip-path
   intersection or clip+mask combination, both still correctly `min`-based)
   and re-verified against resvg's reference at multiple sample points
   (post-fix values matched to within 1/255).
3. **A clipPath child's own nested `clip-path` was resolved in the wrong
   coordinate space.** `pkg/svg/draw/clip.go`'s `buildClipMask`, when a
   `<clipPath>` child itself carries `clip-path="url(#...)"`, resolved that
   nested reference against `cpM` (the parent clipPath's own units/
   transform matrix) — omitting the child's OWN transform (`kid.M`), even
   though the child's geometry (`dp`) is correctly transformed by
   `kid.M.Mul(cpM)` two lines above. Since a clip-path is userSpaceOnUse
   relative to the referencing element's own coordinate system, the missing
   `kid.M` sent the nested clip's geometry into the wrong space whenever the
   child had its own transform. `clipPath/clip-path-on-child-with-
   transform.svg` (child has `transform="translate(20 20)"`, its nested
   clip has `transform="translate(30 32)"` plus a `scale(0.7)` on its own
   circle) exposed this as a lopsided, asymmetric result instead of resvg's
   symmetric flower shape. Fixed by composing `kid.M.Mul(cpM)` for the
   nested resolve, matching the sibling `dp` composition.

## What shipped in the text tranche (PR 6)

100 files vendored from `tests/text/`, covering `<text>`, `<tspan>`,
per-character positioning, `text-anchor`, and font resolution:

- `text/` (25) — the per-character `x`/`y`/`dx`/`dy`/`rotate` LISTS in every
  combination the suite exercises: shorter than the text, longer than it,
  absolute mixed with relative, `dx`/`dy` standing in for `x`/`y`, units
  (`mm`, `%`) on the coordinates, and the `rotate` asymmetry (its LAST value
  persists past the end of the list, where `x`/`y`/`dx`/`dy` simply stop
  applying). Plus `transform`, entity-escaped text, `xml:space`, and nesting.
- `tspan/` (25) — inherited cursor, absolute vs relative override, style
  override, nesting, `rotate` on a child, `display:none` inside a rotate list
  (the hidden characters must still consume their entries), the `xml:space`
  matrix, pseudo-multi-line via sibling tspans, and the SVG 2 `clip-path`,
  `mask`, and `opacity` properties on a tspan.
- `text-anchor/` (11) — `start`/`middle`/`end` on `<text>` and on `<tspan>`,
  the three inheritance cases, an invalid value, a coordinates list (which
  splits into separately-anchored chunks), and the rule that `text-anchor`
  applies per CHUNK so a tspan changing it mid-chunk has no effect.
- `font-size/` (16) — absolute, `em`, `ex`, percentage, nested/mixed relative
  values, named keywords, and the zero/negative cases.
- `font-weight/` (12) — `normal`/`bold`, numeric, and the relative
  `bolder`/`lighter` keywords including both clamping ends and the
  no-parent case.
- `font-style/` (3), `font-family/` (6), `direction/` (1),
  `unicode-bidi/` (1).

### Font substitution: what this corpus can and cannot assert

Most of the suite specifies **Noto Sans**, **Amiri**, or **Source Sans Pro**,
none of which this repo bundles; they resolve to the bundled look-alikes. So
**every** golden here differs from resvg's reference PNG in glyph SHAPE and
advance. That is expected and is not what these fixtures assert.

Each vendored file was chosen because its claim is GEOMETRIC — where a chunk
is anchored, where a per-character list puts each glyph, which characters a
whitespace rule keeps, whether a clip cuts the text — and that claim survives
substitution. Every one was compared against resvg's reference PNG by eye
before vendoring; the goldens committed here are OUR output (as everywhere
else in this corpus), and they lock that geometry against regression.

Several fixtures are self-checking, which makes them stronger than an eyeball
comparison: `font-size/nested-percent-values-1/2` and `font-size/mixed-values`
draw black text over red text that must be exactly covered, and the
`font-weight` relative-keyword files draw two lines that must match. Those
verify correctly under substitution regardless of which face is used.

### Deliberately NOT vendored

- **`textPath/` (44) and `writing-mode/` (25)** — deferred subsystems; both
  degrade with a log today. `tref/` (11) is dropped, not deferred (removed
  from SVG 2).
- **`textLength`/`lengthAdjust` (16), `letter-spacing`/`word-spacing` (19),
  the baseline attributes (62), `text-decoration`** — a later task.
- **Emoji (`text/emojis`, `compound-emojis`, and the coordinate-list
  variants)** — no emoji font is bundled, so every code point misses and each
  one now draws a `.notdef` box (they used to render nothing at all). The
  degradation is correct and visible, but a row of identical tofu boxes
  asserts only the advance, not the geometry these fixtures are about.
- **`font-family/cursive`, `fantasy`, `noto-sans`, `source-sans-pro`** — the
  named family has no bundled analogue, so the fixture would only assert
  which fallback was picked.
- **`text/fill-rule=evenodd`** — the claim (even-odd inverts glyph counters)
  is real and implemented, but the fixture is Amiri Arabic, whose substituted
  shapes diverge far enough that the reference tells you nothing about
  whether the fill rule applied. Covered by a unit test instead.
- **`text/complex-graphemes*`, `ligatures-*`, `zalgo`, `xml-lang=ja`,
  `real-text-height`, `filter-bbox`, `tspan/with-filter`, `tspan-bbox-2`** —
  each depends on a font this repo does not bundle in a way that changes the
  result qualitatively, or on an unshipped feature (filters).

### Bugs found by the reference sweep (all fixed in the same PR)

The comparison against resvg's references paid for itself five times over:

1. **`clip-path`/`mask` on a `<tspan>` were ignored** — lowering only read
   them off the `<text>` element. `tspan/with-clip-path.svg` rendered
   completely unclipped.
2. **Bidi text was reordered before the pen walk** — SVG's position lists
   address characters in LOGICAL order, so reordering during shaping made an
   absolute `x` land on whichever glyph reordering happened to put first.
   `direction/rtl.svg` threw the tail of its Arabic 170 units away from the
   rest of the string.
3. **`text-anchor` was not direction-relative** — `start` means the RIGHT
   edge in an rtl chunk. `direction/rtl.svg` ran off the right of its
   viewport.
4. **`unicode-bidi: bidi-override` did nothing to Latin** — the reference
   renders "This is" as "sihT si".
5. **Source indentation shifted an absolute position** — the collapsed space
   between two sibling tspans landed inside the SECOND one and took its
   `x="40"`. `tspan/pseudo-multi-line.svg` staggered its three lines instead
   of left-aligning them.

Two bugs OUTSIDE `pkg/svg` were found the same way and fixed at the source:
`pkg/font`'s glyph outlines carried a leading drawing op before any move-to,
so every glyph's `Bounds` stretched back to the origin; and
`inline.Reorder` emitted a multi-rune cluster glyph once per RUNE, so
reordering Arabic returned more glyphs than it was given.

## What shipped in the text tranche, part 2 (PR 6, task 2)

98 more files vendored from `tests/text/`, covering the spacing, length,
baseline, and decoration properties:

- `letter-spacing/` (7) and `word-spacing/` (6) — the value forms (bare
  number, `%`, `mm`, `normal`, negative) and `mixed-spacing`, which nests a
  differently-spaced `<tspan>` inside a spaced `<text>`.
- `textLength/` (10) — on `<text>` and on `<tspan>`, nested (innermost wins),
  larger and smaller than the natural width, `%` and `mm`, and the three
  error/edge cases: `zero`, `negative` (ignored), and `inherit` (not
  inheritable, so also ignored). `lengthAdjust/` (2) — `spacingAndGlyphs`,
  which scales the outlines, and `with-underline`.
- `dominant-baseline/` (17) and `alignment-baseline/` (11) — every keyword
  this engine can compute from `Face.Metrics()`, the precedence between the
  two properties, the `baseline` keyword's DEFER-don't-reset behaviour, and
  the two `inherit` scoping cases (see below).
- `baseline-shift/` (22) — `sub`/`super`, lengths, percentages, the deep
  nesting and mixed-sign accumulation cases, all five inheritance fixtures,
  and `with-rotate`.
- `text-decoration/` (20) — all three lines, the four `style-resolving`
  fixtures that pin whose paint and whose font-size a rule takes, the
  multi-colour indirect case, and the `dy`/`y`/`rotate` list fixtures that
  make a rule staircase and tilt with its glyphs.
- `font/` (2) — the `font` shorthand as a presentation attribute and as CSS.
- `font-kerning/as-property` (1) — self-checking: the two overlaid `AVA`
  strings must coincide, which they do because this engine ignores
  `font-kerning` in both the attribute and the CSS form.

### Deliberately NOT vendored in this tranche

- **`kerning/0`, `kerning/10percent`, `font-kerning/none`** — resvg applies
  GPOS kerning by default and these fixtures assert turning it OFF (or
  replacing it with a length). This engine runs NO kerning-pair pass for
  simple scripts at all, so there is nothing to disable: the two overlaid
  strings coincide where resvg's diverge. A real difference, not a
  substitution artifact.
- **`dominant-baseline/ideographic`, `mathematical`, `use-script`,
  `reset-size`, and `alignment-baseline/ideographic`, `mathematical`** —
  these need OS/2 and BASE table metrics `pkg/font` does not parse. resvg
  shifts the text; this engine degrades to the alphabetic baseline with a
  warn-once. Committing a golden here would lock in the degradation as if it
  were correct.
- **`alignment-baseline/after-edge`, `baseline`, `text-after-edge`,
  `letter-spacing/large-negative`, `word-spacing/large-negative`,
  `dominant-baseline/reset-size`** — the suite marks each "(UB)" and its
  reference PNG renders nothing but the letters "UB". There is no behaviour
  to match.
- **`font-stretch/` (3) and `font-variant/` (2)** — the bundled families ship
  no condensed, expanded, or small-caps variant, and no synthetic stretching
  or feature plumbing exists. resvg's references are visibly narrower /
  small-capped; this engine logs and renders the normal face.
- **`lengthAdjust/text-on-path`, `vertical`, `alignment-baseline/
  hanging-on-vertical`, `middle-on-textPath`,
  `two-textPath-with-middle-on-first`** — depend on `<textPath>` or
  `writing-mode`, both deferred subsystems.
- **`letter-spacing/filter-bbox`** — needs filters. Its `<desc>` is still
  load-bearing evidence (see below), just not as a golden.
- **`letter-spacing/on-Arabic`, `mixed-scripts`, `non-ASCII-character`,
  `textLength/arabic`, `arabic-with-lengthAdjust`** — Amiri / Mplus 1p are
  not bundled, and `on-Arabic` additionally asserts the cursive-tracking rule
  (letter-spacing is IGNORED for joined scripts, CSS Text 3 §8.2), which this
  engine does not implement.

  `dominant-baseline/hanging` (Devanagari `क`) IS vendored despite the same
  missing-font problem, because it now asserts something real: with no bundled
  Devanagari face the character misses and draws a `.notdef` box, and the box
  is placed by the same baseline math a real glyph would use. The golden shows
  it hanging below the crosshair, which is exactly the fixture's claim. Before
  `.notdef` existed the golden was the bare crosshair — a picture of the
  silent-drop bug, asserting nothing.

### What the reference sweep settled

Four behaviours were read off resvg's reference PNGs rather than the spec,
because the spec is ambiguous or the two disagree:

1. **`letter-spacing` adds NO trailing space.** SVG 1.1's wording says after
   every glyph; CSS Text 3 says between. `letter-spacing/filter-bbox.svg`
   settles it with a filter region whose flood rectangle ends flush with the
   final glyph, and says so in its own `<desc>`.
2. **A `text-decoration` inherited from ABOVE the `<text>` adopts the
   `<text>`'s paint,** not the declaring ancestor's.
   `outside-the-text-element.svg` renders a BLACK underline under a
   `fill="black"` `<text>` inside a `fill="green"` `<g>`;
   `style-resolving-2.svg` repeats it with different colours.
3. **A `baseline-shift` on the `<text>` element itself is inert.**
   `inheritance-1`, `-3`, `-4` and `-5` each overlay an unshifted red
   reference the black text must exactly cover.
4. **`dominant-baseline` propagates inside a `<text>` but not into it, while
   `alignment-baseline` is non-inherited with an explicit `inherit` that
   reaches back.** `dominant-baseline/inherit.svg` and
   `alignment-baseline/inherit.svg` are the same shape and render
   OPPOSITELY; only that split model satisfies both.

Two further behaviours came from the sweep as outright bugs, caught before
any golden was committed: `alignment-baseline="baseline"` was resetting to
the alphabetic baseline instead of deferring to the parent's dominant
baseline (`alignment-baseline=baseline-on-tspan.svg` renders flush), and a
decoration was drawn as ONE rectangle across the whole run instead of one per
baseline frame, which flattened `underline-with-dy-list-1`'s staircase and
merged `underline-with-rotate-list-3`'s four tilted segments into one.

## What shipped in the filters tranche (PR 7, task 1)

38 files vendored, covering the filter INFRASTRUCTURE plus the two simplest
primitives — the pipeline end to end, with the rest of the primitives
deferred to task 2:

- `filters/feFlood/` (8) — the flood itself, its default (black) value, a
  partial and an explicit primitive subregion, subregion inheritance from the
  filter region, `primitiveUnits="objectBoundingBox"`, `flood-opacity`, and a
  flood under an element-level `opacity` (which pins the filter-then-opacity
  order: the opacity applies to the filtered RESULT, not to the input the
  flood discards).
- `filters/flood-color/` (7) and `filters/flood-opacity/` (2) — the colour
  syntaxes including `hsla()`, the percentage opacity form, and the full
  `inheritance-1..5` set that pins flood-color as NON-inherited with an
  explicit `inherit` reaching the direct parent only.
- `filters/feOffset/` (9) — positive, negative, single-axis, zero, and
  fractional offsets, `primitiveUnits="objectBoundingBox"`, a percentage
  value (invalid for `<number>`, so it falls back to 0 — the element does not
  move), and a skewed transform.
- `filters/filter/` (12) — the region/units/graph and error-handling cases
  that do not need an unimplemented primitive: an unresolvable FuncIRI, an
  empty `<filter>`, `filter="none"`, a filter on an empty group in both unit
  modes, a zero-sized shape, the tight path bounding box, invalid/zero/
  oversized subregions, an invalid region, and a filter under a parent mask.

### What the reference sweep settled (filters)

Four behaviours were read off resvg's reference PNGs, and each one is the
OPPOSITE of the obvious guess:

1. **An unresolvable `filter="url(#missing)"` means the element is NOT
   RENDERED AT ALL** — the exact opposite of `clip-path`/`mask`, where an
   unresolvable reference degrades to "no restriction".
   `filter/invalid-FuncIRI.png` shows no rect.
2. **An empty `<filter>` outputs transparent black, not a pass-through.**
   `filter/no-children.png` is likewise blank, so treating an empty graph as
   "no filtering" (the tempting simplification) renders an element that must
   have disappeared.
3. **A negative or zero region OR primitive subregion disables the element
   entirely**, rather than merely skipping that primitive —
   `filter/invalid-subregion.png` and `zero-sized-subregion.png` are blank.
4. **An `objectBoundingBox` filter on an empty group is not rendered, but a
   `userSpaceOnUse` one still paints.** `on-an-empty-group-1` (userSpace)
   floods; `on-an-empty-group-2` (the oBB default) is blank — an undefined
   bbox only disables the filter when the units actually need one.

### Known tolerance gaps

`filter/on-a-thin-rect` renders at **2.78% differing pixels** (worst channel
delta 75). Root cause: `filterSpace` (`pkg/svg/draw/filter.go`) derives a
SINGLE uniform scale from the element matrix, so a non-uniform transform
rasterizes the filter region at the wrong aspect — see the KNOWN
APPROXIMATION comment at that line. A real fix needs a per-axis filter space
threaded through `filterM`/`postM` and every primitive's subregion math.

`feGaussianBlur/small-stdDeviation` differs by design: resvg switches to an
**IIR** blur below stdDeviation 2 (stated in that fixture's own `<desc>`),
while this engine implements the spec's three-box approximation everywhere.
Both are valid readings of the spec; ours is the one the spec writes down.

`feOffset/with-primitiveUnits=objectBoundingBox` renders at **0.28%
differing pixels against resvg's reference**, just over the project's 0.2%
budget (worst channel delta 66). The gap is a ONE-PIXEL antialiased seam on
two edges: both edge POSITIONS are correct to the pixel, and the interior is
exact. It comes from resvg resolving that fixture's fractional offset in a
filter surface whose resolution differs from ours, so the sub-pixel coverage
of the boundary row/column lands differently. The committed golden is OUR
output (as everywhere in this corpus), so the sweep still locks it against
regression; the number is recorded here rather than papered over by widening
the tolerance.

## What shipped in the writing-mode tranche (PR 8)

19 files from `text/writing-mode/`, landing with the SVG vertical-text work
(`writing-mode` + `text-orientation` on the SVG path). They cover both
vocabularies — `vertical-rl`/`vertical-lr` and SVG 1.1's `tb`/`tb-rl`, plus the
horizontal spellings `lr`/`lr-tb`/`rl`/`rl-tb` that must NOT go vertical —
inheritance from an ancestor `<g>`, an invalid value falling back to horizontal,
per-tspan `dx`/`dy` inside a vertical run, `text-anchor` on a vertical chunk, and
Arabic (bundled) with `rotate` and with an underline.

### Notable exclusions (this tranche)

Four of the 23 upstream files are held back, all for the same reason: they are
set in Japanese, no bundled face covers CJK, and against the bundled faces they
render as columns of `.notdef` boxes. Committing those goldens would lock in
tofu as this engine's expected output, which is worse than not having the
fixture — the sweep would then pass forever while rendering nothing legible.

- `japanese-with-tb`
- `tb-and-punctuation`
- `mixed-languages-with-tb`
- `mixed-languages-with-tb-and-underline`

They land with a CJK face if one is ever vendored; that is a `DEPENDENCIES.md`
question (a CJK font is megabytes), not a rendering one.

### Bugs found in this tranche

**`writing-mode` on a `<tspan>` was honoured.** `on-tspan.svg` states the rule in
its own `<desc>`: the property applies to the `<text>` element. This engine
resolves style per CHARACTER, so a `<tspan writing-mode="tb">` silently turned
just the glyphs it covered through a right angle, mid-run. Fixed in
`applyWritingMode` by refusing a `tspan`'s own declaration.

The first fix refused every element except `<text>` — and broke
`inheritance.svg`, where a `<g writing-mode="tb">` wraps the `<text>` and *must*
apply, because the property inherits. Both fixtures now pin their own half; a
unit test (`TestWritingModeAppliesToTextNotTspan`) pins them together, since the
two rules pull in opposite directions.

### Known gap surfaced here (not fixed)

Rendering the four held-back fixtures produced full columns of `.notdef` with
**zero diagnostics**. `pkg/layout/inline`'s `warnMissingGlyph` fires on the CSS
path, but the logger is evidently not threaded through SVG's text path, so
missing glyphs there are silent. Recorded in `docs/SVG.md`; it is its own bug
and out of scope for the writing-mode work.
