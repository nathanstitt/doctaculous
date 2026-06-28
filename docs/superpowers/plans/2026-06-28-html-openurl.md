# OpenURL + HTTP ResourceLoader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `OpenURL(rawURL, opts...)` and an HTTP-backed `resource.HTTPLoader` so HTML documents and their `<link>`/`<img>`/`@font-face` refs can be fetched over the network (with `data:` URIs decoded inline), resolved relative to the document URL.

**Architecture:** A new `HTTPLoader` (third `ResourceLoader` alongside `MapLoader`/`DirLoader`) in `pkg/resource/http.go` carries the document's base URL, resolves refs via `*url.URL.ResolveReference`, fetches `http(s):` (ctx-aware, non-2xx→`ErrNotFound`, size-capped, default-timeout, default redirects, URL-userinfo→Basic auth with credentials redacted from logs), and decodes `data:` URIs inline. `OpenURL` in `pkg/doctaculous/html_backend.go` fetches the document bytes through that loader, then delegates to the existing `OpenHTMLBytes(data, WithResourceLoader(loader), …)`. No engine, layout, `render.Device`, PDF, or DOCX change.

**Tech Stack:** Go stdlib only — `net/http`, `net/url`, `io`, `encoding/base64`, `time`; `net/http/httptest` for hermetic tests. **No new dependency.**

**Spec:** `docs/superpowers/specs/2026-06-28-html-openurl-design.md`.

---

## Process rules for the implementer (carried from sub-projects 1–10 — read before starting)

- **Branch:** you are on `feat/html-openurl` (off `feat/html-grid`). Do **NOT** checkout/stash/switch branches. Do **NOT** commit unless a step says to. **Scope every `git add` to the exact files named** — NEVER `git add -A`/`git add .` (the repo has unrelated `html-to-pdf-writer` docs that may be dirty; do not touch them).
- **Sandbox blocks the Go build cache + TLS:** run every `go` / `gofmt` / `golangci-lint` command with `dangerouslyDisableSandbox: true`. A sandboxed `go`/lint command fails with cache/permission/"no go files to analyze" errors that are NOT real failures; re-run disabled. The HTTP tests use `httptest` (loopback) so they need no outbound network — but the build cache still needs the sandbox disabled.
- **Never add a test that makes a real outbound network request.** All HTTP tests run against an `httptest.Server` (loopback) or a fake `http.RoundTripper`.
- **Editor diagnostics lag** — after you add a field/file you may see stale "undefined"/"unused"/"redeclared" errors and phantom `zz_*`/`*probe*` files. Trust `go build`/`go test`/`golangci-lint`/`find . -name 'zz_*'`, not the editor panel. Delete any scratch you create before finishing.
- **Lint:** run `golangci-lint run ./pkg/resource/... ./pkg/doctaculous/...` (specific packages, not the repo root). **NO `//nolint`.** The repo declines "modernize" hints — keep explicit `if x < y { x = y }` clamps and indexed `for i := 0; i < n; i++` loops; do not introduce `slices.*`/`maps.*`/`min()`/`max()` builtins. `golangci-lint` here does **NOT** run gofmt — run `gofmt -l` on changed files separately and fix any it lists. errcheck requires `_ = x.Close()` for ignored Close errors. The `unused` linter is enforced: a field/const/function you add must be *read* by code in the same PR (the consuming step adds it).
- **Verify against stdlib, don't assume.** Two spec items are explicitly "verify": (a) whether `http.Client` auto-sends Basic auth from URL userinfo (we set it explicitly regardless), and (b) that `Base.ResolveReference(url.Parse(""))` returns `Base`. Confirm with a tiny check or the unit tests, not by assumption.

---

## File Structure

- **Create `pkg/resource/http.go`** — `HTTPLoader` + its `Load` method, the `data:` decoder, the default client/constants, and the `redact` helper. One responsibility: resolve a ref (over HTTP or as a `data:` URI) to bytes.
- **Create `pkg/resource/http_test.go`** — `HTTPLoader.Load` unit tests against `httptest.Server`.
- **Modify `pkg/doctaculous/html_backend.go`** — add `OpenURL`.
- **Create `pkg/doctaculous/openurl_test.go`** — `OpenURL` end-to-end tests against `httptest.Server`, including a byte-equality proof vs. the `MapLoader` render.
- **Modify `CLAUDE.md`** (final task) — move sub-project 11 to "Done", update the parenthetical, add fidelity follow-ups.

