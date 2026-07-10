package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPLoader is a ResourceLoader that fetches refs over HTTP(S), resolving
// relative refs against a base (document) URL, and also decodes data: URIs
// inline. It is the loader the public URL path (OpenURL) uses. It degrades a
// failed or disallowed fetch to ErrNotFound so a remote sub-resource behaves
// exactly like a missing local one (a skipped stylesheet / a placeholder image),
// never panicking. The zero value is not usable; Base is required.
type HTTPLoader struct {
	// Base is the document's URL; relative refs resolve against it. Required.
	// Userinfo in Base (user:pw@host) is extracted into an Authorization: Basic
	// header for every same-origin fetch; it is NOT forwarded to a cross-origin
	// ref (ResolveReference drops the base's userinfo for a different authority).
	Base *url.URL
	// Client is the HTTP client used for fetches. nil selects a default client
	// with a request timeout (defaultRequestTimeout), created per fetch — so for a
	// page with many sub-resources, inject a shared client to reuse connections.
	// Injecting a client also lets you supply auth via a RoundTripper, a proxy,
	// mTLS, or a test transport.
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

// LoadDataURL decodes a raw data: URI — the loader-independent entry point
// (a data: URI carries its own bytes, so resolving one must not require any
// configured loader; the layout engine uses this for data: image sources).
// A non-data: URL or a malformed URI returns ErrNotFound (wrapped).
func LoadDataURL(rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "data" {
		return nil, "", fmt.Errorf("%q: not a data URI: %w", rawURL, ErrNotFound)
	}
	return decodeDataURL(u)
}

// decodeDataURL decodes a data: URI per RFC 2397:
// data:[<mediatype>][;base64],<payload>. A ;base64 payload is base64-decoded;
// otherwise the payload is percent-decoded text. A missing media type defaults to
// text/plain;charset=US-ASCII. A malformed URI returns ErrNotFound.
func decodeDataURL(u *url.URL) ([]byte, string, error) {
	// url.Parse puts everything after "data:" into u.Opaque. Reconstruct the raw
	// payload from Opaque and split on the first comma.
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
// body, and treating any non-2xx status as absent (ErrNotFound). When u carries
// userinfo, the credentials are extracted into an Authorization: Basic header and
// stripped from the outbound URL so they travel in the header (testable, never
// echoed in the request line) rather than in the URL.
func (h HTTPLoader) fetch(ctx context.Context, u *url.URL) ([]byte, string, error) {
	// Strip userinfo before building the request URL (see the doc comment); the
	// credentials go in the Authorization header below instead.
	outbound := *u
	var user, pass string
	haveAuth := false
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
	// Clamp so max+1 below cannot overflow to a negative limit (io.LimitReader
	// treats a negative n as zero, which would make every body look empty).
	if max > math.MaxInt64-1 {
		max = math.MaxInt64 - 1
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

// redact returns a URL string safe for logs/errors: scheme://host/path with the
// userinfo dropped and the query elided. Userinfo is the obvious credential; the
// query is dropped too because it commonly carries secrets (?token=…), and the
// host+path is what actually aids debugging. The path is kept verbatim.
func redact(u *url.URL) string {
	r := *u
	r.User = nil
	if r.RawQuery != "" || r.ForceQuery {
		r.RawQuery = ""
		r.ForceQuery = false
		r.Fragment = ""
		return r.String() + "?…"
	}
	r.Fragment = ""
	return r.String()
}
