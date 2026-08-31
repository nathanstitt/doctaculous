package pdfwrite

import "fmt"

// pendingExtGState is a named /ExtGState dictionary referenced by a page's
// content stream, recorded so the document assembler can register it in the
// page's /ExtGState resource sub-dictionary. This mirrors pendingShading's role
// for /Shading dictionaries (see shading.go).
//
// state (not a pre-built Dict) is carried through to assembly time because a
// non-empty state.smaskFormName names a pending Form XObject (see
// pendingForm in group.go) that does not have a real indirect object
// reference until assemble allocates one — the /ExtGState dict can only be
// finished once that reference exists.
type pendingExtGState struct {
	name  string // resource name used in the content stream ("GS0", "GS1", ...)
	state extGState
}

// extGState describes the subset of ExtGState parameters this writer emits:
// constant non-stroking/stroking alpha, a blend mode, and (for a group's own
// composite operator) a luminosity soft mask. A zero value (Normal blend,
// both alphas absent, no mask) means "no state change needed" — callers must
// check needed() before allocating a resource for it.
type extGState struct {
	hasFillAlpha   bool
	fillAlpha      float64 // /ca
	hasStrokeAlpha bool
	strokeAlpha    float64 // /CA
	blendMode      string  // /BM; "" or "Normal" means omit (source-over, the default)

	// smaskFormName, when non-empty, names a pending luminosity mask Form
	// XObject (see group.go's pendingForm) this state's /SMask should
	// reference. A plain string (not a Ref) keeps extGState comparable so it
	// stays usable as emitGState's dedup map key — the same reason every
	// other pending* resource in this package is name-keyed and resolved to a
	// real indirect reference only at assemble time (see pendingShading,
	// pendingImage).
	smaskFormName string
}

// needed reports whether g differs from the PDF default graphics state (fully
// opaque, Normal blend, no soft mask) and therefore requires a "/GSn gs"
// operator. Alpha 1.0, an empty/"Normal" blend mode, and no mask emit
// NOTHING — this is the invariant that keeps a fully-opaque, unmasked
// document's output byte-identical to a writer with no ExtGState support at
// all.
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
	if g.smaskFormName != "" {
		return true
	}
	return false
}

// dict builds the PDF dictionary for g. Only fields actually set are written,
// per PDF's own ExtGState semantics (an absent key means "unchanged"). The
// /SMask entry itself is filled in by the caller (see buildSMaskDict in
// group.go) once the named form has a real indirect reference; dict() cannot
// resolve smaskFormName to a Ref by itself since extGState carries only the
// name.
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
		d.extGStates = append(d.extGStates, pendingExtGState{name: name, state: g})
	}
	fmt.Fprintf(&d.buf, "/%s gs\n", name)
}
