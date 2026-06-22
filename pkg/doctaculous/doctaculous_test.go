package doctaculous

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

func TestOpenBytesAndCount(t *testing.T) {
	doc, err := OpenBytes(gen.MultiPagePDF(3))
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 3 {
		t.Fatalf("PageCount = %d, want 3", doc.PageCount())
	}
}

func TestRasterizePage(t *testing.T) {
	doc, err := OpenBytes(gen.VectorPDF())
	if err != nil {
		t.Fatal(err)
	}
	img, err := doc.RasterizePage(context.Background(), 0, RasterOptions{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 612 || img.Bounds().Dy() != 792 {
		t.Errorf("image bounds = %v, want 612x792", img.Bounds())
	}
}

func TestRasterizePageOutOfRange(t *testing.T) {
	doc, _ := OpenBytes(gen.TextPDF())
	if _, err := doc.RasterizePage(context.Background(), 99, RasterOptions{}); err == nil {
		t.Fatal("expected error for out-of-range page")
	}
}

func TestRasterizePagesConcurrent(t *testing.T) {
	doc, err := OpenBytes(gen.MultiPagePDF(8))
	if err != nil {
		t.Fatal(err)
	}
	results := doc.RasterizePages(context.Background(), doc.AllPages(), RasterOptions{DPI: 72, Workers: 4})
	if len(results) != 8 {
		t.Fatalf("got %d results, want 8", len(results))
	}
	for i, r := range results {
		if r.Index != i {
			t.Errorf("result %d has Index %d, want %d (order not preserved)", i, r.Index, i)
		}
		if r.Err != nil {
			t.Errorf("page %d: %v", i, r.Err)
			continue
		}
		if r.Image == nil {
			t.Errorf("page %d: nil image", i)
		}
	}
}

func TestRasterizePagesSubset(t *testing.T) {
	doc, _ := OpenBytes(gen.MultiPagePDF(5))
	idx := []int{4, 0, 2}
	results := doc.RasterizePages(context.Background(), idx, RasterOptions{DPI: 72})
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for i, want := range idx {
		if results[i].Index != want {
			t.Errorf("results[%d].Index = %d, want %d", i, results[i].Index, want)
		}
	}
}

func TestRasterizePagesCancellation(t *testing.T) {
	doc, _ := OpenBytes(gen.MultiPagePDF(8))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting
	results := doc.RasterizePages(ctx, doc.AllPages(), RasterOptions{DPI: 72})
	// Every result should report the cancellation rather than a rendered image.
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("page %d: expected cancellation error, got image=%v", i, r.Image != nil)
		}
	}
}

func TestRasterizePagesEmpty(t *testing.T) {
	doc, _ := OpenBytes(gen.TextPDF())
	results := doc.RasterizePages(context.Background(), nil, RasterOptions{})
	if len(results) != 0 {
		t.Errorf("empty indices returned %d results", len(results))
	}
}
