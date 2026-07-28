# RTL slice 5 — PDF extraction visual→logical (2026-07-28)

## What shipped

Text extracted from a PDF containing right-to-left script now comes out in
**logical** order — the order it would be typed and read — instead of reversed.

This is the last of the five RTL slices, and the only one independent of the
others: it branches from `main` rather than the layout stack, because
`pkg/pdf/extract` does not depend on `pkg/layout/inline`.

## The problem

A PDF stores glyphs by **position**, not reading order. A right-to-left word is
painted with its first character at the largest x, so extraction — which sorts a
line's glyphs left-to-right — yields it backwards. Reproduced before fixing:
`אבג` extracted as `גבא`.

## Why this is not just "run the bidi algorithm"

The inverse of UAX#9 rule L2 cannot be obtained by running the bidi algorithm over
the extracted text, because that text is **already scrambled**. The algorithm's
input must be logical order, which is exactly what is missing — feeding it visual
order produces a second scramble, not a repair.

What *can* be recovered is the run structure. A maximal run of right-to-left text
was laid out right-to-left, so reversing each such run restores logical order. That
is exact for the common cases (a right-to-left phrase in a left-to-right line and
vice versa) and matches what PDF viewers do when copying text.

## Two levels of reversal

The PDF mirrors right-to-left text at both levels, so both are undone:

1. the characters **within** each right-to-left word, and
2. the **order** of consecutive right-to-left words.

Undoing only the first leaves a multi-word phrase with its words backwards.
Mutation testing pins this: removing the word-order reversal fails a *different*
test than removing the reordering entirely, so the two levels are independently
necessary and independently covered.

## Ordering constraint: after word grouping, not before

The reorder runs on assembled **words**, not raw glyphs. Word grouping splits on
the x-gap between a glyph and the previous glyph's right edge, which assumes
ascending x — reordering glyphs first would produce negative gaps and break the
split entirely.

Working at the word level also means each word keeps the geometry (`x0`/`x1`)
that downstream table recognition and block detection key off. The reorder changes
reading order, not where anything was painted, and a test pins that the geometry
follows its word through the swap.

## Classification

A word is right-to-left when it holds at least one strong RTL character and no
strong LTR one. Neutrals — spaces, punctuation, digits — take their direction from
context and never make a word RTL on their own, but they do not make a Hebrew word
LTR either (`שלום7` is still RTL).

A word mixing scripts (a transliteration such as `שlom`) is treated as
left-to-right and left alone: reversing it would scramble the Latin part, and
guessing wrong in that direction is worse than leaving it.

Classification happens **once**, before any reversal. Reversing a word's text does
not change its character classes, so re-testing afterwards would give the same
answer — but relying on that would silently couple run detection to the spelling
step above it.

## No-op for Latin

A line with no right-to-left word returns untouched. That keeps every existing
extraction golden byte-identical, which the full suite confirms: zero golden churn.

## Known limits

- **Numbers inside right-to-left text.** European digits are *weak*, not neutral,
  in UAX#9: they render left-to-right even inside an RTL phrase. A word that is
  purely digits is left alone (correct), but a digit sequence embedded mid-word
  reverses with it. Rare in practice and would need per-character class tracking
  to fix properly.
- **Nested embeddings.** Only one level of run structure is recovered. Text with
  explicit embedding controls that nested more deeply extracts approximately.
- **No `/ReversedChars` handling.** PDF has a marked-content mechanism for
  declaring that a span was written in reverse; it is not read. The heuristic above
  covers the same ground without it, and files using it are rare.
