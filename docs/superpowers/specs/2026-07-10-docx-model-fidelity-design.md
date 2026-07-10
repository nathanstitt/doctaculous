# pkg/docx reader fidelity: revisions, comments, notes, frames, wrap — the public-model PR

First of three PRs making pkg/docx a supported public read+write DOCX document model (the
tinycld "text" adoption path: their collaborative editor stores its canonical document as a
.docx and round-trips DOCX ⇄ ProseMirror ⇄ Yjs on every save; doctaculous replaces their
hand-rolled OOXML layer + wordZero). This PR closes the READ-side vocabulary gaps; the
model-level writer with the Parse∘Write round-trip contract is PR 2; unifying
pkg/render/docxwrite onto that writer is PR 3.

## Model additions (all additive)

- **Tracked changes**: `ParaChild.Revision` (`Revision{Kind Insert|Delete, ID, Author, Date,
  Content []ParaChild}`) — w:ins/w:del as true CONTAINERS (they wrap hyperlinks and nest), so
  grouping and the revision identity survive exactly once. Deleted runs' `w:delText` parses
  as ordinary text (deletion-ness lives on the wrapper). Property changes ride the props:
  `RunProps.RPrChange`, `ParagraphProps.PPrChange`, `CellProps.TcPrChange` (each
  `{Mark RevisionMark, Previous <props>}` — the OUTER props remain the after state, matching
  Word's semantics), plus `TableCell.Ins/Del` (w:cellIns/w:cellDel marks).
- **Comments**: `Document.Comments map[int]*Comment{ID, Author, Initials, Date, Body []Block}`
  (word/comments.xml); in-text anchors as `ParaChild.CommentStart/CommentEnd` markers (ranges
  cross run/hyperlink boundaries, so they are positions, not containers) and `Run.CommentRef`
  reference runs. v1 limit, pinned by test: markers inside w:hyperlink hoist outward (start
  before the link, end after) — the reverse direction is expressible by splitting a link.
- **Endnotes**: `Document.Endnotes` mirroring footnotes; the notes container is now `Notes`
  (`ByID map[int][]Block`, exported for the writer) — renamed from the footnote-specific type
  BEFORE declaring API stability; both parts share one parser (`parseNotes`).
- **Frames/drop caps**: `ParagraphProps.Frame *FramePr{DropCap, Lines, Wrap, HAnchor, VAnchor,
  W, H, HSpace}`.
- **Drawings**: `Anchored`, `WrapKind` (square/topAndBottom/tight/through/none), `AlignH`
  (wp:positionH/wp:align), `Description` (docPr alt text).
- **Tables**: `Border.Style` — the ST_Border name verbatim (single/dashed/dotted/double/...);
  rendering still draws any non-none style solid, conversion round-trips the name.
- **Sections**: `ParagraphProps.SectPr` keeps the paragraph attachment point of a mid-document
  section break (the geometry was already collected into Document.Sections) so the writer can
  re-emit multi-section documents in place.
- **Numbering restructure**: exported `Abstract`/`Instances` maps, `NumLevel.Start`,
  `NumInstance.Overrides[ilvl].Start` (w:startOverride), and `StartAt(numID, ilvl)` resolving
  override → abstract start → 1. `NewNumbering()` for writer-side construction.
- **Plumbing**: `Relationship.Type` exported; `Hyperlink.Target` resolved at parse (post-pass
  over body/notes/comments/headers/footers); `Style.Name` (w:name); run `Shd` shading;
  `Run.EndnoteRef`/`Run.CommentRef`; **`Document.ExtraParts`** — customXml/* parts preserved
  verbatim, the app-data escape hatch (tinycld text keeps its suggestions part schema without
  post-processing the zip).

## Rendering behavior (pinned by tests; all prior goldens byte-identical)

- **Revisions render FINAL state** — Word's "No Markup" view: `effectiveContent` in the
  lowering includes insert content in place and excludes delete content.
- **Comments are invisible** — markers and reference runs contribute nothing.
- **\*PrChange after-state renders** (it is already the outer props — zero lowering change).
- **Drop cap degrades to a plain paragraph** (Frame is not consulted by layout).
- Renderable upgrades, exercised by the new `fidelity` core fixture + golden: endnote markers
  (the footnote superscript mechanism), run shading → text background (highlight paints over
  it), list `start`/`startOverride` seeding the counter (sublists re-seed on reset — Word's
  restart semantics), and **anchored square-wrap drawings → CSS floats** (emitted as a
  block-level float sibling BEFORE the paragraph block, so the engine's float machinery wraps
  the paragraph's lines beside it; every other anchor mode degrades to inline flow).

## Tests

Parser: one unit per construct (parse_revisions_test.go — ins/del/delText/nesting/hyperlink
wrapping, rPrChange/pPrChange/tcPrChange before-after, comment markers + hoisting, endnote and
comment reference runs, run shd, border style names, cellIns/cellDel, anchored drawing facts,
paragraph SectPr attachment, comments part, numbering start/override/StartAt) plus the
package-level plumbing test (endnotes/comments parts, ExtraParts, Target resolution,
Relationship.Type). Lowering: degrade pins (final-state, comment invisibility, drop-cap
degrade) + upgrade pins (endnote marker, shading, float order + inline fallback, list start)
in pkg/docx/cssbox/fidelity_test.go. Visual: `fidelity` in testdata/gen/docx `Core` →
`docx-fidelity.png` golden (eyeballed: deletion absent, anchors invisible, superscript marker,
list at 5, image floated right with wrapping text).
