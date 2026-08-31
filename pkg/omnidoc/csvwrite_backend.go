package omnidoc

import (
	"context"
	"fmt"
	"io"

	"github.com/nathanstitt/omnidoc/pkg/render/csvwrite"
)

// CSVOptions controls conversion to CSV/TSV.
type CSVOptions struct {
	// Logf receives degradation diagnostics — the count of dropped non-table
	// blocks, or a warning that the document has no tables (the output is then
	// empty). nil -> no-op.
	Logf func(string, ...any)
}

// WriteCSV writes the document's TABLES to out as comma-separated values —
// tables in document order, separated by one blank line, with spanned cells
// duplicated into every covered slot. Non-table content is dropped (logged): a
// spreadsheet is a grid, and prose has no columnar meaning. Like the other
// structure writers it works on any document that can produce a cssbox tree —
// including an opened PDF, whose tables are recovered by extraction.
func (d *Document) WriteCSV(ctx context.Context, out io.Writer, opts CSVOptions) error {
	return d.writeSeparated(ctx, out, ',', opts)
}

// WriteTSV is WriteCSV with a tab delimiter.
func (d *Document) WriteTSV(ctx context.Context, out io.Writer, opts CSVOptions) error {
	return d.writeSeparated(ctx, out, '\t', opts)
}

func (d *Document) writeSeparated(_ context.Context, out io.Writer, comma rune, opts CSVOptions) error {
	root, err := structureRoot(d, "WriteCSV")
	if err != nil {
		return err
	}
	if err := csvwrite.Write(root, out, csvwrite.Options{Comma: comma, Logf: opts.Logf}); err != nil {
		return fmt.Errorf("omnidoc: write csv: %w", err)
	}
	return nil
}
