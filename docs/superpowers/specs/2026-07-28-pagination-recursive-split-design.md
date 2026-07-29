# Recursive spine splitting — N1a (2026-07-28)

## What shipped

A child straddling a page boundary is now **split** rather than pushed whole to the
next page. A `section > div > p` spine breaks at a line boundary inside the
paragraph, instead of moving the whole `div` and leaving the head page blank below
its last complete child.

This is what users perceive as "mid-box splitting" — the headline part of backlog
N1. It builds directly on N1b, which removed the content-loss and shared-state
hazards recursion would otherwise have multiplied.

## The problem, measured

`splitMixedBlock` partitioned a parent's children at the last one that fit entirely,
and the straddler rode the tail whole. On a fixture with a 50pt block followed by an
80pt paragraph and a boundary at y=100:

```
before:  head y=0 h=50  → 50pt of blank page below it
after:   head y=0 h=100 → reaches the boundary exactly
```

Five of the paragraph's eight lines fit on the head page and were being wasted.

## Why no signature change was needed

The exploration's key finding: `splitAnyBlockForPage(b, pageBottom, widows, orphans)`
was **already valid at any tree depth**. `pageBottom` is in absolute page space, and
the whole fragment tree lives in one coordinate system — so the dispatcher can be
called on a nested child exactly as it is called on a top-level block.

That is why this landed as a Medium rather than a Large. The abstraction was right;
nothing ever called it recursively.

```go
if straddler := inflow[k]; lineSplittable(straddler) {
    if sub := splitAnyBlockForPage(straddler, pageBottom, widows, orphans); sub.head != nil && sub.tail != nil {
        childHead, childTail = sub.head, sub.tail
    }
}
```

## The recursion guard

`lineSplittable` gates it, and is the right gate for two independent reasons:

1. It is a **shape predicate with no top-level assumption** — it asks whether the
   fragment has ≥2 lines, in-flow block children, or is a table/flex/grid container.
   All meaningful at any depth.
2. It already refuses a `break-inside: avoid` box, which is **exactly the stop
   condition CSS Fragmentation requires**. No new plumbing.

A straddler that is not splittable falls through to the previous behavior — it rides
the tail whole rather than being clipped.

## Geometry now derives from the result

Head and tail extents previously came from the k-th child directly:

```go
head.H = inflow[k-1].Y + inflow[k-1].H - parent.Y
tail.Y = inflow[k].Y
```

That is wrong once the straddler contributes a *head* to this page: the head extends
past `inflow[k-1]`. Both are now computed from the resulting child lists via
`lastChildBottom` / `firstChildTop`, which consider **in-flow children only** — a
float or positioned box does not contribute to its parent's block-axis extent.

## Testing

- **The headline case** — a `section > div > p` spine splitting mid-paragraph, with
  the gap asserted closed and the line count asserted conserved (no line dropped or
  duplicated across the halves).
- **`break-inside: avoid`** on the straddler stops the recursion.
- **An unsplittable straddler** (one tall line) still rides the tail.
- **Nested out-of-flow children** survive the recursive split — N1b's fix must hold
  at depth, not just at the top level.
- **End-to-end through `LayoutPaged`**, not just the splitter unit: a tall spacer
  pushes the boundary into a paragraph nested two levels deep, and both pages carry
  text. Before, page 1 held only the spacer.

Mutation-verified: removing the recursion fails the spine test, removing the
`lineSplittable` guard fails the `break-inside: avoid` test. Different tests, so both
are independently necessary.

Zero golden churn — no existing fixture had a spine straddling a boundary.

## Known limits

- **Margin-collapse state at the split point is still not threaded.** It is consumed
  during layout (`collapseMargins`' `in.trailingMargin`) and never recorded on the
  fragment, so a continuation fragment cannot know what margin was collapsed away at
  its top edge. This did not block the slice, and it is a single float field rather
  than an architecture change when it does.
- **Mid-cell and mid-flex-item splitting are unaffected** (N1c, N1d). A table cell
  or flex item taller than the page still moves whole, because splitting *within* one
  needs a height-budgeted relayout of its formatting context, not a fragment
  partition.
- **No `box-decoration-break: clone`.** Edge suppression is hardcoded to `slice`,
  which is the initial value; the property is not in `ComputedStyle` at all.
