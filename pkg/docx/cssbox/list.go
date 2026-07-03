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
func lowerListParagraph(p *docx.Paragraph, r *style.Resolver, num *docx.Numbering, ctr *listCounter) []*lcssbox.Box {
	blocks := lowerParagraph(p, r)
	if len(blocks) == 0 {
		return blocks
	}
	first := blocks[0]
	first.Display = lcssbox.DisplayListItem
	first.Style.Display = "list-item"
	first.Marker = &lcssbox.MarkerContent{Text: markerText(p.Props, num, ctr), Outside: true}
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
		out = num + "."
	}
	return out + " "
}

// replaceFirstPlaceholder replaces the first "%<digit>" token in pattern with num,
// leaving surrounding literals intact. If there is no placeholder, returns "".
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
