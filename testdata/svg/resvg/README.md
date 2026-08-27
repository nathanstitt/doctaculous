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
