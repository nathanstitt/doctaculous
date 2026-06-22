package pdf

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrEncrypted is returned when a document uses an encryption scheme that is not
// supported: a non-Standard security handler, or a Standard handler with an
// unsupported /V or /R. Standard-handler documents readable with the empty user
// password are decrypted transparently; ones needing a real password return
// ErrEncryptedNeedsPassword instead.
var ErrEncrypted = errors.New("pdf: unsupported encryption")

// Document is a parsed PDF. After Open returns, a Document is read-only and safe
// for concurrent use by multiple goroutines.
type Document struct {
	data    []byte
	xref    map[int]xrefEntry // object number -> location
	trailer Dict

	// cacheMu guards the lazily populated caches below. A Document is logically
	// read-only to callers, but object resolution memoizes parsed objects, so the
	// caches are guarded to keep concurrent page rendering safe. Caches are
	// per-Document (no package-level mutable state).
	cacheMu        sync.Mutex
	objCache       map[int]Object           // resolved top-level objects by number
	objStreamCache map[int]*parsedObjStream // decoded object streams by number

	// enc is the Standard Security Handler state, or nil for an unencrypted
	// document. It is computed once during Parse and read-only afterwards.
	enc *encrypter

	pages []*Page // flattened page list in document order
}

// xrefEntry locates an object: either at a byte offset in the file, or inside a
// compressed object stream.
type xrefEntry struct {
	inStream    bool
	offset      int64 // byte offset (when inStream is false)
	streamObj   int   // object stream's object number (when inStream is true)
	indexInStrm int   // index within the object stream
}

// Open reads and parses a PDF file from disk.
func Open(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse parses a PDF from an in-memory byte slice. The slice is retained by the
// returned Document and must not be modified by the caller.
func Parse(data []byte) (*Document, error) {
	d := &Document{
		data:           data,
		xref:           make(map[int]xrefEntry),
		objCache:       make(map[int]Object),
		objStreamCache: make(map[int]*parsedObjStream),
	}
	if err := d.readXref(); err != nil {
		// Fall back to a brute-force scan for "N G obj" if xref parsing fails;
		// many real-world PDFs have broken xref tables.
		if rerr := d.rebuildXref(); rerr != nil {
			return nil, fmt.Errorf("pdf: %w (xref rebuild also failed: %v)", err, rerr)
		}
	}
	if d.trailer == nil {
		if err := d.rebuildXref(); err != nil {
			return nil, fmt.Errorf("pdf: no trailer found: %w", err)
		}
	}
	enc, err := d.setupEncryption()
	if err != nil {
		return nil, err
	}
	d.enc = enc
	if err := d.loadPages(); err != nil {
		// The xref may have pointed at stale offsets (common in damaged files).
		// Retry once after a brute-force rebuild before giving up.
		if rerr := d.retryWithRebuild(); rerr != nil {
			return nil, err
		}
	}
	return d, nil
}

// retryWithRebuild clears cached state, rebuilds the xref by scanning the file,
// and re-walks the page tree. It is the recovery path when xref-driven parsing
// produced no usable pages.
func (d *Document) retryWithRebuild() error {
	d.xref = make(map[int]xrefEntry)
	d.objCache = make(map[int]Object)
	d.objStreamCache = make(map[int]*parsedObjStream)
	d.trailer = nil
	d.pages = nil
	if err := d.rebuildXref(); err != nil {
		return err
	}
	return d.loadPages()
}

// PageCount returns the number of pages in the document.
func (d *Document) PageCount() int { return len(d.pages) }

// Page returns the page at the given zero-based index.
func (d *Document) Page(i int) (*Page, error) {
	if i < 0 || i >= len(d.pages) {
		return nil, fmt.Errorf("pdf: page index %d out of range [0,%d)", i, len(d.pages))
	}
	return d.pages[i], nil
}

// Trailer returns the document trailer dictionary.
func (d *Document) Trailer() Dict { return d.trailer }
