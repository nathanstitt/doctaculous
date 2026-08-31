# Remediation plan: second batch of Luckfox dashboard gaps

Source: `upnext/luckfox/docs/omnidoc-gaps.md`, findings 2b and 7–14.

The first batch (findings 1–7 in that document's original numbering) is covered by
[GAPS-REMEDIATION.md](GAPS-REMEDIATION.md) and shipped as PRs #127–#134. This plan
covers the nine new entries.

As before, every finding was **re-verified here by measurement** rather than accepted
as written. Three of the report's diagnoses are wrong in ways that change the work, and
one does not reproduce at all.

## Verification summary

| # | Reported | Verified | Correction |
| --- | --- | --- | --- |
| 2b | SVG presentation attributes do not inherit | **Real — FIXED** | Worse than reported: `fill` on the root does not inherit either, so it is not stroke-specific. |
| 7 | `line-height` is ignored | **Real — FIXED** | Only the **unitless** form (`1.2`) is ignored. `line-height: 40px` works. |
| 8 | `margin` ignored on flex children | **Real — FIXED** | Margins work fine on **blocks** — the report's "plain block" contrast case is CSS margin collapsing, not a bug. Only flex children drop them. `transform` is separately unimplemented. |
| 9 | `z-index` does not order positioned siblings | **Does NOT reproduce** | Both orderings paint red on top. Already fixed, or mis-measured. |
| 10 | `top` + `bottom` does not size a box | **Real — FIXED** | — |
| 10b | Abs-positioned flex child ignores `left` | **Real — FIXED** | — |
| 11 | Flex-derived height breaks `justify-content` | **Real — FIXED** | — |
| 12 | Abs-positioned box never shrink-wraps | **Does NOT reproduce** | Re-measured on this branch: an abs box around a 72px string comes out 72px, identical to `inline-block`, and `width:76px` is honoured exactly. Both of the report's claims are wrong, and my own first reading of it was too. |
| 12b | Flex child does not shrink-wrap | **Does NOT reproduce** | Measured 117px for a 117px string, identical to `inline-block`. |
| 13 | Comma-separated `background` list dropped | **Real — FIXED** | — |
| 14 | `writing-mode` ignored | **Real** | — |

### How these were measured

Rasterize, then sample a **specific colour** or measure an ink span — never a coverage
count. The probes are in the commit history of this branch; each finding below quotes
the numbers it produced.

---

## 7. Unitless `line-height` is ignored

**Priority 1.** Silent, affects every text block, and compounds down a page.

### Measured

Two 18px rows, measuring the ink span of both:

```
line-height:1.2    span=44     <- identical to no declaration
(no line-height)   span=44
line-height:1      span=44     <- identical again
line-height:3      span=44     <- identical again
line-height:40px   span=56     <- works
```

### Root cause

Two places, and both must change:

- `pkg/css/value.go` `parseLength` **explicitly rejects** a non-zero unitless number:
  `return Length{}, false // non-zero unitless is not a valid length`. Correct for
  `width`, wrong for `line-height`, where a number is the *most common* spelling and
  means "multiply the font size".
- `pkg/layout/css/inline.go` `resolveLineHeight` switches on `UnitPx`/`UnitPt`/`UnitEm`
  and sends everything else — including whatever a number would parse as — to
  `autoLineHeight`, which is the font-metric height. That is exactly the reported
  "~1.64× no matter what".

### Fix

Add a `UnitNumber` to `pkg/css`, produced by `parseLength` only where a bare number is
meaningful. Rather than making every consumer of `Length` handle it, gate it at the
property: `line-height` (and later `flex-grow`-style values, if any) opt in via a
`parseNumberOrLength` helper, so `width: 5` keeps failing as it must.

Then `resolveLineHeight` multiplies: `UnitNumber` → `lh.Value * fontSizePt`.

Note the inheritance subtlety worth getting right: a unitless `line-height` inherits the
**number**, not the computed length, so a child with a larger font gets a proportionally
larger line box. An `em` value inherits the computed length. `firstInlineLineHeight`
already carries the child's own font size, so this falls out — but it needs a test,
because getting it backwards is invisible until a nested font-size change appears.

### Tests

Unit: `parseLength` still rejects `width: 5`; the number form resolves for
`line-height`. Layout: row height scales with the multiplier (1, 1.2, 3 must differ);
`px`/`em` unchanged; the inheritance case above. Golden: a stacked-rows page.

**STATUS: DONE.** Showcase §37.

Two things found while doing it:

- A test named `TestUnitlessLineHeightDeferred` documented the old behaviour as
  intentional and *predicted this exact fix* ("it needs a UnitNumber/multiplier concept
  the layout engine resolves against font size"). Updated to assert the new contract.
- `em` and `%` were being stored unresolved and multiplied against the ELEMENT's font
  size at layout time, which made them behave identically to a number on inheritance.
  Per CSS 2.1 §10.8.1 they must compute against the declaring element, so they are now
  resolved in the cascade. That was a pre-existing divergence, not something the
  unitless work introduced, but it sits in the same three lines.

Goldens moved across the showcase: its CSS uses unitless values that were being
ignored, so the leading tightened where the stylesheet always asked for it. Verified by
diffing page 0 before and after — identical content, correct leading.

---

## 8. `margin` is ignored on flex children

**Priority 2.** Also silent, and the report's own note — "easy to misread as *the rule
did not apply*" — is the cost.

### Measured

```
block parent,     margin-top:40px   -> child top at y=41   (correct)
flex column,      margin-top:40px   -> child top at y=1    (ignored)
flex row,         margin-left:60px  -> child left at x=0   (ignored)
flex row,         gap:60px          -> child left at x=61  (works)
```

### Correction to the report

The report contrasts a plain block (40px gap) against a flex column (no gap). But a
**bare** `margin-top` on a first child collapses through the body — measured `(0,0)` —
which is correct CSS, not a bug. The contrast only holds once collapsing is blocked
(padding on the parent), and then the block case is right and the flex case is wrong.
Stating it precisely matters because "margin is broken" would send someone to the block
layout code, which is fine.

### Root cause

`pkg/layout/css/flex.go` never reads `Margin` at all — `grep Margin flex.go` returns
nothing. Flex item main-size and cross-position are computed from the item's border box
with no margin contribution.

### Fix

Resolve each item's margins once, then thread them through:

1. **Main axis:** the item's outer hypothetical size includes its main-axis margins, so
   free-space distribution and `justify-content` account for them.
2. **Cross axis:** `align-items`/`align-self` position the *margin* box, and
   `stretch` resolves against the container less the cross margins.
3. **`margin: auto`** in flex absorbs free space (CSS Flexbox §8.1) — the idiomatic way
   to push one item to the end. Worth doing in the same pass since the plumbing is the
   same, but if it proves large it can be a follow-up, stated in FEATURES.md rather
   than left implicit.

### Also here: `transform` is unimplemented

The report bundles `transform: translateX()` with this, but it is a separate gap:
measured, a `translateX(60px)` on a plain **block** also does nothing, so it is not
flex-specific. `transform` is a whole property (matrix composition, transform-origin,
its effect on the containing block for descendants) and belongs in its own branch. It
should be recorded in FEATURES.md as absent rather than silently doing nothing.

### Tests

Margin honoured on a flex child in both axes and both directions; `auto` margins
absorbing free space; `gap` still working (it does today — do not regress it); a block
control proving collapsing behaviour is unchanged.

**STATUS: DONE.** Showcase §38. `margin: auto` landed in the same pass rather than as a
follow-up — the plumbing was the same once margins reached free-space distribution.
`transform` was split out into its own item (6 in the table below) and has since landed —
see FEATURES.md and showcase §42.

---

## 10 / 10b / 11 / 12: absolute positioning and flex containers

These four are one cluster: **a flex container mis-handles boxes that should not be
flex items, and flex-derived heights do not resolve.** Grouping them is not tidiness —
they touch the same code and fixing one in isolation risks contradicting another.

### 10. `top` + `bottom` does not size a box

Measured, in a 120px-tall relative parent:

```
top:0; bottom:20px   ->  totalInk=0     (nothing painted)
top:0; height:100px  ->  totalInk=5000  (renders)
left:0; right:20px   ->  renders        (the horizontal pair works)
```

The horizontal pair (`left`+`right`) resolves a width correctly, so the machinery
exists; the vertical equivalent does not. Per CSS 10.6.4, with `height: auto` and both
`top` and `bottom` set, the used height is the space between them.

**Fix:** in the absolute-positioning pass, resolve height from `top`/`bottom` when
`height` is auto and both offsets are set — mirroring what the width path already does.

### 10b. An abs-positioned child of a flex container ignores `left`

```
plain block parent  ->  red at x=300..339   (correct)
display:flex parent ->  red at x=0..39      (pinned to the edge)
```

Per CSS Flexbox §4.1 an absolutely-positioned child of a flex container is **not a flex
item**: it is taken out of flow and positioned against the container's padding box. This
engine lays it out as a flex item instead.

**Fix:** skip `position: absolute`/`fixed` children when collecting flex items, and hand
them to the same absolute-positioning pass a block container uses. This likely also
fixes part of 10 for flex parents.

### 11. A flex-derived height does not resolve `justify-content`

```
explicit height:200px            ->  child top at y=90   (centred, correct)
parent align-items:stretch       ->  child top at y=0    (ignored)
own flex:1                       ->  child top at y=0    (ignored)
```

`justify-content` itself works — the explicit-height row proves it. The failure is
specifically that a height *arrived at through flex layout* is not available when the
element lays out its own children, so it behaves as auto-height.

**Fix:** ensure a flex item's resolved cross size is written back to the box before its
interior is laid out, so the nested formatting context sees a definite height. This is
the "two-pass" shape flex already needs for stretch; the bug is that the second pass
does not propagate.

### 12. An absolutely positioned box never shrink-wraps

```
abs, left:110px             ->  span 110..181  (72px — stretched to the right edge)
abs, left:110px, width:76px ->  span 110..185  (76px — width IS honoured)
inline-block                ->  span 0..71     (72px — correct shrink-to-fit)
```

**Correction to the report:** it claims `width` is ignored on a positioned box. It is
not — the measured span is exactly 76px. The real gap is narrower: with `width: auto`,
an absolutely positioned box must **shrink-to-fit** (CSS 10.3.7), and here it stretches
to the containing block instead.

**Fix:** use the existing shrink-to-fit path (`inlineBlockCBWidth` computes exactly this
for inline-blocks) when an abs-positioned box has `width: auto` and is not constrained
by both `left` and `right`.

**Does not reproduce:** the report's second claim, that a flex child does not
shrink-wrap. Measured 117px for a 117px string, identical to `inline-block`. Either it
was fixed already or the original probe measured something else — no work needed, and
the plan should say so rather than "fixing" a non-bug.

**STATUS: DONE** for 10, 10b and 11. Showcase §41, zero golden movement.

**12 needed no work.** Re-measured on this branch, an absolutely positioned box around
a 72px string comes out 72px — identical to `display: inline-block` — and an explicit
`width: 76px` is honoured exactly. `absWidthShrinksToFit`/`absShrinkToFitWidth` already
implement CSS 10.3.7. The report's "width ignored" row is wrong, and so was my own
first reading of it: I recorded shrink-to-fit as the real gap, when in fact neither
half reproduces. The measured spans in the report were probably the enclosing card's
edge rather than the box's.

### Tests for the cluster

Each of the four gets its own assertion, plus the controls that already pass
(`left`+`right` sizing, explicit-height `justify-content`, `width` on a positioned box)
so a fix cannot regress them. A WPT-style reftest for the abs-in-flex case would be
worth adding to the existing reftest corpus.

---

## 13. A comma-separated `background` list is dropped

### Measured

```
background: linear-gradient(...)                    -> renders red
background: linear-gradient(...), #0000ff           -> WHITE (nothing)
background: linear-gradient(...), linear-gradient() -> WHITE (nothing)
background-color + background-image (split)         -> renders red
```

The failure is silent and total, and — as the report notes — adding the fallback colour
that every browser wants is exactly what breaks it.

### Root cause

The `background` shorthand parser accepts a single layer. A comma makes the value
unparseable, so the whole declaration is dropped per CSS error handling, taking the
colour with it.

### Fix

Parse `background` as a **layer list**: split on top-level commas, parse each layer,
and take the colour from the **final** layer only (CSS Backgrounds §3.10 — a colour is
only allowed in the last layer). Then either:

**STATUS: DONE**, maximal. Showcase §39.

One trap found while doing it, worth recording because it was silent: an early version
captured `background-size` and the other longhands into each layer record when the
image list was PARSED. Those are separate declarations that may appear either side of
`background-image`, so a size declared afterwards was discarded — `background-image`
then `background-size` lost the size, while the reverse order worked. Caught by two
existing SVG-background tests. The per-layer records now carry only the image, and the
layout side reads the final computed longhands, so declaration order stops mattering.

**DECIDED: the maximal version.** Paint all layers, first on top, with
`background-image` taking the same list. Layering is the point of the property, and
doing half of it silently is the failure mode being fixed. This means `BgImage` becomes
a slice and the paint pass emits one item per layer, back to front — a larger change
than honouring only the first image, and the right one.

### Tests

Each row of the measured table; a two-gradient stack painting both; the colour coming
from the last layer only; an invalid layer dropping the whole declaration (not just
that layer); `background-image` with a list.

---

## 2b. SVG presentation attributes do not inherit

### Measured

```
<svg stroke="#f5a623" stroke-width="12"><path .../></svg>   -> WHITE  (nothing)
<svg><path stroke="#f5a623" stroke-width="12" .../></svg>   -> orange (correct)
<svg fill="#f5a623"><circle .../></svg>                     -> BLACK  (not inherited)
```

**Correction to the report:** it presents this as a stroke problem. `fill` on the root
fails identically — the circle paints black, the SVG default — so it is inheritance in
general, not one property.

### Root cause

`pkg/svg`'s `rootStyle` deliberately copies only a fixed subset of the root's resolved
properties (font family/size/weight/style, text-anchor, direction) into the inherited
style. `fill`, `stroke`, `stroke-width`, and the rest of the paint vocabulary are
resolved for the root element itself and then **discarded** before its children inherit.

I touched this function in the SVG host-cascade branch (#132) to seed `color`/font from
the host, so the shape of the fix is familiar.

### Fix

Carry the full set of *inherited* SVG presentation properties from the root into the
child style, not a hand-picked subset. Per SVG 1.1, `fill`, `stroke`, `stroke-width`,
`stroke-linecap`, `stroke-linejoin`, `stroke-dasharray`, `fill-rule`, `fill-opacity`,
`stroke-opacity`, and the text properties are all inherited; `opacity`, `transform`,
`clip-path`, `mask`, and `filter` are **not**, and must stay excluded — inheriting
those would double-apply them.

The safe construction is to invert the current one: inherit everything the property
table marks inherited, and list the non-inherited exceptions explicitly, so a property
added later defaults to the spec's answer rather than to "dropped".

### Tests

Each inherited paint property set on the root and on an intermediate `<g>`, with a
child that declares nothing; a child override still winning; a non-inherited property
(`opacity`, `transform`) NOT double-applying — that last one is the regression risk and
needs its own assertion.

**STATUS: DONE.** Showcase §40.

`rootStyle`'s own doc comment had already identified this as "a genuine, pre-existing
gap" and predicted that fixing it would move goldens, recommending it be done as "a
separate, self-contained change so its golden movement can be reviewed on its own".
It moved **zero** goldens — the whole resvg conformance corpus passes unchanged, which
is the strongest available evidence the inherited/non-inherited split is right, since
those fixtures exercise inheritance directly.

---

## 14. `writing-mode` is ignored (as of this audit — it has since shipped; see the status below)

### Measured

```
horizontal   firstInk=7 lastInk=21  totalInk=710
vertical-rl  firstInk=7 lastInk=21  totalInk=710   (byte-identical)
```

Confirmed unimplemented.

### Assessment

This is the largest item here and the least like the others. Vertical writing modes
touch the whole inline layer: the block/inline axes swap, so line breaking, alignment,
`text-orientation`, glyph rotation and upright forms, and every place the engine assumes
"inline is horizontal" are involved. It is a sub-project comparable to the RTL work
already in the tree, not a fix.

**DECIDED at the time: deferred, and recorded in `docs/SCOPE.md` rather than silently
absent.** The reporter's workaround — one `<span>` per letter, each a fixed-height block —
was noted beside the gap as a stopgap.

**SUPERSEDED — it was built instead.** Vertical text ships: `writing-mode` and
`text-orientation` on the HTML path, and on the SVG path for `<text>` (FEATURES.md).
The measurement above records the behaviour BEFORE that work and no longer holds. What
remains is ordinary outstanding work — chiefly the UAX #50 `Vertical_Orientation` table
for `text-orientation: mixed` — tracked in `docs/CSS-LAYOUT.md`, not a scope exclusion.
`docs/SCOPE.md` now says exactly this; the only genuinely excluded piece is
vertical GLYPH FORMS, which needs GSUB feature application this engine does not do.

---

## 9 and 12b: do not reproduce

Both were re-measured on the current tree and behave correctly:

- **9, `z-index` ordering:** a `z-index: 30` element paints above a later sibling with
  no `z-index`, in both markup orders. The engine has full Appendix E stacking
  (`pkg/layout/css/fragment.go`), added before this report.
- **12b, flex child shrink-wrap:** a flex child sized to a 117px string measures 117px,
  identical to `inline-block`.

No work planned. Both are worth re-probing on the reporter's side against a current
build — the report was written against `fb42ebe`, which predates several fixes.

---

## Sequencing

Per `CLAUDE.md`, each item is its own branch → PR off `main`, merged when CI is green.
Ordered by (silent × common) first, since a silent failure costs the most.

| Order | Branch | Items | Why here |
| --- | --- | --- | --- |
| 1 | `fix/unitless-line-height` | 7 | **DONE** — showcase §37. |
| 2 | `fix/flex-child-margins` | 8 | **DONE** — showcase §38. `transform` split out, still pending. |
| 3 | `feat/background-layer-list` | 13 | **DONE** — maximal (all layers painted). Showcase §39. |
| 4 | `fix/svg-presentation-inheritance` | 2b | **DONE** — showcase §40, zero golden movement. |
| 5 | `fix/abs-position-in-flex` | 10, 10b, 11 | **DONE** — showcase §41. 12 needed no work. |
| 6 | `feat/css-transform` | (from 8) | **DONE** — showcase §42. 2D functions only; no 3D, no `transform-origin`. |
| — | (docs only) | 14 | **Superseded** — deferred here, then built. Vertical text ships; the rest is in CSS-LAYOUT.md. |

Docs alongside: FEATURES.md gets an entry per landed item, including the honest limits
(one gradient layer vs many; `transform` absent; `writing-mode` absent). Each branch
adds its case to the `testdata/htmldoc/` showcase per the repo's directive.

## Cross-cutting note

Seven of these nine are **silent**: the declaration parses, the page renders, the exit
status is zero, and the result reads as a styling mistake. That is the same pattern as
the first batch, and the same lesson applies — where the engine cannot honour a
property, the useful behaviours are, in order: implement it; refuse the declaration so
the previous value stands; or log once. Doing none of those is what turns a one-line CSS
fix into an afternoon.

Worth noting for the reporter: three of the nine diagnoses pointed at the wrong
mechanism (7 is unitless-only, 8 is flex-only, 12 is shrink-to-fit not `width`), and two
do not reproduce at all. The measurements were sound; the inferences from them were not
always. Probes that isolate one variable — the `line-height: 40px` control, the
block-vs-flex margin pair — would have caught all three.