No new golden image or WPT reftest is added: this slice changes **where bytes come from**, not **what pixels result** (the loader feeds the same bytes the existing `MapLoader`-based goldens already cover). The strongest honest proof is a **byte-equality assertion** that a document rendered via `HTTPLoader` rasterizes identically to the same document via `MapLoader` (Task 7). This also keeps the byte-identical guard trivially clean (only NEW test files appear under `testdata`, and in fact none are needed). This is a deliberate, spec-permitted choice (the spec's testing section offers "a golden OR a fragment-geometry assertion").

---

### Task 1: `HTTPLoader` skeleton + `data:` URI decoding

The smallest first slice: the type, the scheme dispatch, and the network-free `data:` path. HTTP comes in Task 2 so each is tested in isolation.

**Files:**
- Create: `pkg/resource/http.go`
- Create: `pkg/resource/http_test.go`

- [ ] **Step 1: Write the failing tests (data: only)**

Create `pkg/resource/http_test.go`:

```go
package resource

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

// mustURL parses a URL for tests, failing on error.
func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return u
}

func TestHTTPLoaderDataURIBase64(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// "p{color:red}" base64 = cHtjb2xvcjpyZWR9
	data, ct, err := l.Load(context.Background(), "data:text/css;base64,cHtjb2xvcjpyZWR9")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "p{color:red}" {
		t.Errorf("data = %q, want p{color:red}", data)
	}
	if ct != "text/css" {
		t.Errorf("contentType = %q, want text/css", ct)
	}
}

func TestHTTPLoaderDataURIPlain(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	data, ct, err := l.Load(context.Background(), "data:text/plain,hello%20world")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("data = %q, want 'hello world'", data)
	}
	if ct != "text/plain" {
		t.Errorf("contentType = %q, want text/plain", ct)
	}
}

func TestHTTPLoaderDataURINoMediaType(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// "data:,A%20brief%20note" — no media type; default is text/plain;charset=US-ASCII.
	data, ct, err := l.Load(context.Background(), "data:,A%20brief%20note")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "A brief note" {
		t.Errorf("data = %q", data)
	}
	if ct != "text/plain;charset=US-ASCII" {
		t.Errorf("contentType = %q, want default text/plain;charset=US-ASCII", ct)
	}
}

func TestHTTPLoaderDataURIMalformed(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	// No comma → not a valid data: URI.
	if _, _, err := l.Load(context.Background(), "data:text/plain;base64"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	// Bad base64 payload.
	if _, _, err := l.Load(context.Background(), "data:text/css;base64,@@@notb64@@@"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad base64 err = %v, want ErrNotFound", err)
	}
}

func TestHTTPLoaderUnsupportedScheme(t *testing.T) {
	l := HTTPLoader{Base: mustURL(t, "http://example.com/doc.html")}
	if _, _, err := l.Load(context.Background(), "ftp://host/file"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for ftp scheme", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/resource/ -run TestHTTPLoader -v`
Expected: FAIL — `undefined: HTTPLoader`.

- [ ] **Step 3: Implement the skeleton + data: path**

Create `pkg/resource/http.go`. The import block includes `net/http` from the start (the `HTTPLoader.Client` field is `*http.Client`) and the file ends with a temporary `fetch` stub + the real `redact` helper, so the package compiles now and Task 2 only replaces the stub body:

```go
package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// HTTPLoader is a ResourceLoader that fetches refs over HTTP(S), resolving
// relative refs against a base (document) URL, and also decodes data: URIs
// inline. It is the loader the public URL path (OpenURL) uses. It degrades a
// failed or disallowed fetch to ErrNotFound so a remote sub-resource behaves
// exactly like a missing local one (a skipped stylesheet / a placeholder image),
// never panicking. The zero value is not usable; Base is required.
type HTTPLoader struct {
	// Base is the document's URL; relative refs resolve against it. Required.
	Base *url.URL
	// Client is the HTTP client used for fetches. nil selects a default client
	// with a request timeout (defaultRequestTimeout). Inject a client to supply
	// auth via a RoundTripper, a proxy, mTLS, or a test transport.
	Client *http.Client
	// MaxBytes caps a fetched response body; <= 0 selects defaultMaxBytes. A
	// response exceeding the cap is treated as absent (ErrNotFound).
	MaxBytes int64
}

var _ ResourceLoader = HTTPLoader{}

// Load implements ResourceLoader. It resolves ref against Base and then either
// decodes a data: URI inline or fetches an http(s) URL. Any other scheme, or any
// fetch failure, returns ErrNotFound (wrapped) so callers degrade gracefully.
func (h HTTPLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
	}
	u := h.Base.ResolveReference(parsed)
	switch u.Scheme {
	case "data":
		return decodeDataURL(u)
	case "http", "https":
		return h.fetch(ctx, u)
	default:
		return nil, "", fmt.Errorf("%q: unsupported scheme %q: %w", ref, u.Scheme, ErrNotFound)
	}
}

// decodeDataURL decodes a data: URI per RFC 2397:
// data:[<mediatype>][;base64],<payload>. A ;base64 payload is base64-decoded;
// otherwise the payload is percent-decoded text. A missing media type defaults to
// text/plain;charset=US-ASCII. A malformed URI returns ErrNotFound.
func decodeDataURL(u *url.URL) ([]byte, string, error) {
	// url.Parse puts everything after "data:" into u.Opaque (for data:text/...,)
	// or, for data:,... with no media type, the comma-led remainder may land in
	// u.Opaque too. Reconstruct the raw payload from Opaque.
	raw := u.Opaque
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return nil, "", fmt.Errorf("data URI: missing comma: %w", ErrNotFound)
	}
	meta, payload := raw[:comma], raw[comma+1:]
	isBase64 := false
	mediatype := meta
	if i := strings.LastIndex(meta, ";base64"); i >= 0 {
		isBase64 = true
		mediatype = meta[:i]
	}
	if mediatype == "" {
		mediatype = "text/plain;charset=US-ASCII"
	}
	var data []byte
	if isBase64 {
		d, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", fmt.Errorf("data URI base64: %w", ErrNotFound)
		}
		data = d
	} else {
		d, err := url.PathUnescape(payload)
		if err != nil {
			return nil, "", fmt.Errorf("data URI payload: %w", ErrNotFound)
		}
		data = []byte(d)
	}
	return data, mediatype, nil
}
```

Append the temporary `fetch` stub and the real `redact` helper at the end of the same file so the package builds (Task 2 replaces only the stub's body; `redact` is final):

```go
// fetch performs the HTTP GET for an http(s) URL. Fleshed out in the next task;
// the stub keeps the package compiling and every data:/scheme test meaningful.
// (Go does not flag unused function parameters, so the ctx parameter is fine.)
func (h HTTPLoader) fetch(ctx context.Context, u *url.URL) ([]byte, string, error) {
	return nil, "", fmt.Errorf("%s: %w", redact(u), ErrNotFound) // replaced in Task 2
}

// redact returns a URL string safe for logs/errors: scheme://host/path with any
// userinfo (the only credential-bearing component) dropped.
func redact(u *url.URL) string {
	r := *u
	r.User = nil
	return r.String()
}
```

- [ ] **Step 4: Run the data: tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/resource/ -run TestHTTPLoader -v`
Expected: PASS for the data:/scheme tests.

Also run `gofmt -l pkg/resource/http.go pkg/resource/http_test.go` (expect no output) and `go build ./pkg/resource/` (expect success).

- [ ] **Step 5: Commit**

```bash
git add pkg/resource/http.go pkg/resource/http_test.go
git commit -m "resource: HTTPLoader skeleton + data: URI decoding"
```

---

### Task 2: HTTP fetch path (ctx, non-2xx, size cap, default client, redirects)

Flesh out `fetch`: the ctx-aware GET, status handling, the `io.LimitReader` cap, the default client with a timeout, and (implicitly) default redirect following.

**Files:**
- Modify: `pkg/resource/http.go` (replace the `fetch` stub; add constants + default client)
- Modify: `pkg/resource/http_test.go` (add HTTP tests with `httptest`)

- [ ] **Step 1: Write the failing HTTP tests**

Append to `pkg/resource/http_test.go` (add imports `net/http`, `net/http/httptest`, `strings`, `time` to the existing import block):

```go
func TestHTTPLoaderFetchesRelative(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("a{}"))
	}))
	defer srv.Close()
	// Base is .../a/b/doc.html; "../style.css" must resolve to .../a/style.css.
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/a/b/doc.html")}
	data, ct, err := l.Load(context.Background(), "../style.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != "/a/style.css" {
		t.Errorf("server got path %q, want /a/style.css", gotPath)
	}
	if string(data) != "a{}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestHTTPLoaderFetchesAbsoluteRef(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	// Base is one server; an absolute ref to the SAME server passes through unchanged.
	l := HTTPLoader{Base: mustURL(t, "http://other.invalid/doc.html")}
	if _, _, err := l.Load(context.Background(), srv.URL+"/abs.png"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if gotPath != "/abs.png" {
		t.Errorf("server got path %q, want /abs.png", gotPath)
	}
}

