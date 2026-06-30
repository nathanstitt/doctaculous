# HTML rendering — list markers + CSS counters

**Date:** 2026-06-29
**Status:** Design (approved — autonomous maximal-fidelity track)
**Sub-project:** presentational-features program, engine feature 2 of 4

## Problem

`display: list-item` (the UA value for `<li>`) renders as a plain block: no bullet,
no number, and `<ul>`/`<ol>` have no indentation, so lists look like undifferentiated
text. There is no CSS counter system. This implements list markers at full browser
fidelity, built on a real CSS counter engine (so ordered lists, nested numbering, and
custom `counter()`/`counters()` all work).

## Scope

- **`list-style-type`**: `disc`, `circle`, `square` (bullets); `decimal`,
  `decimal-leading-zero`, `lower-alpha`/`upper-alpha` (= `lower-latin`/`upper-latin`),
  `lower-roman`/`upper-roman`; `none`. Unknown types fall back to `disc`
  (unordered context) / `decimal` and are logged.
- **`list-style-position`**: `outside` (initial — marker in the padding/margin to the
  left of the content) and `inside` (marker is the first inline content, flowing with
  text).
- **`list-style` shorthand** (type / position / image, any order).
- **`list-style-image`**: parsed; **falls back to the type marker** (image markers
  need the raster `background-image` path — engine feature 3 — so a `url(...)` marker
  logs + uses the type bullet for now).
- **CSS counters** (the maximal foundation): `counter-reset`, `counter-increment`,
  `counter-set`, and `content: counter(name[, style])` / `counters(name, sep[,
  style])`. The implicit `list-item` counter drives default ordered-list numbering;
  `<ol start>` and `<li value>` map onto it (the start/value *attributes* land in the
  later presentational-attribute pass, but the counter machinery they need is here).
- **UA defaults**: `ul, ol { margin-top: 1em; margin-bottom: 1em; padding-left: 40px }`;
  `ul { list-style-type: disc }`; `ol { list-style-type: decimal }`; nested-depth
  bullet rotation (`ul ul { circle }`, `ul ul ul { square }`); `li { display:
  list-item }`.

Deferred (logged): non-Latin numbering systems (`georgian`, `cjk-*`, …),
`::marker` pseudo-element styling, `list-style-image` actual images (feature 3),
`@counter-style`.

## Architecture

Two cohesive units:

1. **Counter engine** (`pkg/layout/css/counters.go`, new) — a pure tree walk that
   computes, for every box, the active counter values, applying `counter-reset` /
   `counter-increment` / `counter-set` in document order with proper nesting scopes
   (CSS Lists §4). It exposes the values a `counter()`/`counters()` in `content` (and
   a list-item marker) needs. Independently unit-testable on a box tree.

2. **Marker generation + layout** — box generation creates a marker for each
   `list-item` box (from its `list-style-type` + the resolved `list-item` counter
   value), and layout positions it (`outside` → left of the content box on the first
   line's baseline; `inside` → leading inline content). Paint reuses `GlyphKind`.

The number/bullet *formatting* is a pure helper, `formatCounter(value, style) string`
(decimal, leading-zero, roman, alpha, and the bullet glyphs), unit-tested with
hand-computed vectors (e.g. roman 4→"iv", 1990→"mcmxc"; alpha 27→"aa").

## Design

### 1. Cascade (`pkg/css`)

`ComputedStyle` gains:
- `ListStyleType string` (inherited; initial `"disc"`).
- `ListStylePosition string` (inherited; initial `"outside"`).
- `CounterReset`, `CounterIncrement`, `CounterSet` — each a parsed `[]CounterOp`
  (`{Name string; Value int}`); NOT inherited.
- `Content` — the parsed `content` value when it contains counter functions (a small
  token list of literal-string / counter(name,style) / counters(name,sep,style)
  parts); NOT inherited. (Full `content` with strings/attr() is partly covered: we
  parse the pieces we render — strings and counters; other pieces are skipped.)

Parsing: `list-style-type`, `list-style-position`, the `list-style` shorthand,
`counter-reset`/`-increment`/`-set` (name + optional integer, space-separated pairs),
and `content` (string + `counter()`/`counters()` functions).

### 2. Counter engine (`pkg/layout/css/counters.go`)

`resolveCounters(root *cssbox.Box)` walks the tree in document order maintaining a
stack of counter scopes (CSS Lists §4.3–4.4):

- On entering an element: apply its `counter-reset` (creates/【re】sets a counter in
  the current scope), then `counter-increment` (default +1 for the `list-item`
  counter on a `display:list-item` element), then `counter-set`.
- The element's resolved counter values (a snapshot map, or just the value(s) it
  needs) are stored on the box (`box.Counters map[string]int`, or a focused
  `box.ListItemOrdinal int` for the common list case + a general map for `content`).
