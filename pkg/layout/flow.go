// Package layout is the format-neutral reflow engine. It consumes a box.Document
// (produced by a frontend such as pkg/docx) and lays it out into pages of
// positioned glyphs and rules, breaking lines and paginating with real font
// metrics. The engine knows nothing about DOCX/HTML/EPUB; those formats plug in
// only by producing a box.Document, so the line-breaking and pagination logic is
// shared across all reflowable formats.
//
// Coordinates in the output are page space: points, Y-down, origin at the page's
// top-left corner. This matches the device convention so the paint stage maps a
// page to pixels with a single scale matrix.
package layout

import (
	"context"
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/layout/box"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/inline"
)

// defaultLineMult is the line-height multiplier applied to the natural font
// height for auto line spacing, approximating Word's default ~1.15 leading. It is
// used when a block requests LineHeightAuto with no explicit multiplier.
const defaultLineMult = 1.15

// Engine lays out box.Documents. Build one with New; it is safe for concurrent
// use (its only shared state is the face cache, which is itself concurrent).
type Engine struct {
	faces *layoutfont.FaceCache
	logf  func(string, ...any)
}

// New returns an Engine that resolves fonts through faces and logs unsupported
// or degraded cases through logf (nil is allowed).
func New(faces *layoutfont.FaceCache, logf func(string, ...any)) *Engine {
	if faces == nil {
		faces = layoutfont.NewFaceCache()
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Engine{faces: faces, logf: logf}
}

// Layout reflows doc into pages. It honors ctx cancellation between blocks and
// pages. It never panics on a malformed block: a per-block recover skips a bad
// block and continues, so one bad paragraph cannot abort the document.
func (e *Engine) Layout(ctx context.Context, doc box.Document) (*Pages, error) {
	g := doc.Page
	contentW := g.WidthPt - g.MarginLeftPt - g.MarginRightPt
	contentBottom := g.HeightPt - g.MarginBottomPt
	if contentW <= 0 || contentBottom <= g.MarginTopPt {
		// Degenerate geometry: emit a single empty page rather than failing.
		return &Pages{Pages: []Page{{WidthPt: g.WidthPt, HeightPt: g.HeightPt}}}, nil
	}

	st := &flowState{
		eng:           e,
		geom:          g,
		contentW:      contentW,
		contentBottom: contentBottom,
		penY:          g.MarginTopPt,
	}
	st.newPage()

	for i := range doc.Blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e.layoutBlockSafe(st, doc.Blocks[i])
	}
	st.finish()
	return &Pages{Pages: st.pages}, nil
}

// flowState carries the running pagination cursor across blocks.
type flowState struct {
	eng           *Engine
	geom          box.PageGeometry
	contentW      float64
	contentBottom float64

	pages []Page
	cur   []Item  // items accumulated for the current page
	penY  float64 // current vertical pen position, page space
}

// newPage starts a fresh page, resetting the pen to the top margin.
func (s *flowState) newPage() {
	s.cur = nil
	s.penY = s.geom.MarginTopPt
}

// finish flushes the current page into the page list.
func (s *flowState) finish() {
	s.pages = append(s.pages, Page{
		WidthPt:  s.geom.WidthPt,
		HeightPt: s.geom.HeightPt,
		Items:    s.cur,
	})
}

// flushPage finalizes the current page and begins a new one.
func (s *flowState) flushPage() {
	s.finish()
	s.newPage()
}

// layoutBlockSafe runs layoutBlock under a recover so a malformed block degrades
// to "skipped + logged" instead of crashing the batch.
func (e *Engine) layoutBlockSafe(st *flowState, b box.Block) {
	defer func() {
		if r := recover(); r != nil {
			e.logf("layout: recovered from panic laying out block: %v", r)
		}
	}()
	e.layoutBlock(st, b)
}