func TestHTTPLoaderNon2xxIsNotFound(t *testing.T) {
	for _, code := range []int{404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
		_, _, err := l.Load(context.Background(), "x.css")
		srv.Close()
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("status %d: err = %v, want ErrNotFound", code, err)
		}
	}
}

func TestHTTPLoaderSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes
	}))
	defer srv.Close()
	// Cap below the body size → ErrNotFound.
	lSmall := HTTPLoader{Base: mustURL(t, srv.URL+"/"), MaxBytes: 5}
	if _, _, err := lSmall.Load(context.Background(), "big"); !errors.Is(err, ErrNotFound) {
		t.Errorf("over-limit err = %v, want ErrNotFound", err)
	}
	// Cap at exactly the body size → success.
	lExact := HTTPLoader{Base: mustURL(t, srv.URL+"/"), MaxBytes: 10}
	data, _, err := lExact.Load(context.Background(), "big")
	if err != nil {
		t.Fatalf("at-limit Load: %v", err)
	}
	if string(data) != "0123456789" {
		t.Errorf("data = %q", data)
	}
}

func TestHTTPLoaderFollowsRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("arrived"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
	data, _, err := l.Load(context.Background(), "start")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "arrived" {
		t.Errorf("data = %q, want 'arrived' (redirect not followed)", data)
	}
}

func TestHTTPLoaderHonorsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until the client cancels
	}))
	defer srv.Close()
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/")}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := l.Load(ctx, "slow")
	if err == nil {
		t.Fatal("Load returned nil error on a cancelled context, want non-nil")
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run (sandbox disabled): `go test ./pkg/resource/ -run TestHTTPLoaderFetches -v` (and the others)
Expected: FAIL — the `fetch` stub returns `ErrNotFound` for everything (relative/absolute/redirect/size tests fail; the non-2xx test may accidentally pass against the stub — that's fine, it locks in real behavior next).

- [ ] **Step 3: Implement the real `fetch` + constants + default client**

In `pkg/resource/http.go`, add the imports `io` and `time` to the import block, add the constants, the default client, and replace the `fetch` stub:

```go
const (
	// defaultMaxBytes caps a fetched response body (32 MiB) so a hostile or
	// runaway resource cannot exhaust memory. Override per loader via MaxBytes.
	defaultMaxBytes int64 = 32 << 20
	// defaultRequestTimeout bounds a single fetch when the caller's context has
	// no deadline, so a stalled connection cannot hang forever.
	defaultRequestTimeout = 30 * time.Second
)

// defaultHTTPClient returns the client used when HTTPLoader.Client is nil: a
// plain client with a request timeout. It follows redirects with the stdlib
// default policy (up to 10 hops).
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultRequestTimeout}
}

// fetch performs the HTTP GET for an http(s) URL u, honoring ctx, capping the
// body, and treating any non-2xx status as absent (ErrNotFound). u may carry
// userinfo; auth handling is added in the auth task.
func (h HTTPLoader) fetch(ctx context.Context, u *url.URL) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", redact(u), ErrNotFound)
	}
	client := h.Client
	if client == nil {
		client = defaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", redact(u), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("fetch %s: status %d: %w", redact(u), resp.StatusCode, ErrNotFound)
	}
	max := h.MaxBytes
	if max <= 0 {
		max = defaultMaxBytes
	}
	// Read up to max+1 so an over-limit body is detectable.
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", redact(u), err)
	}
	if int64(len(data)) > max {
		return nil, "", fmt.Errorf("%s: response exceeds %d bytes: %w", redact(u), max, ErrNotFound)
	}
	return data, resp.Header.Get("Content-Type"), nil
}
```

NOTE on the fetch error path: a transport error (e.g. context cancel) is returned wrapped with the underlying `err` (so `errors.Is(err, context.Canceled)`/`context.DeadlineExceeded` holds), NOT remapped to `ErrNotFound`. Both the cancel test (wants non-nil) and the pipeline (treats any non-nil loader error as "skip this ref") are satisfied. Only an explicit absent/over-limit/bad-status/bad-scheme is `ErrNotFound`.

- [ ] **Step 4: Run the tests to verify they pass**

Run (sandbox disabled): `go test ./pkg/resource/ -v`
Expected: PASS (all HTTPLoader + the existing MapLoader/DirLoader tests).

Run `gofmt -l pkg/resource/http.go pkg/resource/http_test.go` (no output) and `go vet ./pkg/resource/`.

- [ ] **Step 5: Commit**

```bash
git add pkg/resource/http.go pkg/resource/http_test.go
git commit -m "resource: HTTPLoader HTTP fetch (ctx, non-2xx, size cap, redirects)"
```

---

### Task 3: URL-userinfo → Basic auth (with redaction)

Transform `http://user:pw@host/...` into an `Authorization: Basic …` header, strip userinfo from the outbound request URL, and ensure credentials never appear in logs/errors (already handled by `redact`, which this task tests).

**Files:**
- Modify: `pkg/resource/http.go` (the `fetch` method: set auth, strip userinfo)
- Modify: `pkg/resource/http_test.go` (auth + redaction tests)

- [ ] **Step 1: Write the failing auth tests**

Append to `pkg/resource/http_test.go`:

```go
func TestHTTPLoaderBasicAuthFromUserinfo(t *testing.T) {
	var gotAuth string
	var gotRawURLHasCreds bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// The request line / URL the server sees must not carry userinfo.
		gotRawURLHasCreds = strings.Contains(r.RequestURI, "user:") || (r.URL.User != nil)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	// Inject credentials into the base URL's host. srv.URL is http://127.0.0.1:PORT.
	base := mustURL(t, srv.URL+"/doc.html")
	base.User = url.UserPassword("user", "pw")
	l := HTTPLoader{Base: base}
	if _, _, err := l.Load(context.Background(), "asset.css"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// "user:pw" base64 = dXNlcjpwdw==
	if gotAuth != "Basic dXNlcjpwdw==" {
		t.Errorf("Authorization = %q, want Basic dXNlcjpwdw==", gotAuth)
	}
	if gotRawURLHasCreds {
		t.Error("request URL carried userinfo; it must be stripped")
	}
}

func TestRedactDropsUserinfo(t *testing.T) {
	u := mustURL(t, "https://user:secret@host.example/path?q=1")
	got := redact(u)
	if strings.Contains(got, "user") || strings.Contains(got, "secret") {
		t.Errorf("redact() = %q, leaks credentials", got)
	}
	if !strings.Contains(got, "host.example") || !strings.Contains(got, "/path") {
		t.Errorf("redact() = %q, dropped host/path it should keep", got)
	}
}

func TestHTTPLoaderAuthErrorRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	base := mustURL(t, srv.URL+"/doc.html")
	base.User = url.UserPassword("user", "topsecret")
	l := HTTPLoader{Base: base}
	_, _, err := l.Load(context.Background(), "missing.css")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "topsecret") || strings.Contains(err.Error(), "user:") {
		t.Errorf("error leaks credentials: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run (sandbox disabled): `go test ./pkg/resource/ -run 'TestHTTPLoaderBasicAuth|TestRedact|TestHTTPLoaderAuthError' -v`
Expected: `TestHTTPLoaderBasicAuthFromUserinfo` FAILS (no auth header set yet); `TestRedact*`/`AuthError` likely PASS already (redact exists). The auth test is the one driving the change.

- [ ] **Step 3: Implement auth + userinfo stripping in `fetch`**

Replace the top of `fetch` (the request construction) so it extracts userinfo, builds a userinfo-free outbound URL, and sets the header:

```go
func (h HTTPLoader) fetch(ctx context.Context, u *url.URL) ([]byte, string, error) {
	// Extract URL userinfo into an explicit Authorization header and strip it
	// from the outbound URL, so credentials are in the header (testable, and
	// never echoed in the request line) rather than in the URL.
	outbound := *u
	var user, pass string
	var haveAuth bool
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
		haveAuth = true
		outbound.User = nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, outbound.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", redact(u), ErrNotFound)
	}
	if haveAuth {
		req.SetBasicAuth(user, pass)
	}
	// ... (rest unchanged: client selection, Do, status, size cap, return)
}
```

(Use `req.SetBasicAuth`, which sets `Authorization: Basic base64(user:pass)` — verify it produces exactly that. Keep everything below the request construction as in Task 2.)

- [ ] **Step 4: Run to verify they pass**

Run (sandbox disabled): `go test ./pkg/resource/ -v`
Expected: PASS (all).

`gofmt -l` the two files; `go vet ./pkg/resource/`.

- [ ] **Step 5: Commit**

```bash
git add pkg/resource/http.go pkg/resource/http_test.go
git commit -m "resource: HTTPLoader URL-userinfo -> Basic auth, credentials redacted"
```

---

### Task 4: `OpenURL` entry point

Add the public `OpenURL` to `pkg/doctaculous`, mirroring `OpenHTML`.

**Files:**
- Modify: `pkg/doctaculous/html_backend.go`
- Create: `pkg/doctaculous/openurl_test.go`

- [ ] **Step 1: Write the failing end-to-end test (happy path)**

Create `pkg/doctaculous/openurl_test.go`:

```go
package doctaculous

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A document served over (loopback) HTTP with a relative <link> stylesheet and a
// relative <img> renders without error and produces a single-page Document. The
// styled box proves the CSS loaded; the image proves the <img> decoded. This is
// the OpenURL smoke test: it proves the HTTP loader is wired through the pipeline.
func TestOpenURLRendersRemoteResources(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
			<link rel="stylesheet" href="style.css">
			</head><body><div class="card">Hi</div><img src="quad.png"></body></html>`))
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(`body{margin:0}.card{width:120px;height:40px;background:#cce5ff}`))
	})
	mux.HandleFunc("/quad.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(quadPNG(40)) // reuse the golden-test helper
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenURL returned nil document")
	}
	if doc.PageCount() != 1 {
		t.Errorf("PageCount = %d, want 1", doc.PageCount())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenURL -v`
Expected: FAIL — `undefined: OpenURL`.

- [ ] **Step 3: Implement `OpenURL`**

In `pkg/doctaculous/html_backend.go`, add the `net/url` import and the function (place it next to `OpenHTML`):

```go
// OpenURL fetches the HTML document at rawURL over HTTP(S), lays it out at the
// default viewport width into a single tall page, and returns a Document ready to
// rasterize. Relative <link>/<img>/@font-face refs resolve against rawURL and are
// fetched over HTTP (data: refs are decoded inline) through an HTTPLoader rooted at
// rawURL. Options (e.g. WithViewportWidth, WithLogf, WithSystemFontProvider) may be
// supplied and take effect after the loader is set. Unlike OpenHTML, no system font
// provider is configured by default (a URL has no local font directory), so
// @font-face local() sources do not match unless one is supplied.
func OpenURL(rawURL string, opts ...HTMLOption) (*Document, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("doctaculous: open url %q: unsupported scheme %q", rawURL, u.Scheme)
	}
	loader := resource.HTTPLoader{Base: u}
	data, _, err := loader.Load(context.Background(), "")
	if err != nil {
		return nil, fmt.Errorf("doctaculous: fetch url %q: %w", rawURL, err)
	}
	allOpts := append([]HTMLOption{WithResourceLoader(loader)}, opts...)
	return OpenHTMLBytes(data, allOpts...)
}
```

NOTE: verify `loader.Load(ctx, "")` fetches the base document. `url.Parse("")` yields an empty-relative URL; `u.ResolveReference(that)` returns `u`. If a quick check shows otherwise, fetch the base directly instead: build a one-off request to `u` — but the unit test in Task 5 (an empty ref resolves to Base) will confirm this; prefer keeping the `Load(ctx, "")` form so the document fetch reuses the size cap + auth path.

- [ ] **Step 4: Run to verify it passes**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenURL -v`
Expected: PASS.

`gofmt -l pkg/doctaculous/html_backend.go pkg/doctaculous/openurl_test.go`; `go build ./pkg/doctaculous/`.

- [ ] **Step 5: Commit**

```bash
git add pkg/doctaculous/html_backend.go pkg/doctaculous/openurl_test.go
git commit -m "doctaculous: OpenURL — fetch + render an HTML document over HTTP"
```

---

### Task 5: `HTTPLoader.Load("")` resolves to Base (document-fetch contract)

Lock down the empty-ref → Base behavior `OpenURL` relies on, as an explicit unit test in `pkg/resource` (so the contract is owned where the loader lives, not only implied by the end-to-end test).

**Files:**
- Modify: `pkg/resource/http_test.go`

- [ ] **Step 1: Write the test**

Append to `pkg/resource/http_test.go`:

```go
func TestHTTPLoaderEmptyRefFetchesBase(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	l := HTTPLoader{Base: mustURL(t, srv.URL+"/dir/index.html")}
	data, _, err := l.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if gotPath != "/dir/index.html" {
		t.Errorf("empty ref fetched %q, want /dir/index.html (Base)", gotPath)
	}
	if string(data) != "<html></html>" {
		t.Errorf("data = %q", data)
	}
}
```

- [ ] **Step 2: Run**

Run (sandbox disabled): `go test ./pkg/resource/ -run TestHTTPLoaderEmptyRefFetchesBase -v`
Expected: PASS (the behavior already works; this test pins it). If it FAILS, `ResolveReference(parse(""))` does not return Base on this Go version — fix `Load` to special-case `ref == ""` as `u = h.Base`, and adjust `OpenURL` accordingly. (Do not delete the test.)

- [ ] **Step 3: Commit**

```bash
git add pkg/resource/http_test.go
git commit -m "resource: pin HTTPLoader empty-ref -> Base document-fetch contract"
```

---

### Task 6: Degradation — sub-ref 404 and document-fetch failure

Prove the two degradation contracts end to end: a 404 sub-resource still renders the page (no panic, no error), and a failed *document* fetch returns an error.

**Files:**
- Modify: `pkg/doctaculous/openurl_test.go`

- [ ] **Step 1: Write the tests**

Append to `pkg/doctaculous/openurl_test.go`:

```go
// A 404 on a sub-resource (the <img> and the <link>) must degrade: the page still
// renders (placeholder image / no stylesheet), no error, no panic.
func TestOpenURLSubResource404Degrades(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
			<link rel="stylesheet" href="missing.css">
			</head><body><p>text</p><img src="missing.png"></body></html>`))
	})
	// No handlers for missing.css / missing.png → 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL degraded to an error, want graceful render: %v", err)
	}
	if doc == nil || doc.PageCount() != 1 {
		t.Fatal("want a single-page document despite 404 sub-resources")
	}
}

