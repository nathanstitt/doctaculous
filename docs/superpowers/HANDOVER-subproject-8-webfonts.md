# Handover — Sub-project 8: web fonts (`@font-face` + WOFF/WOFF2)

**Status:** Not started. Sub-project 7 (CSS table layout) is **DONE** — `display:table` and friends now lay
out and paint as a real table (fixed+auto+percentage widths, full colspan/rowspan, vertical-align,
captions, both `border-collapse` models), with RTL the sole deferral. See CLAUDE.md "Done" (the table
bullet) and `docs/superpowers/specs/2026-06-25-html-tables-design.md`. The table slice merged as the
tip of the HTML stack (PR for `feat/html-tables` on top of `feat/html-zindex-6b`).

Web fonts is the **next** roadmap item (CLAUDE.md §6, first in the remaining-slices list). It is a font
**infrastructure** slice — it doesn't touch the layout algorithm; it makes a `@font-face`-declared family
resolve to a real downloaded face instead of falling through to the bundled base-14 substitutes.

**Next action:** Same flow as the prior slices — brainstorm → spec (`docs/superpowers/specs/`) → plan
(`docs/superpowers/plans/`) → subagent-driven execution (per task: implement → spec-review →
code-quality-review → fix) → holistic final review → finish branch / stacked PR. Web fonts is medium-sized;
a written spec + plan are warranted (the WOFF2/Brotli dependency decision and the `@font-face` cascade
integration both deserve a design pass).

---

## The PR stack — where to branch from

The whole HTML pipeline ships as a deep chain of stacked PRs:

```
main ← #2 css-parse-cascade ← #3 box-generation ← #4 block-inline-flow ← #5 replaced-images
     ← #6 floats(5a) ← #7 positioning(5b) ← #8 overflow(5c) ← #9 z-index(6) ← #10 zindex-6b
     ← #11 tables(7)
```

