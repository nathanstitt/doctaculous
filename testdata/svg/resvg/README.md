# resvg-test-suite (curated subset)

SVG files copied verbatim from https://github.com/RazrFalcon/resvg-test-suite
at commit `d8e064337faf01bc5a9579187a56dbdbe3eacc72`, MIT licensed (Copyright (c)
2018 Reizner Evgeniy — see LICENSE in that repository). Each file exercises one
feature. Goldens are OUR renderer's committed output
(`pkg/doctaculous/testdata/golden/svg-resvg/`), not resvg's reference PNGs —
the sweep locks doctaculous against regression, not against resvg.

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
- **`mask/recursive-on-self.svg`** — the same class of genuinely-cyclic
  mask reference (not marked "(UB)" by resvg, but inherently
  implementation-defined for the same reason): two masks each reference the
  other via `mask="url(#...)"`. This engine's `buildingMask` cycle guard
  resolves the inner reference to "no Self" once the cycle is detected,
  producing a fainter, more radial-looking result than resvg's; both are
  plausible resolutions of an ambiguous cycle, verified sane (a smooth
  gradient, not NaN/inverted/garbage pixels).

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
