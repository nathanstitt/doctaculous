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
breaking signature change. — **Resolved:** the surface is now **seven packages**
(`omnidoc`, `docx`, `xlsx`, `pdf`, `crop`, `heif`, `resource`); everything else,
`render` included, is under `pkg/internal/`. See items 1 and 2.

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

`pkg/internal/css/cascade.go`, `parseNonNegNumber` rejects only `v < 0`, so `strconv.ParseFloat`
passes `inf`, `Inf`, `+Inf`, and `nan` straight through — measured, all four return
`ok=true`, and so does `infinity`, which the plan did not list. An infinite
`flex-grow` then makes the free-space loop in `pkg/internal/layout/css/flex.go` never
terminate.

Reproduced: `<div style="display:flex"><div style="flex-grow:inf">a</div></div>` — 66
bytes — hangs until killed.

**Verification changed two things here as well.**

*The hang is in `OpenHTMLBytes`, not in rasterizing.* Layout runs during open, so
it is not merely that "there is no `ctx` check on that path" — no `RasterizePage`
deadline exists yet to be checked. The caller has no interruption point at all.

*`parseNonNegNumber` was not the whole fix.* The tokenizer's own `parseFloat`
(`pkg/internal/css/token.go`) discarded `strconv`'s `ErrRange` and returned the `±Inf` it
came with. `readNumeric` scans only digits, `.` and a leading `-`, so `inf` cannot
be spelled through it — but 400 nines overflow to `+Inf` and reach the identical
hang, through a value that looks like an ordinary number. Fixing only
`parseNonNegNumber` would have left that door open for every consumer of
`Token.Num`. Both are now guarded; `pkg/internal/css/color.go` already documented this
exact reasoning for colour components, so the convention was in place.

Note the audit reported `nan` as hanging too; it does not — NaN degrades safely.
Fixed anyway: accepting NaN as a valid CSS number is wrong regardless.

Covered by `pkg/internal/css/nonfinite_test.go` (parser level) and
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
`attrSpan` in `pkg/internal/layout/css/build.go`, not `table.go`. All three attributes
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
line numbers, and `span` counts against `maxGridTracks` in `pkg/internal/css/grid_value.go`.
The track bound is on the *expanded* count, not the repetition count, since
`repeat(N, a b c)` yields N×3 tracks.

Covered by `pkg/internal/layout/css/unbounded_counts_test.go` (11 termination cases plus
the attribute-clamp table) and `pkg/internal/css/grid_bounds_test.go`.

### 0d. Stack overflows that `recover()` cannot catch — **PDF half DONE**

These raise `fatal error` via `runtime.throw`, which is **not recoverable**. This is
the finding that undermines the batch guarantee, because the per-page `recover` is
structurally unable to catch it:

- ~~`pkg/pdf/parser.go`, `parseArray`~~ — **done.** Reproduced: ~1.2 MB of `[`
  survives, ~1.5 MB raises `fatal error: stack overflow`. Bounded at
  `maxObjectDepth` (256), enforced in both `parseArray` and `parseDictOrStream`
  since they recurse through each other via `parseFromToken`.
- ~~`pkg/internal/layout/css/build.go`~~ — **done.** Reproduced, and worse than recorded:
  **80,000** nested `<div>` (~880 KB, not 1.6 MB) is enough.
  `sizeof(ComputedStyle)` is **2,144 bytes** and `generate` takes it by value, so
  nesting depth is stack depth times a two-kilobyte frame. Bounded at
  `maxBoxTreeDepth` (1024), degrading with a once-only log. Passing the style by
  pointer would raise the ceiling rather than remove it, and touches far more
  code; the bound is what makes the failure mode safe.

### 0d-bis. The HTML parser is quadratic in nesting — **DONE, not in the audit**

Found while reproducing 0d, upstream of it, and different in kind:
`pkg/internal/html.Parse` does not crash on deep nesting — it **hangs**.

`golang.org/x/net/html` resolves a close tag with `indexOfElementInScope`, which
scans the open-element stack linearly, so nesting is quadratic in open elements.
Measured *inside* `xhtml.Parse`:

| nested `<div>` | time in `xhtml.Parse` | this repo's own tree walk |
| --- | --- | --- |
| 30,000 | 3.7 s | ~6 ms |
| 60,000 | 15.1 s | ~12 ms |
| 200,000 | did not finish | — |

