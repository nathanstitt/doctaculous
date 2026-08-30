package layout

import (
	"image"
	"image/color"
	"math"

	"github.com/nathanstitt/omnidoc/pkg/filtereffects"
	"github.com/nathanstitt/omnidoc/pkg/font"
	"github.com/nathanstitt/omnidoc/pkg/render"
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
	// VectorKind is a vector scene (SVG) drawn into a content box (Item.Vector is
	// set).
	VectorKind
	// FilterPushKind opens an isolated offscreen group for a CSS `filter` box
	// (Item.Filter is set): the painter calls render.Device.BeginGroup, so every
	// item up to the matching FilterPopKind composites into that group instead of
	// onto the page. Not a drawing primitive: it carries no color.
	//
	// Emitted by the CSS layout engine around a filtered box's own painted subtree
	// (its background, border, and contents). Item.Filter carries the parsed
	// function list and the box's border-box rectangle in page space, which the
	// pop needs in order to run the chain over the group's pixels.
	//
	// Pushes and pops are balanced by construction: the pair is emitted by a single
	// AppendItems call over one fragment, and PAGINATION SPLITS FRAGMENTS, NOT
	// ITEMS — each page flattens its own fragment subtree — so a box split across a
	// page break yields a separate, balanced pair on each page. That makes the
	// brackets page-local: exact for a per-pixel filter, and a documented
	// approximation for a spatial one (a blur cannot sample the pixels that fell on
	// the other page), logged once by the paginator.
	// TransformPushKind opens a CSS `transform` bracket (Item.Transform is set): the
	// painter multiplies the item's matrix into the page matrix, so every item up to
	// the matching TransformPopKind paints through it. Not a drawing primitive.
	//
	// A transform is a PAINT-TIME effect: it does not change layout, and the box keeps
	// the space it occupied untransformed (CSS Transforms 1 §3). Emitting it as a
	// bracket over the box's already-flattened subtree is what gives that for free —
	// the same shape the filter bracket uses, and balanced for the same reason
	// (pagination splits fragments, not items).
	TransformPushKind
	// TransformPopKind closes the most recent TransformPushKind, restoring the page
	// matrix. Carries no geometry; the matrix lives on the matching push.
	TransformPopKind
	FilterPushKind
	// FilterPopKind closes the most recent group opened by FilterPushKind: the
	// painter runs the pushed filter chain over the group's pixels and composites
	// the result (render.Device.EndGroup). Carries no geometry of its own — the
	// geometry and the chain live on the matching push.
	FilterPopKind
	// ShadowKind is one CSS `box-shadow` (Item.Shadow is set). It is a single
	// item per shadow rather than a bracket, because a box-shadow is not a
	// filter over the box's pixels: it is a fill of the box's own SHAPE, offset
	// and blurred, which the painter can derive from the geometry alone without
	// ever rasterizing the box.
	//
	// An OUTER shadow is emitted BEFORE the box's background (it paints behind
	// everything the box draws); an INSET shadow is emitted between the
	// background image and the border (it paints over the background but under
	// the border). See pkg/layout/css's appendSelfDecorations.
	ShadowKind
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
	// Vector is set when Kind is VectorKind: a vector scene (SVG) drawn into a
	// content box.
	Vector VectorItem
	// Transform is set when Kind is TransformPushKind: the box's resolved matrix,
	// already composed with its transform-origin, in page space.
	Transform render.Matrix

	// Filter is set when Kind is FilterPushKind: the CSS filter chain to apply to
	// the bracketed subtree, plus the box's border-box rectangle. Unused (zero) for
	// FilterPopKind, which carries no geometry.
	Filter FilterItem
	// Shadow is set when Kind is ShadowKind: one CSS box-shadow.
	Shadow ShadowItem
}

