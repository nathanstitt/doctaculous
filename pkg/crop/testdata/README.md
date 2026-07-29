# `pkg/crop` test fixtures

## `hippo.jpg`

A real photograph, used by `golden_test.go` to pin the saliency scorer's
behaviour on natural image statistics.

- **Dimensions:** 480×360, JPEG quality 85 (54 KB). Downscaled from a 1024×768
  original and re-encoded to keep the fixture small, per the repository's
  "keep committed real images small" rule.
- **Author:** Nathan Stitt — an original photograph by the project maintainer.
- **License:** **CC0 1.0 Universal** (public domain dedication), dedicated by
  the author. CC0 places no restriction on redistribution, so shipping the file
  inside this MIT-licensed repository is permitted and no attribution is
  required; the credit above is recorded as good practice, not obligation.

### Licensing note — why not just any free stock photo

"Free to download" is not the same as "free to redistribute". Several popular
stock sites (Freerange Stock's Equalicense, for one) grant broad *use* rights
while explicitly forbidding redistribution: *"You cannot sell, redistribute, or
relicense the images."* Committing such an image here would redistribute it to
every clone, fork and mirror of the repository, which those licenses prohibit
and which conflicts with the project's MIT/permissive constraint in CLAUDE.md.

CC0 and public-domain images are safe. CC-BY is usable with attribution
recorded. **CC-BY-SA is not** — its ShareAlike term would reach into derivative
works and conflict with MIT. Check the actual license text, not the site's
marketing copy, before adding an image fixture.

### Why a real photograph

The synthetic fixtures in `saliency_test.go` are checkerboards: hard edges,
uniform blocks, no noise. They pin the *direction* of the scorer's preference
(detail over flat, saturated over grey) but cannot catch a change that degrades
quality on real images, where gradients are smooth, edges are soft, and the
subject is not a rectangle.

This photograph has a clearly off-centre subject — a hippo lying across the
lower half with its head, the most textured region, at centre-right — plus large
flat stretches of water and a busy vegetated bank along the top competing for
the scorer's attention. Its mean Sobel edge energy is ~0.092, roughly nine times
that of the smooth gradient at `testdata/htmldoc/img/photo.jpg` (~0.010), which
carries no subject at all and is therefore useless for this purpose.

`TestSaliencyBeatsCenterOnPhoto` asserts the property the goldens alone cannot:
that the chosen window is off-centre *and* scores strictly better than the
centred one under the same scorer. A scorer that regressed to "always centre"
would still satisfy a regenerated golden, but not that test.

Note the check is deliberately direction-agnostic. Which way the window shifts
is a property of the fixture, not the algorithm — this photo pulls right, the
one it replaced pulled left — so asserting a direction would overfit the test to
one image and break the next time the fixture changes.

## `golden-*.txt`

One crop rectangle per aspect ratio, as `MinX MinY MaxX MaxY`. Plain text so a
diff is readable in review. Regenerate with:

    go test ./pkg/crop -run TestGoldenSaliencyOnPhoto -update

A golden diff is **not** automatically a failure, but it must be explained and
the resulting crop eyeballed before the new values are committed.
