# RTL slice 3 — inline bidi reordering (2026-07-27)

## What shipped

Text within a line is now reordered from logical to visual order per UAX#9, so a
right-to-left phrase reads right-to-left on the page. This is the piece that
earned backlog A1's "Large" label; slices 1 and 2 moved *boxes*, this one moves
*glyphs*.

Also in this slice: **backlog H4**, the column-flex vertical content measurement,
which was requested alongside and shares no code but was blocking its own item.

## The logical/visual split

The load-bearing design decision. Shaping and line breaking stay in **logical**
order — the order runes appear in the source — because every operation they
perform is logical: breaking at a space, measuring a width, carrying a tail to
the next line, and `lastSpaceBefore` scanning backwards for a break opportunity.

Reordering to **visual** order happens per line in `MakeVisualLine`, *after* the
break is chosen. That ordering is forced by the spec: rule L2 reorders within a
line, and which glyphs share a line is only known once breaking has run. It also
happens to be the cheapest place — one chokepoint that every emitter already
calls, so neither `Break` nor the two glyph emitters needed changes.

### The metrics trap

`Line.WidthPt` and the justification gap count exclude the run of spaces that
**ends** the text, and `VisibleWidth`/`CountSpaces` find that run by scanning
from the end of the slice. That is correct only while the slice end *is* the
text end.

In a reordered RTL line, the trailing space sits at the visual **start**.
Measuring the reordered slice would therefore count it toward the line's ink
width and justify against the wrong gap count. So `MakeVisualLine` computes the
metrics from the logical slice and transplants them onto the reordered one, and
the CSS emitter counts justification gaps on `lineGlyphs` (logical) rather than
`line.Glyphs` (visual). Pinned by `TestMakeVisualLineMetricsAreLogical`.

## Dependency

`golang.org/x/text` moves from indirect to **direct**. No new module: the
complete UAX#9 implementation, including the bracket-pair algorithm (BD16/N0),
was already in the module graph. It is BSD-licensed and pure Go, so it satisfies
the project constraints without further review.

x/text resolves the directional runs; **rule L2 is applied here** because
x/text's own display-order `Reorder` method is unimplemented upstream (commented
out in `bidi.go`).

## Rule L4 — bracket mirroring

A bracket inside an RTL run is *drawn* mirrored: `(` renders as `)`. Implemented
via `unicodedata.LookupMirrorChar` (from `textlayout`, already a direct dep),
re-resolving the outline and GID from the face.

`Glyph.Runes` deliberately keeps the **original** character. The text content is
unchanged — only its rendered form mirrors — so the PDF writer's `/ToUnicode`
still recovers what the author wrote. A glyph whose mirror the face cannot
resolve keeps its original shape, which is the right degradation: still legible,
just not mirrored.

## Bug found: bidi controls were being dropped

`Shape` discarded every rune the face could not map (`if !ok { continue }`).
That includes the invisible bidi formatting characters — LRM/RLM/ALM, the
embedding and override set, and the isolates — which draw nothing but
**determine ordering**. Discarding them silently threw away the author's
directional intent before the reorder could ever see it.

They now survive shaping as zero-width, face-less glyphs. Found by an end-to-end
test that failed for a reason I had not predicted, which is the value of testing
through the real path rather than only the unit.

## Testing, and the font constraint

**UPDATE (2026-07-28):** Noto Sans Hebrew and Noto Naskh Arabic now ship bundled,
reached by per-rune script fallback, so the end-to-end tests below were converted
to assert on real Hebrew and Arabic. The RLO-over-Latin test is kept because it
isolates the reordering machinery from font coverage entirely.

At the time this slice landed, no bundled face covered Hebrew or Arabic and
`Shape` dropped unmappable runes — so **RTL script never reached the reorder as
glyphs**, and depending on a system font would have broken hermeticity. The tests
split accordingly:

- **Unit** (`pkg/layout/inline/bidi_test.go`) — real Hebrew and Arabic, asserted
  on glyph ORDER via `Runes` using synthetic glyphs. No font needed, because
  reordering is a question of character properties. Covers an RTL island in an
  LTR paragraph, an LTR island in an RTL paragraph, a wholly RTL paragraph, the
  atomic/neutral cases, and a multiset check that no glyph is dropped or
  duplicated in any combination.
- **End-to-end** (`pkg/layout/css/inline_test.go`) — drives the whole path (box
  generation → shaping → breaking → reorder → glyph emission) with **U+202E
  RIGHT-TO-LEFT OVERRIDE over Latin characters**, which produces a genuine RTL
  context with renderable output. Asserts the painted order by sorting emitted
  glyphs on their X position.
- **Mirroring** — shaped through a real bundled face, since L4 must re-resolve an
  outline. Mutation-verified: without mirroring the bracket GIDs come out
  swapped.

Every fix in this slice is mutation-verified. Reverting the emitter to
`MakeLine` fails both end-to-end tests; removing the mirroring fails the L4 test.

## Not in this slice

- **Arabic shaping** (slice 4). `Glyph.Runes` is hardcoded to one rune per glyph
  in `Shape`, so there is no cluster model — Arabic contextual forms
  (initial/medial/final/isolated) and the lam-alef ligature have nowhere to live.
  Arabic therefore reorders correctly but renders as disconnected isolated forms.
  Fixing it means adopting harfbuzz and making `Face` expose GSUB/GPOS.
- **PDF extraction visual→logical** (slice 5). Independent.
- **Nested embeddings deeper than one level.** x/text reports a flat list of
  directional runs rather than per-rune embedding levels, so an LTR island inside
  an RTL island inside an LTR paragraph reorders as a single level. The common
  cases — an RTL phrase in an LTR paragraph and vice versa, with bracket pairs —
  are exact.
- **A showcase entry.** Deferred when this slice landed for want of RTL glyph
  coverage. That blocker is now gone (see the update above), so the showcase can
  and should demonstrate real reordering — worth doing alongside slice 4.
