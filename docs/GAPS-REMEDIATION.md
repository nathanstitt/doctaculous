# Remediation plan: gaps reported from the Luckfox dashboard

Source: `upnext/luckfox/docs/doctaculous-gaps.md`, reported against `fb42ebe`.
All findings below were **re-verified in this repo by measurement**, not accepted as
written. Where the report's diagnosis was wrong, the corrected one is recorded here —
the fix follows the measurement, not the report.

## Verification summary

| # | Reported | Verified | Correction to the report |
| --- | --- | --- | --- |
| 1 | `font-family` must end in a generic | **Real** | Not about the generic. Any family that fails to resolve deletes its text; the generic is merely the only *guaranteed* resolution. |
| 2 | CSS does not style inline `<svg>` children | **Real, narrower** | Inline `style=` on a child **does** work, and so does `<style>` inside the `<svg>`. Only the *HTML document* stylesheet is lost. |
| 3 | `sysfont` registry lacks most families | **Real, worse** | `sysfont` *does* walk the font dirs, but identifies files by **filename** against a registry, and `Match` **never returns nil** — so the miss path in `osfont.go` is dead code. |
| 4 | `max-height` / `overflow:clip` do not clip | **Real** | Two independent causes, not one. |
| 5 | `-webkit-line-clamp` unimplemented | **Real, larger** | `text-overflow: ellipsis` does **not** already work either — it is byte-identical to no ellipsis. |
| 6 | `color-mix()` unimplemented | **Real** | — |
| 7 | *(added by user)* colour emoji | **Real** | No `COLR/CPAL`, `sbix`, or `CBDT/CBLC` support anywhere. |
| 8 | *(found while fixing #1)* | **Real** | `<strong>`/`<b>`/`<em>`/`<i>` render with no bold or italic — the UA stylesheet has no rule for them. |

### How these were measured

Rasterize, then count pixels of a *specific colour* (the report's own method — coverage
counts hide a wrong-colour paint). For fonts, the decisive probe bypassed rendering and
called `FaceCache.Resolve` directly, which isolates resolution from painting.

---

## 1. An unresolvable `font-family` silently deletes text

**Priority 1.** Highest-value fix here: it converts a blank page into visible text.

### Root cause

`FaceCache.resolveList` (`pkg/layout/font/cache.go:149`) tries, per comma-separated
candidate: `@font-face` → injected `Provider` → `pkgfont.LoadStandard`. `LoadStandard`
(`pkg/font/family.go:81`) only succeeds for standard-14 aliases and the generic
keywords. When every candidate misses, `Resolve` returns `ok=false` and **the caller
skips the run** — behaviour documented as intended at `cache.go:79-86`
("ok is false only when no candidate resolves (the caller skips affected runs)").

Measured with a provider that always misses (i.e. the reporter's board):

```
DejaVu Sans              resolved=false      Arial      resolved=true
DejaVu Sans, sans-serif  resolved=true       Roboto     resolved=false
Nope XYZ                 resolved=false      Helvetica  resolved=true
```

`Arial`/`Helvetica` survive only because they are bundled aliases. The trailing generic
is not special — it is just the one candidate guaranteed to hit `LoadStandard`.

### Is it *truly* unresolvable?

Usually **no** — and that is the important part. On the reporter's board `DejaVu Sans`
was installed and should have resolved; it failed because of finding #3, not because the
font was absent. Two distinct failures compound:

- **#3** makes a resolvable font look unresolvable (or resolve to the wrong file).
- **#1** turns "unresolvable" into *silence* instead of a substitute.

Fixing #3 removes most of the *cause*; fixing #1 removes the *consequence*. Both are
needed — #1 is the safety net for genuinely-absent fonts, which will always exist.

### Fix

Add a terminal fallback in `resolveList`: after every candidate misses, resolve the
bundled serif substitute and log once per distinct family. Because misses are cached by
family+style (`cache.go:88`), the log fires once, not once per run.

```go
// after the candidate loop
if face, ok := pkgfont.LoadStandard("serif", style); ok {
    c.logf("font: no face for %q; substituting bundled serif", family)
    return face, true
}
```

Keep `Resolve`'s `(face, ok)` signature — `ok=false` stays reachable for the
no-bundle-available case, so callers need no change.

### Risk

This *changes rendering* for documents that currently draw nothing — which is the point,
but it will move goldens. Any golden that shifts must be eyeballed, not blindly
regenerated: a golden that previously captured invisible text is a bug being fixed, and
the new image is the evidence. Update `cache.go:79-86`'s doc comment, which currently
documents the old behaviour as correct.

### Tests

- Unit, in `pkg/layout/font`: a miss-provider resolves `"Nope XYZ"` to a non-nil face and emits exactly one log line.
- Unit: the log fires once across repeated `Resolve` calls (cache behaviour).
- Golden: a page whose only `font-family` is an uninstalled family now renders text.

---

## 2. HTML CSS does not cascade into inline `<svg>` children

**Priority 2.** Narrower than reported, which makes it tractable.

### Measured

| Markup | Centre pixel |
| --- | --- |
| `<rect class="k">` + HTML `<style>.k{fill:…}` | `rgb(0,0,0)` — **fails** |
| `<rect>` + HTML `<style>rect{fill:…}` | `rgb(0,0,0)` — **fails** |
| `<rect fill="…">` | correct |
| `<rect style="fill:…">` | **correct** (report says this fails — it does not) |
| `<style>` *inside* the `<svg>` | **correct** |

### Root cause

`pkg/html/html.go:75-78` returns early on foreign content, serializing the `<svg>`
subtree to a **string**. `pkg/svg` later re-parses that string as an independent
document (`pkg/layout/css/svg.go:151-164` → `svg.Parse`). Only the used box size crosses
the boundary. The SVG runs its own cascade (`pkg/svg/cascade.go`) over sheets collected
solely from `<style>` elements inside the subtree (`pkg/svg/index.go:126-149`).

Inline `style=` and SVG-internal `<style>` work precisely because both survive
serialization as literal text.

### Constraint

`TestInlineSVGGeneratesNoHTMLBoxes` (`pkg/doctaculous/svg_in_html_test.go:325`) asserts
exactly one `VectorKind` item and **zero** other items — it deliberately forbids walking
SVG children into the HTML box tree. That test encodes a real design decision and must
keep passing; the fix must not generate HTML boxes.

### Fix

Thread the host's author sheets and the computed root style into the SVG parse, rather
than re-cascading in the HTML tree:

1. Add an options form to `svg.Parse` (keep the existing 2-arg signature as a wrapper so
   all current callers stay byte-identical) carrying host stylesheets plus an inherited
   root style (`color`, `font-size`) — the latter is what `currentColor` and relative
   units need anyway.
2. Extend `cascadeCtx` (`pkg/svg/cascade.go:20`) to consult host sheets **below** the
   SVG's own sheets in precedence, preserving the existing order: presentation attrs →
   host sheets → SVG sheets → inline `style=` → `!important`.
3. Pass them through the three signatures that currently drop them:
   `replacedSVG` (`replaced.go:179`) → `inlineSVGCache.get` (`svg.go:151`) → `svg.Parse`.
4. Cache key: `inlineSVGCache` is keyed by markup string alone (`svg.go:151`). It must
   also key on the host sheets/root style, or two identical SVGs under different CSS
   would collide. This is a correctness requirement, not an optimisation.

Selector matching across the boundary (`body .k`) stays out of scope: `cssNode.Parent()`
(`pkg/svg/cssnode.go:42-47`) terminates at the SVG root. Selectors that match *within*
the SVG subtree (`.k`, `rect`, `#id`) will work — that covers the reported use case.
Note this honestly in FEATURES.md rather than implying full cross-boundary cascade.

### Tests

Extend `svg_in_html_test.go`: class, element, and `#id` selectors from an HTML `<style>`
colouring an SVG child; `currentColor` inheriting from the HTML box; a cache-collision
test (same markup, two different host sheets → two different colours).

---

## 3. `sysfont` silently substitutes the wrong font

**Priority 3** by report, but it is the *cause* of #1's field symptom — fix alongside it.

### Root cause (worse than reported)

The report says sysfont "matches against a hardcoded registry rather than scanning what
is on disk". Half right. `NewFinder` **does** walk `xdg.FontDirs`
(`sysfont@v0.1.2/finder.go:62-64`) — but identifies each file **by filename** against
`fontRegistry` (`finder.go:51`). An unrecognised filename yields
`&Font{Filename: …}` with empty `Name`/`Family`, which the fuzzy matcher can never score.

Worse: **`Finder.Match` never returns nil** (`finder.go:85-91`) — it always falls
through to `findAlternative`, which returns a "suitable default". Therefore
`OSFontProvider.LoadStyled`'s `ok=false` miss path (`pkg/layout/font/osfont.go:58-61`)
is **dead code**, and the bundled-substitute fallback it guards can never fire.

Measured on this machine (1083 installed fonts, **168 unidentifiable**):

```
DejaVu Sans              -> /System/Library/Fonts/LucidaGrande.ttc      (wrong)
Liberation Sans          -> .../Supplemental/GillSans.ttc               (wrong)
Barlow Condensed         -> Arial Unicode.ttf     (23278008 bytes)
Roboto                   -> Arial Unicode.ttf     (23278008 bytes)
IBM Plex Mono            -> Arial Unicode.ttf     (23278008 bytes)
ZZZZ Totally Fake 12345  -> Arial Unicode.ttf     (23278008 bytes)  <- nonexistent family
```

A family that does not exist returns the same bytes as three that do. This is why the
reporter saw DejaVu everywhere, and why `'DejaVu Sans Condensed'` "did not resolve
distinctly".

### Fix (selected approach: verify sysfont's match)

Keep `sysfont` as the finder; make its answer **honest**. In
`OSFontProvider.LoadStyled`, after `Match` returns a file, parse that file's sfnt `name`
table — `pkg/font` already decodes sfnt, so no new dependency — and compare the declared
family against the request. On mismatch, return `ok=false` so the caller falls through
to the bundled substitute.

```go
// osfont.go, after reading match.Filename
declared, err := pkgfont.FamilyName(b)          // new small helper over the name table
if err != nil || !familyMatches(declared, family) {
    p.log("osfont: %q resolved to %q (%s); rejecting as a mismatch", family, declared, match.Filename)
    return nil, false
}
```

`familyMatches` compares case-insensitively on normalised names, tolerating the
style-suffix convention noted in the report (`"Barlow Condensed SemiBold"` satisfies a
request for `"Barlow Condensed"` — prefix match on the family, with the style words
already carried separately by `styleQuery`).

This makes misses real, which is exactly what #1's new fallback needs to be reachable.
It does **not** find unregistered-but-installed fonts (Roboto on a Linux board stays
unfound) — that would need our own directory scan indexed by the real `name` table.
Record that limitation in FEATURES.md and keep `@font-face url()` as the documented
route for non-standard families; a follow-up branch can add the scan if needed.

### Tests

- Unit: a provider whose match declares a different family returns `ok=false`.
- Unit: a genuine match still returns bytes (guards against over-rejection).
- Unit: the style-suffix case (`Barlow Condensed SemiBold` for `Barlow Condensed`) is accepted.
- Integration: an uninstalled family now reaches the bundled substitute and logs.

---

## 4. Only `height` + `overflow:hidden` clips

Two independent bugs; the report treats them as one.

### 4a. `overflow: clip` is dropped at parse

`pkg/css/cascade.go:1151-1154` accepts only `visible|hidden|scroll|auto`. `clip` falls
through the `switch`, leaving `Overflow` at its initial `"visible"`, so `clips()`
(`pkg/layout/css/block.go:1199`) returns false.

**Fix:** accept `"clip"` in the switch. `clips()` already treats every non-`visible`
value as clipping, so it needs no change. Full fidelity also wants `overflow-x`/
`overflow-y` (`cascade.go:238-242` notes they are unhandled) — include them: `clip`
differs from `hidden` in that it forbids scrolling and permits `overflow-clip-margin`,
but in this engine's single-tall-page model (no scroll affordance, per the log at
`block.go:448`) the used behaviour is identical. Log nothing extra for `clip`; it is
exact here, unlike `scroll`/`auto`.

### 4b. `max-height` never clamps an auto-height block

`max-height` is applied only inside `resolveFixedHeight` (`pkg/layout/css/block.go:1382`),
which runs only when `height` is **not** auto. With `max-height` alone the height stays
auto and grows to content.

Measured (4 lines in a 48px box; unclipped ink = 1600):

```
height:48px; overflow:hidden      ink=703   <- clips
max-height:48px; overflow:hidden  ink=1600  <- no clip
height:48px; overflow:clip        ink=1600  <- no clip
height:48px                       ink=1600  <- correct (visible)
```

(The last row is correct CSS: without `overflow`, content legitimately overflows.)

**Fix:** after a block's auto content height is determined, clamp it by `max-height`
(and floor by `min-height`) before the fragment's `ClipRect` is computed from `borderH`
(`block.go:430-441`). Percentage `max-height` resolves against `cbWidth`, matching
`resolveFixedHeight`'s existing single-axis convention — keep it consistent rather than
introducing a second rule.

### 4c. Line pitch (report §4 "clamp height must be measured")

The report measures a 34px pitch where `font-size × line-height` predicts 27.5. This is
**probably correct behaviour**, not a defect: `line-height` is a minimum, and a line box
grows to the font's ascent+descent+lineGap when those exceed it. Worth confirming
against the actual face metrics before treating it as a bug. Action: verify, then
document the rule in `docs/CSS-LAYOUT.md`. No code change expected.

### Tests

Golden + unit per case: `max-height`+`hidden` clips; `overflow:clip` clips;
`overflow-x`/`overflow-y`; `height` with no `overflow` still overflows (falsifiability —
this must **not** change); `min-height` still floors.

---

## 5. No CSS line truncation — `-webkit-line-clamp` *and* `text-overflow`

The report calls `text-overflow: ellipsis` working. **It is not.** Measured on a
100px-wide `nowrap` overflowing box:

```
white-space:nowrap; overflow:hidden                        ink=658
white-space:nowrap; overflow:hidden; text-overflow:ellipsis ink=658   <- identical
```

Neither property exists anywhere in the tree (`grep` finds no `TextOverflow`, no
`line-clamp`). So this item is **two** features, and the single-line one must land first
because the multi-line clamp reuses its machinery.

### Fix, in order

1. **`text-overflow: ellipsis`** — at line-breaking, when a line overflows its box and
   the box clips, replace trailing glyphs with U+2026 until the line fits. Needs the
   ellipsis advance from the resolved face, and must handle the degenerate case where
   even the ellipsis does not fit (CSS: still render it).
2. **`-webkit-line-clamp`** — parse `display:-webkit-box` + `-webkit-box-orient:vertical`
   + `-webkit-line-clamp:N`; after N line boxes, stop and apply the ellipsis from step 1
   to line N. Also accept the standard `line-clamp` shorthand per current spec.

This is the largest item and should be its own branch, sequenced after #1 (it depends on
reliable face metrics).