// ShadowItem is one CSS `box-shadow` (CSS Backgrounds 3 §6), fully resolved to
// page space (points, Y-down, top-left origin) at layout time.
//
// XPt,YPt,WPt,HPt is the SHADOW BOX — the rectangle whose shape the shadow
// takes. For an OUTER shadow that is the box's BORDER box; for an INSET shadow
// it is the box's PADDING box. The distinction is made at layout time because
// only the layout engine knows the box's border widths, and carrying the border
// box plus the edges here would make the painter re-derive geometry it has no
// other use for.
//
// OffsetX/OffsetY, Blur and Spread are the shadow's own parameters in points,
// already resolved from their authored units. Blur is non-negative (a negative
// blur invalidates the declaration at parse time); Spread may be negative,
// which shrinks the shadow.
type ShadowItem struct {
	XPt, YPt, WPt, HPt float64
	OffsetX, OffsetY   float64
	Blur               float64
	Spread             float64
	Color              color.RGBA
	// Inset selects the inner-shadow rendering: the shadow fills the region of
	// the padding box that the offset/spread/blurred shape does NOT cover, i.e.
	// the complement of the outer case, clipped to the padding box. It is not a
	// sign flip — see pkg/layout/paint's paintShadow.
	Inset bool
}

// ShadowSigma is the Gaussian standard deviation a blur radius of blurPt
// corresponds to.
//
// CSS Backgrounds 3 §6 defines the blur radius as the DISTANCE the shadow's
// edge transitions over, centred on the edge: half the radius outside the
// shape, half inside. A Gaussian whose transition spans a distance d has
// sigma = d/2, which is the factor every browser uses (Blink, WebKit and Gecko
// all halve the CSS radius before handing it to their blur).
//
// Getting this wrong is not subtle in the numbers but IS subtle on screen: a
// sigma of blurPt would spread the shadow twice as far as authored, which reads
// as "our shadows are soft" rather than as an off-by-two.
func ShadowSigma(blurPt float64) float64 { return blurPt / 2 }

// FilterItem is a CSS `filter` bracket's payload, carried on the FilterPushKind
// item that opens the group. Funcs is the already-parsed function list (see
// pkg/filtereffects) in source order, applied left to right; an empty list means
// no filter and the painter treats the bracket as a plain pass-through group.
//
// XPt,YPt,WPt,HPt is the filtered box's BORDER box in page space (points, Y-down,
// top-left origin) — the CSS filter region, which unlike SVG's has no
// filterUnits/x/y/width/height of its own. The painter needs it to size the
// offscreen surface the chain runs over.
type FilterItem struct {
	Funcs []filtereffects.Function

	// ShadowColors carries drop-shadow()'s already-resolved colour, one entry per
	// Funcs entry (positionally aligned; the entry is unused for every other
	// function kind). A shorter or nil slice means opaque black for the entries
	// it does not cover.
	//
	// The colour is resolved at LAYOUT time rather than at paint time because
	// drop-shadow()'s colour argument is optional and, when omitted, means the
	// element's own `color` property — which only the cascade knows. Carrying the
	// raw token here instead would force the painter to grow a CSS colour parser
	// and a way to reach the box's computed style, neither of which it has.
	ShadowColors []color.RGBA

	XPt, YPt, WPt, HPt float64
}

// Spatial reports whether the chain contains a function whose output at a pixel
// depends on NEIGHBOURING pixels — blur and drop-shadow. Only such a function is
// affected by the page-local bracketing model (it cannot sample content that fell
// on the other side of a page break), so the paginator logs its approximation for
// exactly these and stays silent for the per-pixel colour adjustments, where
// splitting is exact.
func (fi *FilterItem) Spatial() bool {
	for _, f := range fi.Funcs {
		if f.Kind == filtereffects.FuncBlur || f.Kind == filtereffects.FuncDropShadow {
			return true
		}
	}
	return false
}

