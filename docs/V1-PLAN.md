# Road to v1.0.0

What has to happen before tagging, and what deliberately waits for 1.1.

Rendering fidelity is ready. Two other things are not, and they are different in kind.

**The engine crashes and hangs on malformed input.** A 2,258-byte `.xlsx` panics out
of `OpenXLSXBytes` and kills the CLI; a 66-byte HTML file hangs forever. Some of the
crashes are `fatal error: stack overflow` raised through `runtime.throw`, which
`recover()` cannot catch by construction — so the per-page recover guarantee does not
hold. `README.md` promises the opposite, without qualification: *"never a panic, and
one bad page can't kill a batch."* That sentence is currently false, and a document
toolkit's whole job is eating files it did not author.

**The API surface is accidental.** Tagging would freeze 50 packages when about six
are public API, including a `render.Device` whose own doc comment records a prior
breaking signature change.

Neither is a rendering bug. Both are worse than one, because a rendering bug can be
fixed in 1.0.1 without breaking anyone.

Each claim below was verified against the code, by rendering, or by running a probe —
not taken from a doc. Where verification changed the answer, the entry says so.

## Sequencing

Phase 0 comes first because it is a correctness and safety problem, and because
fixing it after the tag means either a rushed 1.0.1 or a documented lie. The API
items follow: they are the only entries `v2` cannot undo, and they are mechanical
rather than clever — the cost is review attention, not invention. Everything after
that can ship in any order.

| Phase | Contents | Why here |
| --- | --- | --- |
| 0 | Crashes and hangs on malformed input | The README's central promise is false today |
| 1 | API freeze (items 1–4) | Irreversible after the tag |
| 2 | Doc integrity (items 5–7) | Cheap, and the docs are currently wrong |
| 3 | `margin: 0 auto` (item 8) | Small fix, very visible, needs goldens |
| 4 | Release surface (items 9–13) | Standard 1.0 hygiene |
| — | Tag `v1.0.0` | |
| 5 | 1.1 backlog | Degrades honestly today |

---

## Phase 0 — crashes and hangs on malformed input

Every item here is reachable from a public entry point with a file a user could
plausibly be handed. Line numbers move; grep the symbol.

Two of these were reproduced end to end while writing this plan; the rest come from
the audit and are marked as such. **Reproduce before fixing** — the audit's line
numbers predate the rename, and one of its claims (that `nan` hangs) did not hold up.

### 0a. `parseRef` overflows the column index — panics — **DONE**

`pkg/xlsx/parse.go`, `parseRef`. The `col = col*26 + …` loop is unbounded. Measured:

| ref | column | note |
| --- | --- | --- |
| `XFD1` | 16383 | the legal maximum |
| `ZZZZZZZZZ1` | 5646683826133 | 344 million times the sheet width |
| 14 letters | −6696602603409169451 | **negative** |

That value reaches `make([][]Cell, maxRow+1)` and panics with `makeslice: len out of
range`. Confirmed end to end: the panic escapes `OpenXLSXBytes` — so an embedding
server dies too.

**Verification changed two things about this entry.**

*The row axis is the worse half, and was unrecorded.* `parseRef` bounded neither
axis: `strconv.Atoi` accepts any row number, and `A999999999999` reaches
`make([][]Cell, maxRow+1)` too. That is not a panic but an unbounded allocation —
the process is killed by the OOM killer (`signal: killed`), which `recover()`
cannot catch any more than it can catch a stack overflow. So 0a belonged with 0d
as a defeater of the batch guarantee, not merely as a catchable panic.

*The column overflow is not only the 14-letter wrap.* Any reference past the sheet
width panics — `ZZZZZZZZZ1` (9 letters, a positive number) hits `makeslice` just as
`XFE1` does. The negative wrap is one instance of "column exceeds the grid", not
the bug itself.

Fixed by bounding both axes in `parseRef` against named `maxSheetCols` /
`maxSheetRows` constants, with the column check *inside* the loop so no input
length can overflow `int`. An out-of-sheet address is rejected as malformed
rather than clamped: clamping would silently move a cell to a different address.
The existing `parseColElement` clamp now uses the same constants, and its start
column is bounded too (a large `min` filled the width map with out-of-sheet keys).

Covered by `pkg/xlsx/malformed_test.go` — table cases plus a `FuzzOpenBytes`
target (16.7M executions clean), which is also the first fuzz target on this
package, per 0h.

### 0b. Non-finite CSS numbers hang the layout engine — **DONE**

