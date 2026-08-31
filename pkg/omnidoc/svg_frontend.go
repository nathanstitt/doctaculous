package omnidoc

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/nathanstitt/omnidoc/pkg/internal/layout"
	"github.com/nathanstitt/omnidoc/pkg/internal/svg"
	svgdraw "github.com/nathanstitt/omnidoc/pkg/internal/svg/draw"
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
		return nil, fmt.Errorf("omnidoc: open svg %q: %w", path, err)
	}
	return OpenSVGBytes(data, opts...)
}

// svgzMaxSize caps .svgz decompression (untrusted input; an SVG bigger than
// this is not a real-world document).
const svgzMaxSize = 64 << 20

// OpenSVGBytes opens an in-memory SVG document, applying any options. Of the
// options, only WithLogf currently applies (it receives svg.Parse's
// degradation diagnostics — unsupported elements, ignored group opacity,
// invalid viewBox fallbacks, non-finite coordinates); the rest (viewport,
// resource loader, media, ctx, ...) remain inert for SVG — the resource
// loader starts applying when <image> support lands.
func OpenSVGBytes(data []byte, opts ...OpenOption) (*Document, error) {
	cfg := defaultOpenConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("omnidoc: open svgz: %w", err)
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
			return nil, fmt.Errorf("omnidoc: decompress svgz: %w", err)
		}
		if len(data) > svgzMaxSize {
			return nil, fmt.Errorf("omnidoc: svgz decompresses to more than %d bytes", svgzMaxSize)
		}
	}
	sd, err := svg.Parse(data, cfg.logf)
	if err != nil {
		return nil, fmt.Errorf("omnidoc: parse svg: %w", err)
	}
	pages := &layout.Pages{Pages: []layout.Page{{
		WidthPt:  sd.WidthPt,
		HeightPt: sd.HeightPt,
		Items: []layout.Item{{
			Kind: layout.VectorKind,
			Vector: layout.VectorItem{
				// The same logger svg.Parse got above. Text degrades at DRAW time,
				// not parse time, so passing it only to the parser reported the
				// malformed-input half and silently dropped the missing-glyph half.
				Scene: svgdraw.NewWithLogf(sd, cfg.logf),
				XPt:   0, YPt: 0, WPt: sd.WidthPt, HPt: sd.HeightPt,
			},
		}},
	}}}
	return &Document{r: &reflowRenderer{pages: pages}, format: FormatSVG}, nil
}
