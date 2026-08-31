# Security policy

## Reporting a vulnerability

Report privately through GitHub's [private vulnerability
reporting](https://github.com/nathanstitt/omnidoc/security/advisories/new) rather than
opening a public issue. If that is unavailable to you, email nathan@stitt.org.

Please include the input that triggers it. A file that reproduces the problem is worth
more than a description of it — most of what matters here is shaped by exact byte
sequences, and a reproducer turns a report into a test.

Expect an acknowledgement within a week. Since this is a small project, a fix timeline
depends on severity and on how deep the defect sits; you will get a real answer either
way rather than silence.

## What this project's threat model is

Omnidoc parses **untrusted documents**: PDF, DOCX, XLSX, HTML, EPUB, SVG, RTF, Markdown.
Every one of those formats can be hostile. The design assumption is that input is
attacker-controlled and the process must survive it.

Concretely, these are treated as vulnerabilities:

- A panic, a stack overflow, or an unrecoverable crash reachable from a parsed document.
- Unbounded memory growth or a hang (including a decompression bomb) from a small input.
- Reading a file, opening a network connection, or executing anything the caller did not
  ask for.
- A document escaping its declared resource loader to reach the local filesystem.

These are **not** vulnerabilities, though they are still worth reporting as bugs:

- A document rendering incorrectly or unfaithfully. Fidelity gaps are tracked in
  [docs/BACKLOG.md](docs/BACKLOG.md).
- An unsupported construct being skipped and logged. That is the intended degradation.
- Resource use proportional to a genuinely large input.

## Hardening already in place

A dedicated hardening pass (Phase 0 of the v1 plan) fuzzed the parsers and fixed every
crash and hang it found. The bounds it left behind are load-bearing, so if you are
looking for weak points, these are the edges:

- Recursion depth limits on the PDF object parser, the HTML tokenizer, the Markdown
  block parser, the CSS box tree, and RTF list nesting.
- Cycle detection in the PDF page tree and object streams.
- A 512 MB cap on any single decompressed stream, plus an aggregate per-archive budget
  for DOCX and EPUB parts, so a zip bomb cannot amplify without bound.
- Integer-overflow-safe bounds checks on image dimensions, spreadsheet cell refs, table
  spans, and grid track counts — compared by division rather than multiplication.
- `recover()` around third-party font parsing, which is the one place a dependency can
  panic on malformed input.

Three of the defects that pass found were in **dependencies**, not in this code. If you
find one there, report it here as well as upstream; a fix in this repo may have to be a
bound rather than a patch.

## Supported versions

Pre-1.0, only the latest release gets fixes. That changes at v1.0.0.

## Scope

The library and the `omnidoc` CLI in this repository. Vendored third-party code (the
JBIG2 decoder under `pkg/internal/filter/jbig2`) is in scope when reachable from a
parsed document.