// A failed DOCUMENT fetch (the URL itself 404s) is a hard error — the document is
// mandatory, unlike a sub-resource.
func TestOpenURLDocument404Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if _, err := OpenURL(srv.URL + "/nope.html"); err == nil {
		t.Fatal("OpenURL of a 404 document returned nil error, want an error")
	}
}

// An unsupported scheme is rejected before any fetch.
func TestOpenURLRejectsBadScheme(t *testing.T) {
	if _, err := OpenURL("file:///etc/passwd"); err == nil {
		t.Fatal("OpenURL of a file: URL returned nil error, want an error")
	}
}
```

- [ ] **Step 2: Run to verify they pass**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenURL -v`
Expected: PASS for all (the degradation already flows through the existing pipeline given `ErrNotFound`; these tests assert it).

- [ ] **Step 3: Commit**

```bash
git add pkg/doctaculous/openurl_test.go
git commit -m "doctaculous: OpenURL degradation tests (sub-ref 404, doc 404, bad scheme)"
```

---

### Task 7: Byte-equality proof — HTTPLoader render == MapLoader render

The strongest correctness proof for a byte-sourcing change: render the *same* document two ways — via `OpenURL`/`HTTPLoader` (loopback HTTP) and via `OpenHTMLBytes`/`MapLoader` (in-memory) — and assert the rasters are identical. This proves the HTTP path delivers exactly the bytes the existing (golden-covered) path does, with no new golden needed.

