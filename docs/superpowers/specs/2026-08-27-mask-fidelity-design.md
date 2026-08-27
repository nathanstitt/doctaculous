# Mask luminance fidelity, and the four "stale" goldens — Design

**Date:** 2026-08-27
**Status:** Approved design (autonomous), pending implementation
**Base branch:** `fix/mask-luminance`, off `main`

## What the investigation actually found

This work was queued as "the mask compositing gap + four stale goldens." Measuring
it on merged `main` split that into **two unrelated items, one of which does not
exist**, and corrected an earlier claim of mine.

### The four goldens are NOT stale in any meaningful sense

`-update` rewrites four PNGs on a clean tree, which is what made them look stale:

| Golden | Pixels beyond ±4 tolerance | Budget | Max channel delta |
|---|---|---|---|
| `docx-model-specimen.png` | 409 / 484,704 (0.084%) | 0.2% | 47 |
| `md-specimen.png` | 34 / 362,460 (0.009%) | 0.2% | 85 |
| `masking/mask/mask-on-self-with-mixed-mask-type.png` | 0 / 40,000 | 0.2% | **1** |
| `masking/mask/recursive-on-self.png` | 0 / 40,000 | 0.2% | **1** |

All four are **within tolerance**, and the two mask goldens differ by at most
**1/255** — imperceptible. Rendering is deterministic (6/6 identical hashes).

**Correcting an earlier measurement of mine:** I previously reported these at
0.13% and 7.19% "differing bytes." That was wrong. I compared raw PNG IDAT bytes
without reversing the per-row filters, and every row in these files uses a
non-zero filter (Paeth/Average/Sub/Up). Filter residuals are not pixel values,
and one changed pixel perturbs every residual after it — which is how a 1/255
difference looked like 7%. The numbers above come from a decoder that reconstructs
filters, and they agree with the test harness's own `compareImages`.

So there is **no golden-staleness bug**. `-update` rewriting a file that still
passes is just PNG encoder nondeterminism at the byte level over pixel-identical
output. That deserves a note in the testing docs, not a fix.

### The mask fidelity gap IS real, and it is not about recursion

`masking/mask/recursive-on-self` renders as a faint wash where resvg renders a
strong vertical green gradient. That is a genuine divergence.

But it is **not** a cycle-guard problem. Rendering the same fixture with the
recursive `mask=` attribute removed — leaving one ordinary, non-recursive mask —
still produces near-white output (sampled `{192 223 192}` at the midpoint, and
symmetric top-to-bottom where resvg's is a directional ramp). The recursion is
incidental. **Ordinary mask luminance is wrong**, and the recursive fixture is
merely where it happened to get noticed.

That is a far better-scoped problem than "mask compositing is broken."

## The question this PR must answer

The fixture's mask content is a rect filled with a gradient running
`white @ stop-opacity 0` → `black @ opacity 1`.

Under the luminance formula the engine implements — Rec. 709 coefficients on sRGB,
multiplied by the pixel's own alpha (`pkg/render/raster/device.go:436`) — that
gradient yields near-zero coverage almost everywhere: the white end contributes
high luminance but **zero alpha**, and the black end contributes full alpha but
**zero luminance**. Near-total masking is the arithmetically correct result of
that formula, and it is what we render.

resvg renders a strong gradient from the same input. So one of these is true, and
the PR's first job is to determine which **by experiment, not by reading the spec
and guessing**:

1. **Gradient interpolation of a transparent stop.** Interpolating
   `rgba(255,255,255,0)` → `rgba(0,0,0,255)` in *straight* (non-premultiplied)
   space sweeps the color through mid-greys, giving a real luminance ramp.
   Interpolating *premultiplied* collapses the white end toward black.
   This is the single most likely cause and the first thing to test.
2. **Luminance color space.** The engine deliberately uses sRGB coefficients on
   sRGB values (SVG groups/clip/mask design, decision 2). If resvg linearizes
   first, mid-tones shift substantially.
3. **Alpha multiplication.** Whether the mask's own alpha should multiply
   luminance, and in which order relative to interpolation.

These are distinguishable by measurement: render one non-recursive mask with a
known gradient and compare sampled coverage against resvg's reference at the same
pixels. **The answer must be established before any code changes**, because each
cause implies a different fix in a different file, and two of the three would
silently change every mask in the corpus.

## Scope

**In scope**

- Root-cause the luminance divergence by measurement, and fix it at the source.
- Re-verify every `masking/**` golden against resvg's references afterward, since
  a luminance change touches all of them.
- A unit test pinning the corrected behavior with hand-computed coverage values,
  independent of any golden — the current suite has no test that would have caught
  this, which is why it shipped.
- Document the PNG-encoder byte-nondeterminism in the testing docs so the next
  person does not re-open "stale goldens."

**Out of scope**

- The four goldens as a "staleness bug" — there is none. Goldens that legitimately
  move because the luminance fix changes real output get regenerated **with** that
  fix, each compared against resvg's reference before committing.
- `mask-on-self-with-mixed-mask-type` unless the luminance fix moves it.
- Any change to the clip/mask *compositing* path (`EndGroup`, `attenuateByMask`).
  The evidence points at luminance derivation, not at how coverage is applied.

## Risk

`BuildLuminanceMask` is on the shared `render.Device` and is used by every mask in
every backend. A luminance change moves **every** mask golden in the corpus, so
"no pre-existing golden may move" cannot hold here — the deliberate exception is
that each moved golden must be individually compared against resvg's reference and
shown to move *toward* it. A golden that moves away from resvg means the fix is
wrong.

## Testing

- A unit test on `pixelMaskValue`/`BuildLuminanceMask` with hand-computed values
  at known colors and alphas, including a fully-transparent white pixel and a
  fully-opaque black one — the two ends this fixture exercises.
- A gradient-through-a-mask test asserting sampled coverage at three points along
  the ramp, so a future regression in interpolation is caught by arithmetic rather
  than by eyeballing a PNG.
- The full `masking/**` corpus re-swept against resvg's reference PNGs, with a
  per-fixture verdict recorded.
- `mask-type=alpha` must be unaffected — it bypasses luminance entirely and is the
  control proving the fix is scoped to the luminance path.
