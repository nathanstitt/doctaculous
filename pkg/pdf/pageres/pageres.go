// Package pageres resolves the page-/Resources entries the raster and extract
// backends share: fonts, form XObjects, and their /Matrix and /BBox. Both
// backends implement their own content.Resources type (each needs different
// extra resources — raster also resolves images/shadings/patterns/ExtGState;
// extract needs none of those), but the font and form resolution logic is
// identical, so it lives here as free functions each backend's type calls into.
package pageres

import (
	"github.com/nathanstitt/doctaculous/pkg/font"
	"github.com/nathanstitt/doctaculous/pkg/pdf"
	"github.com/nathanstitt/doctaculous/pkg/pdf/content"
	"github.com/nathanstitt/doctaculous/pkg/render"
)

// ResolveFont resolves res["Font"][name] to a GlyphSource via font.New.
// provider may be nil (bundled substitutes only). Unsupported or malformed
// fonts (non-embedded base-14, classic Type1, non-Identity Type0 CMaps) log
// (via logf, which may be nil) and return nil, so the interpreter advances the
// cursor without drawing.
func ResolveFont(doc *pdf.Document, res pdf.Dict, name string, provider font.Provider, logPrefix string, logf func(string, ...any)) content.GlyphSource {
	fonts := doc.GetDict(res["Font"])
	if fonts == nil {
		return nil
	}
	fontDict := doc.GetDict(fonts[pdf.Name(name)])
	if fontDict == nil {
		return nil
	}
	src, err := font.New(doc, fontDict, provider, logf)
	if err != nil {
		if logf != nil {
			logf(logPrefix+": font %q: %v", name, err)
		}
		return nil
	}
	return src
}

// ResolveForm resolves res["XObject"][name] to a decoded form XObject: its
// content, its child /Resources dict (falling back to res per the spec, since
// a form without its own /Resources inherits the page's so names resolve),
// its /Matrix, and its /BBox. ok=false if name is not a form XObject or its
// content cannot be decoded, so the interpreter skips it gracefully. The
// /Matrix and /BBox degrade per FormMatrix (Identity) and FormBBox (nil).
func ResolveForm(doc *pdf.Document, res pdf.Dict, name string, logPrefix string, logf func(string, ...any)) (data []byte, childRes pdf.Dict, m render.Matrix, bbox *[4]float64, ok bool) {
	xobjs := doc.GetDict(res["XObject"])
	if xobjs == nil {
		return nil, nil, render.Identity, nil, false
	}
	s := doc.GetStream(xobjs[pdf.Name(name)])
	if s == nil {
		return nil, nil, render.Identity, nil, false
	}
	if sub, _ := doc.GetName(s.Dict["Subtype"]); sub != "Form" {
		return nil, nil, render.Identity, nil, false
	}
	data, _, err := doc.DecodedStream(s)
	if err != nil {
		if logf != nil {
			logf(logPrefix+": form %q: %v", name, err)
		}
		return nil, nil, render.Identity, nil, false
	}

	// A form's /Resources is optional; fall back to the page's so names resolve.
	childDict := doc.GetDict(s.Dict["Resources"])
	if childDict == nil {
		childDict = res
	}

	return data, childDict, FormMatrix(doc, s.Dict["Matrix"]), FormBBox(doc, s.Dict["BBox"]), true
}

// FormMatrix reads a 6-number /Matrix array (form XObject, pattern, or shading)
// into a render.Matrix, returning Identity when the entry is absent, malformed,
// or any element is non-numeric — the PDF default.
func FormMatrix(doc *pdf.Document, o pdf.Object) render.Matrix {
	arr := doc.GetArray(o)
	if len(arr) != 6 {
		return render.Identity
	}
	var v [6]float64
	for i, e := range arr {
		f, ok := pdf.Number(doc.Resolve(e))
		if !ok {
			return render.Identity
		}
		v[i] = f
	}
	return render.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
}

// FormBBox reads a form XObject's /BBox [llx lly urx ury] into a normalized
// [minX minY maxX maxY] rectangle, or nil when the array is absent or not 4
// numbers (degrade to no clip). The corners are normalized so min<=max
// regardless of the array's corner order.
func FormBBox(doc *pdf.Document, o pdf.Object) *[4]float64 {
	arr := doc.GetArray(o)
	if len(arr) != 4 {
		return nil
	}
	var v [4]float64
	for i := range v {
		v[i], _ = pdf.Number(doc.Resolve(arr[i]))
	}
	minX, maxX := v[0], v[2]
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY := v[1], v[3]
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	return &[4]float64{minX, minY, maxX, maxY}
}