### Tests

Unit on the line breaker (glyph-level: the ellipsis replaces exactly enough glyphs);
goldens for 1-line ellipsis, N-line clamp, clamp with a trailing float, and the
too-narrow-for-ellipsis edge case.

---

## 6. `color-mix()` unimplemented

Confirmed absent (`grep` finds no `color-mix`). The whole declaration is dropped, so the
background falls back to transparent — measured `rgb(255,255,255)` vs `rgb(211,228,245)`
for the `rgba()` equivalent.

**Fix:** add `color-mix()` to `pkg/css/color.go`. Implement `in srgb` fully
(two colours, optional percentages, normalisation when they do not sum to 100%, and the
`transparent` case the reporter relies on). Per the repo's maximal-fidelity directive
also implement `in srgb-linear`, `in hsl`, `in hwb`, and `in lab`/`oklab`/`lch`/`oklch`
with the spec's hue-interpolation modes; the polar cases are mostly arithmetic once the
existing colour conversions are reused. Unsupported colour spaces must drop the
declaration *and log*, matching the "degrade honestly" rule.

Lowest priority — the `rgba()` substitution is exact for their case — but it is
self-contained and cheap.

### Tests

Table-driven unit tests in `pkg/css` covering each space, percentage normalisation,
missing/over-100% percentages, `transparent`, nesting inside `var()`, and an invalid
space (asserting both the drop and the log). One showcase entry.

