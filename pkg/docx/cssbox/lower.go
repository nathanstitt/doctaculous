// Package cssbox lowers a parsed DOCX document into the recursive cssbox tree the
// CSS layout engine consumes, replacing the flat pkg/docx/lower + pkg/layout/box
// path. It resolves each paragraph and run through the DOCX style cascade and emits
// concrete css.ComputedStyle values, so nothing DOCX-specific crosses the boundary.
// It lives outside pkg/docx to avoid an import cycle with pkg/docx/style.
package cssbox

import (
	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/docx/style"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// PageGeometry is the DOCX section geometry in points, carried alongside the box
// tree (the cssbox tree itself is geometry-free; the engine takes width/height as
// layout inputs).
type PageGeometry struct {
	PageWidthPt, PageHeightPt                                float64
	MarginTopPt, MarginBottomPt, MarginLeftPt, MarginRightPt float64
}

// ContentWidthPt is the page width minus left/right margins (the layout viewport).
func (g PageGeometry) ContentWidthPt() float64 {
	return g.PageWidthPt - g.MarginLeftPt - g.MarginRightPt
}

// ContentHeightPt is the page height minus top/bottom margins (the pagination band).
func (g PageGeometry) ContentHeightPt() float64 {
	return g.PageHeightPt - g.MarginTopPt - g.MarginBottomPt
}

// Geometry resolves a document's section geometry into points. A nil document
// yields the zero geometry.
func Geometry(d *docx.Document) PageGeometry {
	if d == nil {
		return PageGeometry{}
	}
	s := d.Section
	return PageGeometry{
		PageWidthPt:    s.PageW.Points(),
		PageHeightPt:   s.PageH.Points(),
		MarginTopPt:    s.MarginTop.Points(),
		MarginBottomPt: s.MarginBottom.Points(),
		MarginLeftPt:   s.MarginLeft.Points(),
		MarginRightPt:  s.MarginRight.Points(),
	}
}

// Lower converts a parsed DOCX document into a cssbox tree rooted at a block box
// (the <body> analogue). A nil document or resolver yields an empty root rather
// than panicking. Page geometry is obtained separately via Geometry(d).
func Lower(d *docx.Document, r *style.Resolver) *lcssbox.Box {
	root := &lcssbox.Box{Kind: lcssbox.BoxBlock, Display: lcssbox.DisplayBlock, Formatting: lcssbox.BlockFC}
	if d == nil || r == nil {
		return root
	}
	for _, blk := range d.Body {
		if blk.Paragraph == nil {
			continue
		}
		root.Children = append(root.Children, lowerParagraph(blk.Paragraph, r)...)
	}
	return root
}

// lowerParagraph is implemented in the next task; stubbed here.
func lowerParagraph(p *docx.Paragraph, r *style.Resolver) []*lcssbox.Box { return nil }
