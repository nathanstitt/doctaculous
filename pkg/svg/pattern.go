package svg

import "github.com/nathanstitt/doctaculous/pkg/render"

// patternPaint is a fully-resolved <pattern> paint server, ready to paint by
// repeated clipped draws of its tile content (no offscreen raster buffer —
// see the package doc on why pkg/svg/draw cannot create one). Tile is the
// tile's content already built into a scene Group (via the same
// buildGroup/buildNode machinery the rest of the scene uses), so painting it
// is just paint()'ing a Group N times under a translated clip; nothing here
// depends on Document staying mutable after Parse.
//
// Coordinate spaces, outermost to innermost:
//   - X, Y, CellW, CellH place and size ONE tile cell in the shape's own
//     user space (pre Shape.M): patternUnits' bbox-or-user mapping is
//     already baked into these four numbers by resolvePattern, so the grid
//     simply repeats every CellW/CellH starting at (X,Y) in that space.
//   - M is patternTransform alone, applied AFTER cell placement (per spec,
//     it transforms the whole already-established pattern coordinate
//     system): the caller composes Translate(cell origin).Mul(M) per cell,
//     then Shape.M and the accumulated scene-walk matrix on top, mirroring
//     paintServer.Matrix's role but not its contents (a gradient has no
//     separate "cell placement" step, so its Matrix carries the bbox
//     mapping directly; a pattern's does not, since X/Y/CellW/CellH already
//     did).
//   - ContentM maps the tile's own content coordinates (what Tile's shapes
//     are actually authored in) into the [0,CellW]x[0,CellH] cell box: a
//     pattern viewBox's mapping if present, else patternContentUnits'
//     bbox-or-user mapping (identity for the common userSpaceOnUse case).
type patternPaint struct {
	tile     *Group
	m        render.Matrix
	x, y     float64
	cellW    float64
	cellH    float64
	contentM render.Matrix
}

// resolvePattern resolves id (already known, by ps.kind == "pattern", to
// name a pattern) into a *patternPaint, or ok=false when the pattern paints
// nothing: zero tile children (after inheritance), non-positive tile
// width/height, an objectBoundingBox patternUnits mapping over a degenerate
// shape bbox, or a self-referencing tile (see buildingPattern's doc comment)
// — per SVG, an invalid/empty pattern is treated as "none", not a fallback.
//
// path is the shape's own PRE-TRANSFORM geometry (for objectBoundingBox
// patternUnits/patternContentUnits, exactly like resolveGradient's path
// parameter).
func (b *sceneBuilder) resolvePattern(id string, ps *resolvedServer, path *render.Path) (*patternPaint, bool) {
	if len(ps.kids) == 0 {
		return nil, false
	}
	if b.buildingPattern[id] {
		// Cycle: a shape inside this very pattern's tile (directly, or
		// nested through another pattern's tile) references id again. Stop
		// here rather than recursing into buildKidsGroup a second time for
		// the same id — see buildingPattern's doc comment on sceneBuilder.
		b.warnOnceMsg("pattern-cycle:"+id, "svg: <pattern> tile references itself; treating the reference as unpainted")
		return nil, false
	}

	unitsUser := ps.attrs["patternUnits"] == "userSpaceOnUse"
	contentUnitsBBox := ps.attrs["patternContentUnits"] == "objectBoundingBox"

	minX, minY, maxX, maxY, hasBounds := path.Bounds()
	bboxW, bboxH := maxX-minX, maxY-minY
	needsBBox := !unitsUser || contentUnitsBBox
	if needsBBox && (!hasBounds || bboxW == 0 || bboxH == 0) {
		// Degenerate bounding box and something here needs it: mirror
		// resolveGradient's rule that an objectBoundingBox-relative paint
		// server cannot be established over a zero-extent shape.
		return nil, false
	}

	x := gradientCoord(ps.attrs, "x", 0, unitsUser, b.vp.w)
	y := gradientCoord(ps.attrs, "y", 0, unitsUser, b.vp.h)
	w := gradientCoord(ps.attrs, "width", 0, unitsUser, b.vp.w)
	h := gradientCoord(ps.attrs, "height", 0, unitsUser, b.vp.h)
	if !unitsUser {
		x = minX + x*bboxW
		y = minY + y*bboxH
		w *= bboxW
		h *= bboxH
	}
	if w <= 0 || h <= 0 {
		// Spec: a pattern with width or height of zero (or negative)
		// disables rendering of the element referencing it.
		return nil, false
	}

	patM := render.Identity
	if s, ok := ps.attrs["patternTransform"]; ok {
		m, ok := parseTransform(s)
		if !ok {
			b.logf("svg: ignoring patternTransform=%q on pattern %q: unparseable", s, id)
		} else {
			patM = m
		}
	}

	// Content coordinate mapping: a viewBox on the pattern element itself
	// takes precedence over patternContentUnits (SVG2 §13.3), establishing
	// its own viewBox->cell-box mapping exactly like the root <svg>'s.
	contentM := render.Identity
	if vbAttr, ok := ps.attrs["viewBox"]; ok {
		if vb, ok := parseViewBox(vbAttr); ok {
			contentM = viewBoxMatrix(vb, w, h, ps.attrs["preserveAspectRatio"])
		} else {
			b.logf("svg: ignoring viewBox=%q on pattern %q: unparseable or non-positive extent", vbAttr, id)
		}
	} else if contentUnitsBBox {
		contentM = render.Scale(bboxW, bboxH)
	}

	b.buildingPattern[id] = true
	tile := b.buildKidsGroup(ps.kids, defaultStyle(), &cascadeCtx{idx: b.idx, logf: b.logf})
	delete(b.buildingPattern, id)
	if len(tile.Kids) == 0 {
		// Every tile child resolved to nothing (display:none, foreign
		// namespace, all-cycle references, ...): paints nothing, same as
		// zero children up front.
		return nil, false
	}

	return &patternPaint{
		tile:     tile,
		m:        patM,
		x:        x,
		y:        y,
		cellW:    w,
		cellH:    h,
		contentM: contentM,
	}, true
}

// Tile returns the pattern's tile content, already built into a scene Group.
// The caller (pkg/svg/draw) paints it once per repeated cell under a
// translated, clipped matrix; the Group itself is immutable and safe to
// paint repeatedly or concurrently, exactly like the main scene tree it was
// built alongside.
func (pp *patternPaint) Tile() *Group { return pp.tile }

// Matrix returns patternTransform alone (Identity if absent), applied after
// a cell has been placed at its (X,Y) origin in the shape's own user space
// (pre Shape.M) — see the patternPaint doc comment for why this does NOT
// also carry the patternUnits bbox-or-user mapping the way
// paintServer.Matrix carries a gradient's.
func (pp *patternPaint) Matrix() render.Matrix { return pp.m }

// Cell returns the tile cell's origin (X, Y) and size (CellW, CellH) in the
// space Matrix maps from: the grid repeats every CellW horizontally and
// CellH vertically, starting at (X, Y).
func (pp *patternPaint) Cell() (x, y, w, h float64) { return pp.x, pp.y, pp.cellW, pp.cellH }

// ContentMatrix returns the matrix mapping the tile's own content
// coordinates (what Tile()'s shapes are authored in) into the
// [0,CellW]x[0,CellH] cell box: a pattern viewBox's mapping if present, else
// patternContentUnits' bbox-or-user mapping (Identity for the common
// userSpaceOnUse case, which needs no remapping at all).
func (pp *patternPaint) ContentMatrix() render.Matrix { return pp.contentM }
