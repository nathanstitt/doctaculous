# HTML rendering — web fonts (`@font-face` + WOFF/WOFF2) (sub-project 8)

**Branch:** `feat/html-webfonts` (off `feat/html-tables`, PR #11 — the tip of the HTML stack; rebase onto
`main` if the stack has merged by start). This slice does **not** build on the layout machinery the prior
HTML slices added — web fonts is **font infrastructure**: it makes an `@font-face`-declared family resolve
to a real downloaded face instead of falling through to the bundled base-14 substitutes. The layout
algorithm (block/inline/table), the `render.Device` seam, and the PDF pipeline are **untouched** — shaping
already goes through `FaceCache`, so a different `*font.Face` flows through with zero layout change.

**Builds on:** the whole HTML pipeline (parse → box-gen → block/inline/table layout → paint), which is
done. Read CLAUDE.md "Architecture" / "Done" and the §6 TODO bullet (web fonts is first in the
remaining-slices list). The handover (`docs/superpowers/HANDOVER-subproject-8-webfonts.md`) records the
verified foundation; this spec is the agreed scope and design.

## Goal

Turn the CSS `@font-face` at-rule — which the parser **silently discards today** (`pkg/css/parse.go:42`)
— into real downloadable-font support. After this slice, an author can ship a custom typeface:

```css
@font-face {
  font-family: "MyFace";
  src: local("MyFace"), url(myface.woff2) format("woff2"), url(myface.woff) format("woff"),
       url(myface.ttf) format("truetype");
  font-weight: bold;
}
p { font-family: "MyFace", sans-serif; }
```

and a `<p>` styled `font-family: "MyFace"` renders with the **downloaded glyphs** (fetched through the
existing `ResourceLoader`, decoded from WOFF1/WOFF2/raw-sfnt to glyph outlines), not the base-14
substitute. A family with **no** matching `@font-face` (every existing fixture; all DOCX) resolves exactly
as today via `LoadStandard`, so all existing output stays **byte-identical**.

The agreed scope is the *ambitious* one: **all three font formats — raw TTF/OTF, WOFF1, and WOFF2
(including the transformed-glyf reconstruction)** — not the WOFF1-first cut the handover floated.

### In scope (full support)

- **`@font-face` capture (`pkg/css`).** Change the at-rule handling so the `@font-face` block is *parsed
  and kept* (all other at-rules keep being skipped). Capture the `font-family` name, the ordered `src:`
  list (`url(...) format(...)` and `local(...)`, with fallback ordering preserved), and the
  `font-weight`/`font-style` descriptors (which face of the family this resource is). The cascade is
  unchanged — `@font-face` is a side table, not a style rule.
- **Raw sfnt pass-through + WOFF1 + WOFF2 decode (`pkg/font`).** A `src: url()` points at a font file.
  Decode the container to **sfnt (TrueType/OpenType) bytes**, then hand those to the existing
  `parseProgram` (which already reads both `glyf`- and `CFF`-flavored sfnt — no per-format branching past
  the unwrap). Raw `.ttf`/`.otf` *are* sfnt (pass straight through). **WOFF1** is per-table zlib/raw
  repackaging (stdlib `compress/zlib` — no new dep). **WOFF2** is one Brotli-compressed table block plus
  the **glyf/loca transform** (reconstruct standard `glyf`+`loca` from the transformed point/composite
  streams). A single new public entry, `LoadSFNT(data []byte) (*Face, error)`, sniffs the leading tag and
  dispatches, producing the same `*Face` the reflow engine already consumes.
- **`local()` via a system-font adapter (`pkg/layout/font`).** A new `SystemFontProvider` interface
  resolves a `local("Name")` source to font bytes; the first implementation is a **disk adapter**
  (`DiskFontProvider{Dir}`) that matches the name against files in a directory. `local()` is a real
  resolution source (tried in `src` order), not a skip; on no match the resolver falls to the next `src`.
  A nil provider means `local()` never matches.
