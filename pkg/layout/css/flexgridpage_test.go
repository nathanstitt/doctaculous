package css

import (
	"testing"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// makeGrid builds a grid container with rowCount rows of rowH (one item per row), stacked
// from y0. Items are the direct children at successive Ys.
func makeGrid(y0, rowH float64, rowCount int) *Fragment {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayGrid}
	g := &Fragment{Y: y0, H: float64(rowCount) * rowH, Box: box}
	for i := 0; i < rowCount; i++ {
		ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
		g.Children = append(g.Children, &Fragment{Y: y0 + float64(i)*rowH, H: rowH, Box: ib})
	}
	return g
}

func TestSplitGridBetweenRows(t *testing.T) {
	// 6 single-item rows of 20pt; page bottom 65 ⇒ 3 rows fit.
	g := makeGrid(0, 20, 6)
	res := splitFlexGridForPage(g, 65)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 3 || len(res.tail.Children) != 3 {
		t.Errorf("rows split %d/%d, want 3/3", len(res.head.Children), len(res.tail.Children))
	}
}

// A multi-item grid row (two items sharing a Y band) is indivisible: the split falls
// between bands, keeping each row's items together.
func TestSplitGridTwoPerRow(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayGrid}
	g := &Fragment{Y: 0, H: 120, Box: box}
	for i := 0; i < 3; i++ { // 3 rows × 2 items
		y := float64(i) * 40
		for c := 0; c < 2; c++ {
			ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
			g.Children = append(g.Children, &Fragment{X: float64(c) * 60, Y: y, W: 60, H: 40, Box: ib})
		}
	}
	// Page bottom 90 ⇒ bands [0,40) and [40,80) fit (4 items), band [80,120) does not.
	res := splitFlexGridForPage(g, 90)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 4 || len(res.tail.Children) != 2 {
		t.Errorf("split %d/%d items, want 4/2", len(res.head.Children), len(res.tail.Children))
	}
	if res.head.H != 80 {
		t.Errorf("head H = %.1f, want 80 (rows 1-2)", res.head.H)
	}
}

// A column-direction flex container splits between its items (each item its own band).
func TestSplitFlexColumn(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex}
	f := &Fragment{Y: 0, H: 100, Box: box}
	for i := 0; i < 5; i++ {
		ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
		f.Children = append(f.Children, &Fragment{Y: float64(i) * 20, H: 20, Box: ib})
	}
	res := splitFlexGridForPage(f, 45) // 2 items fit (bottoms 20/40)
	if res.head == nil || res.tail == nil {
		t.Fatalf("expected a split, got head=%v tail=%v", res.head, res.tail)
	}
	if len(res.head.Children) != 2 || len(res.tail.Children) != 3 {
		t.Errorf("split %d/%d items, want 2/3", len(res.head.Children), len(res.tail.Children))
	}
}

// A single-line flex row (all items share ONE band) is indivisible: it fits whole or
// overflows whole.
func TestSplitFlexSingleRowIndivisible(t *testing.T) {
	box := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex}
	f := &Fragment{Y: 0, H: 60, Box: box}
	for c := 0; c < 3; c++ { // one row, three items side by side (same Y band)
		ib := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayBlock}
		f.Children = append(f.Children, &Fragment{X: float64(c) * 40, Y: 0, W: 40, H: 60, Box: ib})
	}
	// Overflows the page from the top: cannot split → move whole.
	res := splitFlexGridForPage(f, 40)
	if res.head != nil || res.tail != f {
		t.Errorf("single-band overflow should move whole; got head=%v tail=%v", res.head, res.tail)
	}
	// Fits the page: no split.
	res2 := splitFlexGridForPage(f, 1000)
	if res2.head != f || res2.tail != nil {
		t.Errorf("single-band fit should yield head=f; got head=%v tail=%v", res2.head, res2.tail)
	}
}

// lineSplittable accepts flex and grid fragments.
func TestLineSplittableFlexGrid(t *testing.T) {
	if !lineSplittable(makeGrid(0, 20, 3)) {
		t.Errorf("a grid fragment should be splittable")
	}
	fbox := &cssbox.Box{Kind: cssbox.BoxBlock, Display: cssbox.DisplayFlex}
	f := &Fragment{Y: 0, H: 40, Box: fbox, Children: []*Fragment{{Y: 0, H: 20}, {Y: 20, H: 20}}}
	if !lineSplittable(f) {
		t.Errorf("a flex fragment should be splittable")
	}
	// break-inside: avoid disqualifies.
	f.Box.Style = gcss.ComputedStyle{BreakInside: "avoid"}
	if lineSplittable(f) {
		t.Errorf("break-inside:avoid flex must not be splittable")
	}
}
