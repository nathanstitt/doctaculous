package pdfwrite

import (
	"context"
	"fmt"
	"image"
	"io"
	"runtime"
	"sync"

	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout"
	"github.com/nathanstitt/doctaculous/pkg/layout/paint"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Options controls PDF output geometry and concurrency.
//
// PageWidthPt/PageHeightPt pin the PDF page box. When left <= 0 each PDF page takes
// its layout page's OWN size — so a document the reflow engine already paginated maps
// one layout page to one PDF page (no forced re-fragmentation or overflow). When
// PageHeightPt is pinned, a taller layout page is re-fragmented to fit, inset by
// MarginPt (a single tall page is sliced into fixed pages this way).
type Options struct {
	PageWidthPt  float64 // pinned page width; <= 0 = use the layout page's width
	PageHeightPt float64 // pinned page height; <= 0 = use the layout page's height
	MarginPt     float64 // content inset when PageHeightPt is pinned (0 = none)
	Title        string
	Workers      int // page-render worker cap; default GOMAXPROCS if <= 0
	Logf         func(string, ...any)
}

// band is a vertical slice [topPt, bottomPt) of a layout page placed on one PDF
// page. pdfW/pdfH are that PDF page's size in points and marginPt its content inset;
// they are resolved per layout page so an already-paginated document keeps each
// page's own geometry (no forced re-fragmentation to a single global size).
type band struct {
	page     *layout.Page
	topPt    float64
	bottomPt float64
	pdfW     float64
	pdfH     float64
	marginPt float64
}

// renderedPage is one worker's pure output: the page's content bytes, the images it
// referenced, and its PDF page size. Font codes come from the shared embedder (see
// WriteDocument's pre-pass), so a page carries NO font state and NO object ids —
// object ids are assigned during the sequential assembly, so workers share no writer
// state.
type renderedPage struct {
	index      int
	content    []byte
	images     []pendingImage
	pdfW, pdfH float64
}

// WriteDocument fragments the laid-out pages into fixed-size PDF pages, renders them
// concurrently, then assembles the PDF sequentially and writes it to out. It honors
// context cancellation and recovers per page so one bad page cannot abort the
// document.
func WriteDocument(ctx context.Context, out io.Writer, pages *layout.Pages, opts Options) error {
	opts = normalizeOpts(opts)

	// 1. Fragment every layout page into bands (cheap, sequential). Each layout page
	//    gets a PDF page sized to match it: when the caller pins Options.PageWidthPt/
	//    HeightPt those win (and a taller layout page re-fragments to fit); otherwise
	//    the layout page's OWN size is used, so a document the reflow engine already
	//    paginated maps one layout page to one PDF page with no re-slicing or overflow.
	var bands []band
	if pages != nil {
		// A pinned Options.PageHeightPt means the caller controls the page box, so
		// pdfwrite insets by MarginPt and re-fragments a taller layout page to fit.
		// Otherwise each layout page is taken as an already-finished page (the reflow
		// engine paginated it and applied @page margins): one band per page, no inset.
		pinnedH := opts.PageHeightPt > 0
		for i := range pages.Pages {
			lp := &pages.Pages[i]
			pdfW, pdfH := pdfPageSize(lp, opts)
			if !pinnedH {
				// One PDF page per layout page, verbatim.
				bands = append(bands, band{
					page: lp, topPt: 0, bottomPt: pdfH,
					pdfW: pdfW, pdfH: pdfH, marginPt: 0,
				})
				continue
			}
			margin := opts.MarginPt
			contentH := pdfH - 2*margin
			if contentH <= 0 {
				contentH = pdfH
				margin = 0
			}
			for _, b := range fragment(lp, contentH, opts.Logf) {
				bands = append(bands, band{
					page: lp, topPt: b.topPt, bottomPt: b.bottomPt,
					pdfW: pdfW, pdfH: pdfH, marginPt: margin,
				})
			}
		}
	}
	if len(bands) == 0 {
		// Emit a single blank page so the output is always a valid PDF.
		w, h := fallbackSize(opts)
		bands = append(bands, band{
			page:  &layout.Page{WidthPt: w, HeightPt: h},
			topPt: 0, bottomPt: h, pdfW: w, pdfH: h, marginPt: 0,
		})
	}

	// 2. Pre-pass (sequential): assign every glyph its emit code ONCE, in a shared
	//    embedder, walking bands in document order. This is what makes a face's
	//    /Differences (built from the embedder) agree with the codes the content
	//    streams emit — a per-page-local embedder would number the same glyph
	//    differently on each page and scramble the text. The pre-pass also fixes each
	//    face's resource name. After it, the embedder is frozen: the parallel render's
	//    use() calls all hit the already-seen (read-only) path, so sharing it across
	//    goroutines is race-free.
	embed := newFontEmbedder()
	for i := range bands {
		collectBandGlyphs(&bands[i], embed)
	}
	// Assign every face's resource name now, so the parallel render only READS the
	// name map (resourceName mutates on first sight — doing it here keeps the shared
	// embedder read-only during the concurrent phase).
	for _, face := range embed.orderedFaces() {
		embed.resourceName(face)
	}

	// 3. Render bands concurrently. Each device shares the frozen embedder for code
	//    lookups but owns its own content buffer and image list.
	rendered := renderBandsParallel(ctx, bands, embed, opts)
	if err := ctx.Err(); err != nil {
		return err
	}

	// 4. Assemble sequentially: one writer, each face embedded once from the shared
	//    embedder (so /Differences matches every page's codes).
	return assemble(out, rendered, embed, opts)
}

// collectBandGlyphs walks a band's glyph items (in the same order the painter would)
// and records each into embed, assigning its emit code. It mirrors the device's
// DrawGlyph glyph handling: only glyphs carrying a *font.Face identity are recorded;
// the rest paint as outlines and need no code.
func collectBandGlyphs(b *band, embed *fontEmbedder) {
	for i := range b.page.Items {
		it := &b.page.Items[i]
		if it.Kind != layout.GlyphKind {
			continue
		}
		if it.Glyph.Face == nil {
			continue
		}
		embed.use(it.Glyph.Face, it.Glyph.GID, it.Glyph.Runes)
	}
}

// renderBandsParallel renders each band on a bounded worker pool, returning results
// in document order (results[i] corresponds to bands[i]); a band that panics is left
// with content nil and logged. Every device shares the frozen embedder for code
// lookups (see WriteDocument's pre-pass), so no code is assigned here.
func renderBandsParallel(ctx context.Context, bands []band, embed *fontEmbedder, opts Options) []renderedPage {
	results := make([]renderedPage, len(bands))
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range bands {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil && opts.Logf != nil {
					opts.Logf("pdfwrite: page %d panicked: %v", idx, r)
				}
			}()
			results[idx] = renderBand(bands[idx], idx, embed, opts)
		}(i)
	}
	wg.Wait()
	return results
}