- **Face resolution wiring (`pkg/layout/font` `FaceCache`).** The `FaceCache` — the single seam that maps
  `(family, style) → *font.Face` — gains the `@font-face` sources (a `family → []FontFace` table), the
  `ResourceLoader`, and the `SystemFontProvider`, supplied at construction. `Resolve` consults the
  `@font-face` sources first (walking them in declared order, picking the entry whose weight/style best
  matches the request); on success it returns the downloaded face, on failure (or no `@font-face`) it
  falls back to `LoadStandard` exactly as today. Resolution is **lazy** (decoded on first use, mirroring
  `<img>`) and **cached, negative results included** (a failed fetch caches the fallback so it is not
  re-fetched per glyph).
- **Threading (`pkg/layout/css` + `pkg/doctaculous`).** `Build` aggregates the `@font-face` rules from
  the same origin sheets it already assembles (UA + `<style>` + `<link>`), and the collected table is
  handed to the `FaceCache` alongside the loader. `OpenHTML`/`OpenHTMLBytes` wire it up; a new
  `WithSystemFontProvider` option supplies the `local()` adapter (default nil ⇒ `local()` skipped).
- **Fetch through the existing `ResourceLoader` seam (`pkg/resource` — unchanged).** A `@font-face
  src:url()` is an external resource exactly like an external `<link>` stylesheet or an `<img src>`;
  fetch it through `ResourceLoader`. Tests stay hermetic: a `MapLoader` serves the font bytes.

### Deferred (graceful degradation only — no panic, recover at the page/glyph boundary, debug log)

Each deferral keeps the **degrade-gracefully** contract: the affected `@font-face` falls back to the
existing base-14 substitute resolution (skip + debug log), never a panic.

- **`local()` beyond the disk adapter.** No OS font-store enumeration; only the directory adapter ships.
  A `local()` with no matching file falls to the next `src`.
- **Synthetic bold/oblique.** If a `@font-face` family supplies only one weight/style, the missing variant
  resolves via `LoadStandard` (the bundled base-14 bold/italic substitute) — no algorithmic
  emboldening/slanting. (The bundled substitutes themselves still only ship regular weights today — the
  same pre-existing approximation, see CLAUDE.md §4 TODO — so a missing bold currently maps to the regular
  bundled face. Web fonts do not regress or fix that; they defer *synthesis* on the downloaded face.)
- **`unicode-range` subsetting.** Captured nowhere / ignored; the whole face is used for every covered
  rune. No per-subset face selection.
- **`font-display`.** Ignored (no async/swap timing in the synchronous, single-pass layout model).
- **Variable-font axes (`font-variation-settings`, variable `font-weight` ranges).** Ignored; a variable
  font resolves to its default instance via the normal sfnt parse.
- **WOFF2 transformed-glyf edge cases.** The transform reconstruction is in scope; any malformed/edge
  transform stream that resists reconstruction returns a typed error → fallback (the safety net), rather
  than blocking the slice.

### Explicitly NOT in this slice (separate later roadmap items)

- **HTTP fetching / `OpenURL`.** The `ResourceLoader` still has only hermetic loaders (`MapLoader`,
  `DirLoader`); the HTTP-backed loader + `OpenURL` is its own later slice. Web-font URLs resolve through
  whatever loader the caller supplies (a `MapLoader`/`DirLoader` in tests).
- **`@import` / other at-rules.** Only `@font-face` is captured; every other at-rule keeps being skipped.

## Dependency decision (load-bearing — the constraint requires recording it)

CLAUDE.md "Non-negotiable constraints": **pure-Go, MIT/BSD/Apache, no CGo.** WOFF1 + raw sfnt need only
stdlib (`compress/zlib`, `compress/flate`). **WOFF2 needs Brotli decompression**, which the stdlib does
not provide.