Four times the time for twice the depth — textbook quadratic. **The cost is
entirely in the dependency**, and it lands before this package gets control, so
it cannot be bounded after the fact.

Fixed by declining over-deep input up front: a tokenizer-only pre-pass counts
nesting and returns `ErrTooDeeplyNested` past `maxNestingDepth` (4096). It uses
`x/net/html`'s own tokenizer, so tag recognition matches the parser exactly
rather than relying on a hand-rolled scan of markup, and it is linear — 11 ms for
200,000 levels — with an early exit, so refusing costs microseconds regardless of
file size. Void elements are excluded, since they never enter the open-element
stack.

Worth recording in `docs/DEPENDENCIES.md`: this is a performance characteristic
of an approved dependency that we have to defend against ourselves, not a bug we
can fix upstream.


### 0e. PDF page-tree has no visited-set — **DONE**

`pkg/pdf/page.go`, `walkPageTree` caps depth but never marks visited nodes. It runs
inside `loadPages` during **open**, before any recover exists.

**Reproduced exactly as the audit reported** — 1,427 bytes → 4.2M pages in 0.78 s
(the audit said 0.76 s), and 240 bytes more → 67M pages in 12.8 s.

The important detail, which the "cyclic tree" framing obscures: **the depth cap of
64 never fires.** The blow-up is the *fan-out*, not the depth — each level doubles,
so 2^26 pages are produced at depth 26 and the walk terminates on its own, having
allocated gigabytes. A deeper cap would not have helped; only a visited set does.

Fixed by tracking visited object numbers per walk. Only a `Reference` can reach a
node twice, so tracking object numbers catches every cycle a file can express.
Verified against the 8 real-world corpus PDFs: page counts are byte-identical
before and after, so legitimately-shared page objects are unaffected.

### 0d/0e addendum — what fuzzing found that the audit did not

Per the decision to pull **0h** forward for `pdf`, `FuzzParse` was written before
fixing 0d/0e. That ordering paid for itself: the audit named two PDF defects, and
the fuzzer found **two more**, both in the same unrecoverable class.

- **Object-stream `/N` sizes a slice.** A 504-byte input declaring
  `/N 40000000020` asked for a 320 GB allocation. The header loop would have
  rejected the very first pair, but the process was already dead — an allocation
  cannot be validated after the fact. Now bounded by what the data can hold
  (each entry needs ≥4 bytes of header).
- **Object streams can recurse through the `Document`.** A stream whose `/N` is an
  indirect reference living inside *that same stream* cycles
  `loadObjStream → GetInt → Resolve → loadObject → parseObjectFromStream →
  loadObjStream` until the stack is gone. The parser depth cap cannot see this:
  every level builds a fresh parser. Fixed with an in-progress set.

Two more were found while chasing those, both of the shape **0f** describes but in
files 0f does not name:

- **`/Length` overflow.** `start+n <= len(src)` passes when `start+n` overflows to
  negative, then indexes the slice out of range
  (`index out of range [-9223372036854775764]`). Rewritten as
  `n <= len(src)-start`, which cannot overflow.
- **No ceiling on decoded stream size.** A 2.9 MB PDF whose content stream is a
  flate bomb decoded to 2 GB and drove peak RSS to **4.5 GB in 1.1 s**; the ratio
  holds for larger inputs. Bounded at `filter.MaxDecodedSize` (512 MB) *during*
  decompression via `io.LimitReader`, so the memory is never allocated — checking
  the length afterwards would be too late by definition.

After the four fixes: **181 million executions clean**, against a crash within 78
seconds before them.

### 0f. Overflow defeats the image-dimension guard — **DONE**

**The headline claim does not reproduce, and this is the first audit item that is
simply wrong.** `pkg/internal/raster/page.go` does not multiply in integer
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

**The bug is real, but in a different file.** `pkg/internal/raster/image.go`,
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

Covered by `pkg/internal/raster/image_bounds_test.go`, `pkg/internal/rtf/ilvl_test.go`,
`pkg/docx/zipbomb_test.go`, and `pkg/internal/epub/zipbomb_test.go`. The zip budgets are
test-overridable so proving them costs kilobytes rather than half a gigabyte —
allocating the real ceiling under `-race` is how a CI runner gets OOM-killed
(see item 13).

### 0g. Silent wrong output on the PDF path — **DONE (PDF half)**

