// Package style resolves WordprocessingML's style cascade into the effective
// run and paragraph properties used by the reflow layout engine.
//
// The cascade has three layers, applied lowest-to-highest:
//
//  1. document defaults (w:docDefaults)
//  2. the named style chain (w:pStyle / w:rStyle, following w:basedOn to the root)
//  3. direct formatting on the run/paragraph (w:rPr / w:pPr)
//
// A property set at a higher layer overrides a lower one; an unset property
// inherits. The Resolver memoizes each style's fully-merged properties so
// per-run resolution during layout is a few map lookups, never a chain walk.
package style

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/docx"
)

// Resolver computes effective properties for a document. Build one per document
// with NewResolver; it is read-only after construction and safe for concurrent
// use.
type Resolver struct {
	styles *docx.Styles
	logf   func(string, ...any)

	// mergedRun/mergedPara hold each style's own properties merged down its
	// basedOn chain (docDefaults excluded), keyed by styleId. Resolved eagerly so
	// EffectiveRun/EffectiveParagraph do no chain walking.
	mergedRun  map[string]docx.RunProps
	mergedPara map[string]docx.ParagraphProps
}

// defaults applied when the document supplies neither a value nor a style: 11pt
// Calibri, black, single line spacing — Word's out-of-the-box Normal style.
const (
	defaultFamily      = "Calibri"
	defaultSizeHalfPts = 22 // 11pt
)

// NewResolver builds a Resolver from a parsed document's styles. A nil styles
// table (no styles part) is handled: every property then falls back to
// docDefaults (empty) and the hardcoded defaults. logf may be nil.
func NewResolver(d *docx.Document, logf func(string, ...any)) *Resolver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	r := &Resolver{
		styles:     d.Styles,
		logf:       logf,
		mergedRun:  map[string]docx.RunProps{},
		mergedPara: map[string]docx.ParagraphProps{},
	}
	if r.styles != nil {
		for id := range r.styles.ByID {
			r.mergedRun[id] = r.resolveStyleRun(id, nil)
			r.mergedPara[id] = r.resolveStylePara(id, nil)
		}
	}
	return r
}

// resolveStyleRun merges a style's run properties down its basedOn chain
// (parent first, child overrides). visited guards against basedOn cycles: a
// styleId already on the path is skipped and logged.
func (r *Resolver) resolveStyleRun(id string, visited map[string]bool) docx.RunProps {
	st, ok := r.styles.ByID[id]
	if !ok {
		return docx.RunProps{}
	}
	if visited[id] {
		r.logf("docx/style: basedOn cycle at style %q; truncating", id)
		return docx.RunProps{}
	}
	if visited == nil {
		visited = map[string]bool{}
	}
	visited[id] = true
	var base docx.RunProps
	if st.BasedOn != "" {
		base = r.resolveStyleRun(st.BasedOn, visited)
	}
	return mergeRun(base, st.Run)
}

// resolveStylePara is resolveStyleRun's paragraph-property analogue.
func (r *Resolver) resolveStylePara(id string, visited map[string]bool) docx.ParagraphProps {
	st, ok := r.styles.ByID[id]
	if !ok {
		return docx.ParagraphProps{}
	}
	if visited[id] {
		r.logf("docx/style: basedOn cycle at style %q; truncating", id)
		return docx.ParagraphProps{}
	}
	if visited == nil {
		visited = map[string]bool{}
	}
	visited[id] = true
	var base docx.ParagraphProps
	if st.BasedOn != "" {
		base = r.resolveStylePara(st.BasedOn, visited)
	}
	return mergePara(base, st.Para)
}

// EffectiveRun is the fully-resolved character formatting for a run, in concrete
// units ready for the layout engine.
type EffectiveRun struct {
	Family                  string
	Bold, Italic, Underline bool
	SizePt                  float64
	Color                   color.RGBA
}

// EffectiveParagraph is the fully-resolved paragraph formatting.
type EffectiveParagraph struct {
	Justify         docx.Justify
	SpaceBeforePt   float64
	SpaceAfterPt    float64
	LineRule        docx.LineRule
	LineValue       float64 // twips for exact/atLeast; 240ths-of-a-line for auto
	HasLine         bool
	IndentLeftPt    float64
	IndentRightPt   float64
	FirstLinePt     float64
	PageBreakBefore bool
}