**Files:**
- Modify: `pkg/doctaculous/openurl_test.go`

- [ ] **Step 1: Write the byte-equality test**

Append to `pkg/doctaculous/openurl_test.go` (add imports `bytes`, `context`, `image`, `image/png` to its import block):

```go
// Rendering a document via the HTTP loader must produce the identical raster to
// rendering it via an in-memory MapLoader with the same bytes — proving the HTTP
// path is a transparent byte source (no pixel difference), so the existing goldens
// cover its output without a new golden.
func TestOpenURLMatchesMapLoaderRender(t *testing.T) {
	const doc = `<!DOCTYPE html><html><head>
		<link rel="stylesheet" href="s.css">
		</head><body><div class="card">Same</div><img src="q.png"></body></html>`
	const css = `body{margin:0}.card{width:120px;height:40px;background:#cce5ff;border:3px solid #036}`
	png40 := quadPNG(40)

	// (a) Render over loopback HTTP via OpenURL.
	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(doc))
	})
	mux.HandleFunc("/s.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(css))
	})
	mux.HandleFunc("/q.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png40)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	httpDoc, err := OpenURL(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}

	// (b) Render the same bytes via an in-memory MapLoader.
	loader := resource.MapLoader{
		"s.css": {Data: []byte(css), ContentType: "text/css"},
		"q.png": {Data: png40, ContentType: "image/png"},
	}
	memDoc, err := OpenHTMLBytes([]byte(doc), WithResourceLoader(loader))
	if err != nil {
		t.Fatalf("OpenHTMLBytes: %v", err)
	}

	httpPNG := rasterToPNG(t, httpDoc)
	memPNG := rasterToPNG(t, memDoc)
	if !bytes.Equal(httpPNG, memPNG) {
		t.Errorf("HTTP-loader render differs from MapLoader render (%d vs %d bytes)", len(httpPNG), len(memPNG))
	}
}

