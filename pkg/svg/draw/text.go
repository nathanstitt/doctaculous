package draw

import (
	"math"

	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
	"github.com/nathanstitt/doctaculous/pkg/render"
	"github.com/nathanstitt/doctaculous/pkg/svg"
)

// placedGlyph is one shaped glyph with its final pen position and per-
// character rotation resolved, produced by the measure pass and consumed by
// the paint pass.
//
// The two passes exist because text-anchor shifts a whole text chunk by its
// own total advance, which is not known until every glyph in that chunk has
// been shaped and measured. Measuring first also means the shaper runs
// exactly once per style run, not once per pass.
type placedGlyph struct {
	// glyph is the shaped glyph: its Outline is in em units, Y UP, and must
	// be scaled by SizePt and Y-flipped to reach SVG user space.
	glyph inline.Glyph

	// penX, penY is the glyph's origin on the baseline, in the <text>
	// element's user space, before any text-anchor shift.
	penX, penY float64

	// rotateRad is the per-character rotation about the glyph's own origin.
	rotateRad float64

	// chunk indexes the text chunk this glyph belongs to, so the anchor shift
	// resolved for that chunk is applied to exactly its own glyphs.
	chunk int

	// style is the resolved style at the glyph's source character, carrying
	// fill/stroke paint and the paint servers.
	style svg.Style

	// fillGradient/strokeGradient/fillPattern/strokePattern are the character's
	// resolved paint servers, carried through so paintGlyph can route them to
	// the same fill/stroke helpers a Shape uses.
	fillGradient, strokeGradient gradient
	fillPattern, strokePattern   pattern

	// clip and mask are the clip-path/mask in effect for this glyph, set by a
	// <tspan> that carries one (SVG 2). Glyphs sharing the same pair are
	// composited together in one offscreen group — see paintTextRuns — rather
	// than one group per glyph, which would both cost a scratch buffer each
	// and break a mask whose effect spans the whole run.
	clip *svg.ClipPath
	mask *svg.Mask
}

// matrix returns the transform mapping this glyph's outline from em space
// into the space tm maps from, where tm is the <text>'s own accumulated
// matrix. Innermost first:
//
//  1. Scale by SizePt and FLIP Y. Glyph outlines are in em units with Y UP
//     (see inline.Glyph.Outline); SVG user space has Y DOWN, so the sign flip
//     is what puts the glyph the right way up.
//  2. Rotate by the character's own rotate angle, about the glyph's ORIGIN —
//     which is the origin of the scaled space, so it composes before the
//     translation rather than after. Composing it the other way would rotate
//     each glyph about the text's origin instead of its own, fanning the
//     glyphs out along an arc.
//  3. Translate to the pen position on the baseline.
//  4. Compose with tm.
//
// Painting and clip-geometry extraction share this so the outlines a
// <clipPath> gets are provably the same ones a fill would have drawn.
func (p *placedGlyph) matrix(tm render.Matrix) render.Matrix {
	size := p.glyph.SizePt
	m := render.Scale(size, -size)
	if p.rotateRad != 0 {
		m = m.Mul(render.Rotate(p.rotateRad))
	}
	return m.Mul(render.Translate(p.penX, p.penY)).Mul(tm)
}

// faces returns the Renderer's font-face cache, creating it on first use.
//
// layoutfont.FaceCache is itself mutex-protected and safe for concurrent use,
// so sharing one across the engine's parallel page-render fan-out is exactly
// what it is designed for — and sharing matters, since parsing a font program
// is the expensive step and every <text> in a document resolves the same few
// families. The sync.Once keeps Renderer's "no mutable state" contract
// honest: the cache is created once and never replaced, so concurrent
// DrawVector calls cannot race on the field.
func (r *Renderer) faces() *layoutfont.FaceCache {
	r.faceOnce.Do(func() {
		r.faceCache = layoutfont.NewFaceCache()
	})
	return r.faceCache
}

// paintText paints one Text node: shape, measure, resolve each chunk's
// text-anchor shift, then paint every glyph as ordinary vector geometry.
//
// Glyphs go through FillGlyph/Stroke, never Device.DrawGlyph. DrawGlyph emits
// PDF text-showing operators in the pdfwrite backend, which cannot express
// what SVG text needs: independent fill AND stroke paint on the same glyph,
// an arbitrary per-glyph transform (SVG's rotate list), or a glyph
// participating in a clip path or mask as ordinary geometry. Routing through
// the same paintFill/paintStroke helpers paintShape uses is also what gives
// text gradients and patterns for free. The cost, stated plainly: SVG text in
// PDF output is vector outlines, not selectable text.
func (r *Renderer) paintText(dev render.Device, t *svg.Text, m render.Matrix, alpha float64, warned *warnFlags) {
	if t == nil || len(t.Chars) == 0 {
		return
	}
	if warned.drawCalls >= maxDrawCalls {
		r.logDrawBudgetCapOnce(warned)
		return
	}

	tm := t.M.Mul(m)
	placed := r.layoutText(t)
	if len(placed) == 0 {
		return
	}

	if t.ClipPath != nil || t.Mask != nil {
		// The <text>'s OWN clip-path/mask wrap everything as one unit, so the
		// whole node goes into a single group. Any per-<tspan> clip/mask
		// inside it still applies, nested one level down.
		r.paintTextGrouped(dev, t, placed, tm, alpha, warned)
		return
	}
	r.paintTextRuns(dev, placed, tm, alpha, warned)
}