// renderBand paints one band into a pageDevice that shares the frozen embedder for
// code lookups but owns its own content buffer and image list, so it is safe to call
// from many goroutines. The band's content is painted at page space translated so the
// band's top maps to the content-area top; the PDF page MediaBox clips everything
// outside the band, so no per-item clipping is needed.
func renderBand(b band, index int, embed *fontEmbedder, opts Options) renderedPage {
	dev := newPageDeviceWithEmbedder(b.pdfW, b.pdfH, embed)
	dev.logf = opts.Logf
	mat := render.Translate(b.marginPt, b.marginPt-b.topPt)
	paint.PaintPage(dev, b.page, mat)
	return renderedPage{
		index:   index,
		content: dev.contentStream(),
		images:  dev.images,
		pdfW:    b.pdfW,
		pdfH:    b.pdfH,
	}
}

// assemble folds the rendered pages into a single PDF. Each face used anywhere is
// embedded ONCE from the shared embedder (whose codes match every page's content
// stream), and every page's /Font resource references those shared font objects.
func assemble(out io.Writer, rendered []renderedPage, embed *fontEmbedder, opts Options) error {
	w := newWriter()
	pagesRef := w.alloc()

	// Emit each unique face once; remember its top /Font ref.
	faceRef := map[*font.Face]Ref{}
	for _, face := range embed.orderedFaces() {
		if ref := embed.emit(w, face); ref != 0 {
			faceRef[face] = ref
		}
	}

	// The /Font resource dict is shared by every page: the embedder assigns one
	// resource name per face and one font object, and the device emitted those same
	// names (the shared embedder) into every content stream. Referencing all faces on
	// each page is harmless (an unused resource is ignored).
	sharedFonts := Dict{}
	for _, face := range embed.orderedFaces() {
		if ref, ok := faceRef[face]; ok {
			sharedFonts[embed.resourceName(face)] = ref
		}
	}

	var kids []object
	for _, rp := range rendered {
		if rp.content == nil {
			continue // failed/skipped band
		}
		flip := fmt.Sprintf("1 0 0 -1 0 %s cm\n", formatReal(rp.pdfH))
		contentRef := w.addStream(Dict{}, append([]byte(flip), rp.content...))

		res := Dict{}
		if len(sharedFonts) > 0 {
			res["Font"] = sharedFonts
		}
		if len(rp.images) > 0 {
			xobjs := Dict{}
			for _, pi := range rp.images {
				imgRef, err := embedImage(w, pi.img)
				if err != nil {
					if opts.Logf != nil {
						opts.Logf("pdfwrite: image embed failed: %v", err)
					}
					continue
				}
				xobjs[pi.name] = imgRef
			}
			res["XObject"] = xobjs
		}

		pageRef := w.alloc()
		w.put(pageRef, Dict{
			"Type":      Name("Page"),
			"Parent":    pagesRef,
			"MediaBox":  Array{Int(0), Int(0), Real(rp.pdfW), Real(rp.pdfH)},
			"Contents":  contentRef,
			"Resources": res,
		})
		kids = append(kids, pageRef)
	}

	w.put(pagesRef, Dict{"Type": Name("Pages"), "Kids": Array(kids), "Count": Int(int64(len(kids)))})
	catalog := w.alloc()
	w.put(catalog, Dict{"Type": Name("Catalog"), "Pages": pagesRef})
	w.setRoot(catalog)
	if opts.Title != "" {
		info := w.alloc()
		w.put(info, Dict{"Title": String(opts.Title)})
		w.setInfo(info)
	}
	return w.serialize(out)
}

