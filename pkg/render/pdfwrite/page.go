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
type Options struct {
	PageWidthPt  float64 // default US Letter width (612) if <= 0
	PageHeightPt float64 // default US Letter height (792) if <= 0
	MarginPt     float64 // uniform content margin; default 36 (0.5in) if zero, 0 if negative
	Title        string
	Workers      int // page-render worker cap; default GOMAXPROCS if <= 0
	Logf         func(string, ...any)
}

// band is a vertical slice [topPt, bottomPt) of a layout page placed on one PDF page.
type band struct {
	page     *layout.Page
	topPt    float64
	bottomPt float64
}

// renderedPage is one worker's pure output: the page's content bytes plus the
// per-page font embedder (faces + local resource names) and images it used. It
// carries NO shared object ids — those are assigned during the sequential merge, so
// workers share no writer state.
type renderedPage struct {
	index   int
	content []byte
	embed   *fontEmbedder
	images  []pendingImage
}

// WriteDocument fragments the laid-out pages into fixed-size PDF pages, renders them
// concurrently, then assembles the PDF sequentially and writes it to out. It honors
// context cancellation and recovers per page so one bad page cannot abort the
// document.
func WriteDocument(ctx context.Context, out io.Writer, pages *layout.Pages, opts Options) error {
	opts = withDefaults(opts)
	contentH := opts.PageHeightPt - 2*opts.MarginPt
	if contentH <= 0 {
		contentH = opts.PageHeightPt
	}

	// 1. Fragment every layout page into bands (cheap, sequential).
	var bands []band
	if pages != nil {
		for i := range pages.Pages {
			lp := &pages.Pages[i]
			for _, b := range fragment(lp, contentH, opts.Logf) {
				bands = append(bands, band{page: lp, topPt: b.topPt, bottomPt: b.bottomPt})
			}
		}
	}
	if len(bands) == 0 {
		// Emit a single blank page so the output is always a valid PDF.
		bands = append(bands, band{page: &layout.Page{WidthPt: opts.PageWidthPt, HeightPt: opts.PageHeightPt}, topPt: 0, bottomPt: contentH})
	}

	// 2. Render bands concurrently into pure renderedPage values (no shared state).
	rendered := renderBandsParallel(ctx, bands, opts)
	if err := ctx.Err(); err != nil {
		return err
	}

	// 3. Assemble sequentially: one writer, faces de-duped across pages.
	return assemble(out, rendered, opts)
}

// renderBandsParallel renders each band on a bounded worker pool, returning results
// in document order (results[i] corresponds to bands[i]); a band that panics is left
// with content nil and logged.
func renderBandsParallel(ctx context.Context, bands []band, opts Options) []renderedPage {
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
			results[idx] = renderBand(bands[idx], idx, opts)
		}(i)
	}
	wg.Wait()
	return results
}

// renderBand paints one band into its own pageDevice and returns a pure value. It
// touches no shared writer state, so it is safe to call from many goroutines. The
// band's content is painted at page space translated so the band's top maps to the
// content-area top; the PDF page MediaBox clips everything outside the band, so no
// per-item clipping is needed.
func renderBand(b band, index int, opts Options) renderedPage {
	dev := newPageDevice(opts.PageWidthPt, opts.PageHeightPt)
	dev.logf = opts.Logf
	mat := render.Translate(opts.MarginPt, opts.MarginPt-b.topPt)
	paint.PaintPage(dev, b.page, mat)
	return renderedPage{
		index:   index,
		content: dev.contentStream(),
		embed:   dev.embed,
		images:  dev.images,
	}
}

// assemble folds the rendered pages into a single PDF, de-duplicating fonts across
// pages (one embedded subset per face) and writing in document order.
func assemble(out io.Writer, rendered []renderedPage, opts Options) error {
	w := newWriter()
	pagesRef := w.alloc()

	// Merge all faces used anywhere, in deterministic order (page order, then each
	// page's first-use order), so each face is embedded ONCE. Reuse the merged
	// embedder's per-face glyph sets for emission.
	merged := newFontEmbedder()
	for _, rp := range rendered {
		if rp.embed == nil {
			continue
		}
		for _, face := range rp.embed.orderedFaces() {
			fu := rp.embed.uses[face]
			for _, gid := range fu.order {
				merged.use(face, gid, fu.runes[gid])
			}
		}
	}
	// Emit each unique face once; remember its top /Font ref.
	faceRef := map[*font.Face]Ref{}
	for _, face := range merged.orderedFaces() {
		if ref := merged.emit(w, face); ref != 0 {
			faceRef[face] = ref
		}
	}

	var kids []object
	for _, rp := range rendered {
		if rp.content == nil {
			continue // failed/skipped band
		}
		flip := fmt.Sprintf("1 0 0 -1 0 %s cm\n", formatReal(opts.PageHeightPt))
		contentRef := w.addStream(Dict{}, append([]byte(flip), rp.content...))

		// Per-page /Font maps each of THIS page's local resource names to the shared
		// font ref (the device emitted its local names during painting).
		fonts := Dict{}
		if rp.embed != nil {
			for _, face := range rp.embed.orderedFaces() {
				ref, ok := faceRef[face]
				if !ok {
					continue // non-embeddable face: its glyphs were drawn as outlines
				}
				fonts[rp.embed.resourceName(face)] = ref
			}
		}
		res := Dict{}
		if len(fonts) > 0 {
			res["Font"] = fonts
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
			"MediaBox":  Array{Int(0), Int(0), Real(opts.PageWidthPt), Real(opts.PageHeightPt)},
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

// withDefaults fills a zero page size with US Letter. MarginPt is taken literally
// (0 = no margin); the public API applies its own 0.5in default before calling, so
// the writer treats the value as final and a negative margin clamps to 0.
func withDefaults(o Options) Options {
	if o.PageWidthPt <= 0 {
		o.PageWidthPt = 612
	}
	if o.PageHeightPt <= 0 {
		o.PageHeightPt = 792
	}
	if o.MarginPt < 0 {
		o.MarginPt = 0
	}
	return o
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