// paintTextRuns paints placed in maximal runs of glyphs sharing the same
// per-<tspan> clip-path/mask pair: a run with neither paints directly, a run
// with either goes through one offscreen group.
//
// Grouping by run rather than per glyph is not just an optimisation. A mask
// is evaluated over the region it covers, so applying it separately to each
// glyph would give every glyph its own independent mask evaluation instead of
// one shared across the tspan — visibly wrong for any mask with a gradient or
// a shape smaller than the text. Per-glyph grouping would also allocate a
// full-canvas scratch buffer per character.
func (r *Renderer) paintTextRuns(dev render.Device, placed []placedGlyph, tm render.Matrix, alpha float64, warned *warnFlags) {
	for i := 0; i < len(placed); {
		clip, mask := placed[i].clip, placed[i].mask
		j := i
		for j < len(placed) && placed[j].clip == clip && placed[j].mask == mask {
			j++
		}
		run := placed[i:j]
		if clip == nil && mask == nil {
			for k := range run {
				if warned.drawCalls >= maxDrawCalls {
					r.logDrawBudgetCapOnce(warned)
					return
				}
				r.paintGlyph(dev, &run[k], tm, alpha, warned)
			}
		} else {
			r.paintGlyphRunGrouped(dev, run, clip, mask, tm, alpha, warned)
		}
		i = j
	}
}

// paintGlyphRunGrouped paints one run of glyphs through an offscreen group so
// a <tspan>'s clip-path and/or mask can be applied to the run as a unit —
// EndGroup being the only place a GroupMask can be applied, exactly as in
// paintShapeGrouped.
func (r *Renderer) paintGlyphRunGrouped(dev render.Device, run []placedGlyph, clip *svg.ClipPath, mask *svg.Mask, tm render.Matrix, alpha float64, warned *warnFlags) {
	if warned.groupDepth >= maxGroupNestingDepth {
		// Bounds concurrently-live scratch memory, not just CPU: see
		// paintShapeGrouped's matching guard. Degrade to painting the run
		// unclipped and unmasked rather than dropping it.
		r.logGroupDepthCapOnce(warned)
		for k := range run {
			r.paintGlyph(dev, &run[k], tm, alpha, warned)
		}
		return
	}
	dev.Save()
	dev.BeginGroup()
	warned.groupDepth++
	for k := range run {
		if warned.drawCalls >= maxDrawCalls {
			r.logDrawBudgetCapOnce(warned)
			break
		}
		r.paintGlyph(dev, &run[k], tm, 1.0, warned)
	}
	warned.groupDepth--

	bbox := textUserBounds(run)
	var clipMask, softMask render.GroupMask
	if clip != nil {
		clipMask = r.buildClipMask(dev, clip, tm, bbox)
	}
	if mask != nil {
		// Passed to EndGroup separately from clipMask, never pre-combined —
		// see paintShapeGrouped for the pdfwrite regression that caused.
		softMask = r.buildMask(dev, mask, tm, bbox)
	}
	dev.EndGroup(alpha, "", clipMask, softMask)
	dev.Restore()
}