`pkg/omnidoc/pdf_backend.go` returns bare on error with no log. Every
PDF→anything conversion routes through it, so a failure produces an **empty output
file and exit code 0** — the worst failure mode available, because it looks like
success. A logger is reachable.

**Reproduced exactly**, at both the library and the CLI: a 4-line PDF whose
`/Contents` points at a missing object converts to a zero-byte `.md` with
`err == nil` and **not one diagnostic**, even with `WithLogf` supplied.

**Verification found the real root two layers below the plan's.** The bare return
in `cssboxRoot` is real and is fixed, but it is not what made this input silent:

1. `cssboxRoot` passed **`nil`** as `extract.Lower`'s logger, so even `Lower`'s
   own per-page messages were discarded before reaching anyone.
2. The logger never arrived anyway: `openDetected`'s PDF branch dropped `opts`
   entirely — `&pdfRenderer{doc: d}`, no config applied. "A logger is reachable"
   was true in principle and false in practice.
3. **`pdf.Page.ContentBytes` returned `(nil, nil)`** for a `/Contents` that is
   present but unresolvable. Its type switch had no default, so a dangling
   reference fell through to an empty result.

(3) is the actual origin, and it is a correctness bug in a public API rather than
a logging gap: a blank page and a lost page were the same value. `ContentBytes`
now separates them — *no* `/Contents` (or an empty array) is a blank page with no
error; a present-but-unreadable one is an error. Verified against every
checked-in PDF fixture: page counts and content sizes unchanged.

**CLI.** `cmd/omnidoc` wired no logger at all, so the library could degrade as
honestly as it liked and nothing would surface. `convert` now takes **`-v`**,
which installs the degradation logger onto stderr; default-off keeps ordinary
runs quiet and scripts unaffected.

**What was deliberately NOT done, and why.** An earlier attempt made the CLI exit
non-zero when a conversion wrote zero bytes. Measurement killed it: an empty HTML
document converting to Markdown legitimately produces a zero-byte file, so the
check rejected a correct conversion. Narrowing it to formats that are never empty
(PDF, DOCX, …) removed the false positive but also removed all the value — those
targets emit structure regardless, so it could never fire on the case 0g is about.
**No byte-count signal separates "lost the content" from "there was no content"**,
and `PageCount` does not either (an empty HTML document still lays out one page).
Doing this properly needs the caller to be able to ask *"did structure extraction
fail?"* — which is Phase 1 item 4's `ErrNoStructure`. Left for that item rather
than guessed at here.

### 0g-bis. `applyDeclaration` drops properties silently — still open

Same class, lower blast radius: `applyDeclaration` in `pkg/internal/css/cascade.go` silently
drops every malformed or unsupported property, though `Resolver` already carries a
`logf` used elsewhere in the file.

Scoped while doing 0g and deferred deliberately. `applyDeclaration` is a free
function with **nine call sites**, several inside shorthand expansion — so a
logger threaded through it would also report properties the author never wrote (a
`font:` shorthand fans out into `font-weight`, `font-style`, `line-height`, and
the ones that do not apply are not authoring errors). Doing it right means
distinguishing author-written declarations from synthesized ones, which is a real
design question rather than a plumbing job. It is a missing diagnostic, not wrong
output, so it does not block the tag on 0g's own argument.

### 0h. Add fuzz targets — **DONE, all nine parsers**

The audit's sharpest structural point: **there are no fuzz targets for `pdf`, `docx`,
`pptx`, `rtf`, `epub`, `svg`, `css`, `font`, or `markdown`.** Fuzzing today covers
`heif/hevc` and `xlsx/xmlpart` — precisely the two packages where the fewest crash
bugs were found, which is not a coincidence worth ignoring.

A `FuzzOpen` per parser would have caught nearly every item in this phase. Without
them, Phase 0 fixes the bugs we happened to find rather than the class.

**This prediction was tested and held.** `FuzzOpenBytes` (`pkg/xlsx`) and
`FuzzParse` (`pkg/pdf`) have landed. Writing the PDF target *before* fixing 0d/0e
found two defects the audit missed and two more of 0f's shape in files 0f does not
name — four in total, every one of them in the unrecoverable
`runtime.throw` class. See the 0d/0e addendum above.

The lesson, which the remaining targets bore out: **write the fuzz target
first.** The audit's per-item findings are real but they are a sample, and
reading a parser does not surface the shapes a mutator finds in minutes.