// rasterToPNG renders page 0 of doc at the golden DPI and returns its PNG bytes.
func rasterToPNG(t *testing.T, doc *Document) []byte {
	t.Helper()
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
	if err != nil {
		t.Fatalf("RasterizePage: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("image is %T, want *image.RGBA", img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
```

NOTE: `resource` must be imported in `openurl_test.go` for the `MapLoader`. `goldenDPI` and `RasterOptions`/`RasterizePage` already exist in the package (used by `html_golden_test.go`). Verify `goldenDPI` is the right symbol by reading `html_golden_test.go`; if the golden helpers live behind a different name, reuse those.

- [ ] **Step 2: Run to verify it passes**

Run (sandbox disabled): `go test ./pkg/doctaculous/ -run TestOpenURLMatchesMapLoaderRender -v`
Expected: PASS. If it FAILS with a small pixel difference, investigate — the HTTP path should be byte-identical; a difference means a content-type or decode discrepancy worth fixing (do not loosen to a tolerance without understanding why).

- [ ] **Step 3: Commit**

```bash
git add pkg/doctaculous/openurl_test.go
git commit -m "doctaculous: prove OpenURL render is byte-identical to the MapLoader render"
```

---

### Task 8: Full-suite gates + byte-identical guard

Run the whole test suite (race-clean), lint, and gofmt across the touched packages, and confirm no existing golden/page changed.

**Files:** none (verification only).

- [ ] **Step 1: Full package tests + race**

Run (sandbox disabled):
```
go test ./pkg/resource/... ./pkg/doctaculous/...
go test -race ./pkg/resource/... ./pkg/doctaculous/...
```
Expected: PASS / no races.

- [ ] **Step 2: Lint + gofmt**

Run (sandbox disabled):
```
golangci-lint run ./pkg/resource/... ./pkg/doctaculous/...
gofmt -l pkg/resource/ pkg/doctaculous/
```
Expected: lint clean; `gofmt -l` prints nothing. Fix anything reported (NO `//nolint`; no "modernize" rewrites).

- [ ] **Step 3: Byte-identical guard**

Run:
```
git status --short pkg/doctaculous/testdata pkg/render/raster/testdata
```
Expected: **empty** (this slice adds no goldens/reftests and changes none). If anything appears, investigate — an unintended golden change is a regression.

- [ ] **Step 4: Scratch check**

Run:
```
find . -name 'zz_*' -o -name '*probe*'
git status --short
```
Expected: no scratch files; working tree clean except the work you intend.

- [ ] **Step 5: Whole-repo build (catch cross-package breakage)**

Run (sandbox disabled): `go build ./...`
Expected: success.

(No commit — this is a gate. If a fix was needed, commit it scoped to the specific file with a descriptive message.)

---

### Task 9: Update CLAUDE.md (Done + TODO + follow-ups)

Move sub-project 11 to "Done", update the §6 done-slices parenthetical, and add the fidelity follow-ups. **This is documentation — read the current CLAUDE.md sections first and match their style; do not invent content beyond what shipped.**

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add the "Done" bullet**

Under the HTML-rendering "Done" bullets (after the Grid bullet), add an `OpenURL` bullet describing what shipped: `OpenURL(rawURL, opts...)` + `resource.HTTPLoader` (`http(s):` fetch with ctx/non-2xx→`ErrNotFound`/32 MiB cap/30 s default timeout/default redirects/URL-userinfo→Basic-auth-redacted, plus inline `data:` decoding); base-URL ref resolution via `ResolveReference`; no engine/`render.Device`/PDF/DOCX change; byte-identical (the HTTP path is a transparent byte source, proven by the MapLoader-equality test); stdlib only (no new dep). Note the degradation contract (sub-ref failure → skipped/placeholder; document fetch failure → hard error). Reference `docs/superpowers/specs/2026-06-28-html-openurl-design.md`.

- [ ] **Step 2: Update the §6 remaining-slices parenthetical**

In the TODO §6 "HTML rendering — remaining slices" paragraph, move `OpenURL` from the pending list into the done parenthetical (it currently reads "**`OpenURL` + the HTTP `ResourceLoader`** (network fetching…)" as the first pending item — remove it from pending and note it done), so the remaining pending slices are pagination and EPUB.

- [ ] **Step 3: Add the fidelity follow-ups**

Add an OpenURL/HTTP follow-ups note listing the deferred items from the spec's "Out of scope": `<base href>`; a content-addressed fetch cache (tie to the existing web-font `(family, style)` fetch-per-style note — now relevant since HTTP fetches are real); caller-controlled context (`OpenURLContext`); cookies / richer auth (beyond URL-userinfo Basic, via an injected `Client`); custom redirect/proxy/SSRF hardening; non-`http(s)`/`data:` schemes.

- [ ] **Step 4: Verify the doc reads consistently**

Re-read the changed CLAUDE.md sections; ensure Done/TODO no longer contradict (OpenURL is not in both pending and done). No build/test needed.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record OpenURL + HTTP ResourceLoader (sub-project 11) in CLAUDE.md"
```

---

## After all tasks

Hand back to the controller for:
- The **holistic final review** (adversarial probes that vary what the per-task tests held fixed: e.g. a `data:` image end-to-end, a redirect during the document fetch, a same-origin authed sub-ref, an oversize document, query-string refs, a relative ref with `..` past root; render a real page over httptest via the controller's Read tool; **delete every probe** and confirm `find` empty).
- Finishing the branch / opening the stacked PR (off `feat/html-grid`, scoped commits only; PR description short, no Claude credit; note the dependency on the unmerged stack + the pending sub-project A split).
