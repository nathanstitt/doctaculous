package font

import "testing"

// The sign is the trap in this API, so it gets its own test.
//
// textlayout returns a NEGATIVE vertical advance — it negates for a Y-down
// convention, so a 1000-upem face reports -1000 font units. This package normalizes
// to a POSITIVE downward distance in em units, once, at the program adapter. A
// caller that took the raw upstream value would run the pen backwards up the page,
// and nothing would catch it: the magnitude is right, only the direction is wrong,
// so it renders as text marching off the top rather than as an error.
func TestGlyphVAdvanceIsPositiveDownwardEm(t *testing.T) {
	face, ok := LoadStandard("monospace", Style{}) // Inconsolata, a TrueType face
	if !ok {
		t.Fatal("LoadStandard(monospace) failed")
	}
	gid, ok := face.GID('A')
	if !ok {
		t.Fatal("no GID for 'A'")
	}

	adv, ok := face.GlyphVAdvance(gid)
	if !ok {
		t.Fatal("GlyphVAdvance reported no vertical advance for a TrueType face; " +
			"upstream synthesizes one em when vmtx is absent, so this should be true")
	}
	if adv <= 0 {
		t.Fatalf("vertical advance = %v, want POSITIVE (downward). A negative value means "+
			"upstream's Y-down sign leaked through the adapter", adv)
	}
	// Inconsolata has no vmtx, so upstream synthesizes exactly one em.
	if d := adv - 1; d > 0.001 || d < -0.001 {
		t.Errorf("vertical advance = %v em, want ~1 em (synthesized from upem)", adv)
	}
}

// A face with no `vhea` table is the common case for Latin text, and it is NOT an
// error: upstream synthesizes a one-em advance, which is what browsers do. But
// VMetrics must still report ok=false, so a caller can tell an authored vertical
// metric from a fallback. Conflating the two would hide that every Latin face is
// being laid out on a guess.
func TestVMetricsReportsAbsentVheaHonestly(t *testing.T) {
	face, ok := LoadStandard("monospace", Style{})
	if !ok {
		t.Fatal("LoadStandard(monospace) failed")
	}

	_, _, _, hasVhea := face.VMetrics()
	if hasVhea {
		t.Error("VMetrics reported a vhea table for Inconsolata, which has none; " +
			"a synthesized metric must not be presented as authored")
	}

	// The advance is still usable despite the missing table — that is the point.
	gid, _ := face.GID('A')
	if _, ok := face.GlyphVAdvance(gid); !ok {
		t.Error("no vertical advance despite the synthesis fallback; vertical layout " +
			"would have nothing to advance by")
	}
}

// Type1 carries no vertical writing metrics — the format predates them — but it still
// has to yield a USABLE vertical advance, because the bundled sans-serif and serif
// faces are Type1 and they are what nearly all HTML renders with. One em is the
// standard substitute.
//
// This originally asserted the opposite (ok=false, "the format cannot answer"), which
// read as principled but left the DEFAULT text path with no vertical advance at all.
// The distinction worth keeping is authored-vs-synthesized, and VMetrics carries it;
// making the advance unavailable did not add honesty, it just moved the fallback out to
// callers that know less about the font than this package does.
func TestType1SynthesizesAVerticalAdvance(t *testing.T) {
	face, ok := LoadStandard("sans-serif", Style{}) // TeXGyreHeros, a Type1 .pfb
	if !ok {
		t.Fatal("LoadStandard(sans-serif) failed")
	}
	gid, ok := face.GID('A')
	if !ok {
		t.Fatal("no GID for 'A'")
	}

	adv, ok := face.GlyphVAdvance(gid)
	if !ok {
		t.Fatal("no vertical advance for a Type1 face; vertical layout in the default " +
			"font would have nothing to advance by")
	}
	if d := adv - 1; d > 0.001 || d < -0.001 {
		t.Errorf("vertical advance = %v em, want ~1 em (synthesized)", adv)
	}

	// The synthesis must NOT be the horizontal advance: vertical spacing does not vary
	// with how wide a letter is, and 'A' is nowhere near an em wide. This is the
	// assertion that catches the tempting shortcut.
	h := face.GlyphAdvance(gid)
	if h > 0.99 {
		t.Fatalf("fixture is wrong: 'A' has a %v em horizontal advance, too close to 1 "+
			"to distinguish a synthesized vertical advance from a copied horizontal one", h)
	}
	if d := adv - h; d < 0.01 && d > -0.01 {
		t.Errorf("vertical advance %v matches the horizontal advance %v; the synthesis "+
			"is copying the wrong axis", adv, h)
	}

	// Still no AUTHORED metrics — that distinction is the one that matters, and it stays.
	if _, _, _, ok := face.VMetrics(); ok {
		t.Error("Type1 face reported authored vertical metrics; the format has none")
	}
}

// Every bundled generic family must yield a usable vertical advance. This is the test
// that would have caught the original gap: the TrueType case was covered and passed,
// while sans-serif and serif — the actual defaults — silently returned nothing.
func TestEveryGenericFamilyHasAVerticalAdvance(t *testing.T) {
	for _, fam := range []string{"sans-serif", "serif", "monospace", "cursive", "fantasy"} {
		face, ok := LoadStandard(fam, Style{})
		if !ok {
			continue // not bundled; nothing to assert
		}
		gid, ok := face.GID('A')
		if !ok {
			t.Errorf("%s: no GID for 'A'", fam)
			continue
		}
		adv, ok := face.GlyphVAdvance(gid)
		if !ok {
			t.Errorf("%s: no vertical advance; vertical text in this family cannot lay out", fam)
			continue
		}
		if adv <= 0 {
			t.Errorf("%s: vertical advance = %v, want positive downward", fam, adv)
		}
	}
}

// The horizontal metrics must be untouched by this change: every existing caller
// reads them, and a regression there would move all text, not just vertical text.
func TestHorizontalMetricsUnchangedAlongsideVertical(t *testing.T) {
	face, ok := LoadStandard("monospace", Style{})
	if !ok {
		t.Fatal("LoadStandard(monospace) failed")
	}
	gid, _ := face.GID('A')

	if adv := face.GlyphAdvance(gid); adv <= 0 || adv > 1 {
		t.Errorf("horizontal advance = %v em, want a sane positive sub-em value", adv)
	}
	asc, desc, _ := face.Metrics()
	if asc <= 0 || desc <= 0 {
		t.Errorf("Metrics() = asc %v desc %v, want both positive magnitudes", asc, desc)
	}
}