// yBand is a [top,bottom) vertical slice of a layout page, in page-space points.
type yBand struct {
	topPt, bottomPt float64
}

// fragment slices a layout page into bands at most contentH tall, breaking between
// glyph rows (never inside one). A page that already fits contentH yields one band.
// A single row taller than contentH gets its own band (it overflows, clipped by the
// MediaBox; logged). A page with no items yields one empty band so a blank page is
// still emitted.
func fragment(lp *layout.Page, contentH float64, logf func(string, ...any)) []yBand {
	if lp == nil {
		return []yBand{{0, contentH}}
	}
	total := lp.HeightPt
	if total <= 0 {
		total = itemsExtent(lp)
	}
	if total <= contentH || contentH <= 0 {
		return []yBand{{0, maxFloat(total, contentH)}}
	}

	// Each item occupies a vertical [top,bottom] interval. A band cut at Y=c must not
	// fall strictly inside any item's interval (that would split a glyph/box across
	// two pages). Cut at the ideal target, then pull the cut UP above any item that
	// straddles it, so the whole item flows to the next band.
	intervals := itemIntervals(lp)

	var bands []yBand
	top := 0.0
	for top < total-1e-6 {
		target := top + contentH
		if target >= total {
			bands = append(bands, yBand{top, total})
			break
		}
		cut := straddleSafeCut(intervals, top, target, logf)
		bands = append(bands, yBand{top, cut})
		top = cut
	}
	if len(bands) == 0 {
		bands = append(bands, yBand{0, total})
	}
	return bands
}

// vinterval is one item's vertical extent in page-space points.
type vinterval struct{ top, bottom float64 }

// itemIntervals returns the vertical extent of every item on the page.
func itemIntervals(lp *layout.Page) []vinterval {
	out := make([]vinterval, 0, len(lp.Items))
	for i := range lp.Items {
		it := &lp.Items[i]
		switch it.Kind {
		case layout.GlyphKind:
			// A glyph's ink is above its baseline; approximate its top by the em box.
			out = append(out, vinterval{it.Glyph.YPt - it.Glyph.SizePt, it.Glyph.YPt})
		case layout.RuleKind, layout.BackgroundKind:
			out = append(out, vinterval{it.Rule.YPt, it.Rule.YPt + it.Rule.HPt})
		case layout.BorderKind:
			out = append(out, vinterval{it.Border.YPt, it.Border.YPt + it.Border.HPt})
		case layout.ImageKind:
			out = append(out, vinterval{it.Image.YPt, it.Image.YPt + it.Image.HPt})
		case layout.BackgroundImageKind:
			out = append(out, vinterval{it.BgImage.ClipY, it.BgImage.ClipY + it.BgImage.ClipH})
		}
	}
	return out
}

