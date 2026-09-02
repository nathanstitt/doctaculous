# Go practices


- Target the current stable Go; `go.mod` states the minimum and CI tests the latest patch of it.
- `gofmt`/`goimports` clean. `go vet ./...` and `golangci-lint run` must pass in CI and locally.
- Errors: wrap with `fmt.Errorf("...: %w", err)`; define sentinel/typed errors for conditions
  callers branch on (e.g. `ErrNoStructure`, `ErrEncrypted`, `ErrSheetNotFound`). Never `panic` on malformed
  input — return an error. Recover at the page boundary so one bad page can't kill a batch.
- Accept interfaces, return concrete types. Public API takes a path, a byte slice, an `io.Reader`
  (with a context), or `io.ReaderAt`+size — never a type from `pkg/internal`. An exported
  signature must be nameable by an importer: an internal interface leaking through it compiles
  but cannot be referred to, which is why `FontProvider`/`SystemFontProvider` live in `pkg/omnidoc`.
- All exported identifiers have doc comments. Keep packages cohesive; no cyclic deps between layers.
- Context-aware: long/parallel operations take `context.Context` and honor cancellation.
- No global mutable state. Pass dependencies explicitly.
- Prefer the standard library; reach for a dep only when it removes real, risky work.