// paintTextGrouped paints text carrying a clip-path or mask, which — exactly
// like paintShapeGrouped — requires an offscreen group because EndGroup is
// the only place a GroupMask can be applied.
//
// The objectBoundingBox target passed to buildClipMask/buildMask is the
// text's own device-space glyph bounds, computed from the placed glyphs: text
// has no single pre-transform Path the way a Shape does, so the box is
// measured here rather than read off the node.
func (r *Renderer) paintTextGrouped(dev render.Device, t *svg.Text, placed []placedGlyph, tm render.Matrix, alpha float64, warned *warnFlags) {
	if warned.groupDepth >= maxGroupNestingDepth {
		// Same memory-bounding rationale as paintShapeGrouped's guard:
		// degrade to painting without isolation, clip, or mask rather than
		// dropping the text entirely.
		r.logGroupDepthCapOnce(warned)
		r.paintTextRuns(dev, placed, tm, alpha, warned)
		return
	}
	dev.Save()
	dev.BeginGroup()
	warned.groupDepth++
	// Through paintTextRuns, not a bare glyph loop: a <tspan> INSIDE a
	// clipped/masked <text> carries its own clip/mask, which must still
	// apply, nested one level inside this group.
	r.paintTextRuns(dev, placed, tm, 1.0, warned)
	warned.groupDepth--

	bbox := textUserBounds(placed)
	var clipMask, softMask render.GroupMask
	if t.ClipPath != nil {
		clipMask = r.buildClipMask(dev, t.ClipPath, tm, bbox)
	}
	if t.Mask != nil {
		// Composite order is clip -> mask -> opacity, and the two masks reach
		// EndGroup SEPARATELY rather than pre-combined — see paintShapeGrouped
		// for the pdfwrite regression pre-combining caused.
		softMask = r.buildMask(dev, t.Mask, tm, bbox)
	}
	dev.EndGroup(alpha, "", clipMask, softMask)
	dev.Restore()
}

// textUserBounds returns a bounds function over the placed glyphs' user-space
// extent, in the shape buildClipMask/buildMask expect for an
// objectBoundingBox target (the same signature *render.Path.Bounds has).
//
// The box is the union of each glyph's em box at its pen position rather than
// its true ink extent: an exact ink box would need every outline transformed
// and measured, and objectBoundingBox on text is already an approximation
// (see pkg/svg's textBBox) — the em box is the stable, cheap choice, and it
// is what an author reasoning about a text bounding box expects.
func textUserBounds(placed []placedGlyph) func() (float64, float64, float64, float64, bool) {
	return func() (minX, minY, maxX, maxY float64, ok bool) {
		for i := range placed {
			p := &placed[i]
			size := p.glyph.SizePt
			x0, y0 := p.penX, p.penY-size*0.8
			x1, y1 := p.penX+p.glyph.Advance, p.penY+size*0.2
			if !ok {
				minX, minY, maxX, maxY, ok = x0, y0, x1, y1, true
				continue
			}
			minX, minY = math.Min(minX, x0), math.Min(minY, y0)
			maxX, maxY = math.Max(maxX, x1), math.Max(maxY, y1)
		}
		return minX, minY, maxX, maxY, ok
	}
}

// layoutText runs the measure pass: it shapes each maximal run of characters
// sharing one style, walks the flat glyph slice accumulating advances while
// applying the per-character absolute/relative position adjustments, applies
// bidi reordering per chunk, and finally shifts each chunk by its resolved
// text-anchor offset.
//
// Note what it does NOT call: inline.Break, BreakNextWrap, MakeLine, or
// Place. Those are box-layout machinery — they need a containing width to
// break against, and SVG text does not wrap. inline.Shape is a pure function
// that is not fused to line-breaking (it returns a flat []Glyph), which is
// exactly what makes reusing the engine's real shaper here possible instead
// of writing a second one. Arabic harfbuzz shaping and per-rune script
// fallback both happen INSIDE Shape, so SVG text gets them for free.
func (r *Renderer) layoutText(t *svg.Text) []placedGlyph {
	glyphs := r.shapeChars(t.Chars)
	if len(glyphs) == 0 {
		return nil
	}

	// Place every glyph, threading the pen through the character list.
	placed := make([]placedGlyph, 0, len(glyphs))
	penX, penY := 0.0, 0.0
	chunk := -1
	nextAnchor := 0
	for i := range glyphs {
		g := &glyphs[i]
		c := &t.Chars[g.charIndex]

		if nextAnchor < len(t.Anchors) && g.charIndex >= t.Anchors[nextAnchor] {
			// Advance to the chunk this character belongs to. A chunk can be
			// entered by a character whose index is past the recorded anchor
			// when earlier characters produced no glyphs, so this walks
			// forward rather than testing for equality.
			for nextAnchor < len(t.Anchors) && g.charIndex >= t.Anchors[nextAnchor] {
				nextAnchor++
			}
			chunk++
		}
		if chunk < 0 {
			chunk = 0
		}

		if c.HasAbsX {
			penX = c.AbsX
		}
		if c.HasAbsY {
			penY = c.AbsY
		}
		// dx/dy shift the pen permanently, not just this one glyph: that is
		// what makes a dx list shift each successive character cumulatively.
		penX += c.DX
		penY += c.DY

		placed = append(placed, placedGlyph{
			glyph:          g.glyph,
			penX:           penX,
			penY:           penY,
			rotateRad:      c.RotateDeg * math.Pi / 180,
			chunk:          chunk,
			style:          c.Style,
			fillGradient:   asGradient(c.FillGradient()),
			strokeGradient: asGradient(c.StrokeGradient()),
			fillPattern:    asPattern(c.FillPattern()),
			strokePattern:  asPattern(c.StrokePattern()),
			clip:           c.ClipPath(),
			mask:           c.Mask(),
		})
		penX += g.glyph.Advance
	}

	reorderVisually(placed)
	applyAnchors(placed, t)
	return placed
}