Worth knowing for whoever picks this up: a crash found only under `-fuzz` may not
reproduce from the persisted corpus entry, because the killing input is not always
written before the process dies. Writing each input to a file at the top of the
fuzz body, and removing it on success, is what actually recovered the 504-byte
`/N` case here.

**`css` and `svg` landed here** (`pdf` and `xlsx` on their own branches). Four
targets: `css.Parse` (parse + cascade), `ParseDeclarations`, `ParseColorValue`,
and `svg.Parse` (XML + cascade + scene build, seeded from 40 real resvg
fixtures).

`pkg/internal/css` came back **clean** — 20M, 144M and 169M executions on the three
targets, no crashes. That is a genuine result rather than a weak target: the
package is written defensively, `Parse` is documented as total, and the property
parsers already reject what they cannot use.

`pkg/internal/svg` found **two defects in under four seconds each**, both escaping the
public `Parse` on documents under 70 bytes:

- **`<text x="0">0 <A>` panicked** with `index out of range [1] with length 1`.
  `recordLists` stores a `[start,end)` span over the character slice, and
  `dropTrailingSpace` then shrinks that slice. The parser already knew about this
  hazard — `trimLengths` exists for exactly it, and its comment says so — but the
  position lists were missed while the `textLength` ranges were handled.
- **`<path d="M0 0Z0 0l 0 0">` hung forever.** A number where a command is
  expected means "repeat the previous command", but `closepath` consumes no
  arguments, so repeating it never advances the scanner. SVG's path grammar has
  no implicit repetition for `closepath`, so stopping is both the correct parse
  and the terminating one.

After both fixes: 80M executions clean.

Two things worth carrying to the remaining parsers. First, **a clean fuzz run is
information, not a failed attempt** — `css` being clean at 300M+ executions is
evidence the package is sound, and it took the same effort to establish as the
two SVG bugs. Second, **seed from the real corpus**: the SVG target seeds from 40
resvg fixtures, which is what gave the mutator valid structure to corrupt rather
than making it discover SVG syntax from scratch.

**All nine parsers now have targets.** The last six — `docx`, `pptx`, `rtf`,
`epub`, `font` and `markdown` — completed the set.

Results for those six:

| target | executions | outcome |
| --- | --- | --- |
| `rtf` | 54.5M | clean (the `\ilvl` bound from 0f holds) |
| `docx` | 16.5M | clean |
| `font` | — | **panic in a dependency**, fixed |
| `markdown` | — | **quadratic hang in a dependency**, fixed |

**Two of the six found defects, and both are in approved dependencies.** That is
now the recurring shape of this work — `x/net/html` in 0d-bis, and these two:

- **`pkg/internal/font`: a panic inside `textlayout`.** `parseCmapFormat4` computes a
  slice bound from a subtable's own length fields and slices at a **negative**
  index when they disagree — `slice bounds out of range [-390:]`. It matters
  because a font program is untrusted document input (embedded in a PDF, or
  fetched as a web font) and the panic is raised during **open**, before any
  per-page recover exists. Fixed by recovering at the parse boundary, which is
  the guard this package already applies to `FontHExtents`/`FontVExtents` for
  exactly the same reason — the parse entry point simply lacked it.

- **`pkg/internal/markdown`: goldmark is quadratic in per-line block nesting.** A line of
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

Also worth noting: `pkg/internal/filter/jbig2` is excluded from golangci-lint as vendored
code, so it receives no static analysis at all despite containing several of the
reported hangs.

### Suggested order

**Revised: fuzz the parser before fixing it.** The original order put 0h last. On
the evidence above that is backwards — the PDF target found twice as many defects
as the audit had for that package, in one afternoon, and three of the four could
not have been caught by a `recover`. For each remaining parser, write `FuzzOpen`
first and let it set the work list.

1. ~~Clamp `parseRef` (0a)~~ — **done**
2. ~~Reject non-finite numbers (0b)~~ — **done**
3. ~~Cap `colspan`/`rowspan`/grid lines/`repeat()` (0c)~~ — **done**
4. ~~Depth cap in the PDF object parser (0d)~~ — **done**; the
   `layout/css/build.go` half remains
5. ~~Visited-set in `walkPageTree` (0e)~~ — **done**
6. Overflow-safe dimension guard (0f) — note two instances of this shape have
   already been fixed in `pkg/pdf`; `render/raster`, `rtf`, and the docx/epub zip
   ratios remain
