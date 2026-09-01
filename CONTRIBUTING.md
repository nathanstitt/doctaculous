# Contributing

Thanks for considering it. This file covers what is specific to this repository; if
something here contradicts what you would normally do in a Go project, this file wins.

## Before you start

Read [docs/SCOPE.md](docs/SCOPE.md) — it records what is deliberately out of scope and
why, so you do not spend a weekend on something that will be declined on principle.
[docs/BACKLOG.md](docs/BACKLOG.md) is the list of known-open work with a size estimate
and the reason each item was deferred; anything there is fair game.

For an architectural change, open an issue first. For a bug fix or a backlog item, a PR
is welcome directly.

## The two hard constraints

Both are non-negotiable, and both will fail review no matter how good the code is.

1. **Pure Go, no cgo.** The binary must cross-compile to every target with no C
   toolchain. This is the project's reason to exist.
2. **MIT-compatible licensing, and a very short dependency list.** See
   [docs/DEPENDENCIES.md](docs/DEPENDENCIES.md) for the approved list and the bar a new
   dependency has to clear. Adding one is a conversation, not a commit.

A related point that has bitten before: an approved dependency is vetted for *licensing
and purity*, not for its behaviour on hostile input. Three defects found during the
hardening pass were in dependencies. If you are adding a parser, assume its input is
attacker-controlled.

## What a change looks like here

- **One sub-project per branch, one PR off `main`.** Keep changes additive and
  byte-identical for callers you did not intend to touch.
- **Every feature lands with tests AND a visual entry.** Unit or golden tests for each
  part, plus a section in the `testdata/htmldoc/` showcase so the feature is exercised
  end to end and can be eyeballed.
- **Degrade honestly.** An unsupported case skips with a debug log and a test covering
  that behaviour. If a degradation genuinely cannot log — no logger on that path — say
  so in FEATURES.md rather than implying it does. Claiming something "degrades with a
  log" when it is silent is worse than admitting the gap.
- **Implement the fullest spec-compliant behaviour** when a feature has a fidelity
  choice. Do not ship a subset and leave the rest as a TODO if the full version is
  reachable.
- **Update FEATURES.md in the same PR.** It inventories what has shipped and is kept
  current; it is not a changelog.

## Testing

`docs/TESTING.md` is the real reference — it documents traps that have cost real time.
The two worth knowing before your first PR:

- **Assert on a specific colour, not on "something was painted."** A test that checks
  ink landed passes just as happily on the *wrong* ink, and a family of bugs survived a
  full suite exactly that way.
- **Compare PNGs by decoded pixels, not raw bytes.** Per-row PNG filters mean one
  changed pixel perturbs every byte after it; a 1/255 difference can look like 7% of
  the file. `-update` rewriting a golden does not mean it was stale.

Run before pushing:

```sh
gofmt -l .
go vet ./...
go test ./...
golangci-lint run ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

`govulncheck` gates CI. It is reachability-aware, so it fails only when something in
this module actually calls a vulnerable symbol. If it reports findings only in
`Standard library` packages, your local Go is probably behind — `go.mod`'s `go`
directive is a minimum, and stdlib advisories are fixed by the toolchain rather than by
anything in this repo.

CI runs the suite with `-race` on Linux, and without it on macOS and Windows. It also
caps `go test -p 2`: the race suite peaks around 9 GB on a standard runner, and the
uncapped version has been OOM-killed. Do not raise that without measuring first —
`/usr/bin/time -l go test -race -p 2 -count=1 ./...` gives the peak RSS.

If a golden image changes, **eyeball every changed PNG** and say in the PR why it
changed. An unexplained golden diff is a regression.

## Commits and PRs

Explain *why*, not what — the diff already says what. Design rationale lives in commit
and PR history, and it gets read: `git log` for the area you are extending is often the
fastest way to find out why something is the way it is.

Keep PR descriptions short and factual. If you measured something, give the number.