// reorderVisually applies UAX#9 bidi reordering to the already-PLACED glyphs,
// per text chunk, by permuting which glyph occupies each x slot.
//
// The ordering has to happen here rather than before the pen walk. SVG's
// x/y/dx/dy lists address characters in LOGICAL order (SVG2 §11.5), so the
// walk must see glyphs in logical order or an absolute x meant for the first
// logical character lands on whatever reordering put first — which for the
// corpus's direction/rtl.svg threw the anchor 170 units away from the rest of
// the text.
//
// So the walk assigns each glyph its advance-accumulated slot in logical
// order, and this then REASSIGNS those slots: the x positions themselves stay
// exactly where the walk put them (preserving every dx, dy, and absolute
// reset), and only WHICH glyph sits at each is permuted. Because the slots
// were laid left to right and inline.Reorder returns visual order, walking
// the reordered glyphs and re-dealing the slots left to right is precisely
// UAX#9 rule L2 applied to a line.
//
// inline.Reorder is standalone on a flat glyph slice — it needs no line box —
// which is what lets SVG reuse the engine's real UAX#9 implementation (rule
// L2 plus L4 bracket mirroring) rather than approximating it. An SVG <text>
// chunk is one line for bidi purposes, since it never wraps.
func reorderVisually(placed []placedGlyph) {
	for i := 0; i < len(placed); {
		chunk := placed[i].chunk
		j := i
		for j < len(placed) && placed[j].chunk == chunk {
			j++
		}
		reorderChunk(placed[i:j])
		i = j
	}
}

// reorderChunk reorders one chunk in place. It is a no-op unless reordering
// actually changes the sequence, so a Latin-only document pays nothing beyond
// inline.Reorder's own fast path.
func reorderChunk(run []placedGlyph) {
	if len(run) < 2 {
		return
	}
	dir := inline.DirLTR
	if run[0].style.DirectionRTL() {
		dir = inline.DirRTL
	}
	// Reorder a slice of TAGGED copies rather than the glyphs themselves, so
	// the permutation can be read back exactly.
	//
	// Matching reordered glyphs back to their sources by VALUE does not work:
	// a shaped run is full of zero-advance, nil-outline mark glyphs that are
	// indistinguishable from one another, so any value-based match assigns
	// several visual slots to the same logical glyph and scrambles the rest.
	// Encoding the index in a field inline.Reorder preserves but does not read
	// for ordering — Atomic, which it treats as an opaque payload — makes the
	// permutation exact and total. Runes must stay untouched: they are what
	// the algorithm reads to classify each glyph's bidi character type.
	tagged := make([]inline.Glyph, 0, len(run)+2)
	idx := make([]inline.AtomicItem, len(run))

	// unicode-bidi: bidi-override forces EVERY character into the base
	// direction rather than letting UAX#9 choose per character, so Latin
	// inside an rtl override reads backwards too. That is precisely what the
	// Unicode override controls mean, so the run is bracketed in an
	// LRO/RLO ... PDF pair here — synthetic, zero-width, and carrying no tag,
	// so they order the real glyphs without becoming any.
	//
	// They are added at REORDER time rather than folded into the shaper's
	// input, because the pen walk between shaping and here drops control
	// glyphs (a control must not consume a position-list entry), which would
	// have discarded them before they could do their job.
	override := run[0].style.BidiOverride()
	if override {
		open := '‭' // LRO
		if dir == inline.DirRTL {
			open = '‮' // RLO
		}
		tagged = append(tagged, inline.Glyph{Runes: []rune{open}})
	}
	for i := range run {
		g := run[i].glyph
		idx[i] = inline.AtomicItem{Ref: i}
		g.Atomic = &idx[i]
		tagged = append(tagged, g)
	}
	if override {
		tagged = append(tagged, inline.Glyph{Runes: []rune{'‬'}}) // PDF
	}

	visual := inline.Reorder(tagged, dir)
	if len(visual) != len(tagged) {
		return // defensive: a reorder that changed the count is not usable
	}
	order := make([]int, 0, len(run))
	for _, vg := range visual {
		if vg.Atomic == nil {
			continue // a synthetic bracket control: it maps to no real glyph
		}
		l, ok := vg.Atomic.Ref.(int)
		if !ok || l < 0 || l >= len(run) {
			return
		}
		order = append(order, l)
	}
	if len(order) != len(run) {
		return // not a total permutation: leave logical order rather than guess
	}

	if isIdentity(order) {
		return // nothing moved: leave the logical placement untouched
	}

	// Permute the whole glyph record, not just the outline: a glyph's paint,
	// clip/mask, and rotation all belong to the CHARACTER, and follow it into
	// its visual position.
	reordered := make([]placedGlyph, len(run))
	for s, l := range order {
		reordered[s] = run[l] // the untagged original; the tag lived only on the copy
	}

	// Re-lay the x positions along the visual sequence. The slots the logical
	// walk produced cannot simply be reused: they were accumulated from the
	// LOGICAL advances, so once the glyphs are permuted a glyph would sit in a
	// slot sized for a different glyph, overlapping its neighbour wherever the
	// two advances differ.
	//
	// Instead the chunk keeps its logical START — the pen position of its
	// first character, which is what carries the element's x/y and any leading
	// dx — and each visual glyph is laid from there by its own advance. Each
	// glyph also carries the LEAD GAP the logical walk left in front of it
	// (its own dx, i.e. whatever separated it from its logical predecessor
	// beyond that predecessor's advance), so a dx on an RTL character still
	// offsets it in the visual layout.
	//
	// Per-character absolute x/y resets inside an RTL chunk are the one case
	// this does not reproduce exactly: an absolute reset starts a NEW chunk
	// (see svg.Text.Anchors), so within a single chunk there is at most the
	// leading one, which is exactly the start captured here.
	leads := make([]float64, len(run))
	for l := 1; l < len(run); l++ {
		if gap := run[l].penX - (run[l-1].penX + run[l-1].glyph.Advance); gap > 0 {
			leads[l] = gap
		}
	}
	pen := run[0].penX
	for s := range reordered {
		pen += leads[order[s]]
		reordered[s].penX = pen
		pen += reordered[s].glyph.Advance
	}
	copy(run, reordered)
}

