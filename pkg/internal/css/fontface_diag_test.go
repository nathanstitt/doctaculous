package css

import (
	"strings"
	"testing"
)

// TestFontFaceDescriptorsReported covers the two @font-face descriptors this
// parser recognizes but does not implement.
//
// Before this they were dropped by the switch with no record at all, so an
// author who subset a family across several faces with unicode-range got the
// whole face used for every rune and nothing to explain why. The backlog
// described unicode-range as "captured-but-ignored", which overstated it: it was
// not captured.
func TestFontFaceDescriptorsReported(t *testing.T) {
	sheet := Parse(`@font-face {
		font-family: Subset;
		src: url(latin.woff2);
		unicode-range: U+0000-00FF;
		font-display: swap;
	}`)

	if len(sheet.FontFaces) != 1 {
		t.Fatalf("faces = %d, want 1 (the face must still load)", len(sheet.FontFaces))
	}
	if got := sheet.FontFaces[0].Family; got != "Subset" {
		t.Errorf("family = %q, want Subset", got)
	}

	want := map[string]string{
		unsupportedUnicodeRange: "U+0000-00FF",
		unsupportedFontDisplay:  "swap",
	}
	got := map[string]string{}
	for _, u := range sheet.Unsupported {
		got[u.Construct] = u.Selector
		if !u.Descriptor {
			t.Errorf("%s: Descriptor = false, want true (it is a descriptor, not a selector)", u.Construct)
		}
	}
	for c, v := range want {
		if got[c] != v {
			t.Errorf("%s: recorded value %q, want %q", c, got[c], v)
		}
	}
}

// TestFontFaceSupportedDescriptorsSilent guards against the diagnostic becoming
// noise: a face using only descriptors this parser implements must report
// nothing.
func TestFontFaceSupportedDescriptorsSilent(t *testing.T) {
	sheet := Parse(`@font-face {
		font-family: Plain;
		src: url(p.woff2) format("woff2"), local("Plain");
		font-weight: 700;
		font-style: italic;
	}`)
	if len(sheet.Unsupported) != 0 {
		t.Errorf("a fully-supported face reported %d diagnostics: %+v",
			len(sheet.Unsupported), sheet.Unsupported)
	}
	ff := sheet.FontFaces[0]
	if ff.Weight != "700" || ff.Style != "italic" || len(ff.Sources) != 2 {
		t.Errorf("supported descriptors were not parsed: %+v", ff)
	}
}

// TestFontFaceDiagnosticDedupes pins the warn-once contract: a stylesheet with
// many subsetted faces (the normal way unicode-range is used) must record the
// construct once, not once per face.
func TestFontFaceDiagnosticDedupes(t *testing.T) {
	var b strings.Builder
	for i := range 50 {
		b.WriteString(`@font-face { font-family: S; src: url(s.woff2);` +
			` unicode-range: U+` + string(rune('0'+i%10)) + `000-` + string(rune('0'+i%10)) + `0FF; }`)
	}
	sheet := Parse(b.String())
	if len(sheet.FontFaces) != 50 {
		t.Fatalf("faces = %d, want 50", len(sheet.FontFaces))
	}
	// Distinct values are distinct records, but the cap must hold and the
	// construct name must be shared so a caller warns once per construct.
	if len(sheet.Unsupported) > maxUnsupportedSelectors {
		t.Errorf("records = %d, past the %d cap", len(sheet.Unsupported), maxUnsupportedSelectors)
	}
	for _, u := range sheet.Unsupported {
		if u.Construct != unsupportedUnicodeRange {
			t.Errorf("unexpected construct %q", u.Construct)
		}
	}
}

// TestFontFaceDroppedRuleReportsNothing covers a rule the caller discards
// entirely: with no family or src there is no face, and warning about a
// descriptor on a rule that never applied would be misleading.
func TestFontFaceDroppedRuleReportsNothing(t *testing.T) {
	sheet := Parse(`@font-face { unicode-range: U+0-7F; font-display: block; }`)
	if len(sheet.FontFaces) != 0 {
		t.Fatalf("faces = %d, want 0 (no family, no src)", len(sheet.FontFaces))
	}
	if len(sheet.Unsupported) != 0 {
		t.Errorf("a dropped rule reported %d diagnostics: %+v",
			len(sheet.Unsupported), sheet.Unsupported)
	}
}
