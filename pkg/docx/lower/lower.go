// Package lower converts a parsed DOCX document into the format-neutral
// box.Document the reflow engine consumes. It is the only seam between DOCX and
// the shared engine: it resolves each paragraph and run through the style cascade
// and emits resolved points/colors/family names, so nothing DOCX-specific crosses
// the boundary. It lives in its own package (rather than pkg/docx) to avoid an
// import cycle with pkg/docx/style, which depends on pkg/docx.
package lower

import (
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	"github.com/nathanstitt/doctaculous/pkg/layout/box"
)

// Document converts a parsed DOCX document into a box.Document. The resolver must
// be built from the same document (style.NewResolver(d, ...)). A nil document or
// nil resolver yields an empty box.Document rather than panicking.
func Document(d *docx.Document, r *style.Resolver) box.Document {
	if d == nil || r == nil {
		return box.Document{}
	}
	out := box.Document{Page: lowerPage(d.Section)}
	for _, blk := range d.Body {
		if blk.Paragraph == nil {
			continue
		}
		out.Blocks = append(out.Blocks, lowerParagraph(blk.Paragraph, r)...)
	}
	return out
}

// lowerPage converts a section's twip geometry into the engine's point geometry.
func lowerPage(s docx.SectionProps) box.PageGeometry {
	return box.PageGeometry{
		WidthPt:        s.PageW.Points(),
		HeightPt:       s.PageH.Points(),
		MarginTopPt:    s.MarginTop.Points(),
		MarginBottomPt: s.MarginBottom.Points(),
		MarginLeftPt:   s.MarginLeft.Points(),
		MarginRightPt:  s.MarginRight.Points(),
	}
}

// lowerParagraph resolves a paragraph's effective formatting and lowers its runs
// into inline spans. A page break inside a run splits the paragraph into two
// blocks, the second carrying BreakBefore, so flow continues onto a new page.
func lowerParagraph(p *docx.Paragraph, r *style.Resolver) []box.Block {
	eff := r.EffectiveParagraph(p.Props)
	base := box.Block{
		Align:         lowerAlign(eff.Justify),
		LineHeight:    lowerLineHeight(eff),
		SpaceBeforePt: eff.SpaceBeforePt,
		SpaceAfterPt:  eff.SpaceAfterPt,
		IndentLeftPt:  eff.IndentLeftPt,
		IndentRightPt: eff.IndentRightPt,
		FirstLinePt:   eff.FirstLinePt,
		BreakBefore:   eff.PageBreakBefore,
	}

	var blocks []box.Block
	cur := base
	for _, run := range p.Runs {
		switch run.Break {
		case docx.BreakPage:
			blocks = append(blocks, cur)
			cur = base
			cur.BreakBefore = true
			cur.Inlines = nil
			continue
		case docx.BreakLine, docx.BreakColumn:
			cur.Inlines = append(cur.Inlines, box.Inline{ForceBreak: true})
			continue
		}
		if run.Text == "" {
			continue
		}
		er := r.EffectiveRun(p.Props, run.Props)
		cur.Inlines = append(cur.Inlines, box.Inline{
			Text:      run.Text,
			Face:      box.FaceRef{Family: er.Family, Bold: er.Bold, Italic: er.Italic},
			SizePt:    er.SizePt,
			Color:     er.Color,
			Underline: er.Underline,
		})
	}
	blocks = append(blocks, cur)
	return blocks
}

// lowerAlign maps a DOCX Justify to a box.Align.
func lowerAlign(j docx.Justify) box.Align {
	switch j {
	case docx.JustifyCenter:
		return box.AlignCenter
	case docx.JustifyRight:
		return box.AlignRight
	case docx.JustifyBoth:
		return box.AlignJustify
	default:
		return box.AlignLeft
	}
}

// lowerLineHeight maps the effective line-spacing into the box model. Auto
// spacing carries a multiplier (the engine applies its ~1.15 default when the
// multiplier is unset); exact/atLeast carry a point value.
func lowerLineHeight(eff style.EffectiveParagraph) box.LineHeight {
	if !eff.HasLine {
		return box.LineHeight{Mode: box.LineHeightAuto} // engine default multiplier
	}
	switch eff.LineRule {
	case docx.LineRuleExact:
		return box.LineHeight{Mode: box.LineHeightExact, ValuePt: eff.LineValue}
	case docx.LineRuleAtLeast:
		return box.LineHeight{Mode: box.LineHeightAtLeast, ValuePt: eff.LineValue}
	default:
		// Auto: LineValue is in 240ths of a line (240 = single).
		mult := eff.LineValue / 240
		if mult <= 0 {
			mult = 0 // let the engine apply its default
		}
		return box.LineHeight{Mode: box.LineHeightAuto, Mult: mult}
	}
}
