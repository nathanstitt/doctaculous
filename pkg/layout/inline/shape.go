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
	// Strike marks a run whose text has text-decoration: line-through. Like Underline it
	// is carried opaquely onto each shaped glyph (Glyph.Strike) for the engine to paint
	// (a rule at mid-glyph rather than below the baseline); the shaper does nothing with
	// it. The zero value (false) is the historical behavior, so a caller that never sets
	// it is unaffected.
	Strike bool
	// BaselineShiftPt raises (positive) or lowers (negative) the run's glyphs relative to
	// the line baseline, in points — vertical-align: super/sub. It is carried opaquely
	// onto each shaped glyph (Glyph.BaselineShiftPt); the shaper does nothing with it and
	// it does not affect line-box metrics here. Zero (the default) leaves the run on the
	// baseline, so a caller (e.g. the DOCX engine) that never sets it is unaffected.
	BaselineShiftPt float64
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
	// Strike carries the run's text-decoration: line-through onto the glyph, for the
	// engine's line emitter to paint as a mid-glyph rule. The shaper does not act on it.
	// Zero (false) for callers that don't set Run.Strike (e.g. DOCX).
	Strike bool
	// BaselineShiftPt carries the run's vertical-align: super/sub shift (points, positive
	// = up) onto the glyph, for the engine's line emitter to offset the glyph's paint Y
	// from the line baseline. The shaper does not act on it. Zero for callers that don't
	// set Run.BaselineShiftPt (e.g. DOCX).
	BaselineShiftPt float64
	// Face, GID, and Runes carry font identity for text-emitting backends (the PDF
	// writer embeds Face's program for GID and maps GID -> Runes in /ToUnicode). Face
	// is nil for whitespace/atomic/break glyphs; a rasterizing backend ignores all
	// three and paints Outline.
	Face  *pkgfont.Face
	GID   uint16
	Runes []rune
}

// Color is the package's own RGBA so the public glyph type carries no image/color
// dependency; it is the same four-uint8 layout as image/color.RGBA (no size win),
// and each engine converts to/from image/color at emit time.
type Color struct{ R, G, B, A uint8 }