7. Plumb `Logf`, starting at `pdf_backend.go` (0g)
8. `FuzzOpen` for the remaining parsers, wired into CI (0h) — `xlsx` and `pdf`
   are done

Then re-read the "Limitations" paragraph in `README.md` — the one beginning
"Unsupported constructs degrade rather than crash" — and confirm it is true before
tagging. If any of this phase slips, that paragraph has to be qualified instead;
shipping the promise while knowing it is false is the one option that is not open.

---

## Phase 1 — API freeze

After the tag, exported API is frozen under semver: a breaking change means `v2`.
These four are the ones we would regret.

### 1. Move the engine packages under `internal/` — **DONE**

**50 packages were exported. Eight are public API.** There were only two
`internal/` directories, both deep (`pkg/xlsx/internal`, `pkg/internal/render/internal`);
everything else would have frozen on the tag.

`pkg/css` alone would have frozen **371 exported symbols** — the plan estimated
87, an undercount — including `ComputedStyle`, whose own doc comment says the
typed property set is *"deliberately minimal"* and that properties outside it
*"do not yet populate a typed computed field. That is expected, not a gap."* That
is a written promise that the type will change, on a type v1 would freeze.

**Result: 50 exported packages → 8.** Public: `omnidoc`, `docx`, `xlsx`, `pdf`,
`render`, `crop`, `heif`, `resource`. The other 40 moved to a single flat
`pkg/internal/`.

**Verification changed the plan's lists in three ways.**

*The move-list and keep-list were mutually inconsistent.* Three "keep public"
packages exposed "move to internal" types in exported signatures: `font.New`
returns `content.GlyphSource`, and `svg.HostContext` has `css.Stylesheet` and
`css.Node` fields. An external caller could not have named those types — the
packages would have compiled but been unusable. Resolved by moving `font` and
`svg` internal too; both leaks were only exercised from packages that were
themselves moving, so nothing was lost.

*The plan classified 30 of 50 packages and was silent on 20* — including `html`,
`pptx`, `epub`, `rtf`, `markdown`, `pdf/extract`, `render/raster` and `webp`.
Checked individually: every one is imported only from inside this repo, so all
moved.

*Per-parent `internal/` directories do not work here.* The first attempt put each
under its public ancestor (`pdf/internal/content`, `render/internal/raster`), and
the compiler rejected it: Go scopes `internal` to the parent of the `internal`
element, so `render/internal/raster` cannot import `pdf/internal/content`. A
single top-level `pkg/internal/` is the only arrangement that lets the engine
packages keep importing each other.

Scale, as predicted: 263 files needed import rewrites (`layout/cssbox` was
referenced by 104, `css` by 78, `layout` by 66). The diff is mechanical and
compiler-checked, and the full suite — including every golden image — passes
unchanged, which is the property that matters for a pure move.

**What the compiler could not check.** Three classes of stale reference survived
the build and had to be found by hand:

- `.golangci.yml` excluded the vendored jbig2 decoder by its old path, so 14 lint
  findings appeared in code we deliberately do not lint.
- Relative `testdata` paths in tests broke wherever a package changed depth
  (`filepath.Join("..", "..", ...)`), including cross-package ones where the
  sibling also moved — and one, `imageconv` → `heif`, where the sibling stayed
  public and the other did not.
- 35 non-Go files referenced old paths. The docs are updated; `testdata/htmldoc/`
  and the goldens are deliberately **not**, because that showcase's text is
  rendered into golden output, so a prose fix there would force a golden
  regeneration and bury the real diff.

Verified from outside the module: a separate module can import all eight public
packages, and importing `pkg/internal/css` fails with *"use of internal package …
not allowed"*.

### 2. Decide `render.Device`'s fate — **DONE: the whole package is internal**

A 15-method interface with no embedding escape hatch. The README called it "the
seam… new backends bolt on without touching them", which invites external
implementations — and adding a 16th method after v1 breaks every one of them.

This interface has demonstrably grown: `BuildClipMask`, `BuildLuminanceMask`, and
`RenderOffscreen` were added over time, and `EndGroup`'s doc comment records a prior
signature change in so many words — "Pre-combining them into a single opaque
GroupMask value, *as an earlier revision of this interface did*, silently breaks any
backend…". An interface that already changed shape twice is the worst possible
freeze candidate. Confirmed against `git log -S`: both additions and the `EndGroup`
break are real commits.