// layoutBlock shapes a block, breaks it into lines, and flows those lines down
// the page, paginating on overflow.
func (e *Engine) layoutBlock(st *flowState, b box.Block) {
	if b.BreakBefore && len(st.cur) > 0 {
		st.flushPage()
	}
	st.penY += b.SpaceBeforePt

	glyphs := e.shapeBlock(b)

	leftPt := st.geom.MarginLeftPt + b.IndentLeftPt
	rightInsetPt := b.IndentRightPt
	lineW := st.contentW - b.IndentLeftPt - rightInsetPt
	if lineW <= 0 {
		lineW = st.contentW
	}
	firstW := lineW - b.FirstLinePt // first-line indent narrows (or widens) line 1
	if firstW <= 0 {
		// A first-line indent at least as wide as the line would leave the first line
		// zero or negative available width; clamp to the full line width so words
		// still break sensibly instead of each landing on its own line.
		firstW = lineW
	}

	lines := inline.Break(glyphs, lineW, firstW)

	for i, ln := range lines {
		lineHeight := e.lineHeight(b.LineHeight, ln)
		// Paginate: if this line would cross the bottom margin, start a new page.
		// Always keep at least one line per page to guarantee progress.
		if st.penY+lineHeight > st.contentBottom && len(st.cur) > 0 {
			st.flushPage()
		}
		baseline := st.penY + ascentOf(ln)
		xStart := leftPt
		if i == 0 {
			xStart += b.FirstLinePt
		}
		e.emitLine(st, ln, xStart, baseline, lineW, b.Align, i == len(lines)-1)
		st.penY += lineHeight
	}

	st.penY += b.SpaceAfterPt
}

// shapeBlock turns a block's inlines into a flat slice of shaped glyphs by adapting
// them into the neutral inline.Run model and delegating to the shared inline core.
// Inlines whose family has no bundled face are skipped (logged once per family by
// the cache miss path).
func (e *Engine) shapeBlock(b box.Block) []inline.Glyph {
	runs := make([]inline.Run, 0, len(b.Inlines))
	for _, in := range b.Inlines {
		runs = append(runs, inline.Run{
			Text:   in.Text,
			Family: in.Face.Family,
			Bold:   in.Face.Bold,
			Italic: in.Face.Italic,
			SizePt: in.SizePt,
			Color:  in.Color,
			Break:  in.ForceBreak,
		})
	}
	return inline.Shape(e.faces, runs, e.logf)
}

// lineHeight computes a line's height from the block's line-height spec and the
// line's own font metrics. The natural height is ascent + descent + line gap.
func (e *Engine) lineHeight(spec box.LineHeight, ln inline.Line) float64 {
	natural := ln.AscentPt + ln.DescentPt + ln.LineGapPt
	switch spec.Mode {
	case box.LineHeightExact:
		return spec.ValuePt
	case box.LineHeightAtLeast:
		if spec.ValuePt > natural {
			return spec.ValuePt
		}
		return natural
	default: // LineHeightAuto
		mult := spec.Mult
		if mult <= 0 {
			mult = defaultLineMult
		}
		return natural * mult
	}
}

// ascentOf returns a line's ascent, falling back to a nominal value for an empty
// line so an empty paragraph still occupies sensible vertical space.
func ascentOf(ln inline.Line) float64 {
	if ln.AscentPt > 0 {
		return ln.AscentPt
	}
	return 0
}

// emitLine places a line's glyphs at the given baseline, applying horizontal
// alignment. lineW is the content width available to this line; isLast suppresses
// full justification on the final line of a paragraph (per convention).
func (e *Engine) emitLine(st *flowState, ln inline.Line, xStart, baseline, lineW float64, align box.Align, isLast bool) {
	if len(ln.Glyphs) == 0 {
		return
	}
	p := inline.Place(toInlineAlign(align), xStart, lineW, ln.WidthPt, inline.CountSpaces(ln.Glyphs), isLast)
	x := p.StartX

	for _, gl := range ln.Glyphs {
		if gl.Outline != nil {
			st.cur = append(st.cur, Item{
				Kind: GlyphKind,
				Glyph: GlyphItem{
					Outline: gl.Outline,
					XPt:     x,
					YPt:     baseline,
					SizePt:  gl.SizePt,
					Color:   color.RGBA{R: gl.Color.R, G: gl.Color.G, B: gl.Color.B, A: gl.Color.A},
					Face:    gl.Face,
					GID:     gl.GID,
					Runes:   gl.Runes,
				},
			})
		}
		x += gl.Advance
		if gl.Space {
			x += p.ExtraPerSpace
		}
	}
}

// toInlineAlign maps the flat box model's alignment onto the inline core's neutral
// alignment vocabulary. The switch is explicit rather than a cast because the two
// enums are independent types whose integer values must not be assumed equal.
func toInlineAlign(a box.Align) inline.Align {
	switch a {
	case box.AlignCenter:
		return inline.AlignCenter
	case box.AlignRight:
		return inline.AlignRight
	case box.AlignJustify:
		return inline.AlignJustify
	default:
		return inline.AlignLeft
	}
}
