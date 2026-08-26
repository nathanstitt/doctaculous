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
