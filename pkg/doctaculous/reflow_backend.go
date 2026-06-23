package doctaculous

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	docxlower "github.com/nathanstitt/doctaculous/pkg/docx/lower"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	"github.com/nathanstitt/doctaculous/pkg/layout"
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

// docxDocument lowers a parsed DOCX through the style cascade into the neutral box
// model and runs the reflow engine, wrapping the result for rasterization.
func docxDocument(d *docx.Document) (*Document, error) {
	resolver := style.NewResolver(d, nil)
	boxDoc := docxlower.Document(d, resolver)
	engine := layout.New(layoutfont.NewFaceCache(), nil)
	pages, err := engine.Layout(context.Background(), boxDoc)
	if err != nil {
		return nil, err
	}
	return &Document{r: &reflowRenderer{pages: pages}}, nil
}

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
	bg := opts.Background
	if bg == nil {
		bg = color.White
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
