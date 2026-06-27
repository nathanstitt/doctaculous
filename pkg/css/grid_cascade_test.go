package css

import "testing"

// TestGridTemplateColumns checks that grid-template-columns parses into GridTemplateColumns.
func TestGridTemplateColumns(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-template-columns", "1fr 2fr")
	tracks := cs.GridTemplateColumns.Expand(0)
	if len(tracks) != 2 {
		t.Fatalf("grid-template-columns: 1fr 2fr => %d tracks, want 2", len(tracks))
	}
	if tracks[0].Max.Fr != 1 {
		t.Errorf("track0 fr = %v, want 1", tracks[0].Max.Fr)
	}
	if tracks[1].Max.Fr != 2 {
		t.Errorf("track1 fr = %v, want 2", tracks[1].Max.Fr)
	}
}

// TestGridTemplateRows checks that grid-template-rows parses.
func TestGridTemplateRows(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-template-rows", "100px auto")
	tracks := cs.GridTemplateRows.Expand(0)
	if len(tracks) != 2 {
		t.Fatalf("grid-template-rows: 100px auto => %d tracks, want 2", len(tracks))
	}
	if tracks[0].Min.Kind != trackLength || tracks[0].Min.Len.Value != 100 {
		t.Errorf("track0 = %+v, want 100px", tracks[0].Min)
	}
	if tracks[1].Min.Kind != trackAuto {
		t.Errorf("track1 = %+v, want auto", tracks[1].Min)
	}
}

// TestGridTemplateAreas checks that grid-template-areas parses.
func TestGridTemplateAreas(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-template-areas", `"a a b" "a a c"`)
	ga := cs.GridTemplateAreas
	if ga.Rows != 2 || ga.Cols != 3 {
		t.Fatalf("grid-template-areas rows=%d cols=%d, want 2/3", ga.Rows, ga.Cols)
	}
	if len(ga.Named) != 3 {
		t.Fatalf("grid-template-areas named count = %d, want 3", len(ga.Named))
	}
	a, ok := ga.Named["a"]
	if !ok {
		t.Fatal("named area 'a' missing")
	}
	if a.RowStart != 1 || a.RowEnd != 2 || a.ColStart != 1 || a.ColEnd != 2 {
		t.Errorf("area 'a' = %+v, want {1,2,1,2}", a)
	}
}

// TestGridAutoColumns checks that grid-auto-columns parses to []TrackSize.
func TestGridAutoColumns(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-auto-columns", "200px")
	if len(cs.GridAutoColumns) != 1 {
		t.Fatalf("grid-auto-columns: 200px => %d tracks, want 1", len(cs.GridAutoColumns))
	}
	if cs.GridAutoColumns[0].Min.Len.Value != 200 {
		t.Errorf("grid-auto-columns[0] = %+v, want 200px", cs.GridAutoColumns[0])
	}
}

// TestGridAutoRows checks that grid-auto-rows parses to []TrackSize.
func TestGridAutoRows(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-auto-rows", "50px")
	if len(cs.GridAutoRows) != 1 {
		t.Fatalf("grid-auto-rows: 50px => %d tracks, want 1", len(cs.GridAutoRows))
	}
	if cs.GridAutoRows[0].Min.Len.Value != 50 {
		t.Errorf("grid-auto-rows[0] = %+v, want 50px", cs.GridAutoRows[0])
	}
}

// TestGridAutoFlow checks normalizeAutoFlow and the property parse.
func TestGridAutoFlow(t *testing.T) {
	cases := []struct{ in, want string }{
		{"row", "row"},
		{"column", "column"},
		{"row dense", "row dense"},
		{"column dense", "column dense"},
		{"dense", "row dense"},
		{"dense column", "column dense"},
	}
	for _, c := range cases {
		cs := initialStyle()
		applyOne(&cs, "grid-auto-flow", c.in)
		if cs.GridAutoFlow != c.want {
			t.Errorf("grid-auto-flow: %q => %q, want %q", c.in, cs.GridAutoFlow, c.want)
		}
	}
}

// TestGridAutoFlowDefault checks the initial value.
func TestGridAutoFlowDefault(t *testing.T) {
	cs := initialStyle()
	if cs.GridAutoFlow != "row" {
		t.Errorf("initial GridAutoFlow = %q, want \"row\"", cs.GridAutoFlow)
	}
}

// TestJustifyItemsDefault checks the initial value.
func TestJustifyItemsDefault(t *testing.T) {
	cs := initialStyle()
	if cs.JustifyItems != "stretch" {
		t.Errorf("initial JustifyItems = %q, want \"stretch\"", cs.JustifyItems)
	}
}

// TestJustifyItems checks parse of justify-items.
func TestJustifyItems(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "justify-items", "center")
	if cs.JustifyItems != "center" {
		t.Errorf("justify-items: center => %q, want \"center\"", cs.JustifyItems)
	}
}

// TestJustifySelfDefault checks the initial value.
func TestJustifySelfDefault(t *testing.T) {
	cs := initialStyle()
	if cs.JustifySelf != "auto" {
		t.Errorf("initial JustifySelf = %q, want \"auto\"", cs.JustifySelf)
	}
}

// TestJustifySelf checks parse of justify-self.
func TestJustifySelf(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "justify-self", "start")
	if cs.JustifySelf != "start" {
		t.Errorf("justify-self: start => %q, want \"start\"", cs.JustifySelf)
	}
}

