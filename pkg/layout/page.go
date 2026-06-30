package layout

import (
	"image"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Pages is the engine's output: a document laid out into discrete pages of
// positioned drawing primitives. It is read-only after Layout, so it can be
// shared across the per-page render fan-out without locks.
type Pages struct {
	Pages []Page
	// CanvasBackground is the page canvas fill propagated from the document's root
	// background (CSS background propagation: the <html> background, else the <body>
	// background). A zero (transparent) value means no propagation — the renderer
	// uses its own default (RasterOptions.Background, else white). It applies to
	// every page. Set by the CSS layout engine; left zero by DOCX.
	CanvasBackground color.RGBA
}

// Page is one laid-out page: its size in points plus the primitives to draw,
// already positioned in page space (points, Y-down, origin at the top-left
// corner of the page). The paint stage maps page space to device pixels with a
// single matrix.
type Page struct {
	WidthPt, HeightPt float64
	Items             []Item
}

// BorderStyle is a border edge's line style.
type BorderStyle int

const (
	// BorderNone draws no border.
	BorderNone BorderStyle = iota
	// BorderSolid is a single continuous line filling the whole edge strip.
	BorderSolid
	// BorderDashed is a run of filled dashes along the edge with gaps between.
	BorderDashed
	// BorderDotted is a run of square dots along the edge with gaps between.
	BorderDotted
	// BorderDouble is two parallel lines (the outer and inner thirds of the strip)
	// with an empty gap between them.
	BorderDouble
	// BorderOutset is a 3D "raised" edge: the top/left edges paint a lightened color
	// and the bottom/right edges a darkened color, so the box appears to pop out.
	BorderOutset
	// BorderInset is a 3D "sunken" edge: the inverse of outset (top/left darkened,
	// bottom/right lightened), so the box appears pressed in.
	BorderInset
	// BorderRidge is a 3D ridge: the strip is split across its thickness into an outer
	// and inner half, painted as outset then inset, so it appears raised from the
	// surface on both sides of the edge.
	BorderRidge
	// BorderGroove is a 3D groove: the inverse of ridge (inset outer half, outset
	// inner half), so it appears carved into the surface.
	BorderGroove
)

// ObjectFit is how a replaced element's image is fitted into its content box,
// mirroring the CSS object-fit property. It is format-neutral (like BorderStyle):
// the layout engine maps the CSS keyword onto it and the painter honors it.
type ObjectFit int

const (
	// FitFill stretches the image to fill the content box exactly, ignoring the
	// intrinsic aspect ratio (the CSS initial value).
	FitFill ObjectFit = iota
	// FitContain scales the image to the largest size that fits inside the content
	// box while preserving aspect ratio, centered (letterboxed).
	FitContain
	// FitCover scales the image to the smallest size that covers the content box
	// while preserving aspect ratio, centered; the overflow is clipped to the box.
	FitCover
	// FitNone uses the image's intrinsic size, centered in the content box and
	// clipped to it.
	FitNone
	// FitScaleDown uses whichever of FitNone or FitContain yields the smaller image
	// (intrinsic size unless it overflows, then contained).
	FitScaleDown
)

// EdgeSide identifies which side of a box a border edge is on. It also tells the
// painter whether a dashed/dotted run steps along X (top/bottom) or Y (left/right)
// and which axis carries the edge's thickness.
type EdgeSide int

const (
	// EdgeTop is the top edge (horizontal strip; thickness along Y).
	EdgeTop EdgeSide = iota
	// EdgeRight is the right edge (vertical strip; thickness along X).
	EdgeRight
	// EdgeBottom is the bottom edge (horizontal strip; thickness along Y).
	EdgeBottom
	// EdgeLeft is the left edge (vertical strip; thickness along X).
	EdgeLeft
)

// ItemKind discriminates the Item union.
type ItemKind int

const (
	// GlyphKind is a single positioned glyph (Item.Glyph is set).
	GlyphKind ItemKind = iota
	// RuleKind is a filled rectangle — underlines and borders (Item.Rule is set).
	RuleKind
	// BackgroundKind is a filled rectangle behind content (Item.Rule is set); it is
	// painted exactly like a rule.
	BackgroundKind
	// BorderKind is one styled border edge (Item.Border is set).
	BorderKind
	// ImageKind is a decoded raster image drawn into a content box (Item.Image is
	// set), e.g. an <img> replaced element.
	ImageKind
	// BackgroundImageKind is a CSS background image painted behind a box's content
	// (Item.BgImage is set): positioned, optionally tiled, and clipped to the
	// background-clip box. Distinct from ImageKind, which has content-box/object-fit
	// semantics; a background has its own origin/clip/position/repeat model.
	BackgroundImageKind
	// ClipPushKind pushes a clip rectangle (Item.Rule carries the rect; Color is
	// unused). The painter saves the clip state and intersects the active clip with
	// the rect, so subsequent items paint clipped until the matching ClipPopKind.
	// Emitted by the CSS layout engine for an overflow≠visible box (its padding box).
	// Not a drawing primitive: it carries no color. Pushes and pops are balanced by
	// construction (every push has a matching pop from the same AppendItems call).
	ClipPushKind
	// ClipPopKind pops the most recent clip pushed by ClipPushKind (the painter
	// restores the prior clip state). Carries no geometry.
	ClipPopKind
)

// Item is one drawing primitive on a page. It is a small tagged union rather than
// an interface so a page's items live in one contiguous slice.
type Item struct {
	Kind    ItemKind
	Glyph   GlyphItem
	Rule    RuleItem
	Border  BorderItem
	Image   ImageItem
	BgImage BackgroundImageItem
}

// GlyphItem is a glyph to fill. The outline is kept in raw em units (Y up, as the
// font face returns it); the paint stage composes one matrix per glyph from
// SizePt, the baseline origin, and the page matrix, mirroring the PDF text path.
type GlyphItem struct {
	Outline  *render.Path // em units, Y up; nil for whitespace
	XPt, YPt float64      // pen origin on the baseline, in page space
	SizePt   float64      // em scale in points
	Color    color.RGBA
}

// RuleItem is an axis-aligned filled rectangle in page space (points, Y-down). It
// backs both RuleKind (underlines, and solid borders the engine flattens to rules)
// and BackgroundKind (a fill drawn behind a box's content).
type RuleItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA
}

