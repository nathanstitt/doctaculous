package cssbox

import (
	"strconv"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// MediaLoader builds an in-memory ResourceLoader that resolves a drawing's
// relationship id (the "src" set on the replaced image box) to the bytes of the
// media part that relationship targets. A document with no media yields an empty
// loader (every Load misses -> the engine paints a placeholder). Content type is
// left "" so the engine sniffs the format from the bytes.
func MediaLoader(d *docx.Document) resource.MapLoader {
	m := resource.MapLoader{}
	if d == nil {
		return m
	}
	for id, rel := range d.Rels {
		if rel.External {
			continue
		}
		if data, ok := d.Media[rel.Target]; ok {
			m[id] = resource.Resource{Data: data}
		}
	}
	return m
}

// emuPerPt is the EMU-to-point conversion (914400 EMU = 1 inch = 72 pt).
const emuPerPt = 12700

// drawingBox lowers a DOCX drawing into an inline replaced image box. src is the
// rel id (resolved by MediaLoader); the extent (EMU) becomes width/height point
// attributes so the CSS replaced-sizing uses them (CSS width/height would
// override, but a DOCX drawing has no CSS). The box inherits the paragraph style
// so it flows inline.
func drawingBox(dr *docx.Drawing, para gcss.ComputedStyle) *lcssbox.Box {
	cs := para
	cs.Display = "inline-block"
	attrs := map[string]string{"src": dr.RelID}
	if dr.WidthEMU > 0 {
		attrs["width"] = strconv.Itoa(int(dr.WidthEMU / emuPerPt))
	}
	if dr.HeightEMU > 0 {
		attrs["height"] = strconv.Itoa(int(dr.HeightEMU / emuPerPt))
	}
	return &lcssbox.Box{
		Kind:     lcssbox.BoxReplaced,
		Display:  lcssbox.DisplayInlineBlock,
		Style:    cs,
		Replaced: &lcssbox.ReplacedContent{Tag: "img", Attrs: attrs},
	}
}