// isIdentity reports whether order is 0,1,2,... i.e. reordering changed
// nothing.
func isIdentity(order []int) bool {
	for i, v := range order {
		if i != v {
			return false
		}
	}
	return true
}

// asGradient narrows svg.GradientPaint to this package's own `gradient`
// accessor interface (the method sets are identical), preserving nil-ness so
// the `!= nil` checks in paintGlyphFill/paintGlyphStroke stay meaningful.
func asGradient(g svg.GradientPaint) gradient {
	if g == nil {
		return nil
	}
	return g
}

// asPattern narrows svg.PatternPaint to this package's own `pattern`
// accessor interface. See asGradient.
func asPattern(p svg.PatternPaint) pattern {
	if p == nil {
		return nil
	}
	return p
}

// applyAnchors shifts each text chunk by its text-anchor offset, measured
// from the chunk's own first glyph to the end of its last so an intervening
// dx or absolute reset inside the chunk is included.
//
// The offsets are DIRECTION-RELATIVE, which is the part that is easy to miss:
// text-anchor's "start" and "end" name the start and end of the INLINE BASE
// DIRECTION, not the left and right of the canvas (SVG2 §11.5, CSS Writing
// Modes). In an ltr chunk "start" is the left edge and shifts nothing; in an
// rtl chunk "start" is the RIGHT edge, so the chunk must extend leftward from
// its anchor point — shifted by its whole advance, exactly what "end" does in
// ltr. The corpus's direction/rtl.svg is anchored at x=170 with the default
// text-anchor and expects the Arabic to run leftward from there; treating
// "start" as "no shift" ran it off the right edge of the viewport.
//
// text-anchor itself is read from the chunk's FIRST character's style, per
// SVG2 §11.5: the property applies to a text chunk as a whole, so a <tspan>
// that changes it mid-chunk has no effect on that chunk (the corpus's
// text-anchor-not-on-text-chunk.svg asserts exactly this).
func applyAnchors(placed []placedGlyph, t *svg.Text) {
	if len(placed) == 0 {
		return
	}
	i := 0
	for i < len(placed) {
		chunk := placed[i].chunk
		j := i
		minX, maxX := math.Inf(1), math.Inf(-1)
		for j < len(placed) && placed[j].chunk == chunk {
			minX = math.Min(minX, placed[j].penX)
			maxX = math.Max(maxX, placed[j].penX+placed[j].glyph.Advance)
			j++
		}
		width := maxX - minX
		anchor := placed[i].style.TextAnchor()
		if placed[i].style.DirectionRTL() {
			// Map the direction-relative keywords onto physical edges for an
			// rtl chunk: start is the right edge, end the left. "middle" is
			// symmetric and unaffected.
			switch anchor {
			case "start":
				anchor = "end"
			case "end":
				anchor = "start"
			}
		}
		var shift float64
		switch anchor {
		case "middle":
			shift = -width / 2
		case "end":
			shift = -width
		}
		if shift != 0 {
			for k := i; k < j; k++ {
				placed[k].penX += shift
			}
		}
		i = j
	}
}

