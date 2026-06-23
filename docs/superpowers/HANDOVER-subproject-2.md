# Handover — Sub-project 2: HTML frontend + box generation

**Status:** Not started. Brainstorm was begun and interrupted; no design doc or code yet.
**Branch:** `feat/html-box-generation` (already created, branched off `feat/css-parse-cascade`).
**Next action:** Resume the `superpowers:brainstorming` skill for sub-project 2, then writing-plans, then subagent-driven execution — the same flow used for sub-project 1.

---

## Where we are

- **Sub-project 1 (CSS parse + cascade) is DONE** and in **PR #2**
  (`feat/css-parse-cascade` → `main`): https://github.com/nathanstitt/doctaculous/pull/2.
  It is the package `pkg/css` (35 tests, race-clean, lint-clean, no new deps).
- This branch (`feat/html-box-generation`) is stacked **on top of** `feat/css-parse-cascade`,
  not `main` — so `pkg/css` is available here. When PR #2 merges, rebase this branch onto the
  updated `main` (or keep it stacked). Sub-project 2's own PR base will need attention then.

## What sub-project 2 must build (from the roadmap spec §5 row 2)

Design source of truth: `docs/superpowers/specs/2026-06-23-html-rendering-design.md`.

1. **`pkg/html`** — wrap `golang.org/x/net/html` into a DOM that implements the `pkg/css` `Node`
   interface; collect `<style>`, `<link>`, and inline `style=""`.
2. **`pkg/layout/cssbox`** — the recursive box tree (the new neutral model that DOCX will also
   converge onto later, per the spec's Option-C plan).
3. **Box generation** — walk the DOM, run each node through the `pkg/css` `Resolver`, emit the
   `cssbox` tree, including **anonymous boxes** (block-in-inline / inline-in-block fixups).
4. **`pkg/resource`** — the `ResourceLoader` seam + a **hermetic in-memory/testdata loader**.
   **No HTTP yet** (HTTP fetching is sub-project 7). Tests must stay network-free.

**Still produces NO pixels.** Layout + paint is sub-project 3. #2's deliverable is a correct box
tree, verified by structural assertions (not golden images).

## The exact seam #2 builds on (verified against current `pkg/css`)

`pkg/css` Node interface that `pkg/html`'s DOM must satisfy:
```go
type Node interface {
    Tag() string                  // lowercased element name; "" for non-elements
    ID() string                   // id attr, or ""
    Classes() []string            // class list, already whitespace-split
    Parent() Node                 // nil at root
    Attr(key string) (string, bool)
}
```

Cascade entry points #2 consumes (`pkg/css/cascade.go`):
- `NewResolver(sheet Stylesheet, logf func(string,...any)) *Resolver`
- `(*Resolver).Compute(n Node, parentStyle ComputedStyle) ComputedStyle`
  — caller computes the parent first and passes its `ComputedStyle` down as the inheritance base
  (this is the tree-walk threading model box generation will drive).
- `Parse(src string) Stylesheet` — parses a stylesheet string.
- `ComputedStyle` — the resolved per-element style (display, color/background, font-*, line-height,
  text-align, margin/padding/border, width/height).

## ⚠️ Known API gap to resolve early in #2

`initialStyle()` (the CSS initial-values base for the **root** element's `parentStyle`) is
**unexported** in `pkg/css`. Box generation lives in a different package and needs a root base to
start the tree walk. Decide in the #2 design how to get one, e.g.:
- export it (`InitialStyle()`), or
- have `Compute` accept a sentinel / a nil-parent convenience, or
- expose a `RootStyle()` / document-default helper.
This is a real day-one blocker for box generation; pick the approach during brainstorming.

## First brainstorming questions that were queued

1. **DOM → Node adapter & tree ownership**: wrap `x/net/html`'s `*html.Node` live (thin adapter
   implementing `Node`), or convert to a parallel owned tree? Affects box-gen traversal, the
   `Classes()`/`Attr()` cost, and how `<style>`/`<link>` collection works. (Live-wrapper is the
   likely recommendation — less copying, `x/net/html`'s tree is already read-only after parse.)
2. **cssbox model shape**: what's the minimal `Box` node for #2 (block/inline/anonymous + the
   `ComputedStyle` + children + text runs)? Keep it the contract that #3's layout and the eventual
   DOCX convergence depend on — design the vocabulary deliberately.
3. **Anonymous box rules**: how far to go now (block-in-inline splitting, inline-in-block wrapping)
   vs. defer — these are correctness-critical for #3.
4. **ResourceLoader interface**: confirm the `Load(ctx, url) (data, contentType, err)` shape from
   the roadmap spec §7.1; build the hermetic test loader; stub external `<link>` resolution.

## Dependencies note

`golang.org/x/net/html` is BSD, pure-Go, **already in the module cache** (verified during the
HTML-rendering brainstorm). It's the one new dependency #2 adds; record the reason in the PR per
CLAUDE.md.

## Process reminder (what worked for #1)

Brainstorm → spec (`docs/superpowers/specs/`) → plan (`docs/superpowers/plans/`) → subagent-driven
execution (implement → spec-review → code-quality-review → fix, per task) → holistic final review.
Two notes from #1:
- The command sandbox blocks the Go build cache (`operation not permitted` on
  `~/Library/Caches/go-build`) and TLS to GitHub/proxy — rerun those commands with the sandbox
  disabled. `golangci-lint run` from the repo root reports pre-existing typecheck errors in the
  **untracked** `agent/skills/.../examples/` stray dir; ignore them (not part of the module).
- Propagate any review fixes back into the plan doc so it stays authoritative.