---

## 7. Colour emoji fonts (added during review)

Not in the report; raised by the user. **Confirmed unsupported.**

Measured — counting *non-greyscale* pixels, so colour cannot hide:

```
😀🎉 default font-family                    ink=2960  colouredPx=0   <- monochrome
😀🎉 font-family:'Apple Color Emoji'        ink=0     colouredPx=0   <- nothing at all
AB   (control)                              ink=1337  colouredPx=0
```

Two separate failures: emoji render monochrome at best, and naming the emoji family
explicitly triggers the **same run-skip as #1** — reproduced here on macOS, independent
of the reporter's board.

### Root cause

`pkg/font` parses only `glyf`/`CFF ` outlines (`pkg/font/sfnt.go:52-62`). None of the
colour-glyph tables exist anywhere in the tree:

- **`sbix`** — Apple Color Emoji (PNG strikes)
- **`CBDT`/`CBLC`** — Noto Color Emoji (embedded PNG bitmaps)
- **`COLR`/`CPAL`** — layered vector emoji (Segoe UI Emoji, Noto COLRv1); the only
  format that scales cleanly and the best fit for a vector-first pipeline

The `.ttc` container is a further wrinkle — Apple Color Emoji is a collection, and
`LoadSFNT` would need to select the right face.

### Fix (own sub-project; largest item here — do not fold into another branch)