`pkg/css/cascade.go`, `parseNonNegNumber` rejects only `v < 0`, so `strconv.ParseFloat`
passes `inf`, `Inf`, `+Inf`, and `nan` straight through — measured, all four return
`ok=true`, and so does `infinity`, which the plan did not list. An infinite
`flex-grow` then makes the free-space loop in `pkg/layout/css/flex.go` never
terminate.

Reproduced: `<div style="display:flex"><div style="flex-grow:inf">a</div></div>` — 66
bytes — hangs until killed.

**Verification changed two things here as well.**

*The hang is in `OpenHTMLBytes`, not in rasterizing.* Layout runs during open, so
it is not merely that "there is no `ctx` check on that path" — no `RasterizePage`
deadline exists yet to be checked. The caller has no interruption point at all.

*`parseNonNegNumber` was not the whole fix.* The tokenizer's own `parseFloat`
(`pkg/css/token.go`) discarded `strconv`'s `ErrRange` and returned the `±Inf` it
came with. `readNumeric` scans only digits, `.` and a leading `-`, so `inf` cannot
be spelled through it — but 400 nines overflow to `+Inf` and reach the identical
hang, through a value that looks like an ordinary number. Fixing only
`parseNonNegNumber` would have left that door open for every consumer of
`Token.Num`. Both are now guarded; `pkg/css/color.go` already documented this
exact reasoning for colour components, so the convention was in place.

Note the audit reported `nan` as hanging too; it does not — NaN degrades safely.
Fixed anyway: accepting NaN as a valid CSS number is wrong regardless.

Covered by `pkg/css/nonfinite_test.go` (parser level) and
`pkg/omnidoc/malformed_input_test.go` (the 66-byte file, end to end, asserting
termination). The formerly-infinite cases now return in ~100 ms.

### 0c. Unbounded counts from attributes — **DONE**

Each took a small integer straight from markup with no cap. **All four reproduced**
(the audit was right here), each hanging `OpenHTMLBytes` indefinitely:

- `<td colspan="900000000">` and `rowspan` — HTML caps these at 1000 and 65534.
- `grid-row: 1 / 500000000`.
- `grid-template-columns: repeat(200000000, 1px)`.

**Verification changed three things.**

*The parse site was not where the plan said.* `colspan`/`rowspan` are read by
`attrSpan` in `pkg/layout/css/build.go`, not `table.go`. All three attributes
(`colspan`, `rowspan`, `<col span>`) flow through that one function, so a single
clamp fixes them — and its doc comment already cited HTML's clamping rule while
implementing only the lower half of it.

*Clamping the attribute is not sufficient.* The ceiling bounds one cell, but the
cost is per cell: 50 cells at the clamped maximum (1000 × 65534) still took **64
seconds**. The reason is that `buildGrid`'s `ensure` grows the grid to whatever a
span demands, so a `rowspan` fabricates rows the document does not have. Clipping
`rowspan` to `len(visualRows)` — which CSS 2.1 17.5.1 requires anyway — is what
actually bounds it. That case is now 0.01s.

*Two of these terminate without the fix, and would pass a naive timeout test.*
`repeat(200000000, 1px)` completes in 13.4s unclamped; the 50-cell table in 64s.
Neither is a crash, both are denial of service. The regression test therefore
asserts a **5-second budget** rather than mere termination — every bounded case
finishes in under 0.1s, so that is ~2 orders of magnitude of headroom while still
failing if a bound is removed. Verified by reverting each fix individually.

Fixed by clamping in `attrSpan` (`maxColSpan`/`maxRowSpan`), clipping `rowspan` to
the real row count in `buildGrid`, and bounding the expanded track count, explicit
line numbers, and `span` counts against `maxGridTracks` in `pkg/css/grid_value.go`.
The track bound is on the *expanded* count, not the repetition count, since
`repeat(N, a b c)` yields N×3 tracks.

Covered by `pkg/layout/css/unbounded_counts_test.go` (11 termination cases plus
the attribute-clamp table) and `pkg/css/grid_bounds_test.go`.

### 0d. Stack overflows that `recover()` cannot catch — audit-reported

These raise `fatal error` via `runtime.throw`, which is **not recoverable**. This is
the finding that undermines the batch guarantee, because the per-page `recover` is
structurally unable to catch it:

- `pkg/pdf/parser.go`, `parseArray` — recursion with no depth cap, while every
  sibling has one (`resolve.go` 32, `page.go` 64, `function.go` 32).
- `pkg/layout/css/build.go` — ~150k nested `<div>` (1.6 MB) kills the process.
  Frames are large because `css.ComputedStyle` passes **by value** through
  `Compute`→`inheritFrom`.

### 0e. PDF page-tree has no visited-set — audit-reported