**Decision:** add **one** dependency — **`github.com/andybalholm/brotli`** — used *only* for Brotli
decompression inside `pkg/font/woff2.go`. The WOFF2 container parse and the glyf/loca table
reconstruction are written **in this repo** (`woff2.go`), tested against committed fixtures.

Rationale (vetted for this spec):

| | `andybalholm/brotli` (chosen) | `dmitri.shuralyov.com/font/woff2` (rejected) |
|---|---|---|
| License | **MIT** ✓ | BSD-3 ✓ |
| Pure Go, no CGo | **Yes** (c2go-translated, native Go) ✓ | Yes ✓ |
| Maintained | **Actively, v1.2.1 (Mar 2026), ~1350 importers** | Unmaintained since 2018 |
| Host | github.com ✓ | vanity domain `dmitri.shuralyov.com` ✗ |
| Delivers usable sfnt? | We write the transform → yes | **No** — parses container only, does **not** reverse the glyf transform; pulls in a *second*, dormant Brotli (`dsnet/compress/brotli`) |

The shuralyov package would add **two** dependencies (vanity-domain woff2 + dsnet brotli) and still leave
us writing the glyf transform — the only hard part. `andybalholm/brotli` is one well-maintained MIT
pure-Go dep giving exactly the one primitive stdlib lacks; we own the WOFF2 logic where it is testable.
**This rationale must be repeated in the PR description** per the "record the reason in the PR" constraint.

## Architecture (layers touched — keep them honest)

```
pkg/css          capture @font-face → Stylesheet.FontFaces []FontFace; cascade UNCHANGED
pkg/font         woff1.go (zlib), woff2.go (brotli + glyf/loca transform), LoadSFNT(bytes) → *Face
                 NEW dep github.com/andybalholm/brotli (Brotli decode only, woff2.go)
pkg/layout/font  SystemFontProvider + DiskFontProvider (local()); FaceCache resolves @font-face →
                 downloaded face → LoadStandard fallback (lazy, cached, negative results too)
pkg/layout/css   Build aggregates @font-face from origin sheets → []FontFace, threaded to the FaceCache
pkg/doctaculous  OpenHTML/OpenHTMLBytes wire the table + loader into the cache; WithSystemFontProvider
pkg/resource     UNCHANGED — reuse ResourceLoader; MapLoader serves font bytes in tests
```

**Data flow for a downloaded face (lazy, cached):**

1. Box-gen (`Build`) parses `@font-face` rules from all origin sheets → `family → []FontFace` map → handed
   to the `FaceCache` at construction (alongside the loader + system provider).
2. Layout shapes a run in family "Foo" → calls `FaceCache.Resolve("Foo", style)`.
3. Cache miss + "Foo" has `@font-face` entries → pick the best entry for `style`, walk its `Sources` in
   order: `local(name)` → `SystemFontProvider.LoadLocal(name)`; `url(ref)` → `ResourceLoader.Load(ref)`;
   each candidate's bytes → `font.LoadSFNT` (sniffs WOFF1/WOFF2/sfnt) → `*Face`.
4. First success wins; cached. All sources fail (or no `@font-face` for the family) → `LoadStandard(family,
   style)`, **also cached** (negative result included → no per-glyph re-fetch).
5. No `@font-face` for the family (every existing page; all DOCX) → straight to `LoadStandard`, exactly as
   today → **byte-identical** output.

### `pkg/css` — capturing `@font-face`

New types alongside `Stylesheet`:

```go
// FontFace is one captured @font-face rule.
type FontFace struct {
    Family  string       // font-family descriptor (unquoted, normalized)
    Sources []FontSource // src: list, in declared (fallback) order
    Weight  string       // font-weight descriptor ("normal"/"bold"/numeric); "" if absent
    Style   string       // font-style descriptor ("normal"/"italic"/"oblique"); "" if absent
}

// FontSource is one entry in a src: list: a url() or a local().
type FontSource struct {
    URL    string // url() ref; "" for a local() source
    Local  string // local() family name; "" for a url() source
    Format string // format("woff2") hint, lowercased; "" if absent
}
```