// Shape turns styled runs into a flat slice of shaped glyphs, resolving each run's
// face through faces and measuring every rune at the run's size. A run whose family
// has no bundled face is skipped (logged via logf); a rune that neither the run's
// face nor any script fallback can map yields a .notdef glyph — the font's own glyph
// 0, or a synthesized tofu box — and is logged once per rune per call. A Break run
// yields one hard-break glyph; an Atomic run yields one atomic glyph whose Advance is
// AtomicItem.WidthPt. A zero-alpha Run.Color is shaped opaque. logf may be nil. Shape
// never panics on malformed input.
func Shape(faces *layoutfont.FaceCache, runs []Run, logf func(string, ...any)) []Glyph {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var out []Glyph
	// warnedRunes records which missing runes have already been logged, so a document
	// with 500 unmappable emoji emits a bounded number of lines instead of 500. It is
	// per-CALL rather than per-engine on purpose: Shape is the only place that knows
	// the rune is missing, it is called once per paragraph/measure pass, and a set
	// scoped here needs no lock and cannot leak between documents. The cost is that a
	// rune missing in several paragraphs logs once per paragraph — bounded by the
	// paragraph count, which is the property that matters, and it keeps the report
	// attached to where the text actually is. (The CSS engine also shapes during
	// intrinsic-width measurement, so a missing rune inside a table cell or flex item
	// can be reported by the measure pass as well as the layout pass — still bounded,
	// and still one line per rune per pass rather than one per occurrence.)
	warnedRunes := map[rune]bool{}
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
		// spaceEm is the face's ' ' advance in EM units, kept unscaled so it can serve
		// both the tab-stop math below and the advance given to an unmapped invisible
		// character — deriving the latter by dividing the scaled value back down would
		// produce NaN at a zero font-size, which SVG's zero-size fixtures do reach.
		spaceEm := 0.25 // fallback if the face has no space glyph
		if _, sa, ok := face.Glyph(' '); ok {
			spaceEm = sa
		}
		spaceAdv := spaceEm * r.SizePt
		tabStop := tabSize * spaceAdv // width of one tab-stop interval, points
		base := Glyph{Color: col, SizePt: r.SizePt, AscentPt: asc * r.SizePt, DescentPt: desc * r.SizePt, LineGapPt: gap * r.SizePt, NoWrap: noWrap, Underline: r.Underline, Strike: r.Strike, BaselineShiftPt: r.BaselineShiftPt, Face: face}
		// Complex-script segments (Arabic and friends) are shaped as whole runs through
		// harfbuzz rather than rune-at-a-time, since a letter's form depends on its
		// neighbours. runes carries the run's text once so segments can be sliced from
		// it; skipTo lets the per-rune loop below jump past a segment already emitted.
		runes := []rune(r.Text)
		skipTo := 0
		for ri := 0; ri < len(runes); ri++ {
			rn := runes[ri]
			if ri < skipTo {
				continue
			}
			if needsComplexShaping(rn) {
				end := ri + 1
				for end < len(runes) && needsComplexShaping(runes[end]) {
					end++
				}
				seg := runes[ri:end]
				// The run's own face may not cover the script (a Latin family with an
				// Arabic phrase in it); fall back to the covering bundled face first, so
				// shaping runs against the face that actually has the glyphs.
				shapeFace := face
				if _, _, ok := face.Glyph(rn); !ok {
					if fb, fbOK := faces.ResolveScriptFallback(rn, style); fbOK {
						shapeFace = fb
					}
				}
				if shaped, ok := shapeComplex(shapeFace, seg); ok {
					for _, sg := range shaped {
						g := base
						g.Face = shapeFace
						g.GID = sg.gid
						g.Outline = shapeFace.Outline(sg.gid)
						g.Advance = sg.advance * r.SizePt
						g.Runes = sg.runes
						out = append(out, g)
						lineCol += g.Advance
					}
					skipTo = end
					continue
				}
				// Shaping unavailable (no layout tables, or it produced nothing): fall
				// through to the per-rune path, which still renders isolated forms.
			}
			switch {
			case rn == '\n' && preserveNL:
				// A preserved newline becomes a hard break and re-bases the tab column.
				// The break glyph carries the run's font metrics so an empty forced line
				// (a blank line in pre/pre-wrap/pre-line) gets a CSS strut height — see
				// the empty-forced-line rule in Break/BreakNextWrap — instead of
				// collapsing to zero height.
				out = append(out, Glyph{Break: true, SizePt: r.SizePt, AscentPt: base.AscentPt, DescentPt: base.DescentPt, LineGapPt: base.LineGapPt})
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
				// A bidi control (LRM/RLM, the embedding/override set, the isolates) draws
				// nothing but DETERMINES ordering, so it must survive shaping as a
				// zero-width glyph — the reorder reads the line's runes, and dropping the
				// control here would silently discard the author's directional intent.
				if isBidiControlRune(rn) {
					g := base
					g.Runes = []rune{rn}
					g.Face = nil // no outline to draw; not a font glyph
					out = append(out, g)
					continue
				}
				outline, advEm, ok := face.Glyph(rn)
				glyphFace := face
				if !ok {
					// The run's family has no glyph for this rune. Before dropping it,
					// try a bundled face that covers its script: each bundled face
					// covers one script, so Hebrew or Arabic inside an otherwise-Latin
					// paragraph resolves here rather than vanishing.
					if fb, fbOK := faces.ResolveScriptFallback(rn, style); fbOK {
						if o, a, gOK := fb.Glyph(rn); gOK {
							outline, advEm, ok, glyphFace = o, a, true, fb
						}
					}
				}
				// missing marks a rune NO face could map AND that would have drawn ink.
				// Two very different cases hide behind an unmapped rune:
				//
				//   - An INVISIBLE character (a space variant like U+202F NARROW NO-BREAK
				//     SPACE, a format control, a variation selector). It draws nothing even
				//     in a font that maps it, so a tofu box would INVENT a mark the author
				//     never asked for — a regression, not a fix. Browsers render an unmapped
				//     invisible character as blank. It gets an advance and no outline, and no
				//     warning: nothing about the page is wrong.
				//   - A VISIBLE character the available fonts genuinely cannot draw. This is
				//     the case .notdef exists for.
				//
				// Distinguishing them is the whole reason invisibleRune exists; without it,
				// turning on .notdef sprays boxes through documents that render correctly
				// today (this repo's own showcase carries a U+202F).
				missing := !ok && !invisibleRune(rn)
				switch {
				case missing:
					// Historically the rune was DROPPED here, which renders as empty space —
					// and because the surrounding text is unaffected, the result reads as a
					// layout bug rather than a font problem. It is worse still when only SOME
					// runes of a set are missing (three of nine weather emoji drawing, six
					// vanishing), which is exactly what happened on a board carrying only
					// DejaVu and Liberation.
					//
					// Draw .notdef instead, as a browser does. Face.NotdefGlyph picks the
					// font's own glyph 0 when it has geometry and synthesizes a tofu box when
					// it does not (the bundled substitutes all ship a BLANK .notdef, so the
					// synthesized box is what usually shows).
					//
					// The mark comes from the RUN's face, not a script fallback: the fallback
					// search has already failed, and the run's face is the one whose size and
					// metrics the rest of the line uses, so its .notdef is the mark that
					// visually belongs in this text.
					outline, advEm = face.NotdefGlyph()
					glyphFace = face
					warnMissingGlyph(logf, warnedRunes, rn, r.Family)
				case !ok:
					// Invisible and unmapped: occupy space, draw nothing. The advance uses the
					// face's space advance — the closest available approximation, and the value
					// the tab-stop code above already trusts for exactly this purpose — so the
					// character still separates its neighbours instead of collapsing the line.
					outline, advEm, glyphFace = nil, spaceEm, face
				}
				g := base
				g.Face = glyphFace
				g.Outline = outline
				g.Advance = advEm * r.SizePt
				g.Space = rn == ' '
				// Carry font identity ONLY for a real font glyph. When GID lookup fails the
				// outline came from a synthesized marker (e.g. a bullet the face lacks) —
				// its GID would be .notdef, so a text-emitting backend must not re-fetch by
				// GID; clear Face so paint fills the synthesized Outline directly.
				// NOTE: this resolves against glyphFace, which for a fallback glyph is the
				// covering script face, not the run's own — a GID is only meaningful
				// against the face it came from.
				//
				// A .notdef glyph takes that same outline route, unconditionally — hence the
				// `missing` guard rather than relying on GID lookup to fail. The reason is on
				// Face.NotdefGlyph: handing a text backend Face+GID 0 would have the PDF
				// writer embed and emit the font's own .notdef, which in every bundled
				// substitute is BLANK, so the box would show in a raster and vanish in a PDF
				// of the same page.
				//
				// Runes IS still set for a .notdef glyph, so the bidi pass sees the real
				// character's class — rather than the U+FFFC placeholder lineText substitutes
				// for a runeless glyph, which reorders differently — and so SVG's
				// glyph-to-character mapping still locates the box.
				switch gid, gidOK := glyphFace.GID(rn); {
				case missing:
					g.Face = nil
					g.Runes = []rune{rn}
				case gidOK:
					g.GID = gid
					g.Runes = []rune{rn}
				default:
					g.Face = nil
				}
				out = append(out, g)
				lineCol += g.Advance
			}
		}
	}
	return out
}

