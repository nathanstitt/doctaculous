# EPUB input (pkg/epub): books through the HTML pipeline

First half of the EPUB format (the plan's area E; the writer `pkg/render/epubwrite` is E2).
This REVERSES the repo's long-standing "EPUB is out of scope" note — the any⇄any conversion
goal made it a requirement, and it keeps drive's .epub thumbnails working after go-fitz
retires. The bet the old note encoded still holds: EPUB content IS XHTML, so the HTML
pipeline does all the real work — the reader is only a container walk.

## Reader (pkg/epub)

`Open`/`OpenBytes` → `Book{Title, Chapters, StylesheetRefs, InlineCSS}` + `Resource(ref)`:

- **Container walk**: META-INF/container.xml → the OPF package document → dc:title, the
  manifest (id → href), and the spine in reading order (`linear="no"` auxiliary items
  skipped). EPUB 2 and 3 both resolve through the spine; the NCX is not consulted (it
  duplicates the order).
- **Chapter extraction**: each spine document's `<body>` inner markup is taken by a tag-level
  scan — XHTML is markup the lenient HTML parser downstream accepts verbatim, so there is no
  re-serialization pass to perturb content. Head `<link rel=stylesheet>` hrefs (deduped,
  first-use order) and `<style>` blocks are collected.
- **Resources**: refs resolve against the OPF's directory — the single-content-directory
  layout every real-world book uses — with fragment/query stripping and a root-relative
  fallback.
- **DRM**: a META-INF/encryption.xml refuses the book with the typed `ErrEncrypted` (pinned).

## Frontend (OpenEPUB*)

One merged HTML document: the collected styling in the head, each chapter's body wrapped in a
`<section>` with `break-before: page` (chapters start fresh pages when paginated; unpaginated
reflow is the default single tall page). A loader adapter backs `<img>`/`<link>`/@font-face
resolution from the container; `openDetected` skips the dir-rooted loader default for EPUB
(it would override the container loader — a caller's explicit loader still wins, documented
as losing the container's media).

## Wiring

`FormatEPUB` (input-only until E2); detection via the OCF signature — the `mimetype` zip
entry declaring `application/epub+zip` — in classifyOPC; `.epub` extension; the
`application/epub+zip` MIME row flipped from its deliberate-Unknown placeholder; CLI help.
CLAUDE.md's two out-of-scope notes are removed/annotated in the same PR.

## Tests

`epub-specimen` golden (a generated two-chapter book: package CSS coloring the headings, an
inline-styled note, lists, a container-resolved image; chapter two breaking to page 2 —
eyeballed); detection + format stamping; epub→markdown conversion pinning headings, emphasis,
lists, links, and tables across chapters; the DRM refusal. The gen/epub builder emits the
OCF-conformant container (stored-first mimetype entry, deterministic). A small public-domain
real .epub remains a follow-up alongside the other real-file fixtures.