`pkg/pdf/page.go`, `walkPageTree` caps depth but never marks visited nodes, so a
cyclic tree expands exponentially: reported as 22 levels → 4.2M pages in 0.76 s. It
runs inside `loadPages` during **open**, before any recover exists.

### 0f. Overflow defeats the image-dimension guard — **DONE**

**The headline claim does not reproduce, and this is the first audit item that is
simply wrong.** `pkg/render/raster/page.go` does not multiply in integer
arithmetic — it computes `fW*fH` in **float64**, which cannot wrap, and its
comment already says why: *"Compute dimensions in float64, then validate before
casting to int, so an attacker-controlled MediaBox or DPI cannot overflow int."*
`git log -S` shows that guard has been there since the raster layer was written.

Measured, every case the audit named is correctly refused:

| MediaBox | result |
| --- | --- |
| 2³¹ × 2³¹ | refused — "page too large … exceeds 134217728-pixel cap" |
| 2³⁴ × 2³⁴ | refused |
| 2⁴⁰ × 2⁴⁰ | refused |

**The bug is real, but in a different file.** `pkg/render/raster/image.go`,
`decodeRawImage`, takes an image's `/Width` and `/Height` with only a `> 0` check
and then does the integer arithmetic the audit described: `w*nComps*bpc` for the
row stride and `rowBytes*h` for the short-data guard. At h = 2⁶⁰ that product goes
negative, the guard passes, and `image.NewRGBA` is reached with an impossible
height — where it **panics**. Caught only by the per-page recover, which costs the
whole page for one bad image. Now bounded against the same `maxPixels` the page
path uses, compared by **division** so the check cannot itself overflow;
`maxPixels` is promoted to a package constant so both paths share one definition.

**RTF `\ilvl` is worse than recorded.** Not "35 bytes to 56 MB" — the HTML emitter
writes one `<ul>`/`<ol>` per level, so a **34-byte** document with
`\ilvl2000000000` **never finishes at all**. At `\ilvl100000` it is 30 bytes in,
1.4 MB out (46,000×). Clamped to `maxListLevel` (64); RTF allows 0–8 and Word
exposes nine.

**The zip-bomb entry understates the ratio and misplaces the cause.** Both readers
already bound each part at 256 MiB — the gap is that neither bounded the **total**,
and both do bulk reads (`partsWithPrefix` over `word/media/*`, and EPUB's
`OpenBytes`, which decompresses every entry). Measured: a **4 MB `.docx` with 20
compressible media parts decompressed to 4.2 GB** (1,028×, not ~2,500×) and drove
peak RSS to 6 GB — with every individual part inside its 256 MiB limit the entire
way. Fixed with a `maxTotalPartBytes` budget (512 MiB) in both; DOCX takes parts
in sorted order so a truncation is deterministic rather than depending on map
iteration.

**The pptx/xlsx immunity claim holds.** Checked: `rawPart` and its siblings fetch
one part at a time and never accumulate, so the per-part cap is sufficient there.

Covered by `pkg/render/raster/image_bounds_test.go`, `pkg/rtf/ilvl_test.go`,
`pkg/docx/zipbomb_test.go`, and `pkg/epub/zipbomb_test.go`. The zip budgets are
test-overridable so proving them costs kilobytes rather than half a gigabyte —
allocating the real ceiling under `-race` is how a CI runner gets OOM-killed
(see item 13).

### 0g. Silent wrong output on the PDF path

`pkg/omnidoc/pdf_backend.go` returns bare on error with no log. Every
PDF→anything conversion routes through it, so a failure produces an **empty output
file and exit code 0** — the worst failure mode available, because it looks like
success. A logger is reachable.

Same class, lower blast radius: `applyDeclaration` in `pkg/css/cascade.go` silently
drops every malformed or unsupported property, though `Resolver` already carries a
`logf` used elsewhere in the file.

### 0h. Add fuzz targets

The audit's sharpest structural point: **there are no fuzz targets for `pdf`, `docx`,
`pptx`, `rtf`, `epub`, `svg`, `css`, `font`, or `markdown`.** Fuzzing today covers
`heif/hevc` and `xlsx/xmlpart` — precisely the two packages where the fewest crash
bugs were found, which is not a coincidence worth ignoring.

A `FuzzOpen` per parser would have caught nearly every item in this phase. Without
them, Phase 0 fixes the bugs we happened to find rather than the class.

**All nine parsers now have targets.** `xlsx` and `pdf` landed on their own
branches, `css` and `svg` on another; this change adds the last six — `docx`,
`pptx`, `rtf`, `epub`, `font`, and `markdown`.