Sequenced so each stage ships value independently:

1. **`COLR`v0 + `CPAL`** — layered solid-colour glyphs. Pure vector, composes with the
   existing outline pipeline and the `Device` seam, and works in PDF/SVG/raster alike.
2. **Bitmap strikes (`sbix`, `CBDT`/`CBLC`)** — decode the PNG strike nearest the used
   size and emit it as an image. Raster and PDF only; note honestly that it does not
   scale like an outline.
3. **`COLR`v1** — gradients and transforms. Reuses `pkg/css/gradient.go` concepts.
4. **Emoji in the fallback chain** — extend `ResolveScriptFallback`
   (`pkg/layout/font/cache.go:112`), which today covers only Hebrew and Arabic, to route
   emoji codepoints to a colour face. This is what makes `😀` in a normal paragraph work
   without the author naming a font, and it is the fix with the most user-visible effect.

Stage 4 depends on #1's terminal fallback, so sequence this after that branch.

### Tests

Golden rasters asserting **non-greyscale** pixels (a greyscale-only assertion would pass
on tofu — the same trap the report calls out); per-table unit tests in `pkg/font`; a
`.ttc` face-selection test; a "no colour face available" degradation test asserting the
monochrome fallback *and* its log.

---

## 8. `<strong>`/`<b>`/`<em>`/`<i>` are not bold or italic (found while fixing #1)

