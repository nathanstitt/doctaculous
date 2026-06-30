package css

import (
	"github.com/nathanstitt/doctaculous/pkg/layout"
)

// appendMarginBoxes lays out and appends a page's @page margin boxes (running
// headers/footers) to its item list, after the page content so they paint over the
// margin band. g carries the page geometry (size + margins + the resolved UsedPage);
// pageIndex is the zero-based page number and pageCount the total (so counter(page) /
// counter(pages) resolve). It is a no-op when the page has no margin boxes.
//
// (Component 5 implements the body; this stub keeps the pagination wiring buildable.)
func (e *Engine) appendMarginBoxes(items []layout.Item, g pageGeom, pageIndex, pageCount int) []layout.Item {
	return items
}
