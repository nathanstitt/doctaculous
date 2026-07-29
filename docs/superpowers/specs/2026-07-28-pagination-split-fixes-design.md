# Pagination split fixes — N1b (2026-07-28)

## What shipped

Three latent correctness bugs in the page-splitting layer, each reproduced before
being fixed. No new feature: this is the correctness debt that sat under backlog
N1, separated out because it is cheap, independently valuable, and de-risks the
recursive splitting that N1a will add on top.

## Bug 1 — out-of-flow children were dropped

`splitMixedBlock` rebuilt both fragments' child lists from `inFlowChildren`, which
filters out floats and positioned boxes. Neither fragment got them, so **the
content simply disappeared from the document** — silently, with no log.

Measured on a four-child fixture: 2 of 4 children lost, one float and one
absolutely-positioned box.

Fixed by `distributeOutOfFlow`, which routes each out-of-flow child to the fragment
whose band contains it. A child straddling the boundary rides the tail whole,
matching how a straddling in-flow child is already handled.

**The threshold matters and I got it wrong first.** Routing by `tail.Y` puts a box
in the head whenever it sits in the gap between the page boundary and the tail's
first in-flow child — the tail starts at its first child, which can be well below
the break. Routing by `pageBottom` is correct. The test caught it.

Note the two pre-existing splitters disagreed on policy: `splitHead` keeps
out-of-flow children with the head ("they were positioned in this block's space"),
while `splitMixedBlock` dropped them. Neither is right in general; geometry decides.

## Bug 2 — head and tail shared mutable paint state

A split is `head := *b; tail := *b`. That copies the `BgImage` **pointer** and the
`ClipChain` slice header — and `shiftFragmentExtras` (`block.go`) moves a fragment
into its page's local frame by mutating exactly those **in place**.

So shifting the head also moved the tail's background origin, by the head page's
offset, on top of the tail's own shift. Measured: an origin of 7 became 1007 after
the head was shifted by 1000. The continuation page paints its background in the
wrong place.

`detachSharedExtras` deep-copies `BgImage`, `PositionedInfo` (including each
entry's `ClipChain`), and `Collapsed`. A fragment with none of these — the common
case — allocates nothing.

## Bug 3 — a split clipping box clipped to its pre-split extent

`ClipRect` is a value, so the halves do not alias, but each inherited the **whole
original box's** rect. A split `overflow: hidden` block therefore clipped to the
full pre-split height on both pages, letting content belonging to the other
fragment paint through.

`clampClipToFragment` narrows the rect to the fragment's own vertical extent. Only
vertically: a page break divides a box along the block axis, so the horizontal clip
is unchanged.

## Where the fixes live

All three apply at the **`splitAnyBlockForPage` dispatch point**, not inside the
individual splitters. Every split already flows through it, so a future splitter
cannot forget to detach or clamp.

The dispatcher gained a thin wrapper (`splitOneBlockForPage` keeps the shape
dispatch) and applies the fixes only when both `head` and `tail` are present — a
"fits whole" or "moves whole" result hands back the **original pointer**, which the
bucketer relies on for identity and which must not be copied. That is pinned by
`TestSplitWholeDoesNotCopy`.

## Testing

Each bug has a regression test that reproduces the original symptom, and all three
are **mutation-verified with a clean one-to-one correspondence**: removing the
out-of-flow redistribution fails only `TestSplitMixedKeepsOutOfFlowChildren`,
removing the detach fails only `TestSplitDetachesSharedExtras`, removing the clamp
fails only `TestSplitClampsClipRect`. No overlap, so each fix is independently
necessary and independently covered.

Zero golden churn — these are latent bugs that no existing fixture triggered, which
is precisely why they survived.

## What this does NOT do

Mid-box splitting itself. A block whose child straddles the page boundary still
pushes that child whole to the tail, leaving a gap on the head page. That is N1a,
and it is now unblocked: the shared-state and content-loss hazards that recursive
splitting would have multiplied are gone.

## Correction to the backlog's framing

N1 said mid-box splitting "breaks the post-pass model" because state was already
resolved. That is imprecise. Heights, borders, and glyph positions are all cheaply
re-derivable — the existing splitters do exactly that, and the whole tree lives in
one page-space coordinate system, which makes relocation cheap by design.

The real blocker is **recursion**: all four splitters are flat, one-level
partitions, and none calls back into `splitAnyBlockForPage`. Splitting a nested
spine needs a different algorithm shape, not more state. The one genuine
"insufficient state" case is margin-collapse at the split point, which is consumed
during layout and never recorded on the fragment — a single float field, not an
architecture change.

`paginateRuns` is also worth knowing about: it **already re-runs `layoutTree`** per
distinct `@page` width and caches the results. Relayout during pagination is
therefore already architecturally acceptable here; only the height axis has never
been re-fed.
