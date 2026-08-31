package omnidoc

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"math"

	"github.com/nathanstitt/omnidoc/pkg/internal/font"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
	"github.com/nathanstitt/omnidoc/pkg/internal/svgwrite"
)

// SVGOptions controls SVG output.
type SVGOptions struct {
	// Page is the zero-based page WriteSVG renders on the generic Convert
	// path: one io.Writer holds exactly one SVG document, so multi-page output
	// is a batch concern handled by the caller (the CLI fans out with a %d
	// output pattern, exactly as it does for PNG). WriteSVG takes its page
	// index explicitly and ignores this field.
	Page int
	// Scale multiplies document points to SVG user units; default 1 when zero,
	// which makes one user unit one point. The output is resolution
	// independent, so this only sets the document's intrinsic size — it is not
	// a quality knob like RasterOptions.DPI.
	Scale float64
	// Background fills the page before drawing. nil (the default) leaves the
	// page transparent, unlike rasterization's opaque-white default: a vector
	// document composited over an unknown backdrop should not carry an assumed
	// white rectangle.
	Background color.Color
	// Title sets the root <title>, which assistive technology reads.
	Title string
	// Logf receives degradation diagnostics (nil -> no-op).
	Logf func(string, ...any)
	// FontProvider and BundledFonts mirror RasterOptions: they resolve the
	// faces whose outlines are emitted. Glyphs are written as outlines, so the
	// face chosen here determines the shapes in the output.
	FontProvider font.Provider
	BundledFonts bool
}

func (o SVGOptions) scale() float64 {
	if o.Scale <= 0 || math.IsNaN(o.Scale) || math.IsInf(o.Scale, 0) {
		return 1
	}
	return o.Scale
}

// toRasterOptions adapts the shared knobs for the paint/interpret pass, whose
// font resolution is identical to the rasterizer's.
func (o SVGOptions) toRasterOptions() RasterOptions {
	return RasterOptions{
		Logf:         o.Logf,
		FontProvider: o.FontProvider,
		BundledFonts: o.BundledFonts,
	}
}

// vectorPages is implemented by renderers that can paint a page onto an
// arbitrary render.Device rather than only onto a bitmap.
//
// This is deliberately NOT reflowPages (the interface WritePDF uses): that one
// hands back *layout.Pages, which an opened PDF has no equivalent of, so a
// writer built on it works for reflow inputs only. Every frontend already
// paints through render.Device — the PDF content interpreter, the reflow paint
// layer, and the SVG painter alike — so handing over the Device instead of the
// page model is what lets SVG output work for every input format.
type vectorPages interface {
	paintVector(ctx context.Context, index int, dev render.Device, scale float64, opts RasterOptions) error
}

// canvasBackgrounder is implemented by renderers whose source can propagate a
// background to the canvas (the CSS root/body rule). PDF has no equivalent, so
// pdfRenderer does not implement it.
type canvasBackgrounder interface{ canvasBackground() color.RGBA }

// resolveBackground applies the same precedence the rasterizer uses: a
// CSS-propagated canvas background wins over the caller's option.
//
// Only the default differs, and deliberately: rasterization falls back to
// opaque white because a bitmap has to commit to something, whereas an SVG
// composited over an unknown backdrop should stay transparent unless asked.
func resolveBackground(r renderer, opts SVGOptions) color.Color {
	if cb, ok := r.(canvasBackgrounder); ok {
		if bg := cb.canvasBackground(); bg.A != 0 {
			return bg
		}
	}
	return opts.Background
}

// paintVectorSafely runs the paint pass with a page-boundary panic recovery,
// mirroring what raster.RenderPage does for the bitmap path.
//
// The rule is the same one the rasterizer states: one bad page must not kill a
// batch. The CLI writes a page per file in a loop, so an unrecovered panic
// would abandon every remaining page — and callers may run this from worker
// goroutines, where a panic is process-fatal. On a fault the document written
// so far is still serialized, matching the rasterizer's "return the page
// painted so far" behavior rather than losing the whole conversion.
//
// A genuine error — a bad page index, a cancelled context, an unparseable page
// — propagates untouched, so this never turns a legitimate failure into a
// silently empty document.
//
// A recovered panic is reported as an error rather than swallowed. That is a
// deliberate divergence from raster.RenderPage, which returns the partially
// painted image with err=nil: a bitmap degrades usefully (background plus
// whatever drew), whereas a truncated vector document is indistinguishable
// from a correct sparse one, so a caller has no way to notice. The CLI's
// per-page loop already reports the failure and continues to the next page,
// which preserves the "one bad page can't kill a batch" rule while keeping the
// defect visible instead of shipping a quietly incomplete file.
func paintVectorSafely(ctx context.Context, vp vectorPages, index int, dev render.Device, scale float64, opts SVGOptions) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if opts.Logf != nil {
				opts.Logf("svgwrite: recovered from panic painting page %d: %v", index, r)
			}
			err = fmt.Errorf("panic painting page: %v", r)
		}
	}()
	return vp.paintVector(ctx, index, dev, scale, opts.toRasterOptions())
}

// WriteSVG renders page index (zero-based) to out as a standalone SVG document.
//
// It works on every input format, including PDF. Output is genuinely vector —
// paths, clips, masks and native gradients — not a rasterized page wrapped in
// an <image>. Text is emitted as glyph outlines; see pkg/render/svgwrite's
// package doc for why.
func (d *Document) WriteSVG(ctx context.Context, out io.Writer, index int, opts SVGOptions) error {
	vp, ok := d.r.(vectorPages)
	if !ok {
		return fmt.Errorf("omnidoc: WriteSVG: %s documents cannot be painted as vector", d.format)
	}
	wPt, hPt, err := d.r.pageSize(index)
	if err != nil {
		return fmt.Errorf("omnidoc: write svg: %w", err)
	}
	scale := opts.scale()
	w, h := int(math.Ceil(wPt*scale)), int(math.Ceil(hPt*scale))
	if w <= 0 || h <= 0 {
		return fmt.Errorf("omnidoc: write svg: degenerate page size %dx%d", w, h)
	}

	dev := svgwrite.New(w, h)
	dev.SetLogf(opts.Logf)
	if err := paintVectorSafely(ctx, vp, index, dev, scale, opts); err != nil {
		return fmt.Errorf("omnidoc: write svg page %d: %w", index, err)
	}
	if err := dev.WriteTo(out, svgwrite.Options{
		Background: resolveBackground(d.r, opts),
		Title:      opts.Title,
		Logf:       opts.Logf,
	}); err != nil {
		return fmt.Errorf("omnidoc: write svg: %w", err)
	}
	return nil
}
