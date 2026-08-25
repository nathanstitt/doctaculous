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
		data, err = io.ReadAll(io.LimitReader(zr, svgzMaxSize))
		if err != nil {
			return nil, fmt.Errorf("doctaculous: decompress svgz: %w", err)
		}
	}
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
