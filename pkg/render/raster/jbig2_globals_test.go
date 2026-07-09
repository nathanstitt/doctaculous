package raster

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// TestJBIG2ParmsDict_Array verifies that jbig2ParmsDict finds the parameter dict
// carrying /JBIG2Globals when /DecodeParms is the parallel-array form produced by a
// multi-filter chain (e.g. /Filter [/FlateDecode /JBIG2Decode]). The array holds one
// parm dict per filter; only the JBIG2Decode element carries /JBIG2Globals, and the
// other slots may be null. Historically GetDict returned nil for the array and the
// globals were silently lost.
func TestJBIG2ParmsDict_Array(t *testing.T) {
	doc := &pdf.Document{}
	globalsRef := pdf.Reference{Number: 5}

	// Array form: parallel to /Filter [/FlateDecode /JBIG2Decode]. First slot is the
	// FlateDecode parms (null here), second slot carries the JBIG2 globals.
	parms := pdf.Array{
		pdf.Null{},
		pdf.Dict{"JBIG2Globals": globalsRef},
	}

	got := jbig2ParmsDict(doc, parms)
	if got == nil {
		t.Fatalf("jbig2ParmsDict returned nil for array-form /DecodeParms; want the JBIG2 parm dict")
	}
	if got["JBIG2Globals"] != globalsRef {
		t.Fatalf("jbig2ParmsDict returned wrong dict: %#v", got)
	}
}

// TestJBIG2ParmsDict_SingleDict verifies the lone-JBIG2Decode form (a single parm dict)
// still resolves, so the fix is additive for the pre-existing single-filter path.
func TestJBIG2ParmsDict_SingleDict(t *testing.T) {
	doc := &pdf.Document{}
	globalsRef := pdf.Reference{Number: 7}
	parms := pdf.Dict{"JBIG2Globals": globalsRef}

	got := jbig2ParmsDict(doc, parms)
	if got == nil {
		t.Fatalf("jbig2ParmsDict returned nil for single-dict /DecodeParms")
	}
	if got["JBIG2Globals"] != globalsRef {
		t.Fatalf("jbig2ParmsDict returned wrong dict: %#v", got)
	}
}

// TestJBIG2ParmsDict_None verifies both forms return nil when no element carries
// /JBIG2Globals (e.g. a plain FlateDecode-only image), so the caller yields nil globals.
func TestJBIG2ParmsDict_None(t *testing.T) {
	doc := &pdf.Document{}
	if got := jbig2ParmsDict(doc, pdf.Dict{"Predictor": pdf.Integer(12)}); got != nil {
		t.Fatalf("jbig2ParmsDict(single dict without JBIG2Globals) = %#v; want nil", got)
	}
	if got := jbig2ParmsDict(doc, pdf.Array{pdf.Null{}, pdf.Dict{"Columns": pdf.Integer(1)}}); got != nil {
		t.Fatalf("jbig2ParmsDict(array without JBIG2Globals) = %#v; want nil", got)
	}
	if got := jbig2ParmsDict(doc, nil); got != nil {
		t.Fatalf("jbig2ParmsDict(nil) = %#v; want nil", got)
	}
}