// GlyphItem is a glyph to fill. The outline is kept in raw em units (Y up, as the
// font face returns it); the paint stage composes one matrix per glyph from
// SizePt, the baseline origin, and the page matrix, mirroring the PDF text path.
type GlyphItem struct {
	Outline  *render.Path // em units, Y up; nil for whitespace
	XPt, YPt float64      // pen origin on the baseline, in page space
	SizePt   float64      // em scale in points
	Color    color.RGBA

	// Face, GID, and Runes carry font identity for text-emitting backends (the PDF
	// writer). Face is nil when the glyph has no identity (whitespace, or a caller
	// that predates the seam), in which case paint falls back to filling Outline.
	Face  *font.Face
	GID   uint16
	Runes []rune

	// Rotate turns the glyph clockwise about its OWN pen origin, in radians. It carries
	// CSS text-orientation: a Latin glyph in a vertical line under the initial `mixed`
	// value is rotated a quarter turn so the run reads down the page, while an upright
	// CJK glyph on the same line is not.
	//
	// Zero (the default) is skipped outright rather than multiplied through, so every
	// glyph emitted before this field existed paints byte-identically.
	//
	// It rides on the glyph rather than on a TransformPush/Pop bracket around the run,
	// which is the other seam that could carry it. Two reasons: the rotation is about
	// each glyph's OWN origin, so a shared bracket would need a per-glyph translation
	// anyway and would not actually amortize; and the bracket costs two display-list
	// items plus a recursive paint call per push, which under `mixed` — the initial
	// value, so the common case for CJK — would land on the hottest path in painting.
	Rotate float64
}

// RuleItem is an axis-aligned filled rectangle in page space (points, Y-down). It
// backs both RuleKind (underlines, and solid borders the engine flattens to rules)
// and BackgroundKind (a fill drawn behind a box's content).
type RuleItem struct {
	XPt, YPt, WPt, HPt float64
	Color              color.RGBA

	// Radii rounds the rectangle's corners (CSS border-radius), already resolved to
	// absolute points and already overlap-corrected for this rectangle. The zero
	// value — every corner square — is the common case and makes the painter emit
	// exactly the four-line path it emitted before radii existed, which is what
	// keeps untouched output byte-identical.
	//
	// It rides on RuleItem rather than only on a clip bracket because a rounded
	// BACKGROUND is a filled rounded rect, not a square fill behind a clip: filling
	// the shape directly antialiases its own edge, whereas clipping a square fill
	// would hard-cut it against the clip mask and leave the corners visibly
	// staircased on the raster backend.
	//
	// It is unused for RuleKind (underlines/strikes are never rounded); only the
	// BackgroundKind and ClipPushKind carriers set it.
	Radii CornerRadii
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

	// Ring, when non-nil, makes this item the box's WHOLE rounded border ring
	// rather than one flat edge strip: a rounded border cannot be drawn as four
	// independent rectangles, because each corner's ink is shared between two
	// adjacent edges and follows an arc neither strip contains.
	//
	// A box with any rounded corner therefore emits ONE BorderKind item carrying
	// Ring (with XPt..HPt spanning the whole border box and Side/Style/Color taken
	// from the top edge) in place of the up-to-four strip items. A box with square
	// corners keeps emitting strips and leaves Ring nil, so that path — every
	// existing document — is untouched.
	Ring *BorderRing
}

