package doctaculous

import (
	"fmt"
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
	return openDetected(DetectFormat(data, path), data, filepath.Dir(path), opts)
}

// OpenBytes opens an in-memory document, detecting its format from content
// (there is no filename to consult, so extension-only formats like Markdown
// need OpenBytesAs). The slice is retained and must not be modified by the
// caller.
func OpenBytes(data []byte, opts ...OpenOption) (*Document, error) {
	return openDetected(DetectFormat(data, ""), data, "", opts)
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
	return openDetected(f, data, filepath.Dir(path), opts)
}

// OpenBytesAs opens an in-memory document as the named format, skipping
// detection. The slice is retained and must not be modified by the caller.
func OpenBytesAs(f Format, data []byte, opts ...OpenOption) (*Document, error) {
	return openDetected(f, data, "", opts)
}

// openDetected is the single input dispatch behind Open/OpenAs/Convert: it
// opens data as format f. A non-empty dir roots the default resource loader
// and disk font provider for HTML-family inputs (prepended before the caller's
// opts, exactly as OpenHTMLFile does, so caller options win).
func openDetected(f Format, data []byte, dir string, opts []OpenOption) (*Document, error) {
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
		return docxDocument(d)
	case FormatHTML:
		return openReflowFrontend(OpenHTMLBytes, data, dir, opts)
	case FormatMarkdown:
		return openReflowFrontend(OpenMarkdownBytes, data, dir, opts)
	case FormatText:
		return openReflowFrontend(OpenTextBytes, data, dir, opts)
	case FormatPNG, FormatJPEG:
		return nil, fmt.Errorf("doctaculous: %s is not a supported input format: %w", f, ErrUnsupportedFormat)
	default:
		return nil, fmt.Errorf("doctaculous: cannot detect the document format (open with an explicit format via OpenAs, or use a recognizable file extension): %w", ErrUnknownFormat)
	}
}

// openReflowFrontend opens data through one of the HTML-family frontends
// (HTML, Markdown, plain text — everything that flows through the HTML
// pipeline), prepending the dir-rooted resource defaults when the source
// directory is known, exactly as OpenHTMLFile does, so caller options win.
func openReflowFrontend(open func([]byte, ...OpenOption) (*Document, error), data []byte, dir string, opts []OpenOption) (*Document, error) {
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
