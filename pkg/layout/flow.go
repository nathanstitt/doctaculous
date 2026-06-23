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

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/layout/box"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
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

	lines := breakLines(glyphs, lineW, firstW)

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

// shapeBlock turns a block's inlines into a flat slice of shaped glyphs, resolving
// each inline's face and measuring every rune. Inlines whose family has no bundled
// face are skipped (logged once per family by the cache miss path).
func (e *Engine) shapeBlock(b box.Block) []shapedGlyph {
	var out []shapedGlyph
	for _, in := range b.Inlines {
		if in.ForceBreak {
			out = append(out, shapedGlyph{hardBreak: true})
			continue
		}
		style := pkgfont.Style{Bold: in.Face.Bold, Italic: in.Face.Italic}
		face, ok := e.faces.Resolve(in.Face.Family, style)
		if !ok {
			e.logf("layout: no font for family %q; skipping run", in.Face.Family)
			continue
		}
		asc, desc, gap := face.Metrics()
		col := glyphColor{R: in.Color.R, G: in.Color.G, B: in.Color.B, A: in.Color.A}
		if in.Color.A == 0 {
			col.A = 0xff // a zero-alpha color is unset; treat as opaque
		}
		for _, r := range in.Text {
			outline, advEm, ok := face.Glyph(r)
			if !ok {
				continue
			}
			out = append(out, shapedGlyph{
				outline:   outline,
				advance:   advEm * in.SizePt,
				color:     col,
				sizePt:    in.SizePt,
				ascentPt:  asc * in.SizePt,
				descentPt: desc * in.SizePt,
				lineGapPt: gap * in.SizePt,
				isSpace:   r == ' ' || r == '\t',
			})
		}
	}
	return out
}

// lineHeight computes a line's height from the block's line-height spec and the
// line's own font metrics. The natural height is ascent + descent + line gap.
func (e *Engine) lineHeight(spec box.LineHeight, ln line) float64 {
	natural := ln.ascentPt + ln.descentPt + ln.lineGapPt
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
func ascentOf(ln line) float64 {
	if ln.ascentPt > 0 {
		return ln.ascentPt
	}
	return 0
}

// emitLine places a line's glyphs at the given baseline, applying horizontal
// alignment. lineW is the content width available to this line; isLast suppresses
// full justification on the final line of a paragraph (per convention).
func (e *Engine) emitLine(st *flowState, ln line, xStart, baseline, lineW float64, align box.Align, isLast bool) {
	if len(ln.glyphs) == 0 {
		return
	}
	x := xStart
	extraPerSpace := 0.0

	switch align {
	case box.AlignRight:
		x = xStart + (lineW - ln.widthPt)
	case box.AlignCenter:
		x = xStart + (lineW-ln.widthPt)/2
	case box.AlignJustify:
		if !isLast {
			if n := countSpaces(ln.glyphs); n > 0 {
				extraPerSpace = (lineW - ln.widthPt) / float64(n)
				if extraPerSpace < 0 {
					extraPerSpace = 0
				}
			}
		}
	}

	for _, gl := range ln.glyphs {
		if gl.outline != nil {
			st.cur = append(st.cur, Item{
				Kind: GlyphKind,
				Glyph: GlyphItem{
					Outline: gl.outline,
					XPt:     x,
					YPt:     baseline,
					SizePt:  gl.sizePt,
					Color:   color.RGBA{R: gl.color.R, G: gl.color.G, B: gl.color.B, A: gl.color.A},
				},
			})
		}
		x += gl.advance
		if gl.isSpace {
			x += extraPerSpace
		}
	}
}

// countSpaces counts break-opportunity glyphs excluding any trailing run of
// spaces (which receive no justification stretch).
func countSpaces(glyphs []shapedGlyph) int {
	end := len(glyphs)
	for end > 0 && glyphs[end-1].isSpace {
		end--
	}
	n := 0
	for i := 0; i < end; i++ {
		if glyphs[i].isSpace {
			n++
		}
	}
	return n
}
