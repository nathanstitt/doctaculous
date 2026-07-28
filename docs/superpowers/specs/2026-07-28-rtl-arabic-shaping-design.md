# RTL slice 4 — Arabic contextual shaping (2026-07-28)

## What shipped

Arabic renders with **connected letterforms**. Previously it reordered correctly
but drew as disconnected isolated letters, because the shaper emitted one glyph
per rune and an Arabic letter's form depends on its neighbours.

Complex-script runs are now shaped as whole segments through harfbuzz (the pure-Go
port already in the module graph via `textlayout`), which resolves the font's GSUB
tables to pick initial/medial/final/isolated forms and fuse ligatures.

## Scope: only scripts that need it

`needsComplexShaping` covers the Arabic-family joining scripts — Arabic, Syriac,
Thaana, and the Arabic presentation-form blocks. **Hebrew is deliberately not
included**: it is non-joining and shapes correctly rune-at-a-time.

Keeping the complex path narrow means every other script stays on the cheap loop
and its output stays byte-identical to before harfbuzz existed here. Zero golden
churn outside the intentional showcase change confirms it.

## The direction decision

The load-bearing choice. Harfbuzz emits **visual** order for RTL by default: a
probe of `مرحبا` returned clusters descending 4→0.

That would collide with the L2 reorder from slice 3 — the text would be reversed
twice and come out backwards. So `shapeComplex` forces
`buf.Props.Direction = LeftToRight`, keeping the whole pipeline in **logical**
order right up to the single reorder in `MakeVisualLine`. The contextual forms are
applied either way; only the output order differs (verified by probing both).

The alternative — letting harfbuzz reorder and suppressing L2 for those runs —
would have split the reordering across two mechanisms with different rules for
different scripts. One reorder, one place.

## Cluster attribution, and a bug the mutation testing found

Harfbuzz reports a cluster index per glyph: the offset of the first rune it came
from. Several glyphs sharing a cluster decompose one character (a base plus
marks); one glyph spanning several positions is a ligature. Exactly one glyph per
cluster carries the runes, so a backend mapping glyphs back to text (the PDF
writer's `/ToUnicode`) neither duplicates nor drops characters.

The first implementation found the cluster start by comparing against the
*previous* glyph — which assumes ascending clusters. That is true only because we
force LTR, so the code was correct but silently coupled to a decision made
elsewhere. Mutation testing exposed it: dropping the LTR force made every leading
glyph lose its runes and the final glyph swallow the entire string.

Worse, the test did not catch it, because it only asserted that the concatenation
equalled the source — which still held. `clusterRunes` now computes the extent
without assuming an order, and the test additionally asserts the runes are
**spread across glyphs** rather than collapsed onto one.

## Where the fallback interacts

A run's family often has no Arabic coverage (a `serif` paragraph containing an
Arabic phrase). Shaping resolves the covering bundled face *before* handing the
segment to harfbuzz, so GSUB comes from the face that actually has the glyphs.

## Testing

- **Contextual forms** — the same letter alone vs. mid-word must produce different
  GIDs. Mutation-verified: disabling the complex path makes them identical, which
  is precisely the disconnected-letters bug.
- **Cluster attribution** — every source rune appears exactly once, in order, and
  spread across glyphs. Covers a real ligature (`لا`, lam-alef).
- **Logical order** — the shaped runes must come back in source order, pinning the
  direction decision.
- **Scope** — Hebrew and Latin must *not* take the complex path.

## Showcase

Section 15 gains a "Real script" page: Hebrew reordering, an RTL island inside
Latin with mirrored brackets, and Arabic with connected forms. The Latin
box-mirroring demos above it stay Latin, since mirroring a table's column order is
easier to see with familiar glyphs.

Two content fixes while eyeballing the render:

- The real-script demos moved to their own page. Kept together, the 20px Arabic
  sample pushed the last line under the footer.
- Demo labels were reworded to avoid trailing neutral punctuation. `2fr (left)` in
  an RTL container renders as `(2fr (left` — which is **correct** UAX#9 (rule L1
  resolves trailing neutrals to the paragraph direction, and `x/text` classifies
  them the same way), but it reads as a bug in a demo about box mirroring.

## Known limits

- **Vertical positioning from GPOS is not applied.** Only the X advance is read,
  so a mark that needs a Y offset sits on the baseline. Arabic diacritics are the
  visible case.
- **One face per segment.** A complex segment that mixes scripts resolves its
  face from the first rune.
- **No `font-feature-settings`.** Harfbuzz is driven with default features; the
  CSS property is not plumbed through.
