package pdfwrite

import (
	"bytes"
	"fmt"
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// pendingForm is a named Form XObject referenced by a page's content stream
// (either a transparency group produced by EndGroup, or a luminosity mask's
// content produced by BuildLuminanceMask), recorded so the document assembler
// can allocate it a real indirect object and register it in the page's
// /XObject resource sub-dictionary. This mirrors pendingImage's role for
// image XObjects and pendingShading's for /Shading dictionaries.
type pendingForm struct {
	name    string // resource name used in the content stream ("Fm0", "Fm1", ...)
	content []byte // the form's own (uncompressed) content stream
	bbox    [4]float64
	// colorSpace is the transparency group's /CS: DeviceRGB for an ordinary
	// group (EndGroup's Form), DeviceGray for a luminosity mask's content
	// (only the luminance/alpha of that result is ever read, per
	// render.Device.BuildLuminanceMask's contract, so a smaller color space
	// is both correct and cheaper).
	colorSpace string
	// images/shadings/extGStates/forms are the resources THIS form's own
	// content stream references (nested groups/masks can themselves contain
	// images, shadings, gradients, or further groups) — the same four
	// resource categories a page itself carries, needed here because a form
	// XObject does not inherit a page's /Resources and must declare its own
	// (ISO 32000-1 §8.10.2).
	images     []pendingImage
	shadings   []pendingShading
	extGStates []pendingExtGState
	forms      []pendingForm
}

// groupFrame is one entry on pageDevice's group stack: the content buffer,
// resource accumulators, and clip state that were active before BeginGroup,
// swapped back in by EndGroup. Mirrors raster.groupState's role for the
// bitmap backend, but a PDF page has four resource categories to isolate
// (images/shadings/extGStates/forms) instead of one pixel buffer.
type groupFrame struct {
	buf        bytes.Buffer
	images     []pendingImage
	shadings   []pendingShading
	extGStates []pendingExtGState
	forms      []pendingForm

	extGStateNames map[extGState]string

	// outerClip is the clip rectangle active when BeginGroup ran (nil =
	// unclipped) — the group's OWN clip, applied once at EndGroup by clipping
	// the "Do" that paints the finished form, mirroring how raster.Device
	// suspends the outer clip while painting into the scratch and re-applies
	// it exactly once at composite time (see raster/device.go's BeginGroup/
	// EndGroup doc comments) rather than letting it restrict the group's
	// interior twice.
	outerClip *clipBounds
	clipStack []*clipBounds
}

// BeginGroup starts an isolated transparency-group Form XObject: it swaps out
// the page's content buffer and per-group resource accumulators (images,
// shadings, ExtGStates, nested forms) onto a stack, so everything painted
// until the matching EndGroup accumulates into the group's OWN form instead
// of the page (or enclosing group) content stream. The clip active at this
// point becomes the group's outer clip (see groupFrame) and is suspended
// (reset to unclipped) for the group's own content — re-applied once, at
// EndGroup, exactly like raster's BeginGroup/EndGroup pair.
func (d *pageDevice) BeginGroup() {
	d.groupStack = append(d.groupStack, groupFrame{
		buf:            d.buf,
		images:         d.images,
		shadings:       d.shadings,
		extGStates:     d.extGStates,
		forms:          d.forms,
		extGStateNames: d.extGStateNames,
		outerClip:      d.clipRect,
		clipStack:      d.clipStack,
	})
	d.buf = bytes.Buffer{}
	d.images = nil
	d.shadings = nil
	d.extGStates = nil
	d.forms = nil
	d.extGStateNames = nil
	d.clipRect = nil
	d.clipStack = nil
}

// EndGroup closes the most recently opened BeginGroup: it registers the
// group's accumulated content as a transparency Form XObject (/Group <<
// /S /Transparency /CS /DeviceRGB /I true >>), pops the outer buffer/
// resources/clip back into place, then emits "q /GSn gs /Fmn Do Q" into the
// restored (outer) stream so the form paints once, through an ExtGState
// carrying the group's constant alpha, blend mode, and (when mask is
// non-nil) a luminosity soft mask. An unbalanced call (no matching
// BeginGroup) is a no-op, matching render.Device's documented contract.
//
// alpha<=0 still emits the form (so a caller reading the content stream back
// sees a structurally valid, balanced document) but with /ca 0, which PDF
// itself already treats as fully transparent — no special-case skip is
// needed the way raster.Device.EndGroup skips compositing entirely, since
// pdfwrite has no pixels to save work on.
func (d *pageDevice) EndGroup(alpha float64, blendMode string, mask render.GroupMask) {
	n := len(d.groupStack)
	if n == 0 {
		return // unbalanced EndGroup: degrade to a no-op, never panic
	}
	frame := d.groupStack[n-1]
	d.groupStack = d.groupStack[:n-1]

	// The group's BBox must be computed in the SAME raw, top-left/Y-down space
	// this device emits everything else in — the page-level Y-flip CTM
	// (assembled in page.go's assemble) applies OUTSIDE the form, so the form
	// itself must never flip. Using the clip that was active when BeginGroup
	// ran (conservatively; the exact painted extent is not tracked) keeps the
	// BBox tight when a clip was active and falls back to the full page box
	// (also raw, un-flipped device space) when it was not — either way this
	// is a bound on where content COULD have painted, never smaller than
	// what actually did, so nothing is clipped away.
	bb := pageBoxBounds(d.wPt, d.hPt)
	if frame.outerClip != nil {
		bb = intersectClipBounds(bb, frame.outerClip)
	}

	name := fmt.Sprintf("Fm%d", len(frame.forms))
	form := pendingForm{
		name:       name,
		content:    d.buf.Bytes(),
		bbox:       [4]float64{bb.minX, bb.minY, bb.maxX, bb.maxY},
		colorSpace: "DeviceRGB",
		images:     d.images,
		shadings:   d.shadings,
		extGStates: d.extGStates,
		forms:      d.forms,
	}

	// Restore the outer buffer/resources/clip BEFORE emitting the "Do" call,
	// so it lands in the right stream and the clip stack unwinds to exactly
	// what it was pre-BeginGroup (mirroring raster's clipBase reset).
	d.buf = frame.buf
	d.images = frame.images
	d.shadings = frame.shadings
	d.extGStates = frame.extGStates
	d.forms = append(frame.forms, form)
	d.extGStateNames = frame.extGStateNames
	d.clipRect = frame.outerClip
	d.clipStack = frame.clipStack

	g := extGState{hasFillAlpha: true, fillAlpha: clamp01(alpha), blendMode: blendMode}
	if mask != nil {
		g.smaskFormName = d.registerLuminosityMask(mask)
	}

	d.buf.WriteString("q\n")
	d.emitGState(g)
	// The outer clip is reapplied here (once), around the group's own "Do",
	// exactly like BuildClipMask's rectangular approximation is the ONLY
	// place clip state affects a group in this writer: PushClip calls made
	// AFTER this EndGroup are unaffected (they operate on the just-restored
	// outer clip stack), and the group's own interior painted unclipped by
	// its outer clip while BeginGroup had it suspended.
	fmt.Fprintf(&d.buf, "/%s Do\n", name)
	d.buf.WriteString("Q\n")
}

// registerLuminosityMask converts mask (an *image.Alpha built by
// BuildClipMask or BuildLuminanceMask) into a DeviceGray Form XObject and
// returns its resource name, ready to be set as extGState.smaskFormName.
//
// mask is a coverage BUFFER (a rasterized approximation for BuildClipMask, or
// this writer's own soft-mask form recorded via pendingSoftMask for
// BuildLuminanceMask — see softmask.go), not a live PDF object, by the time
// it reaches EndGroup: render.Device's contract hands EndGroup a
// backend-neutral GroupMask, and pdfwrite's own BuildLuminanceMask already
// built a real luminosity Form XObject for the common case (see
// softmask.go). When d.pendingSoftMaskFor(mask) recognizes the exact mask
// pointer it just built, it reuses that form directly instead of
// re-rasterizing — this is what keeps a native gradient-based mask (not just
// a flat rectangle) as real PDF vector/shading content instead of a baked
// raster fallback. Any other mask (e.g. BuildClipMask's rectangular
// approximation, or one produced by combineClipRegions/attenuateByMask/
// unionMasks combining two masks) falls back to emitting mask's own pixel
// buffer as a DeviceGray image-backed form.
func (d *pageDevice) registerLuminosityMask(mask render.GroupMask) string {
	if name, ok := d.takePendingSoftMask(mask); ok {
		return name
	}
	return d.registerRasterMaskForm(mask)
}

// registerRasterMaskForm bakes an *image.Alpha coverage buffer into a
// DeviceGray image XObject wrapped in a one-shot Form XObject, and returns
// the FORM's resource name (not the image's) so the caller can treat every
// mask uniformly as "a form to reference from /SMask /G". This is the
// fallback for a mask this writer cannot trace back to real vector/shading
// content (BuildClipMask's rectangular approximation, or the result of
// combining two masks via combineClipRegions/attenuateByMask/unionMasks in
// pkg/svg/draw) — still correct (the soft mask's rendered result matches the
// coverage buffer exactly), just not resolution-independent.
func (d *pageDevice) registerRasterMaskForm(mask render.GroupMask) string {
	b := mask.Bounds()
	w, h := b.Dx(), b.Dy()
	imgName := "Im0"
	var content bytes.Buffer
	var images []pendingImage
	if w > 0 && h > 0 {
		gray := image.NewGray(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				gray.SetGray(x, y, color.Gray{Y: mask.AlphaAt(b.Min.X+x, b.Min.Y+y).A})
			}
		}
		images = []pendingImage{{name: imgName, img: gray, ctm: render.Identity}}
		// Place the unit square exactly over the mask's own device-space
		// bounds (raw, un-flipped space — same reasoning as EndGroup's BBox).
		place := render.Scale(float64(w), float64(h)).Mul(render.Translate(float64(b.Min.X), float64(b.Min.Y)))
		fmt.Fprintf(&content, "%s %s %s %s %s %s cm\n",
			formatReal(place.A), formatReal(place.B), formatReal(place.C),
			formatReal(place.D), formatReal(place.E), formatReal(place.F))
		fmt.Fprintf(&content, "/%s Do\n", imgName)
	}
	name := fmt.Sprintf("Fm%d", len(d.forms))
	bb := pageBoxBounds(d.wPt, d.hPt)
	if w > 0 && h > 0 {
		bb = &clipBounds{float64(b.Min.X), float64(b.Min.Y), float64(b.Max.X), float64(b.Max.Y)}
	}
	d.forms = append(d.forms, pendingForm{
		name:       name,
		content:    content.Bytes(),
		bbox:       [4]float64{bb.minX, bb.minY, bb.maxX, bb.maxY},
		colorSpace: "DeviceGray",
		images:     images,
	})
	return name
}

// pageBoxBounds returns the device's full raw-space bounds (the conservative
// BBox fallback used when no clip was active — see EndGroup).
func pageBoxBounds(wPt, hPt float64) *clipBounds {
	return &clipBounds{0, 0, wPt, hPt}
}

// clamp01 constrains a to [0,1], guarding against a caller-supplied alpha
// outside the documented range (render.Device.EndGroup's contract already
// requires this, but a defensive clamp here keeps a malformed caller from
// producing an out-of-spec /ca value).
func clamp01(a float64) float64 {
	switch {
	case a < 0:
		return 0
	case a > 1:
		return 1
	default:
		return a
	}
}