// straddleSafeCut returns a cut Y in (top, target]: the target pulled up above any
// item straddling it (top < item.top < target < item.bottom), so the item is not
// split. If an item taller than the band straddles even at top (it can't fit any
// band), the cut stays at target and the item overflows (clipped by the MediaBox),
// logged once.
func straddleSafeCut(intervals []vinterval, top, target float64, logf func(string, ...any)) float64 {
	cut := target
	for _, iv := range intervals {
		if iv.top < cut && iv.bottom > cut { // straddles the current cut
			if iv.top > top {
				cut = iv.top // pull the cut up to the item's top
			} else if logf != nil {
				logf("pdfwrite: content taller than the page height; it overflows the page")
			}
		}
	}
	if cut <= top {
		cut = target // no safe pull-up point; overflow this band
	}
	return cut
}

// itemsExtent returns the maximum Y extent of any item on the page (for a page with
// no declared height).
func itemsExtent(lp *layout.Page) float64 {
	max := 0.0
	for i := range lp.Items {
		it := &lp.Items[i]
		var y float64
		switch it.Kind {
		case layout.GlyphKind:
			y = it.Glyph.YPt
		case layout.RuleKind, layout.BackgroundKind:
			y = it.Rule.YPt + it.Rule.HPt
		case layout.BorderKind:
			y = it.Border.YPt + it.Border.HPt
		case layout.ImageKind:
			y = it.Image.YPt + it.Image.HPt
		case layout.BackgroundImageKind:
			y = it.BgImage.ClipY + it.BgImage.ClipH
		}
		if y > max {
			max = y
		}
	}
	return max
}

// embedImage encodes img as an RGB image XObject (Flate), adding an /SMask for alpha
// when present, and returns its reference.
func embedImage(w *writer, img image.Image) (Ref, error) {
	b := img.Bounds()
	wd, ht := b.Dx(), b.Dy()
	if wd <= 0 || ht <= 0 {
		return 0, fmt.Errorf("pdfwrite: empty image")
	}
	rgb := make([]byte, 0, wd*ht*3)
	alpha := make([]byte, 0, wd*ht)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			rgb = append(rgb, byte(r>>8), byte(g>>8), byte(bl>>8))
			a8 := byte(a >> 8)
			if a8 != 0xff {
				hasAlpha = true
			}
			alpha = append(alpha, a8)
		}
	}
	dict := Dict{
		"Type":             Name("XObject"),
		"Subtype":          Name("Image"),
		"Width":            Int(int64(wd)),
		"Height":           Int(int64(ht)),
		"ColorSpace":       Name("DeviceRGB"),
		"BitsPerComponent": Int(8),
	}
	if hasAlpha {
		smaskDict := Dict{
			"Type": Name("XObject"), "Subtype": Name("Image"),
			"Width": Int(int64(wd)), "Height": Int(int64(ht)),
			"ColorSpace": Name("DeviceGray"), "BitsPerComponent": Int(8),
		}
		smask := w.addStream(smaskDict, alpha)
		dict["SMask"] = smask
	}
	return w.addStream(dict, rgb), nil
}

// normalizeOpts clamps a negative margin to 0. It does NOT force a page size: an
// unset (<=0) Options.PageWidthPt/HeightPt means "use each layout page's own size"
// (see pdfPageSize), so an already-paginated document keeps its geometry. MarginPt is
// taken literally (0 = no margin); the public API applies its own 0.5in default.
func normalizeOpts(o Options) Options {
	if o.MarginPt < 0 {
		o.MarginPt = 0
	}
	return o
}

// pdfPageSize resolves the PDF page size for one layout page: an explicit
// Options.PageWidthPt/HeightPt wins per axis; otherwise the layout page's own size is
// used (falling back to US Letter, 612×792, only if the layout page has no size).
func pdfPageSize(lp *layout.Page, o Options) (w, h float64) {
	w, h = o.PageWidthPt, o.PageHeightPt
	if w <= 0 {
		w = lp.WidthPt
	}
	if h <= 0 {
		h = lp.HeightPt
	}
	if w <= 0 {
		w = 612
	}
	if h <= 0 {
		h = 792
	}
	return w, h
}

// fallbackSize is the page size for the synthesized blank page (no layout pages):
// explicit Options size, else US Letter.
func fallbackSize(o Options) (w, h float64) {
	w, h = o.PageWidthPt, o.PageHeightPt
	if w <= 0 {
		w = 612
	}
	if h <= 0 {
		h = 792
	}
	return w, h
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
