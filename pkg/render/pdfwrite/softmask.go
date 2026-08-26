package pdfwrite

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// opaqueAlpha is full coverage (255), used by BuildClipMask's rectangular
// approximation.
var opaqueAlpha = color.Alpha{A: 255}

// intFloor and intCeil round a float64 device coordinate down/up to an int,
// matching the rounding FillShading's rasterizeShading already uses for the
// same purpose (a coverage/clip bounding box must never be smaller than the
// true geometric extent it approximates).
func intFloor(v float64) int { return int(math.Floor(v)) }
func intCeil(v float64) int  { return int(math.Ceil(v)) }

// BuildLuminanceMask renders paint's content into a SCRATCH pageDevice (a
// second, independent Form XObject in DeviceGray), so an SVG <mask> (or the
// alpha half of a gradient — see the shading-alpha lift in shading.go)
// becomes a real PDF luminosity soft mask (/SMask << /S /Luminosity /G <form>
// /BC [0] >>) rather than a baked raster fallback.
//
// render.Device's contract hands back a Device for paint to draw into and a
// GroupMask (*image.Alpha) for the caller (EndGroup) to apply; this writer
// has no pixel buffer to convert, so it takes a different route: it records
// the finished form's resource name in d.pendingSoftMasks, keyed by a
// SENTINEL *image.Alpha it allocates and returns as the GroupMask. EndGroup's
// registerLuminosityMask recognizes that exact pointer (see
// takePendingSoftMask) and wires the real form into /SMask directly — the
// sentinel's own pixels are never read, only its identity. This is the
// "widen the mask representation minimally" the design doc calls for:
// render.GroupMask itself (an *image.Alpha alias) is untouched, so raster and
// every other Device implementation are unaffected; only pdfwrite attaches
// extra, backend-private meaning to the pointer it hands back.
//
// alphaOnly selects mask-type exactly like the raster backend: the scratch
// device paints color as usual, and /Luminosity vs /Alpha is a property of
// the SOFT MASK dictionary in PDF (the /S entry — see buildSMaskDict), not
// something this method needs to compute per-pixel itself. When alphaOnly is
// true this writer still emits /S /Luminosity: PDF's SMask model has no
// native "alpha" mode (only /Luminosity and /Alpha, where /Alpha reads the
// GROUP's own alpha channel rather than any color it painted) — reusing the
// group's alpha channel directly is the closer match, so alphaOnly instead
// switches the transparency group's /CS in this case (handled by
// registerScratchForm) to make the scratch's own alpha the read channel; see
// that function's doc comment.
//
// A nil paint or non-positive size degrades to "no masking" (nil), matching
// the documented graceful-degradation contract: never invoke paint with a
// nil Device, never panic.
func (d *pageDevice) BuildLuminanceMask(size image.Point, alphaOnly bool, paint func(dev render.Device)) render.GroupMask {
	if paint == nil || size.X <= 0 || size.Y <= 0 {
		return nil
	}
	scratch := newPageDeviceWithEmbedder(float64(size.X), float64(size.Y), d.embed)
	scratch.logf = d.logf
	paint(scratch)

	name := d.registerScratchForm(scratch, alphaOnly)
	sentinel := &image.Alpha{Pix: []uint8{0xFF}, Stride: 1, Rect: image.Rect(0, 0, 1, 1)}
	if d.pendingSoftMasks == nil {
		d.pendingSoftMasks = map[*image.Alpha]string{}
	}
	d.pendingSoftMasks[sentinel] = name
	return sentinel
}

// registerScratchForm folds a scratch pageDevice's painted content (and
// whatever resources it accumulated: images, shadings, ExtGStates, nested
// forms) into a new Form XObject, returning its resource name.
//
// THE SCOPING TRAP this guards against: pkg/svg/draw always calls
// dev.BeginGroup() BEFORE building a group's mask (see draw.go's Group case
// and paintShapeGrouped — BeginGroup, paint children, THEN buildClipMask/
// buildMask, THEN EndGroup). By the time this method runs, d.forms is
// therefore the group's OWN, still-open forms accumulator (BeginGroup
// already swapped it in) — appending the mask form there would nest it
// INSIDE the group's own /Resources, but the "/GSn gs /Fmn Do" that actually
// references the mask (via the ExtGState's /SMask /G) is emitted by the
// MATCHING EndGroup into the OUTER buffer, after EndGroup restores d.forms
// back to the pre-BeginGroup slice. A mask form appended to the (about to be
// discarded into the group's own pendingForm.forms) current d.forms would be
// orphaned there — never referenced by the group's own content stream, and
// invisible to the outer scope that actually needs to resolve /Fmn.
//
// The fix: register the mask form into whatever forms slice will be ACTIVE
// once the next EndGroup restores it — the top of d.groupStack when one is
// open (frame.forms, not d.forms), or d.forms directly at the top level
// (mask/clip on a shape with no enclosing group besides its own, or a mask
// built for the page's own top-level content).
//
// alphaOnly selects the transparency group's /CS: DeviceGray (the default,
// mask-type=luminance — PDF reads /Luminosity from the group's rendered
// color, matching this writer's DeviceRGB main content converted to gray by
// the group's own color space per ISO 32000-1 §11.5.3) for the common case,
// or DeviceRGB CARRYING alpha so a /S /Alpha soft mask (mask-type=alpha) can
// read the group's own alpha channel directly instead of any color — PDF's
// /S /Alpha mode reads alpha regardless of /CS, so DeviceRGB here just avoids
// forcing an alpha-bearing paint through a gray conversion that would lose
// the very channel /Alpha needs to read cleanly if the writer ever populates
// per-pixel alpha (today every paint call in this writer is fully opaque
// unless /ca is set, so in practice both modes look the same on the group's
// own alpha channel; keeping them distinct here documents the intent instead
// of silently aliasing).
func (d *pageDevice) registerScratchForm(scratch *pageDevice, alphaOnly bool) string {
	cs := "DeviceGray"
	if alphaOnly {
		cs = "DeviceRGB"
	}
	forms := &d.forms
	if n := len(d.groupStack); n > 0 {
		forms = &d.groupStack[n-1].forms
	}
	name := fmt.Sprintf("Fm%d", len(*forms))
	*forms = append(*forms, pendingForm{
		name:       name,
		content:    scratch.contentStream(),
		bbox:       [4]float64{0, 0, scratch.wPt, scratch.hPt},
		colorSpace: cs,
		images:     scratch.images,
		shadings:   scratch.shadings,
		extGStates: scratch.extGStates,
		forms:      scratch.forms,
	})
	return name
}

