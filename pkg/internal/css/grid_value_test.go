package css

import "testing"

func TestParseTrackListFixed(t *testing.T) {
	tl, ok := parseTrackList("100px 1fr auto")
	if !ok {
		t.Fatal("parseTrackList ok=false")
	}
	tracks := tl.Expand(0) // 0 = no auto-repeat container size; fixed lists ignore it
	if len(tracks) != 3 {
		t.Fatalf("len(tracks)=%d want 3", len(tracks))
	}
	if tracks[0].Min.Kind != TrackLength || tracks[0].Min.Len.Value != 100 || tracks[0].Min.Len.Unit != UnitPx {
		t.Errorf("track0 = %+v want 100px", tracks[0].Min)
	}
	if tracks[1].Max.Kind != TrackFlex || tracks[1].Max.Fr != 1 {
		t.Errorf("track1 = %+v want 1fr", tracks[1].Max)
	}
	if tracks[2].Min.Kind != TrackAuto || tracks[2].Max.Kind != TrackAuto {
		t.Errorf("track2 = %+v want auto/auto", tracks[2])
	}
}

func TestParseTrackListMinmax(t *testing.T) {
	tl, ok := parseTrackList("minmax(100px, 1fr)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(0)
	if len(tracks) != 1 {
		t.Fatalf("len=%d want 1", len(tracks))
	}
	if tracks[0].Min.Kind != TrackLength || tracks[0].Min.Len.Value != 100 {
		t.Errorf("min = %+v want 100px", tracks[0].Min)
	}
	if tracks[0].Max.Kind != TrackFlex || tracks[0].Max.Fr != 1 {
		t.Errorf("max = %+v want 1fr", tracks[0].Max)
	}
}

func TestParseTrackListRepeatN(t *testing.T) {
	tl, ok := parseTrackList("repeat(3, 50px)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(0)
	if len(tracks) != 3 {
		t.Fatalf("len=%d want 3", len(tracks))
	}
	for i, tr := range tracks {
		if tr.Min.Len.Value != 50 {
			t.Errorf("track %d = %+v want 50px", i, tr)
		}
	}
}

func TestParseTrackListRepeatInner(t *testing.T) {
	// repeat(2, 1fr 2fr) => 1fr 2fr 1fr 2fr.
	tl, _ := parseTrackList("repeat(2, 1fr 2fr)")
	tracks := tl.Expand(0)
	want := []float64{1, 2, 1, 2}
	if len(tracks) != 4 {
		t.Fatalf("len=%d want 4", len(tracks))
	}
	for i, fr := range want {
		if tracks[i].Max.Fr != fr {
			t.Errorf("track %d fr=%v want %v", i, tracks[i].Max.Fr, fr)
		}
	}
}

func TestParseTrackListAutoFill(t *testing.T) {
	// repeat(auto-fill, 100px) in a 350px container with 0 gap => floor(350/100)=3 tracks.
	tl, ok := parseTrackList("repeat(auto-fill, 100px)")
	if !ok {
		t.Fatal("ok=false")
	}
	tracks := tl.Expand(350)
	if len(tracks) != 3 {
		t.Fatalf("auto-fill len=%d want 3", len(tracks))
	}
	// Indefinite container (size 0) => 1 repetition (spec fallback).
	if n := len(tl.Expand(0)); n != 1 {
		t.Fatalf("auto-fill indefinite len=%d want 1", n)
	}
}

func TestParseTrackListBadDegrades(t *testing.T) {
	if _, ok := parseTrackList("nonsense !!!"); ok {
		t.Error("expected ok=false for garbage track list")
	}
}

func TestParseTemplateAreas(t *testing.T) {
	// "a a b" "a a c" => Named["a"]={1,2,1,2}, Named["b"]={1,1,3,3}, Named["c"]={2,2,3,3},
	// Rows=2, Cols=3.
	ga, ok := parseTemplateAreas(`"a a b" "a a c"`)
	if !ok {
		t.Fatal("parseTemplateAreas ok=false")
	}
	if ga.Rows != 2 {
		t.Errorf("Rows=%d want 2", ga.Rows)
	}
	if ga.Cols != 3 {
		t.Errorf("Cols=%d want 3", ga.Cols)
	}
	wantA := GridRect{RowStart: 1, RowEnd: 2, ColStart: 1, ColEnd: 2}
	if ga.Named["a"] != wantA {
		t.Errorf("Named[a]=%+v want %+v", ga.Named["a"], wantA)
	}
	wantB := GridRect{RowStart: 1, RowEnd: 1, ColStart: 3, ColEnd: 3}
	if ga.Named["b"] != wantB {
		t.Errorf("Named[b]=%+v want %+v", ga.Named["b"], wantB)
	}
	wantC := GridRect{RowStart: 2, RowEnd: 2, ColStart: 3, ColEnd: 3}
	if ga.Named["c"] != wantC {
		t.Errorf("Named[c]=%+v want %+v", ga.Named["c"], wantC)
	}
}

func TestParseTemplateAreasNonRectangular(t *testing.T) {
	// "a a" "a b" => 'a' is non-rectangular (L-shaped) => ok=false
	if _, ok := parseTemplateAreas(`"a a" "a b"`); ok {
		t.Error("expected ok=false for non-rectangular area 'a'")
	}
}

func TestParseTemplateAreasRagged(t *testing.T) {
	// "a a" "a a a" => ragged rows => ok=false
	if _, ok := parseTemplateAreas(`"a a" "a a a"`); ok {
		t.Error("expected ok=false for ragged rows")
	}
}

func TestParseGridColumnNum(t *testing.T) {
	// grid-column: 1 / 3
	start, end, ok := parseGridColumnRow("1 / 3")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != LineNum || start.N != 1 {
		t.Errorf("start=%+v want LineNum{1}", start)
	}
	if end.Kind != LineNum || end.N != 3 {
		t.Errorf("end=%+v want LineNum{3}", end)
	}
}

func TestParseGridColumnSpan(t *testing.T) {
	// grid-column: span 2 => start LineSpan{2}, end auto
	start, end, ok := parseGridColumnRow("span 2")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != LineSpan || start.N != 2 {
		t.Errorf("start=%+v want LineSpan{2}", start)
	}
	if end.Kind != LineAuto {
		t.Errorf("end=%+v want LineAuto", end)
	}
}

func TestParseGridColumnNegative(t *testing.T) {
	// grid-column: 1 / -1 => start LineNum{1}, end LineNum{-1}
	start, end, ok := parseGridColumnRow("1 / -1")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != LineNum || start.N != 1 {
		t.Errorf("start=%+v want LineNum{1}", start)
	}
	if end.Kind != LineNum || end.N != -1 {
		t.Errorf("end=%+v want LineNum{-1}", end)
	}
}

func TestParseGridAreaName(t *testing.T) {
	// grid-area: foo => {AreaName:"foo"}
	p, ok := parseGridArea("foo")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.AreaName != "foo" {
		t.Errorf("AreaName=%q want %q", p.AreaName, "foo")
	}
	// All endpoints should be auto.
	if p.RowStart.Kind != LineAuto || p.RowEnd.Kind != LineAuto ||
		p.ColStart.Kind != LineAuto || p.ColEnd.Kind != LineAuto {
		t.Errorf("expected all auto endpoints for area name, got %+v", p)
	}
}

func TestParseGridAreaFourValues(t *testing.T) {
	// grid-area: 1 / 1 / 3 / 2 => RowStart=1,ColStart=1,RowEnd=3,ColEnd=2
	p, ok := parseGridArea("1 / 1 / 3 / 2")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != LineNum || p.RowStart.N != 1 {
		t.Errorf("RowStart=%+v want LineNum{1}", p.RowStart)
	}
	if p.ColStart.Kind != LineNum || p.ColStart.N != 1 {
		t.Errorf("ColStart=%+v want LineNum{1}", p.ColStart)
	}
	if p.RowEnd.Kind != LineNum || p.RowEnd.N != 3 {
		t.Errorf("RowEnd=%+v want LineNum{3}", p.RowEnd)
	}
	if p.ColEnd.Kind != LineNum || p.ColEnd.N != 2 {
		t.Errorf("ColEnd=%+v want LineNum{2}", p.ColEnd)
	}
	if p.AreaName != "" {
		t.Errorf("AreaName=%q want empty", p.AreaName)
	}
}

func TestParseGridAreaThreeValues(t *testing.T) {
	// grid-area: 1 / 2 / 3 (3 values) => RowStart=LineNum{1}, ColStart=LineNum{2},
	// RowEnd=LineNum{3}; ColEnd omitted: col-start is LineNum (not LineName) so ColEnd=LineAuto.
	p, ok := parseGridArea("1 / 2 / 3")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != LineNum || p.RowStart.N != 1 {
		t.Errorf("RowStart=%+v want LineNum{1}", p.RowStart)
	}
	if p.ColStart.Kind != LineNum || p.ColStart.N != 2 {
		t.Errorf("ColStart=%+v want LineNum{2}", p.ColStart)
	}
	if p.RowEnd.Kind != LineNum || p.RowEnd.N != 3 {
		t.Errorf("RowEnd=%+v want LineNum{3}", p.RowEnd)
	}
	// col-start is numeric, not an ident, so col-end defaults to auto.
	if p.ColEnd.Kind != LineAuto {
		t.Errorf("ColEnd=%+v want LineAuto (col-start was numeric, not ident)", p.ColEnd)
	}
}

func TestParseGridAreaTwoIdents(t *testing.T) {
	// grid-area: header / main (2 values, both idents) =>
	// RowStart=LineName{"header"}, ColStart=LineName{"main"},
	// RowEnd copies RowStart (ident) = LineName{"header"},
	// ColEnd copies ColStart (ident) = LineName{"main"}.
	p, ok := parseGridArea("header / main")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != LineName || p.RowStart.Name != "header" {
		t.Errorf("RowStart=%+v want LineName{header}", p.RowStart)
	}
	if p.ColStart.Kind != LineName || p.ColStart.Name != "main" {
		t.Errorf("ColStart=%+v want LineName{main}", p.ColStart)
	}
	// Omitted row-end: row-start is ident, so row-end copies row-start.
	if p.RowEnd.Kind != LineName || p.RowEnd.Name != "header" {
		t.Errorf("RowEnd=%+v want LineName{header} (copied from row-start)", p.RowEnd)
	}
	// Omitted col-end: col-start is ident, so col-end copies col-start.
	if p.ColEnd.Kind != LineName || p.ColEnd.Name != "main" {
		t.Errorf("ColEnd=%+v want LineName{main} (copied from col-start)", p.ColEnd)
	}
}

func TestParseGridColumnRowBareIdent(t *testing.T) {
	// grid-column: foo (single ident) => both start and end are LineName{"foo"}.
	start, end, ok := parseGridColumnRow("foo")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != LineName || start.Name != "foo" {
		t.Errorf("start=%+v want LineName{foo}", start)
	}
	if end.Kind != LineName || end.Name != "foo" {
		t.Errorf("end=%+v want LineName{foo} (copied from start for bare ident)", end)
	}
}

func TestParseTemplateAreasSingleCell(t *testing.T) {
	// Single-cell template: "a" => Named["a"]={1,1,1,1}, Rows=1, Cols=1.
	ga, ok := parseTemplateAreas(`"a"`)
	if !ok {
		t.Fatal("parseTemplateAreas ok=false")
	}
	if ga.Rows != 1 {
		t.Errorf("Rows=%d want 1", ga.Rows)
	}
	if ga.Cols != 1 {
		t.Errorf("Cols=%d want 1", ga.Cols)
	}
	want := GridRect{RowStart: 1, RowEnd: 1, ColStart: 1, ColEnd: 1}
	if ga.Named["a"] != want {
		t.Errorf("Named[a]=%+v want %+v", ga.Named["a"], want)
	}
}

func TestParseTemplateAreasEmpty(t *testing.T) {
	// Empty/whitespace input => ok=false, no panic.
	if _, ok := parseTemplateAreas(""); ok {
		t.Error("expected ok=false for empty input")
	}
	if _, ok := parseTemplateAreas("   "); ok {
		t.Error("expected ok=false for whitespace-only input")
	}
}

func TestParseTemplateAreasMultiDotNullCell(t *testing.T) {
	// CSS §7.3: ".." and "..." are also null cells (one or more "." characters).
	// ".. a" ".. a" => Named["a"]={1,2,2,2}, Rows=2, Cols=2.
	ga, ok := parseTemplateAreas(`".. a" ".. a"`)
	if !ok {
		t.Fatal("parseTemplateAreas with multi-dot null cell ok=false")
	}
	if ga.Rows != 2 || ga.Cols != 2 {
		t.Errorf("Rows=%d Cols=%d want 2x2", ga.Rows, ga.Cols)
	}
	// ".." cells must be treated as null (not named), so only "a" is in Named.
	if _, exists := ga.Named[".."]; exists {
		t.Error("multi-dot '..' should be a null cell, not a named area")
	}
	wantA := GridRect{RowStart: 1, RowEnd: 2, ColStart: 2, ColEnd: 2}
	if ga.Named["a"] != wantA {
		t.Errorf("Named[a]=%+v want %+v", ga.Named["a"], wantA)
	}
}