`Stylesheet` gains `FontFaces []FontFace`. Implementation: in `Parse`, when an at-rule prelude is
`@font-face`, parse its already-consumed body as descriptor declarations (reuse `parseDeclarations`), then
map descriptors → `FontFace` (`font-family`, `src`, `font-weight`, `font-style`). All other at-rule
preludes keep hitting the existing `continue` (skipped).

A small dedicated **`src` tokenizer** splits the value on commas while respecting parentheses and quotes,
then recognizes per-entry `url(...)`, `local(...)`, `format(...)`. It tolerates quoted/unquoted args.

**Degradation:** a `@font-face` with no `font-family` or no usable `src` is dropped (logged); a malformed
`src` entry is skipped but the rest of the list survives. Unknown descriptors (`unicode-range`,
`font-display`, `font-variation-settings`) are not mapped → simply not acted on (the deferral).

### `pkg/font` — decoders + `LoadSFNT`

`LoadSFNT(data []byte) (*Face, error)` mirrors `LoadStandard`'s tail (`parseProgram(b, progTrueType)` →
`&Face{prog, names: prog.nameToGID()}`), but sniffs the leading 4-byte tag first:

- `0x00010000` / `true` / `OTTO` / `ttcf` → already sfnt → `parseProgram` directly.
- `wOFF` → `decodeWOFF1(data)` → sfnt bytes → `parseProgram`.
- `wOF2` → `decodeWOFF2(data)` → sfnt bytes → `parseProgram`.
- anything else → typed error (caller falls back).

**`woff1.go` — `decodeWOFF1([]byte) ([]byte, error)`** (W3C WOFF1 spec): read the 44-byte header
(validate `wOFF` signature + `numTables`), read the table directory (tag/offset/compLength/origLength/
origChecksum per entry), inflate each table (`compLength < origLength` → `compress/zlib`; else copy raw),
then **reassemble a valid sfnt**: offset table (search params from numTables) + a fresh, tag-sorted,
4-byte-aligned table directory + the table data. No new dep.

**`woff2.go` — `decodeWOFF2([]byte) ([]byte, error)`** (W3C WOFF2 spec) — the intricate one:
- Read the WOFF2 header + the **compact table directory** (per entry: a `flags` byte with a 6-bit known-tag
  index or `0x3f` custom 4-byte tag; `UIntBase128` `origLength`; for transformed tables a
  `transformLength`).