// shapedChar pairs one shaped glyph with the index of the source character it
// came from, so the per-character position adjustments survive shaping.
//
// The mapping is not one-to-one in either direction: a ligature or an Arabic
// contextual form can turn several characters into one glyph, and a
// decomposition can turn one into several. The index recorded is the FIRST
// source character a glyph covers, which is what SVG's positioning model
// wants — an x/dx entry addresses a character, and when several characters
// fuse into one glyph the fused glyph is placed by the first of them.
type shapedChar struct {
	glyph     inline.Glyph
	charIndex int
}

// shapeChars shapes the character list, one inline.Run per maximal span of
// characters sharing the same font style, and maps each resulting glyph back
// to a source character index.
//
// Each style span is shaped SEPARATELY, which is a real fidelity limit worth
// naming: a ligature or a cursive Arabic join spanning a <tspan> boundary
// will not form, because the two sides reach the shaper as different runs.
// That matches how the boundary must behave anyway whenever the two sides
// differ in family or size, and merging spans that happen to share a style
// would make the behavior depend on whether an author's <tspan> changed
// anything visible — inconsistent in a way that is harder to reason about
// than the uniform rule.
func (r *Renderer) shapeChars(chars []svg.TextChar) []shapedChar {
	faces := r.faces()
	var out []shapedChar
	for i := 0; i < len(chars); {
		j := i
		for j < len(chars) && sameShapingStyle(chars[i].Style, chars[j].Style) {
			j++
		}
		span := chars[i:j]
		st := span[0].Style
		if st.FontSizePt() <= 0 {
			// A zero (or, defensively, negative) font-size paints nothing.
			// The characters still consumed their position-list entries
			// above, which is correct: SVG's zero-size fixtures expect the
			// text to vanish, not the following text to shift.
			i = j
			continue
		}
		runes := make([]rune, len(span))
		for k := range span {
			runes[k] = span[k].R
		}
		// unicode-bidi: bidi-override is NOT applied here by wrapping the
		// text in the Unicode LRO/RLO controls. That works at the shaper
		// level, but reordering now happens after the pen walk (see
		// reorderVisually), and the walk drops the zero-width control glyphs
		// because they must not consume a position-list entry — so the
		// controls would be gone by the time the reorder ran. The override is
		// applied at reorder time instead, in reorderChunk.
		run := inline.Run{
			Text:   string(runes),
			Family: familyWithFallback(st.FontFamily()),
			Bold:   st.FontBold(),
			Italic: st.FontItalic(),
			SizePt: st.FontSizePt(),
			// white-space is already fully resolved at build time (see
			// pkg/svg's appendText), so the shaper must not collapse again;
			// "pre" preserves what arrives. No '\n' or '\t' survives
			// lowering, so the preserving mode's newline/tab handling never
			// triggers.
			WhiteSpace: "pre",
		}
		glyphs := inline.Shape(faces, []inline.Run{run}, r.Logf)
		// Shaping ONLY here — deliberately NOT inline.Reorder. Bidi reordering
		// has to happen AFTER the pen walk, not before it: SVG's x/y/dx/dy
		// lists address characters in LOGICAL order, so a reorder applied here
		// would hand the pen walk glyphs whose source-character indices jump
		// around, and an absolute x meant for the first logical character
		// would land on whichever glyph reordering happened to put first.
		// See reorderVisually, which the caller runs on the PLACED glyphs.
		out = append(out, mapGlyphsToChars(glyphs, span, i)...)
		i = j
	}
	return out
}

// familyWithFallback appends a terminal generic family to a font-family list
// so resolution can never fail outright.
//
// This matters because inline.Shape SKIPS a run whose family does not
// resolve, which for a reflow document is the right call (the box had a
// fallback chain and none of it matched) but for SVG would silently delete
// the text. Naming a font the engine has no bundled look-alike for —
// `font-family="Noto Sans"`, which most of the resvg text corpus does — is
// ordinary, and every SVG renderer substitutes rather than rendering nothing.
// The generic keyword always resolves (see pkg/font/standard), so appending
// one makes the chain terminal.
//
// It is appended, never substituted: an author's own list is still tried
// first, name by name, exactly as written.
func familyWithFallback(family string) string {
	if family == "" {
		return "sans-serif"
	}
	return family + ", sans-serif"
}

