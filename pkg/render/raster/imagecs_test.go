package raster

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/pdf"
)

// TestResolveImageCSArrayMalformedNoPanic pins the adversarial-review finding that a
// malformed single-element array color space ("[/ICCBased]" or "[/DeviceN]") indexed
// arr[1] out of range — a panic that, in a render-worker goroutine with no recover, was
// process-fatal. The length guard must degrade to a device space instead. (A direct,
// non-reference array resolves without dereferencing the document, so a nil doc suffices.)
func TestResolveImageCSArrayMalformedNoPanic(t *testing.T) {
	cases := []pdf.Array{
		{pdf.Name("ICCBased")},
		{pdf.Name("DeviceN")},
		{pdf.Name("Indexed")}, // resolveIndexedCS guards len<4 separately
		{pdf.Name("ICCBased")},
	}
	for _, arr := range cases {
		// Must not panic; returns a usable fallback color space (or an error for Indexed).
		cs, err := resolveImageCSArray(nil, arr, 8, nil)
		_ = cs
		_ = err
		// The ICCBased/DeviceN single-element forms degrade to RGB with no error.
		fam, _ := arr[0].(pdf.Name)
		if fam == "ICCBased" || fam == "DeviceN" {
			if err != nil {
				t.Errorf("%v: unexpected error %v (should degrade, not error)", arr, err)
			}
			if cs.nComps == 0 {
				t.Errorf("%v: degraded color space has 0 components", arr)
			}
		}
	}
}
