package css

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// tbox/ttext build raw (pre-fixup) boxes with a given display.
func tbox(d cssbox.DisplayKind, kids ...*cssbox.Box) *cssbox.Box {
	fc := cssbox.TableFC
	if d == cssbox.DisplayTableCaption || d == cssbox.DisplayTableCell {
		fc = cssbox.BlockFC
	}
	return &cssbox.Box{Kind: cssbox.BoxBlock, Display: d, Formatting: fc, Children: kids}
}
func ttext(s string) *cssbox.Box {
	return &cssbox.Box{Kind: cssbox.BoxText, Display: cssbox.DisplayInline, Text: s}
}

func TestFixupCellWithoutRow(t *testing.T) {
	// table > cell  =>  table > anon-row-group > anon-row > cell
	tbl := tbox(cssbox.DisplayTable, tbox(cssbox.DisplayTableCell, ttext("X")))
	fixupTable(tbl)
	rg := tbl.Children[0]
	if rg.Display != cssbox.DisplayTableRowGroup || rg.Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("want anon row-group, got display=%v kind=%v", rg.Display, rg.Kind)
	}
	row := rg.Children[0]
	if row.Display != cssbox.DisplayTableRow || row.Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("want anon row, got display=%v kind=%v", row.Display, row.Kind)
	}
	if row.Children[0].Display != cssbox.DisplayTableCell {
		t.Fatalf("cell lost under anon row")
	}
}

func TestFixupStrayTextInRow(t *testing.T) {
	// row > text  =>  row > anon-cell > text
	row := tbox(cssbox.DisplayTableRow, ttext("hi"))
	wrapStrayInRow(row)
	if row.Children[0].Display != cssbox.DisplayTableCell || row.Children[0].Kind != cssbox.BoxAnonTablePart {
		t.Fatalf("stray text not wrapped in anon cell: %+v", row.Children[0])
	}
}

func TestFixupWhitespaceDropped(t *testing.T) {
	// table > "  " between two row-groups: the whitespace text is dropped, not wrapped.
	tbl := tbox(cssbox.DisplayTable,
		tbox(cssbox.DisplayTableRowGroup),
		ttext("   "),
		tbox(cssbox.DisplayTableRowGroup),
	)
	fixupTable(tbl)
	for _, c := range tbl.Children {
		if c.Kind == cssbox.BoxText {
			t.Fatalf("whitespace text survived in table: %q", c.Text)
		}
	}
	if len(tbl.Children) != 2 {
		t.Fatalf("want 2 row-groups after dropping whitespace, got %d", len(tbl.Children))
	}
}

func TestFixupCaptionStaysDirectChild(t *testing.T) {
	tbl := tbox(cssbox.DisplayTable,
		tbox(cssbox.DisplayTableCaption, ttext("cap")),
		tbox(cssbox.DisplayTableRow, tbox(cssbox.DisplayTableCell)),
	)
	fixupTable(tbl)
	if tbl.Children[0].Display != cssbox.DisplayTableCaption {
		t.Fatalf("caption not first child after fixup")
	}
	if len(tbl.Children) != 2 {
		t.Fatalf("want [caption, anon-row-group], got %d children", len(tbl.Children))
	}
	rg := tbl.Children[1]
	if rg.Kind != cssbox.BoxAnonTablePart || rg.Display != cssbox.DisplayTableRowGroup {
		t.Errorf("bare row after caption should be wrapped in an anon row-group; got kind=%v display=%v", rg.Kind, rg.Display)
	}
}
