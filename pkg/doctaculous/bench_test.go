package doctaculous

import (
	"context"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// BenchmarkRasterizePages measures multi-page rasterization. Compare the
// Workers=1 and Workers=GOMAXPROCS sub-benchmarks to see the goroutine speedup:
//
//	go test ./pkg/doctaculous -run '^$' -bench RasterizePages
func BenchmarkRasterizePages(b *testing.B) {
	data := gen.MultiPagePDF(16)
	doc, err := OpenBytes(data)
	if err != nil {
		b.Fatal(err)
	}
	pages := doc.AllPages()

	b.Run("serial", func(b *testing.B) {
		for b.Loop() {
			res := doc.RasterizePages(context.Background(), pages, RasterOptions{DPI: 100, Workers: 1})
			requireOK(b, res)
		}
	})
	b.Run("parallel", func(b *testing.B) {
		for b.Loop() {
			res := doc.RasterizePages(context.Background(), pages, RasterOptions{DPI: 100})
			requireOK(b, res)
		}
	})
}

func requireOK(b *testing.B, res []PageResult) {
	b.Helper()
	for _, r := range res {
		if r.Err != nil {
			b.Fatalf("page %d: %v", r.Index, r.Err)
		}
	}
}