// TestAlignContentDefault checks the initial value.
func TestAlignContentDefault(t *testing.T) {
	cs := initialStyle()
	if cs.AlignContent != "start" {
		t.Errorf("initial AlignContent = %q, want \"start\"", cs.AlignContent)
	}
}

// TestAlignContent checks parse of align-content.
func TestAlignContent(t *testing.T) {
	cases := []string{"start", "end", "center", "space-between", "space-around", "space-evenly", "stretch"}
	for _, v := range cases {
		cs := initialStyle()
		applyOne(&cs, "align-content", v)
		if cs.AlignContent != v {
			t.Errorf("align-content: %q => %q, want same", v, cs.AlignContent)
		}
	}
}

// TestGridPlacementColumn checks that grid-column writes cs.GridPlacement.ColStart/ColEnd.
func TestGridPlacementColumn(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-column", "1 / 3")
	if cs.GridPlacement.ColStart.Kind != lineNum || cs.GridPlacement.ColStart.N != 1 {
		t.Errorf("ColStart = %+v, want lineNum{1}", cs.GridPlacement.ColStart)
	}
	if cs.GridPlacement.ColEnd.Kind != lineNum || cs.GridPlacement.ColEnd.N != 3 {
		t.Errorf("ColEnd = %+v, want lineNum{3}", cs.GridPlacement.ColEnd)
	}
}

// TestGridPlacementRow checks that grid-row writes cs.GridPlacement.RowStart/RowEnd.
func TestGridPlacementRow(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-row", "2 / 4")
	if cs.GridPlacement.RowStart.Kind != lineNum || cs.GridPlacement.RowStart.N != 2 {
		t.Errorf("RowStart = %+v, want lineNum{2}", cs.GridPlacement.RowStart)
	}
	if cs.GridPlacement.RowEnd.Kind != lineNum || cs.GridPlacement.RowEnd.N != 4 {
		t.Errorf("RowEnd = %+v, want lineNum{4}", cs.GridPlacement.RowEnd)
	}
}

// TestGridPlacementArea checks that grid-area with an ident sets AreaName.
func TestGridPlacementArea(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-area", "header")
	if cs.GridPlacement.AreaName != "header" {
		t.Errorf("GridPlacement.AreaName = %q, want \"header\"", cs.GridPlacement.AreaName)
	}
}

// TestGridPlacementAreaFourValues checks the 4-value grid-area form.
func TestGridPlacementAreaFourValues(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "grid-area", "1 / 1 / 3 / 2")
	if cs.GridPlacement.RowStart.N != 1 {
		t.Errorf("RowStart = %+v, want lineNum{1}", cs.GridPlacement.RowStart)
	}
	if cs.GridPlacement.ColStart.N != 1 {
		t.Errorf("ColStart = %+v, want lineNum{1}", cs.GridPlacement.ColStart)
	}
	if cs.GridPlacement.RowEnd.N != 3 {
		t.Errorf("RowEnd = %+v, want lineNum{3}", cs.GridPlacement.RowEnd)
	}
	if cs.GridPlacement.ColEnd.N != 2 {
		t.Errorf("ColEnd = %+v, want lineNum{2}", cs.GridPlacement.ColEnd)
	}
}

// TestAlignItemsAcceptsGridSpellings verifies that "start" and "flex-start" are
// both accepted on align-items (grid and flex share the field).
func TestAlignItemsAcceptsGridSpellings(t *testing.T) {
	cs := initialStyle()
	applyOne(&cs, "align-items", "start")
	if cs.AlignItems != "start" {
		t.Errorf("align-items: start => %q, want \"start\"", cs.AlignItems)
	}

	cs2 := initialStyle()
	applyOne(&cs2, "align-items", "flex-start")
	if cs2.AlignItems != "flex-start" {
		t.Errorf("align-items: flex-start => %q, want \"flex-start\"", cs2.AlignItems)
	}

	cs3 := initialStyle()
	applyOne(&cs3, "align-items", "end")
	if cs3.AlignItems != "end" {
		t.Errorf("align-items: end => %q, want \"end\"", cs3.AlignItems)
	}
}

// TestAlignSelfAcceptsGridSpellings verifies that "start"/"end" are accepted on align-self.
func TestAlignSelfAcceptsGridSpellings(t *testing.T) {
	for _, v := range []string{"start", "end", "flex-start", "flex-end"} {
		cs := initialStyle()
		applyOne(&cs, "align-self", v)
		if cs.AlignSelf != v {
			t.Errorf("align-self: %q => %q, want same", v, cs.AlignSelf)
		}
	}
}

// TestJustifyContentAcceptsGridSpellings verifies that "start"/"end"/"space-evenly"
// are accepted on justify-content (previously only flex-start/flex-end/space-evenly).
func TestJustifyContentAcceptsGridSpellings(t *testing.T) {
	for _, v := range []string{"start", "end", "center", "space-between", "space-around", "space-evenly", "flex-start", "flex-end"} {
		cs := initialStyle()
		applyOne(&cs, "justify-content", v)
		if cs.JustifyContent != v {
			t.Errorf("justify-content: %q => %q, want same", v, cs.JustifyContent)
		}
	}
}
