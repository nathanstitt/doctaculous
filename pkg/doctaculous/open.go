package doctaculous

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// OpenOption configures generic opening. It is an alias of HTMLOption: the
// options configure reflow layout and resource loading, and apply to inputs
// that flow through the HTML pipeline (HTML files and URLs); PDF and DOCX
// inputs ignore them. Format-specific rendering knobs live on the Write side
// (RasterOptions, PDFOptions, ...).
type OpenOption = HTMLOption

// Open opens the document at path, detecting its format from content and
// filename (see DetectFormat) — a PDF, DOCX, or HTML file regardless of how it
// is named. When path is an http(s):// URL the document is fetched and
// rendered as a web page (OpenURL). Undetectable content returns
// ErrUnknownFormat; use OpenAs to name the format explicitly. For HTML files,
// relative <link>/<img>/@font-face refs resolve from the file's directory,
// exactly as OpenHTMLFile.
func Open(path string, opts ...OpenOption) (*Document, error) {
	if isHTTPURL(path) {
		return OpenURL(path, opts...)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open %q: %w", path, err)
	}
	return openDetected(context.Background(), DetectFormat(data, path), data, filepath.Dir(path), opts)
}

// OpenBytes opens an in-memory document, detecting its format from content
// (there is no filename to consult, so extension-only formats like Markdown
// need OpenBytesAs). The slice is retained and must not be modified by the
// caller.
func OpenBytes(data []byte, opts ...OpenOption) (*Document, error) {
	return openDetected(context.Background(), DetectFormat(data, ""), data, "", opts)
}

// OpenReader opens a document from a stream, detecting its format from content
// (DetectFormat with no filename hint, so extension-only formats like Markdown
// and CSV need OpenReaderAs). The reader is fully buffered in memory before
// parsing — every parser needs random access — so cap untrusted input upstream
// (io.LimitReader, http.MaxBytesReader); the buffer is owned by the returned
// Document. ctx bounds open-time layout and resource loading for reflow inputs
// (HTML, DOCX, Markdown, ...) and is honored by rasterization later; PDF
// parsing itself is not interruptible today.
func OpenReader(ctx context.Context, r io.Reader, opts ...OpenOption) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: read input: %w", err)
	}
	return openDetected(ctx, DetectFormat(data, ""), data, "", opts)
}

// OpenReaderAs opens a stream as the named format, skipping detection — the
// normal entry point when the MIME type is known:
// OpenReaderAs(ctx, FormatFromMIME(mt), r). Same buffering and ctx semantics
// as OpenReader.
func OpenReaderAs(ctx context.Context, f Format, r io.Reader, opts ...OpenOption) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: read input: %w", err)
	}
	return openDetected(ctx, f, data, "", opts)
}

// OpenAs opens the file at path as the named format, skipping detection — the
// escape hatch for extension-less or mislabeled files (it backs the CLI's
// --from flag). An http(s):// URL is only openable as HTML (a URL is always
// fetched as a web page).
func OpenAs(f Format, path string, opts ...OpenOption) (*Document, error) {
	if isHTTPURL(path) {
		if f == FormatHTML {
			return OpenURL(path, opts...)
		}
		return nil, fmt.Errorf("doctaculous: open %q: an http(s) URL is fetched as a web page (HTML), not %s", path, f)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open %q: %w", path, err)
	}
	return openDetected(context.Background(), f, data, filepath.Dir(path), opts)
}

// OpenBytesAs opens an in-memory document as the named format, skipping
// detection. The slice is retained and must not be modified by the caller.
func OpenBytesAs(f Format, data []byte, opts ...OpenOption) (*Document, error) {
	return openDetected(context.Background(), f, data, "", opts)
}

// openDetected is the single input dispatch behind Open/OpenAs/Convert: it
// opens data as format f. A non-empty dir roots the default resource loader
// and disk font provider for HTML-family inputs (prepended before the caller's
// opts, exactly as OpenHTMLFile does, so caller options win). ctx bounds
// open-time layout and resource loading for the reflow frontends.
func openDetected(ctx context.Context, f Format, data []byte, dir string, opts []OpenOption) (*Document, error) {
	switch f {
	case FormatPDF:
		d, err := pdf.Parse(data)
		if err != nil {
			return nil, err
		}
		return &Document{r: &pdfRenderer{doc: d}, format: FormatPDF}, nil
	case FormatDOCX:
		d, err := docx.OpenBytes(data)
		if err != nil {
			return nil, err
		}
		return docxDocument(ctx, d)
	case FormatHTML:
		return openReflowFrontend(ctx, OpenHTMLBytes, data, dir, opts)
	case FormatMarkdown:
		return openReflowFrontend(ctx, OpenMarkdownBytes, data, dir, opts)
	case FormatText:
		return openReflowFrontend(ctx, OpenTextBytes, data, dir, opts)
	case FormatCSV:
		return openReflowFrontend(ctx, OpenCSVBytes, data, dir, opts)
	case FormatTSV:
		return openReflowFrontend(ctx, OpenTSVBytes, data, dir, opts)
	case FormatXLSX:
		return openReflowFrontend(ctx, OpenXLSXBytes, data, dir, opts)
	case FormatRTF:
		return openReflowFrontend(ctx, OpenRTFBytes, data, dir, opts)
	case FormatPPTX:
		return openReflowFrontend(ctx, OpenPPTXBytes, data, dir, opts)
	case FormatEPUB:
		// A book's resources live in its container, not next to the file: skip
		// the dir-rooted loader default (it would override the container
		// loader the frontend installs) — a caller's explicit loader still wins.
		return openReflowFrontend(ctx, OpenEPUBBytes, data, "", opts)
	case FormatPNG, FormatJPEG:
		// An image opens as a single page exactly its pixel size; the frontend
		// stamps the format from the actual encoding.
		return openReflowFrontend(ctx, OpenImageBytes, data, "", opts)
	default:
		return nil, fmt.Errorf("doctaculous: cannot detect the document format (open with an explicit format via OpenAs or OpenReaderAs, or use a recognizable file extension): %w", ErrUnknownFormat)
	}
}

// openReflowFrontend opens data through one of the HTML-family frontends
// (HTML, Markdown, plain text — everything that flows through the HTML
// pipeline), prepending the dir-rooted resource defaults when the source
// directory is known, exactly as OpenHTMLFile does, so caller options win. The
// open context rides along as a prepended (unexported) option so the frontend
// signatures stay unchanged.
func openReflowFrontend(ctx context.Context, open func([]byte, ...OpenOption) (*Document, error), data []byte, dir string, opts []OpenOption) (*Document, error) {
	opts = append([]OpenOption{withOpenContext(ctx)}, opts...)
	if dir != "" {
		opts = append([]OpenOption{
			WithResourceLoader(resource.DirLoader{Base: dir}),
			WithSystemFontProvider(layoutfont.DiskFontProvider{Dir: dir}),
		}, opts...)
	}
	return open(data, opts...)
}

// isHTTPURL reports whether path is an http:// or https:// URL. A simple
// scheme-prefix test: a bare path or file:// URL is not matched (OpenURL
// rejects non-http(s) schemes anyway).
func isHTTPURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
