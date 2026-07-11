package doctaculous

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/extract"
	"github.com/nathanstitt/doctaculous/pkg/render/raster"
)

// pdfRenderer renders a parsed PDF document. The *pdf.Document is read-only after
// parsing, so it is shared across the page fan-out without locks.
type pdfRenderer struct {
	doc *pdf.Document

	// extractOnce/extractRoot lazily hold the structure-recovery cssbox tree, built on
	// the first WriteMarkdown/WriteText/WriteHTML (extraction is expensive and most
	// callers only rasterize). The tree is read-only once built. This makes a PDF
	// document satisfy reflowTree, so the conversion backends work on PDF inputs.
	extractOnce sync.Once
	extractRoot *cssbox.Box
}

func (r *pdfRenderer) pageCount() int { return r.doc.PageCount() }

// cssboxRoot lazily extracts the PDF's logical structure (paragraphs, headings, lists,
// tables) into a cssbox tree the conversion backends walk, satisfying reflowTree so
// WriteMarkdown/WriteText/WriteHTML work on PDF inputs. Extraction runs once and is
// cached; it never panics (the extractor recovers per page). A nil result (extraction
// failure) yields an empty document downstream rather than an error, matching the
// degrade-gracefully rule.
func (r *pdfRenderer) cssboxRoot() *cssbox.Box {
	r.extractOnce.Do(func() {
		root, err := extract.Lower(r.doc, nil)
		if err != nil {
			return // extractRoot stays nil; downstream writes an empty document
		}
		r.extractRoot = root
	})
	return r.extractRoot
}

// pageSize reports the page's MediaBox size in points, post-/Rotate — the same
// geometry choices raster.RenderPage makes, so a fit computed from this size
// matches the rendered pixel dimensions exactly.
func (r *pdfRenderer) pageSize(index int) (float64, float64, error) {
	pg, err := r.doc.Page(index)
	if err != nil {
		return 0, 0, fmt.Errorf("page %d: %w", index, err)
	}
	w, h := pg.MediaBox.Width(), pg.MediaBox.Height()
	// Mirror raster.RenderPage's validation so fit math never divides by junk
	// from a crafted MediaBox (NaN fails the > 0 comparison).
	if !(w > 0 && h > 0) || math.IsInf(w, 1) || math.IsInf(h, 1) {
		return 0, 0, fmt.Errorf("page %d: invalid MediaBox %gx%g", index, w, h)
	}
	if pg.Rotate == 90 || pg.Rotate == 270 {
		w, h = h, w
	}
	return w, h, nil
}

func (r *pdfRenderer) renderPage(ctx context.Context, index int, opts RasterOptions) (image.Image, error) {
	pg, err := r.doc.Page(index)
	if err != nil {
		return nil, fmt.Errorf("page %d: %w", index, err)
	}
	return raster.RenderPage(ctx, pg, raster.Options{
		DPI:          opts.dpi(),
		Background:   opts.Background,
		Logf:         opts.Logf,
		FontProvider: opts.fontProvider(),
	})
}

// fontProvider resolves the font provider for a rasterize call per the mode precedence:
// an explicit FontProvider always wins; else bundled mode (BundledFonts) installs no
// provider (bundled-only); else the default installs an OSFontProvider so installed OS
// fonts are used, falling through to the bundled substitute when none match.
func (o RasterOptions) fontProvider() font.Provider {
	if o.FontProvider != nil {
		return o.FontProvider
	}
	if o.BundledFonts {
		return nil
	}
	return layoutfont.NewOSFontProviderWithLogf(o.Logf)
}
