package doctaculous

import (
	"context"
	"image"
	"image/color"
	"testing"
)

// TestPositionedDescendantInContainerPaints is a regression test for a bug where an
// absolutely- or relatively-positioned descendant of a flex item, grid item, or table
// cell was silently dropped at paint time. Each of those container layouts lays its
// items/cells out as an isolated BFC paint atom, but a static item did not consume the
// relative descendants that bubbled out of its interior (and the item fragment was not
// marked a BFC), so the descendants — and any abs box riding on a relative wrapper —
// never reached a stacking context's positioned layer and were never emitted. The fix:
// each item/cell fragment is marked a BFC and consumes its bubbled relative descendants
// (consumePendingPositioned). Here a 60x60 red abs box sits inside a relative wrapper
// inside each container type; before the fix none of them painted (0 red pixels).
func TestPositionedDescendantInContainerPaints(t *testing.T) {
	red := color.RGBA{0xcc, 0x22, 0x22, 0xff}
	// A relative wrapper holding an abs box (the abs box rides the wrapper's positioned
	// layer; the wrapper is the abs box's containing block).
	const inner = `<div style="position:relative;height:90px">` +
		`<div style="position:absolute;left:0;top:0;width:60px;height:60px;background:#cc2222"></div>` +
		`</div>`
	cases := []struct {
		name string
		html string
	}{
		{"flex-item", `<!DOCTYPE html><html><body style="margin:0">` +
			`<div style="display:flex"><div>` + inner + `</div></div></body></html>`},
		{"flex-direct", `<!DOCTYPE html><html><body style="margin:0">` +
			`<div style="display:flex">` + inner + `</div></body></html>`},
		{"grid-item", `<!DOCTYPE html><html><body style="margin:0">` +
			`<div style="display:grid;grid-template-columns:1fr"><div>` + inner + `</div></div></body></html>`},
		{"table-cell", `<!DOCTYPE html><html><body style="margin:0">` +
			`<table><tr><td>` + inner + `</td></tr></table></body></html>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := OpenHTMLBytes([]byte(tc.html), WithViewportWidth(240))
			if err != nil {
				t.Fatalf("OpenHTMLBytes: %v", err)
			}
			img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: goldenDPI})
			if err != nil {
				t.Fatalf("RasterizePage: %v", err)
			}
			rgba, ok := img.(*image.RGBA)
			if !ok {
				t.Fatalf("image is %T, want *image.RGBA", img)
			}
			// The 60x60 box is 3600px; allow for anti-aliased edges by requiring most of it.
			if n := countColor(rgba, red); n < 3000 {
				t.Errorf("red abs box painted %d px, want ~3600 (the positioned descendant was dropped)", n)
			}
		})
	}
}