// EffectiveRun resolves a run's character formatting: docDefaults → paragraph
// style's run props (the linked character formatting of the paragraph style) →
// direct rPr. (A character-style reference on the run, w:rStyle, would layer
// between the paragraph style and direct props; it is not yet modeled.)
func (r *Resolver) EffectiveRun(p docx.ParagraphProps, run docx.RunProps) EffectiveRun {
	var merged docx.RunProps
	if r.styles != nil {
		merged = r.styles.DocDefaultRun
	}
	if styleID := r.effectiveParaStyleID(p); styleID != "" {
		merged = mergeRun(merged, r.mergedRun[styleID])
	}
	merged = mergeRun(merged, run)

	eff := EffectiveRun{
		Family: defaultFamily,
		SizePt: float64(defaultSizeHalfPts) / 2,
		Color:  color.RGBA{A: 0xff},
	}
	if merged.Family != "" {
		eff.Family = merged.Family
	}
	if merged.HasBold {
		eff.Bold = merged.Bold
	}
	if merged.HasItalic {
		eff.Italic = merged.Italic
	}
	if merged.HasUnderline {
		eff.Underline = merged.Underline
	}
	if merged.HasSize {
		eff.SizePt = float64(merged.SizeHalfPts) / 2
	}
	if merged.HasColor {
		eff.Color = merged.Color
	}
	return eff
}

// EffectiveParagraph resolves a paragraph's formatting: docDefaults → paragraph
// style chain → direct pPr.
func (r *Resolver) EffectiveParagraph(p docx.ParagraphProps) EffectiveParagraph {
	var merged docx.ParagraphProps
	if r.styles != nil {
		merged = r.styles.DocDefaultPara
	}
	if styleID := r.effectiveParaStyleID(p); styleID != "" {
		merged = mergePara(merged, r.mergedPara[styleID])
	}
	merged = mergePara(merged, p)

	eff := EffectiveParagraph{
		Justify:         merged.Justify,
		PageBreakBefore: merged.PageBreakBefore,
	}
	if merged.HasSpacingBefore {
		eff.SpaceBeforePt = docx.Twips(merged.SpacingBefore).Points()
	}
	if merged.HasSpacingAfter {
		eff.SpaceAfterPt = docx.Twips(merged.SpacingAfter).Points()
	}
	if merged.HasLine {
		eff.HasLine = true
		eff.LineRule = merged.LineRule
		if merged.LineRule == docx.LineRuleAuto {
			eff.LineValue = float64(merged.Line) // 240ths of a line
		} else {
			eff.LineValue = docx.Twips(merged.Line).Points()
		}
	}
	if merged.HasIndentLeft {
		eff.IndentLeftPt = docx.Twips(merged.IndentLeft).Points()
	}
	if merged.HasIndentRight {
		eff.IndentRightPt = docx.Twips(merged.IndentRight).Points()
	}
	if merged.HasFirstLine {
		eff.FirstLinePt = docx.Twips(merged.FirstLine).Points()
	}
	return eff
}

// effectiveParaStyleID returns the paragraph's explicit style id, or the
// document's default paragraph style id when none is set.
func (r *Resolver) effectiveParaStyleID(p docx.ParagraphProps) string {
	if p.StyleID != "" {
		return p.StyleID
	}
	if r.styles != nil {
		return r.styles.DefaultParaID
	}
	return ""
}

// mergeRun overlays over's set properties onto base, returning the combination.
// A Has* flag on over means that property wins; otherwise base's value is kept.
func mergeRun(base, over docx.RunProps) docx.RunProps {
	out := base
	if over.Family != "" {
		out.Family = over.Family
	}
	if over.HasBold {
		out.Bold, out.HasBold = over.Bold, true
	}
	if over.HasItalic {
		out.Italic, out.HasItalic = over.Italic, true
	}
	if over.HasUnderline {
		out.Underline, out.HasUnderline = over.Underline, true
	}
	if over.HasSize {
		out.SizeHalfPts, out.HasSize = over.SizeHalfPts, true
	}
	if over.HasColor {
		out.Color, out.HasColor = over.Color, true
	}
	return out
}

// mergePara overlays over's set properties onto base.
func mergePara(base, over docx.ParagraphProps) docx.ParagraphProps {
	out := base
	// StyleID is not inherited through the merge (it selects the chain, it is not a
	// formatting property), so it is intentionally left as base's.
	if over.HasJustify {
		out.Justify, out.HasJustify = over.Justify, true
	}
	if over.HasSpacingBefore {
		out.SpacingBefore, out.HasSpacingBefore = over.SpacingBefore, true
	}
	if over.HasSpacingAfter {
		out.SpacingAfter, out.HasSpacingAfter = over.SpacingAfter, true
	}
	if over.HasLine {
		out.Line, out.LineRule, out.HasLine = over.Line, over.LineRule, true
	}
	if over.HasIndentLeft {
		out.IndentLeft, out.HasIndentLeft = over.IndentLeft, true
	}
	if over.HasIndentRight {
		out.IndentRight, out.HasIndentRight = over.IndentRight, true
	}
	if over.HasFirstLine {
		out.FirstLine, out.HasFirstLine = over.FirstLine, true
	}
	if over.PageBreakBefore {
		out.PageBreakBefore = true
	}
	return out
}
