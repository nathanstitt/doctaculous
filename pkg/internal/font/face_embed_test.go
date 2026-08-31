package font

import "testing"

func TestFaceProgramBytesAndGID(t *testing.T) {
	face, ok := LoadStandard("Helvetica", Style{})
	if !ok {
		t.Fatal("LoadStandard Helvetica: not available")
	}
	gid, ok := face.GID('A')
	if !ok || gid == 0 {
		t.Fatalf("GID('A') = %d, %v; want nonzero, true", gid, ok)
	}
	data, kind := face.ProgramBytes()
	if len(data) == 0 {
		t.Fatal("ProgramBytes returned empty data")
	}
	if kind == ProgramKindUnknown {
		t.Fatal("ProgramBytes returned unknown kind")
	}
	if upm := face.UnitsPerEm(); upm <= 0 {
		t.Fatalf("UnitsPerEm = %v; want > 0", upm)
	}
	if adv := face.GlyphAdvance(gid); adv <= 0 {
		t.Fatalf("GlyphAdvance(%d) = %v; want > 0", gid, adv)
	}
	if face.Outline(gid) == nil {
		t.Fatal("Outline returned nil for 'A'")
	}
}

// TestFaceProgramKindForBundledFaces locks down the program kind of the bundled
// substitutes: the default sans/serif faces are classic Type1 (PFB) and the
// monospace face is TrueType. The PDF writer branches on this to pick the
// embedding path (/FontFile vs /FontFile2).
func TestFaceProgramKindForBundledFaces(t *testing.T) {
	cases := []struct {
		family string
		want   ProgramKind
	}{
		{"Helvetica", ProgramKindType1},
		{"Times", ProgramKindType1},
		{"Courier", ProgramKindTrueType},
	}
	for _, tc := range cases {
		face, ok := LoadStandard(tc.family, Style{})
		if !ok {
			t.Fatalf("LoadStandard(%q): not available", tc.family)
		}
		if _, kind := face.ProgramBytes(); kind != tc.want {
			t.Errorf("%s: ProgramBytes kind = %v; want %v", tc.family, kind, tc.want)
		}
	}
}