// BorderItem is one border edge: the edge's own rectangle (the strip) in page
// space (points, Y-down), its color, line style, and which side it is. Side gives
// the strip's orientation so the painter knows the thickness axis and, for
// dashed/dotted styles, the length axis to step along.
type BorderItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA
	Style              BorderStyle
	Side               EdgeSide
}

// ImageItem is a decoded raster image to draw into a content box. The rectangle
// (XPt,YPt,WPt,HPt) is the box's content box in page space (points, Y-down,
// top-left origin); Fit selects how the image's intrinsic pixels map into that box
// (object-fit). The painter composes the unit-square→content-box mapping with the
// page→device matrix and calls render.Device.DrawImage. A nil Img draws nothing.
type ImageItem struct {
	Img                image.Image
	XPt, YPt, WPt, HPt float64
	Fit                ObjectFit
	// PosX, PosY are the object-position as fractions of the free space in the content
	// box (0 = left/top edge, 1 = right/bottom edge, 0.5 = centered — the default).
	// They shift the fitted image within the content box for fits that leave free space
	// (contain / none / scale-down); fill ignores them.
	PosX, PosY float64
}

// BgSizeKind selects how a CSS background image's painted (tile) size is computed. It
// is the layout-local mirror of the css BackgroundSizeKind (layout must not import
// css), mapped when the item is built.
type BgSizeKind int

const (
	// BgSizeAuto: each axis is the image's intrinsic size (a single explicit axis
	// scales the other to preserve the intrinsic ratio).
	BgSizeAuto BgSizeKind = iota
	// BgSizeCover: scale (preserving ratio) to cover the origin box.
	BgSizeCover
	// BgSizeContain: scale (preserving ratio) to fit inside the origin box.
	BgSizeContain
	// BgSizeExplicit: each axis from the resolved W/H below (≤0 = auto for that axis).
	BgSizeExplicit
)

// BackgroundImageItem is a CSS background image to paint behind a box's content (CSS
// Backgrounds 3). All rects are page space (points, Y-down, top-left origin). The
// painter computes the painted tile size from SizeKind + SizeW/SizeH and the intrinsic
// size, places the first tile from the position within the origin box, tiles along
// RepeatX/RepeatY, and clips every tile to the clip box. A nil Img draws nothing.
type BackgroundImageItem struct {
	Img                    image.Image
	IntrinsicW, IntrinsicH float64 // decoded pixel size, > 0

	// Origin box: where the image is sized and positioned (background-origin box).
	OriginX, OriginY, OriginW, OriginH float64
	// Clip box: the paint area every tile is confined to (background-clip box).
	ClipX, ClipY, ClipW, ClipH float64

	SizeKind     BgSizeKind
	SizeW, SizeH float64 // for BgSizeExplicit: resolved px per axis (≤0 = auto)

	// Background-position per axis: Pos*Frac is the percentage as a fraction [0,1]
	// (resolved against origin − tile size) when Pos*IsPct; otherwise Pos*Px is an
	// absolute offset (px) from the origin box's leading edge.
	PosXFrac, PosYFrac   float64
	PosXPx, PosYPx       float64
	PosXIsPct, PosYIsPct bool

	RepeatX, RepeatY bool
}
