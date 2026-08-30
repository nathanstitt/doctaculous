# Road to v1.0.0

What has to happen before tagging, and what deliberately waits for 1.1.

The engine is ready. Every finding below is about **API scope** and **documentation
integrity**, not rendering correctness — no wrong-output or data-loss bug survived the
audit. The risk in shipping today is not that the code is wrong; it is that `go.mod`
would freeze 50 packages we never meant to support, and that the backlog docs would
ship telling users a shipped feature is missing.

Each claim below was verified against the code or by rendering, not taken from a doc.
Where verification changed the answer, the entry says so.

## Sequencing

The API items come first and land together. They are the only entries here that
`v2` cannot undo, and every one of them is mechanical rather than clever — the cost
is review attention, not invention. Everything after that can ship in any order.

| Phase | Contents | Why here |
| --- | --- | --- |
| 1 | API freeze (items 1–4) | Irreversible after the tag |
| 2 | Doc integrity (items 5–7) | Cheap, and the docs are currently wrong |
| 3 | `margin: 0 auto` (item 8) | Small fix, very visible, needs goldens |
| 4 | Release surface (items 9–12) | Standard 1.0 hygiene |
| — | Tag `v1.0.0` | |
| 5 | 1.1 backlog | Degrades honestly today |

---

## Phase 1 — API freeze

After the tag, exported API is frozen under semver: a breaking change means `v2`.
These four are the ones we would regret.

### 1. Move the engine packages under `internal/`

**50 packages are exported. About six are public API.** There are only two
`internal/` directories today, both deep (`pkg/xlsx/internal`, `pkg/render/internal`).
Everything else freezes on the tag.

The decisive evidence is our own CLI: `cmd/omnidoc` imports exactly three packages —
`pkg/omnidoc`, `pkg/crop`, `pkg/pdf`. Every engine package is imported only from
inside this repo.

`pkg/css` alone would freeze 87 symbols including `ComputedStyle`, whose own doc
comment says the typed property set is *"deliberately minimal"* and that properties
outside it *"do not yet populate a typed computed field. That is expected, not a
gap."* That is a written promise that the type will change, on a type v1 would
freeze.

Move under `internal/`: `css`, `layout`, `layout/css`, `layout/cssbox`,
`layout/inline`, `layout/paint`, `filtereffects`, `pdf/content`, `pdf/pageres`,
`svg/draw`, `render/imageconv`, and the `render/*write` backends.

Keep public: `omnidoc`, `docx`, `xlsx`, `crop`, `heif`, `resource`, `render`, `pdf`,
`svg`, `font`.

Scale: `layout/cssbox` is referenced by 104 files, `css` by 78, `layout` by 66. It is
a large mechanical diff and a compiler-checked one — nothing outside the repo can
break, because nothing outside the repo can import these yet. That is exactly why it
has to happen before the tag and not after.

### 2. Decide `render.Device`'s fate

A 15-method interface with no embedding escape hatch. The README calls it "the
seam… new backends bolt on without touching them", which invites external
implementations — and adding a 16th method after v1 breaks every one of them.

This interface has demonstrably grown: `BuildClipMask`, `BuildLuminanceMask`, and
`RenderOffscreen` were added over time, and `EndGroup`'s doc comment records a prior
signature change in so many words — "Pre-combining them into a single opaque
GroupMask value, *as an earlier revision of this interface did*, silently breaks any
backend…". An interface that already changed shape twice is the worst possible
freeze candidate.

All four implementers are in-repo (`render/raster`, `render/pdfwrite`,
`pdf/extract.nullDevice`, `svg/draw.transformDevice`).

Two viable options, pick one:

- **Internal.** Move the interface behind `internal/` and export only a registration
  function. Cheapest, and reversible later in the additive direction.
- **Core + capabilities.** A small required interface plus optional capability
  interfaces discovered by type assertion (`interface{ RenderOffscreen(...) }`).
  More work now; grows forever without breaking.

### 3. Export `WithContext(ctx)` as an `OpenOption`

`context.Context` is missing from 40 of 45 `Open*` functions, and absent entirely
from `pkg/docx` and `pkg/xlsx` — the two packages the README designates as supported
surfaces for tinycld. `docx.Open`, `xlsx.Open`, `xlsx.Edit`, and `File.Save` all do
full-package zip and XML parsing with no way to cancel.

The codebase already felt this and worked around it with a parallel `*Context` naming
family for HTML and URL only. `OpenHTMLBytesContext`'s doc admits the plumbing
exists but is unexported: *"ctx rides in as the unexported withOpenContext option…
(there is no exported way to, today)"*.

Exporting that one option retires the whole `*Context` name family before it is
frozen. `OpenOption` is already variadic on most of these, so it is additive.

### 4. Reconcile the duplicate `ErrSheetNotFound`

Two sentinels that do not interoperate:

- `pkg/omnidoc/xlsx_frontend.go:15` — `errors.New("xlsx sheet not found")`
- `pkg/xlsx/edit.go:21` — `errors.New("xlsx: sheet not found")`

Verified by compiling a program against both packages: `errors.Is` between them
returns **false**, and the messages differ only by a colon. A caller using both —
exactly the tinycld case — will write the wrong check and it will compile and pass
review.

Fix: make one an alias of the other, or wrap.

While here, two error classes have no sentinel at all. Ten sites return bare
`fmt.Errorf` for one branchable condition ("this document cannot produce a box
tree"): `docxwrite_backend.go:41`, `csvwrite_backend.go:37`, `rtfwrite_backend.go:41`,
`epubwrite_backend.go:35`, `htmlwrite_backend.go:31`, `pptxwrite_backend.go:36`,
`xlsxwrite_backend.go:28`, `markdown_backend.go:39`, `pdfwrite_backend.go:64`. A
caller wanting "fall back to rasterizing if structure extraction won't work" has to
string-match. Add `ErrNoStructure`, and `ErrPageOutOfRange` for
`reflow_paint.go:19`.

Adding sentinels later is technically additive, but errors returned by v1.0 stay
unmatchable forever, so callers written against v1.0 keep string-matching.

---

## Phase 2 — Documentation integrity

All cheap. All currently wrong.

### 5. The backlog says shipped features are missing

- **`transform` shipped** (PR #140, `FEATURES.md:336`) but `docs/GAPS-REMEDIATION-2.md`
  still says it "remains split out and unimplemented" at `:148`, `:164`, and calls it
  "currently silent" at `:421`/`:426`.
- **`writing-mode` never reached `SCOPE.md`.** The decision record says it "goes in
  `docs/SCOPE.md` as a known gap with its cost stated"; grepping `SCOPE.md` for it
  returns zero hits. The SVG path logs honestly; the HTML/CSS path has no
  `WritingMode` handling anywhere, so for HTML it is silent *and* undocumented —
  precisely the state the decision was meant to avoid.
- **The status header is stale.** `FIDELITY-BACKLOG.md:15` claims "48 done · 2 in
  progress · 30 open" as of 2026-07-29, predating PRs #127–#140.
- **A dangling sentence** at `FIDELITY-BACKLOG.md:435`: "see FEATURES.md and."
- **Three questions addressed to the maintainer** sitting in a shipped doc at
  `FIDELITY-BACKLOG.md:453-456`. Two are already answered by history.

### 6. Two silent degradations, and a backlog entry that understates them

`unicode-range` and `font-display` are **not parsed at all**. `parseFontFace`
(`pkg/css/fontface.go:26-36`) switches on `font-family`, `src`, `font-weight`, and
`font-style`; everything else is discarded with no log, and `FontFace` has no field
for them. Grep finds them nowhere in non-test source.

`FIDELITY-BACKLOG.md:238` describes `unicode-range` as "captured-but-ignored; whole
face used for every rune". That is wrong — it is not captured. Note
`pkg/omnidoc/html_webfont_test.go:91` feeds both descriptors into a test, which reads
as coverage but exercises neither.

Fix: add the warn-once log, correct both entries. Actually implementing
`unicode-range` subsetting is a 1.1 item.

Three more silent paths worth a log while in here:

- `position: relative` on a text-only inline box — a no-op with no log
  (`FIDELITY-BACKLOG.md:145`).
- Grid flow-axis-locked placement — documented in a code comment at
  `pkg/layout/css/grid_place.go:335`, not at runtime.
- Margin-collapse edge cases — same, `pkg/layout/css/block.go:345`.

And verify one claim before repeating it: `FIDELITY-BACKLOG.md:335` says PDF tiling
patterns are "skipped + logged"; the log was not found at
`pkg/pdf/content/interp.go:47`.

### 7. Move shipped work out of the backlog docs

`CLAUDE.md` is explicit that these files hold only outstanding work. They are in
gross violation:

- `docs/GAPS-REMEDIATION.md` — **all 9 findings done.** The entire file.
- `docs/GAPS-REMEDIATION-2.md` — 7 of 9 done, 2 do not reproduce.
- `docs/FIDELITY-BACKLOG.md` — roughly 40 entries marked done, with full prose.

These carry real retrospective value (the anonymous-box `max-height` trap, the four
COLR bugs including signed 24-bit offsets, the Chrome-capture methodology for
`color-mix`). Extract the durable lessons, archive the rest, and collapse what
remains into one `docs/BACKLOG.md` holding only the ~25 genuinely open items.

Worth noting: `FIDELITY-BACKLOG.md:37-39` already warns that its own checkboxes are
untrustworthy and names five stale entries. This audit found two more (`unicode-range`,
and `transform` in GAPS-2), so the warning is still under-counting.

---

## Phase 3 — `margin: 0 auto` does not center

**Verified by rendering, not by reading the comment.** A 200px box with
`margin: 0 auto` in a 600px viewport renders hard against the left edge.

`pkg/layout/css/block.go:1131` — `usedEdges` resolves auto margins to 0, and says so:
*"Auto margins compute to 0 in this PR (horizontal margin:auto centering is
deferred)."*

This is the most common centering idiom in CSS. Every centered-layout document we
render is wrong today, silently. The backlog sizes it small–medium, and the
absolutely-positioned equivalent already shipped, so the concept is proven.

Needs a showcase section and regenerated goldens per the project rule.

---

## Phase 4 — Release surface

9. **`pkg/layout` has no package doc comment** — the only one of 50 missing it. It
   renders blank on pkg.go.dev, which the README badge links to directly.
10. **Zero `Example` functions in the repo.** With ~70 exported functions, pkg.go.dev
    will show no runnable examples at all. The highest-leverage doc fix available;
    `Convert`, `ConvertFile`, `Open`, and `RasterizePage` are the ones that matter.
11. **README badge contradicts `go.mod`** — badge says Go 1.26, `go.mod:3` says
    `1.25.0`. Fix the badge; 1.25 is the correct floor and CI derives from the file.
12. **CI tests one OS.** `ci.yml:14` is `ubuntu-latest` only, while `.goreleaser.yaml`
    ships darwin and windows binaries. Windows path handling is never tested. Add a
    matrix.
13. **The race suite has almost no memory headroom.** It peaks at ~13.3 GB RSS
    against a 16 GB runner. That was latent until the rename nudged it over, and the
    kernel killed the job (SIGTERM, exit 143) minutes into a 30-minute budget — a
    failure that reads as a hang. `-p 2` bought the margin back, but three levels of
    parallelism still compound (package binaries × `t.Parallel` × per-rasterize
    worker pools), and `-race` multiplies all of it. Worth measuring again before
    item 12 lands: a matrix multiplies runners, not per-runner memory, and macOS and
    Windows runners are not more generous. If it is still near the ceiling, cap the
    per-rasterize pool in tests rather than trimming coverage.

Also missing, in rough priority: `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`.
Consider SBOM and artifact signing — both increasingly expected at 1.0.

Two things confirmed **fine**, so nobody re-investigates them:

- **Version wiring is correct.** `.goreleaser.yaml:27-29` injects
  `-X main.version/commit/date` and the names match `cmd/omnidoc/main.go:21-23`,
  verified by building with ldflags. The bare `dev` from a local build is by design.
- **Coverage is strong** — 87.9% on `pkg/omnidoc`, 86.3% `docx`, 84.3% `xlsx`. The
  apparent 0% packages are a measurement artifact; re-measured with `-coverpkg` they
  are 73–92%. One genuine gap: `TranscodeToPNG`
  (`pkg/render/imageconv/imageconv.go:25`) is 0% even cross-package, and it is the
  sole path stopping HEIC images from degrading to alt text in DOCX/EPUB/RTF/PPTX.

---

## Deliberately waiting for 1.1

Each of these degrades honestly today — a log and a skip, never a crash or silent
wrong output. That is what makes them safe to ship.

| Item | Why it waits |
| --- | --- |
| Base-14 weights + Symbol/ZapfDingbats (K6) | Bold text rendering non-bold is the most visible PDF gap, but the fix is medium–large and unblocks G1 with it |
| DOCX embedded fonts (M5) | De-obfuscate `word/fonts/*`; the `OSFontProvider` seam exists but is not installed in `docxDocument` |
| Full `vertical-align` keyword set (E1) | Only `super`/`sub` shift in inline context today; table cells are already correct |
| PDF encryption with real passwords (K5) | Common in the wild; no password API exists yet |
| `unicode-range` subsetting (G2) | The *log* is a Phase 2 blocker; the implementation is not |
| Grid named-line placement (I1), `auto-fit` collapse (I8) | Parsed and ignored, degrading to auto-placement |
| Tiling patterns (K2) | Skipped; verify the log claim first |
| Per-page float distribution (N4) | Medium |
| Font scan for unregistered families | `sysfont` cannot see Roboto/Barlow on many hosts; needs our own indexed directory scan |
| `TranscodeToPNG` test coverage | Real gap, small fix |

## Not planned

Recorded so they stop being re-litigated: JPEG2000 (no viable pure-Go decoder
exists), mid-flex/mid-grid-item splitting (owner-signed deferral; flex and grid size
items collectively, so it needs the fragmentainer inside track sizing), `subgrid`,
variable-font axes, and `writing-mode` for HTML — the last of which still needs its
`SCOPE.md` entry per item 5.

## If this is too much

Everything in Phase 1 exists because a `v1.0.0` tag is a promise about the exported
API. If the appetite for that work is lower than the appetite for shipping, the
honest alternative is to tag **`v0.9.0`** instead: it keeps the API unfrozen, ships
all the same functionality, and lets Phases 2–4 land at their own pace. What is not
a good option is tagging `v1.0.0` and quietly breaking the API in `v1.1`.
