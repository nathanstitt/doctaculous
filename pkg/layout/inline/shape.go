// Package inline is the format-neutral inline-layout core: text shaping, greedy
// line-breaking, and horizontal alignment math shared by every reflow engine.
//
// Both the flat DOCX engine (pkg/layout) and the forthcoming CSS inline
// formatting context adapt their own styled-run models into this package's
// neutral Run, then reuse one implementation of shaping, breaking, and
// alignment. The core does no cascade and no unit math: every Run arrives with a
// concrete family, point size, and concrete color, so the only font work here is
// face resolution and per-rune measurement.
//
// Coordinates follow the same convention as the rest of the reflow pipeline:
// advances and metrics are in points; glyph outlines stay in em units with Y up
// (the engine scales them at emit time).
package inline

import (
	"image/color"

	pkgfont "github.com/nathanstitt/doctaculous/pkg/font"
	layoutfont "github.com/nathanstitt/doctaculous/pkg/layout/font"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// Run is the neutral styled run: the shaper's single input, which both the flat
// DOCX engine and the CSS inline formatting context adapt into. Everything is
// already resolved (concrete family, point size, concrete color); the core does
// no cascade or unit math.
type Run struct {
	Text         string      // ignored when Break or Atomic != nil
	Family       string      // resolved family name (e.g. "Arial")
	Bold, Italic bool        // weight/slant request, resolved against the face cache
	SizePt       float64     // em size in points
	Color        color.RGBA  // zero-alpha => shaped opaque (the historical flat-engine fixup)
	Break        bool        // hard line break: forces a new line, produces no glyphs
	Atomic       *AtomicItem // non-nil => an unbreakable inline box (inline-block/replaced)
}

// AtomicItem is an inline-level box that participates in a line as one unbreakable
// unit of a fixed width. The IFC lays out its own fragment separately; the line
// only needs its advance and baseline placement. Carried opaquely through shaping.
type AtomicItem struct {
	WidthPt, HeightPt float64
	BaselinePt        float64 // distance from the item's top down to the baseline it rests on
	Ref               any     // opaque back-reference the IFC uses to position the item's fragment
}

// Glyph is one shaped glyph, or whitespace (nil Outline, Space=true), or a hard
// break (Break=true), or an atomic inline box (Atomic != nil). It is the unit
// passed from Shape to Break and to each engine's line emitter.
type Glyph struct {
	Outline             *render.Path // em units, Y up; nil for whitespace/missing ink/atomic
	Advance             float64      // points
	Color               Color
	SizePt              float64
	AscentPt, DescentPt float64
	LineGapPt           float64
	Space               bool        // a break opportunity; excluded from a line's trailing width
	Break               bool        // a hard line break (no ink)
	Atomic              *AtomicItem // non-nil => atomic box occupying Advance width
}

// Color is the package's own RGBA so the public glyph type carries no image/color
// dependency; it is the same four-uint8 layout as image/color.RGBA (no size win),
// and each engine converts to/from image/color at emit time.
type Color struct{ R, G, B, A uint8 }

// Shape turns styled runs into a flat slice of shaped glyphs, resolving each run's
// face through faces and measuring every rune at the run's size. A run whose family
// has no bundled face is skipped (logged via logf); a rune the face cannot map is
// skipped. A Break run yields one hard-break glyph; an Atomic run yields one atomic
// glyph whose Advance is AtomicItem.WidthPt. A zero-alpha Run.Color is shaped
// opaque. logf may be nil. Shape never panics on malformed input.
func Shape(faces *layoutfont.FaceCache, runs []Run, logf func(string, ...any)) []Glyph {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var out []Glyph
	for _, r := range runs {
		if r.Break {
			out = append(out, Glyph{Break: true})
			continue
		}
		if r.Atomic != nil {
			out = append(out, Glyph{Advance: r.Atomic.WidthPt, Atomic: r.Atomic})
			continue
		}
		style := pkgfont.Style{Bold: r.Bold, Italic: r.Italic}
		face, ok := faces.Resolve(r.Family, style)
		if !ok {
			logf("layout: no font for family %q; skipping run", r.Family)
			continue
		}
		asc, desc, gap := face.Metrics()
		col := Color{R: r.Color.R, G: r.Color.G, B: r.Color.B, A: r.Color.A}
		if r.Color.A == 0 {
			col.A = 0xff // a zero-alpha color is unset; treat as opaque
		}
		for _, rn := range r.Text {
			outline, advEm, ok := face.Glyph(rn)
			if !ok {
				continue
			}
			out = append(out, Glyph{
				Outline:   outline,
				Advance:   advEm * r.SizePt,
				Color:     col,
				SizePt:    r.SizePt,
				AscentPt:  asc * r.SizePt,
				DescentPt: desc * r.SizePt,
				LineGapPt: gap * r.SizePt,
				Space:     rn == ' ' || rn == '\t',
			})
		}
	}
	return out
}
