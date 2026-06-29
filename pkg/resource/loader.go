// Package resource defines the seam by which a document's external references
// (stylesheets via <link>, images, and fonts) are resolved to bytes. It provides
// three implementations: MapLoader (in-memory, the primary hermetic loader for
// tests), DirLoader (on-disk, for local fixtures), and HTTPLoader (HTTP(S) fetch
// plus inline data: URI decoding, used by the public OpenURL path). Every loader
// honors ctx cancellation and degrades an absent or failed ref to ErrNotFound so
// the pipeline never panics on a missing sub-resource. The two hermetic loaders
// touch no network; HTTPLoader's tests stay offline via httptest (loopback).
package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned (wrapped) when a loader cannot find a ref, so callers
// can distinguish "absent" from "broken" and degrade gracefully.
var ErrNotFound = errors.New("resource not found")

// ResourceLoader resolves a ref (a URL or path string) to its bytes and content
// type. Implementations must honor ctx cancellation.
type ResourceLoader interface {
	Load(ctx context.Context, ref string) (data []byte, contentType string, err error)
}

var (
	_ ResourceLoader = MapLoader(nil)
	_ ResourceLoader = DirLoader{}
)

// Resource is one entry in a MapLoader.
type Resource struct {
	Data        []byte // the resource's raw bytes
	ContentType string // MIME type, e.g. "text/css"; "" if unknown
}

// MapLoader is an in-memory ResourceLoader keyed by exact ref string. It is the
// primary hermetic loader for tests.
type MapLoader map[string]Resource

// Load implements ResourceLoader.
func (m MapLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	r, ok := m[ref]
	if !ok {
		return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
	}
	return r.Data, r.ContentType, nil
}

// DirLoader is a ResourceLoader that serves files from a base directory, with
// content type inferred from the file extension. It is intended for test
// fixtures on local disk; it refuses refs that escape Base (e.g. "../x") rather
// than following them, but is not hardened for untrusted input beyond that.
type DirLoader struct {
	Base string
}

// Load implements ResourceLoader.
func (d DirLoader) Load(ctx context.Context, ref string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	// Refuse refs that escape Base via "..". Treat an out-of-bounds ref as absent.
	base := filepath.Clean(d.Base)
	full := filepath.Clean(filepath.Join(base, ref))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("%q: %w", ref, ErrNotFound)
		}
		return nil, "", fmt.Errorf("read %q: %w", ref, err)
	}
	return data, contentTypeByExt(ref), nil
}

// contentTypeByExt returns a minimal content type from a ref's extension; "" if
// unknown (callers that care can sniff, but this sub-project only needs CSS).
func contentTypeByExt(ref string) string {
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".css":
		return "text/css"
	case ".html", ".htm":
		return "text/html"
	default:
		return ""
	}
}