// invisibleRune reports whether r is a character that draws NOTHING even in a font
// that maps it, so an unmappable one must degrade to blank space rather than to a
// .notdef box. Drawing tofu for an invisible character would invent a visible mark
// where the correctly-rendered page has none — the opposite of the fidelity the
// .notdef mark is there to provide.
//
// It covers, by Unicode general category and by name:
//
//   - Zs SPACE SEPARATOR beyond U+0020: the no-break, en/em/thin/hair quad family
//     (U+00A0, U+2000..U+200A), U+202F NARROW NO-BREAK SPACE, U+205F MEDIUM
//     MATHEMATICAL SPACE, and U+3000 IDEOGRAPHIC SPACE. These appear in ordinary
//     well-formed text — U+202F alone occurs in this repo's own showcase — and the
//     bundled substitutes map only a few of them.
//   - Zl/Zp LINE and PARAGRAPH SEPARATOR (U+2028, U+2029).
//   - Cf FORMAT: the zero-width set (U+200B..U+200F), the bidi controls
//     (U+202A..U+202E, U+2060..U+2064, U+2066..U+2069), U+00AD SOFT HYPHEN, the
//     variation selectors (U+FE00..U+FE0F), and U+FEFF ZERO WIDTH NO-BREAK SPACE.
//   - Cc CONTROL (U+0000..U+001F, U+007F..U+009F), which has no printable form.
//
// The bidi controls are listed for completeness; in practice isBidiControlRune
// intercepts them earlier in Shape, since they must keep a zero-width slot for the
// reorder rather than a space-sized advance.
//
// The list is only consulted for a rune NOTHING maps, so a font's own decision wins
// where it has one. U+00AD SOFT HYPHEN is the instructive case: the bundled faces
// DO map it, to a visible hyphen — which is exactly right, since a soft hyphen that
// has been broken at renders as one — so it never reaches this function at all.
//
// Unicode's own tables are not consulted because pkg/layout/inline deliberately
// carries no Unicode-database dependency, and this set is small, stable, and
// exactly the characters a text run actually contains.
func invisibleRune(r rune) bool {
	switch {
	case r <= 0x001F, r >= 0x007F && r <= 0x009F: // Cc control
		return true
	case r == 0x00A0, r == 0x00AD: // NBSP, SOFT HYPHEN
		return true
	case r >= 0x2000 && r <= 0x200F: // en/em/thin/hair spaces, ZWSP..RLM
		return true
	case r == 0x2028, r == 0x2029: // LINE/PARAGRAPH SEPARATOR
		return true
	case r >= 0x202A && r <= 0x202F: // bidi embedding/override, NNBSP
		return true
	case r == 0x205F, r == 0x3000: // MEDIUM MATHEMATICAL, IDEOGRAPHIC SPACE
		return true
	case r >= 0x2060 && r <= 0x2064, r >= 0x2066 && r <= 0x2069: // word joiner, isolates
		return true
	case r >= 0xFE00 && r <= 0xFE0F: // variation selectors
		return true
	case r == 0xFEFF: // ZERO WIDTH NO-BREAK SPACE / BOM
		return true
	}
	return false
}

// warnMissingGlyph logs that rn has no glyph in any face reachable from family, the
// first time this Shape call sees rn. seen is the call's warn-once set; logf is never
// nil here (Shape substitutes a no-op).
//
// The rune is reported as U+XXXX rather than as the character itself, because the
// whole point is that the character does not render — a log line whose diagnostic
// content is an unprintable box in the reader's terminal helps nobody. The family is
// included because it is the actionable half: the fix is almost always to install or
// embed a font for that text, and the family name says which request went unmet.
func warnMissingGlyph(logf func(string, ...any), seen map[rune]bool, rn rune, family string) {
	if seen[rn] {
		return
	}
	seen[rn] = true
	logf("layout: no glyph for U+%04X in family %q or any fallback; drawing .notdef", rn, family)
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
