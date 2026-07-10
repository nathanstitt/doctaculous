package doctaculous

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/doctaculous/pkg/render/xlsxwrite"
)

// XLSXOptions controls conversion to XLSX.
type XLSXOptions struct {
	// Logf receives degradation diagnostics — the count of dropped non-table
	// blocks, or a warning that the document has no tables (the workbook then
	// holds one empty sheet). nil -> no-op.
	Logf func(string, ...any)
}

// WriteXLSX writes the document's TABLES to out as an .xlsx workbook — one
// worksheet per table, named from the table's caption when present, with
// merged cells emitted natively and header rows bold. Non-table content is
// dropped (logged): a spreadsheet is a grid. Like the other structure writers
// it works on any document that can produce a cssbox tree — including an
// opened PDF, whose tables are recovered by extraction.
func (d *Document) WriteXLSX(_ context.Context, out io.Writer, opts XLSXOptions) error {
	rt, ok := d.r.(reflowTree)
	if !ok {
		return fmt.Errorf("doctaculous: WriteXLSX: document has no convertible structure")
	}
	if err := xlsxwrite.Write(rt.cssboxRoot(), out, xlsxwrite.Options{Logf: opts.Logf}); err != nil {
		return fmt.Errorf("doctaculous: write xlsx: %w", err)
	}
	return nil
}
