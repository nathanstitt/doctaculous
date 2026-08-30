# Dependencies and licensing constraints

These are non-negotiable. A change that violates one is wrong regardless of what it buys.

- **Pure Go. No CGo, no native bindings, no WASM engines.** No PDFium / MuPDF / Poppler.
- **MIT licensed.** Every dependency must be MIT/BSD/Apache and pure Go. No GPL/AGPL.
- Approved deps: `golang.org/x/image/*` (BSD), `github.com/srwiley/rasterx` (BSD),
  `github.com/benoitkugler/textlayout` (font parsing, plus its pure-Go harfbuzz port for Arabic
  contextual shaping and `unicodedata` for bracket mirroring), `golang.org/x/net/html` (HTML parse),
  `golang.org/x/text` (BSD — `unicode/bidi`, a complete UAX#9 incl. bracket pairs; promoted from
  indirect when inline bidi reordering landed, no new module),
  `github.com/andybalholm/brotli` (MIT, pure-Go — WOFF2 Brotli decompression only),
  `github.com/beevik/etree` (BSD-2, pure-Go, zero deps — the raw-fidelity XML DOM the xlsx
  editor rewrites dirty parts through; prefixes/attr order/CDATA preserved, verified in source
  before adoption),
  `github.com/HugoSmits86/nativewebp` (MIT, pure-Go — WebP *encoding* only; its sole dependency is
  `golang.org/x/image`, already used, so it adds no transitive surface. Decoding stays on
  `x/image/webp`; this is used purely for the VP8L encoder Go otherwise lacks. It encodes lossless
  VP8L only — there is no pure-Go lossy VP8 encoder — which is why WebP output is a PNG-class
  target, not a JPEG-class one. Adopted rather than vendored because it is small, MIT, and
  dependency-clean; `pkg/webp.Encode` wraps it to check the writes it does not check itself, and
  the round-trip is verified pixel-exact against `x/image`'s independent decoder).
  Add new deps only if pure-Go + permissive; record the reason in the PR.
- Vendored (copied into the tree, not a `go get` dep): `github.com/xiaoqidun/jbig2` (Apache-2.0, pure
  Go — JBIG2 image decode) in `pkg/pdf/filter/jbig2/`, vendored because it is new/solo-authored (see
  that dir's README + NOTICE); its only dep is `golang.org/x/image` (already used). Excluded from
  golangci-lint via `.golangci.yml` as an unmodified third-party copy.
- **Concurrency-first.** Multi-page work fans out across goroutines (bounded worker pool sized to
  `GOMAXPROCS`). A parsed `*Document` is read-only after Open so it's shared without locks.
- Module path: `github.com/nathanstitt/omnidoc`.

