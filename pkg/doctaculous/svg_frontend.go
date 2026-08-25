package doctaculous

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/svg"
	svgdraw "github.com/nathanstitt/doctaculous/pkg/svg/draw"
)

// OpenSVG reads a standalone SVG (or gzip-compressed .svgz) as a single-page
// document: the page is exactly the SVG's viewport (1 user unit = 1 pt), and
// every conversion follows — rasterization renders at device resolution, and
// PDF output carries real vector paths, not a bitmap.
func OpenSVG(path string, opts ...OpenOption) (*Document, error) { return OpenSVGFile(path, opts...) }

// OpenSVGFile reads an SVG file at path, applying any options.
func OpenSVGFile(path string, opts ...OpenOption) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: open svg %q: %w", path, err)
	}
	return OpenSVGBytes(data, opts...)
}

// svgzMaxSize caps .svgz decompression (untrusted input; an SVG bigger than
// this is not a real-world document).
const svgzMaxSize = 64 << 20

// OpenSVGBytes opens an in-memory SVG document, applying any options. Options
// are accepted for uniformity with the other frontends; none affect SVG yet
// (the resource loader starts applying when <image> support lands).
func OpenSVGBytes(data []byte, opts ...OpenOption) (*Document, error) {
	_ = opts
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("doctaculous: open svgz: %w", err)
		}
		// Read one byte past the cap so an exactly-at-the-limit stream and an
		// over-the-limit stream are distinguishable. svg.Parse's XML reader
		// treats input truncated after the root element as valid-but-partial
		// (a deliberate leniency for genuinely malformed real-world SVG), so a
		// silent LimitReader cutoff here would be indistinguishable from that
		// and would return a Document with quietly missing content instead of
		// an error. Detect the cutoff explicitly and fail closed.
		data, err = io.ReadAll(io.LimitReader(zr, svgzMaxSize+1))
		if err != nil {
			return nil, fmt.Errorf("doctaculous: decompress svgz: %w", err)
		}
		if len(data) > svgzMaxSize {
			return nil, fmt.Errorf("doctaculous: svgz decompresses to more than %d bytes", svgzMaxSize)
		}
	}
	// logf is nil: OpenOption's WithLogf/openConfig plumbing belongs to the
	// HTML-family reflow frontends (openReflowFrontend / openConfig.logf) and
	// isn't threaded into this entry point, so svg.Parse's degradation
	// diagnostics (unsupported elements, ignored group opacity, invalid
	// viewBox fallbacks, non-finite coordinates) are currently dropped rather
	// than silently misdirected to the wrong channel.
	sd, err := svg.Parse(data, nil)
	if err != nil {
		return nil, fmt.Errorf("doctaculous: parse svg: %w", err)
	}
	pages := &layout.Pages{Pages: []layout.Page{{
		WidthPt:  sd.WidthPt,
		HeightPt: sd.HeightPt,
		Items: []layout.Item{{
			Kind: layout.VectorKind,
			Vector: layout.VectorItem{
				Scene: svgdraw.New(sd),
				XPt:   0, YPt: 0, WPt: sd.WidthPt, HPt: sd.HeightPt,
			},
		}},
	}}}
	return &Document{r: &reflowRenderer{pages: pages}, format: FormatSVG}, nil
}
