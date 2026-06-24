# WPT-style reftests (in-house)

Reference-comparison reftests in the WPT style: each test page is paired with a
reference page (`NAME.html` + `NAME-ref.html`) that is written differently but is
designed to lay out to the **identical** pixels. The harness rasterizes both and
asserts they match within the same tolerance the golden tests use. This follows the
Web Platform Tests reftest pattern (a `<link rel="match">` test/reference pair), made
hermetic so no browser is involved — we assert self-consistency between two of our
own renders rather than matching a real browser pixel-for-pixel.

These files are **authored for this project, not vendored** from the W3C Web Platform
Tests suite. The *pattern* follows WPT reftests (the WPT suite is BSD-3-Clause); no
WPT files are copied here.

`css21-normal-flow/` holds CSS 2.1 normal-flow equivalences our engine supports:

| pair               | equivalence asserted                                              |
|--------------------|-------------------------------------------------------------------|
| `margin-collapse`  | adjacent vertical margins collapse to their max (8.3.1)           |
| `shorthand`        | margin/border/padding shorthands == their longhands               |
| `box-sizing`       | `border-box` width includes padding+border (== content-box equiv) |
| `auto-width`       | an auto-width block fills its containing block                     |
| `percent-width`    | a `%` width resolves against the containing block width           |
| `padding-shorthand`| 2-value padding shorthand == the 4-value form                     |

The runner is `pkg/doctaculous/wpt_reftest_test.go` (`TestWPTReftests`).
