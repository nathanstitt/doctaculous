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
	// WhiteSpace is the run's CSS white-space value ("normal" | "nowrap" | "pre" |
	// "pre-wrap" | "pre-line"). The empty string means "normal" — the historical
	// behavior — so a caller (e.g. the DOCX engine) that never sets it is unaffected.
	// In a preserving mode (pre/pre-wrap/pre-line) a '\n' in Text becomes a hard
	// break and a '\t' advances to the next tab stop; box generation has already
	// collapsed whitespace for the non-preserving modes, so Text arrives pre-collapsed
	// there.
	WhiteSpace string
	// Underline marks a run whose text has text-decoration: underline. It is carried
	// opaquely onto each shaped glyph (Glyph.Underline) for the engine to paint; the
	// shaper itself does nothing with it. The zero value (false) is the historical
	// behavior, so a caller (e.g. the DOCX engine) that never sets it is unaffected.
	Underline bool
}

// AtomicItem is an inline-level box that participates in a line as one unbreakable
// unit of a fixed width. The IFC lays out its own fragment separately; the line
// only needs its advance and baseline placement. Carried opaquely through shaping.
//
// WidthPt is the item's full inline advance INCLUDING its horizontal margins;
// MarginLeftPt is the left margin within that advance, so the IFC offsets the
// item's border box past it when placing the kept fragment. HeightPt and BaselinePt
// describe the margin box's vertical extent and the baseline it rests on. The core
// uses only WidthPt (for breaking/placement) and the vertical metrics (for line-box
// sizing); MarginLeftPt, BaselinePt, and Ref are read by the IFC at emit time.
type AtomicItem struct {
	WidthPt, HeightPt float64
	MarginLeftPt      float64 // left margin within WidthPt; the IFC shifts the box past it
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
	// NoWrap marks a glyph belonging to a non-wrapping run (white-space: nowrap/pre):
	// the breaker must not take a soft (width) break at or before it. A Space with
	// NoWrap set is still a space for width/trailing purposes but is NOT a break
	// opportunity, so a nowrap inline span stays on one line even inside a wrapping
	// block.
	NoWrap bool
	// Underline carries the run's text-decoration: underline onto the glyph, for the
	// engine's line emitter to paint as an underline rule. The shaper does not act on
	// it. Zero (false) for callers that don't set Run.Underline (e.g. DOCX).
	Underline bool
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
	// lineCol tracks the current column x-position (points) since the last line start,
	// used to compute tab-stop advances. It re-bases to 0 at each hard break.
	lineCol := 0.0
	for _, r := range runs {
		if r.Break {
			lineCol = 0
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
		_, preserveNL, wrap := flagsFor(r.WhiteSpace)
		noWrap := !wrap
		// Space advance (for tab stops) in points: the face's ' ' advance × size.
		spaceAdv := r.SizePt * 0.25 // fallback if the face has no space glyph
		if _, sa, ok := face.Glyph(' '); ok {
			spaceAdv = sa * r.SizePt
		}
		tabStop := tabSize * spaceAdv // width of one tab-stop interval, points
		base := Glyph{Color: col, SizePt: r.SizePt, AscentPt: asc * r.SizePt, DescentPt: desc * r.SizePt, LineGapPt: gap * r.SizePt, NoWrap: noWrap, Underline: r.Underline}
		for _, rn := range r.Text {
			switch {
			case rn == '\n' && preserveNL:
				// A preserved newline becomes a hard break and re-bases the tab column.
				out = append(out, Glyph{Break: true})
				lineCol = 0
			case rn == '\t' && preserveNL:
				// A preserved tab advances to the next tab stop from the current column.
				adv := tabStop
				if tabStop > 0 {
					if a := tabStop - mathMod(lineCol, tabStop); a > 0 {
						adv = a
					}
				}
				g := base
				g.Advance = adv
				g.Space = true
				out = append(out, g)
				lineCol += adv
			default:
				// Ordinary rune (and, in collapsing modes, a stray '\n'/'\t' that box-gen
				// already reduced to a space — shape it as a space).
				if rn == '\n' || rn == '\t' {
					rn = ' '
				}
				outline, advEm, ok := face.Glyph(rn)
				if !ok {
					continue
				}
				g := base
				g.Outline = outline
				g.Advance = advEm * r.SizePt
				g.Space = rn == ' '
				out = append(out, g)
				lineCol += g.Advance
			}
		}
	}
	return out
}

// tabSize is the CSS tab-size used for tab-stop advance in preserving white-space
// modes (the CSS initial value; the tab-size property itself is not yet supported).
const tabSize = 8

// flagsFor decomposes an inline run's white-space value into (collapseSpaces,
// preserveNewlines, wrap). It mirrors css.WhiteSpaceFlags; the inline core keeps its
// own copy so pkg/layout/inline has no dependency on pkg/css. An empty/unknown value
// is "normal".
func flagsFor(ws string) (collapseSpaces, preserveNewlines, wrap bool) {
	switch ws {
	case "nowrap":
		return true, false, false
	case "pre":
		return false, true, false
	case "pre-wrap":
		return false, true, true
	case "pre-line":
		return true, true, true
	default:
		return true, false, true
	}
}

// mathMod returns the non-negative remainder a mod m (m > 0). A local helper to
// avoid importing math for one fmod (the values are small, positive points).
func mathMod(a, m float64) float64 {
	if m <= 0 {
		return 0
	}
	for a >= m {
		a -= m
	}
	for a < 0 {
		a += m
	}
	return a
}