// mapGlyphsToChars pairs each shaped glyph with the index (in the whole
// character list) of the source character it came from, dropping the
// zero-width bidi control glyphs a bidi-override wrapper introduced.
//
// Matching is by RUNE IDENTITY against the span, scanning forward from the
// last match: Glyph.Runes carries the source runes a glyph covers, so the
// first rune of each glyph locates its character. A glyph whose runes cannot
// be located (a synthesized outline with no Runes, or reordering that moved a
// glyph before its predecessor's match point) falls back to the span's
// running position, which keeps every glyph placed rather than dropping it.
func mapGlyphsToChars(glyphs []inline.Glyph, span []svg.TextChar, spanStart int) []shapedChar {
	out := make([]shapedChar, 0, len(glyphs))
	next := 0
	for _, g := range glyphs {
		if g.Break {
			continue
		}
		if len(g.Runes) == 1 && isBidiControl(g.Runes[0]) {
			// A bidi control draws nothing and has no advance, so it must not
			// occupy a position-list entry. It is dropped here rather than
			// carried through placement; the reorder that would have read it
			// runs later on the placed glyphs, and gets the base direction
			// and the override flag from the style instead.
			continue
		}
		idx := -1
		if len(g.Runes) > 0 {
			for k := next; k < len(span); k++ {
				if span[k].R == g.Runes[0] {
					idx = k
					break
				}
			}
			if idx < 0 {
				// Reordering can place a glyph whose character sits BEFORE
				// the running position; search the whole span before giving
				// up, so an RTL line still maps every glyph.
				for k := 0; k < len(span); k++ {
					if span[k].R == g.Runes[0] {
						idx = k
						break
					}
				}
			}
		}
		if idx < 0 {
			idx = next
			if idx >= len(span) {
				idx = len(span) - 1
			}
		}
		next = idx + 1
		out = append(out, shapedChar{glyph: g, charIndex: spanStart + idx})
	}
	return out
}

// isBidiControl reports whether r is one of the Unicode explicit
// bidirectional formatting controls. Only the two overrides and the
// terminating pop are produced by this package, but the whole set is
// recognized so a control an AUTHOR wrote into the text is never mistaken for
// content with an advance.
func isBidiControl(r rune) bool {
	switch r {
	case '‎', '‏', // LRM, RLM
		'‪', '‫', '‬', '‭', '‮', // LRE, RLE, PDF, LRO, RLO
		'⁦', '⁧', '⁨', '⁩': // LRI, RLI, FSI, PDI
		return true
	}
	return false
}

// sameShapingStyle reports whether two characters' styles shape identically,
// i.e. whether they can share one inline.Run. Only the properties the shaper
// reads matter — family, size, weight, slant, and the bidi properties that
// change how the run is reordered. Fill/stroke deliberately do NOT: a colour
// change mid-word must not break a ligature, and paint is applied per glyph
// afterward from each character's own style.
func sameShapingStyle(a, b svg.Style) bool {
	return a.FontFamily() == b.FontFamily() &&
		a.FontSizePt() == b.FontSizePt() &&
		a.FontBold() == b.FontBold() &&
		a.FontItalic() == b.FontItalic() &&
		a.DirectionRTL() == b.DirectionRTL() &&
		a.BidiOverride() == b.BidiOverride()
}

// paintGlyph paints one placed glyph as ordinary vector geometry: it builds
// the em-space-to-device transform, maps the outline through it, and routes
// the result to the SAME paintFill/paintStroke helpers a Shape uses — which
// is what gives text gradients, patterns, and the whole paint pipeline for
// free.
//
// See placedGlyph.matrix for the transform itself.
func (r *Renderer) paintGlyph(dev render.Device, p *placedGlyph, tm render.Matrix, alpha float64, warned *warnFlags) {
	if p.glyph.Outline == nil {
		return // whitespace, or a rune the face has no ink for: advance only
	}
	if p.glyph.SizePt <= 0 {
		return
	}

	dp := render.TransformPath(p.glyph.Outline, p.matrix(tm))
	if dp == nil {
		return
	}

	opacity := clamp01(p.style.Opacity())
	a := alpha * opacity

	// A glyph is a shape for painting purposes, so the fill/stroke split goes
	// through the identical helpers — including the gradient/pattern routing
	// and the stroke's user-space-to-device width scaling. tm — NOT the
	// glyph's own matrix — is what a paint server's local space composes
	// under: a gradient on text is defined in the TEXT's user space, not in
	// each glyph's scaled, rotated em space, so passing the glyph matrix here
	// would give every glyph its own private copy of the gradient.
	warned.drawCalls++
	r.paintGlyphFill(dev, p, dp, tm, a, warned)
	r.paintGlyphStroke(dev, p, dp, tm, a, warned)
}

