package svgwrite

import (
	"image"

	"github.com/nathanstitt/omnidoc/pkg/internal/raster"
	"github.com/nathanstitt/omnidoc/pkg/internal/render"
)

// These three Device methods all answer the same question — "what pixels does
// this paint call cover?" — which a markup writer cannot answer about itself.
// render.Device permits a vector backend to decline (pdfwrite returns nil from
// RenderOffscreen and approximates clip masks by bounding box) because a PDF
// has no spare raster surface. An SVG document does: it can embed a bitmap.
// So rather than degrade, this backend borrows the raster backend's rasterizer,
// which keeps ONE implementation as the source of truth for coverage across
// both backends and makes svgwrite strictly more faithful than pdfwrite here.
//
// Depending on pkg/render/raster is a sibling edge inside the backend layer,
// not a layering inversion: raster imports pkg/render, pkg/font and pkg/pdf,
// and no *write backend, so there is no cycle.

// scratch returns a raster Device over a fresh transparent surface w by h,
// sharing this Device's logger so degradations logged while painting offscreen
// content still surface.
func (d *Device) scratch(w, h int) (*raster.Device, *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rd := raster.New(img)
	rd.SetLogf(d.logf)
	return rd, img
}

// BuildClipMask rasterizes the union of paths into a single coverage mask.
//
// The union (not intersection) semantics, the per-path fill rules, and the
// "empty slice yields a non-nil mask covering nothing" rule all come from the
// raster backend's implementation, which is the same code the raster pipeline
// uses — so a clipPath clips identically in both backends.
func (d *Device) BuildClipMask(paths []render.MaskPath) render.GroupMask {
	w, h := d.wPx, d.hPx
	if w <= 0 || h <= 0 {
		// Still non-nil: nil means "no restriction" to EndGroup, which is the
		// opposite of what an unrenderable clip must do.
		return image.NewAlpha(image.Rectangle{})
	}
	rd, _ := d.scratch(w, h)
	return rd.BuildClipMask(paths)
}

// BuildLuminanceMask renders paint's content offscreen and converts it to a
// coverage mask (luminance times alpha, or alpha alone when alphaOnly).
func (d *Device) BuildLuminanceMask(size image.Point, alphaOnly bool, paint func(dev render.Device)) render.GroupMask {
	if paint == nil || size.X <= 0 || size.Y <= 0 {
		return image.NewAlpha(image.Rectangle{})
	}
	rd, _ := d.scratch(size.X, size.Y)
	return rd.BuildLuminanceMask(size, alphaOnly, paint)
}

// RenderOffscreen renders paint's content into an isolated transparent surface
// and hands back its pixels, so an SVG <filter> can read back what it filters.
//
// Unlike pdfwrite, which must return nil here and leave the caller to render
// unfiltered, this returns real pixels — filters work on SVG output.
func (d *Device) RenderOffscreen(size image.Point, paint func(dev render.Device)) *image.RGBA {
	if paint == nil || size.X <= 0 || size.Y <= 0 {
		return nil
	}
	rd, _ := d.scratch(size.X, size.Y)
	return rd.RenderOffscreen(size, paint)
}
