# RTL slice 2 — box-level mirroring (2026-07-27)

## What shipped

`direction: rtl` now mirrors the inline axis for tables, flexbox, and grid. This
retires **all three** "laying out LTR" degradation logs — the sole RTL deferral in
each of those three modes (backlog H2, I3, and the table entry, all marked
"covered by A1").

Slice 2 of 5. Depends on slice 1's `effectiveDirection`
(`2026-07-27-rtl-cascade-design.md`).

**This is box-level only.** Text within a line is still not reordered, so a
right-to-left script renders in logical glyph order. That is slice 3.

## Tables

The solved column x-offsets are mirrored about the grid span after they are
computed, so column 0 lays out right-most. Everything downstream reads
`cols[ci].x` and is geometry-driven, so cell placement and
`colsRect`/`backgroundLayers` need no changes.

### The collapsed-border trap

`buildCollapsedBorders` (`tableborder.go`) mixes **grid-index** reasoning with
**physical geometry**:

- `gc.col == 0` → "resolve against the table's LEFT border"
- `cellAt(gc.row, gc.col-1)` → "my LEFT neighbor"
- `emit(..., x, ...)` → draws at the fragment's actual x

Those agree only under LTR. With a mirrored grid, column 0 sits at the **largest**
x, so without a matching flip the table's left border is resolved onto the
right-most cell and every interior grid line resolves its conflict against the
wrong neighbor — picking the wrong winner among competing border widths/styles.
Nothing logs; the borders are simply wrong.

The fix flips the index→physical-side mapping under RTL: the cell physically to
the left is the *next* one in grid order, and the outer edge reached at the left is
the grid's *end*.

This was the highest-risk item in the slice because it is invisible in the common
`border-collapse: separate` case and has no failure signal. Covered by
`TestRTLCollapsedBordersMirror`, which asserts the RTL strips are the exact mirror
of the LTR ones (same widths, mirrored centers) and is mutation-verified.

**Note on the assertion:** compare grid-line *centers*, not strip left edges. Each
strip is drawn centered on its line (`x = line - width/2`), so a wider strip starts
further left; comparing left edges reports spurious 1–3px mismatches.

## Flexbox

Resolved in `axisFor(dir, direction)`, so every existing caller is unchanged.

- **Row**: the main axis *is* the inline axis, so RTL is exactly
  `reverseMain = !reverseMain`. `justifyOffsets` works in abstract main-axis terms
  and the placement loop applies the reverse formula uniformly, so `flex-start`,
  `flex-end`, `space-around`, and `space-evenly` all flip with it — no
  justify-content special-casing. Because it is an XOR, `direction:rtl` composes
  with `row-reverse`: both flips cancel and the result is LTR-row order.
- **Column**: the main axis is vertical and direction-independent, but the CROSS
  axis is the inline one — so RTL flips *that* (`reverseCross`), swapping
  cross-start and cross-end for `align-items`/`align-self`. The previous log was
  guarded by `&& !ax.vertical`, so this case was silently wrong rather than logged.
  Handling it means no silent RTL gap remains in flex.

Also fixed a pre-existing bug found here: `crossOffset` switched only on the
Flexbox `flex-start`/`flex-end` spellings, but the cascade also accepts the CSS Box
Alignment `start`/`end` for `align-items`/`align-self` — so `align-items: end`
silently fell through to flex-start.

## Grid

Grid needs **two independent flips**, and applying only one is the classic
double-mirroring bug:

1. **Track positions** mirror about the content width. Because the
   `justify-content` leading/between offsets are already baked into `colPos`, the
   distribution mirrors for free — no direction-aware `contentOffsets` or
   `trackPositionsDist` variant is needed. A spanning item's area left edge is
   derived from its start track's *right* edge minus the span width, since under
   RTL the start column of a span is its right-most track.
2. **`justify-items`/`justify-self`** `start`/`end` resolve logically within the
   area (under RTL, `start` is the area's right edge).

Both were mutation-verified **independently**: removing the track mirroring fails
`TestGridRTLMirrorsTracks` and `TestGridRTLSpanAndAlignment`; removing the
alignment flip fails only `TestGridRTLJustifyItemsStartIsRightEdge`. Different
tests catch different mutations, which is what proves the two flips are separately
necessary and separately covered.

## Testing

- **Exact-mirror assertions, not just reversal.** Table columns and grid tracks use
  distinct sizes (40/60/80 and 60/120) so an index reversal — which would put a
  narrow item into a wide slot — fails while a true mirror passes.
- **The flex double-negative**: `rtl` + `row-reverse` must equal plain LTR row
  order. A sign error that assigns rather than XORs `reverseMain` passes the
  single-flip test and fails this one.
- **Pagination**: an RTL table fragmenting across a page break keeps its mirrored
  column order on every fragment.
- **Two existing degradation tests inverted, not deleted**:
  `TestRTLTableDegradesGracefully` (asserted the log) and
  `TestGridRTLDegradesToLTR` (asserted LTR geometry) are now geometry assertions of
  the mirrored result, plus explicit no-longer-logs assertions so a regression that
  reintroduces the fallback fails loudly.
- **WPT reftests** (there were zero RTL cases before): `rtl-table`, `rtl-grid`,
  `rtl-text-align-end`, and `rtl-flex-row` — the last being the strongest oracle
  available, since `direction:rtl` and `flex-direction:row-reverse` reach the same
  placement through independent inputs.

## Showcase

`testdata/htmldoc/index.html` gains section 15, "RTL & Bidirectional Layout",
placed before the landscape section so it appends a page rather than reflowing
existing ones.

**It uses Latin text deliberately.** No bundled font (TeX Gyre Heros/Termes,
Inconsolata) carries Hebrew or Arabic coverage, so real right-to-left script would
rasterize as `.notdef` boxes and lock a meaningless golden. Latin text is also the
honest demonstration: what shipped is box-level mirroring, and glyph reordering has
not.

### Golden churn accounting

The page count goes 15 → 16, and **every** page's golden changes. That is expected
and was verified rather than assumed: `main.css` renders
`counter(page) " / " counter(pages)` in the `@bottom-center` margin box, so the
total in every footer changes. A per-page pixel diff confirms each pre-existing
page differs in exactly 7 rows at 96–97% down the page (the footer line), the
landscape page moved from p14 to p15 unchanged apart from its own footer/header,
and p14 is the new section.

## Known limitation carried forward

`direction` now moves boxes, but **not glyphs within a line**. Slice 3 brings
`golang.org/x/text/unicode/bidi` (already in the module graph) for UAX#9 level
resolution, the logical-vs-visual split in `Line.Glyphs`, per-line L2 reordering,
and bracket mirroring. Arabic joining needs a cluster model and is slice 4.
