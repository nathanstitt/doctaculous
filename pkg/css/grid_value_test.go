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
	if tracks[0].Min.Kind != trackLength || tracks[0].Min.Len.Value != 100 || tracks[0].Min.Len.Unit != UnitPx {
		t.Errorf("track0 = %+v want 100px", tracks[0].Min)
	}
	if tracks[1].Max.Kind != trackFlex || tracks[1].Max.Fr != 1 {
		t.Errorf("track1 = %+v want 1fr", tracks[1].Max)
	}
	if tracks[2].Min.Kind != trackAuto || tracks[2].Max.Kind != trackAuto {
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
	if tracks[0].Min.Kind != trackLength || tracks[0].Min.Len.Value != 100 {
		t.Errorf("min = %+v want 100px", tracks[0].Min)
	}
	if tracks[0].Max.Kind != trackFlex || tracks[0].Max.Fr != 1 {
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
	if start.Kind != lineNum || start.N != 1 {
		t.Errorf("start=%+v want lineNum{1}", start)
	}
	if end.Kind != lineNum || end.N != 3 {
		t.Errorf("end=%+v want lineNum{3}", end)
	}
}

func TestParseGridColumnSpan(t *testing.T) {
	// grid-column: span 2 => start lineSpan{2}, end auto
	start, end, ok := parseGridColumnRow("span 2")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != lineSpan || start.N != 2 {
		t.Errorf("start=%+v want lineSpan{2}", start)
	}
	if end.Kind != lineAuto {
		t.Errorf("end=%+v want lineAuto", end)
	}
}

func TestParseGridColumnNegative(t *testing.T) {
	// grid-column: 1 / -1 => start lineNum{1}, end lineNum{-1}
	start, end, ok := parseGridColumnRow("1 / -1")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != lineNum || start.N != 1 {
		t.Errorf("start=%+v want lineNum{1}", start)
	}
	if end.Kind != lineNum || end.N != -1 {
		t.Errorf("end=%+v want lineNum{-1}", end)
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
	if p.RowStart.Kind != lineAuto || p.RowEnd.Kind != lineAuto ||
		p.ColStart.Kind != lineAuto || p.ColEnd.Kind != lineAuto {
		t.Errorf("expected all auto endpoints for area name, got %+v", p)
	}
}

func TestParseGridAreaFourValues(t *testing.T) {
	// grid-area: 1 / 1 / 3 / 2 => RowStart=1,ColStart=1,RowEnd=3,ColEnd=2
	p, ok := parseGridArea("1 / 1 / 3 / 2")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != lineNum || p.RowStart.N != 1 {
		t.Errorf("RowStart=%+v want lineNum{1}", p.RowStart)
	}
	if p.ColStart.Kind != lineNum || p.ColStart.N != 1 {
		t.Errorf("ColStart=%+v want lineNum{1}", p.ColStart)
	}
	if p.RowEnd.Kind != lineNum || p.RowEnd.N != 3 {
		t.Errorf("RowEnd=%+v want lineNum{3}", p.RowEnd)
	}
	if p.ColEnd.Kind != lineNum || p.ColEnd.N != 2 {
		t.Errorf("ColEnd=%+v want lineNum{2}", p.ColEnd)
	}
	if p.AreaName != "" {
		t.Errorf("AreaName=%q want empty", p.AreaName)
	}
}

func TestParseGridAreaThreeValues(t *testing.T) {
	// grid-area: 1 / 2 / 3 (3 values) => RowStart=lineNum{1}, ColStart=lineNum{2},
	// RowEnd=lineNum{3}; ColEnd omitted: col-start is lineNum (not lineName) so ColEnd=lineAuto.
	p, ok := parseGridArea("1 / 2 / 3")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != lineNum || p.RowStart.N != 1 {
		t.Errorf("RowStart=%+v want lineNum{1}", p.RowStart)
	}
	if p.ColStart.Kind != lineNum || p.ColStart.N != 2 {
		t.Errorf("ColStart=%+v want lineNum{2}", p.ColStart)
	}
	if p.RowEnd.Kind != lineNum || p.RowEnd.N != 3 {
		t.Errorf("RowEnd=%+v want lineNum{3}", p.RowEnd)
	}
	// col-start is numeric, not an ident, so col-end defaults to auto.
	if p.ColEnd.Kind != lineAuto {
		t.Errorf("ColEnd=%+v want lineAuto (col-start was numeric, not ident)", p.ColEnd)
	}
}

func TestParseGridAreaTwoIdents(t *testing.T) {
	// grid-area: header / main (2 values, both idents) =>
	// RowStart=lineName{"header"}, ColStart=lineName{"main"},
	// RowEnd copies RowStart (ident) = lineName{"header"},
	// ColEnd copies ColStart (ident) = lineName{"main"}.
	p, ok := parseGridArea("header / main")
	if !ok {
		t.Fatal("parseGridArea ok=false")
	}
	if p.RowStart.Kind != lineName || p.RowStart.Name != "header" {
		t.Errorf("RowStart=%+v want lineName{header}", p.RowStart)
	}
	if p.ColStart.Kind != lineName || p.ColStart.Name != "main" {
		t.Errorf("ColStart=%+v want lineName{main}", p.ColStart)
	}
	// Omitted row-end: row-start is ident, so row-end copies row-start.
	if p.RowEnd.Kind != lineName || p.RowEnd.Name != "header" {
		t.Errorf("RowEnd=%+v want lineName{header} (copied from row-start)", p.RowEnd)
	}
	// Omitted col-end: col-start is ident, so col-end copies col-start.
	if p.ColEnd.Kind != lineName || p.ColEnd.Name != "main" {
		t.Errorf("ColEnd=%+v want lineName{main} (copied from col-start)", p.ColEnd)
	}
}

func TestParseGridColumnRowBareIdent(t *testing.T) {
	// grid-column: foo (single ident) => both start and end are lineName{"foo"}.
	start, end, ok := parseGridColumnRow("foo")
	if !ok {
		t.Fatal("parseGridColumnRow ok=false")
	}
	if start.Kind != lineName || start.Name != "foo" {
		t.Errorf("start=%+v want lineName{foo}", start)
	}
	if end.Kind != lineName || end.Name != "foo" {
		t.Errorf("end=%+v want lineName{foo} (copied from start for bare ident)", end)
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
