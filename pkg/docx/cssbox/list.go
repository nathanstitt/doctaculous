package cssbox

import (
	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// listCounter tracks per-(numId, ilvl) counter values as paragraphs are lowered
// in document order. Advancing a level resets all deeper levels (CSS/Word list
// nesting semantics).
type listCounter struct {
	// counts[numID][ilvl] = current value at that level.
	counts map[int]map[int]int
}

func newListCounter() *listCounter { return &listCounter{counts: map[int]map[int]int{}} }

// next increments the (numID, ilvl) counter, resets deeper levels, and returns
// the new value at ilvl.
func (c *listCounter) next(numID, ilvl int) int {
	m := c.counts[numID]
	if m == nil {
		m = map[int]int{}
		c.counts[numID] = m
	}
	m[ilvl]++
	for deeper := range m {
		if deeper > ilvl {
			delete(m, deeper)
		}
	}
	return m[ilvl]
}

// lowerListParagraph lowers a numbered paragraph into a DisplayListItem box with
// a resolved marker. It reuses lowerParagraph for the item's inline content, then
// overlays list-item display + the marker. A page break inside the paragraph
// (multiple blocks) keeps the marker only on the first block.
//
// The DOCX render path never runs the HTML counter pass (pkg/layout/css/counters.go
// resolveCounters), which is what makes a marker visible on the HTML side by
// prepending a counter text box. So we mirror that here: after recording the marker
// on the box, we prepend the marker string as a leading inline text box on the first
// block so it actually renders in front of the item content.
func lowerListParagraph(p *docx.Paragraph, r *style.Resolver, num *docx.Numbering, rels map[string]docx.Relationship, ctr *listCounter) []*lcssbox.Box {
	blocks := lowerParagraph(p, r, rels)
	if len(blocks) == 0 {
		return blocks
	}
	first := blocks[0]
	first.Display = lcssbox.DisplayListItem
	first.Style.Display = "list-item" // match Box.Display (Style.Display is unread by layout, but reads clearly)
	// markerText advances the list counter as a side effect — call it exactly once.
	mt := markerText(p.Props, num, ctr)
	first.Marker = &lcssbox.MarkerContent{Text: mt, Outside: true}
	if mt != "" {
		// Style the marker from the paragraph's effective (default) run formatting so
		// it shares the item's font/size/color and sits on the item's first line.
		er := r.EffectiveRun(p.Props, docx.RunProps{})
		marker := runTextBox(mt, er, first.Style)
		first.Children = append([]*lcssbox.Box{marker}, first.Children...)
	}
	return blocks
}

// markerText resolves a paragraph's list marker string ("1. ", "• ", "a. ").
// The counter is advanced for numbered formats; a bullet uses the lvlText glyph
// verbatim. An unknown numId falls back to a bullet.
func markerText(pp docx.ParagraphProps, num *docx.Numbering, ctr *listCounter) string {
	lvl, ok := num.Level(pp.NumID, pp.ILvl)
	if !ok {
		return "• "
	}
	switch lvl.Format {
	case docx.NumFmtBullet:
		glyph := lvl.Text
		if glyph == "" {
			glyph = "•"
		}
		return glyph + " "
	case docx.NumFmtNone:
		return ""
	default:
		val := ctr.next(pp.NumID, pp.ILvl)
		return formatMarker(lvl, val)
	}
}

// formatMarker substitutes the level's counter value into the lvlText pattern.
// OOXML lvlText uses %N placeholders (N = 1-based level); we resolve %(ilvl+1)
// with the current value formatted per the level's numFmt, and append a trailing
// space so the marker reads as "1. ". Other %M placeholders (parent levels) are
// dropped in this slice (multi-level "1.2." numbering is a follow-up).
func formatMarker(lvl docx.NumLevel, value int) string {
	num := gcss.FormatCounter(value, cssListStyle(lvl.Format))
	// Replace the first %N run in the pattern with the formatted number; keep the
	// literal suffix (e.g. the "." in "%1.").
	out := replaceFirstPlaceholder(lvl.Text, num)
	if out == "" {
		// A pattern with no %N placeholder (e.g. a literal "Note") is malformed for a
		// numbered level; fall back to "N." rather than dropping the number. The
		// authored literal is intentionally not preserved (a rare degenerate case).
		out = num + "."
	}
	return out + " "
}

// replaceFirstPlaceholder replaces the first "%<digit>" token in pattern with num,
// leaving surrounding literals intact. If there is no placeholder, returns "".
//
// LIMITATION (single-level only): it substitutes num for the FIRST %<digit> found,
// ignoring which level the digit names. This is correct for single-level markers
// ("%1." / "%2."), but multi-level patterns ("%1.%2.") need each %N resolved against
// level N-1's counter — a follow-up when multi-level numbering lands (make this
// level-aware / rename to replaceLevelPlaceholder then).
func replaceFirstPlaceholder(pattern, num string) string {
	for i := 0; i+1 < len(pattern); i++ {
		if pattern[i] == '%' && pattern[i+1] >= '0' && pattern[i+1] <= '9' {
			return pattern[:i] + num + pattern[i+2:]
		}
	}
	return ""
}

// cssListStyle maps a DOCX NumFmt onto the CSS list-style-type keyword that
// pkg/css.FormatCounter understands.
func cssListStyle(f docx.NumFmt) string {
	switch f {
	case docx.NumFmtLowerRoman:
		return "lower-roman"
	case docx.NumFmtUpperRoman:
		return "upper-roman"
	case docx.NumFmtLowerLetter:
		return "lower-alpha"
	case docx.NumFmtUpperLetter:
		return "upper-alpha"
	default:
		return "decimal"
	}
}
