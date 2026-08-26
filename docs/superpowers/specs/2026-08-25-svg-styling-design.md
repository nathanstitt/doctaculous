# SVG CSS Styling (PR 2 of 8) — Design

**Date:** 2026-08-25
**Status:** Approved design, pending implementation plan
**Parent spec:** [2026-08-25-svg-support-design.md](2026-08-25-svg-support-design.md)
**Base branch:** `feat/svg-styling`, stacked on `feat/svg-support` (PR #102)

## Goal

Add CSS styling to the SVG frontend: `<style>` element sheets with real selector
matching and specificity, the `style="..."` attribute, `class`, and the correct
cascade ordering. PR 1 resolved style purely from presentation attributes; this
slots the other two origins around them.

Cascade order (SVG 1.1 §6.1, CSS Cascade 4):

    UA/initial  <  presentation attributes  <  author sheet rules  <  style="" attribute

with `!important` and specificity handled per CSS.

## Decisions taken (with rationale)

Two were escalated because they change shared code or downstream PRs:

1. **Case-sensitive type selectors, fixed in `pkg/css`.** SVG element names are
   case-sensitive (`linearGradient`, `clipPath`, `feGaussianBlur`) but
   `pkg/css/selector.go:281` lowercases every parsed type selector, so
   `linearGradient { }` could never match. Rejected alternatives: lowercasing the
   SVG side too (works, but silently wrong and would collide if SVG ever adds
   case-distinct names), and writing an SVG-local matcher (duplicates a
   well-tested matcher and diverges over time).
2. **The pre-pass builds a shared document index now.** PR 2 needs a full-tree
   walk anyway to find `<style>` elements before the scene walk. PRs 3–5
   (gradients, `<use>`, clipPath) all need an id index and retained `<defs>`.
   Building it once here avoids restructuring the same walk twice — a concern the
   PR 1 final review raised explicitly.

## Architecture

### 1. `pkg/css` — two narrow changes

**a. Case-preserving type selectors.**
`parseSimple` (`selector.go:281`) currently does `ss.tag = strings.ToLower(f[:i])`.
Stop lowercasing at parse time; keep the tag verbatim. `simpleSelector.matches`
(`selector.go:112`) then compares case-insensitively — HTML type selectors ARE
case-insensitive per CSS, and HTML nodes always report a lowercase `Tag()`
because `x/net/html` lowercases at parse time (`pkg/html/html.go:55`), so
`DIV`/`div`/`Div` all keep matching an HTML `div`. An SVG node reporting
`linearGradient` matches a `linearGradient` selector and, per the same
case-insensitive rule, a `lineargradient` one — acceptable, since no two SVG
element names differ only by case.

Verified safe: no test in `pkg/css` and no CSS fixture in `testdata/` depends on
the parse-time lowercasing.

`Node.Tag()`'s doc comment ("the lowercased element name") is updated to describe
the actual contract: the element name as the host format reports it (lowercase
for HTML, verbatim for SVG), matched case-insensitively.

**b. Export `ParseDeclarations`.**
`parseDeclarations(body string) []Declaration` (`parse.go:170`) becomes exported.
Required for `style=""`; `Resolver.Compute` reads `style` internally, and SVG does
not go through `Compute`. This is the only other `pkg/css` change — nothing else
needs exporting because `css.Parse()` already reaches selectors via
`Stylesheet.Rules[i].Selectors`.

### 2. `pkg/svg` — document index pre-pass

A single pre-order walk before scene building, producing a `docIndex`:

- **`sheets []css.Stylesheet`** — parsed from every `<style>` element's text
  content, in document order. (`element.text` already captures character data
  from PR 1; nothing read it until now.) `type` is honored: absent, empty, or
  `text/css` is a sheet; anything else is skipped with a warn-once log.
- **`ids map[string]*element`** — every element carrying an `id`, first
  occurrence winning (duplicate ids are invalid; log once).
- **`defs map[string]*element`** — `<defs>` subtrees retained by id, so PRs 3–5
  can resolve `url(#...)` references without re-walking.

`<defs>` therefore stops being a blanket skip in the scene walk: it is *walked*
by the pre-pass for sheets and ids, but still *not emitted* into the visible
scene. That semantic split is made explicit in the code and its comment.

The index lives on `sceneBuilder` (constructed per `Parse`, discarded after), so
`Document` stays read-only and lock-free-shareable as before.

### 3. `pkg/svg` — the cascade

An SVG-local cascade, reusing `pkg/css` for the parts that are format-neutral and
keeping SVG's own property model:

- **Matching**: `css.Selector.Matches(css.Node)` + `.Specificity()` +
  `Specificity.Less`.
- **A `css.Node` adapter over `*element`.** `element` gains a `parent` pointer
  (backfilled in `parseXML`'s `EndElement` branch) plus precomputed `id` and
  `classes`. `Tag()` returns `local` verbatim; `Attr` reads `attrs` (already
  namespace-flattened by PR 1's `buildAttrs`). Only SVG-namespace elements ever
  get an adapter, so foreign-namespace elements are excluded for free.
- **Origin ladder**: reuse `css.OriginUA` / `OriginPresentationalHint` /
  `OriginAuthor` and reimplement the ~20-line normal/important rank functions
  locally (they are unexported in `cascade.go` and trivially small).
- **`svgPresentationHints(el) []css.Declaration`** — the SVG analogue of
  `pkg/css/hints.go`'s HTML-specific `presentationalHints`, emitting the fixed
  set of SVG presentation attributes at `OriginPresentationalHint`.
- **Output** lands in the EXISTING applier. `Style.apply`'s `attr` closure
  (`style.go:85-88`) reads `el.attrs[name]` today; it becomes a lookup into the
  cascade-resolved property map. All twelve `applyXxx` helpers are untouched, and
  the fixed resolution order (`color` first, so `currentColor` sees the element's
  own color) is preserved.

`apply`'s signature changes to carry the cascade context. That is one production
call site (`svg.go:278`) and nine test call sites; the tests gain a helper so
future signature churn is a one-line edit.

**`css.ComputedStyle` and `applyDeclaration` are NOT touched.** Adding SVG paint
properties there would put ~15 unused fields on every HTML box, and SVG's
inheritance set and initial values contradict CSS's for the same property names.

## Selector support

Working via the existing matcher: type, class, id, universal, compound,
descendant combinator, grouping, and the structural pseudo-classes
(`:first-child`, `:nth-child()`, …). Dynamic pseudo-classes (`:hover`) parse and
are inert, which is correct for static rendering.

**Unsupported, failing safe** (the selector is dropped entirely and can never
mis-match): child combinator `>`, adjacent/general sibling `+`/`~`, attribute
selectors `[fill="none"]`, `:not()`/`:is()`/`:where()`/`:has()`, pseudo-elements,
and namespace selectors (`svg|rect`). The child combinator is the most likely to
appear in real files; it is documented as a known gap. Tests mirror
`pkg/css/selector_test.go`'s `TestDeferredSelectorsDoNotMisMatch` to lock in the
safe-degradation behavior.

## Out of scope

- **`@import`** — needs a resource loader and a fetch policy; skipped with a log.
- **`font-size` / real `em`/`ex` resolution.** `pkg/svg/length.go:87` hardcodes
  `em`=16px, `ex`=8px with a comment promising resolution "when the style cascade
  lands (PR 2)". `font-size` is not in `svg.Style` at all and adding it is a text
  concern; the comment is retargeted to the text PR rather than left stale.
- **Consolidating duplicate value parsers.** `pkg/svg`'s `parseColorValue` (full
  CSS Color 4 table, hex alpha, hsl, space syntax) and `parseLength` are strictly
  better than `pkg/css`'s equivalents (8 named colors, no alpha, comma-only rgb).
  Unifying would churn HTML goldens. The duplicate `hexNibble`/`namedColors` pair
  gets a cross-reference comment noting the intentional divergence.
- **`@media`** — `pkg/css` supports type-only media; SVG inherits whatever that
  gives and does not extend it.

## Error handling & degradation

Consistent with PR 1: never panic on malformed input; every unsupported construct
degrades with a warn-once debug log through the existing `warnOnceMsg` mechanism,
reachable by callers via `WithLogf`. Specifically logged: a `<style>` with an
unsupported `type`, an `@import` rule, a dropped selector, and a duplicate `id`.

Malformed CSS follows CSS error recovery — a bad declaration is dropped and the
rest of the rule still applies; a bad selector drops only its own rule.

## Testing

- **Cascade ordering** — the SVG origin triple (presentation attribute < sheet
  rule < inline style), `!important` at each level, specificity ties broken by
  source order. Mirrors `pkg/css/hints_test.go`'s precedence tests.
- **Selector matching on SVG** — type (including case-sensitive
  `linearGradient`), class, id, descendant, grouping; plus a
  safe-degradation table for `>`/`+`/`~`/`[attr]` mirroring
  `TestDeferredSelectorsDoNotMisMatch`.
- **`pkg/css` regression** — HTML selector matching is unchanged by the
  case-sensitivity fix, asserted directly (`DIV`, `div`, `Div` all match an HTML
  `div`); the full HTML golden suite must stay byte-identical.
- **Pre-pass index** — sheets found inside `<defs>` and after the shapes they
  style still apply; ids indexed; `<defs>` content still not painted.
- **resvg corpus tranche** — vendor the upstream `tests/structure/style/**`
  files that PR 2 supports, plus class/style permutations from `tests/painting/**`.
  Every new golden is eyeballed against the fixture's `<title>`/`<desc>` intent,
  per the corpus README's standing rule that generating a golden proves nothing.
  The README gains a "What shipped in this tranche (PR 2)" section and per-item
  exclusion reasons.
- Race detector; full suite including all existing goldens.

## Delivery

One branch → one PR, stacked on `feat/svg-support` until PR #102 merges, then
retargeted to `main`. CI runs on stacked PRs (the `pull_request:` trigger in
`.github/workflows/ci.yml` has no branch filter), though while stacked it
validates the combined PR 1 + PR 2 diff.
