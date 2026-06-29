# HTML rendering — `OpenURL` + the HTTP `ResourceLoader` (sub-project 11)

**Date:** 2026-06-28
**Status:** Design approved; ready for implementation plan.
**Branch:** `feat/html-openurl` (off `feat/html-grid`, PR #14 tip; rebase onto `main` if the stack has merged).
**Spec references:** `net/http` and `net/url` (stdlib); the `data:` URL scheme (RFC 2397); HTTP Basic
authentication (RFC 7617). No external spec algorithm to encode — this is a network/IO frontend, not a
layout mode.

## Summary

Add a public `OpenURL(rawURL string, opts ...HTMLOption) (*Document, error)` and an HTTP-backed
`resource.ResourceLoader` (`HTTPLoader`) so a document's external references — `<link>` stylesheets,
`<img src>`, and `@font-face url(...)` — can be fetched over the network and resolved relative to the
document's URL. Today every loader is hermetic (in-memory `MapLoader`, on-disk `DirLoader`); nothing below
the public API touches the network. This slice adds the third loader the seam was always designed for
(`pkg/resource/loader.go`'s package doc: "The library will ship an HTTP-backed loader for the public URL
path in a later sub-project").

There is **no engine, layout, `render.Device`, PDF, or DOCX change.** The whole pipeline already routes
refs through `cfg.loader` (`BuildWithFonts` for `<link>`/`@font-face`; image decode for `<img>`), so once a
loader resolves URLs to bytes, remote resources "just work." **No new dependencies** (stdlib `net/http`,
`net/url`, `encoding/base64`).

This is sub-project 11 of the HTML-rendering roadmap. It is the smallest remaining slice and unblocks remote
`<img>`/`<link>`/`@font-face` in one stroke.

## Architecture / seam fit

Two touch points, both above the layout engine:

- **`pkg/resource`** — a new `HTTPLoader` type (a third `ResourceLoader` implementation alongside
  `MapLoader` and `DirLoader`), in a **new file `pkg/resource/http.go`** so `loader.go` stays focused. The
  `ResourceLoader` interface is **unchanged** (one method, `Load(ctx, ref) (data, contentType, err)`):
  `HTTPLoader` carries the document's base URL and resolves relative refs internally.
- **`pkg/doctaculous`** — `OpenURL` in `html_backend.go`, mirroring `OpenHTML(path)`: fetch the document
  bytes over HTTP, then delegate to the existing `OpenHTMLBytes(data, WithResourceLoader(httpLoader), …)`.

The seam map (from CLAUDE.md "Architecture"), unchanged:

`pkg/html` + `pkg/css` (parse/cascade) → `pkg/layout/cssbox` → `pkg/layout/css` (the layout engine) →
`pkg/layout/paint` → `render.Device`. `OpenURL`/`HTTPLoader` live entirely in `pkg/resource` (the loader) +
`pkg/doctaculous` (the entry + base-URL wiring). The `render.Device` seam, the PDF pipeline, the DOCX
pipeline, and the shared inline core are **untouched** — this slice only feeds bytes through the existing
`ResourceLoader` seam.

## Component 1 — `HTTPLoader` (`pkg/resource/http.go`)

```go
// HTTPLoader is a ResourceLoader that fetches refs over HTTP(S), resolving
// relative refs against a base (document) URL, and also decodes data: URIs
// inline. It is the loader the public URL path uses. It degrades a failed or
// disallowed fetch to ErrNotFound so a remote sub-resource behaves exactly like
// a missing local one (skipped stylesheet / placeholder image), never panicking.
type HTTPLoader struct {
    Base     *url.URL     // the document's URL; relative refs resolve against it. Required.
    Client   *http.Client // nil → a default client with a request timeout. Inject one for auth/proxy/mTLS or tests.
    MaxBytes int64        // response-body cap; <= 0 → defaultMaxBytes. Over-limit → ErrNotFound.
}

func (h HTTPLoader) Load(ctx context.Context, ref string) (data []byte, contentType string, err error)
```

`Load` algorithm:

1. **Resolve.** Parse `ref` with `url.Parse`. `u := h.Base.ResolveReference(parsed)`. (Resolving an
   already-absolute ref returns it unchanged; resolving a relative ref joins it against `Base` per
   RFC 3986, which `ResolveReference` implements.) A parse error → `ErrNotFound` (wrapped).
2. **`data:` scheme** (`u.Scheme == "data"`) → decode inline, **no network**:
   parse `data:[<mediatype>][;base64],<payload>`; `;base64` payload is `base64.StdEncoding`-decoded,
   otherwise the payload is percent-decoded (`url.PathUnescape`) text bytes. Return the decoded bytes and
   the `<mediatype>` (defaulting to `text/plain;charset=US-ASCII` per RFC 2397 when absent). A malformed
   `data:` URI → `ErrNotFound`.
3. **`http`/`https` scheme** → fetch:
   - **Auth (userinfo → header).** If `u.User != nil`, set `Authorization: Basic base64(user:password)` on
     the request and **strip userinfo from the request URL** (build the outbound URL with `u.User = nil`).
     This makes the header explicit and testable and keeps credentials out of the request line. (Note: Go's
     `http.Client` would auto-send Basic auth from URL userinfo; the implementer should **verify** this and
     still do it explicitly so behavior is predictable and the redaction below is guaranteed.)
   - **Request.** `http.NewRequestWithContext(ctx, http.MethodGet, outboundURL, nil)` — so the caller's
     context cancels/times out the fetch.
   - **Client.** Use `h.Client` if set, else a package default (`defaultHTTPClient()`: an `*http.Client`
     with `Timeout: defaultRequestTimeout` (~30s) as a backstop for a stalled connection even under a
     background context). Default redirect policy (follows up to 10 hops).
   - **Status.** Any response status outside `200–299` → `ErrNotFound` (wrapped, status in the message),
     after closing the body. This makes a 404/500 degrade exactly like a missing local ref.
   - **Body.** Read through `io.LimitReader(resp.Body, max+1)` where `max = effective MaxBytes`; if the read
     exceeds `max`, → `ErrNotFound` (+ a debug log via the loader's logger if one is wired; see "Logging"),
     so a hostile/huge resource can't exhaust memory.
   - **Content type.** From the response `Content-Type` header (verbatim; the consumer sniffs if it cares).
4. **Any other scheme** (`file:`, `ftp:`, empty after resolution, …) → `ErrNotFound` (degrade, no panic).

### Constants

- `defaultMaxBytes int64 = 32 << 20` (32 MiB).
- `defaultRequestTimeout = 30 * time.Second`.

These are package-level defaults; `MaxBytes` is overridable per loader, and a caller who wants a different
timeout/transport injects their own `Client`.

### Logging / credential redaction (hard requirement)

`HTTPLoader` must **never log credentials.** Wherever a URL appears in a log line or error message, it is
redacted via a small `redact(*url.URL) string` helper that **drops userinfo** (the only credential-bearing
component) while keeping scheme + host + path so the line is still useful for debugging. The query is kept
unless it is simpler to also drop it; the non-negotiable part is that `u.User` (the `user:password`) never
appears. Error values returned to callers likewise must not embed the userinfo — when building an
`ErrNotFound`-wrapped error for an authed URL, format the redacted form, not `u.String()`. (The loader does not own a
logger field today; if a log is needed for the size-limit/degradation case, thread the existing `logf`
pattern used elsewhere, or return a sufficiently descriptive `ErrNotFound`-wrapped error and let the
pipeline's existing debug log report the skip. Prefer the latter to avoid adding loader state — decide in
the plan.)

### Why `HTTPLoader` carries the base URL

The `ResourceLoader` interface takes a bare `ref` string. Resolution must happen somewhere; putting it in
the loader (mirroring `DirLoader{Base}`) keeps URL knowledge in one place and leaves the interface and every
ref call site (`BuildWithFonts`, image decode) untouched. The alternative — resolving refs to absolute URLs
higher up — would thread the base URL through the pipeline and spread URL handling across layers. Rejected.

### Why one loader handles both `http(s):` and `data:`

A real document interleaves remote refs and `data:` refs freely (`<img src="data:…">` next to
`<img src="https://…">`). Handling both behind the single `Load` entry means no new interface, no
`ChainLoader`, and no call-site changes — `data:` decoding is a small branch in the same method. A separate
`DataLoader` + composition was considered and rejected as speculative generality (nothing else needs a
chain yet).

## Component 2 — `OpenURL` (`pkg/doctaculous/html_backend.go`)

```go
// OpenURL fetches the HTML document at rawURL over HTTP(S), lays it out at the
// default viewport width into a single tall page, and returns a Document ready
// to rasterize. Relative <link>/<img>/@font-face refs resolve against rawURL and
// are fetched over HTTP through an HTTPLoader; data: refs are decoded inline.
// Options (viewport width, logger, a custom loader/font provider) may be passed.
func OpenURL(rawURL string, opts ...HTMLOption) (*Document, error)
```

Algorithm:

1. Parse `rawURL` with `url.Parse`; a parse error or a non-`http(s)` scheme → a wrapped error
   (`doctaculous: open url %q: …`). (The *document* URL failing is a hard error, unlike a sub-ref.)
2. `loader := resource.HTTPLoader{Base: u}`.
3. Fetch the document bytes: `data, _, err := loader.Load(ctx, "")` — an empty ref resolves to `Base`
   itself (`Base.ResolveReference(url.Parse(""))` returns `Base`; the implementer should **verify** this
   edge in `net/url` and, if it does not hold, fetch `Base` directly instead), reusing the loader's
   fetch/limit/auth path for the document too (so an authed document URL works, and the size cap applies to
   the document). A fetch error → wrapped error.
4. `return OpenHTMLBytes(data, append([]HTMLOption{WithResourceLoader(loader)}, opts...)...)`.

**No `SystemFontProvider`.** `OpenHTML` supplies a `DiskFontProvider` rooted at the file's directory so
`@font-face local()` can find OS-installed fonts on disk; a URL has no local font directory, so `OpenURL`
leaves the provider nil — `local()` simply won't match and the next `src` is tried (correct for a remote
document). A caller who wants one can pass `WithSystemFontProvider(...)` via `opts`.

The `ctx` used for the document fetch is `context.Background()` (matching `htmlDocument`, which uses a
background context today); a caller-controlled context (`OpenURLContext`) is **out of scope** for this slice
(see "Out of scope").

## Error handling / degradation

- **Document fetch failure** (invalid URL, unsupported scheme, 404/timeout/cancelled-ctx, over-size) →
  `OpenURL` returns a wrapped error. The document itself is mandatory; failing to obtain it is a hard error.
- **Sub-ref failure** (a remote `<link>`/`<img>`/`@font-face` that 404s, times out, is over-size, has an
  unsupported scheme, or is a malformed `data:`) → `HTTPLoader.Load` returns `ErrNotFound` (wrapped), and
  the **existing** pipeline degrades exactly as for a missing local ref: a `<link>` is skipped, an `<img>`
  becomes a sized placeholder, an `@font-face` source falls to the next/base-14 — each with the existing
  debug log, no panic. This slice adds no new degradation path; it only feeds `ErrNotFound` into the one
  that already exists. Recovery remains at the page boundary.

## Testing (hermetic — NO real network)

The project's hard rule: tests are offline. The HTTP loader is **real** but the bytes are **local**, served
by an `httptest.Server` (loopback) or a fake `http.RoundTripper`/`http.Client` over an in-memory map. **No
test may make a real outbound request.**

`pkg/resource` (`http_test.go`) — `HTTPLoader.Load` unit tests, asserting **actual values**:

- **Relative-ref resolution**: `Base = http://h/a/b/doc.html`, `ref = "../style.css"` → fetches
  `http://h/a/style.css` (assert the server received that path); `ref = "img/x.png"` → `http://h/a/b/img/x.png`.
- **Absolute ref pass-through**: `ref = "https://other/y.css"` fetches that URL unchanged.
- **`data:` URIs**: `data:text/css;base64,<b64>` → decoded CSS bytes + `text/css`; a plain (non-base64)
  `data:text/plain,hello%20world` → `hello world`; a malformed `data:` → `ErrNotFound`.
- **non-2xx → `ErrNotFound`**: a handler returning 404 and one returning 500 both yield `errors.Is(err,
  ErrNotFound)` and nil data.
- **ctx honored**: a cancelled context (and a context with an already-past deadline) → `Load` returns the
  ctx error (or a wrapped form) and does not hang; a handler that blocks past a short ctx deadline is
  cancelled.
- **size limit**: `MaxBytes = N`, a handler returning `N+1` bytes → `ErrNotFound`; returning exactly `N` →
  success.
- **redirect**: a handler 302-redirecting to a second path that serves the body → `Load` follows it and
  returns the body (default policy).
- **auth (userinfo → header)**: `Base`/`ref` with `user:pw@` → the server **receives**
  `Authorization: Basic base64("user:pw")` and the **request URL has no userinfo** (assert
  `r.URL.User == nil` and the raw request line carries no creds); a same-origin relative sub-ref inherits
  the base's creds (resolves with userinfo and sends the header).
- **redaction**: capture the loader's diagnostics (or the returned error string) for an authed URL that
  fails, and assert the userinfo substring (`user:pw`) does **not** appear.

`pkg/doctaculous` (`openurl_test.go` + golden/reftest) — `OpenURL` end-to-end against an `httptest.Server`:

- Serve an HTML document at `/index.html` that references a `<link>` CSS, an `<img>` (a tiny PNG), and an
  `@font-face url(...)` (a small WOFF/TTF), all by **relative** path. `OpenURL(server.URL + "/index.html")`
  renders a page; assert it via a **fragment-geometry assertion** (the styled box dimensions prove the CSS
  loaded; the image box proves the image decoded) and/or a **golden image** (`html-openurl-*`, controller-
  eyeballed). Reuse the existing remote-resource fixtures patterns from the web-font/image slices for the
  asset bytes.
- **404 sub-ref degrades**: the same document but the `<img>`/`<link>` path 404s → the page still renders
  (placeholder image / no stylesheet), no error, no panic.
- **document fetch fails**: `OpenURL` of a path the server 404s → a non-nil error.
- Optionally a **WPT-style reftest** (`openurl` vs a `-ref.html` built with the same boxes from a local
  `MapLoader`/inline data) if it adds correctness signal beyond the golden; otherwise the golden +
  geometry assertions suffice.

**Byte-identical guard:** this slice is purely additive. After implementation, `git status --short
pkg/doctaculous/testdata pkg/render/raster/testdata` must show **only NEW files** — no existing golden or
page changes. Run it as a dedicated checkpoint.

**Degradation tests** (every deferral degrades gracefully + is tested): the 404/timeout/cancelled-ctx/
over-size/unsupported-scheme/malformed-`data:` cases above, each asserting `ErrNotFound` (loader) or a
rendered-with-fallback page (end-to-end), no panic.

## Out of scope (deferred; each degrades gracefully)

These are explicitly **not** in this slice. Each already degrades (the document still renders; an unknown
ref is `ErrNotFound`):

- **`<base href>`** — the in-document base override; refs resolve against the document URL only.
- **Caching / content-addressed fetch** — the web-font follow-up notes the `FaceCache` is keyed
  `(family, style)` so one font file is fetched once *per style*; a shared content-addressed fetch cache is
  a follow-up now that HTTP lands (harmless with hermetic loaders; relevant for real remote fetches).
- **Cookies / non-Basic auth** — only URL-userinfo → Basic is supported; richer auth is a caller concern
  via an injected `Client` (a `RoundTripper` that attaches headers).
- **Caller-controlled context** (`OpenURLContext(ctx, …)`) — the document fetch uses a background context;
  per-call cancellation of the whole fetch+layout is a follow-up. (The loader already honors `ctx` on each
  fetch; this is only about the top-level entry's signature.)
- **Custom redirect policy / redirect to a different scheme hardening**, proxy/SSRF allow-listing, retries,
  conditional requests (ETag/If-Modified-Since), `Accept`/`User-Agent` negotiation — the default client
  behavior is taken as-is.
- **Non-`http(s)`/`data:` schemes** (`file:`, `ftp:`) — `ErrNotFound`.

## No new dependencies

`net/http`, `net/url`, `io`, `encoding/base64`, `time`, `net/http/httptest` (test-only) are all stdlib. The
project prefers stdlib and a new dep needs a PR-recorded reason; none is needed here.

## CLAUDE.md update (when the PR lands)

Move sub-project 11 from §6 "remaining slices" into a new "Done" bullet (an `OpenURL`/`HTTPLoader` bullet
under the HTML-rendering done section), update the §6 done-slices parenthetical, and add the fidelity
follow-ups note (the deferred items above: `<base href>`, content-addressed fetch cache, caller-controlled
context, cookies/richer auth). Keep Done/TODO the honest source of truth.