The plan counted four in-repo implementers; there are **five** — `svgwrite` was
added by the SVG output work while this plan was being executed. An interface
still gaining implementers is not one to freeze.

**Item 1 changed the decision.** Once the engine packages moved, a fact emerged
that neither of the plan's two options anticipated: **no public package exposes
`render` in any exported signature.** `pkg/omnidoc`'s uses are all on unexported
functions and an unexported interface (`vectorPages`), and all five implementers
are internal. `pkg/render` was public only because it had not been moved.

So the choice was not "how do we shape a public `Device`" but "is `render` public
at all", and the answer is no. The whole package moved to `pkg/internal/render` —
**69 exported symbols across 25 types** (`Device`, `Shader`, `Path`, `Matrix`,
`FillPaint`, …), none of them frozen now.

121 files needed the import rewrite; it built first try, which is itself the
evidence that nothing public depended on it. Public API is now **seven packages**.

The "core + capabilities" option was rejected as speculative: it is real design
work plus a type-assertion fallback at every call site, to build an extension
point no caller has asked for. Opening the interface later is additive and always
possible; withdrawing it after a tag is not. The README now says so plainly
rather than describing a seam that reads as an invitation.

### 3. Export `WithContext(ctx)` as an `OpenOption` — **DONE**

`context.Context` is missing from 40 of 45 `Open*` functions, and absent entirely
from `pkg/docx` and `pkg/xlsx` — the two packages the README designates as supported
surfaces for tinycld. `docx.Open`, `xlsx.Open`, `xlsx.Edit`, and `File.Save` all do
full-package zip and XML parsing with no way to cancel.

The codebase already felt this and worked around it with a parallel `*Context` naming
family for HTML and URL only. `OpenHTMLBytesContext`'s doc admits the plumbing
exists but is unexported: *"ctx rides in as the unexported withOpenContext option…
(there is no exported way to, today)"* — quoted verbatim from the source, and still
accurate.