Not in the report. Found by eyeballing the showcase page added for #1, whose bold/italic
line rendered flat.

**Confirmed by measurement.** Rendering the same word at 30px, counting ink:

```
<strong>Hello</strong>                  ink=626   <- same as plain
<em>Hello</em>                          ink=626   <- same as plain
<b>/<i>                                 ink=626   <- same as plain
plain Hello                             ink=626
style="font-weight:bold"                ink=808   <- works
style="font-style:italic"               ink=605   <- works
```

**Root cause:** the UA stylesheet (`pkg/html/ua.go`) declares `font-weight: bold` for
`h1`–`h6` and `th`, but has **no rule at all** for `b`, `strong`, `i`, `em`. The style
machinery is fine — CSS `font-weight`/`font-style` apply correctly on a `<p>` or a
`<span>`; only the UA defaults are missing. Emphasis is therefore structurally present
(and survives conversion to Markdown) but visually absent in every rasterized format.

**Fix:** add to `uaSource`:

```css
strong, b { font-weight: bold; }
em, i, cite, var, dfn, address { font-style: italic; }
```

Use `bold`, **not** the spec's `bolder`: `pkg/css/cascade.go` does not parse the
relative weight keywords, so `bolder` is dropped as an invalid value and the emphasis
stays invisible. Adding `bolder` support is a reasonable follow-up; spelling the rule
`bold` is the honest description of what the renderer can express today.

**Why its own branch.** This was drafted and verified during the #1 work (the md-specimen
golden confirms *emphasis* and **strong** then render correctly), but reverted: a UA
stylesheet change moves goldens across HTML, EPUB, PPTX, Markdown-text and the whole
showcase — 10 golden files and dozens of pages. That blast radius does not belong in a
font-resolution branch. Sequence it on its own, regenerate every affected golden, and
eyeball them.

## Sequencing

Per `CLAUDE.md`, each item is its own branch → PR off `main`, merged when CI is green.

| Order | Branch | Items | Why here |
| --- | --- | --- | --- |
| 1 | `fix/font-terminal-fallback` | #1 + #3 | Same failure in the field; #3 makes #1's fallback reachable. Highest value: blank pages become text. |
| 2 | `fix/overflow-clip-and-max-height` | #4a, #4b | Small, self-contained, no dependencies. |
| 3 | `feat/css-color-mix` | #6 | Self-contained, cheap. |
| 4 | `feat/svg-host-cascade` | #2 | Medium; touches three signatures + a cache key. |
| 5 | `feat/text-overflow-and-line-clamp` | #5 | Largest CSS item; depends on #1's reliable metrics. |
| 6 | `feat/color-fonts` | #7 | Own sub-project, staged 1–4 internally; stage 4 depends on branch 1. |
| — | `fix/ua-emphasis-defaults` | #8 | Found during branch 1. One-rule fix, but regenerates goldens across every format; slot it anywhere, alone. |

Docs task alongside: #4c (verify line pitch, document in `docs/CSS-LAYOUT.md`), and a
FEATURES.md entry per landed item — including the honest limits (#2 does not cross
selector boundaries; #3 does not find unregistered fonts).

## Cross-cutting note

Findings 1, 3, and 7 share one failure mode: **the engine treats "cannot resolve" as
"draw nothing", and treats a wrong answer as a right one.** The pixel-level lesson from
the report — sample the colour, not the coverage — applies to the fix as much as the
diagnosis. Every test above asserts on a *specific* expected colour or a non-greyscale
count, because an assertion on "something was painted" would have passed throughout the
entire period these bugs existed.