Results, and what they found:

| target | executions | outcome |
| --- | --- | --- |
| `rtf` | 54.5M | clean (the `\ilvl` bound from 0f holds) |
| `docx` | 16.5M | clean |
| `font` | — | **panic in a dependency**, fixed |
| `markdown` | — | **quadratic hang in a dependency**, fixed |

**Two of the six found defects, and both are in approved dependencies.** That is
now the recurring shape of this work — `x/net/html` in 0d-bis, and these two:

- **`pkg/font`: a panic inside `textlayout`.** `parseCmapFormat4` computes a
  slice bound from a subtable's own length fields and slices at a **negative**
  index when they disagree — `slice bounds out of range [-390:]`. It matters
  because a font program is untrusted document input (embedded in a PDF, or
  fetched as a web font) and the panic is raised during **open**, before any
  per-page recover exists. Fixed by recovering at the parse boundary, which is
  the guard this package already applies to `FontHExtents`/`FontVExtents` for
  exactly the same reason — the parse entry point simply lacked it.

- **`pkg/markdown`: goldmark is quadratic in per-line block nesting.** A line of
  N `- ` markers costs 0.49s at N=12,500, 2.6s at 25,000, 10.8s at 50,000 (four
  times the work for twice the input), and 200,000 markers — a **400 KB file,
  smaller than many READMEs** — does not finish in a minute.

  The measurement that shaped the fix: it is **depth**, not size. 50,000 list
  items over 50,000 lines parse in 79ms; the same 50,000 markers on ONE line take
  8.6s — a hundred times slower for half the bytes. So the bound is per line
  (`maxBlockNesting`, 1024) rather than a document-size cap, which would have
  rejected large legitimate documents while still admitting the small hostile one.

  Fenced code blocks are skipped by the scanner. That was not foresight — the
  over-reach test caught it: a README showing example Markdown inside ``` was
  refused, and measured, 50,000 markers inside a fence cost 238µs against 10.8s
  outside one. Counting them would have rejected a document that is both
  legitimate and cheap.

**The pattern is worth stating plainly for `docs/DEPENDENCIES.md`:** three of the
defects this phase found are performance or safety characteristics of approved
dependencies that we cannot fix upstream and must defend against at our own
boundary. The approved-dependency list vets licensing and purity; it does not vet
behaviour on hostile input, and nothing else in the repo does either.

Also worth noting: `pkg/pdf/filter/jbig2` is excluded from golangci-lint as vendored
code, so it receives no static analysis at all despite containing several of the
reported hangs.

### Suggested order

1. ~~Bound `parseRef` (0a)~~ — **done**; stopped a live panic *and* an
   unrecoverable OOM on the row axis
2. ~~Reject non-finite numbers (0b)~~ — **done**; needed the tokenizer fix as
   well as `parseNonNegNumber` to actually close the class
3. Cap `colspan`/`rowspan`/grid lines/`repeat()` (0c)
4. Depth caps in `parseArray` and friends (0d)
5. Visited-set in `walkPageTree` (0e)
6. Overflow-safe dimension guard (0f)
7. Plumb `Logf`, starting at `pdf_backend.go` (0g)
8. `FuzzOpen` per parser, wired into CI (0h)

Then re-read the "Limitations" paragraph in `README.md` — the one beginning
"Unsupported constructs degrade rather than crash" — and confirm it is true before
tagging. If any of this phase slips, that paragraph has to be qualified instead;
shipping the promise while knowing it is false is the one option that is not open.

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

  Read that number with Phase 0 in mind, though. `pkg/xlsx` sits at 84.3% and still
  panics on a 2 KB file, because line coverage measures which lines ran on inputs we
  wrote — and we do not write hostile inputs. That gap is what 0h is for.

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

Phases differ in how negotiable they are.

**Phase 0 is not.** It is not about the version number — the crashes and hangs are
equally real at `v0.9.0`, and the README makes the same promise either way. Tagging
anything while a 2 KB file panics the CLI means shipping a known-false claim. The
only alternative to fixing it is qualifying the README, and "unsupported constructs
degrade rather than crash, except when they crash" is not a sentence worth writing.

**Phase 1 is negotiable, via the version number.** It exists because a `v1.0.0` tag
is a promise about the exported API. If the appetite for that work is lower than the
appetite for shipping, tag **`v0.9.0`** instead: the API stays unfrozen, all the
functionality ships, and Phases 2–4 land at their own pace. What is not a good option
is tagging `v1.0.0` and quietly breaking the API in `v1.1`.

So: Phase 0, then pick a number.