// BorderRing is a rounded border drawn as a single ring: the area between the
// outer (border-box) rounded rectangle and the inner (padding-box) one, filled with
// the even-odd rule so the interior falls out as a hole.
//
// Filling the ring is what makes the corners correct. Stroking the outer path with
// the border width would keep a CONSTANT thickness around the arc and centre the
// ink on the path (half of it spilling outside the border box); the true shape has
// the outer and inner curves at DIFFERENT radii — the inner one reduced by the
// border width and floored at zero (see CornerRadii.Inset) — which only a two-path
// fill reproduces.
//
// Per-side colours and non-solid styles are a documented degradation: the ring is
// filled with one colour and treated as solid. A box whose rounded sides disagree
// in colour or style paints with the first present side's, and the CSS engine logs
// it. Painting them properly needs each side's ink clipped to its own mitre wedge —
// deferred; see docs/CSS-LAYOUT.md.
type BorderRing struct {
	// Outer is the border box's already-corrected radii; Inner is the padding
	// box's, normally Outer.Inset(widths) but carried explicitly so the painter
	// never has to re-derive it.
	Outer, Inner CornerRadii
	// Top/Right/Bottom/Left are the four border widths in points. The inner
	// rectangle is the outer one deflated by these.
	Top, Right, Bottom, Left float64
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
//
// The source may be a raster image (Img) or a vector scene (Scene) — the geometry
// model is identical either way, so only the final draw call differs.
type BackgroundImageItem struct {
	Img image.Image
	// Scene is set INSTEAD of Img when the background image is a vector source (an
	// SVG). The painter draws the scene into the computed tile rectangle through a
	// ctm, so a vector backend emits real path operators rather than a bitmap —
	// the same guarantee VectorItem gives a replaced <img src="*.svg">. Exactly one
	// of Img and Scene is set; a nil pair draws nothing.
	Scene VectorScene
	// SceneW, SceneH are Scene's own authored viewport size in points, which the
	// painter needs to scale the scene into the tile rect. Meaningful only when
	// Scene is set.
	SceneW, SceneH float64

	// Gradient is set INSTEAD of Img and Scene when the background image is a CSS
	// <gradient>. Exactly one of the three sources is ever set.
	//
	// A gradient has NO INTRINSIC SIZE (CSS Images 3: it is a generated image
	// with no natural dimensions), so unlike a bitmap or an SVG it cannot supply
	// IntrinsicW/IntrinsicH from its content. The layout engine therefore sets
	// them to the ORIGIN BOX's size, which is exactly what `background-size:
	// auto` must resolve to for a sizeless image — that one substitution makes
	// every other background property (size, position, repeat, origin, clip)
	// work on a gradient through the unchanged geometry path, rather than needing
	// a parallel set of rules.
	//
	// The gradient's own coordinates live in TILE space, so a `background-size`
	// that shrinks the tile correctly shrinks the gradient with it.
	Gradient *BackgroundGradient

	IntrinsicW, IntrinsicH float64 // decoded pixel size (or the vector/generated source's intrinsic size), > 0

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

// TileSize returns the painted size of one background tile in points, resolving
// background-size (SizeKind plus SizeW/SizeH) against the intrinsic size and the
// origin box. cover/contain scale uniformly by the larger/smaller axis ratio; an
// explicit size with one auto axis derives that axis from the intrinsic ratio;
// auto is the intrinsic size. A non-positive result means nothing should paint.
//
// It is pure geometry over the item and identical for a raster and a vector
// source, which is why it lives here rather than in the painter.
func (it *BackgroundImageItem) TileSize() (w, h float64) {
	iw, ih := it.IntrinsicW, it.IntrinsicH
	if iw <= 0 || ih <= 0 {
		return 0, 0
	}
	switch it.SizeKind {
	case BgSizeCover:
		s := math.Max(it.OriginW/iw, it.OriginH/ih)
		return iw * s, ih * s
	case BgSizeContain:
		s := math.Min(it.OriginW/iw, it.OriginH/ih)
		return iw * s, ih * s
	case BgSizeExplicit:
		w, h = it.SizeW, it.SizeH
		switch {
		case w <= 0 && h <= 0: // both auto → intrinsic
			return iw, ih
		case w <= 0: // width auto → preserve intrinsic ratio from the height
			return h * (iw / ih), h
		case h <= 0: // height auto → preserve intrinsic ratio from the width
			return w, w * (ih / iw)
		default:
			return w, h
		}
	default: // BgSizeAuto → intrinsic
		return iw, ih
	}
}

// VectorScene is a resolution-independent drawing (an SVG scene) that paints
// itself onto a Device. Implemented by pkg/svg/draw.Renderer; layout stays
// decoupled from the SVG packages.
type VectorScene interface {
	// DrawVector renders the scene with ctm mapping its viewport coordinates
	// (points, origin at the viewport's top-left) into device space.
	DrawVector(dev render.Device, ctm render.Matrix)
}

// VectorItem is a vector scene drawn into a box on the page. The rectangle is
// the viewport in page space (points, Y-down); the painter clips to it (the
// SVG viewport is overflow:hidden) and hands the scene a viewport->device ctm.
type VectorItem struct {
	Scene              VectorScene
	XPt, YPt, WPt, HPt float64
}