- Brotli-decompress the single table-data block (`brotli.NewReader` — the new dep).
- Walk tables, slicing the decompressed block per directory order. Non-transformed tables pass through.
- **Reverse the glyf/loca transform**: for the transformed `glyf`, parse the WOFF2 sub-streams
  (nContours; for composites the composite-glyph stream; for simple glyphs nPoints + the flag stream + the
  triplet-encoded x/y coordinate streams + instruction stream), rebuild standard simple/composite glyf
  outlines and recompute the `loca` offsets (loca's transform is implicit — derived from the rebuilt glyf).
- Reassemble the sfnt (same offset-table/directory reassembly as WOFF1).

**Verification before coding (handover-mandated):** `WebFetch` the W3C WOFF2 spec for the exact
`UIntBase128`, the 255UInt16 / triplet coordinate encoding, and the transformed-glyf byte layout — these
are easy to get subtly wrong. Write the WOFF2 reconstruction **test-first** against the committed fixture
with an adversarial round-trip (decoded sfnt's glyph outlines == the bare TTF's).

**Degradation:** a corrupt/truncated container, unknown signature, Brotli error, or malformed transform
returns a typed error (a new `ErrInvalidWOFF` or the existing `ErrUnsupportedFontProgram`) — **never a
panic**. The caller catches it and falls back to `LoadStandard`.

### `pkg/layout/font` — `SystemFontProvider`, `DiskFontProvider`, `FaceCache`

```go
// SystemFontProvider resolves a local() font name to font bytes (sfnt or WOFF).
// nil provider → local() never matches (caller tries the next src).
type SystemFontProvider interface {
    LoadLocal(name string) (data []byte, ok bool)
}

// DiskFontProvider serves local() fonts by matching name against files in Dir
// (case-insensitive, extension-stripped: name.ttf/.otf/.woff/.woff2). Hermetic
// for tests: point Dir at testdata/.
type DiskFontProvider struct{ Dir string }
```

`FaceCache` gains web-font state, supplied at construction via a new constructor that keeps the existing
`NewFaceCache` (bundled-only; used by DOCX + existing callers) byte-for-byte:

```go
func NewFaceCacheWithFonts(faces []css.FontFace, loader resource.ResourceLoader,
    sys SystemFontProvider, logf func(string, ...any)) *FaceCache
```

Internally the cache holds a `map[normFamily][]css.FontFace` (built once), the `loader`, the `sys`
provider, and `logf`. `Resolve(family, style)` keeps its current signature. On a miss:

- If `family` has `@font-face` entries: pick the best-matching entry for `style` (exact weight+style →
  else regular → else first), walk its `Sources` in order. `local(name)` → `sys.LoadLocal`; `url(ref)` →
  `loader.Load(context.Background(), ref)`; each candidate's bytes → `font.LoadSFNT`. First success → cache
  + return.
- All sources fail, or no `@font-face` → `LoadStandard(family, style)`, cached (negative result included).

**Context note:** `Resolve` has no `ctx` parameter and is called deep in layout. Rather than store a
`context.Context` on the cache (a Go anti-pattern, and the cache may outlive a single `Layout` call when
reused), the fetch uses `context.Background()`. These loads are local + hermetic; the future HTTP loader
(its own slice) can add its own timeout. This trade-off is intentional and documented here.

### `pkg/layout/css` + `pkg/doctaculous` — threading

- `Build` already calls `assembleSheets` (UA + `<style>` + `<link>`). Add an aggregation that collects
  `FontFaces` from those same parsed sheets → `[]css.FontFace`, and **surface it** from `Build` (a new
  return value or a small result struct — chosen in the plan as the least-disruptive change; `Build`'s
  existing callers are few and in-repo).
- `html_backend.go` changes the engine's cache from `NewFaceCache()` to `NewFaceCacheWithFonts(fontFaces,
  cfg.loader, cfg.sys, cfg.logf)`. A new `HTMLOption` — `WithSystemFontProvider(p)` — supplies the
  `local()` adapter (default nil ⇒ `local()` skipped). `OpenHTML` (path-based) may default the system
  provider to a `DiskFontProvider` rooted at the document's directory (consistent with how it defaults the
  `DirLoader`) — decided in the plan.

### Byte-identical guarantee

Every existing page and all DOCX have no `@font-face`, so `Resolve` falls straight through to
`LoadStandard` — identical bytes. Enforced after **every** task: render goldens/reftests *without*
`-update`, then `git status --short pkg/doctaculous/testdata pkg/render/raster/testdata` must show **only
new files**. A changed existing golden = web-font resolution leaked into the base-14 path → fix before
proceeding.

## Testing (this project lives or dies on its test corpus)

Every layer gets tests **in the same PR** (new feature ⇒ new fixture + test). Hermetic — **no network**;
font bytes served from a `MapLoader` (or a `DirLoader`/`DiskFontProvider` rooted at `testdata/`).

### Fixtures (prerequisite task — do this first)

Find and download a small, permissively-licensed font with glyph shapes **obviously distinct from the
base-14 substitutes** (so a golden proves the *substitution* happened, not merely that some text rendered
— e.g. a geometric/display face whose letterforms differ clearly from TeX Gyre Heros/Termes).

- **Acceptable licenses: OFL, Apache, MIT, or Creative Commons.** If a **CC-licensed** font is used, add
  its **attribution to the repo `README.md`** (CC licenses require attribution); for OFL also ship the
  license file alongside the fixture.
- Obtain the bare **TTF**; generate **WOFF1** and **WOFF2** from it (a standard tool — e.g. `fonttools`)
  so all three fixtures share **one ground truth** for the round-trip tests. Subset small (a handful of
  glyphs — enough for the golden's text).
- Commit under `testdata/` with provenance + license noted in the PR.
- **The implementer fetches with the sandbox disabled** (network is blocked in-sandbox except
  `proxy.golang.org`) and **stops to confirm the chosen font + license with the controller before
  committing** the binary fixture (a binary asset with a license obligation warrants a human glance).

### Per-layer tests

- **`pkg/css` `@font-face` parse** — `@font-face { font-family: Foo; src: local("Foo"), url(foo.woff2)
  format("woff2"), url(foo.ttf); font-weight: bold }` parses to the right `FontFace` (family; ordered
  sources with `Local`/`URL`/`Format`; weight/style). Malformed `src` entry skipped, rest survive; a
  `@font-face` with no family or no src dropped. Assert the parsed structure directly.
- **`pkg/font` WOFF decode** — the committed WOFF1 fixture decodes to sfnt bytes `parseProgram` reads, and
  the resulting `*Face` yields the **same glyph outlines** as the bare TTF. Same round-trip from the WOFF2
  fixture (**including the transformed-glyf path**). A corrupt container (truncated header, bad signature,
  Brotli garbage, malformed triplet stream) → typed error, **no panic** (adversarial cases).
- **`pkg/layout/font` resolution** — `Resolve` returns the **downloaded** face for an `@font-face` family
  and the **bundled** face for an unknown family; a 404 / decode failure caches + returns the fallback and
  does **not** re-fetch (assert the loader's call-count == 1 across repeated `Resolve`s). `local()`
  resolves via a `DiskFontProvider` pointed at `testdata/`; a nil provider (or no match) skips to the next
  `src`. Best-variant selection: a family with separate regular + bold `@font-face` entries resolves the
  requested style to the right entry.
- **Golden image** (`pkg/doctaculous`, `htmlGoldens` + committed PNG) — a page whose text uses an
  `@font-face` family served by a `MapLoader`, rendered with the **custom** glyphs (visibly distinct from
  base-14). Generate with `go test ./pkg/doctaculous -run TestHTMLGolden -update`; the implementer (no
  image vision) **STOPs after `-update` and hands back the PNG paths** for the controller to eyeball via
  the Read tool before they are committed.

### WPT-style reftest

Fonts are hard to reftest (a reference can't easily reproduce custom glyphs). Attempt one only if it fits
cleanly — e.g. text rendered via the `@font-face` family == the same text where that family is *also* the
document default (both routes hit the same downloaded face, so the rendered output should match). If it
does not fit, rely on goldens + the unit round-trip (and say so in the PR) — no forced reftest.

### Degradation tests (each → no panic + base-14 fallback + debug log)

- 404 / missing `url()` font.
- Corrupt WOFF1; corrupt WOFF2; corrupt transformed-glyf.
- `local()` with no matching system font; nil provider.
- Deferred descriptors present but ignored (`unicode-range`, `font-display`, `font-variation-settings`) →
  the font still resolves (descriptors not acted on).
- Missing variant: `@font-face` supplies only regular, page asks bold → bold resolves via `LoadStandard`.

## Process reminders (carried across #1–#7 — these earned their keep)

- **Sandbox blocks the Go build cache + TLS + the font download** — run `go` / `golangci-lint` / `gofmt`
  (and the fixture download, `gh pr create`, `git push` over HTTPS) with `dangerouslyDisableSandbox:
  true`. A sandboxed `go`/lint failure with cache/permission/"no go files" errors is the sandbox, not a
  real failure — re-run disabled.
- **Editor diagnostics LAG** — stale "undefined"/"unused"/"redeclared"/"not in go.mod" and phantom
  `zz_*`/`*probe*` files persist after a subagent's edits/deletes. Trust `go build`/`go test` and `find .
  -name 'zz_*'`, not the panel. Delete every `zz_*`/`*probe*` scratch file before finishing (confirm `git
  status` clean + `find . -name 'zz_*'` empty); make this an explicit instruction to every subagent.
- **`golangci-lint` here does NOT gofmt** — run `gofmt -l` on changed packages separately. Lint specific
  packages (`./pkg/css/... ./pkg/font/... ./pkg/layout/... ./pkg/doctaculous/...`), not the repo root.
  **NO `//nolint`**; the repo **declines all "modernize" hints** (`max()`/`min()`/`slices.*`/range-over-int)
  — keep explicit `if x < y { x = y }` clamps, indexed `for i := 0; i < n; i++` loops, `sort.SliceStable`.
  golangci-lint flags `if !(a && b)` (QF1001 — write the De Morgan form). The `unused` linter IS enforced:
  a struct field added must be *read* by code in the same PR; defer adding a field until the task that
  reads it.