// paintGlyphFill fills one glyph outline, mirroring paintFill's branch order
// exactly (gradient, then pattern, then solid) so text and shapes can never
// diverge in which paint wins.
func (r *Renderer) paintGlyphFill(dev render.Device, p *placedGlyph, dp *render.Path, tm render.Matrix, alpha float64, warned *warnFlags) {
	if p.fillGradient != nil {
		r.fillGradient(dev, dp, p.style, p.fillGradient, tm, alpha)
		return
	}
	if p.fillPattern != nil {
		r.fillPattern(dev, dp, p.style, p.fillPattern, tm, alpha, warned)
		return
	}
	fp, ok := p.style.FillPaint()
	if !ok {
		return
	}
	fp.Color.A = scaleAlpha(fp.Color.A, alpha)
	if fp.Rule != render.NonZero {
		// render.FillColor carries no fill rule and FillGlyph is defined to
		// use nonzero winding, so a fill-rule:evenodd on text has to go
		// through the general Fill path to be honored at all. The corpus's
		// text/fill-rule=evenodd.svg is what makes this branch load-bearing:
		// the counters of glyphs like "o" invert under even-odd, which is a
		// visible difference, not a rounding one.
		dev.Fill(dp, fp)
		return
	}
	// FillGlyph, not Fill: the outline IS a glyph, and a backend that can
	// treat it specially (hinting, a glyph cache) should get the chance to.
	// Both current backends rasterize it identically to Fill.
	dev.FillGlyph(dp, render.FillColor{R: fp.Color.R, G: fp.Color.G, B: fp.Color.B, A: fp.Color.A}, "")
}

// paintGlyphStroke strokes one glyph outline. It reuses the plain
// Device.Stroke path with the same user-space-to-device width scaling
// paintStroke applies, so stroke-width on text means the same thing it means
// on a <path>.
func (r *Renderer) paintGlyphStroke(dev render.Device, p *placedGlyph, dp *render.Path, tm render.Matrix, alpha float64, warned *warnFlags) {
	if p.strokeGradient != nil || p.strokePattern != nil {
		// Same documented degradation as a gradient stroke on a shape: no
		// stroke-to-outline conversion exists to clip a shading against, so
		// this falls back to StrokePaint's fallback colour.
		r.logStrokeGradientOnce(warned)
	}
	sp, ok := p.style.StrokePaint()
	if !ok {
		return
	}
	sf := tm.ScaleFactor()
	if sf == 0 {
		return
	}
	sp.Color.A = scaleAlpha(sp.Color.A, alpha)
	sp.Width *= sf
	sp.DashPhase *= sf
	if sp.DashArray != nil {
		scaled := make([]float64, len(sp.DashArray))
		for i, d := range sp.DashArray {
			scaled[i] = d * sf
		}
		sp.DashArray = scaled
	}
	dev.Stroke(dp, sp)
}

// textClipPath shapes t and returns every glyph outline unioned into a single
// device-space path under m, for a <text> used as clip or mask geometry.
//
// This is the whole payoff of painting text as outlines rather than through
// DrawGlyph: a glyph is already a *render.Path, so text becomes clip geometry
// with no new machinery — the same transform paintGlyph builds, applied to
// the same outline, appended into one path instead of filled.
//
// Returns nil when t shapes to nothing (no characters, an unresolvable
// zero font-size, or no glyph carrying ink), which the caller treats as "this
// child contributes nothing to the union" — NOT as "clip to everything".
func (r *Renderer) textClipPath(t *svg.Text, m render.Matrix) *render.Path {
	if t == nil {
		return nil
	}
	placed := r.layoutText(t)
	if len(placed) == 0 {
		return nil
	}
	// t.M is the <text> element's own transform, which composes under the
	// caller's matrix exactly as it does in paintText.
	tm := t.M.Mul(m)
	out := &render.Path{}
	for i := range placed {
		p := &placed[i]
		if p.glyph.Outline == nil || p.glyph.SizePt <= 0 {
			continue
		}
		dp := render.TransformPath(p.glyph.Outline, p.matrix(tm))
		if dp == nil {
			continue
		}
		out.Segments = append(out.Segments, dp.Segments...)
	}
	if out.Empty() {
		return nil
	}
	return out
}

// TextAdvances shapes text exactly as paintText would and returns each
// glyph's advance in points. It exists so a test can assert that an SVG
// <text> and an equivalent CSS/HTML inline run produce the SAME advances —
// i.e. that this package really does reuse inline.Shape rather than having
// forked a second shaper. It is not used by rendering.
func (r *Renderer) TextAdvances(chars []svg.TextChar) []float64 {
	shaped := r.shapeChars(chars)
	out := make([]float64, 0, len(shaped))
	for _, s := range shaped {
		out = append(out, s.glyph.Advance)
	}
	return out
}