// takePendingSoftMask reports whether mask is the exact sentinel pointer a
// prior BuildLuminanceMask call on d returned, and if so the form name it
// stands for (removing it from the pending map — a sentinel is single-use,
// consumed by the very next EndGroup that receives it, mirroring how
// pkg/svg/draw always calls BuildLuminanceMask immediately before the
// EndGroup that consumes its result).
//
// SILENT FAILURE MODE, DOCUMENTED HERE SO IT CANNOT SNEAK BACK IN: this
// lookup only succeeds when EndGroup receives the EXACT sentinel pointer
// unmodified. A caller that combines the sentinel with another mask first
// (e.g. pkg/svg/draw's attenuateByMask/combineClipRegions, when a clip-path
// and a <mask> both apply to the same element) produces a NEW *image.Alpha
// that is not in d.pendingSoftMasks — this lookup then misses (returns
// false, not an error) and registerLuminosityMask falls back to
// registerRasterMaskForm, which calls AlphaAt on a value that, if one
// operand was this sentinel, is a 1x1-pixel opaque stand-in rather than the
// real mask content: it reads as fully-transparent everywhere outside that
// single pixel, silently producing a wrong (near-empty) mask instead of an
// error. If a future change defers an EndGroup, reorders it relative to its
// BuildLuminanceMask call, or combines a pdfwrite sentinel with another
// GroupMask before EndGroup sees it, this will fail exactly this way:
// silently, not loudly. There is no cheap guard against it here (a sentinel
// is indistinguishable from a real 1x1 mask by type), so any code path that
// might do this needs to route the two masks to PDF as separate composited
// groups instead of combining them client-side — see the draw-side callers
// of BuildLuminanceMask for the current invariant this relies on.
func (d *pageDevice) takePendingSoftMask(mask render.GroupMask) (string, bool) {
	if d.pendingSoftMasks == nil {
		return "", false
	}
	name, ok := d.pendingSoftMasks[mask]
	if ok {
		delete(d.pendingSoftMasks, mask)
	}
	return name, ok
}

// BuildClipMask is a documented APPROXIMATION: this writer has no offscreen
// raster surface to build an exact per-pixel union mask into (see
// render.Device.BuildClipMask's doc comment on why the exact union requires
// rasterizing), so it computes a faithful rectangular approximation instead
// (the union of each child path's device-space bounding box, as a
// fully-opaque mask over that combined rectangle). Once EndGroup applies a
// GroupMask (see group.go), this gives clip-path a reasonable non-exact
// result in PDF output: a rectangular stand-in rather than the precise
// per-child-rule union raster achieves, but still real clipping rather than
// none at all. This is exactly decision (c) from the design doc — geometry
// concatenation with a rectangular stand-in for the rule — used ONLY here,
// where no exact backend exists yet; the raster backend never takes this
// shortcut.
func (d *pageDevice) BuildClipMask(paths []render.MaskPath) render.GroupMask {
	var union *clipBounds
	for _, mp := range paths {
		if mp.Path == nil || mp.Path.Empty() {
			continue
		}
		minX, minY, maxX, maxY, ok := mp.Path.Bounds()
		if !ok {
			continue
		}
		union = unionClipBounds(union, &clipBounds{minX, minY, maxX, maxY})
	}
	if union == nil {
		return image.NewAlpha(image.Rectangle{})
	}
	r := image.Rect(intFloor(union.minX), intFloor(union.minY), intCeil(union.maxX), intCeil(union.maxY))
	mask := image.NewAlpha(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			mask.SetAlpha(x, y, opaqueAlpha)
		}
	}
	return mask
}