- **Verify against the spec + the actual code, don't trust the handover blindly.** Confirm the WOFF/WOFF2
  byte layout against the W3C specs before encoding the decoders (`WebFetch`); the WOFF2 triplet/glyf
  encoding is the highest-risk piece. A change that forces inverting an existing, passing test is a red
  flag — stop and verify.
- **Two-stage review (spec-fidelity + code-quality, per task) + a holistic final review.** Have spec
  reviewers verify the load-bearing logic adversarially (the WOFF round-trip; the `@font-face` →
  resolution path; the no-re-fetch caching) with throwaway tests, and **delete the throwaways**. **Render
  real pages** at milestones (the controller, via the Read tool) — visible bugs are caught by rendering,
  not by a passing unit test.
- **Prefer the simpler mechanism.** Reuse the existing `parseProgram` (decode WOFF *to* sfnt; do not write
  a new font parser) and the existing `ResourceLoader`/`FaceCache` seams (no new plumbing). Add exactly
  one dependency (Brotli), for exactly the one primitive stdlib lacks.
- **Update CLAUDE.md when the PR lands** — move web fonts from the §6 TODO into a new "Done" bullet
  (describing `@font-face` capture, the raw-sfnt/WOFF1/WOFF2 decode incl. the glyf transform, the
  face-resolution wiring through the loader + `SystemFontProvider`, the new `andybalholm/brotli` dep with
  its rationale, what goldens/tests cover, and the deferrals), and remove "web fonts" from the §6
  remaining-slices list. Update the "Approved deps" list in CLAUDE.md to include `andybalholm/brotli`.

## Open questions resolved in brainstorm

- **WOFF2 scope** → **all three formats** (raw TTF/OTF + WOFF1 + WOFF2 incl. the transformed-glyf
  reconstruction), not WOFF1-first.
- **WOFF2 implementation** → **`github.com/andybalholm/brotli`** (MIT, pure-Go) for Brotli decode only;
  the WOFF2 container parse + glyf/loca transform written in-repo. (The shuralyov package was rejected: it
  does not reverse the glyf transform, pulls in a second dormant Brotli, and is on a vanity domain.)
- **Threading** → the **`FaceCache` owns the `@font-face` sources** (single resolution seam); `Build`
  collects them, the cache resolves lazily + caches.
- **`local()`** → a **`SystemFontProvider` interface with a `DiskFontProvider`** first implementation
  (load from a directory); `local()` is a real source tried in order, falling to the next `src` on no
  match.
- **Missing variant** → fall back to the bundled substitute (no synthesis).
- **`unicode-range` / `font-display` / variable axes** → deferred (ignored, stated above).