Exported as `WithContext`, a one-line wrapper over the existing unexported option.
Verified by cancellation, not by compilation: an already-cancelled context makes
`OpenHTMLBytes` return `context.Canceled`, and — the point of the change — the
same option cancels the **Markdown, text and CSV** frontends, which the `*Context`
family never covered. Ordering matches those functions (they prepend, so a
caller's own `WithContext` still wins), and a nil ctx is ignored.

Note this does **not** reach `pkg/docx` / `pkg/xlsx` themselves: their `Open`,
`Edit` and `Save` take no options at all, so cancelling their zip and XML parsing
needs new signatures rather than a new option. That is a separate change, and one
worth making before the tag for the same reason as this one.

### 4. Reconcile the duplicate `ErrSheetNotFound` — **DONE**

Two sentinels that do not interoperate:

- `pkg/omnidoc/xlsx_frontend.go:15` — `errors.New("xlsx sheet not found")`
- `pkg/xlsx/edit.go:21` — `errors.New("xlsx: sheet not found")`

Verified by compiling a program against both packages: `errors.Is` between them
returns **false**, and the messages differ only by a colon. A caller using both —
exactly the tinycld case — will write the wrong check and it will compile and pass
review. **Reproduced exactly**, both directions false.

Fixed by aliasing: `omnidoc.ErrSheetNotFound = xlsx.ErrSheetNotFound`. Both names
keep working, so no caller breaks, and `pkg/xlsx` owns the concept because it is
the lower-level package that resolves sheet names. The `omnidoc` message gains a
colon as a result — a visible string change, harmless because the whole point is
that nobody should have been matching on it.

`ErrNoStructure` and `ErrPageOutOfRange` added and wired: nine writers and the
page-range helper now wrap them. The nine did not even agree on their wording —
seven said "document has no convertible structure" and two said "document is not a
reflow document" — so a caller string-matching had two strings to guess at.

**Writing the test found a live bug the plan did not have.** Asserting
`ErrNoStructure` needs a document with no box tree, and the obvious candidate (an
opened image) turned out to have one. The real case is **SVG**: its renderer
satisfies `reflowTree` but is built with pages and no root, so `cssboxRoot()`
returns nil, the type assertion succeeds, and the writers walked a nil tree —
producing **an empty output file and a nil error**. That is precisely the failure
mode item 0g was about, on a path 0g never reached: `omnidoc convert x.svg x.md`
wrote a zero-byte file and exited 0.

Fixed with a `structureRoot` helper that checks the ROOT rather than just the
interface, used by all nine writers. `svg → md` now exits 1 with a matchable
error; `svg → png` is unaffected.

Adding sentinels later is technically additive, but errors returned by v1.0 stay
unmatchable forever, so callers written against v1.0 keep string-matching.

---

## Phase 2 — Documentation integrity

All cheap. All currently wrong.

### 5. The backlog says shipped features are missing — **DONE**

- **`transform` shipped** (PR #140, `FEATURES.md:336`) but `docs/GAPS-REMEDIATION-2.md`
  still says it "remains split out and unimplemented" at `:148`, `:164`, and calls it
  "currently silent" at `:421`/`:426`. **Fixed** — both sites now point at FEATURES.md
  and showcase §42.
- **`writing-mode` never reached `SCOPE.md`.** ~~grepping `SCOPE.md` returns zero hits~~
  — **this claim was stale when written.** `SCOPE.md` DOES carry the entry, and it says
  the gap is moot because vertical text shipped. Verified in code: `WritingMode` is a
  real `ComputedStyle` field, inherited per CSS Writing Modes 4, with a `cascade.go`
  case; `text-orientation` ships too, on both the HTML and SVG paths. What remains is
  ordinary outstanding work (chiefly the UAX #50 table) in `docs/CSS-LAYOUT.md`. The
  stale record was in GAPS-REMEDIATION-2's item 14, which still described the property
  as deferred-and-unimplemented; **that** is what was corrected.
- **The status header is stale.** `FIDELITY-BACKLOG.md:15` claims "48 done · 2 in
  progress · 30 open" as of 2026-07-29. **The COUNTS are correct** — recounted against
  the actual ☑/◐/☐ markers (the naive grep over-counts by two: the legend line and one
  prose mention). Only the date was stale, and it now reads "counts verified".
- **A dangling sentence** at `FIDELITY-BACKLOG.md:435`: "see FEATURES.md and." **Fixed** —
  it now names the "CSS Paged Media" entry it was reaching for.
- **Three questions addressed to the maintainer** sitting in a shipped doc at
  `FIDELITY-BACKLOG.md:453-456`. **Fixed** — all three are answered by history
  (DOCX in scope, M1–M4 landed and M5 open; A1 sequenced first; small stacked PRs), and
  the answers are recorded in place so they are not re-litigated.

### 6. Two silent degradations, and a backlog entry that understates them — **DONE**

`unicode-range` and `font-display` are **not parsed at all**. `parseFontFace`
(`pkg/internal/css/fontface.go:26-36`) switches on `font-family`, `src`, `font-weight`, and
`font-style`; everything else is discarded with no log, and `FontFace` has no field
for them. Grep finds them nowhere in non-test source.

`FIDELITY-BACKLOG.md:238` describes `unicode-range` as "captured-but-ignored; whole
face used for every rune". That is wrong — it is not captured. Note
`pkg/omnidoc/html_webfont_test.go:91` feeds both descriptors into a test, which reads
as coverage but exercises neither.

Fix: add the warn-once log, correct both entries. **Done** — G2/G3 in
`FIDELITY-BACKLOG.md` now state that the descriptors were not captured at all and are
reported rather than dropped. Actually implementing `unicode-range` subsetting stays a
1.1 item.

Both descriptors now report through `Stylesheet.Unsupported` — the same carried-as-data
channel the dropped-selector diagnostics use, because `Parse` has no logger and cannot
gain one (`html.UAStylesheet` is a package-level var it initializes, so there is no
caller to hold one). `UnsupportedSelector` gained a `Descriptor` flag: the inherited
message "rules using it are ignored" is WRONG for a descriptor, since the face still
loads and only that descriptor is dropped.

`pkg/omnidoc/html_webfont_test.go` asserted only "no error" and would have passed
whether or not the descriptors were handled. It now asserts the diagnostics.

Three more silent paths worth a log while in here — **two logged, one deliberately not:**

- `position: relative` on a text-only inline box — **now logged.** Confirmed silent
  first: a `<span style="position:relative;left:40px;top:15px">` produced a
  byte-identical PDF to the same span without it. Warns once per layout on a non-zero
  offset; a zero/absent offset stays quiet, since that is the
  establish-a-containing-block idiom and it works.
- Margin-collapse edge cases — **now logged** for the empty-block collapse-through case.
  Measured: an empty `<div style="margin:40px 0">` between two paragraphs opens an 80pt
  gap where a browser gives 40pt. Clearance and the `min-height` interaction stay silent;
  both need the same across-a-split-point collapse state that item E3 is about.
- Grid flow-axis-locked placement — **deliberately NOT logged.** Unlike the other two,
  this always yields a valid, non-overlapping placement and differs from a browser only
  in WHICH free slot a sparse locked item lands in. There is no runtime test for "this
  diverged" short of also running the browser algorithm — which is the fix — so a log
  would fire on every sparse locked item and tell the author nothing actionable. The
  reasoning is recorded on the I2 backlog entry.

And one claim verified before repeating it: `FIDELITY-BACKLOG.md:335` says PDF tiling
patterns are "skipped + logged". **The backlog is right and this plan's doubt was
wrong.** The log exists at `pkg/internal/raster/page.go` ("unsupported /PatternType %d
(only shading patterns)") and `pkg/internal/content/shading.go`. The audit had looked at
`interp.go:47`, which is an interface doc comment, not the implementation. The entry now
cites the real sites so this is not re-doubted.

### 7. Move shipped work out of the backlog docs — **DONE**

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

**Outcome.** All three files were deleted and replaced by a single `docs/BACKLOG.md`
holding the 30 open + 2 in-progress items. The done-work prose was NOT extracted — it
lives in git history (`git log --diff-filter=D --name-only`), which the new file explains
how to read. Two things were promoted rather than dropped, because they are reusable
guidance rather than records of finished work, and `TESTING.md` had neither: "assert on a
specific colour, not on *something was painted*" and "a sound measurement can carry an
unsound inference". Both are now in `docs/TESTING.md`.

Every carried-over entry was re-verified against the code, which is how the stale-entry
count went from seven to eight: **E1 claimed the full `vertical-align` keyword set had
landed, and only `super`/`sub` are implemented** — the first stale entry found running in
the *overstating* direction rather than the understating one. I1 was also imprecise (a
`LineName` kind does exist, for grid-area names; the missing piece is track-list storage
and resolution, not the tokenizer). Both corrections are recorded in `BACKLOG.md`.

---

## Phase 3 — `margin: 0 auto` does not center — **DONE**

**Verified by rendering, not by reading the comment.** A 200px box with
`margin: 0 auto` in a 600px viewport renders hard against the left edge.
Reproduced before fixing (X=0, want X=200) and mutation-verified after.

`pkg/internal/layout/css/block.go` — `usedEdges` resolved auto margins to 0, and said so:
*"Auto margins compute to 0 in this PR (horizontal margin:auto centering is
deferred)."* That comment now points at `resolveAutoMarginsX` instead.

This is the most common centering idiom in CSS. Every centered-layout document we
render is wrong today, silently. The backlog sizes it small–medium, and the
absolutely-positioned equivalent already shipped, so the concept is proven.

Needs a showcase section and regenerated goldens per the project rule.

**Outcome.** `resolveAutoMarginsX` implements CSS 2.1 §10.3.3, called from `layoutBlock`
after the used width is known. It deliberately does NOT live in `usedEdges`: that
function has 16 callers including intrinsic-width measurement and float sizing, where an
auto margin genuinely is zero, so resolving there would have changed sizing paths that
must not move.

Four cases, all tested: two auto margins split the leftover evenly; one absorbs all of it
and pushes the box to the opposite edge; a specified margin is honoured with auto taking
only the remainder; and — the one that would have been a bug — a box WIDER than its
container has negative leftover, which is floored at 0 so it overflows right instead of
being dragged off the left edge of the page. The leftover is measured from the BORDER
box, so padding and borders count; a content-box measurement would mis-centre by half
the insets.

Showcase §44 covers all five cases plus the over-wide clamp. **Zero layout movement in
the existing corpus:** the full suite passed before the goldens were touched, meaning no
document in the corpus used the idiom — which is why nothing caught this. The 44 existing
golden pages each changed by exactly 30 pixels, in a 6×7 box in the footer, verified by
decoded-pixel comparison and by cropping the region: the page-count total reading
"21 / 44" became "21 / 45". Nothing else moved.

`docs/BACKLOG.md` E2 is removed (29 open now), per the rule that shipped work leaves the
backlog.

---

## Phase 4 — Release surface

9. **`pkg/internal/layout` has no package doc comment** — the only one of 50 missing it. It
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
  (`pkg/internal/imageconv/imageconv.go:25`) is 0% even cross-package, and it is the
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
