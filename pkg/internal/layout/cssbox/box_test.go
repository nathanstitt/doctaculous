package cssbox

import "testing"

func TestBoxKindPredicates(t *testing.T) {
	cases := []struct {
		k                   BoxKind
		blockLvl, inlineLvl bool
	}{
		{BoxBlock, true, false},
		{BoxAnonBlock, true, false},
		{BoxInline, false, true},
		{BoxAnonInline, false, true},
		{BoxAnonTablePart, true, false},
		{BoxText, false, true},
		{BoxReplaced, false, true}, // a bare <img> is inline-level by default
	}
	for _, c := range cases {
		if got := c.k.IsBlockLevel(); got != c.blockLvl {
			t.Errorf("%v.IsBlockLevel() = %v, want %v", c.k, got, c.blockLvl)
		}
		if got := c.k.IsInlineLevel(); got != c.inlineLvl {
			t.Errorf("%v.IsInlineLevel() = %v, want %v", c.k, got, c.inlineLvl)
		}
	}
}

func TestLeafBoxesHaveNoChildren(t *testing.T) {
	// Documents the contract that text/replaced boxes are leaves.
	for _, k := range []BoxKind{BoxText, BoxReplaced} {
		b := &Box{Kind: k}
		if len(b.Children) != 0 {
			t.Errorf("%v leaf unexpectedly has children", k)
		}
	}
}

func TestTableDisplayKinds(t *testing.T) {
	// The new table-part display kinds must be distinct values.
	kinds := []DisplayKind{
		DisplayTable, DisplayTableRowGroup, DisplayTableHeaderGroup,
		DisplayTableFooterGroup, DisplayTableRow, DisplayTableColumn,
		DisplayTableColumnGroup, DisplayTableCaption, DisplayTableCell,
	}
	seen := map[DisplayKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate DisplayKind value %d", k)
		}
		seen[k] = true
	}
}

func TestSpanFieldsDefaultZero(t *testing.T) {
	// A non-table box never sets spans; the grid builder reads zero as 1.
	b := &Box{Kind: BoxBlock}
	if b.ColSpan != 0 || b.RowSpan != 0 {
		t.Errorf("spans default to zero; got col=%d row=%d", b.ColSpan, b.RowSpan)
	}
}

func TestReplacedContentCarriesControl(t *testing.T) {
	rc := &ReplacedContent{Tag: "input", Control: CtrlCheckbox, Text: ""}
	if rc.Control != CtrlCheckbox {
		t.Errorf("Control = %v, want CtrlCheckbox", rc.Control)
	}
	// The zero value is CtrlNone (an <img>), so existing replaced content is unchanged.
	img := &ReplacedContent{Tag: "img"}
	if img.Control != CtrlNone {
		t.Errorf("default Control = %v, want CtrlNone", img.Control)
	}
}