If the stack has merged to `main` by the time you start, branch web fonts off `main`. Otherwise branch
**off `feat/html-tables`** (the tip, PR #11). Name it e.g. `feat/html-webfonts`. Tell every subagent: you
are on `feat/html-webfonts`, do NOT checkout/stash/switch branches, do NOT commit unless asked, and delete
any `zz_*`/`*probe*` scratch file before finishing (confirm `git status` clean + `find . -name 'zz_*'`
empty). The per-task **spec reviewers and code reviewers will write throwaway probe tests** — make
deleting them an explicit instruction (across sub-project 7 every reviewer left a probe behind that the
controller had to confirm-gone; the lagging editor panel shows them as phantoms after deletion).

## Scope (propose this in the spec; adjust in brainstorm)

The CSS `@font-face` rule + downloadable font formats, so an author can ship a custom typeface:

- **`@font-face` parsing + capture.** Today the CSS parser **skips every at-rule** (`pkg/css/parse.go:42`
  — any `@`-prefixed rule has its block consumed and discarded). You will capture `@font-face` blocks:
  the `font-family` name, the `src:` list (`url(...) format(...)`, with fallback ordering and `local(...)`
  ignored or mapped to a bundled face), and the descriptors `font-weight`/`font-style` (which face of the
  family this resource is). This is the first at-rule the engine actually *keeps*.
- **WOFF / WOFF2 decode.** A `src: url()` points at a font file. The existing font parser (`pkg/font`,
  via `parseProgram`) already reads bare **TrueType / OpenType / CFF / Type1** (`pkg/font/font.go`,
  `program.go`). **WOFF and WOFF2 are container/compression wrappers around an sfnt (TrueType/OpenType)**
  table directory — decode them to the wrapped sfnt bytes, then hand those to the existing parser.
  **WOFF1** is a simple per-table **zlib/DEFLATE** wrapper (stdlib `compress/zlib` / `compress/flate` —
  no new dep). **WOFF2** uses **Brotli** compression + a transformed glyf/loca table layout — this needs a
  pure-Go Brotli decoder and the WOFF2 table reconstruction (see the dependency note below). Decide in
  brainstorm whether to ship **WOFF1 + raw TTF/OTF first** (smaller, no new dep) and defer WOFF2, or do
  all three. Plain `.ttf`/`.otf`/`.woff` URLs are common; `.woff2` is the modern default on the web.
- **Wire the downloaded face into the cascade + face resolution.** When box generation / shaping asks
  `FaceCache.Resolve(family, style)` (`pkg/layout/font/cache.go:47`) for a family that an `@font-face`
  declared, return the **downloaded** `*font.Face` instead of the bundled substitute. The `@font-face`
  table must reach the layout engine: it is collected at box-generation time (like `<style>`/`<link>`
  sheets are) and threaded to the engine, OR resolved into faces eagerly and passed alongside the loader.
  Decide the threading in brainstorm (the engine already takes a `ResourceLoader`; the `@font-face` map
  is the new state).
- **Fetch through the existing `ResourceLoader` seam.** A `@font-face src:url()` is an external resource
  exactly like an external `<link>` stylesheet or an `<img src>` — fetch it through
  `pkg/resource.ResourceLoader` (`pkg/resource/loader.go`), which has hermetic in-memory (`MapLoader`) and
  directory (`DirLoader`) loaders today (no HTTP yet — `OpenURL` + an HTTP loader is a separate later
  slice). So web-font tests stay hermetic: a `MapLoader` serves the font bytes.

**Likely deferrals (state them in the spec's Degradation section, each with a graceful fallback):**
WOFF2 (if you ship WOFF1-first), `local()` src (map to a bundled face or skip), `unicode-range`
subsetting, `font-display`, variable-font axes (`font-variation-settings`), and synthetic bold/oblique
when only one weight is supplied. Each deferral must keep the **degrade-gracefully** contract: a
`@font-face` that can't be fetched/decoded falls back to the existing base-14 substitute resolution (skip
+ debug log), never a panic.

## What already exists (the foundation — grounded in the code, verified for this handover)

- **`pkg/css/parse.go`** — the parser already **consumes and discards** at-rule blocks (lines 27, 42–43,
  88–89; it depth-tracks nested braces so `@media{ p{} }` is fully consumed). **Gap:** you change the
  `@font-face` case to *capture* the block (parse its descriptors) rather than discard it. Other at-rules
  keep being skipped.
- **`pkg/css/cascade.go`** — `firstFamily(val)` (line 628) resolves the `font-family` value to a single
  family name today; `ComputedStyle.FontFamily` carries it. The cascade itself does not need to change —
  web fonts change *face resolution* (which face that family name maps to), not the cascade.
- **`pkg/font` (the font program parser)** — `parseProgram(b, kind)` reads **TrueType/OpenType**
  (`progTrueType`) and **bare CFF** (`progCFF`); `font.go` already handles `FontFile2`/`FontFile3`/
  OpenType subtypes (lines 61–72). `*Face` exposes `Glyph(r)` → outline+advance and `Metrics()`. **This
  is the parser WOFF/WOFF2 must unwrap *to*** — decompress the container to sfnt bytes, then call the
  existing parse path. The dep is `github.com/benoitkugler/textlayout` (`go.mod`).
- **`pkg/font/family.go`** — `LoadStandard(family, style) (*Face, bool)` loads the bundled base-14
  substitutes (TeX Gyre Heros/Termes, Inconsolata) by family name. This is the **fallback** a failed/absent
  `@font-face` degrades to. `pkg/font/standard` holds the bundled faces.
- **`pkg/layout/font/cache.go`** — `FaceCache.Resolve(family, style) (*font.Face, bool)` (line 47) is **the
  seam**: it maps a family name + style to a `*font.Face`, caching the result. Web-font resolution plugs in
  here — a family declared by `@font-face` resolves to the downloaded face; everything else falls through
  to `LoadStandard` as today. The cache is concurrent-safe (the render fan-out shares it).
- **`pkg/resource/loader.go`** — `ResourceLoader` (line 21–23: `Load(ref) → bytes + content type`) with
  `MapLoader` (in-memory, line 38–42) and `DirLoader` (directory, line 54–62). The HTML engine already
  takes a `ResourceLoader` (it fetches external `<link>` CSS and `<img src>` through it). `@font-face
  src:url()` fetches the same way — **no new seam, reuse this one.** Tests stay hermetic via `MapLoader`.
- **The engine entry points** — `pkg/doctaculous` `OpenHTML`/`OpenHTMLBytes` build the box tree (collecting
  `<style>`/`<link>` sheets) and lay it out via the CSS engine (`pkg/layout/css`), which holds the
  `FaceCache` + `ResourceLoader`. The `@font-face` map produced at parse/box-gen time must reach the engine
  the same way the stylesheets do — see `pkg/html` box generation and `pkg/layout/css` `Engine` construction.

## Architecture fit (keep the layers honest — see CLAUDE.md "Architecture")

Web fonts live in the **font + CSS + resource** layers; the **`render.Device` seam, the PDF pipeline, and
the layout algorithm (block/inline/table) are untouched** — shaping already goes through `FaceCache`, so a
different `*font.Face` flows through with no layout change. Concretely:

- **`pkg/css`** — capture `@font-face` (parse.go) into a new structure (e.g. `FontFace{Family, Sources
  []FontSource, Weight, Style}` where `FontSource{URL, Format, Local}`); expose the collected faces from a
  parsed stylesheet. The cascade is unchanged.
- **`pkg/font`** — a WOFF (and optionally WOFF2) decoder that unwraps the container to sfnt bytes, then
  reuses `parseProgram`. A new `woff.go` (and `woff2.go`) decode step; the rest of `pkg/font` is reused.
- **`pkg/layout/font`** — extend `FaceCache.Resolve` (or add a parallel resolver it consults first) so a
  `@font-face` family resolves to the downloaded face. The cache key already includes style; add the
  `@font-face` source of truth (a map family→sources, resolved lazily through the loader + decoder, cached
  like the bundled faces, **negative results included** — a failed fetch caches the fallback so it is not
  retried per glyph).
- **`pkg/html` + `pkg/layout/css`** — thread the `@font-face` table from box generation to the engine
  (alongside the loader), mirroring how `<style>`/`<link>` sheets already flow.
- **`pkg/resource`** — **unchanged** (reuse `ResourceLoader`; the hermetic `MapLoader` serves font bytes
  in tests).

**Dependency note (the load-bearing brainstorm decision):** this project is **pure-Go, MIT/BSD/Apache,
no CGo** (CLAUDE.md "Non-negotiable constraints"). WOFF1 needs only stdlib (`compress/zlib`). **WOFF2
needs Brotli** — there is a pure-Go Brotli decoder (`github.com/andybalholm/brotli`, MIT) — vet its license
+ purity and **record the reason in the PR** (the constraint requires it). WOFF2 also requires the
table-reconstruction transform (the glyf/loca transform, the collection format) beyond raw Brotli; budget
for that or defer WOFF2. The recommendation: **ship raw TTF/OTF + WOFF1 first (no new dep), defer WOFF2**
to a focused follow-up that adds the Brotli dep + the WOFF2 transform — but confirm in brainstorm.

## Testing (this project lives or dies on its test corpus — see CLAUDE.md "Testing")

Every layer gets tests **in the same PR** (new feature ⇒ new fixture + test). Keep it hermetic — **no
network**; serve font bytes from a `MapLoader`. You will need a **small, real, permissively-licensed font
file** committed under `testdata/` (a tiny TTF/WOFF with a handful of glyphs — note its provenance +
license in the PR, as the corpus rules allow for fixtures impractical to generate). For web fonts
specifically:

- **`pkg/css` `@font-face` parse tests** — a `@font-face { font-family: Foo; src: url(foo.woff)
  format("woff"), url(foo.ttf); font-weight: bold }` parses to the right `FontFace` (family, ordered
  sources with formats, weight/style descriptors). Malformed/partial `@font-face` degrades (skipped). Assert
  the parsed structure directly (like the existing cascade/parse tests).
- **`pkg/font` WOFF decode tests** — a committed WOFF (wrapping a known TTF) decodes to sfnt bytes that
  `parseProgram` reads, and the resulting `*Face` yields the same glyph outlines as the bare TTF. (If WOFF2
  ships, the same round-trip from a WOFF2 fixture.) A corrupt container degrades to an error (no panic).
- **`pkg/layout/font` resolution tests** — `FaceCache.Resolve` returns the **downloaded** face for an
  `@font-face` family and the **bundled** face for an unknown family; a fetch failure caches + returns the
  fallback (and does not re-fetch).
- **Golden images** (`pkg/doctaculous`, `htmlGoldens` + committed PNGs) — a page whose text uses an
  `@font-face` family served by a `MapLoader`, rendered with the **custom** glyphs (distinguishable from
  the base-14 substitute — pick a fixture font whose shapes are obviously different, or compare against a
  base-14 render to prove the substitution happened). Generate with `go test ./pkg/doctaculous -run
  TestHTMLGolden -update`, then **eyeball every new PNG** (the controller, via the Read tool — the
  implementer has no image vision; have the implementer STOP after `-update` and hand back the PNG paths).
- **WPT-style reftest** (`pkg/doctaculous`, `wptReftests` + `NAME.html`/`NAME-ref.html`) — harder for fonts
  (the reference can't easily reproduce custom glyphs), so a reftest may not fit; if you can author one
  (e.g. the same text rendered via the `@font-face` family == the same text where the family is also the
  document default), do; otherwise rely on goldens + the unit round-trip.
- **Byte-identical guard.** Web fonts ADD face resolution for `@font-face` families; **no existing page
  should change** (every current fixture uses base-14 families, which still resolve via `LoadStandard`).
  After each task, run goldens/reftests WITHOUT `-update` and confirm `git status --short
  pkg/doctaculous/testdata pkg/render/raster/testdata` shows only NEW files. A changed existing golden
  means web-font resolution leaked into the base-14 path — fix before proceeding.
- **Degradation tests.** Each deferral (WOFF2 if deferred, `local()`, a 404 font URL, a corrupt font,
  `unicode-range`) degrades gracefully and is covered by a test asserting no panic + the base-14 fallback +
  the debug log.

## Process reminders (carried across #1–#7 — these earned their keep)

- **Sandbox blocks the Go build cache + TLS** — run `go` / `golangci-lint` / `gofmt` (and `gh pr create`,
  `git push` over HTTPS) with the sandbox disabled (`dangerouslyDisableSandbox: true`). This repo's
  `origin` is HTTPS. A sandboxed `go`/lint command fails with cache/permission errors that are NOT real
  failures; if you see "no go files to analyze" from `golangci-lint`, that's the sandbox — re-run disabled.
- **Editor diagnostics LAG badly** — after a subagent adds a field/file you'll see stale
  "undefined"/"unused"/"redeclared"/"not in go.mod" errors and **phantom `zz_*`/`*probe*` scratch files**
  that no longer exist on disk. Trust `go build`/`go test` and `find . -name 'zz_*'`, not the panel.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/font/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root.
  **NO `//nolint`**; the repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int)
  — keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`.
  golangci-lint flags `if !(a && b)` (QF1001 — write the De Morgan form `if !a || !b`). The `unused`
  linter IS enforced — a struct field you add must be *read* by code in the same PR; if a field is for a
  later task, defer adding it until that task reads it (in sub-project 7, the grid struct's layout fields
  had to be removed and re-added per consuming task to keep `unused` happy — see that slice's tasks 6–8).
- **Verify against the spec + the actual code, don't trust the handover blindly.** In sub-project 6b the
  handover's central premise was wrong per CSS 2.1 §11.1.1; in 7 the plan's code read `f.Box` for a static
  cell's vertical-align (always nil → silently broken) and the implementer had to switch to the source box.
  **A change that forces you to invert an existing, passing test is a red flag — stop and verify the spec.**
  For web fonts, confirm the WOFF/WOFF2 byte layout against the W3C WOFF specs before encoding the decoder
  (a `WebFetch` of the WOFF File Format spec); confirm a candidate Brotli dep's license + pure-Go status
  before adding it.
- **The two-stage review (spec-fidelity + code-quality, per task) + a holistic final review** earn their
  keep — they caught **four real bugs in sub-project 7** the implementers' own tests missed (a percentage
  width-conservation bug that silently dropped 292px, a vertical-align float-shift bug, a caption-side bug
  ignored when set on `<caption>`, and an auto-width-table-box over-paint). Have spec reviewers verify the
  load-bearing logic adversarially (the WOFF round-trip, the `@font-face` → resolution path) with throwaway
  tests, and **delete the throwaways**. **Render real pages** at milestones (the controller, via the Read
  tool) — every sub-project-7 visible bug was caught by rendering, not by a passing unit test.
- **Prefer the simpler mechanism.** Sub-project 7's border-collapse reused the existing `BorderItem` paint
  path instead of inventing a new paint primitive (the plan had proposed a new one) — much smaller. For web
  fonts, the analogue: reuse the existing `parseProgram` (decode WOFF *to* sfnt, don't write a new font
  parser) and the existing `ResourceLoader`/`FaceCache` seams (don't add new plumbing).
- **Update CLAUDE.md when the PR lands** — move web fonts from the §6 TODO into a new "Done" bullet
  (describing `@font-face` capture, the WOFF/WOFF2 decode, the face-resolution wiring, what goldens/tests
  cover, and the deferrals), and remove "web fonts" from the §6 remaining-slices list. Keep the Done/TODO
  the honest source of truth.

## Open questions to resolve in brainstorm (not blocking the start)

- **WOFF1-first or WOFF1+WOFF2?** WOFF2 needs a Brotli dep + the table-reconstruction transform — bigger,
  and the dep needs a license/purity sign-off. WOFF1 + raw TTF/OTF is stdlib-only and shippable; WOFF2 can
  be a focused follow-up. Recommendation: **WOFF1 + TTF/OTF first**, WOFF2 next.
- **How is the `@font-face` table threaded to the engine?** Eagerly resolved to faces at box-gen, or carried
  as a family→sources map the `FaceCache` resolves lazily through the loader? (Lazy + cached, mirroring how
  `<img>` decodes on demand, is the likely fit — but confirm.)
- **`local()` src** — map to a bundled base-14 face, or skip and move to the next `src`? (Skip-to-next is
  simplest and correct enough.)
- **Synthetic bold/oblique** — if a `@font-face` family supplies only one weight/style, fake the others
  (synthesize) or fall back to the bundled substitute for the missing variant? (Defer synthesis; fall back.)
- **`unicode-range` / `font-display` / variable axes** — all almost certainly **defer** (state in the spec).

None of these block branching and writing the spec; they shape its scope. The recommendation: **ship
`@font-face` capture + raw TTF/OTF + WOFF1 decode + face-resolution wiring through the existing
loader/cache, with a committed hermetic font fixture and an eyeball-verified golden — and defer WOFF2,
`local()` synthesis, `unicode-range`, `font-display`, and variable fonts.** Pin the exact line in the spec.
