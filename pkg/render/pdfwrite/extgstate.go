package pdfwrite

import "fmt"

// pendingExtGState is a named /ExtGState dictionary referenced by a page's
// content stream, recorded so the document assembler can register it in the
// page's /ExtGState resource sub-dictionary. This mirrors pendingShading's role
// for /Shading dictionaries (see shading.go).
type pendingExtGState struct {
	name string // resource name used in the content stream ("GS0", "GS1", ...)
	dict Dict   // the ExtGState dictionary (/ca, /CA, and/or /BM)
}

// extGState describes the subset of ExtGState parameters this writer emits:
// constant non-stroking/stroking alpha and a blend mode. A zero value (Normal
// blend, both alphas absent) means "no state change needed" — callers must
// check needed() before allocating a resource for it.
type extGState struct {
	hasFillAlpha   bool
	fillAlpha      float64 // /ca
	hasStrokeAlpha bool
	strokeAlpha    float64 // /CA
	blendMode      string  // /BM; "" or "Normal" means omit (source-over, the default)
}

// needed reports whether g differs from the PDF default graphics state (fully
// opaque, Normal blend) and therefore requires a "/GSn gs" operator. Alpha
// 1.0 and an empty/"Normal" blend mode emit NOTHING — this is the invariant
// that keeps a fully-opaque document's output byte-identical to a writer with
// no ExtGState support at all.
func (g extGState) needed() bool {
	if g.hasFillAlpha && g.fillAlpha < 1 {
		return true
	}
	if g.hasStrokeAlpha && g.strokeAlpha < 1 {
		return true
	}
	if g.blendMode != "" && g.blendMode != "Normal" {
		return true
	}
	return false
}

// dict builds the PDF dictionary for g. Only fields actually set are written,
// per PDF's own ExtGState semantics (an absent key means "unchanged").
func (g extGState) dict() Dict {
	d := Dict{}
	if g.hasFillAlpha {
		d["ca"] = Real(g.fillAlpha)
	}
	if g.hasStrokeAlpha {
		d["CA"] = Real(g.strokeAlpha)
	}
	if g.blendMode != "" && g.blendMode != "Normal" {
		d["BM"] = Name(g.blendMode)
	}
	return d
}

// emitGState writes a "/GSn gs" operator for g into the content stream if (and
// only if) g.needed(), reusing an already-emitted resource describing the same
// state (extGState is a plain comparable struct, so it is used directly as the
// dedup key) rather than allocating a new one. It is a no-op for the fully-
// opaque/Normal-blend default, which is what keeps unaffected output byte-
// identical.
func (d *pageDevice) emitGState(g extGState) {
	if !g.needed() {
		return
	}
	name, ok := d.extGStateNames[g]
	if !ok {
		name = fmt.Sprintf("GS%d", len(d.extGStates))
		if d.extGStateNames == nil {
			d.extGStateNames = map[extGState]string{}
		}
		d.extGStateNames[g] = name
		d.extGStates = append(d.extGStates, pendingExtGState{name: name, dict: g.dict()})
	}
	fmt.Fprintf(&d.buf, "/%s gs\n", name)
}
