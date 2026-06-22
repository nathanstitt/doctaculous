package raster

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Device renders into an *image.RGBA, implementing render.Device. Paths arrive
// already in device space (origin top-left, y down). Nonzero fills use
// golang.org/x/image/vector; even-odd fills use a built-in scanline rasterizer
// (evenOddCoverage); strokes and glyphs are flattened to filled paths. Clipping
// is an alpha-mask intersection tracked on a small state stack.
//
// A Device is not safe for concurrent use; render one page per Device. Separate
// pages render on separate Devices, which is how the page-parallel path stays
// lock-free.
type Device struct {
	img  *image.RGBA
	clip []*image.Alpha // clip stack; top is the active mask (nil = unclipped)
	logf func(string, ...any)
}

// New returns a Device drawing onto img. The caller owns img and reads the
// result after interpretation completes.
func New(img *image.RGBA) *Device {
	return &Device{img: img, logf: func(string, ...any) {}}
}

// SetLogf installs a debug logger that receives messages about approximated or
// unsupported features (e.g. even-odd fills). Safe to call before rendering.
func (d *Device) SetLogf(logf func(string, ...any)) {
	if logf != nil {
		d.logf = logf
	}
}

// Size reports the bitmap dimensions.
func (d *Device) Size() (w, h int) {
	b := d.img.Bounds()
	return b.Dx(), b.Dy()
}

// Fill paints path's interior with paint.Color under the current clip.
func (d *Device) Fill(path *render.Path, paint render.FillPaint) {
	if path == nil || path.Empty() {
		return
	}
	mask := d.rasterizeMask(path, paint.Rule)
	if mask == nil {
		return
	}
	d.composite(mask, paint.Color)
}

// Stroke approximates a stroke by filling a thin quad along each segment. This
// is a first-pass stroker (butt caps, no joins/dashes); rasterx replaces it when
// stroke fidelity is needed. It keeps the common "thin line" case correct.
func (d *Device) Stroke(path *render.Path, paint render.StrokePaint) {
	if path == nil || path.Empty() {
		return
	}
	w := paint.Width
	if w <= 0 {
		w = 1 // PDF zero-width means "thinnest renderable line"
	}
	half := w / 2
	outline := &render.Path{}
	var cx, cy float64
	var have bool
	flush := func(x0, y0, x1, y1 float64) {
		dx, dy := x1-x0, y1-y0
		l := math.Hypot(dx, dy)
		if l == 0 {
			return
		}
		// Unit normal.
		nx, ny := -dy/l*half, dx/l*half
		outline.MoveTo(x0+nx, y0+ny)
		outline.LineTo(x1+nx, y1+ny)
		outline.LineTo(x1-nx, y1-ny)
		outline.LineTo(x0-nx, y0-ny)
		outline.Close()
	}
	for _, s := range path.Segments {
		switch s.Kind {
		case render.MoveTo:
			cx, cy, have = s.P0.X, s.P0.Y, true
		case render.LineTo:
			if have {
				flush(cx, cy, s.P0.X, s.P0.Y)
			}
			cx, cy = s.P0.X, s.P0.Y
		case render.CubeTo:
			// Flatten the cubic to short line segments.
			flattenCubic(cx, cy, s.P0, s.P1, s.P2, func(x, y float64) {
				flush(cx, cy, x, y)
				cx, cy = x, y
			})
		case render.Close:
			have = false
		}
	}
	mask := d.rasterizeMask(outline, render.NonZero)
	if mask == nil {
		return
	}
	d.composite(mask, paint.Color)
}

// FillGlyph fills a glyph outline (device space) with a solid color.
func (d *Device) FillGlyph(outline *render.Path, c render.FillColor) {
	if outline == nil || outline.Empty() {
		return
	}
	mask := d.rasterizeMask(outline, render.NonZero)
	if mask == nil {
		return
	}
	d.composite(mask, color.RGBA(c))
}

// DrawImage maps img's unit square through ctm into device space using inverse
// sampling (nearest neighbor), respecting the current clip.
func (d *Device) DrawImage(img image.Image, ctm render.Matrix) {
	if img == nil {
		return
	}
	inv, ok := invert(ctm)
	if !ok {
		return
	}
	// Device-space bounding box of the unit square's four corners.
	minX, minY, maxX, maxY := unitQuadBounds(ctm)
	b := d.img.Bounds()
	x0 := clampInt(int(math.Floor(minX)), b.Min.X, b.Max.X)
	y0 := clampInt(int(math.Floor(minY)), b.Min.Y, b.Max.Y)
	x1 := clampInt(int(math.Ceil(maxX)), b.Min.X, b.Max.X)
	y1 := clampInt(int(math.Ceil(maxY)), b.Min.Y, b.Max.Y)

	sb := img.Bounds()
	clip := d.activeClip()
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if clip != nil && clip.AlphaAt(x, y).A == 0 {
				continue
			}
			// Map pixel center to unit space, then to source pixels. PDF image
			// space has y up with the image's top row at v=1, so flip v.
			u, v := inv.Apply(float64(x)+0.5, float64(y)+0.5)
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			sx := sb.Min.X + int(u*float64(sb.Dx()))
			sy := sb.Min.Y + int((1-v)*float64(sb.Dy()))
			// Guard against float rounding landing on the exclusive max edge.
			sx = clampInt(sx, sb.Min.X, sb.Max.X-1)
			sy = clampInt(sy, sb.Min.Y, sb.Max.Y-1)
			d.img.Set(x, y, img.At(sx, sy))
		}
	}
}

