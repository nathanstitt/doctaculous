# RTL slice 1 — cascade + plumbing (2026-07-27)

## What shipped

The property-level and attribute-level groundwork for `direction`/RTL: the
direction-relative `text-align` keywords, the `unicode-bidi` property, the HTML
`dir` attribute, the `bdi`/`bdo` UA rules, and the one helper every future
direction read must go through. **No box geometry changes in this slice** — it is
deliberately invisible for LTR content (zero golden churn, asserted by running the
full golden suite without `-update`).

This is slice 1 of 5 in the RTL sub-project (backlog A1, *Large*). The backlog left
the sequencing open — "*first (so the per-mode RTL items below become free) or
last. Decision needed.*" — and this answers it: **first**, because flex's H2 and
grid's I3 are both marked "covered by A1", so writing multi-line flex before RTL
would mean writing its placement loop LTR-only and reopening it later.

## Scope

Implemented:

- **`text-align: start | end | match-parent`.** The initial value changes from the
  physical `"left"` to `"start"`. `match-parent` collapses to `start` (the cascade
  has already folded the parent's direction into the inherited value, so the used
  value is identical for everything this engine can express).
- **`unicode-bidi`** (`normal|embed|isolate|bidi-override|isolate-override|plaintext`).
  Parsed and stored only — the embedding levels it controls are meaningless without
  the reordering pass, which is slice 3. **Not inherited**, unlike `direction`
  directly above it in `ComputedStyle`.
- **The `dir` attribute**, as a presentational hint, emitting `direction` plus the
  `unicode-bidi` isolation HTML §15.3.3 specifies (`isolate`, or `isolate-override`
  on `<bdo>`).
- **`bdi`/`bdo` UA rules** for the isolation defaults.
- **`effectiveDirection`** + direction-aware `mapTextAlign`, and the RTL text-indent
  edge.

Explicitly NOT in this slice: box-level mirroring (slice 2 — the three "laying out
LTR" logs still fire), inline bidi reordering (slice 3), Arabic shaping (slice 4),
PDF extraction visual→logical (slice 5).

## Why `dir` is a presentational hint, not a UA rule

The HTML spec expresses `dir` in the UA stylesheet with attribute selectors
(`[dir=rtl] { direction: rtl }`). **This engine's selector parser has no
attribute-selector support at all** — `simpleSelector` (`pkg/css/selector.go`) is
`{tag, id, classes, pseudos, nths}` — so that is not expressible today.

Routing `dir` through `presentationalHints` gives an equivalent cascade rank for
every case that matters: a hint sits above UA and below author, so
`<div dir="rtl" style="direction:ltr">` correctly resolves LTR. Pinned by
`TestHintDirLosesToAuthor`.

`dir="auto"` needs first-strong-character detection over the element's text, which
needs the bidi character database. It contributes no direction hint and is logged
by the resolver (`dirAutoRequested`) rather than silently ignored.

## The anonymous-box trap

The load-bearing correctness constraint in this slice, and the reason
`effectiveDirection` exists rather than callers reading the field:

An anonymous block (`cssbox.BoxAnonBlock`) carries a **zero-value**
`ComputedStyle`, so `b.Style.Direction` is `""` — **not** `"ltr"`. A naive read
therefore lays every anonymous block out LTR inside an RTL subtree, silently.
`effectiveDirection` recovers the inherited value from the first inline descendant
that carries one (the cascade copies it onto every inline descendant), mirroring
the existing `effectiveTextAlign`/`firstInlineTextAlign` and
`lineHeightGuess`/`firstInlineLineHeight` pattern.

**Rule for later slices: never read `b.Style.Direction` directly.** Regression test:
`TestEffectiveDirectionAnonymousBox`, mutation-verified (replacing the helper body
with a direct field read fails it, and fails
`TestEffectiveTextAlignStartUnderRTL` too).

## Why changing the text-align initial value is safe

Changing an initial value is the kind of edit that quietly moves rendering
everywhere, so all eight `TextAlign` consumers were checked before the change, not
after:

| Consumer | Behavior for `"start"` |
|---|---|
| `layout/css/inline.go` `mapTextAlign` | direction-resolved (this slice) |
| `layout/css/marginbox.go` `alignOf` | delegates to `mapTextAlign(_, "ltr")` |
| `render/markdown/table.go` `separatorFor` | `default:` → `---`, same as `left` |
| `render/docxwrite/inline.go` | falls through to `JustifyLeft`, omitted |
| `render/rtfwrite/inline.go` | falls through to `""` |
| `render/htmlwrite/table.go` | only emits for center/right |
| `render/pptxwrite` `alignOf` | allowlist-guarded → `""` |
| `docx/cssbox/lower.go` | writes only physical keywords |

Every one defaults to left for an unrecognized value, so LTR output is
byte-identical. The `pptxwrite` case is the one that *looks* like a verbatim
passthrough (`return b.Style.TextAlign`) but is guarded by an explicit
`case "center", "right", "justify":` allowlist — since the initial value now
reaches it on every unstyled paragraph, that guard is pinned by a dedicated test
(`TestAlignOfDropsDirectionRelativeKeywords`) so a future refactor cannot silently
emit `algn="start"` into a .pptx.

## Testing

- **Cascade**: keyword parsing; the `start` initial; `unicode-bidi` values and
  dropped bad values; and one table test pinning the asymmetry that
  **`direction` inherits and `unicode-bidi` does not** (they are declared
  adjacently, so this is the copy-paste trap).
- **Hints**: `dir` on many tags (it is a global attribute, not a tag allowlist);
  `dir=ltr` as well as `rtl`; `<bdo>` isolation override; author-beats-hint;
  `dir=auto` degrades and logs, while a normal `dir` stays silent.
- **Layout**: the `mapTextAlign` matrix (start/end × ltr/rtl, plus
  direction-invariance of the physical keywords); the anonymous-box trap
  (mutation-verified); RTL text-indent.
- **WPT reftest** `text-align-start`: `text-align:start`, an explicit
  `direction:ltr` + `start`, and a block with no `text-align` at all must all
  rasterize identically to physical `left` — this is what locks the
  "initial-value change is byte-identical" claim as a pixel assertion rather than
  an argument.
- **Golden gate**: the full suite runs without `-update` and **no golden file
  changed**.

## Deviation from the per-PR showcase rule

CLAUDE.md requires a `testdata/htmldoc/` showcase entry with every feature. This
slice deliberately ships without one (user-approved): nothing it does is visually
observable in an LTR document, so a new section would regenerate ~19 goldens for
zero visual change and dilute the zero-churn assertion that is this slice's main
safety property. Slice 2 adds one section covering both slices' visible behavior.

## Known limitation carried forward

After this slice `direction` still does not move any box, and text within a line is
never reordered. A right-to-left script renders in logical glyph order. Slice 2
retires the three box-level logs; slice 3 is the inline core.
