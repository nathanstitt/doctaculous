package inline

import "testing"

func TestPlace(t *testing.T) {
	// availWidth=100, widthPt=40, originX=10 unless noted; spaceCount=3.
	tests := []struct {
		name       string
		align      Align
		originX    float64
		availWidth float64
		widthPt    float64
		spaceCount int
		last       bool
		wantStart  float64
		wantExtra  float64
	}{
		{name: "left", align: AlignLeft, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 3, wantStart: 10, wantExtra: 0},
		{name: "right", align: AlignRight, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 3, wantStart: 70, wantExtra: 0},      // 10 + (100-40)
		{name: "center", align: AlignCenter, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 3, wantStart: 40, wantExtra: 0},    // 10 + (100-40)/2
		{name: "justify", align: AlignJustify, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 3, wantStart: 10, wantExtra: 20}, // (100-40)/3
		{name: "justify last suppressed", align: AlignJustify, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 3, last: true, wantStart: 10, wantExtra: 0},
		{name: "justify zero spaces", align: AlignJustify, originX: 10, availWidth: 100, widthPt: 40, spaceCount: 0, wantStart: 10, wantExtra: 0},
		{name: "justify over-full clamps", align: AlignJustify, originX: 10, availWidth: 100, widthPt: 130, spaceCount: 3, wantStart: 10, wantExtra: 0}, // (100-130)/3 < 0 -> 0
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Place(tt.align, tt.originX, tt.availWidth, tt.widthPt, tt.spaceCount, tt.last)
			if p.StartX != tt.wantStart {
				t.Errorf("StartX = %v, want %v", p.StartX, tt.wantStart)
			}
			if p.ExtraPerSpace != tt.wantExtra {
				t.Errorf("ExtraPerSpace = %v, want %v", p.ExtraPerSpace, tt.wantExtra)
			}
		})
	}
}

func TestVisibleWidthExcludesTrailingSpaces(t *testing.T) {
	// "aa " with advance 1: visible width counts the two letters but not the
	// trailing space.
	glyphs := mkGlyphs("aa ", 1)
	if got := VisibleWidth(glyphs); got != 2 {
		t.Errorf("VisibleWidth = %v, want 2 (trailing space excluded)", got)
	}
	// An interior space still counts toward width.
	if got := VisibleWidth(mkGlyphs("a a", 1)); got != 3 {
		t.Errorf("VisibleWidth = %v, want 3 (interior space counts)", got)
	}
}

func TestCountSpacesExcludesTrailingSpaces(t *testing.T) {
	// "a a a  " has two interior gaps; the two trailing spaces are excluded.
	if got := CountSpaces(mkGlyphs("a a a  ", 1)); got != 2 {
		t.Errorf("CountSpaces = %v, want 2", got)
	}
	if got := CountSpaces(mkGlyphs("nospace", 1)); got != 0 {
		t.Errorf("CountSpaces = %v, want 0", got)
	}
}

func TestMakeLineMaxMetrics(t *testing.T) {
	// Mixed-size glyphs: max ascent/descent/line-gap win, and width excludes the
	// trailing space.
	glyphs := []Glyph{
		{Advance: 5, AscentPt: 8, DescentPt: 2, LineGapPt: 1},
		{Advance: 7, AscentPt: 12, DescentPt: 4, LineGapPt: 3},
		{Advance: 3, Space: true}, // trailing space: excluded from width
	}
	l := MakeLine(glyphs)
	if l.WidthPt != 12 {
		t.Errorf("WidthPt = %v, want 12 (5+7, trailing space excluded)", l.WidthPt)
	}
	if l.AscentPt != 12 {
		t.Errorf("AscentPt = %v, want 12", l.AscentPt)
	}
	if l.DescentPt != 4 {
		t.Errorf("DescentPt = %v, want 4", l.DescentPt)
	}
	if l.LineGapPt != 3 {
		t.Errorf("LineGapPt = %v, want 3", l.LineGapPt)
	}
}