// PushClip intersects the current clip with path. Clip masks are sub-rectangle
// sized (see rasterizeMask); a point outside a mask's bounds has zero coverage,
// which correctly means "clipped out".
func (d *Device) PushClip(path *render.Path, rule render.FillRule) {
	if path == nil || path.Empty() {
		return
	}
	next := d.rasterizeMask(path, rule)
	if next == nil {
		// Clip to nothing: an empty mask covering the (empty) intersection.
		next = image.NewAlpha(image.Rectangle{})
	}
	if cur := d.activeClip(); cur != nil {
		next = intersectClips(cur, next)
	}
	if len(d.clip) == 0 {
		d.clip = append(d.clip, next)
	} else {
		d.clip[len(d.clip)-1] = next
	}
}

// Save pushes the current clip onto the stack.
func (d *Device) Save() {
	d.clip = append(d.clip, d.activeClip())
}

// Restore pops the clip stack.
func (d *Device) Restore() {
	if len(d.clip) > 0 {
		d.clip = d.clip[:len(d.clip)-1]
	}
}

func (d *Device) activeClip() *image.Alpha {
	if len(d.clip) == 0 {
		return nil
	}
	return d.clip[len(d.clip)-1]
}

// rasterizeMask renders path into an alpha coverage mask. The mask is sized to
// the path's device-space bounding box clipped to the image bounds — not the
// whole image — so a page with thousands of small glyphs costs O(Σ glyph areas)
// rather than O(glyphs × image area). The returned mask's Bounds() reflect that
// sub-rectangle; callers (composite, intersect) iterate Bounds() and so stay
// bounded automatically. Returns nil if the path lies entirely off-canvas.
// evenOddSupersample is the number of subscanlines per pixel row the even-odd
// rasterizer averages for vertical anti-aliasing. 4 matches the visual quality of
// the vector backend's nonzero coverage closely enough for the golden tolerance.
const evenOddSupersample = 4

func (d *Device) rasterizeMask(path *render.Path, rule render.FillRule) *image.Alpha {
	bb := pathDeviceBounds(path).Intersect(d.img.Bounds())
	if bb.Empty() {
		return nil
	}
	// golang.org/x/image/vector only implements nonzero winding, so even-odd fills
	// go through our own scanline rasterizer (see evenOddCoverage). Nonzero stays
	// on the fast vector path.
	if rule == render.EvenOdd {
		return evenOddCoverage(flattenToPolygons(path), bb, evenOddSupersample)
	}
	r := vector.NewRasterizer(bb.Dx(), bb.Dy())
	replay(r, path, float32(bb.Min.X), float32(bb.Min.Y))
	mask := image.NewAlpha(bb)
	// Draw into the mask. The rasterizer's coordinate space starts at (0,0); the
	// mask's Bounds() start at bb.Min, so draw with that as the destination rect.
	r.Draw(mask, bb, image.Opaque, image.Point{})
	return mask
}

// composite blends src color through the coverage mask (and active clip) onto
// the image using source-over alpha.
func (d *Device) composite(mask *image.Alpha, c color.RGBA) {
	b := mask.Bounds()
	clip := d.activeClip()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cov := mask.AlphaAt(x, y).A
			if cov == 0 {
				continue
			}
			if clip != nil {
				cov = mulU8(cov, clip.AlphaAt(x, y).A)
				if cov == 0 {
					continue
				}
			}
			a := mulU8(c.A, cov)
			if a == 0 {
				continue
			}
			over(d.img, x, y, c, a)
		}
	}
}

// over blends a straight (non-premultiplied) source color c, at coverage-scaled
// alpha a, onto a premultiplied destination pixel (Go's *image.RGBA convention)
// using source-over. Callers must pass straight-alpha colors.
func over(img *image.RGBA, x, y int, c color.RGBA, a uint8) {
	dst := img.RGBAAt(x, y)
	ia := 255 - uint32(a)
	out := color.RGBA{
		R: uint8((uint32(c.R)*uint32(a) + uint32(dst.R)*ia) / 255),
		G: uint8((uint32(c.G)*uint32(a) + uint32(dst.G)*ia) / 255),
		B: uint8((uint32(c.B)*uint32(a) + uint32(dst.B)*ia) / 255),
		A: uint8(uint32(a) + uint32(dst.A)*ia/255),
	}
	img.SetRGBA(x, y, out)
}

// Fill, draw helpers ---------------------------------------------------------

// fillBackground paints the whole image a solid color (used for opaque page
// backgrounds before interpretation).
func fillBackground(img *image.RGBA, c color.Color) {
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
}

func mulU8(a, b uint8) uint8 { return uint8(uint32(a) * uint32(b) / 255) }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
