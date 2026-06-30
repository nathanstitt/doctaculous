# HTML rendering — CSS `white-space`

**Date:** 2026-06-29
**Status:** Design (approved — autonomous maximal-fidelity track)
**Sub-project:** presentational-features program, engine feature 1 of 4

## Problem

The engine collapses all runs of whitespace to a single space and always wraps,
unconditionally, at box-generation time (`collapseWS`/`handleWhitespace` in
`pkg/layout/css/anon.go`). There is no `white-space` property, so `<pre>` content,
`white-space: pre-wrap` code blocks, and `nowrap` cells all render wrong, and the
legacy `nowrap` attribute has no target. This implements the full CSS `white-space`
property at the highest fidelity the engine can reach.

## Scope

All five CSS `white-space` values, decomposed into three independent toggles
(CSS Text §3):

| value | collapse spaces | preserve newlines | wrap |
|---|---|---|---|
| `normal` | yes | no | yes |
| `nowrap` | yes | no | **no** |
| `pre` | **no** | **yes** | **no** |
| `pre-wrap` | **no** | **yes** | yes |
| `pre-line` | yes | **yes** | yes |

- **Tabs**: in preserving modes (`pre`/`pre-wrap`/`pre-line`), a `\t` advances to the
  next tab stop at `tab-size: 8` (8 × the run's space advance), with the shaper
  tracking the current column; the column re-bases at every line start — including
  after a soft (auto) wrap, not only after a hard break (full fidelity). In
  collapsing modes a tab is collapsed to a space at box-gen, as today.
- **Leading/trailing space at a line break**: collapsing modes drop a collapsible
  space at the start/end of a line per CSS (preserve today's behavior); preserving
  modes keep all spaces.
- **Blank lines**: preserving modes keep whitespace-only text (blank lines in
  `<pre>`); collapsing modes drop inter-block whitespace as today.

`tab-size` (the CSS property) is out of scope — fixed at 8 (the initial value).
`white-space` is inherited (initial `normal`).

## Approach

Decompose `white-space` into its three toggles and handle each where the relevant
machinery already lives (Approach A):

1. **Collapse-spaces / collapse-newlines** — box generation (`anon.go`), gated by the
   element's computed `white-space`.
2. **Newlines → forced breaks** and **tab stops** — the shared shaper
   (`pkg/layout/inline/shape.go`), reusing the existing `Break` glyph marker.
3. **Wrap suppression** (`nowrap`/`pre`) — the shared breaker
   (`pkg/layout/inline/break.go`).

The inline-core change is **additive**: a new `WhiteSpace` field on `inline.Run`
whose zero value reproduces today's behavior, so the flat DOCX engine (which never
sets it) is byte-identical.

## Design

### 1. The property + three flags

`css.ComputedStyle` gains `WhiteSpace string` (inherited; initial `"normal"`). The
cascade parses `case "white-space"` accepting `normal|nowrap|pre|pre-wrap|pre-line`
(unknown → `normal`, logged). The UA stylesheet adds `pre { white-space: pre }` (and
`textarea { white-space: pre-wrap }`).

A pure helper derives the three behaviors (the single source of truth for the value
table above), used by box-gen, shaper, and breaker:

```go
// in pkg/css (or a shared spot both layout/css and layout/inline can use)
func WhiteSpaceFlags(ws string) (collapseSpaces, preserveNewlines, wrap bool)
```

`""` and unknown map to the `normal` row. To avoid a layout→css dependency cycle, the
flags live where both consumers can reach them: `inline.Run` carries the raw
`WhiteSpace` string and `pkg/layout/inline` has its own `flagsFor(ws)`; box-gen uses
the `pkg/css` copy. (Two tiny identical pure functions, each unit-tested, rather than
a new shared dependency — matches the repo's preference for no cyclic deps.)

### 2. Box generation — gated collapse (`pkg/layout/css/anon.go`)

`handleWhitespace(children, parent)` reads `parent.Style.WhiteSpace` (the text box is
anonymous and inherits). Per the collapse/preserve flags:

- **collapse, collapse-newlines** (`normal`, `nowrap`): `collapseWS` as today (runs of
  any whitespace incl. `\n` → one space).
- **collapse, preserve-newlines** (`pre-line`): new `collapseWSKeepNewlines` — runs of
  spaces/tabs collapse to one space, but each `\n` is preserved.
- **no-collapse** (`pre`, `pre-wrap`): skip collapsing; the raw text (spaces, tabs,
  `\n`) stays on the box.

The inter-block whitespace-only drop runs **only in collapsing modes**; in preserving
modes a whitespace-only text box is significant and kept.

### 3. Inline `Run` + shaper (`pkg/layout/inline/shape.go`)

`Run` gains `WhiteSpace string` (zero `""` ≡ normal). The CSS engine's
`gatherInlineRuns` sets it from `child.Style.WhiteSpace`; DOCX leaves it zero.

`Shape` consumes it via `flagsFor`:

- **preserve-newlines**: a `\n` in the run emits a `Break` glyph (the existing hard
  break marker) instead of a shaped space. (Collapsing modes never see a `\n` here —
  box-gen removed it.)
- **tabs**: in preserving modes a `\t` emits a `Space` glyph whose advance reaches the
  next `tab-size`(=8)·(space-advance) multiple from the current line column. The shaper
  tracks a running line-column x; it resets to 0 at the run-sequence start and at each
  `Break` glyph. (Soft-wrap re-basing is handled in the breaker, §4.)
- preserved spaces are ordinary `Space` glyphs with un-collapsed advances — no new
  glyph kind.

### 4. Breaker (`pkg/layout/inline/break.go`)

The greedy line breaker gains a **no-wrap** mode (from the run's `wrap` flag):

- **wrap=false** (`nowrap`, `pre`): never break on width; the line ends only at a
  `Break` glyph (a hard break / preserved `\n`) or the end of input. Content overflows
  the available width (clipped by an `overflow:hidden` ancestor, else it spills — as
  CSS specifies for `nowrap`/`pre`).
- **wrap=true** (`normal`, `pre-wrap`, `pre-line`): greedy wrapping as today. For
  `pre-wrap`/`pre-line`, a soft wrap re-bases the tab column: when the breaker starts a
  new line, the downstream tab-advance must measure from the new line start. Since tab
  advances are computed in the shaper before breaking, the breaker recomputes the
  affected run's tab glyph advances for the post-wrap segment (a small re-measure pass
  on a line that begins mid-run and contains tabs). For the common case (no tabs after
  a soft wrap) this is a no-op.

The existing "force at least one unit at width 0" guard is preserved (an unbreakable
unit wider than the line still advances rather than spinning).

### 5. Where wrap/flags reach the breaker

The breaker today takes a flat glyph stream. The `wrap` flag is uniform per inline
formatting context in practice (a single `white-space` on the block), so the breaker
gains a `wrap bool` parameter (threaded from the IFC's controlling `white-space`).
Mixed `white-space` across inline children within one line is a rare edge; this
sub-project applies the block's `white-space` to its IFC wrap decision and the
per-run flags to collapse/preserve/tabs — covering `<pre>`, `nowrap`, and code blocks
faithfully. (Per-segment wrap changes mid-line are a documented edge deferral.)

## Testing

- `pkg/css`: cascade parses all 5 values; inherits; UA `pre`/`textarea` rules.
- `WhiteSpaceFlags`/`flagsFor`: the value→(collapse,preserveNL,wrap) table.
- `pkg/layout/css/anon.go`: collapse gated by `white-space` (normal collapses; pre
  keeps spaces+tabs+newlines; pre-line collapses spaces but keeps newlines;
  whitespace-only box kept in pre, dropped in normal).
- `pkg/layout/inline`: shaper emits a `Break` for a preserved `\n`; a tab advances to
  the tab stop; the no-wrap breaker produces one line per hard break; pre-wrap wraps;
  tab column re-bases after a hard break (and a soft wrap).
- Golden images (`pkg/doctaculous`): a `<pre>` block (preserved spaces + newlines +
  tab alignment), a `pre-wrap` block (wraps but preserves), a `nowrap` line
  (overflows, single line). Eyeballed.
- **Byte-identical:** every page with no `white-space` set (all DOCX, the whole
  existing corpus) is unchanged — `Run.WhiteSpace` zero = normal; `collapseWS`
  unchanged for the normal path.

## Files

- `pkg/css/cascade.go` — `WhiteSpace` field, parse, inherit; `WhiteSpaceFlags`.
- `pkg/html/ua.go` — `pre`/`textarea` rules.
- `pkg/layout/css/anon.go` — gated collapse + `collapseWSKeepNewlines`.
- `pkg/layout/css/inline.go` — set `Run.WhiteSpace`.
- `pkg/layout/inline/shape.go` — `Run.WhiteSpace`, `flagsFor`, preserved `\n`→Break,
  tab stops + column tracking.
- `pkg/layout/inline/break.go` — no-wrap mode + soft-wrap tab re-base.
- Tests + goldens as above.

## Out of scope / deferrals

- `tab-size` CSS property (fixed 8).
- Per-segment `white-space` changes mid-line (block-level `white-space` drives wrap).
- `break-spaces`, `white-space-collapse`/`text-wrap` longhands (CSS Text 4).