- Nesting scope rules: a counter created by `counter-reset` on an element is in scope
  for that element's descendants and following siblings until the scope closes;
  `counters(name, sep)` joins all active values of `name` up the scope chain
  (nested-list numbering 1.2.3).

The implicit list-item counter: a `display:list-item` element auto-increments a
counter named `list-item`; an `<ol reversed>`/`start`/`<li value>` adjust it (start/
value via the attribute pass). The marker uses this value.

### 3. Marker generation (`pkg/layout/css/build.go` + a marker box)

For a `list-item` box whose `list-style-type != none`:
- Compute the marker string: a bullet glyph for disc/circle/square, else
  `formatCounter(ordinal, type)` + the marker suffix (". " for numeric, per UA).
- Store it as a **marker** on the box: `box.Marker *MarkerContent{Text, Position}`.
  (A focused field, not a full pseudo-element box — markers are leaf text.)

`content: counter(...)` on a normal element (not a list item) generates the box's
text content from the resolved counters via the same `formatCounter`.

### 4. Layout (`pkg/layout/css`)

When laying out a `list-item` block fragment that has a marker:
- **outside**: emit the marker as a glyph run on the first content line's baseline,
  positioned at `contentX − markerWidth − markerGap` (markerGap ≈ a small constant /
  the marker sits in the `padding-left` gutter the UA provides). The marker does not
  affect the content's own layout width.
- **inside**: prepend the marker text to the item's inline runs so it flows as the
  first inline content (shifting the first line's text right).

Marker glyphs are shaped via the existing inline shaper (the item's font). A
list-item with no line box (empty `<li>`) still shows its marker on a marker-height
line.

### 5. Paint

Marker glyphs are ordinary `GlyphKind` items (no new primitive). The
marker rides the fragment's translate/shift like any glyph (it lives in the
fragment's Lines/marker glyph list).

## Testing

- `pkg/css`: parse `list-style-type`/`-position`/shorthand;
  `counter-reset`/`-increment`/`-set`; `content: counter()/counters()`. Inheritance
  of `list-style-*`.
- `formatCounter`: decimal, decimal-leading-zero (7→"07"), lower/upper-roman
  (4→"iv", 1990→"mcmxc", 0/negative → decimal fallback), lower/upper-alpha (1→"a",
  26→"z", 27→"aa"), disc/circle/square bullet chars.
- Counter engine: list-item ordinals (flat list 1..n; two sibling `<ol>`s each restart;
  nested `<ol>` independent); `counter-reset`/`-increment` on non-list elements;
  `counters(item, ".")` nested join (1, 1.1, 1.2, 2).
- Marker generation: a `<ul><li>` gets a disc marker; `<ol><li>` gets "1." etc.;
  `list-style-type:none` → no marker; inside vs outside flag.
- Layout: an outside marker sits left of the content (X < content X); an inside marker
  shifts the first line right; nested list indentation from UA `padding-left`.
- Golden images (`pkg/doctaculous`): a `11 / LISTS` showcase section — `ul`
  disc/circle/square (nested), `ol` decimal/lower-alpha/upper-roman, `inside`
  position, a `counters()` nested-numbering example. Eyeballed.
- **Byte-identical:** pages with no list-item/counter/content are unchanged (the
  marker + counter passes are gated; `resolveCounters` no-ops a tree with no counter
  ops or list items). DOCX unaffected.

## Files

- `pkg/css/cascade.go` — list-style + counter + content fields, parsing, inheritance;
  `formatCounter`.
- `pkg/css/shorthand.go` — `list-style` shorthand expansion.
- `pkg/layout/css/counters.go` (new) — `resolveCounters` tree walk.
- `pkg/layout/cssbox/box.go` — `Marker *MarkerContent`, `Counters`/ordinal fields.
- `pkg/layout/css/build.go` — call `resolveCounters`; generate markers + `content`
  text.
- `pkg/layout/css/block.go` / `inline.go` / `fragment.go` — lay out + carry the
  marker (outside glyph run / inside leading inline); paint via GlyphKind.
- `pkg/html/ua.go` — ul/ol/li margins, padding, list-style-type, nesting.
- Tests + the showcase section as above.

## Out of scope / deferrals (each logged, none crash)

- `list-style-image` actual images (feature 3 — falls back to type marker).
- `::marker` styling, `@counter-style`, non-Latin numbering systems.
- `content` pieces other than strings + counter()/counters() (e.g. `attr()`, images).
- `reversed` ordered lists (rare; the counter engine can add it later).
