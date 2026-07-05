package doctaculous

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	docxcssbox "github.com/nathanstitt/doctaculous/pkg/docx/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/paint"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// reflowRenderer renders a reflowable document that has already been laid out into
// pages. It is shared by every reflow format (DOCX today; HTML/EPUB later), since
// once a frontend has produced *layout.Pages the rasterization is identical. The
// laid-out pages are read-only, so the page fan-out needs no locks.
type reflowRenderer struct {
	pages *layout.Pages
}

// OpenDOCX reads and parses a .docx file, lays out all pages, and returns a
// Document ready to rasterize. Layout runs once here (pagination is global, so
// pages cannot be laid out independently); rasterization then parallelizes over
// the precomputed pages.
func OpenDOCX(path string) (*Document, error) {
	d, err := docx.Open(path)
	if err != nil {
		return nil, err
	}
	return docxDocument(d)
}

// OpenDOCXBytes parses a .docx from an in-memory byte slice and lays it out.
func OpenDOCXBytes(data []byte) (*Document, error) {
	d, err := docx.OpenBytes(data)
	if err != nil {
		return nil, err
	}
	return docxDocument(d)
}

// docxDocument lowers a parsed DOCX through the style cascade into the recursive
// cssbox tree and runs the shared CSS layout engine, wrapping the result for
// rasterization. The DOCX section's page size and margins are carried into the CSS
// paged engine as a synthesized @page stylesheet (docxPageSheet), reusing the exact
// margin-inset machinery HTML uses for a real @page rule.
func docxDocument(d *docx.Document) (*Document, error) {
	resolver := style.NewResolver(d, nil)
	root := docxcssbox.Lower(d, resolver)
	geom := docxcssbox.Geometry(d)
	ctx := context.Background()
	faces := layoutfont.NewFaceCache()
	engine := layoutcss.New(faces, docxcssbox.MediaLoader(d), nil)
	pages, err := engine.LayoutPagedDoc(ctx, root, layoutcss.PagedConfig{
		Paged:        true,
		FallbackW:    geom.PageWidthPt, // full page; @page size/margins refine below
		FallbackH:    geom.PageHeightPt,
		ExplicitSize: false, // let the synthesized @page size apply
		Pages:        docxPageSheet(geom),
	})
	if err != nil {
		return nil, err
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}

// docxPageSheet synthesizes an @page stylesheet carrying the DOCX section's page
// size and margins, so the CSS paged engine insets DOCX content exactly as it does
// for an HTML @page rule. Point values are emitted as px (the layout scalar treats
// px:pt 1:1), preserving DOCX's physical 72dpi-equivalent scale.
func docxPageSheet(g docxcssbox.PageGeometry) gcss.Stylesheet {
	// %f (not %g) so a fractional twip→point value can never fall into %g's exponent
	// notation, which the @page length parser would reject.
	px := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "px" }
	css := fmt.Sprintf("@page { size: %s %s; margin: %s %s %s %s }",
		px(g.PageWidthPt), px(g.PageHeightPt),
		px(g.MarginTopPt), px(g.MarginRightPt), px(g.MarginBottomPt), px(g.MarginLeftPt))
	return gcss.Parse(css)
}

// reflowPages is implemented by renderers backed by *layout.Pages, so the PDF writer
// can drive the same laid-out pages the rasterizer uses.
type reflowPages interface{ layoutPages() *layout.Pages }

// layoutPages exposes the laid-out pages for the PDF writer (WritePDF).
func (r *reflowRenderer) layoutPages() *layout.Pages { return r.pages }

func (r *reflowRenderer) pageCount() int { return len(r.pages.Pages) }

func (r *reflowRenderer) renderPage(_ context.Context, index int, opts RasterOptions) (image.Image, error) {
	if index < 0 || index >= len(r.pages.Pages) {
		return nil, errPageOutOfRange(index, len(r.pages.Pages))
	}
	pg := &r.pages.Pages[index]

	scale := opts.dpi() / 72
	pxW := int(math.Ceil(pg.WidthPt * scale))
	pxH := int(math.Ceil(pg.HeightPt * scale))
	if pxW <= 0 || pxH <= 0 {
		pxW, pxH = 1, 1
	}

	img := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	// Canvas fill precedence: a CSS-propagated root/body background (the browser's
	// background-propagation rule, set by the layout engine) wins; else the caller's
	// RasterOptions.Background; else opaque white.
	bg := opts.Background
	if bg == nil {
		bg = color.White
	}
	if cb := r.pages.CanvasBackground; cb.A != 0 {
		bg = cb
	}
	fillBackground(img, bg)

	dev := raster.New(img)
	dev.SetLogf(opts.Logf)
	// Page space is already points, Y-down, origin top-left, so the transform to
	// device pixels is a single uniform scale — no Y-flip (unlike PDF).
	mat := render.Scale(scale, scale)
	paint.PaintPage(dev, pg, mat)
	return img, nil
}
