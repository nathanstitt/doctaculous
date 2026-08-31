package svg

import "testing"

func TestBuildIndex(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg">
	  <rect id="early" width="1" height="1"/>
	  <defs>
	    <style>.a { fill: red }</style>
	    <rect id="indefs" width="1" height="1"/>
	  </defs>
	  <style type="text/css">.b { fill: blue }</style>
	  <style type="text/nonsense">.c { fill: lime }</style>
	  <g id="early"/>
	</svg>`)
	root, err := parseXML(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	var warns []string
	idx := buildIndex(root, func(key, msg string) { warns = append(warns, key) })

	// Two usable sheets, in document order; the bad-type one is skipped.
	if len(idx.sheets) != 2 {
		t.Fatalf("sheets = %d, want 2 (defs sheet + text/css sheet)", len(idx.sheets))
	}
	if len(idx.sheets[0].Rules) != 1 || idx.sheets[0].Rules[0].Declarations[0].Value != "red" {
		t.Errorf("first sheet should be the one inside <defs>: %+v", idx.sheets[0].Rules)
	}
	if idx.sheets[1].Rules[0].Declarations[0].Value != "blue" {
		t.Errorf("second sheet wrong: %+v", idx.sheets[1].Rules)
	}

	// ids: first occurrence wins, duplicate warns.
	if idx.ids["early"] == nil || idx.ids["early"].local != "rect" {
		t.Errorf("id 'early' should resolve to the first (rect), got %v", idx.ids["early"])
	}
	if idx.ids["indefs"] == nil {
		t.Error("ids must include elements inside <defs>")
	}
	if idx.defs["indefs"] == nil {
		t.Error("defs table missing the defs child")
	}
	if idx.defs["early"] != nil {
		t.Error("defs table must contain only <defs> descendants")
	}
	if len(warns) < 2 {
		t.Errorf("warns = %v, want at least the bad style type and the duplicate id", warns)
	}
}

// TestBuildIndexDefsAgreesWithIDs covers the case where a top-level element
// claims an id first and a later <defs>-scoped element reuses it: ids and
// defs must agree about what the id means, so defs must NOT record the
// defs-scoped element just because it happens to satisfy the "descendant of
// a <defs> with this id" structural predicate. Reproduces the reviewer's
// finding that ids["dup"] and defs["dup"] could previously disagree.
func TestBuildIndexDefsAgreesWithIDs(t *testing.T) {
	src := []byte(`<svg xmlns="http://www.w3.org/2000/svg">
	  <rect id="dup" width="1" height="1"/>
	  <defs>
	    <circle id="dup" r="1"/>
	  </defs>
	</svg>`)
	root, err := parseXML(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	var warns []string
	idx := buildIndex(root, func(key, msg string) { warns = append(warns, key) })

	if idx.ids["dup"] == nil || idx.ids["dup"].local != "rect" {
		t.Fatalf("ids[\"dup\"] should be the first element (rect), got %v", idx.ids["dup"])
	}
	if idx.defs["dup"] != nil {
		t.Errorf("defs[\"dup\"] must be absent, not the rejected defs-scoped circle: got %v", idx.defs["dup"])
	}
	if len(warns) < 1 {
		t.Error("expected a duplicate-id warn for the reused id")
	}
}
